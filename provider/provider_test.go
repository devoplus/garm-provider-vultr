// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Devoplus

package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	garmErrors "github.com/cloudbase/garm-provider-common/errors"
	"github.com/cloudbase/garm-provider-common/execution/common"
	"github.com/cloudbase/garm-provider-common/params"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/devoplus/garm-provider-vultr/config"
	"github.com/devoplus/garm-provider-vultr/internal/client"
	"github.com/devoplus/garm-provider-vultr/internal/spec"
	"github.com/devoplus/garm-provider-vultr/internal/util"
)

const (
	thisController  = "f9286791-1589-4f39-a106-5b68c2a18af4"
	otherController = "11111111-2222-3333-4444-555555555555"
	testPoolID      = "9dcf590a-1192-4a9c-b3e4-e0902974c2c0"
	otherPoolID     = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	testInstanceID  = "4f0f12e5-1f84-404f-aa84-85f431ea5ec2"
	otherInstanceID = "99999999-1f84-404f-aa84-85f431ea5ec2"
)

// fakeVultrAPI records the calls the provider makes and serves canned
// instances, so the lifecycle can be tested without the Vultr API.
type fakeVultrAPI struct {
	instances map[string]client.Instance
	listErr   error
	getErr    error
	createErr error
	deleteErr error
	startErr  error
	haltErr   error

	createdReq *client.CreateInstanceRequest
	deleted    []string
	started    []string
	halted     []string
	listCalls  int
}

func newFakeAPI(instances ...client.Instance) *fakeVultrAPI {
	byID := make(map[string]client.Instance, len(instances))
	for _, instance := range instances {
		byID[instance.ID] = instance
	}
	return &fakeVultrAPI{instances: byID}
}

func (f *fakeVultrAPI) CreateInstance(_ context.Context, req *client.CreateInstanceRequest) (*client.Instance, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	f.createdReq = req

	return &client.Instance{
		ID:          testInstanceID,
		Label:       req.Label,
		Status:      util.VultrStatusPending,
		PowerStatus: util.VultrPowerStatusRunning,
		MainIP:      "0.0.0.0",
		Tags:        req.Tags,
	}, nil
}

func (f *fakeVultrAPI) GetInstance(_ context.Context, instanceID string) (*client.Instance, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	instance, ok := f.instances[instanceID]
	if !ok {
		return nil, fmt.Errorf("no such instance %s: %w", instanceID, garmErrors.ErrNotFound)
	}
	return &instance, nil
}

func (f *fakeVultrAPI) DeleteInstance(_ context.Context, instanceID string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deleted = append(f.deleted, instanceID)
	return nil
}

func (f *fakeVultrAPI) ListInstances(_ context.Context) ([]client.Instance, error) {
	f.listCalls++
	if f.listErr != nil {
		return nil, f.listErr
	}

	instances := make([]client.Instance, 0, len(f.instances))
	for _, instance := range f.instances {
		instances = append(instances, instance)
	}
	return instances, nil
}

func (f *fakeVultrAPI) StartInstance(_ context.Context, instanceID string) error {
	if f.startErr != nil {
		return f.startErr
	}
	f.started = append(f.started, instanceID)
	return nil
}

func (f *fakeVultrAPI) HaltInstance(_ context.Context, instanceID string) error {
	if f.haltErr != nil {
		return f.haltErr
	}
	f.halted = append(f.halted, instanceID)
	return nil
}

// ownedInstance builds an instance tagged for the given controller and pool.
func ownedInstance(id, label, controllerID, poolID string) client.Instance {
	return client.Instance{
		ID:          id,
		Label:       label,
		Status:      util.VultrStatusActive,
		PowerStatus: util.VultrPowerStatusRunning,
		MainIP:      "192.0.2.10",
		Tags: []string{
			util.ControllerIDTag(controllerID),
			util.PoolIDTag(poolID),
			util.OSTypeTag(params.Linux),
			util.OSArchTag(params.Amd64),
		},
	}
}

