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

// Package spec turns GARM bootstrap params and provider configuration into a
// Vultr instance create request.
package spec

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/cloudbase/garm-provider-common/cloudconfig"
	"github.com/cloudbase/garm-provider-common/params"
	commonUtil "github.com/cloudbase/garm-provider-common/util"
	"github.com/invopop/jsonschema"
	"github.com/xeipuuv/gojsonschema"

	"github.com/devoplus/garm-provider-vultr/config"
	"github.com/devoplus/garm-provider-vultr/internal/client"
	"github.com/devoplus/garm-provider-vultr/internal/util"
)

// ToolFetchFunc selects the runner tools for an OS and architecture. It is a
// variable so that tests can supply the tools without a GitHub lookup.
type ToolFetchFunc func(osType params.OSType, osArch params.OSArch, tools []params.RunnerApplicationDownload) (params.RunnerApplicationDownload, error)

// DefaultToolFetch is the tool selection used in production.
var DefaultToolFetch ToolFetchFunc = commonUtil.GetTools

// extraSpecs are the per-pool overrides. Every field is optional; an unset
// field leaves the provider configuration in place.
//
// The struct is also the source of the extra specs JSON schema GARM shows to
// users, so each field carries a description.
type extraSpecs struct {
	// Region overrides the region from the provider config for this pool.
	Region *string `json:"region,omitempty" jsonschema:"description=Vultr region id for this pool. Overrides the provider configuration."`

	// Hostname sets the instance hostname. It defaults to the runner name.
	Hostname *string `json:"hostname,omitempty" jsonschema:"description=Hostname for the instance. Defaults to the runner name."`

	// SSHKeyIDs replaces the SSH keys from the provider config.
	SSHKeyIDs []string `json:"sshkey_ids,omitempty" jsonschema:"description=Vultr SSH key ids to install on the runner. Replaces the provider configuration."`

	// FirewallGroupID replaces the firewall group from the provider config.
	FirewallGroupID *string `json:"firewall_group_id,omitempty" jsonschema:"description=Vultr firewall group id to attach. Overrides the provider configuration."`

	// ScriptID replaces the startup script from the provider config.
	ScriptID *string `json:"script_id,omitempty" jsonschema:"description=Vultr startup script id to run on the runner. Overrides the provider configuration."`

	// ReservedIPv4 uses an existing floating IP as the main address.
	ReservedIPv4 *string `json:"reserved_ipv4,omitempty" jsonschema:"description=Id of a reserved IPv4 address to use as the main IP of the runner."`

	// Tags are added to the tags from the provider config.
	Tags []string `json:"tags,omitempty" jsonschema:"description=Extra Vultr tags added to the runner, in addition to the ones from the provider configuration."`

	EnableIPv6        *bool    `json:"enable_ipv6,omitempty" jsonschema:"description=Assign an IPv6 address. Overrides the provider configuration."`
	DisablePublicIPv4 *bool    `json:"disable_public_ipv4,omitempty" jsonschema:"description=Do not assign a public IPv4 address. Requires enable_ipv6."`
	EnableVPC         *bool    `json:"enable_vpc,omitempty" jsonschema:"description=Attach a single VPC, creating one in the region if none exists."`
	VPCIDs            []string `json:"vpc_ids,omitempty" jsonschema:"description=Vultr VPC ids to attach. Replaces the provider configuration and takes precedence over enable_vpc."`
	VPCOnly           *bool    `json:"vpc_only,omitempty" jsonschema:"description=Create the runner without a public interface. Requires vpc_ids whose first VPC has a NAT gateway."`
	DDoSProtection    *bool    `json:"ddos_protection,omitempty" jsonschema:"description=Enable Vultr DDoS protection. This carries an additional charge."`
	Backups           *string  `json:"backups,omitempty" jsonschema:"enum=enabled,enum=disabled,description=Enable Vultr automatic backups for the runner."`
	UserScheme        *string  `json:"user_scheme,omitempty" jsonschema:"enum=root,enum=limited,description=Login user Vultr configures on the runner."`

	// The following three mirror the options every GARM provider exposes for
	// the cloud-init run on the runner.
	DisableUpdates  *bool    `json:"disable_updates,omitempty" jsonschema:"description=Do not run package updates when the runner boots."`
	EnableBootDebug *bool    `json:"enable_boot_debug,omitempty" jsonschema:"description=Enable verbose output in the runner bootstrap script."`
	ExtraPackages   []string `json:"extra_packages,omitempty" jsonschema:"description=Extra packages to install when the runner boots."`

	cloudconfig.CloudConfigSpec
}

