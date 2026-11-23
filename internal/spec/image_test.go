// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Devoplus

package spec

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseBootImage(t *testing.T) {
	tests := []struct {
		name  string
		image string
		want  BootImage
	}{
		{
			name:  "a bare number is an operating system id",
			image: "1743",
			want:  BootImage{OsID: 1743},
		},
		{
			name:  "surrounding whitespace is ignored",
			image: "  1743  ",
			want:  BootImage{OsID: 1743},
		},
		{
			name:  "an explicit os id",
			image: "os_id:2284",
			want:  BootImage{OsID: 2284},
		},
		{
			name:  "a snapshot",
			image: "snapshot_id:cb676a46-66fd-4dfb-b839-443f2e6c0b60",
			want:  BootImage{SnapshotID: "cb676a46-66fd-4dfb-b839-443f2e6c0b60"},
		},
		{
			name:  "a one-click application",
			image: "app_id:1234",
			want:  BootImage{AppID: 1234},
		},
		{
			name:  "a marketplace image",
			image: "image_id:docker",
			want:  BootImage{ImageID: "docker"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseBootImage(tt.image)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			assert.NoError(t, got.Validate())
		})
	}
}

func TestParseBootImageRejectsBadInput(t *testing.T) {
	tests := []struct {
		name    string
		image   string
		wantErr string
	}{
		{
			name:    "empty",
			image:   "",
			wantErr: "image is required",
		},
		{
			name:    "whitespace only",
			image:   "   ",
			wantErr: "image is required",
		},
		{
			name:    "a name instead of an id",
			image:   "ubuntu-22.04",
			wantErr: "image must be a Vultr os id",
		},
		{
			name:    "a non numeric os id",
			image:   "os_id:ubuntu",
			wantErr: "is not a number",
		},
		{
			name:    "a zero os id",
			image:   "os_id:0",
			wantErr: "not a valid id",
		},
		{
			name:    "a negative os id",
			image:   "os_id:-1",
			wantErr: "not a valid id",
		},
		{
			name:    "an empty snapshot id",
			image:   "snapshot_id:",
			wantErr: "snapshot id is empty",
		},
		{
			name:    "an empty image id",
			image:   "image_id:",
			wantErr: "image id is empty",
		},
		{
			name:    "a non numeric app id",
			image:   "app_id:wordpress",
			wantErr: "is not a number",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseBootImage(tt.image)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestBootImageValidate(t *testing.T) {
	assert.ErrorContains(t, BootImage{}.Validate(), "no boot source selected")

	assert.NoError(t, BootImage{OsID: 1743}.Validate())
	assert.NoError(t, BootImage{SnapshotID: "snap-1"}.Validate())
	assert.NoError(t, BootImage{AppID: 7}.Validate())
	assert.NoError(t, BootImage{ImageID: "docker"}.Validate())

	// The Vultr API accepts exactly one boot source per instance.
	assert.ErrorContains(t, BootImage{OsID: 1743, SnapshotID: "snap-1"}.Validate(), "only one boot source")
	assert.ErrorContains(t, BootImage{AppID: 7, ImageID: "docker"}.Validate(), "only one boot source")
}
