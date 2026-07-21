// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
)

const (
	recipeNamePrefixLength = 16
	runIDSuffixLength      = 3
	runNameSeparator       = "_"
)

// RunBuilder represents a run under construction.
type RunBuilder struct {
	runID      RunID              // The ID of the run we're building
	basePath   string             // Base path of run collection we're building an entry for
	runPath    string             // Path of the Run we're building
	entities   []cdf.Entity       // Entities that make up this Run
	components []builderComponent // Components tracked while the run is under construction.
	toolsUsed  []cdf.ToolUsed     // Describes the tools used when creating this run
}

// builderComponent is RunBuilder's internal component record. It contains the
// CDF component data that will be written to the run and the pending state.
type builderComponent struct {
	cdf.Component
	Pending bool
}

func (b *RunBuilder) RunID() RunID {
	return b.runID
}

// Clone returns an independent copy of the builder state.
func (b *RunBuilder) Clone() RunBuilder {
	clone := *b
	clone.entities = slices.Clone(b.entities)
	clone.components = slices.Clone(b.components)
	clone.toolsUsed = slices.Clone(b.toolsUsed)
	return clone
}

// GenerateRunName generates a run name based on the recipe name and the run ID.
func (b *RunBuilder) GenerateRunName(recipeName string) string {
	name := recipeName
	if len(name) > recipeNamePrefixLength {
		name = name[:recipeNamePrefixLength]
	}
	id := b.runID.Value
	if len(id) > runIDSuffixLength {
		id = id[len(id)-runIDSuffixLength:]
	}
	return name + runNameSeparator + id
}

// AddEntity adds a new Entity to a RunBuilder.
func (b *RunBuilder) AddEntity(relativePath string) {
	if len(relativePath) == 0 {
		return
	}

	relativePath = cdf.NormalizePath(relativePath)

	dirs := strings.Split(relativePath, "/")
	var dirPath string
	for d := range len(dirs) {
		if strings.ContainsAny(dirs[d], "*?[") {
			break
		}
		dirPath = filepath.Join(dirPath, dirs[d])
		if !b.ContainsEntity(dirPath) {
			b.entities = append(b.entities, cdf.Entity{RelativePath: cdf.NormalizePath(dirPath)})
		}
	}
}

func (b *RunBuilder) AddToolOutput(toolName, version string, invocation int) {
	b.toolsUsed = append(b.toolsUsed, cdf.ToolUsed{
		Tool:       toolName,
		Version:    version,
		Invocation: invocation,
	})
}

// ContainsEntity returns whether an Entity already exists in the
// RunBuilder with the specified relative path.
func (b *RunBuilder) ContainsEntity(relativePath string) bool {
	relativePath = cdf.NormalizePath(relativePath)
	return slices.Contains(b.entities, cdf.Entity{RelativePath: relativePath})
}

func (b *RunBuilder) EntityCount() int {
	return len(b.entities)
}

func (b *RunBuilder) ComponentCount() int { return len(b.components) }

// AddComponent records a complete component and returns its absolute path.
// Duplicate manifest paths are still added to the manifest.
func (b *RunBuilder) AddComponent(componentType cdf.ComponentType, relativePath string) string {
	return b.addComponent(componentType, relativePath, false)
}

// AddPendingComponent records a component that is expected but not complete yet and returns its absolute path.
// Duplicate manifest paths are still added to the manifest.
func (b *RunBuilder) AddPendingComponent(componentType cdf.ComponentType, relativePath string) string {
	return b.addComponent(componentType, relativePath, true)
}

// addComponent adds a component using the normalized manifest path.
func (b *RunBuilder) addComponent(componentType cdf.ComponentType, relativePath string, pending bool) string {
	relativePath = cdf.NormalizePath(relativePath)

	dir, _ := filepath.Split(relativePath)
	b.AddEntity(dir)

	absolutePath := filepath.Join(b.runPath, relativePath)
	b.components = append(b.components, builderComponent{
		Component: cdf.Component{Type: componentType, RelativePath: relativePath, AbsolutePath: absolutePath},
		Pending:   pending,
	})

	return absolutePath
}

// ClearPending clears the pending flag on the first matching manifest path that is already pending. It is a no-op if the path does not exist.
func (b *RunBuilder) ClearPending(relativePath string) {
	relativePath = cdf.NormalizePath(relativePath)
	for i := range b.components {
		if b.components[i].RelativePath == relativePath && b.components[i].Pending {
			b.components[i].Pending = false
			return
		}
	}
}

// RemoveComponent removes a component by manifest path. It is a no-op if the path does not exist.
func (b *RunBuilder) RemoveComponent(relativePath string) {
	relativePath = cdf.NormalizePath(relativePath)
	for i := range b.components {
		if b.components[i].RelativePath == relativePath {
			b.components = slices.Delete(b.components, i, i+1)
			return
		}
	}
}

// IsComponentPending returns whether a component exists at the manifest path and is pending.
func (b *RunBuilder) IsComponentPending(relativePath string) bool {
	relativePath = cdf.NormalizePath(relativePath)
	for i := range b.components {
		if b.components[i].RelativePath == relativePath {
			return b.components[i].Pending
		}
	}
	return false
}

// buildManifest builds the Manifest for the contents of this RunBuilder
func (b *RunBuilder) buildManifest() *cdf.Manifest {
	entries := make([]cdf.ManifestEntry, len(b.components))
	for i := range b.components {
		entries[i] = cdf.ManifestEntry{
			Path:          b.components[i].RelativePath,
			ComponentType: b.components[i].Type,
			Pending:       b.components[i].Pending,
		}
	}

	return &cdf.Manifest{
		Entries:   entries,
		ToolsUsed: b.toolsUsed,
	}
}
