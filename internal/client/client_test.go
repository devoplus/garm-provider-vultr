// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Devoplus

package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	garmErrors "github.com/cloudbase/garm-provider-common/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestClient returns a client pointed at the given handler.
func newTestClient(t *testing.T, handler http.Handler) (*VultrClient, *httptest.Server) {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	// The retry backoff is shortened so that the suite does not spend seconds
	// waiting. The retry behaviour itself is unchanged.
	cli, err := New("test-api-key", Options{
		BaseURL:        server.URL,
		HTTPClient:     server.Client(),
		RetryBaseDelay: 10 * time.Millisecond,
	})
	require.NoError(t, err)

	return cli, server
}

func TestNewRequiresAPIKey(t *testing.T) {
	_, err := New("", Options{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "api key")
}

func TestNewDefaultsBaseURL(t *testing.T) {
	cli, err := New("key", Options{})
	require.NoError(t, err)
	assert.Equal(t, "https://api.vultr.com", cli.baseURL)
	assert.Equal(t, "garm-provider-vultr", cli.userAgent)

	cli, err = New("key", Options{BaseURL: "https://proxy.example.com/"})
	require.NoError(t, err)
	assert.Equal(t, "https://proxy.example.com", cli.baseURL, "a trailing slash must not end up in request paths")
}

func TestCreateInstanceSendsAuthAndBody(t *testing.T) {
	var gotBody CreateInstanceRequest
	var gotAuth, gotMethod, gotPath, gotContentType string

	cli, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotContentType = r.Header.Get("Content-Type")
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		fmt.Fprint(w, `{"instance":{"id":"4f0f12e5-1f84-404f-aa84-85f431ea5ec2","status":"pending","power_status":"running","main_ip":"0.0.0.0"}}`)
	}))

	instance, err := cli.CreateInstance(context.Background(), &CreateInstanceRequest{
		Region:     "ewr",
		Plan:       "vc2-2c-4gb",
		Label:      "garm-test",
		OsID:       1743,
		Tags:       []string{"garm-controller-id-abc"},
		UserData:   "QmFzZTY0",
		EnableIPv6: Bool(true),
	})
	require.NoError(t, err)

	assert.Equal(t, "Bearer test-api-key", gotAuth)
	assert.Equal(t, http.MethodPost, gotMethod)
	assert.Equal(t, "/v2/instances", gotPath)
	assert.Equal(t, "application/json", gotContentType)
	assert.Equal(t, "ewr", gotBody.Region)
	assert.Equal(t, 1743, gotBody.OsID)
	require.NotNil(t, gotBody.EnableIPv6)
	assert.True(t, *gotBody.EnableIPv6)
	assert.Equal(t, "4f0f12e5-1f84-404f-aa84-85f431ea5ec2", instance.ID)
	assert.Equal(t, "pending", instance.Status)
}

// TestCreateInstanceOmitsUnsetBooleans checks that an option left alone is not
// sent as false, which would override the Vultr account default.
func TestCreateInstanceOmitsUnsetBooleans(t *testing.T) {
	var rawBody map[string]interface{}

	cli, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&rawBody))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		fmt.Fprint(w, `{"instance":{"id":"id-1"}}`)
	}))

	_, err := cli.CreateInstance(context.Background(), &CreateInstanceRequest{
		Region: "ewr",
		Plan:   "vc2-1c-1gb",
		OsID:   1743,
	})
	require.NoError(t, err)

	for _, field := range []string{"enable_ipv6", "disable_public_ipv4", "enable_vpc", "vpc_only", "ddos_protection"} {
		_, present := rawBody[field]
		assert.Falsef(t, present, "%s must be omitted when it was not set", field)
	}
}

func TestGetInstanceNotFoundIsGarmNotFound(t *testing.T) {
	cli, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":"Instance not found","status":404}`)
	}))

	_, err := cli.GetInstance(context.Background(), "4f0f12e5-1f84-404f-aa84-85f431ea5ec2")
	require.Error(t, err)

	assert.True(t, errors.Is(err, garmErrors.ErrNotFound), "a 404 must be reported as garm's ErrNotFound")
	assert.Contains(t, err.Error(), "Instance not found")

	var apiErr *APIError
	require.True(t, errors.As(err, &apiErr))
	assert.Equal(t, http.StatusNotFound, apiErr.StatusCode)
}