func newTestProvider(t *testing.T, api client.VultrAPI) *vultrProvider {
	t.Helper()

	prov, err := NewWithClient(&config.Config{
		APIKey:  "secret",
		Region:  "ewr",
		BaseURL: config.DefaultBaseURL,
	}, api, thisController)
	require.NoError(t, err)

	concrete, ok := prov.(*vultrProvider)
	require.True(t, ok)

	return concrete
}

// stubToolFetch replaces the GitHub tool lookup for the duration of a test.
func stubToolFetch(t *testing.T) {
	t.Helper()

	original := spec.DefaultToolFetch
	t.Cleanup(func() { spec.DefaultToolFetch = original })

	spec.DefaultToolFetch = func(_ params.OSType, _ params.OSArch, _ []params.RunnerApplicationDownload) (params.RunnerApplicationDownload, error) {
		name := "actions-runner-linux-x64-2.299.1.tar.gz"
		url := "https://example.com/" + name
		osName := "linux"
		arch := "x64"
		return params.RunnerApplicationDownload{
			OS:           &osName,
			Architecture: &arch,
			DownloadURL:  &url,
			Filename:     &name,
		}, nil
	}
}

func bootstrapParams() params.BootstrapInstance {
	return params.BootstrapInstance{
		Name:        "garm-ny9HeeQYw2rl",
		OSType:      params.Linux,
		OSArch:      params.Amd64,
		Flavor:      "vc2-2c-4gb",
		Image:       "1743",
		PoolID:      testPoolID,
		RepoURL:     "https://github.com/devoplus/example",
		CallbackURL: "https://garm.example.com/api/v1/callbacks",
		MetadataURL: "https://garm.example.com/api/v1/metadata",
	}
}

func TestNewWithClientValidatesArguments(t *testing.T) {
	cfg := &config.Config{APIKey: "k", Region: "ewr"}

	_, err := NewWithClient(nil, newFakeAPI(), thisController)
	assert.ErrorContains(t, err, "configuration")

	_, err = NewWithClient(cfg, nil, thisController)
	assert.ErrorContains(t, err, "api client")

	_, err = NewWithClient(cfg, newFakeAPI(), "")
	assert.ErrorContains(t, err, "controller id")
}

func TestCreateInstance(t *testing.T) {
	stubToolFetch(t)

	api := newFakeAPI()
	prov := newTestProvider(t, api)

	instance, err := prov.CreateInstance(context.Background(), bootstrapParams())
	require.NoError(t, err)

	assert.Equal(t, testInstanceID, instance.ProviderID)
	assert.Equal(t, "garm-ny9HeeQYw2rl", instance.Name)
	assert.Equal(t, params.Linux, instance.OSType)
	assert.Equal(t, params.Amd64, instance.OSArch)
	assert.Equal(t, params.InstanceRunning, instance.Status)

	require.NotNil(t, api.createdReq)
	assert.Equal(t, "ewr", api.createdReq.Region)
	assert.Equal(t, "vc2-2c-4gb", api.createdReq.Plan)
	assert.Equal(t, 1743, api.createdReq.OsID)
	assert.Contains(t, api.createdReq.Tags, util.ControllerIDTag(thisController))
	assert.Contains(t, api.createdReq.Tags, util.PoolIDTag(testPoolID))
	assert.NotEmpty(t, api.createdReq.UserData)
}

func TestCreateInstanceRejectsBadPoolDefinition(t *testing.T) {
	stubToolFetch(t)

	api := newFakeAPI()
	prov := newTestProvider(t, api)

	bad := bootstrapParams()
	bad.Image = "ubuntu-22.04"

	_, err := prov.CreateInstance(context.Background(), bad)
	require.Error(t, err)
	assert.Nil(t, api.createdReq, "nothing may be created from an invalid pool definition")
}

