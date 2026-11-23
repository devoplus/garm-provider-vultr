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

// Package provider implements the GARM external provider interface on top of
// the Vultr API.
package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	garmErrors "github.com/cloudbase/garm-provider-common/errors"
	"github.com/cloudbase/garm-provider-common/execution/common"
	executionv011 "github.com/cloudbase/garm-provider-common/execution/v0.1.1"
	"github.com/cloudbase/garm-provider-common/params"

	"github.com/devoplus/garm-provider-vultr/config"
	"github.com/devoplus/garm-provider-vultr/internal/client"
	"github.com/devoplus/garm-provider-vultr/internal/spec"
	"github.com/devoplus/garm-provider-vultr/internal/util"
)

// Version is set at build time from the git describe output.
var Version = "v0.0.0-unknown"

// The provider implements the v0.1.1 interface, which is a superset of
// v0.1.0, so GARM can drive it with either interface version.
var _ executionv011.ExternalProvider = &vultrProvider{}

// vultrIDPattern matches the UUID form of a Vultr instance id.
//
// GARM calls GetInstance, DeleteInstance, Start and Stop with the provider id
// returned by CreateInstance, but when a create fails before reporting one it
// passes the runner name instead. The two are told apart by shape.
var vultrIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type vultrProvider struct {
	cfg          *config.Config
	cli          client.VultrAPI
	controllerID string
}

// New returns a provider that reads its configuration from configPath.
func New(configPath, controllerID string) (executionv011.ExternalProvider, error) {
	if controllerID == "" {
		return nil, fmt.Errorf("a controller id is required")
	}

	cfg, err := config.New(configPath)
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}

	cli, err := client.New(cfg.APIKey, client.Options{
		BaseURL:   cfg.BaseURL,
		UserAgent: "garm-provider-vultr/" + Version,
	})
	if err != nil {
		return nil, fmt.Errorf("creating vultr client: %w", err)
	}

	return &vultrProvider{
		cfg:          cfg,
		cli:          cli,
		controllerID: controllerID,
	}, nil
}

// NewWithClient returns a provider backed by the given config and API client.
// It exists so that the lifecycle can be tested without reading a config file
// or reaching the Vultr API.
func NewWithClient(cfg *config.Config, cli client.VultrAPI, controllerID string) (executionv011.ExternalProvider, error) {
	if cfg == nil {
		return nil, fmt.Errorf("a provider configuration is required")
	}
	if cli == nil {
		return nil, fmt.Errorf("an api client is required")
	}
	if controllerID == "" {
		return nil, fmt.Errorf("a controller id is required")
	}

	return &vultrProvider{
		cfg:          cfg,
		cli:          cli,
		controllerID: controllerID,
	}, nil
}

// CreateInstance deploys a runner instance.
//
// The Vultr API creates the instance in one call, so there is no partially
// built state to unwind here. GARM retries a failed create and runs
// DeleteInstance with the runner name beforehand, which cleans up an instance
// that was created but never reported back.
func (p *vultrProvider) CreateInstance(ctx context.Context, bootstrapParams params.BootstrapInstance) (params.ProviderInstance, error) {
	runnerSpec, err := spec.GetRunnerSpecFromBootstrapParams(p.cfg, bootstrapParams, p.controllerID)
	if err != nil {
		return params.ProviderInstance{}, fmt.Errorf("building runner spec: %w", err)
	}

	createReq, err := runnerSpec.CreateRequest()
	if err != nil {
		return params.ProviderInstance{}, fmt.Errorf("building create request: %w", err)
	}

	instance, err := p.cli.CreateInstance(ctx, createReq)
	if err != nil {
		return params.ProviderInstance{}, fmt.Errorf("creating instance: %w", err)
	}

	providerInstance := util.VultrInstanceToProviderInstance(*instance)

	// A freshly created instance carries the label and tags from the request,
	// but Vultr does not always echo them back on the create response. The
	// values from the bootstrap params are authoritative here.
	providerInstance.Name = bootstrapParams.Name
	providerInstance.OSType = bootstrapParams.OSType
	providerInstance.OSArch = bootstrapParams.OSArch

	return providerInstance, nil
}

