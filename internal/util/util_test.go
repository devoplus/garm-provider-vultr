// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Devoplus

package util

import (
	"testing"

	"github.com/cloudbase/garm-provider-common/params"
	"github.com/stretchr/testify/assert"

	"github.com/devoplus/garm-provider-vultr/internal/client"
)

func TestTagHelpers(t *testing.T) {
	controllerID := "f9286791-1589-4f39-a106-5b68c2a18af4"
	poolID := "9dcf590a-1192-4a9c-b3e4-e0902974c2c0"

	tags := []string{
		ControllerIDTag(controllerID),
		PoolIDTag(poolID),
		OSTypeTag(params.Linux),
		OSArchTag(params.Amd64),
	}

	assert.Equal(t, "garm-controller-id-"+controllerID, tags[0])
	assert.Equal(t, "garm-pool-id-"+poolID, tags[1])
	assert.Equal(t, "garm-os-type-linux", tags[2])
	assert.Equal(t, "garm-os-arch-amd64", tags[3])

	assert.Equal(t, controllerID, TagValue(tags, ControllerIDTagPrefix))
	assert.Equal(t, poolID, TagValue(tags, PoolIDTagPrefix))
	assert.Equal(t, "linux", TagValue(tags, OSTypeTagPrefix))
	assert.Equal(t, "amd64", TagValue(tags, OSArchTagPrefix))
	assert.Equal(t, "", TagValue(nil, ControllerIDTagPrefix))
	assert.Equal(t, "", TagValue([]string{"unrelated"}, ControllerIDTagPrefix))

	assert.True(t, HasTag(tags, ControllerIDTag(controllerID)))
	assert.False(t, HasTag(tags, ControllerIDTag("someone-else")))
	assert.False(t, HasTag(nil, ControllerIDTag(controllerID)))
}

func TestVultrStatusToProviderStatus(t *testing.T) {
	tests := []struct {
		name        string
		status      string
		powerStatus string
		want        params.InstanceStatus
	}{
		{
			name:        "pending is reported as running so garm keeps waiting for the callback",
			status:      VultrStatusPending,
			powerStatus: VultrPowerStatusRunning,
			want:        params.InstanceRunning,
		},
		{
			name:        "active and running",
			status:      VultrStatusActive,
			powerStatus: VultrPowerStatusRunning,
			want:        params.InstanceRunning,
		},
		{
			name:        "active and stopped",
			status:      VultrStatusActive,
			powerStatus: VultrPowerStatusStopped,
			want:        params.InstanceStopped,
		},
		{
			name:        "active with no power state is still actionable",
			status:      VultrStatusActive,
			powerStatus: "",
			want:        params.InstanceRunning,
		},
		{
			name:        "resizing",
			status:      VultrStatusResizing,
			powerStatus: VultrPowerStatusRunning,
			want:        params.InstanceRunning,
		},
		{
			name:        "suspended is an error the provider cannot fix",
			status:      VultrStatusSuspended,
			powerStatus: VultrPowerStatusStopped,
			want:        params.InstanceError,
		},
		{
			name:        "an unknown state maps to unknown",
			status:      "something-new",
			powerStatus: "",
			want:        params.InstanceStatusUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, VultrStatusToProviderStatus(tt.status, tt.powerStatus))
		})
	}
}

// TestProviderStatusIsAcceptedByGarm pins the contract: GARM only accepts
// running, stopped, error and unknown from a provider, and sets every other
// status itself.
func TestProviderStatusIsAcceptedByGarm(t *testing.T) {
	accepted := map[params.InstanceStatus]bool{
		params.InstanceRunning:       true,
		params.InstanceStopped:       true,
		params.InstanceError:         true,
		params.InstanceStatusUnknown: true,
	}

	vultrStates := []struct {
		status      string
		powerStatus string
	}{
		{VultrStatusPending, VultrPowerStatusRunning},
		{VultrStatusActive, VultrPowerStatusRunning},
		{VultrStatusActive, VultrPowerStatusStopped},
		{VultrStatusResizing, VultrPowerStatusRunning},
		{VultrStatusSuspended, VultrPowerStatusStopped},
		{"", ""},
		{"brand-new-state", "brand-new-power-state"},
	}

	for _, state := range vultrStates {
		got := VultrStatusToProviderStatus(state.status, state.powerStatus)
		assert.Truef(t, accepted[got], "status %q/%q mapped to %q, which garm rejects", state.status, state.powerStatus, got)
	}
}

