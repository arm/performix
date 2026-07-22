// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package cdf

type ComponentResolver interface {
	ResolveComponent(relativePath string) (Component, error)
}

// ComponentType represents the type of a Component, in terms of name and version.
type ComponentType struct {
	Name          string `json:"name"`
	SchemaVersion string `json:"schema_version"`
}

// Component represents a Component within a OnDiskModel.
type Component struct {
	Type         ComponentType
	RelativePath string
	AbsolutePath string
}