// RunnerSpec is everything needed to create one runner instance.
type RunnerSpec struct {
	Region          string
	Plan            string
	BootImage       BootImage
	Label           string
	Hostname        string
	Tags            []string
	SSHKeyIDs       []string
	FirewallGroupID string
	ScriptID        string
	ReservedIPv4    string
	Backups         string
	UserScheme      string

	EnableIPv6        bool
	DisablePublicIPv4 bool
	EnableVPC         bool
	VPCIDs            []string
	VPCOnly           bool
	DDoSProtection    bool

	DisableUpdates  bool
	EnableBootDebug bool
	ExtraPackages   []string

	ControllerID    string
	Tools           params.RunnerApplicationDownload
	BootstrapParams params.BootstrapInstance
}

// GetRunnerSpecFromBootstrapParams builds the spec for one runner from the
// provider config, the pool bootstrap params and the pool extra specs.
func GetRunnerSpecFromBootstrapParams(cfg *config.Config, data params.BootstrapInstance, controllerID string) (*RunnerSpec, error) {
	if cfg == nil {
		return nil, fmt.Errorf("a provider configuration is required")
	}

	if err := ValidateOSType(data.OSType); err != nil {
		return nil, err
	}

	tools, err := DefaultToolFetch(data.OSType, data.OSArch, data.Tools)
	if err != nil {
		return nil, fmt.Errorf("getting runner tools: %w", err)
	}

	extra, err := newExtraSpecsFromBootstrapData(data)
	if err != nil {
		return nil, fmt.Errorf("loading extra specs: %w", err)
	}

	bootImage, err := ParseBootImage(data.Image)
	if err != nil {
		return nil, err
	}

	spec := &RunnerSpec{
		Region:            cfg.Region,
		Plan:              data.Flavor,
		BootImage:         bootImage,
		Label:             data.Name,
		Hostname:          data.Name,
		SSHKeyIDs:         cfg.SSHKeyIDs,
		FirewallGroupID:   cfg.FirewallGroupID,
		ScriptID:          cfg.ScriptID,
		Backups:           cfg.Backups,
		UserScheme:        cfg.UserScheme,
		EnableIPv6:        cfg.EnableIPv6,
		DisablePublicIPv4: cfg.DisablePublicIPv4,
		EnableVPC:         cfg.EnableVPC,
		VPCIDs:            cfg.VPCIDs,
		VPCOnly:           cfg.VPCOnly,
		DDoSProtection:    cfg.DDoSProtection,
		ControllerID:      controllerID,
		Tools:             tools,
		BootstrapParams:   data,
	}

	spec.Tags = instanceTags(cfg.Tags, data, controllerID)
	spec.MergeExtraSpecs(extra)

	if err := spec.Validate(); err != nil {
		return nil, fmt.Errorf("validating runner spec: %w", err)
	}

	return spec, nil
}

// instanceTags returns the tags a runner is created with: the provider's own
// identifying tags first, then any extra tags from the configuration.
func instanceTags(configured []string, data params.BootstrapInstance, controllerID string) []string {
	tags := []string{
		util.ControllerIDTag(controllerID),
		util.PoolIDTag(data.PoolID),
		util.OSTypeTag(data.OSType),
		util.OSArchTag(data.OSArch),
	}

	return append(tags, configured...)
}

