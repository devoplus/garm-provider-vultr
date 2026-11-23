// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Devoplus
//
//    Licensed under the Apache License, Version 2.0 (the "License"); you may
//    not use this file except in compliance with the License. You may obtain
//    a copy of the License at
//
//         http://www.apache.org/licenses/LICENSE-2.0
//
//    Unless required by applicable law or agreed to in writing, software
//    distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
//    WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the
//    License for the specific language governing permissions and limitations
//    under the License.

// Package client talks to the Vultr API v2.
//
// It is a small hand written client rather than a wrapper around the official
// govultr SDK. The provider contract requires a Not Found to be reported as
// such: GARM maps it to a dedicated exit code and DeleteInstance must be a
// no-op for an instance that is already gone. The govultr SDK reduces every
// non-2xx response to an error string and drops the HTTP status, which is why
// even Vultr's own Terraform provider has to match on error text. Reading the
// status code directly keeps that semantics exact, and the provider only needs
// six endpoints.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	garmErrors "github.com/cloudbase/garm-provider-common/errors"
)

const (
	instancesPath = "/v2/instances"

	// maxPerPage is the largest page the Vultr API serves.
	maxPerPage = 500

	// maxListPages bounds the pagination loop. At 500 instances per page this
	// covers accounts far larger than any runner fleet, and it guarantees the
	// loop ends even if the API ever returns a repeating cursor.
	maxListPages = 200

	// defaultTimeout bounds a single HTTP request. GARM applies its own
	// deadline to the whole provider invocation on top of this.
	defaultTimeout = 60 * time.Second

	// maxRetries is the number of additional attempts made for a rate limited
	// or server side failure. Vultr rate limits at 30 requests per second.
	maxRetries = 3

	// retryBaseDelay is the first backoff step; it doubles per attempt.
	retryBaseDelay = 1 * time.Second

	// maxRetryDelay caps both the backoff and any Retry-After the API sends,
	// so that a hostile or mistaken header cannot stall the provider.
	maxRetryDelay = 30 * time.Second
)

// APIError is a non-2xx answer from the Vultr API.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("vultr api returned status %d", e.StatusCode)
	}
	return fmt.Sprintf("vultr api returned status %d: %s", e.StatusCode, e.Message)
}

// Is lets callers test a 404 with errors.Is(err, garmErrors.ErrNotFound),
// which is what the provider and GARM use to tell a missing instance from a
// real failure.
func (e *APIError) Is(target error) bool {
	if target == garmErrors.ErrNotFound {
		return e.StatusCode == http.StatusNotFound
	}
	return false
}

// VultrClient is the concrete VultrAPI implementation.
type VultrClient struct {
	apiKey         string
	baseURL        string
	userAgent      string
	httpClient     *http.Client
	maxRetries     int
	retryBaseDelay time.Duration
}

// Options tune a client. The zero value is usable: it talks to the public
// Vultr API with the default timeout and retry behaviour.
type Options struct {
	// BaseURL overrides the API endpoint.
	BaseURL string
	// UserAgent identifies the provider to the API.
	UserAgent string
	// HTTPClient replaces the default client. Tests use it to point at an
	// httptest server.
	HTTPClient *http.Client
	// MaxRetries overrides how many extra attempts a rate limited or server
	// side failure gets. A negative value disables retries.
	MaxRetries *int
	// RetryBaseDelay overrides the first backoff step, which doubles per
	// attempt. Tests use it to keep the suite fast.
	RetryBaseDelay time.Duration
}

// New returns a client authenticated with the given API key.
func New(apiKey string, opts Options) (*VultrClient, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("an api key is required")
	}

	baseURL := strings.TrimRight(opts.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.vultr.com"
	}
	if _, err := url.Parse(baseURL); err != nil {
		return nil, fmt.Errorf("parsing base url: %w", err)
	}

	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}

	userAgent := opts.UserAgent
	if userAgent == "" {
		userAgent = "garm-provider-vultr"
	}

	retries := maxRetries
	if opts.MaxRetries != nil {
		retries = *opts.MaxRetries
		if retries < 0 {
			retries = 0
		}
	}

	baseDelay := opts.RetryBaseDelay
	if baseDelay <= 0 {
		baseDelay = retryBaseDelay
	}

	return &VultrClient{
		apiKey:         apiKey,
		baseURL:        baseURL,
		userAgent:      userAgent,
		httpClient:     httpClient,
		maxRetries:     retries,
		retryBaseDelay: baseDelay,
	}, nil
}

