// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package render

type RendererIdentity struct {
	Index int
	ID    *string
	Name  string
}

func (ri RendererIdentity) Equals(other RendererIdentity) bool {
	if ri.Index != other.Index {
		return false
	}
	if ri.Name != other.Name {
		return false
	}
	switch {
	case ri.ID == nil && other.ID == nil:
		return true
	case ri.ID == nil || other.ID == nil:
		return false
	default:
		return *ri.ID == *other.ID
	}
}

// Config is the configuration of a Renderer.
type Config struct {
	Identity RendererIdentity
	JSON     string
}