func TestCreateInstancePropagatesAPIErrors(t *testing.T) {
	stubToolFetch(t)

	api := newFakeAPI()
	api.createErr = errors.New("quota exceeded")
	prov := newTestProvider(t, api)

	_, err := prov.CreateInstance(context.Background(), bootstrapParams())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "quota exceeded")
}

func TestGetInstanceByProviderID(t *testing.T) {
	api := newFakeAPI(ownedInstance(testInstanceID, "garm-ny9HeeQYw2rl", thisController, testPoolID))
	prov := newTestProvider(t, api)

	instance, err := prov.GetInstance(context.Background(), testInstanceID)
	require.NoError(t, err)

	assert.Equal(t, testInstanceID, instance.ProviderID)
	assert.Equal(t, "garm-ny9HeeQYw2rl", instance.Name)
	assert.Equal(t, params.InstanceRunning, instance.Status)
	assert.Equal(t, 0, api.listCalls, "a provider id must be fetched directly, not by listing")
}

// TestGetInstanceByRunnerName covers the contract case where a create failed
// before reporting a provider id and GARM passes the runner name instead.
func TestGetInstanceByRunnerName(t *testing.T) {
	api := newFakeAPI(ownedInstance(testInstanceID, "garm-ny9HeeQYw2rl", thisController, testPoolID))
	prov := newTestProvider(t, api)

	instance, err := prov.GetInstance(context.Background(), "garm-ny9HeeQYw2rl")
	require.NoError(t, err)

	assert.Equal(t, testInstanceID, instance.ProviderID)
	assert.Equal(t, 1, api.listCalls, "a runner name has to be resolved through the listing")
}

func TestGetInstanceNotFound(t *testing.T) {
	prov := newTestProvider(t, newFakeAPI())

	_, err := prov.GetInstance(context.Background(), testInstanceID)
	require.Error(t, err)
	assert.True(t, errors.Is(err, garmErrors.ErrNotFound))
}

// TestGetInstanceRefusesForeignInstance is the tenant boundary: an instance
// created by another GARM installation must be invisible to this one.
func TestGetInstanceRefusesForeignInstance(t *testing.T) {
	api := newFakeAPI(ownedInstance(otherInstanceID, "garm-other", otherController, otherPoolID))
	prov := newTestProvider(t, api)

	_, err := prov.GetInstance(context.Background(), otherInstanceID)
	require.Error(t, err)
	assert.True(t, errors.Is(err, garmErrors.ErrNotFound), "a foreign instance must look like it does not exist")

	_, err = prov.GetInstance(context.Background(), "garm-other")
	require.Error(t, err)
	assert.True(t, errors.Is(err, garmErrors.ErrNotFound))
}

func TestDeleteInstance(t *testing.T) {
	api := newFakeAPI(ownedInstance(testInstanceID, "garm-ny9HeeQYw2rl", thisController, testPoolID))
	prov := newTestProvider(t, api)

	require.NoError(t, prov.DeleteInstance(context.Background(), testInstanceID))
	assert.Equal(t, []string{testInstanceID}, api.deleted)
}

func TestDeleteInstanceByRunnerName(t *testing.T) {
	api := newFakeAPI(ownedInstance(testInstanceID, "garm-ny9HeeQYw2rl", thisController, testPoolID))
	prov := newTestProvider(t, api)

	require.NoError(t, prov.DeleteInstance(context.Background(), "garm-ny9HeeQYw2rl"))
	assert.Equal(t, []string{testInstanceID}, api.deleted)
}

// TestDeleteInstanceIsANoOpWhenMissing is required by the provider contract:
// GARM runs a delete before retrying a failed create.
func TestDeleteInstanceIsANoOpWhenMissing(t *testing.T) {
	api := newFakeAPI()
	prov := newTestProvider(t, api)

	assert.NoError(t, prov.DeleteInstance(context.Background(), testInstanceID))
	assert.NoError(t, prov.DeleteInstance(context.Background(), "garm-never-created"))
	assert.Empty(t, api.deleted)
}

