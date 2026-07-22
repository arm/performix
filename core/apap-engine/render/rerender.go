// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

var ErrRenderFSRunNotFound = errors.New("render fs run builder not found")
var ErrRenderTargetNotFound = errors.New("render target not found")

// ErrRenderNoMatches indicates a globbed render source produced no matches.
var ErrRenderNoMatches = errors.New("render glob has no matches")

// SessionRenderFS provides a session-scoped interface for creating and managing render filesystems.
type SessionRenderFS interface {
	EmitOutputForRun(runID run.RunID, filePath string, rendererRelPath string, meta OutputMetadata) error
	EmitPendingOutputForRun(runID run.RunID, rendererRelPath string, meta OutputMetadata) error
	RemoveRenderForRun(runID run.RunID) error
	CreateTempDirForRun(runID run.RunID) (string, error)
	Cleanup() error
}

type SessionRenderFSImpl struct {
	ctx         context.Context
	builders    map[run.RunID]*run.RunRenderFS
	targets     map[run.RunID]*RenderTarget
	renderLocks map[run.RunID]func() error
}

// RenderTarget provides the minimal data needed to place render artifacts for a run:
// the render ID and the in‑memory manifest to update.
type RenderTarget struct {
	renderID    string
	renderModel *cdf.OnDiskModel
}

// NewRenderTarget constructs a RenderTarget.
func NewRenderTarget(renderID string, renderModel *cdf.OnDiskModel) *RenderTarget {
	return &RenderTarget{
		renderID:    renderID,
		renderModel: renderModel,
	}
}

func (t *RenderTarget) RenderID() string {
	return t.renderID
}

func (t *RenderTarget) RenderModel() *cdf.OnDiskModel {
	return t.renderModel
}

// NewSessionRenderFS constructs a render filesystem for a render session.
func NewSessionRenderFS(
	ctx context.Context,
	builders map[run.RunID]*run.RunRenderFS,
	targets map[run.RunID]*RenderTarget,
) *SessionRenderFSImpl {
	return &SessionRenderFSImpl{
		ctx:         ctx,
		builders:    builders,
		targets:     targets,
		renderLocks: make(map[run.RunID]func() error),
	}
}

// EmitOutput publishes a file from the tool with metadata.
type OutputMetadata struct {
	ComponentType string `json:"name"`
	Version       string `json:"version"`
}

// EmitOutput will move the files from the temporary render dir to the final destination and update the render manifest.
// Accepts the filePath as either an absolute path, or a path relative to the temp dir within the render dir.
func (fs *SessionRenderFSImpl) EmitOutputForRun(
	runID run.RunID,
	filePath string,
	rendererRelPath string,
	meta OutputMetadata,
) error {
	builder, ok := fs.builders[runID]
	if !ok {
		return ErrRenderFSRunNotFound
	}
	target, ok := fs.targets[runID]
	if !ok {
		return ErrRenderTargetNotFound
	}

	if pathUsesGlob(filePath) || pathUsesGlob(rendererRelPath) {
		return fs.emitOutputForRunGlob(builder, target, runID, filePath, rendererRelPath, meta)
	}

	if err := builder.MoveTempFile(fs.ctx, target.RenderID(), filePath, rendererRelPath); err != nil {
		return err
	}

	entryPath := cdf.NormalizePath(rendererRelPath)
	componentType := cdf.ComponentType{Name: meta.ComponentType, SchemaVersion: meta.Version}
	manifestEntry := cdf.ManifestEntry{
		Path:          entryPath,
		ComponentType: componentType,
	}
	target.RenderModel().AddEntry(manifestEntry)

	return nil
}

// EmitPendingOutputForRun adds a pending entry to the render manifest. It does not move any files to the specified path.
func (fs *SessionRenderFSImpl) EmitPendingOutputForRun(
	runID run.RunID,
	rendererRelPath string,
	meta OutputMetadata,
) error {
	target, ok := fs.targets[runID]
	if !ok {
		return ErrRenderTargetNotFound
	}

	entryPath := cdf.NormalizePath(rendererRelPath)
	componentType := cdf.ComponentType{Name: meta.ComponentType, SchemaVersion: meta.Version}
	manifestEntry := cdf.ManifestEntry{
		Path:          entryPath,
		ComponentType: componentType,
		Pending:       true,
	}
	target.RenderModel().AddEntry(manifestEntry)

	return nil
}

// pathUsesGlob returns true when the supplied path contains a wildcard token.
func pathUsesGlob(path string) bool {
	return strings.Contains(path, "*")
}

