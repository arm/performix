// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"path"
	"path/filepath"
	"sync"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
)

// RunManifestUpdater serializes manifest mutations.
// It owns the lock that protects RunBuilder changes and the matching manifest write.
type RunManifestUpdater struct {
	mu      sync.Mutex
	builder *RunBuilder
	writer  RunWriter
	// Retain declarations after removal: failed transfers may leave files behind.
	declarations []cdf.ManifestEntry
}

func NewRunManifestUpdater(builder *RunBuilder, writer RunWriter) *RunManifestUpdater {
	return &RunManifestUpdater{
		builder:      builder,
		writer:       writer,
		declarations: builder.buildManifest().Entries,
	}
}

// AddComponent adds a complete component and writes the updated manifest.
func (u *RunManifestUpdater) AddComponent(relativePath string, componentType cdf.ComponentType) error {
	return u.AddComponentWithFlags(relativePath, componentType, ComponentFlags{})
}

// AddComponentWithFlags adds a component with the supplied state and storage flags and writes the updated manifest.
func (u *RunManifestUpdater) AddComponentWithFlags(relativePath string, componentType cdf.ComponentType, flags ComponentFlags) error {
	if relativePath == "" {
		return nil
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	entry := cdf.ManifestEntry{Path: path.Clean(cdf.NormalizePath(relativePath)), Compressed: flags.Compressed}
	if err := u.validateComponentCompression(entry); err != nil {
		return err
	}
	if err := u.updateLocked(func(builder *RunBuilder) {
		builder.AddComponentWithFlags(componentType, relativePath, flags)
	}); err != nil {
		return err
	}
	u.declarations = append(u.declarations, entry)
	return nil
}

// validateComponentCompression rejects incompatible destinations before manifest insertion.
// It includes removed declarations because failed transfers may have left files behind.
// The caller must hold u.mu through validation, persistence, and declaration registration.
func (u *RunManifestUpdater) validateComponentCompression(entry cdf.ManifestEntry) error {
	for _, existing := range u.declarations {
		existing.Path = path.Clean(cdf.NormalizePath(existing.Path))
		if componentCompressionConflict(existing, entry) {
			return message.New(message.EngineRunComponentCompressionConflict).
				WithMetadata(map[string]string{"first": existing.Path, "second": entry.Path})
		}
	}
	return nil
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

	return u.updateLocked(mutate)
}

// updateLocked commits a mutation only after its manifest has been persisted.
func (u *RunManifestUpdater) updateLocked(mutate func(*RunBuilder)) error {
	updatedBuilder := u.builder.Clone()
	mutate(&updatedBuilder)
	if err := u.writer.WriteManifest(updatedBuilder); err != nil {
		return err
	}
	*u.builder = updatedBuilder
	return nil
}