// TestOtherErrorsAreNotNotFound guards the boundary: only a 404 may look like
// a missing instance, otherwise a provider error would silently be treated as
// a successful delete.
func TestOtherErrorsAreNotNotFound(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusInternalServerError} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			cli, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				fmt.Fprint(w, `{"error":"nope"}`)
			}))

			_, err := cli.GetInstance(context.Background(), "4f0f12e5-1f84-404f-aa84-85f431ea5ec2")
			require.Error(t, err)
			assert.False(t, errors.Is(err, garmErrors.ErrNotFound))
		})
	}
}

func TestDeleteInstanceIsIdempotent(t *testing.T) {
	cli, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":"Instance not found","status":404}`)
	}))

	err := cli.DeleteInstance(context.Background(), "4f0f12e5-1f84-404f-aa84-85f431ea5ec2")
	assert.NoError(t, err, "deleting an instance that is already gone must be a no-op")
}

func TestDeleteInstanceSuccess(t *testing.T) {
	cli, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v2/instances/4f0f12e5-1f84-404f-aa84-85f431ea5ec2", r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	}))

	require.NoError(t, cli.DeleteInstance(context.Background(), "4f0f12e5-1f84-404f-aa84-85f431ea5ec2"))
}

func TestDeleteInstancePropagatesRealErrors(t *testing.T) {
	cli, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"error":"insufficient permissions"}`)
	}))

	err := cli.DeleteInstance(context.Background(), "4f0f12e5-1f84-404f-aa84-85f431ea5ec2")
	require.Error(t, err, "a permission failure must not be mistaken for a completed delete")
	assert.Contains(t, err.Error(), "insufficient permissions")
}

func TestListInstancesFollowsPagination(t *testing.T) {
	var requests []string

	cli, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.RawQuery)
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Query().Get("cursor") {
		case "":
			fmt.Fprint(w, `{"instances":[{"id":"id-1"},{"id":"id-2"}],"meta":{"total":3,"links":{"next":"cursor-2"}}}`)
		case "cursor-2":
			fmt.Fprint(w, `{"instances":[{"id":"id-3"}],"meta":{"total":3,"links":{"next":""}}}`)
		default:
			t.Fatalf("unexpected cursor %q", r.URL.Query().Get("cursor"))
		}
	}))

	instances, err := cli.ListInstances(context.Background())
	require.NoError(t, err)

	require.Len(t, instances, 3)
	assert.Equal(t, "id-1", instances[0].ID)
	assert.Equal(t, "id-3", instances[2].ID)

	require.Len(t, requests, 2)
	assert.Contains(t, requests[0], "per_page=500")
	assert.Contains(t, requests[1], "cursor=cursor-2")
}

// TestListInstancesStopsOnRepeatedCursor makes sure a misbehaving API cannot
// hang the provider in an endless pagination loop.
func TestListInstancesStopsOnRepeatedCursor(t *testing.T) {
	var calls int32

	cli, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"instances":[{"id":"id-1"}],"meta":{"links":{"next":"same-cursor"}}}`)
	}))

	instances, err := cli.ListInstances(context.Background())
	require.NoError(t, err)

	assert.Equal(t, int32(2), atomic.LoadInt32(&calls), "the loop must stop once the cursor repeats")
	assert.Len(t, instances, 2)
}

