// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"path/filepath"
	"sync"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
)

// RunManifestUpdater serializes manifest mutations.
// It owns the lock that protects RunBuilder changes and the matching manifest write.
type RunManifestUpdater struct {
	mu      sync.Mutex
	builder *RunBuilder
	writer  RunWriter
}

func NewRunManifestUpdater(builder *RunBuilder, writer RunWriter) *RunManifestUpdater {
	return &RunManifestUpdater{builder: builder, writer: writer}
}

// AddPendingComponent adds a pending manifest entry. Duplicate paths are still added to the manifest.
func (u *RunManifestUpdater) AddPendingComponent(relativePath string, componentType cdf.ComponentType) error {
	if relativePath == "" {
		return nil
	}
	return u.update(func(builder *RunBuilder) {
		builder.AddPendingComponent(componentType, relativePath)
	})
}

// AddComponent adds a complete component and writes the updated manifest. Duplicate paths are still added to the manifest.
func (u *RunManifestUpdater) AddComponent(relativePath string, componentType cdf.ComponentType) error {
	if relativePath == "" {
		return nil
	}
	return u.update(func(builder *RunBuilder) {
		builder.AddComponent(componentType, relativePath)
	})
}

func (u *RunManifestUpdater) AddToolOutput(toolName, version string, invocation int) error {
	return u.update(func(builder *RunBuilder) {
		builder.AddToolOutput(toolName, version, invocation)
	})
}

// ComponentPath returns the absolute run path for a component (whether it exists or not).
func (u *RunManifestUpdater) ComponentPath(relativePath string) string {
	return filepath.Join(u.builder.runPath, cdf.NormalizePath(relativePath))
}

// ClearPending clears the pending flag on the first matching manifest path. It is a no-op if the path does not exist.
func (u *RunManifestUpdater) ClearPending(relativePath string) error {
	if relativePath == "" {
		return nil
	}
	return u.update(func(builder *RunBuilder) {
		builder.ClearPending(relativePath)
	})
}

// RemoveComponent deletes the first manifest entry with the matching path. It is a no-op if the path does not exist.
func (u *RunManifestUpdater) RemoveComponent(relativePath string) error {
	if relativePath == "" {
		return nil
	}
	return u.update(func(builder *RunBuilder) {
		builder.RemoveComponent(relativePath)
	})
}

// RemovePendingComponent deletes the first matching manifest path only if it is pending. It is a no-op if the path does not exist.
func (u *RunManifestUpdater) RemovePendingComponent(relativePath string) error {
	if relativePath == "" {
		return nil
	}
	return u.update(func(builder *RunBuilder) {
		if builder.IsComponentPending(relativePath) {
			builder.RemoveComponent(relativePath)
		}
	})
}

// WriteEntityDirs writes entity directories for the current builder state.
func (u *RunManifestUpdater) WriteEntityDirs() error {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.writer.WriteEntityDirs(*u.builder)
}

// update applies one live-run manifest mutation and persists the manifest.
func (u *RunManifestUpdater) update(mutate func(*RunBuilder)) error {
	u.mu.Lock()
	defer u.mu.Unlock()

	updatedBuilder := u.builder.Clone()
	mutate(&updatedBuilder)
	if err := u.writer.WriteManifest(updatedBuilder); err != nil {
		return err
	}
	*u.builder = updatedBuilder
	return nil
}
