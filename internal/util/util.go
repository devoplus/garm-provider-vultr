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

package util

import (
	"strings"

	"github.com/cloudbase/garm-provider-common/params"

	"github.com/devoplus/garm-provider-vultr/internal/client"
)

// Vultr tags carry the only metadata the provider stores on an instance.
//
// The Vultr API documents no character set for tags and its list endpoint can
// only filter on the deprecated singular "tag" query parameter, so the
// provider keeps tags to lowercase letters, digits and dashes and filters
// client side. Each prefix is followed by the value it carries, which is why
// every prefix ends in a dash.
const (
	// ControllerIDTagPrefix identifies the GARM installation that owns the
	// instance. The external provider contract requires every resource to
	// carry it so that one GARM deployment never touches another one's
	// instances.
	ControllerIDTagPrefix = "garm-controller-id-"

	// PoolIDTagPrefix identifies the pool the runner belongs to. ListInstances
	// selects on it.
	PoolIDTagPrefix = "garm-pool-id-"

	// OSTypeTagPrefix and OSArchTagPrefix preserve the bootstrap params that
	// the Vultr instance object does not report back, so that GetInstance and
	// ListInstances can return them.
	OSTypeTagPrefix = "garm-os-type-"
	OSArchTagPrefix = "garm-os-arch-"
)

// Instance states reported by the Vultr API. "status" describes the
// subscription, "power_status" the virtual machine.
const (
	VultrStatusPending   = "pending"
	VultrStatusActive    = "active"
	VultrStatusSuspended = "suspended"
	VultrStatusResizing  = "resizing"

	VultrPowerStatusRunning = "running"
	VultrPowerStatusStopped = "stopped"
)

// ControllerIDTag returns the tag that marks an instance as owned by a GARM
// controller.
func ControllerIDTag(controllerID string) string {
	return ControllerIDTagPrefix + controllerID
}

// PoolIDTag returns the tag that marks an instance as part of a pool.
func PoolIDTag(poolID string) string {
	return PoolIDTagPrefix + poolID
}

// OSTypeTag returns the tag that records the runner OS type.
func OSTypeTag(osType params.OSType) string {
	return OSTypeTagPrefix + string(osType)
}

// OSArchTag returns the tag that records the runner CPU architecture.
func OSArchTag(osArch params.OSArch) string {
	return OSArchTagPrefix + string(osArch)
}

// TagValue returns the value carried by the first tag with the given prefix,
// or an empty string when no tag matches.
func TagValue(tags []string, prefix string) string {
	for _, tag := range tags {
		if strings.HasPrefix(tag, prefix) {
			return strings.TrimPrefix(tag, prefix)
		}
	}
	return ""
}

// HasTag reports whether the exact tag is present.
func HasTag(tags []string, want string) bool {
	for _, tag := range tags {
		if tag == want {
			return true
		}
	}
	return false
}

// VultrStatusToProviderStatus maps the Vultr instance state onto the only four
// statuses GARM accepts from a provider: running, stopped, error and unknown.
// GARM sets the create and delete states itself, so a provider must never
// report them.
func VultrStatusToProviderStatus(status, powerStatus string) params.InstanceStatus {
	switch status {
	case VultrStatusPending, VultrStatusResizing:
		// The instance exists and Vultr is still working on it. Reporting it
		// as running keeps GARM waiting for the runner to call back instead
		// of treating the instance as gone.
		return params.InstanceRunning
	case VultrStatusActive:
		switch powerStatus {
		case VultrPowerStatusStopped:
			return params.InstanceStopped
		case VultrPowerStatusRunning:
			return params.InstanceRunning
		default:
			// An active subscription with an unreported power state is still
			// an instance GARM can act on.
			return params.InstanceRunning
		}
	case VultrStatusSuspended:
		// Vultr suspends instances for billing or abuse reasons. The runner
		// will never come up and the provider cannot fix it, so this is an
		// error rather than a stopped instance.
		return params.InstanceError
	default:
		return params.InstanceStatusUnknown
	}
}

// VultrInstanceToProviderInstance converts a Vultr instance into the structure
// GARM expects on stdout.
func VultrInstanceToProviderInstance(inst client.Instance) params.ProviderInstance {
	providerInstance := params.ProviderInstance{
		ProviderID: inst.ID,
		Name:       inst.Label,
		OSType:     params.OSType(TagValue(inst.Tags, OSTypeTagPrefix)),
		OSArch:     params.OSArch(TagValue(inst.Tags, OSArchTagPrefix)),
		OSName:     inst.OS,
		Status:     VultrStatusToProviderStatus(inst.Status, inst.PowerStatus),
		Addresses:  instanceAddresses(inst),
	}

	if providerInstance.Status == params.InstanceError {
		providerInstance.ProviderFault = []byte("vultr reports the instance as suspended")
	}

	return providerInstance
}

// instanceAddresses collects the addresses Vultr reports for an instance.
// A freshly created instance has no address yet and reports 0.0.0.0, which is
// left out rather than reported as an address.
func instanceAddresses(inst client.Instance) []params.Address {
	var addresses []params.Address

	if isUsableAddress(inst.MainIP) {
		addresses = append(addresses, params.Address{
			Address: inst.MainIP,
			Type:    params.PublicAddress,
		})
	}

	if isUsableAddress(inst.V6MainIP) {
		addresses = append(addresses, params.Address{
			Address: inst.V6MainIP,
			Type:    params.PublicAddress,
		})
	}

	if isUsableAddress(inst.InternalIP) {
		addresses = append(addresses, params.Address{
			Address: inst.InternalIP,
			Type:    params.PrivateAddress,
		})
	}

	for _, vpc := range inst.VPCs {
		if isUsableAddress(vpc.IPAddress) {
			addresses = append(addresses, params.Address{
				Address: vpc.IPAddress,
				Type:    params.PrivateAddress,
			})
		}
	}

	return addresses
}

// isUsableAddress filters out the placeholders Vultr returns before an address
// is assigned.
func isUsableAddress(addr string) bool {
	switch strings.TrimSpace(addr) {
	case "", "0.0.0.0", "::":
		return false
	default:
		return true
	}
}
