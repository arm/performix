// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gofrs/flock"
	"github.com/google/uuid"

	"github.com/Arm-Debug/apap-cli/apap-engine/logging/logx"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
)

const renderDirName = "render"

// ErrRenderTempFileNotFound indicates a missing temp file during a render move.
var ErrRenderTempFileNotFound = errors.New("render temp file not found")

// RenderPath builds the render directory path for a render ID.
func RenderPath(renderID string) string {
	return filepath.Join(renderDirName, renderID)
}

// RunRenderFS manages render filesystem operations for a specific run.
type RunRenderFS struct {
	runID         RunID
	runCollection *RunCollection
}

// GenerateNewRenderID generates a unique render ID for a run.
func (b *RunRenderFS) GenerateNewRenderID() (string, error) {
	// Returns a unique ID with low collision probability
	return newRenderHash(), nil
}

// CreateTempRenderDir creates a temp dir in <run_dir>/render/<unique_id>/temp
func (b *RunRenderFS) CreateTempRenderDir(ctx context.Context, renderID string) (string, error) {
	if renderID == "" {
		return "", fmt.Errorf("render ID is empty")
	}
	unlock, err := b.runCollection.LockRun(ctx, b.runID)
	if err != nil {
		return "", err
	}
	defer func() { _ = unlock() }()

	absPath := filepath.Join(b.runCollection.GetRunPath(b.runID), RenderPath(renderID), "temp")
	err = os.MkdirAll(absPath, perms.LocalDirPerm)
	if err != nil {
		return "", err
	}
	return absPath, nil
}

// RemoveRenderDir removes the render directory for a given render ID, if it exists.
// Run lock is acquired to ensure safe deletion
// Render lock is not held - it is the caller's responsibility to ensure no active locks on the render ID before calling this method.
func (b *RunRenderFS) RemoveRenderDir(ctx context.Context, renderID string) error {
	if renderID == "" {
		return fmt.Errorf("render ID is empty")
	}
	// Check if render root dir exists, if not, don't attempt to acquire the run lock.
	renderRoot := filepath.Join(b.runCollection.GetRunPath(b.runID), renderDirName)
	if _, err := os.Stat(renderRoot); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	unlock, err := b.runCollection.LockRun(ctx, b.runID)
	if err != nil {
		return err
	}
	defer func() { _ = unlock() }()

	absPath := filepath.Join(b.runCollection.GetRunPath(b.runID), RenderPath(renderID))
	if err := os.RemoveAll(absPath); err != nil {
		return err
	}
	return nil
}

// MoveTempFile moves a temp file into the render directory.
// Accepts the tempFile as either an absolute path, or a path relative to the temp dir within the render dir.
func (b *RunRenderFS) MoveTempFile(ctx context.Context, renderID string, tempFile string, destRelPath string) error {
	if renderID == "" {
		return fmt.Errorf("render ID is empty")
	}
	unlock, err := b.runCollection.LockRun(ctx, b.runID)
	if err != nil {
		return err
	}
	defer func() { _ = unlock() }()

	basePath := b.runCollection.GetRunPath(b.runID)
	renderPath := RenderPath(renderID)
	tempRoot := filepath.Join(basePath, filepath.FromSlash(renderPath), "temp")
	// Normalize temp path to a safe relative path under tempRoot.
	cleanTempFile, err := normalizeTempFilePath(tempFile, tempRoot)
	if err != nil {
		return err
	}
	cleanDestPath, err := cleanRelativePath(destRelPath)
	if err != nil {
		return err
	}

	srcPath := filepath.Join(tempRoot, cleanTempFile)
	destPath := filepath.Join(basePath, filepath.FromSlash(renderPath), cleanDestPath)

	if _, err := os.Stat(srcPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%w: %s", ErrRenderTempFileNotFound, srcPath)
		}
		return err
	}

	if _, err := os.Stat(destPath); err == nil {
		return fmt.Errorf("render destination already exists: %s", destPath)
	} else if !os.IsNotExist(err) {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(destPath), perms.LocalDirPerm); err != nil {
		return err
	}
	// Move the file from temp to final destination, within the same file system.
	logx.FromContext(ctx).Debugf("Moving render file from temp path %s to final destination %s", srcPath, destPath)
	return os.Rename(srcPath, destPath)
}

// ListRenderDirs lists all render dirs in this run.
// Outputs only the render directory names (excluding the render root dir)
func (b *RunRenderFS) ListRenderDirs(ctx context.Context) ([]string, error) {
	// If the render root is missing, return early
	renderRoot := filepath.Join(b.runCollection.GetRunPath(b.runID), renderDirName)
	if _, err := os.Stat(renderRoot); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	unlock, err := b.runCollection.RLockRun(ctx, b.runID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = unlock() }()

	dirEntries, err := os.ReadDir(renderRoot)
	if err != nil {
		return nil, err
	}

	renderDirs := []string{}
	for _, entry := range dirEntries {
		if !entry.IsDir() {
			return nil, fmt.Errorf("unexpected non-directory entry in render dir: %s", entry.Name())
		}
		renderDirs = append(renderDirs, entry.Name())
	}
	return renderDirs, nil
}