// CreateInstance deploys a new instance.
func (c *VultrClient) CreateInstance(ctx context.Context, req *CreateInstanceRequest) (*Instance, error) {
	if req == nil {
		return nil, fmt.Errorf("a create request is required")
	}

	var resp instanceResponse
	if err := c.do(ctx, http.MethodPost, instancesPath, req, &resp); err != nil {
		return nil, err
	}

	if resp.Instance == nil {
		return nil, fmt.Errorf("vultr api returned no instance")
	}

	return resp.Instance, nil
}

// GetInstance returns one instance by its Vultr id.
func (c *VultrClient) GetInstance(ctx context.Context, instanceID string) (*Instance, error) {
	if instanceID == "" {
		return nil, fmt.Errorf("an instance id is required")
	}

	var resp instanceResponse
	path := instancesPath + "/" + url.PathEscape(instanceID)
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}

	if resp.Instance == nil {
		return nil, fmt.Errorf("vultr api returned no instance for %s: %w", instanceID, garmErrors.ErrNotFound)
	}

	return resp.Instance, nil
}

// DeleteInstance destroys an instance. An instance that is already gone is
// treated as deleted, as the provider contract requires.
func (c *VultrClient) DeleteInstance(ctx context.Context, instanceID string) error {
	if instanceID == "" {
		return fmt.Errorf("an instance id is required")
	}

	path := instancesPath + "/" + url.PathEscape(instanceID)
	if err := c.do(ctx, http.MethodDelete, path, nil, nil); err != nil {
		if errors.Is(err, garmErrors.ErrNotFound) {
			return nil
		}
		return err
	}

	return nil
}

// ListInstances returns every instance in the account.
//
// The Vultr API can only filter a listing on the deprecated singular "tag"
// query parameter, so the provider reads the whole list and selects on the
// tags it set itself. Pages are requested at the maximum size to keep the
// number of API calls down.
func (c *VultrClient) ListInstances(ctx context.Context) ([]Instance, error) {
	var (
		instances []Instance
		cursor    string
	)

	for page := 0; page < maxListPages; page++ {
		query := url.Values{}
		query.Set("per_page", strconv.Itoa(maxPerPage))
		if cursor != "" {
			query.Set("cursor", cursor)
		}

		var resp instanceListResponse
		path := instancesPath + "?" + query.Encode()
		if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
			return nil, err
		}

		instances = append(instances, resp.Instances...)

		if resp.Meta == nil || resp.Meta.Links == nil || resp.Meta.Links.Next == "" {
			return instances, nil
		}

		if resp.Meta.Links.Next == cursor {
			// The API repeated the cursor it was given. Stopping here returns
			// what was collected instead of looping forever.
			return instances, nil
		}
		cursor = resp.Meta.Links.Next
	}

	return nil, fmt.Errorf("listing instances did not finish within %d pages", maxListPages)
}

// StartInstance boots a halted instance.
func (c *VultrClient) StartInstance(ctx context.Context, instanceID string) error {
	if instanceID == "" {
		return fmt.Errorf("an instance id is required")
	}

	path := instancesPath + "/" + url.PathEscape(instanceID) + "/start"
	return c.do(ctx, http.MethodPost, path, nil, nil)
}

// HaltInstance shuts an instance down without destroying it.
func (c *VultrClient) HaltInstance(ctx context.Context, instanceID string) error {
	if instanceID == "" {
		return fmt.Errorf("an instance id is required")
	}

	path := instancesPath + "/" + url.PathEscape(instanceID) + "/halt"
	return c.do(ctx, http.MethodPost, path, nil, nil)
}