func TestListInstancesHandlesMissingMeta(t *testing.T) {
	cli, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"instances":[{"id":"id-1"}]}`)
	}))

	instances, err := cli.ListInstances(context.Background())
	require.NoError(t, err)
	assert.Len(t, instances, 1)
}

func TestStartAndHaltInstance(t *testing.T) {
	var paths []string

	cli, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		paths = append(paths, r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	}))

	require.NoError(t, cli.StartInstance(context.Background(), "id-1"))
	require.NoError(t, cli.HaltInstance(context.Background(), "id-1"))

	assert.Equal(t, []string{"/v2/instances/id-1/start", "/v2/instances/id-1/halt"}, paths)
}

func TestEmptyInstanceIDIsRejected(t *testing.T) {
	cli, err := New("key", Options{})
	require.NoError(t, err)

	ctx := context.Background()
	_, getErr := cli.GetInstance(ctx, "")
	assert.Error(t, getErr)
	assert.Error(t, cli.DeleteInstance(ctx, ""))
	assert.Error(t, cli.StartInstance(ctx, ""))
	assert.Error(t, cli.HaltInstance(ctx, ""))

	_, createErr := cli.CreateInstance(ctx, nil)
	assert.Error(t, createErr)
}

func TestRetriesRateLimitAndHonoursRetryAfter(t *testing.T) {
	var calls int32

	cli, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, `{"error":"rate limited"}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"instance":{"id":"id-1","status":"active","power_status":"running"}}`)
	}))

	start := time.Now()
	instance, err := cli.GetInstance(context.Background(), "4f0f12e5-1f84-404f-aa84-85f431ea5ec2")
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.Equal(t, "id-1", instance.ID)
	assert.Equal(t, int32(2), atomic.LoadInt32(&calls))
	assert.GreaterOrEqual(t, elapsed, time.Second, "the Retry-After wait must be honoured")
}

func TestRetriesServerErrors(t *testing.T) {
	var calls int32

	cli, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&calls, 1) < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, `{"error":"boom"}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"instance":{"id":"id-1"}}`)
	}))

	instance, err := cli.GetInstance(context.Background(), "4f0f12e5-1f84-404f-aa84-85f431ea5ec2")
	require.NoError(t, err)
	assert.Equal(t, "id-1", instance.ID)
	assert.Equal(t, int32(3), atomic.LoadInt32(&calls))
}

// TestDoesNotRetryClientErrors keeps a rejected request from being sent four
// times, which would waste the rate limit budget and could duplicate work.
func TestDoesNotRetryClientErrors(t *testing.T) {
	var calls int32

	cli, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":"bad plan"}`)
	}))

	_, err := cli.CreateInstance(context.Background(), &CreateInstanceRequest{Region: "ewr", Plan: "nope", OsID: 1})
	require.Error(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls), "a 400 must not be retried")
	assert.Contains(t, err.Error(), "bad plan")
}

func TestRespectsContextCancellation(t *testing.T) {
	cli, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := cli.GetInstance(ctx, "4f0f12e5-1f84-404f-aa84-85f431ea5ec2")
	require.Error(t, err)
}

func TestErrorMessageFallsBackToBody(t *testing.T) {
	assert.Equal(t, "", errorMessage(nil))
	assert.Equal(t, "Instance not found", errorMessage([]byte(`{"error":"Instance not found","status":404}`)))
	assert.Equal(t, "<html>gateway timeout</html>", errorMessage([]byte("<html>gateway timeout</html>")))

	long := make([]byte, 1024)
	for i := range long {
		long[i] = 'x'
	}
	assert.Len(t, errorMessage(long), 515, "a long body must be truncated before it reaches the logs")
}

func TestParseRetryAfter(t *testing.T) {
	assert.Equal(t, time.Duration(0), parseRetryAfter(""))
	assert.Equal(t, time.Duration(0), parseRetryAfter("not-a-number"))
	assert.Equal(t, time.Duration(0), parseRetryAfter("-5"))
	assert.Equal(t, 3*time.Second, parseRetryAfter(" 3 "))
}

func TestRetryDelayIsCapped(t *testing.T) {
	cli, err := New("key", Options{})
	require.NoError(t, err)

	hostileHeader := &rateLimitError{
		APIError:   &APIError{StatusCode: http.StatusTooManyRequests},
		RetryAfter: time.Hour,
	}
	assert.Equal(t, maxRetryDelay, cli.retryDelay(1, hostileHeader), "a hostile Retry-After must not stall the provider")

	assert.Equal(t, retryBaseDelay, cli.retryDelay(1, &APIError{StatusCode: 500}))
	assert.Equal(t, 2*retryBaseDelay, cli.retryDelay(2, &APIError{StatusCode: 500}))
	assert.LessOrEqual(t, cli.retryDelay(20, &APIError{StatusCode: 500}), maxRetryDelay)
	assert.Greater(t, cli.retryDelay(99, &APIError{StatusCode: 500}), time.Duration(0), "an overflowing shift must not produce a zero delay")
}

func TestRetriesCanBeDisabled(t *testing.T) {
	var calls int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	noRetries := 0
	cli, err := New("key", Options{
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
		MaxRetries: &noRetries,
	})
	require.NoError(t, err)

	_, err = cli.GetInstance(context.Background(), "4f0f12e5-1f84-404f-aa84-85f431ea5ec2")
	require.Error(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls))
}

func TestAPIErrorMessage(t *testing.T) {
	assert.Equal(t, "vultr api returned status 500", (&APIError{StatusCode: 500}).Error())
	assert.Equal(t, "vultr api returned status 400: bad plan", (&APIError{StatusCode: 400, Message: "bad plan"}).Error())
}
