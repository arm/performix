// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package support

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/userdirs"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

var errNoLogs = errors.New("logs not found")

type candidateLog struct {
	path string
	mod  time.Time
}

// collectEngineLogs collects the configured engine log file and recent engine logs from the state directory.
func collectEngineLogs(ctx context.Context, logsRoot string, limit int, configuredLogFile string) error {
	engineDir := filepath.Join(logsRoot, engineLogsDir)
	if err := os.MkdirAll(engineDir, perms.LocalDirPerm); err != nil {
		return fmt.Errorf("failed to create engine logs directory %q: %w", engineDir, err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	logPaths := make([]string, 0, limit)
	seen := map[string]struct{}{}

	configuredLogFile = strings.TrimSpace(configuredLogFile)
	if configuredLogFile != "" && configuredLogFile != "stdout" {
		if info, err := os.Stat(configuredLogFile); err == nil && !info.IsDir() {
			cleanPath := filepath.Clean(configuredLogFile)
			logPaths = append(logPaths, cleanPath)
			seen[cleanPath] = struct{}{}
		} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("failed to access configured engine log file %q: %w", configuredLogFile, err)
		}
	}

	if limit <= 0 || len(logPaths) < limit {
		stateDir, err := userdirs.StateDir()
		if err != nil {
			return fmt.Errorf("failed to get state directory: %w", err)
		}

		recentPaths, err := recentLogFiles(stateDir, limit, ".log")
		if err != nil && len(logPaths) == 0 {
			return err
		} else if err != nil && !errors.Is(err, errNoLogs) {
			return err
		}

		for _, logPath := range recentPaths {
			cleanPath := filepath.Clean(logPath)
			if _, ok := seen[cleanPath]; ok {
				continue
			}
			logPaths = append(logPaths, cleanPath)
			seen[cleanPath] = struct{}{}
			if limit > 0 && len(logPaths) >= limit {
				break
			}
		}
	}

	if len(logPaths) == 0 {
		return errNoLogs
	}

	for _, logPath := range logPaths {
		dest := filepath.Join(engineDir, filepath.Base(logPath))
		if err := copyFile(ctx, logPath, dest); err != nil {
			return fmt.Errorf("failed to copy engine log file %q to %q: %w", logPath, dest, err)
		}
	}
	return nil
}

// collectGUILogs copies GUI log files from the provided directory into the support package.
func collectGUILogs(ctx context.Context, logsRoot string, limit int, guiLogDir string) error {
	guiDir := filepath.Join(logsRoot, guiLogsDir)
	if err := os.MkdirAll(guiDir, perms.LocalDirPerm); err != nil {
		return fmt.Errorf("failed to create GUI logs directory %q: %w", guiDir, err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	guiLogDir = strings.TrimSpace(guiLogDir)
	if guiLogDir == "" {
		return errNoLogs
	}

	info, err := os.Stat(guiLogDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return errNoLogs
		}
		return fmt.Errorf("failed to access GUI log directory %q: %w", guiLogDir, err)
	}

	var logPaths []string
	if info.IsDir() {
		logPaths, err = recentLogFiles(guiLogDir, limit, ".log")
		if err != nil {
			return err
		}
	} else {
		logPaths = []string{guiLogDir}
	}

	if len(logPaths) == 0 {
		return errNoLogs
	}

	for _, logPath := range logPaths {
		dest := filepath.Join(guiDir, filepath.Base(logPath))
		if err := copyFile(ctx, logPath, dest); err != nil {
			return fmt.Errorf("failed to copy GUI log file %q to %q: %w", logPath, dest, err)
		}
	}
	return nil
}

// recentLogFiles returns the most recent log files in the given directory.
func recentLogFiles(dir string, limit int, extension string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, errNoLogs
		}
		return nil, fmt.Errorf("failed to read recent logs directory %q: %w", dir, err)
	}

	files := make([]candidateLog, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if extension != "" {
			name := entry.Name()
			// Does this look like a log file? Also check for .old versions
			if !strings.HasSuffix(strings.ToLower(name), strings.ToLower(extension)) && !strings.Contains(strings.ToLower(name), strings.ToLower(extension)+".") {
				continue
			}
		}
		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("failed to get info for recent log file %q: %w", entry.Name(), err)
		}
		files = append(files, candidateLog{
			path: filepath.Join(dir, entry.Name()),
			mod:  info.ModTime(),
		})
	}

	if len(files) == 0 {
		return nil, errNoLogs
	}

	// Sort the files by last modified time, most recent first
	sort.Slice(files, func(i, j int) bool {
		return files[i].mod.After(files[j].mod)
	})

	if limit > 0 && limit < len(files) {
		files = files[:limit]
	}

	return util.MapI(files, func(i int) string { return files[i].path }), nil
}