func TestDeleteInstanceRefusesForeignInstance(t *testing.T) {
	api := newFakeAPI(ownedInstance(otherInstanceID, "garm-other", otherController, otherPoolID))
	prov := newTestProvider(t, api)

	require.NoError(t, prov.DeleteInstance(context.Background(), otherInstanceID))
	assert.Empty(t, api.deleted, "another controller's instance must never be deleted")
}

// TestDeleteInstanceSurfacesLookupFailures keeps a transport failure from
// being read as a completed delete, which would strand a running instance.
func TestDeleteInstanceSurfacesLookupFailures(t *testing.T) {
	api := newFakeAPI()
	api.getErr = &client.APIError{StatusCode: http.StatusForbidden, Message: "insufficient permissions"}
	prov := newTestProvider(t, api)

	err := prov.DeleteInstance(context.Background(), testInstanceID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "insufficient permissions")
}

func TestListInstances(t *testing.T) {
	api := newFakeAPI(
		ownedInstance(testInstanceID, "garm-one", thisController, testPoolID),
		ownedInstance("22222222-1f84-404f-aa84-85f431ea5ec2", "garm-two", thisController, testPoolID),
		ownedInstance("33333333-1f84-404f-aa84-85f431ea5ec2", "garm-other-pool", thisController, otherPoolID),
		ownedInstance(otherInstanceID, "garm-other-controller", otherController, testPoolID),
	)
	prov := newTestProvider(t, api)

	instances, err := prov.ListInstances(context.Background(), testPoolID)
	require.NoError(t, err)

	require.Len(t, instances, 2, "only this controller's instances in this pool may be returned")
	names := []string{instances[0].Name, instances[1].Name}
	assert.ElementsMatch(t, []string{"garm-one", "garm-two"}, names)
}

func TestListInstancesEmptyPool(t *testing.T) {
	prov := newTestProvider(t, newFakeAPI())

	instances, err := prov.ListInstances(context.Background(), testPoolID)
	require.NoError(t, err)
	assert.NotNil(t, instances, "an empty pool must serialise as [] and not as null")
	assert.Empty(t, instances)
}

func TestListInstancesRequiresPoolID(t *testing.T) {
	prov := newTestProvider(t, newFakeAPI())

	_, err := prov.ListInstances(context.Background(), "")
	assert.ErrorContains(t, err, "pool id")
}

func TestRemoveAllInstancesOnlyTouchesOwnInstances(t *testing.T) {
	api := newFakeAPI(
		ownedInstance(testInstanceID, "garm-one", thisController, testPoolID),
		ownedInstance("33333333-1f84-404f-aa84-85f431ea5ec2", "garm-two", thisController, otherPoolID),
		ownedInstance(otherInstanceID, "garm-other-controller", otherController, testPoolID),
	)
	prov := newTestProvider(t, api)

	require.NoError(t, prov.RemoveAllInstances(context.Background()))

	assert.ElementsMatch(t, []string{testInstanceID, "33333333-1f84-404f-aa84-85f431ea5ec2"}, api.deleted)
	assert.NotContains(t, api.deleted, otherInstanceID)
}

func TestStartAndStop(t *testing.T) {
	api := newFakeAPI(ownedInstance(testInstanceID, "garm-ny9HeeQYw2rl", thisController, testPoolID))
	prov := newTestProvider(t, api)

	require.NoError(t, prov.Start(context.Background(), testInstanceID))
	require.NoError(t, prov.Stop(context.Background(), testInstanceID, true))
	require.NoError(t, prov.Stop(context.Background(), testInstanceID, false))

	assert.Equal(t, []string{testInstanceID}, api.started)
	assert.Equal(t, []string{testInstanceID, testInstanceID}, api.halted, "vultr has a single halt operation, so force makes no difference")
}

func TestStartAndStopRefuseForeignInstances(t *testing.T) {
	api := newFakeAPI(ownedInstance(otherInstanceID, "garm-other", otherController, otherPoolID))
	prov := newTestProvider(t, api)

	assert.Error(t, prov.Start(context.Background(), otherInstanceID))
	assert.Error(t, prov.Stop(context.Background(), otherInstanceID, true))
	assert.Empty(t, api.started)
	assert.Empty(t, api.halted)
}

