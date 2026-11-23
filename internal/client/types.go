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

package client

import "context"

// VultrAPI is the slice of the Vultr API the provider needs. The provider
// depends on this interface rather than on the concrete client so that the
// lifecycle logic can be tested without an HTTP endpoint.
type VultrAPI interface {
	// CreateInstance deploys a new instance. The Vultr API answers 202 with an
	// instance whose status is still pending.
	CreateInstance(ctx context.Context, req *CreateInstanceRequest) (*Instance, error)
	// GetInstance returns one instance by its Vultr id. It returns an error
	// that satisfies errors.Is(err, garm errors.ErrNotFound) when the instance
	// does not exist.
	GetInstance(ctx context.Context, instanceID string) (*Instance, error)
	// DeleteInstance destroys an instance. Deleting an instance that is
	// already gone is not an error.
	DeleteInstance(ctx context.Context, instanceID string) error
	// ListInstances returns every instance in the account, following the
	// cursor pagination to the end.
	ListInstances(ctx context.Context) ([]Instance, error)
	// StartInstance boots a halted instance.
	StartInstance(ctx context.Context, instanceID string) error
	// HaltInstance shuts an instance down without destroying it.
	HaltInstance(ctx context.Context, instanceID string) error
}

// Instance holds the fields of the Vultr instance object the provider uses.
// The Vultr API returns more; unused fields are deliberately left out.
type Instance struct {
	ID           string    `json:"id"`
	OS           string    `json:"os"`
	OsID         int       `json:"os_id"`
	AppID        int       `json:"app_id"`
	ImageID      string    `json:"image_id"`
	SnapshotID   string    `json:"snapshot_id"`
	Region       string    `json:"region"`
	Plan         string    `json:"plan"`
	Status       string    `json:"status"`
	PowerStatus  string    `json:"power_status"`
	ServerStatus string    `json:"server_status"`
	MainIP       string    `json:"main_ip"`
	V6MainIP     string    `json:"v6_main_ip"`
	InternalIP   string    `json:"internal_ip"`
	Label        string    `json:"label"`
	Hostname     string    `json:"hostname"`
	Tags         []string  `json:"tags"`
	VPCs         []VPCInfo `json:"vpcs"`
	DateCreated  string    `json:"date_created"`
}

// VPCInfo is the VPC attachment Vultr reports on an instance.
type VPCInfo struct {
	ID         string `json:"id"`
	MacAddress string `json:"mac_address"`
	IPAddress  string `json:"ip_address"`
}

// CreateInstanceRequest is the body of POST /v2/instances.
//
// Booleans are pointers so that an unset option is omitted from the request
// and the Vultr account default applies, rather than being sent as false.
type CreateInstanceRequest struct {
	Region string   `json:"region"`
	Plan   string   `json:"plan"`
	Label  string   `json:"label,omitempty"`
	Tags   []string `json:"tags,omitempty"`

	// Exactly one of the following selects what the instance boots.
	OsID       int    `json:"os_id,omitempty"`
	SnapshotID string `json:"snapshot_id,omitempty"`
	AppID      int    `json:"app_id,omitempty"`
	ImageID    string `json:"image_id,omitempty"`

	Hostname        string   `json:"hostname,omitempty"`
	UserData        string   `json:"user_data,omitempty"`
	SSHKeyIDs       []string `json:"sshkey_id,omitempty"`
	FirewallGroupID string   `json:"firewall_group_id,omitempty"`
	ScriptID        string   `json:"script_id,omitempty"`
	ReservedIPv4    string   `json:"reserved_ipv4,omitempty"`
	Backups         string   `json:"backups,omitempty"`
	UserScheme      string   `json:"user_scheme,omitempty"`

	EnableIPv6        *bool    `json:"enable_ipv6,omitempty"`
	DisablePublicIPv4 *bool    `json:"disable_public_ipv4,omitempty"`
	EnableVPC         *bool    `json:"enable_vpc,omitempty"`
	AttachVPC         []string `json:"attach_vpc,omitempty"`
	VPCOnly           *bool    `json:"vpc_only,omitempty"`
	DDoSProtection    *bool    `json:"ddos_protection,omitempty"`
	ActivationEmail   *bool    `json:"activation_email,omitempty"`
}

// instanceResponse is the envelope the Vultr API wraps a single instance in.
type instanceResponse struct {
	Instance *Instance `json:"instance"`
}

// instanceListResponse is the envelope for a page of instances.
type instanceListResponse struct {
	Instances []Instance `json:"instances"`
	Meta      *meta      `json:"meta"`
}

// meta carries the cursor pagination the Vultr API documents.
type meta struct {
	Total int    `json:"total"`
	Links *links `json:"links"`
}

type links struct {
	Next string `json:"next"`
	Prev string `json:"prev"`
}

// errorResponse is the body the Vultr API returns with every 4xx.
type errorResponse struct {
	Error  string `json:"error"`
	Status int    `json:"status"`
}