// do performs one API call, retrying rate limited and server side failures,
// and decodes the response body into out when out is not nil.
func (c *VultrClient) do(ctx context.Context, method, path string, body, out interface{}) error {
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encoding request body: %w", err)
		}
	}

	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			delay := c.retryDelay(attempt, lastErr)
			select {
			case <-ctx.Done():
				return fmt.Errorf("%w (last error: %v)", ctx.Err(), lastErr)
			case <-time.After(delay):
			}
		}

		respBody, err := c.attempt(ctx, method, path, payload)
		if err != nil {
			lastErr = err
			if !isRetryable(err) || ctx.Err() != nil {
				return err
			}
			continue
		}

		if out == nil || len(respBody) == 0 {
			return nil
		}

		if err := json.Unmarshal(respBody, out); err != nil {
			// A malformed body is not worth retrying; the request itself
			// succeeded.
			return fmt.Errorf("decoding response body: %w", err)
		}

		return nil
	}

	return fmt.Errorf("gave up after %d attempts: %w", c.maxRetries+1, lastErr)
}

// attempt performs a single HTTP request and returns the response body for a
// successful call, or an error describing the failure.
func (c *VultrClient) attempt(ctx context.Context, method, path string, payload []byte) ([]byte, error) {
	var reader io.Reader
	if payload != nil {
		reader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Transport failures carry no status code. They are retried unless
		// the context is done, which do() checks.
		return nil, fmt.Errorf("calling vultr api: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return respBody, nil
	}

	apiErr := &APIError{
		StatusCode: resp.StatusCode,
		Message:    errorMessage(respBody),
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, &rateLimitError{
			APIError:   apiErr,
			RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
		}
	}

	return nil, apiErr
}

// rateLimitError is a 429 together with the wait the API asked for.
type rateLimitError struct {
	*APIError
	RetryAfter time.Duration
}

// errorMessage pulls the error text out of a Vultr error body, falling back to
// the raw body when it is not the documented shape. The body is truncated so
// that an unexpected HTML error page does not end up in GARM's logs in full.
func errorMessage(body []byte) string {
	if len(body) == 0 {
		return ""
	}

	var parsed errorResponse
	if err := json.Unmarshal(body, &parsed); err == nil && parsed.Error != "" {
		return parsed.Error
	}

	message := strings.TrimSpace(string(body))
	const maxMessage = 512
	if len(message) > maxMessage {
		return message[:maxMessage] + "..."
	}
	return message
}

// isRetryable reports whether another attempt could succeed. Rate limits and
// server side failures are retried; a rejected or missing resource is not.
func isRetryable(err error) bool {
	var rateLimited *rateLimitError
	if errors.As(err, &rateLimited) {
		return true
	}

	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode >= 500
	}

	// Transport and body read failures have no status code.
	return true
}

// retryDelay returns how long to wait before the given attempt, honouring a
// Retry-After from the API when there is one.
func (c *VultrClient) retryDelay(attempt int, err error) time.Duration {
	var rateLimited *rateLimitError
	if errors.As(err, &rateLimited) && rateLimited.RetryAfter > 0 {
		if rateLimited.RetryAfter > maxRetryDelay {
			return maxRetryDelay
		}
		return rateLimited.RetryAfter
	}

	delay := c.retryBaseDelay << (attempt - 1)
	if delay > maxRetryDelay || delay <= 0 {
		// The shift overflows for an absurd attempt count; the cap covers it.
		return maxRetryDelay
	}
	return delay
}

// parseRetryAfter reads the Retry-After header, which the Vultr API sends as a
// number of seconds.
func parseRetryAfter(value string) time.Duration {
	if value == "" {
		return 0
	}

	seconds, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || seconds <= 0 {
		return 0
	}

	return time.Duration(seconds) * time.Second
}

// Bool returns a pointer to v, for the optional booleans in
// CreateInstanceRequest.
func Bool(v bool) *bool {
	return &v
}