// CleanupStaleRenders removes render dirs that are not locked and cleans up orphan lockfiles.
// A render directory is considered stale if its lockfile is missing or can be locked.
// Orphan lockfiles (no matching render dir) are removed best-effort.
//
// NOTE: It is assumed that once a flock (file lock) is held by this process, any subsequent lock attempts
// (via new fds) will fail. In other words, if a render session is active within this process, we should
// fail to acquire the lock corresponding to that session and skip cleanup.
// From current understanding, this is true for the flock implementation we use across Windows, macOS (BSD) and Linux.
func (b *RunRenderFS) CleanupStaleRenders(ctx context.Context) error {
	renderDirs, err := b.ListRenderDirs(ctx)
	if err != nil {
		return err
	}

	renderSet := make(map[string]struct{}, len(renderDirs))
	for _, id := range renderDirs {
		renderSet[id] = struct{}{}
		lockPath := b.renderLockPath(id)
		if _, err := os.Stat(lockPath); err != nil {
			if os.IsNotExist(err) {
				if rmErr := b.RemoveRenderDir(ctx, id); rmErr != nil {
					return rmErr
				}
				continue
			}
			return err
		}

		unlock, acquired, lockErr := b.TryLockRender(id)
		// If we couldn't check the lock state, be conservative and skip cleanup.
		if lockErr != nil {
			continue
		}
		if !acquired {
			// Other process holds the lock, so this render is active. Skip cleanup.
			continue
		}
		if rmErr := b.RemoveRenderDir(ctx, id); rmErr != nil {
			_ = unlock()
			return rmErr
		}
		_ = unlock()
		_ = os.Remove(b.renderLockPath(id))
	}

	// Remove orphan lockfiles that don't have a matching render dir. This is best-effort.
	lockDir := b.renderLockDir()
	lockDirInfo, err := os.Stat(lockDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !lockDirInfo.IsDir() {
		return fmt.Errorf("render lock path is not a directory: %s", lockDir)
	}

	lockEntries, err := os.ReadDir(lockDir)
	if err != nil {
		return err
	}

	for _, entry := range lockEntries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if _, exists := renderSet[name]; exists {
			continue
		}
		// Attempt a non-blocking lock to ensure we don't delete an active lockfile.
		lock := flock.New(filepath.Join(lockDir, name))
		locked, err := lock.TryLock()
		if err != nil || !locked {
			continue
		}
		_ = lock.Unlock()
		_ = os.Remove(filepath.Join(lockDir, name))
	}

	return nil
}

// renderLockDir returns the directory holding render lockfiles for this run.
func (b *RunRenderFS) renderLockDir() string {
	return filepath.Join(b.runCollection.getLockDir(b.runID), "render")
}

// renderLockPath returns the lockfile path for a rerender entity ID.
func (b *RunRenderFS) renderLockPath(renderID string) string {
	return filepath.Join(b.renderLockDir(), renderID)
}

// RemoveRenderLock removes the lockfile for a render entity ID.
func (b *RunRenderFS) RemoveRenderLock(renderID string) error {
	return os.Remove(b.renderLockPath(renderID))
}

// newRenderLock creates a file lock handle for a rerender entity ID.
func (b *RunRenderFS) newRenderLock(rerenderID string) (*flock.Flock, error) {
	lockDir := b.renderLockDir()
	if err := os.MkdirAll(lockDir, perms.LocalDirPerm); err != nil {
		return nil, fmt.Errorf("failed to create render lock dir: %w", err)
	}
	return flock.New(b.renderLockPath(rerenderID)), nil
}

// TryLockRender attempts to acquire a lock for a rerender entity ID.
func (b *RunRenderFS) TryLockRender(renderID string) (unlock func() error, locked bool, err error) {
	lock, err := b.newRenderLock(renderID)
	if err != nil {
		return nil, false, err
	}

	locked, err = lock.TryLock()
	if err != nil || !locked {
		return nil, false, err
	}

	unlock = func() error {
		return lock.Unlock()
	}
	return unlock, true, nil
}

// newRenderHash generates the short hash used in render directory names.
func newRenderHash() string {
	return uuid.New().String()
}

// cleanRelativePath ensures the provided path is a clean relative path that does not escape its base directory.
func cleanRelativePath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("path is empty")
	}
	cleaned := filepath.Clean(filepath.FromSlash(path))
	if filepath.IsAbs(cleaned) || filepath.VolumeName(cleaned) != "" {
		return "", fmt.Errorf("path must be relative: %s", path)
	}
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes render directory: %s", path)
	}
	return cleaned, nil
}

// normalizeTempFilePath accepts either a relative path inside the temp directory
// or an absolute path rooted within tempRoot, returning a clean relative path.
func normalizeTempFilePath(tempFile string, tempRoot string) (string, error) {
	if tempFile == "" {
		return "", fmt.Errorf("path is empty")
	}

	cleaned := filepath.Clean(filepath.FromSlash(tempFile))
	if filepath.IsAbs(cleaned) || filepath.VolumeName(cleaned) != "" {
		rel, err := filepath.Rel(filepath.Clean(tempRoot), cleaned)
		if err != nil {
			return "", fmt.Errorf("failed to relativize temp path: %w", err)
		}
		return cleanRelativePath(rel)
	}

	return cleanRelativePath(tempFile)
}