func TestGetVersion(t *testing.T) {
	prov := newTestProvider(t, newFakeAPI())
	assert.Equal(t, Version, prov.GetVersion(context.Background()))
}

func TestGetSupportedInterfaceVersions(t *testing.T) {
	prov := newTestProvider(t, newFakeAPI())

	versions := prov.GetSupportedInterfaceVersions(context.Background())
	assert.Equal(t, []string{common.Version010, common.Version011}, versions)
}

func TestValidatePoolInfo(t *testing.T) {
	prov := newTestProvider(t, newFakeAPI())
	ctx := context.Background()

	assert.NoError(t, prov.ValidatePoolInfo(ctx, "1743", "vc2-2c-4gb", "", ""))
	assert.NoError(t, prov.ValidatePoolInfo(ctx, "snapshot_id:snap-1", "vc2-2c-4gb", "", `{"region":"fra"}`))

	assert.ErrorContains(t, prov.ValidatePoolInfo(ctx, "ubuntu", "vc2-2c-4gb", "", ""), "image must be")
	assert.ErrorContains(t, prov.ValidatePoolInfo(ctx, "1743", "vc2-2c-4gb", "", `{"regionn":"fra"}`), "schema validation")
	assert.ErrorContains(t, prov.ValidatePoolInfo(ctx, "1743", "   ", "", ""), "flavor must be")
}

// TestValidatePoolInfoToleratesEmptyFields covers how GARM actually invokes
// this today: the v0.1.1 execution environment only reads bootstrap params
// from stdin for CreateInstance, so image and flavor arrive empty. Rejecting
// them here would block pool creation.
func TestValidatePoolInfoToleratesEmptyFields(t *testing.T) {
	prov := newTestProvider(t, newFakeAPI())
	ctx := context.Background()

	assert.NoError(t, prov.ValidatePoolInfo(ctx, "", "", "", ""))
	assert.NoError(t, prov.ValidatePoolInfo(ctx, "", "", "", `{"region":"fra"}`))

	// Extra specs are still validated when they are present.
	assert.ErrorContains(t, prov.ValidatePoolInfo(ctx, "", "", "", `{"nope":1}`), "schema validation")
}

func TestJSONSchemaEndpoints(t *testing.T) {
	prov := newTestProvider(t, newFakeAPI())
	ctx := context.Background()

	configSchema, err := prov.GetConfigJSONSchema(ctx)
	require.NoError(t, err)

	extraSchema, err := prov.GetExtraSpecsJSONSchema(ctx)
	require.NoError(t, err)

	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(configSchema), &parsed))
	require.NoError(t, json.Unmarshal([]byte(extraSchema), &parsed))
}

// TestVultrIDPattern pins how a provider id is told apart from a runner name.
func TestVultrIDPattern(t *testing.T) {
	assert.True(t, vultrIDPattern.MatchString("4f0f12e5-1f84-404f-aa84-85f431ea5ec2"))
	assert.True(t, vultrIDPattern.MatchString("4F0F12E5-1F84-404F-AA84-85F431EA5EC2"))

	assert.False(t, vultrIDPattern.MatchString("garm-ny9HeeQYw2rl"))
	assert.False(t, vultrIDPattern.MatchString(""))
	assert.False(t, vultrIDPattern.MatchString("4f0f12e5-1f84-404f-aa84"))
	assert.False(t, vultrIDPattern.MatchString("prefix-4f0f12e5-1f84-404f-aa84-85f431ea5ec2"))
}

func TestResolveInstanceRequiresAnIdentifier(t *testing.T) {
	prov := newTestProvider(t, newFakeAPI())

	_, err := prov.resolveInstance(context.Background(), "")
	assert.ErrorContains(t, err, "instance id or name")
}