// emitOutputForRunGlob expands the file path and moves all matching files.
func (fs *SessionRenderFSImpl) emitOutputForRunGlob(
	builder *run.RunRenderFS,
	target *RenderTarget,
	runID run.RunID,
	filePath string,
	rendererRelPath string,
	meta OutputMetadata,
) error {
	// Ensure the temp render directory exists so glob expansion is scoped correctly.
	tempDir, err := fs.CreateTempDirForRun(runID)
	if err != nil {
		return err
	}

	// Resolve the glob pattern to an absolute path rooted in the temp directory.
	pattern := filePath
	remapBase := filePath
	remapRelativeToTempDir := false
	if !filepath.IsAbs(pattern) && filepath.VolumeName(pattern) == "" {
		pattern = filepath.Join(tempDir, filepath.FromSlash(pattern))
		remapRelativeToTempDir = true
	}

	// Expand the glob and ensure at least one match exists.
	matches, err := doublestar.Glob(pattern)
	if err != nil {
		return err
	}
	if len(matches) == 0 {
		return ErrRenderNoMatches
	}

	// Move each matched file into the render overlay using a remapped destination path.
	for _, match := range matches {
		remapMatch := match
		if remapRelativeToTempDir {
			remapMatch, err = filepath.Rel(tempDir, match)
			if err != nil {
				return err
			}
		}

		destRelPath, err := util.RemapGlobbedPath(rendererRelPath, remapMatch, remapBase)
		if err != nil {
			return err
		}
		if err := builder.MoveTempFile(fs.ctx, target.RenderID(), match, destRelPath); err != nil {
			return err
		}
	}

	// Register a single manifest entry using the wildcard path.
	entryPath := cdf.NormalizePath(rendererRelPath)
	componentType := cdf.ComponentType{Name: meta.ComponentType, SchemaVersion: meta.Version}
	manifestEntry := cdf.ManifestEntry{
		Path:          entryPath,
		ComponentType: componentType,
	}
	target.RenderModel().AddEntry(manifestEntry)

	return nil
}

// CreateTempDirForRun creates a temporary directory for rendering for the specified run.
// Returns the absolute path to the temporary directory that can be used for staging render outputs before they are emitted.
func (fs *SessionRenderFSImpl) CreateTempDirForRun(runID run.RunID) (string, error) {
	builder, ok := fs.builders[runID]
	if !ok {
		return "", ErrRenderFSRunNotFound
	}
	target, ok := fs.targets[runID]
	if !ok {
		return "", ErrRenderTargetNotFound
	}

	// Acquire and hold the rerender lock on first use for this run.
	// If we already hold it for this session, CreateTempRerenderDir is idempotent
	// (it uses MkdirAll), so repeated calls safely return the same path.
	if _, locked := fs.renderLocks[runID]; !locked {
		unlock, acquired, err := builder.TryLockRender(target.RenderID())
		if err != nil {
			return "", err
		}
		if !acquired {
			return "", errors.New("render entity is locked")
		}

		tempDir, err := builder.CreateTempRenderDir(fs.ctx, target.RenderID())
		if err != nil {
			_ = unlock()
			return "", err
		}
		fs.renderLocks[runID] = unlock
		return tempDir, nil
	}

	return builder.CreateTempRenderDir(fs.ctx, target.RenderID())
}

// RemoveRenderForRun removes the render directory for the specified run.
func (fs *SessionRenderFSImpl) RemoveRenderForRun(runID run.RunID) error {
	builder, ok := fs.builders[runID]
	if !ok {
		return ErrRenderFSRunNotFound
	}
	target, ok := fs.targets[runID]
	if !ok {
		return ErrRenderTargetNotFound
	}
	// Release and delete any lock held for this rerender
	if unlock, ok := fs.renderLocks[runID]; ok {
		if err := builder.RemoveRenderDir(fs.ctx, target.RenderID()); err != nil {
			return err
		}
		if err := unlock(); err != nil {
			return err
		}
		_ = builder.RemoveRenderLock(target.RenderID())
		delete(fs.renderLocks, runID)
		return nil
	}
	return builder.RemoveRenderDir(fs.ctx, target.RenderID())
}

// Cleanup removes any render directories created during the session.
func (fs *SessionRenderFSImpl) Cleanup() error {
	var cleanupErr error
	for runID := range fs.targets {
		if err := fs.RemoveRenderForRun(runID); err != nil && cleanupErr == nil {
			cleanupErr = err
		}
	}
	return cleanupErr
}
