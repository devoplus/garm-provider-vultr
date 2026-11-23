// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Devoplus

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeConfig writes a config file into a temporary directory and returns its
// path.
func writeConfig(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "garm-provider-vultr.toml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	return path
}

func TestNewReadsAFullConfig(t *testing.T) {
	path := writeConfig(t, `
api_key = "secret-key"
region = "ewr"
sshkey_ids = ["key-1", "key-2"]
firewall_group_id = "fw-1"
script_id = "script-1"
enable_ipv6 = true
enable_vpc = true
vpc_ids = ["vpc-1"]
ddos_protection = true
backups = "disabled"
user_scheme = "limited"
tags = ["team-platform"]
`)

	cfg, err := New(path)
	require.NoError(t, err)

	assert.Equal(t, "secret-key", cfg.APIKey)
	assert.Equal(t, "ewr", cfg.Region)
	assert.Equal(t, DefaultBaseURL, cfg.BaseURL)
	assert.Equal(t, []string{"key-1", "key-2"}, cfg.SSHKeyIDs)
	assert.Equal(t, "fw-1", cfg.FirewallGroupID)
	assert.Equal(t, "script-1", cfg.ScriptID)
	assert.True(t, cfg.EnableIPv6)
	assert.True(t, cfg.EnableVPC)
	assert.Equal(t, []string{"vpc-1"}, cfg.VPCIDs)
	assert.True(t, cfg.DDoSProtection)
	assert.Equal(t, BackupsDisabled, cfg.Backups)
	assert.Equal(t, UserSchemeLimited, cfg.UserScheme)
	assert.Equal(t, []string{"team-platform"}, cfg.Tags)
}

func TestNewFailsOnMissingFile(t *testing.T) {
	_, err := New(filepath.Join(t.TempDir(), "does-not-exist.toml"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decoding config")
}

func TestNewFailsOnInvalidToml(t *testing.T) {
	_, err := New(writeConfig(t, "this is not toml"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decoding config")
}

// TestAPIKeyFallsBackToEnvironment covers the recommended setup, where the key
// is handed to the provider through the environment instead of being written
// into the config file.
func TestAPIKeyFallsBackToEnvironment(t *testing.T) {
	t.Setenv(APIKeyEnvVar, "key-from-env")

	cfg, err := New(writeConfig(t, `region = "fra"`))
	require.NoError(t, err)
	assert.Equal(t, "key-from-env", cfg.APIKey)
}

// TestConfigFileAPIKeyWinsOverEnvironment pins the precedence: an explicit key
// in the config file is a deliberate choice and is not overridden.
func TestConfigFileAPIKeyWinsOverEnvironment(t *testing.T) {
	t.Setenv(APIKeyEnvVar, "key-from-env")

	cfg, err := New(writeConfig(t, `
api_key = "key-from-file"
region = "fra"
`))
	require.NoError(t, err)
	assert.Equal(t, "key-from-file", cfg.APIKey)
}

func TestMissingAPIKeyIsRejected(t *testing.T) {
	t.Setenv(APIKeyEnvVar, "")

	_, err := New(writeConfig(t, `region = "fra"`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "api_key is required")
	assert.Contains(t, err.Error(), APIKeyEnvVar)
}

func TestMissingRegionIsRejected(t *testing.T) {
	_, err := New(writeConfig(t, `api_key = "secret"`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "region is required")
}

func TestBaseURLIsNormalised(t *testing.T) {
	cfg, err := New(writeConfig(t, `
api_key = "secret"
region = "ewr"
base_url = "https://proxy.example.com/"
`))
	require.NoError(t, err)
	assert.Equal(t, "https://proxy.example.com", cfg.BaseURL)
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{
			name: "a minimal config is valid",
			cfg:  Config{APIKey: "k", Region: "ewr"},
		},
		{
			name:    "an unknown backups value is rejected",
			cfg:     Config{APIKey: "k", Region: "ewr", Backups: "yes-please"},
			wantErr: "backups must be",
		},
		{
			name:    "an unknown user scheme is rejected",
			cfg:     Config{APIKey: "k", Region: "ewr", UserScheme: "admin"},
			wantErr: "user_scheme must be",
		},
		{
			name:    "dropping public ipv4 without ipv6 would leave the runner unreachable",
			cfg:     Config{APIKey: "k", Region: "ewr", DisablePublicIPv4: true},
			wantErr: "disable_public_ipv4 requires enable_ipv6",
		},
		{
			name: "dropping public ipv4 with ipv6 is allowed",
			cfg:  Config{APIKey: "k", Region: "ewr", DisablePublicIPv4: true, EnableIPv6: true},
		},
		{
			name:    "vpc_only without a vpc has no route to the internet",
			cfg:     Config{APIKey: "k", Region: "ewr", VPCOnly: true},
			wantErr: "vpc_only requires vpc_ids",
		},
		{
			name: "vpc_only with a vpc is allowed",
			cfg:  Config{APIKey: "k", Region: "ewr", VPCOnly: true, VPCIDs: []string{"vpc-1"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
