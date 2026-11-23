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

package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
)

const (
	// DefaultBaseURL is the Vultr API v2 endpoint.
	DefaultBaseURL = "https://api.vultr.com"

	// APIKeyEnvVar is consulted when api_key is not set in the config file.
	// GARM forwards it to the provider when the provider section lists it in
	// environment_variables.
	APIKeyEnvVar = "VULTR_API_KEY"

	// BackupsEnabled and BackupsDisabled are the only values the Vultr API
	// accepts for the backups attribute.
	BackupsEnabled  = "enabled"
	BackupsDisabled = "disabled"

	// UserSchemeRoot and UserSchemeLimited are the only values the Vultr API
	// accepts for the user_scheme attribute.
	UserSchemeRoot    = "root"
	UserSchemeLimited = "limited"
)

// Config holds the provider configuration. GARM passes the path of this file
// to the provider in the GARM_PROVIDER_CONFIG_FILE environment variable.
//
// Every field other than api_key and region is a default that a pool can
// override through extra_specs.
type Config struct {
	// APIKey is a Vultr API key with permission to manage instances. When it
	// is empty, the VULTR_API_KEY environment variable is used instead, which
	// keeps the key out of the config file.
	APIKey string `toml:"api_key" json:"api_key,omitempty" jsonschema:"description=Vultr API key. Leave empty to read it from the VULTR_API_KEY environment variable."`

	// Region is the Vultr region id (for example ewr or fra) runners are
	// created in. See GET /v2/regions.
	Region string `toml:"region" json:"region" jsonschema:"description=Vultr region id the runners are created in. See GET /v2/regions."`

	// BaseURL overrides the Vultr API endpoint. It exists for testing and for
	// deployments that proxy the API; leave it empty in normal use.
	BaseURL string `toml:"base_url" json:"base_url,omitempty" jsonschema:"description=Override for the Vultr API endpoint. Leave empty to use the public Vultr API."`

	// SSHKeyIDs are Vultr SSH key ids injected into every runner.
	SSHKeyIDs []string `toml:"sshkey_ids" json:"sshkey_ids,omitempty" jsonschema:"description=Vultr SSH key ids to install on every runner. See GET /v2/ssh-keys."`

	// FirewallGroupID is the Vultr firewall group attached to every runner.
	FirewallGroupID string `toml:"firewall_group_id" json:"firewall_group_id,omitempty" jsonschema:"description=Vultr firewall group id attached to every runner. See GET /v2/firewalls."`

	// ScriptID is a Vultr startup script run on every runner. The GARM
	// bootstrap itself is delivered through user data, not through this.
	ScriptID string `toml:"script_id" json:"script_id,omitempty" jsonschema:"description=Vultr startup script id to run on every runner. See GET /v2/startup-scripts."`

	// EnableIPv6 assigns an IPv6 address to every runner.
	EnableIPv6 bool `toml:"enable_ipv6" json:"enable_ipv6,omitempty" jsonschema:"description=Assign an IPv6 address to every runner."`

	// DisablePublicIPv4 skips the public IPv4 address. The Vultr API ignores
	// it unless enable_ipv6 is also true.
	DisablePublicIPv4 bool `toml:"disable_public_ipv4" json:"disable_public_ipv4,omitempty" jsonschema:"description=Do not assign a public IPv4 address. Requires enable_ipv6."`

	// EnableVPC attaches a single VPC, creating one in the region if none
	// exists. Use VPCIDs when the region has more than one VPC.
	EnableVPC bool `toml:"enable_vpc" json:"enable_vpc,omitempty" jsonschema:"description=Attach a single VPC, creating one in the region if none exists."`

	// VPCIDs are the VPC ids attached to every runner. It takes precedence
	// over EnableVPC in the Vultr API.
	VPCIDs []string `toml:"vpc_ids" json:"vpc_ids,omitempty" jsonschema:"description=Vultr VPC ids to attach to every runner. Takes precedence over enable_vpc."`

	// VPCOnly creates runners without a public interface. The instance then
	// reaches the internet through the NAT gateway of the first VPC, so
	// VPCIDs must be set and its first entry must have a NAT gateway.
	VPCOnly bool `toml:"vpc_only" json:"vpc_only,omitempty" jsonschema:"description=Create runners without a public interface. Requires vpc_ids whose first VPC has a NAT gateway."`

	// DDoSProtection enables Vultr DDoS protection, which carries an extra
	// charge.
	DDoSProtection bool `toml:"ddos_protection" json:"ddos_protection,omitempty" jsonschema:"description=Enable Vultr DDoS protection. This carries an additional charge."`

	// Backups enables Vultr automatic backups. Runners are disposable, so the
	// default is to leave this unset.
	Backups string `toml:"backups" json:"backups,omitempty" jsonschema:"enum=enabled,enum=disabled,description=Enable Vultr automatic backups. Runners are disposable so this is normally left unset."`

	// UserScheme selects the login user Vultr configures on Linux instances.
	UserScheme string `toml:"user_scheme" json:"user_scheme,omitempty" jsonschema:"enum=root,enum=limited,description=Login user Vultr configures on Linux instances."`

	// Tags are extra Vultr tags added to every runner, next to the tags the
	// provider uses to identify its own resources.
	Tags []string `toml:"tags" json:"tags,omitempty" jsonschema:"description=Extra Vultr tags added to every runner."`
}

// New reads, defaults and validates the provider config file.
func New(cfgFile string) (*Config, error) {
	var config Config
	if _, err := toml.DecodeFile(cfgFile, &config); err != nil {
		return nil, fmt.Errorf("decoding config: %w", err)
	}

	config.applyDefaults()

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}

	return &config, nil
}

// applyDefaults fills in values that can be derived from the environment or
// that have a single sensible default.
func (c *Config) applyDefaults() {
	if c.APIKey == "" {
		c.APIKey = os.Getenv(APIKeyEnvVar)
	}
	c.APIKey = strings.TrimSpace(c.APIKey)

	if c.BaseURL == "" {
		c.BaseURL = DefaultBaseURL
	}
	c.BaseURL = strings.TrimRight(c.BaseURL, "/")

	c.Region = strings.TrimSpace(c.Region)
}

// Validate checks the config against the constraints the Vultr API documents
// for POST /v2/instances.
func (c *Config) Validate() error {
	if c.APIKey == "" {
		return fmt.Errorf("api_key is required; set it in the config file or in the %s environment variable", APIKeyEnvVar)
	}

	if c.Region == "" {
		return fmt.Errorf("region is required")
	}

	switch c.Backups {
	case "", BackupsEnabled, BackupsDisabled:
	default:
		return fmt.Errorf("backups must be %q or %q, got %q", BackupsEnabled, BackupsDisabled, c.Backups)
	}

	switch c.UserScheme {
	case "", UserSchemeRoot, UserSchemeLimited:
	default:
		return fmt.Errorf("user_scheme must be %q or %q, got %q", UserSchemeRoot, UserSchemeLimited, c.UserScheme)
	}

	// The Vultr API ignores disable_public_ipv4 unless IPv6 is enabled. A
	// runner with neither address can never reach the callback URL, so this
	// combination is rejected instead of being silently ignored.
	if c.DisablePublicIPv4 && !c.EnableIPv6 {
		return fmt.Errorf("disable_public_ipv4 requires enable_ipv6")
	}

	// vpc_only instances have no public interface and reach the internet
	// through the NAT gateway attached to the first VPC in the list.
	if c.VPCOnly && len(c.VPCIDs) == 0 {
		return fmt.Errorf("vpc_only requires vpc_ids; the first VPC must have a NAT gateway attached")
	}

	return nil
}
