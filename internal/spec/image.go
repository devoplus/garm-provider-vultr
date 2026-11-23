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

package spec

import (
	"fmt"
	"strconv"
	"strings"
)

// Prefixes accepted in a pool image field. A bare number is read as an
// operating system id, which is the common case.
const (
	osIDPrefix      = "os_id:"
	snapshotIDPref  = "snapshot_id:"
	appIDPrefix     = "app_id:"
	imageIDPrefix   = "image_id:"
	imageFieldUsage = `image must be a Vultr os id (for example 1743), or one of os_id:<id>, snapshot_id:<uuid>, app_id:<id> or image_id:<slug>`
)

// BootImage is what a Vultr instance boots from. The Vultr API accepts
// exactly one of these per instance, so exactly one field is ever set.
type BootImage struct {
	OsID       int
	SnapshotID string
	AppID      int
	ImageID    string
}

// ParseBootImage reads a pool image field.
//
// GARM carries a single opaque image string, while the Vultr API offers four
// different ways to pick what an instance boots. A bare number is an operating
// system id; the prefixed forms select a snapshot, a one-click application or
// a marketplace image.
func ParseBootImage(image string) (BootImage, error) {
	trimmed := strings.TrimSpace(image)
	if trimmed == "" {
		return BootImage{}, fmt.Errorf("image is required: %s", imageFieldUsage)
	}

	switch {
	case strings.HasPrefix(trimmed, osIDPrefix):
		osID, err := parsePositiveInt(strings.TrimPrefix(trimmed, osIDPrefix))
		if err != nil {
			return BootImage{}, fmt.Errorf("parsing os id from %q: %w", image, err)
		}
		return BootImage{OsID: osID}, nil

	case strings.HasPrefix(trimmed, snapshotIDPref):
		snapshotID := strings.TrimSpace(strings.TrimPrefix(trimmed, snapshotIDPref))
		if snapshotID == "" {
			return BootImage{}, fmt.Errorf("snapshot id is empty in %q", image)
		}
		return BootImage{SnapshotID: snapshotID}, nil

	case strings.HasPrefix(trimmed, appIDPrefix):
		appID, err := parsePositiveInt(strings.TrimPrefix(trimmed, appIDPrefix))
		if err != nil {
			return BootImage{}, fmt.Errorf("parsing app id from %q: %w", image, err)
		}
		return BootImage{AppID: appID}, nil

	case strings.HasPrefix(trimmed, imageIDPrefix):
		imageID := strings.TrimSpace(strings.TrimPrefix(trimmed, imageIDPrefix))
		if imageID == "" {
			return BootImage{}, fmt.Errorf("image id is empty in %q", image)
		}
		return BootImage{ImageID: imageID}, nil

	default:
		osID, err := parsePositiveInt(trimmed)
		if err != nil {
			return BootImage{}, fmt.Errorf("%s; got %q", imageFieldUsage, image)
		}
		return BootImage{OsID: osID}, nil
	}
}

// Validate checks that the image selects exactly one boot source.
func (b BootImage) Validate() error {
	set := 0
	if b.OsID != 0 {
		set++
	}
	if b.SnapshotID != "" {
		set++
	}
	if b.AppID != 0 {
		set++
	}
	if b.ImageID != "" {
		set++
	}

	switch set {
	case 1:
		return nil
	case 0:
		return fmt.Errorf("no boot source selected: %s", imageFieldUsage)
	default:
		return fmt.Errorf("only one boot source may be selected: %s", imageFieldUsage)
	}
}

// parsePositiveInt parses a Vultr numeric id, which is always positive.
func parsePositiveInt(value string) (int, error) {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("%q is not a number", strings.TrimSpace(value))
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("%d is not a valid id", parsed)
	}
	return parsed, nil
}