// DeleteInstance destroys a runner instance. Deleting an instance that no
// longer exists, or that belongs to another GARM controller, is a no-op.
func (p *vultrProvider) DeleteInstance(ctx context.Context, instance string) error {
	target, err := p.resolveInstance(ctx, instance)
	if err != nil {
		if errors.Is(err, garmErrors.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("looking up instance %s: %w", instance, err)
	}

	if err := p.cli.DeleteInstance(ctx, target.ID); err != nil {
		return fmt.Errorf("deleting instance %s: %w", target.ID, err)
	}

	return nil
}

// GetInstance returns the state of one runner instance.
func (p *vultrProvider) GetInstance(ctx context.Context, instance string) (params.ProviderInstance, error) {
	target, err := p.resolveInstance(ctx, instance)
	if err != nil {
		return params.ProviderInstance{}, fmt.Errorf("looking up instance %s: %w", instance, err)
	}

	return util.VultrInstanceToProviderInstance(*target), nil
}

// ListInstances returns every runner this controller owns in the given pool.
func (p *vultrProvider) ListInstances(ctx context.Context, poolID string) ([]params.ProviderInstance, error) {
	if poolID == "" {
		return nil, fmt.Errorf("a pool id is required")
	}

	instances, err := p.cli.ListInstances(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing instances: %w", err)
	}

	poolTag := util.PoolIDTag(poolID)
	providerInstances := []params.ProviderInstance{}
	for _, instance := range instances {
		if !p.ownsInstance(instance) {
			continue
		}
		if !util.HasTag(instance.Tags, poolTag) {
			continue
		}
		providerInstances = append(providerInstances, util.VultrInstanceToProviderInstance(instance))
	}

	return providerInstances, nil
}

// RemoveAllInstances destroys every instance this controller owns.
//
// GARM does not call this today, but the interface requires it. It only ever
// touches instances tagged with this controller id.
func (p *vultrProvider) RemoveAllInstances(ctx context.Context) error {
	instances, err := p.cli.ListInstances(ctx)
	if err != nil {
		return fmt.Errorf("listing instances: %w", err)
	}

	var failures []error
	for _, instance := range instances {
		if !p.ownsInstance(instance) {
			continue
		}
		if err := p.cli.DeleteInstance(ctx, instance.ID); err != nil {
			failures = append(failures, fmt.Errorf("deleting instance %s: %w", instance.ID, err))
		}
	}

	return errors.Join(failures...)
}

// Stop halts a runner instance.
//
// The force flag is ignored: the Vultr API offers a single halt operation,
// which powers the instance off.
func (p *vultrProvider) Stop(ctx context.Context, instance string, _ bool) error {
	target, err := p.resolveInstance(ctx, instance)
	if err != nil {
		return fmt.Errorf("looking up instance %s: %w", instance, err)
	}

	if err := p.cli.HaltInstance(ctx, target.ID); err != nil {
		return fmt.Errorf("halting instance %s: %w", target.ID, err)
	}

	return nil
}

// Start boots a halted runner instance.
func (p *vultrProvider) Start(ctx context.Context, instance string) error {
	target, err := p.resolveInstance(ctx, instance)
	if err != nil {
		return fmt.Errorf("looking up instance %s: %w", instance, err)
	}

	if err := p.cli.StartInstance(ctx, target.ID); err != nil {
		return fmt.Errorf("starting instance %s: %w", target.ID, err)
	}

	return nil
}

// GetVersion returns the provider version.
func (p *vultrProvider) GetVersion(_ context.Context) string {
	return Version
}

// GetSupportedInterfaceVersions reports the provider interface versions this
// binary implements.
func (p *vultrProvider) GetSupportedInterfaceVersions(_ context.Context) []string {
	return []string{common.Version010, common.Version011}
}

// ValidatePoolInfo checks a pool definition, so that a bad image, flavor or
// extra spec can be reported when the pool is created rather than when the
// first runner fails.
//
// GARM does not call this operation yet, and the v0.1.1 execution environment
// only reads the bootstrap params from stdin for CreateInstance, so image and
// flavor can arrive empty. An empty value is therefore skipped rather than
// rejected: refusing it would block pool creation once GARM starts calling
// this. CreateInstance validates the same fields in full, so nothing reaches
// the Vultr API unchecked either way.
func (p *vultrProvider) ValidatePoolInfo(_ context.Context, image string, flavor string, _ string, extraspecs string) error {
	if image != "" {
		if _, err := spec.ParseBootImage(image); err != nil {
			return err
		}
	}

	if flavor != "" && strings.TrimSpace(flavor) == "" {
		return fmt.Errorf("flavor must be a Vultr plan id such as vc2-2c-4gb. See GET /v2/plans")
	}

	if extraspecs != "" {
		if err := spec.ValidateExtraSpecs(json.RawMessage(extraspecs)); err != nil {
			return err
		}
	}

	return nil
}

// GetConfigJSONSchema returns the schema of the provider config file.
func (p *vultrProvider) GetConfigJSONSchema(_ context.Context) (string, error) {
	return spec.ConfigJSONSchema()
}

// GetExtraSpecsJSONSchema returns the schema of the pool extra specs.
func (p *vultrProvider) GetExtraSpecsJSONSchema(_ context.Context) (string, error) {
	return spec.ExtraSpecsJSONSchema()
}

// resolveInstance finds an instance from either a Vultr instance id or a
// runner name, and refuses to return one that belongs to another GARM
// controller.
func (p *vultrProvider) resolveInstance(ctx context.Context, idOrName string) (*client.Instance, error) {
	if idOrName == "" {
		return nil, fmt.Errorf("an instance id or name is required")
	}

	if vultrIDPattern.MatchString(idOrName) {
		instance, err := p.cli.GetInstance(ctx, idOrName)
		if err != nil {
			return nil, err
		}
		if !p.ownsInstance(*instance) {
			// Another GARM installation owns this instance. Reporting it as
			// not found keeps this controller from acting on it.
			return nil, fmt.Errorf("instance %s is not owned by this controller: %w", idOrName, garmErrors.ErrNotFound)
		}
		return instance, nil
	}

	return p.findInstanceByName(ctx, idOrName)
}

// findInstanceByName looks an instance up by the label the provider sets to
// the runner name. The Vultr list endpoint can only filter on a deprecated
// query parameter, so the listing is filtered here.
func (p *vultrProvider) findInstanceByName(ctx context.Context, name string) (*client.Instance, error) {
	instances, err := p.cli.ListInstances(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing instances: %w", err)
	}

	for idx := range instances {
		instance := instances[idx]
		if instance.Label != name {
			continue
		}
		if !p.ownsInstance(instance) {
			continue
		}
		return &instance, nil
	}

	return nil, fmt.Errorf("no instance named %s: %w", name, garmErrors.ErrNotFound)
}

// ownsInstance reports whether an instance carries this controller's tag. It
// is the boundary that keeps one GARM installation from touching the
// instances of another.
func (p *vultrProvider) ownsInstance(instance client.Instance) bool {
	return util.HasTag(instance.Tags, util.ControllerIDTag(p.controllerID))
}