// MergeExtraSpecs applies the pool overrides on top of the provider defaults.
func (r *RunnerSpec) MergeExtraSpecs(extra *extraSpecs) {
	if extra == nil {
		return
	}

	if extra.Region != nil && *extra.Region != "" {
		r.Region = *extra.Region
	}
	if extra.Hostname != nil && *extra.Hostname != "" {
		r.Hostname = *extra.Hostname
	}
	if len(extra.SSHKeyIDs) > 0 {
		r.SSHKeyIDs = extra.SSHKeyIDs
	}
	if extra.FirewallGroupID != nil {
		r.FirewallGroupID = *extra.FirewallGroupID
	}
	if extra.ScriptID != nil {
		r.ScriptID = *extra.ScriptID
	}
	if extra.ReservedIPv4 != nil {
		r.ReservedIPv4 = *extra.ReservedIPv4
	}
	if extra.Backups != nil {
		r.Backups = *extra.Backups
	}
	if extra.UserScheme != nil {
		r.UserScheme = *extra.UserScheme
	}
	if extra.EnableIPv6 != nil {
		r.EnableIPv6 = *extra.EnableIPv6
	}
	if extra.DisablePublicIPv4 != nil {
		r.DisablePublicIPv4 = *extra.DisablePublicIPv4
	}
	if extra.EnableVPC != nil {
		r.EnableVPC = *extra.EnableVPC
	}
	if len(extra.VPCIDs) > 0 {
		r.VPCIDs = extra.VPCIDs
	}
	if extra.VPCOnly != nil {
		r.VPCOnly = *extra.VPCOnly
	}
	if extra.DDoSProtection != nil {
		r.DDoSProtection = *extra.DDoSProtection
	}
	if extra.DisableUpdates != nil {
		r.DisableUpdates = *extra.DisableUpdates
	}
	if extra.EnableBootDebug != nil {
		r.EnableBootDebug = *extra.EnableBootDebug
	}
	if len(extra.ExtraPackages) > 0 {
		r.ExtraPackages = extra.ExtraPackages
	}
	if len(extra.Tags) > 0 {
		r.Tags = append(r.Tags, extra.Tags...)
	}
}

// Validate checks the spec against the constraints the Vultr API documents for
// POST /v2/instances.
func (r *RunnerSpec) Validate() error {
	if r.Region == "" {
		return fmt.Errorf("region is required")
	}
	if r.Plan == "" {
		return fmt.Errorf("plan is required; set the pool flavor to a Vultr plan id such as vc2-2c-4gb")
	}
	if r.Label == "" {
		return fmt.Errorf("the bootstrap params carry no runner name")
	}
	if r.ControllerID == "" {
		return fmt.Errorf("a controller id is required")
	}
	if err := r.BootImage.Validate(); err != nil {
		return err
	}

	switch r.Backups {
	case "", config.BackupsEnabled, config.BackupsDisabled:
	default:
		return fmt.Errorf("backups must be %q or %q, got %q", config.BackupsEnabled, config.BackupsDisabled, r.Backups)
	}

	switch r.UserScheme {
	case "", config.UserSchemeRoot, config.UserSchemeLimited:
	default:
		return fmt.Errorf("user_scheme must be %q or %q, got %q", config.UserSchemeRoot, config.UserSchemeLimited, r.UserScheme)
	}

	// Without IPv6 the Vultr API ignores disable_public_ipv4, and a runner
	// with no address at all could never reach the GARM callback URL.
	if r.DisablePublicIPv4 && !r.EnableIPv6 {
		return fmt.Errorf("disable_public_ipv4 requires enable_ipv6")
	}

	if r.VPCOnly && len(r.VPCIDs) == 0 {
		return fmt.Errorf("vpc_only requires vpc_ids; the first VPC must have a NAT gateway attached")
	}

	return nil
}

// ComposeUserData renders the GARM bootstrap as base64 encoded cloud-init
// data, which is the form the Vultr API expects.
func (r *RunnerSpec) ComposeUserData() (string, error) {
	bootstrapParams := r.BootstrapParams
	bootstrapParams.UserDataOptions.DisableUpdatesOnBoot = r.DisableUpdates
	bootstrapParams.UserDataOptions.EnableBootDebug = r.EnableBootDebug
	if len(r.ExtraPackages) > 0 {
		bootstrapParams.UserDataOptions.ExtraPackages = r.ExtraPackages
	}

	if err := ValidateOSType(bootstrapParams.OSType); err != nil {
		return "", err
	}

	cloudCfg, err := cloudconfig.GetCloudConfig(bootstrapParams, r.Tools, bootstrapParams.Name)
	if err != nil {
		return "", fmt.Errorf("generating cloud config: %w", err)
	}

	return base64.StdEncoding.EncodeToString([]byte(cloudCfg)), nil
}

