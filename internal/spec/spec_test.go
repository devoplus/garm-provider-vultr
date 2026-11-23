// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Devoplus

package spec

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudbase/garm-provider-common/params"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/devoplus/garm-provider-vultr/config"
	"github.com/devoplus/garm-provider-vultr/internal/util"
)

const (
	testControllerID = "f9286791-1589-4f39-a106-5b68c2a18af4"
	testPoolID       = "9dcf590a-1192-4a9c-b3e4-e0902974c2c0"
)

// stubToolFetch replaces the GitHub tool lookup for the duration of a test.
func stubToolFetch(t *testing.T) {
	t.Helper()

	original := DefaultToolFetch
	t.Cleanup(func() { DefaultToolFetch = original })

	DefaultToolFetch = func(_ params.OSType, _ params.OSArch, _ []params.RunnerApplicationDownload) (params.RunnerApplicationDownload, error) {
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

func testConfig() *config.Config {
	return &config.Config{
		APIKey:  "secret",
		Region:  "ewr",
		BaseURL: config.DefaultBaseURL,
	}
}

func testBootstrapParams() params.BootstrapInstance {
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

func TestGetRunnerSpecFromBootstrapParams(t *testing.T) {
	stubToolFetch(t)

	spec, err := GetRunnerSpecFromBootstrapParams(testConfig(), testBootstrapParams(), testControllerID)
	require.NoError(t, err)

	assert.Equal(t, "ewr", spec.Region)
	assert.Equal(t, "vc2-2c-4gb", spec.Plan)
	assert.Equal(t, 1743, spec.BootImage.OsID)
	assert.Equal(t, "garm-ny9HeeQYw2rl", spec.Label)
	assert.Equal(t, "garm-ny9HeeQYw2rl", spec.Hostname)
	assert.Equal(t, testControllerID, spec.ControllerID)

	// The identifying tags are what keeps one GARM installation from touching
	// another one's instances, and what ListInstances selects on.
	assert.Equal(t, []string{
		util.ControllerIDTag(testControllerID),
		util.PoolIDTag(testPoolID),
		util.OSTypeTag(params.Linux),
		util.OSArchTag(params.Amd64),
	}, spec.Tags)
}

func TestGetRunnerSpecAppliesConfigDefaults(t *testing.T) {
	stubToolFetch(t)

	cfg := testConfig()
	cfg.SSHKeyIDs = []string{"key-1"}
	cfg.FirewallGroupID = "fw-1"
	cfg.EnableIPv6 = true
	cfg.Tags = []string{"team-platform"}

	spec, err := GetRunnerSpecFromBootstrapParams(cfg, testBootstrapParams(), testControllerID)
	require.NoError(t, err)

	assert.Equal(t, []string{"key-1"}, spec.SSHKeyIDs)
	assert.Equal(t, "fw-1", spec.FirewallGroupID)
	assert.True(t, spec.EnableIPv6)
	assert.Contains(t, spec.Tags, "team-platform")
}

func TestExtraSpecsOverrideConfig(t *testing.T) {
	stubToolFetch(t)

	bootstrapParams := testBootstrapParams()
	bootstrapParams.ExtraSpecs = json.RawMessage(`{
		"region": "fra",
		"hostname": "runner-01",
		"sshkey_ids": ["key-from-pool"],
		"firewall_group_id": "fw-from-pool",
		"enable_ipv6": true,
		"vpc_ids": ["vpc-from-pool"],
		"tags": ["pool-tag"],
		"disable_updates": true,
		"extra_packages": ["jq"]
	}`)

	cfg := testConfig()
	cfg.SSHKeyIDs = []string{"key-from-config"}
	cfg.FirewallGroupID = "fw-from-config"

	spec, err := GetRunnerSpecFromBootstrapParams(cfg, bootstrapParams, testControllerID)
	require.NoError(t, err)

	assert.Equal(t, "fra", spec.Region)
	assert.Equal(t, "runner-01", spec.Hostname)
	assert.Equal(t, []string{"key-from-pool"}, spec.SSHKeyIDs)
	assert.Equal(t, "fw-from-pool", spec.FirewallGroupID)
	assert.True(t, spec.EnableIPv6)
	assert.Equal(t, []string{"vpc-from-pool"}, spec.VPCIDs)
	assert.True(t, spec.DisableUpdates)
	assert.Equal(t, []string{"jq"}, spec.ExtraPackages)

	// Pool tags are added to the identifying tags, never replace them.
	assert.Contains(t, spec.Tags, util.ControllerIDTag(testControllerID))
	assert.Contains(t, spec.Tags, "pool-tag")
}

// TestUnknownExtraSpecIsRejected catches a typo in a pool definition at pool
// creation time instead of letting every runner fail later.
func TestUnknownExtraSpecIsRejected(t *testing.T) {
	stubToolFetch(t)

	bootstrapParams := testBootstrapParams()
	bootstrapParams.ExtraSpecs = json.RawMessage(`{"regionn": "fra"}`)

	_, err := GetRunnerSpecFromBootstrapParams(testConfig(), bootstrapParams, testControllerID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "schema validation")
}

func TestInvalidExtraSpecTypeIsRejected(t *testing.T) {
	stubToolFetch(t)

	bootstrapParams := testBootstrapParams()
	bootstrapParams.ExtraSpecs = json.RawMessage(`{"enable_ipv6": "yes"}`)

	_, err := GetRunnerSpecFromBootstrapParams(testConfig(), bootstrapParams, testControllerID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "schema validation")
}

func TestWindowsIsRejectedUntilVerified(t *testing.T) {
	stubToolFetch(t)

	bootstrapParams := testBootstrapParams()
	bootstrapParams.OSType = params.Windows

	_, err := GetRunnerSpecFromBootstrapParams(testConfig(), bootstrapParams, testControllerID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "linux")
}

func TestMissingControllerIDIsRejected(t *testing.T) {
	stubToolFetch(t)

	_, err := GetRunnerSpecFromBootstrapParams(testConfig(), testBootstrapParams(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "controller id")
}

func TestMissingFlavorIsRejected(t *testing.T) {
	stubToolFetch(t)

	bootstrapParams := testBootstrapParams()
	bootstrapParams.Flavor = ""

	_, err := GetRunnerSpecFromBootstrapParams(testConfig(), bootstrapParams, testControllerID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "plan is required")
}

func TestCreateRequest(t *testing.T) {
	stubToolFetch(t)

	cfg := testConfig()
	cfg.EnableIPv6 = true
	cfg.FirewallGroupID = "fw-1"
	cfg.SSHKeyIDs = []string{"key-1"}

	spec, err := GetRunnerSpecFromBootstrapParams(cfg, testBootstrapParams(), testControllerID)
	require.NoError(t, err)

	req, err := spec.CreateRequest()
	require.NoError(t, err)

	assert.Equal(t, "ewr", req.Region)
	assert.Equal(t, "vc2-2c-4gb", req.Plan)
	assert.Equal(t, "garm-ny9HeeQYw2rl", req.Label)
	assert.Equal(t, "garm-ny9HeeQYw2rl", req.Hostname)
	assert.Equal(t, 1743, req.OsID)
	assert.Empty(t, req.SnapshotID)
	assert.Equal(t, "fw-1", req.FirewallGroupID)
	assert.Equal(t, []string{"key-1"}, req.SSHKeyIDs)

	require.NotNil(t, req.EnableIPv6)
	assert.True(t, *req.EnableIPv6)
	require.NotNil(t, req.ActivationEmail)
	assert.False(t, *req.ActivationEmail, "a deployment email per runner would be noise")

	// Options that were not asked for stay unset so that the Vultr account
	// default applies.
	assert.Nil(t, req.DDoSProtection)
	assert.Nil(t, req.VPCOnly)
	assert.Nil(t, req.DisablePublicIPv4)

	// The user data has to be base64 cloud-init carrying the GARM bootstrap.
	require.NotEmpty(t, req.UserData)
	cloudConfig, err := base64.StdEncoding.DecodeString(req.UserData)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(string(cloudConfig), "#cloud-config"), "vultr expects cloud-init user data")
	assert.Contains(t, string(cloudConfig), "/install_runner.sh")

	// cloud-init writes the bootstrap script as a base64 file. Decoding it
	// proves the runner will actually reach back to this GARM installation.
	installScript := decodeEmbeddedFile(t, string(cloudConfig))
	assert.Contains(t, installScript, "https://garm.example.com/api/v1/metadata")
	assert.Contains(t, installScript, "https://garm.example.com/api/v1/callbacks")
	assert.Contains(t, installScript, "garm-ny9HeeQYw2rl")
}

// decodeEmbeddedFile returns the contents of the base64 file cloud-init writes
// out of the given cloud-config.
func decodeEmbeddedFile(t *testing.T, cloudConfig string) string {
	t.Helper()

	const marker = "content: "
	start := strings.Index(cloudConfig, marker)
	require.Greater(t, start, -1, "the cloud config carries no embedded file")

	rest := cloudConfig[start+len(marker):]
	if end := strings.IndexAny(rest, "\r\n"); end > -1 {
		rest = rest[:end]
	}

	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(rest))
	require.NoError(t, err)

	return string(decoded)
}

// TestCreateRequestVPCPrecedence covers the Vultr rule that attach_vpc takes
// precedence over enable_vpc: sending both would be ambiguous.
func TestCreateRequestVPCPrecedence(t *testing.T) {
	stubToolFetch(t)

	cfg := testConfig()
	cfg.EnableVPC = true

	spec, err := GetRunnerSpecFromBootstrapParams(cfg, testBootstrapParams(), testControllerID)
	require.NoError(t, err)

	req, err := spec.CreateRequest()
	require.NoError(t, err)
	require.NotNil(t, req.EnableVPC)
	assert.True(t, *req.EnableVPC)
	assert.Empty(t, req.AttachVPC)

	cfg.VPCIDs = []string{"vpc-1"}
	spec, err = GetRunnerSpecFromBootstrapParams(cfg, testBootstrapParams(), testControllerID)
	require.NoError(t, err)

	req, err = spec.CreateRequest()
	require.NoError(t, err)
	assert.Nil(t, req.EnableVPC, "enable_vpc must not be sent together with attach_vpc")
	assert.Equal(t, []string{"vpc-1"}, req.AttachVPC)
}

func TestCreateRequestBootSources(t *testing.T) {
	stubToolFetch(t)

	for _, tt := range []struct {
		image          string
		wantOsID       int
		wantSnapshotID string
		wantAppID      int
		wantImageID    string
	}{
		{image: "1743", wantOsID: 1743},
		{image: "os_id:2284", wantOsID: 2284},
		{image: "snapshot_id:cb676a46-66fd-4dfb-b839-443f2e6c0b60", wantSnapshotID: "cb676a46-66fd-4dfb-b839-443f2e6c0b60"},
		{image: "app_id:1234", wantAppID: 1234},
		{image: "image_id:docker", wantImageID: "docker"},
	} {
		t.Run(tt.image, func(t *testing.T) {
			bootstrapParams := testBootstrapParams()
			bootstrapParams.Image = tt.image

			spec, err := GetRunnerSpecFromBootstrapParams(testConfig(), bootstrapParams, testControllerID)
			require.NoError(t, err)

			req, err := spec.CreateRequest()
			require.NoError(t, err)

			assert.Equal(t, tt.wantOsID, req.OsID)
			assert.Equal(t, tt.wantSnapshotID, req.SnapshotID)
			assert.Equal(t, tt.wantAppID, req.AppID)
			assert.Equal(t, tt.wantImageID, req.ImageID)
		})
	}
}

func TestRunnerSpecValidate(t *testing.T) {
	base := func() *RunnerSpec {
		return &RunnerSpec{
			Region:       "ewr",
			Plan:         "vc2-1c-1gb",
			Label:        "garm-test",
			ControllerID: testControllerID,
			BootImage:    BootImage{OsID: 1743},
		}
	}

	assert.NoError(t, base().Validate())

	noRegion := base()
	noRegion.Region = ""
	assert.ErrorContains(t, noRegion.Validate(), "region is required")

	noPlan := base()
	noPlan.Plan = ""
	assert.ErrorContains(t, noPlan.Validate(), "plan is required")

	noLabel := base()
	noLabel.Label = ""
	assert.ErrorContains(t, noLabel.Validate(), "runner name")

	badBackups := base()
	badBackups.Backups = "sometimes"
	assert.ErrorContains(t, badBackups.Validate(), "backups must be")

	badScheme := base()
	badScheme.UserScheme = "admin"
	assert.ErrorContains(t, badScheme.Validate(), "user_scheme must be")

	noAddress := base()
	noAddress.DisablePublicIPv4 = true
	assert.ErrorContains(t, noAddress.Validate(), "disable_public_ipv4 requires enable_ipv6")

	noRoute := base()
	noRoute.VPCOnly = true
	assert.ErrorContains(t, noRoute.Validate(), "vpc_only requires vpc_ids")

	twoImages := base()
	twoImages.BootImage = BootImage{OsID: 1743, SnapshotID: "snap-1"}
	assert.ErrorContains(t, twoImages.Validate(), "only one boot source")
}

func TestValidateExtraSpecs(t *testing.T) {
	assert.NoError(t, ValidateExtraSpecs(nil))
	assert.NoError(t, ValidateExtraSpecs(json.RawMessage(`{}`)))
	assert.NoError(t, ValidateExtraSpecs(json.RawMessage(`{"region":"fra","enable_ipv6":true}`)))

	assert.Error(t, ValidateExtraSpecs(json.RawMessage(`{"unknown":"x"}`)))
	assert.Error(t, ValidateExtraSpecs(json.RawMessage(`{"backups":"maybe"}`)))
	assert.Error(t, ValidateExtraSpecs(json.RawMessage(`not json`)))
}

func TestJSONSchemasAreGenerated(t *testing.T) {
	extraSchema, err := ExtraSpecsJSONSchema()
	require.NoError(t, err)
	assert.Contains(t, extraSchema, "enable_ipv6")
	assert.Contains(t, extraSchema, "vpc_only")

	configSchema, err := ConfigJSONSchema()
	require.NoError(t, err)
	assert.Contains(t, configSchema, "region")
	assert.Contains(t, configSchema, "api_key")

	// Both have to be valid JSON, since GARM hands them to clients as-is.
	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(extraSchema), &parsed))
	require.NoError(t, json.Unmarshal([]byte(configSchema), &parsed))
}

func TestValidateOSType(t *testing.T) {
	assert.NoError(t, ValidateOSType(params.Linux))
	assert.ErrorContains(t, ValidateOSType(""), "os_type is required")
	assert.ErrorContains(t, ValidateOSType(params.Windows), "linux")
	assert.ErrorContains(t, ValidateOSType(params.Darwin), "linux")
}