func TestVultrInstanceToProviderInstance(t *testing.T) {
	instance := client.Instance{
		ID:          "4f0f12e5-1f84-404f-aa84-85f431ea5ec2",
		Label:       "garm-ny9HeeQYw2rl",
		OS:          "Ubuntu 22.04 x64",
		Status:      VultrStatusActive,
		PowerStatus: VultrPowerStatusRunning,
		MainIP:      "192.0.2.123",
		V6MainIP:    "2001:db8::1",
		InternalIP:  "10.0.0.5",
		Tags: []string{
			ControllerIDTag("controller-1"),
			PoolIDTag("pool-1"),
			OSTypeTag(params.Linux),
			OSArchTag(params.Amd64),
		},
		VPCs: []client.VPCInfo{
			{ID: "vpc-1", IPAddress: "10.1.0.7"},
		},
	}

	got := VultrInstanceToProviderInstance(instance)

	assert.Equal(t, "4f0f12e5-1f84-404f-aa84-85f431ea5ec2", got.ProviderID)
	assert.Equal(t, "garm-ny9HeeQYw2rl", got.Name)
	assert.Equal(t, params.Linux, got.OSType)
	assert.Equal(t, params.Amd64, got.OSArch)
	assert.Equal(t, "Ubuntu 22.04 x64", got.OSName)
	assert.Equal(t, params.InstanceRunning, got.Status)
	assert.Empty(t, got.ProviderFault)

	assert.Equal(t, []params.Address{
		{Address: "192.0.2.123", Type: params.PublicAddress},
		{Address: "2001:db8::1", Type: params.PublicAddress},
		{Address: "10.0.0.5", Type: params.PrivateAddress},
		{Address: "10.1.0.7", Type: params.PrivateAddress},
	}, got.Addresses)
}

// TestVultrInstanceToProviderInstanceFreshInstance covers the create response,
// where Vultr reports the placeholder address 0.0.0.0 and no tags yet.
func TestVultrInstanceToProviderInstanceFreshInstance(t *testing.T) {
	instance := client.Instance{
		ID:          "4f0f12e5-1f84-404f-aa84-85f431ea5ec2",
		Status:      VultrStatusPending,
		PowerStatus: VultrPowerStatusRunning,
		MainIP:      "0.0.0.0",
		V6MainIP:    "",
	}

	got := VultrInstanceToProviderInstance(instance)

	assert.Equal(t, params.InstanceRunning, got.Status)
	assert.Empty(t, got.Addresses, "the placeholder address must not be reported")
}

func TestVultrInstanceToProviderInstanceSuspended(t *testing.T) {
	instance := client.Instance{
		ID:          "4f0f12e5-1f84-404f-aa84-85f431ea5ec2",
		Status:      VultrStatusSuspended,
		PowerStatus: VultrPowerStatusStopped,
	}

	got := VultrInstanceToProviderInstance(instance)

	assert.Equal(t, params.InstanceError, got.Status)
	assert.NotEmpty(t, got.ProviderFault, "a suspended instance must explain itself to garm")
}

func TestIsUsableAddress(t *testing.T) {
	assert.False(t, isUsableAddress(""))
	assert.False(t, isUsableAddress("  "))
	assert.False(t, isUsableAddress("0.0.0.0"))
	assert.False(t, isUsableAddress("::"))
	assert.True(t, isUsableAddress("192.0.2.1"))
	assert.True(t, isUsableAddress("2001:db8::1"))
}