// CreateRequest turns the spec into the Vultr API call that creates the
// runner.
func (r *RunnerSpec) CreateRequest() (*client.CreateInstanceRequest, error) {
	userData, err := r.ComposeUserData()
	if err != nil {
		return nil, err
	}

	req := &client.CreateInstanceRequest{
		Region:          r.Region,
		Plan:            r.Plan,
		Label:           r.Label,
		Hostname:        r.Hostname,
		Tags:            r.Tags,
		OsID:            r.BootImage.OsID,
		SnapshotID:      r.BootImage.SnapshotID,
		AppID:           r.BootImage.AppID,
		ImageID:         r.BootImage.ImageID,
		UserData:        userData,
		SSHKeyIDs:       r.SSHKeyIDs,
		FirewallGroupID: r.FirewallGroupID,
		ScriptID:        r.ScriptID,
		ReservedIPv4:    r.ReservedIPv4,
		Backups:         r.Backups,
		UserScheme:      r.UserScheme,
		AttachVPC:       r.VPCIDs,
		// Runners are created and destroyed constantly; a deployment email
		// for each one is noise.
		ActivationEmail: client.Bool(false),
	}

	// Optional booleans are only sent when they are turned on, so that the
	// Vultr account default applies otherwise.
	if r.EnableIPv6 {
		req.EnableIPv6 = client.Bool(true)
	}
	if r.DisablePublicIPv4 {
		req.DisablePublicIPv4 = client.Bool(true)
	}
	if r.VPCOnly {
		req.VPCOnly = client.Bool(true)
	}
	if r.DDoSProtection {
		req.DDoSProtection = client.Bool(true)
	}
	// attach_vpc takes precedence over enable_vpc in the Vultr API, so
	// enable_vpc is only sent when no explicit VPC was given.
	if r.EnableVPC && len(r.VPCIDs) == 0 {
		req.EnableVPC = client.Bool(true)
	}

	return req, nil
}

// ValidateOSType rejects the runner platforms this provider has not been
// verified against.
//
// GARM delivers the runner bootstrap through cloud-init user data. Vultr Linux
// images run cloud-init; whether Vultr Windows images process user data the
// same way has not been verified, so Windows pools are rejected rather than
// silently created without a runner.
func ValidateOSType(osType params.OSType) error {
	switch osType {
	case params.Linux:
		return nil
	case "":
		return fmt.Errorf("os_type is required")
	default:
		return fmt.Errorf("os_type %q is not supported; this provider currently supports linux runners", osType)
	}
}

// newExtraSpecsFromBootstrapData validates the pool extra specs against the
// schema and decodes them.
func newExtraSpecsFromBootstrapData(data params.BootstrapInstance) (*extraSpecs, error) {
	spec := &extraSpecs{}

	if len(data.ExtraSpecs) == 0 {
		return spec, nil
	}

	if err := ValidateExtraSpecs(data.ExtraSpecs); err != nil {
		return nil, err
	}

	if err := json.Unmarshal(data.ExtraSpecs, spec); err != nil {
		return nil, fmt.Errorf("decoding extra specs: %w", err)
	}

	return spec, nil
}

// ValidateExtraSpecs checks raw extra specs against the generated schema. It
// rejects unknown fields, which catches typos in a pool definition before any
// instance is created.
func ValidateExtraSpecs(raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}

	schemaLoader := gojsonschema.NewGoLoader(generateJSONSchema(&extraSpecs{}))
	specLoader := gojsonschema.NewBytesLoader(raw)

	result, err := gojsonschema.Validate(schemaLoader, specLoader)
	if err != nil {
		return fmt.Errorf("validating extra specs: %w", err)
	}

	if !result.Valid() {
		return fmt.Errorf("extra specs failed schema validation: %s", result.Errors())
	}

	return nil
}

// ExtraSpecsJSONSchema returns the JSON schema of the pool extra specs.
func ExtraSpecsJSONSchema() (string, error) {
	return marshalSchema(generateJSONSchema(&extraSpecs{}))
}

// ConfigJSONSchema returns the JSON schema of the provider config file.
func ConfigJSONSchema() (string, error) {
	return marshalSchema(generateJSONSchema(&config.Config{}))
}

// generateJSONSchema reflects a struct into a JSON schema that rejects
// unknown properties.
func generateJSONSchema(v interface{}) *jsonschema.Schema {
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties: false,
	}

	return reflector.Reflect(v)
}

func marshalSchema(schema *jsonschema.Schema) (string, error) {
	asJSON, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encoding json schema: %w", err)
	}

	return string(asJSON), nil
}
