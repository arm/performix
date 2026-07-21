// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package support

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Arm-Debug/apap-cli/apap-engine/logging/logx"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

const (
	supportDirName    = "support"
	logsDirName       = "logs"
	engineLogsDir     = "engine"
	guiLogsDir        = "gui"
	runsDirName       = "runs"
	runSummariesFile  = "runs.json"
	systemDirName     = "system"
	targetsDirName    = "targets"
	hostInfoFilename  = "host_info.json"
	diskUsageFilename = "disk_usage.txt"
	versionsFilename  = "versions.json"
	supportPkgPrefix  = "support_pkg"
	metadataFilename  = "metadata.json"
	unknownValue      = "unknown"
)

// PackageOptions configure how a support package should be created.
type PackageOptions struct {
	RunIDs     []run.RunID
	OutputDir  string
	CLIVersion string
	GUIVersion string
	LogCount   int
	LogFile    string
	GUILogDir  string
}

// PackageResult contains metadata about a generated support package.
type PackageResult struct {
	PackagePath      string
	PackageSizeBytes int64
}

// CreateSupportPackage gathers logs and any specified runs into a support package. If the engine logs, GUI logs,
// target config file, or host info are missing, the support package will still be created as a partial support package.
// The resulting archive contains engine/GUI logs, optional run exports, target configuration, host info, disk usage,
// version metadata, and a generation timestamp under a support_pkg_<timestamp> root directory.
func CreateSupportPackage(ctx context.Context, opts PackageOptions, configValues map[string]any, runs *run.RunCollection) (*PackageResult, error) {
	cliVersion := strings.TrimSpace(opts.CLIVersion)
	if cliVersion == "" {
		cliVersion = unknownValue
	}

	guiVersion := strings.TrimSpace(opts.GUIVersion)
	if guiVersion == "" {
		guiVersion = unknownValue
	}

	// Validate the runs exist before creating the support package
	if err := runs.EnsureRunsExist(opts.RunIDs); err != nil {
		return nil, err
	}
	if err := message.CancellationError(ctx, nil); err != nil {
		return nil, err
	}

	generatedAt := time.Now().UTC()
	hostOpts := resolveHostOpts()

	tempBaseDir := opts.OutputDir
	if tempBaseDir == "" {
		tempBaseDir = os.TempDir()
	} else if err := os.MkdirAll(tempBaseDir, perms.LocalDirPerm); err != nil {
		mkdirError := fmt.Errorf("failed to create temporary output directory %q: %w", tempBaseDir, err)
		return nil, message.New(message.EngineSupportCollectFailed).WithCause(mkdirError)
	}

	tempParent, err := os.MkdirTemp(tempBaseDir, supportPkgPrefix+"-*")
	if err != nil {
		mkdirTmpError := fmt.Errorf("failed to create temporary directory for support package: %w", err)
		return nil, message.New(message.EngineSupportCollectFailed).WithCause(mkdirTmpError)
	}
	defer func() {
		if err := os.RemoveAll(tempParent); err != nil {
			logx.FromContext(ctx).Warnf("failed to clean temporary directory %q: %v", tempParent, err)
		}
	}()

	// Create a temporary directory for the support package and a subdirectory for the logs.
	pkgName := fmt.Sprintf("%s_%s", supportPkgPrefix, formatSupportTimestamp(generatedAt))
	pkgRoot := filepath.Join(tempParent, pkgName)
	logsRoot := filepath.Join(pkgRoot, logsDirName)
	if err := os.MkdirAll(logsRoot, perms.LocalDirPerm); err != nil {
		mkdirError := fmt.Errorf("failed to create logs directory %q: %w", logsRoot, err)
		return nil, message.New(message.EngineSupportCollectFailed).WithCause(mkdirError)
	}

	numLogs := max(1, opts.LogCount)

	// If engine logs are missing, just keep going as a partial support package might still be useful.
	if err := collectEngineLogs(ctx, logsRoot, numLogs, opts.LogFile); err != nil {
		if cancelErr := message.CancellationError(ctx, err); cancelErr != nil {
			return nil, cancelErr
		}
		if errors.Is(err, errNoLogs) {
			logx.FromContext(ctx).Warnf("failed to include engine logs in support package: %v", err)
		} else {
			return nil, err
		}
	}

	// If GUI logs are missing, just keep going as a partial support package might still be useful.
	if err := collectGUILogs(ctx, logsRoot, numLogs, opts.GUILogDir); err != nil {
		if cancelErr := message.CancellationError(ctx, err); cancelErr != nil {
			return nil, cancelErr
		}
		if errors.Is(err, errNoLogs) {
			logx.FromContext(ctx).Warnf("failed to include GUI logs in support package: %v", err)
		} else {
			return nil, err
		}
	}

	if len(opts.RunIDs) > 0 {
		if err := collectRuns(ctx, runs, pkgRoot, opts.RunIDs); err != nil {
			if cancelErr := message.CancellationError(ctx, err); cancelErr != nil {
				return nil, cancelErr
			}
			if message.IsMessage(err) != nil {
				return nil, err
			}
			return nil, message.New(message.EngineSupportCollectFailed).WithCause(err)
		}
	}
	if err := message.CancellationError(ctx, nil); err != nil {
		return nil, err
	}

	if err := collectRunSummaries(ctx, runs, pkgRoot); err != nil {
		logx.FromContext(ctx).Warnf("failed to include run summaries in support package: %v", err)
	}

	if err := collectTargets(pkgRoot); err != nil {
		logx.FromContext(ctx).Warnf("failed to include targets configuration in support package: %v", err)
	}

	if err := collectHostInfoWithOpts(ctx, configValues, pkgRoot, hostOpts); err != nil {
		logx.FromContext(ctx).Warnf("failed to include host information in support package: %v", err)
	}

	if err := writeMetadataFile(pkgRoot, generatedAt); err != nil {
		return nil, message.New(message.EngineSupportCollectFailed).WithCause(err)
	}

	if err := writeVersionFile(pkgRoot, cliVersion, guiVersion); err != nil {
		return nil, message.New(message.EngineSupportCollectFailed).WithCause(err)
	}

	packagePath, err := createArchive(ctx, pkgRoot, opts.OutputDir, pkgName)
	if err != nil {
		if cancelErr := message.CancellationError(ctx, err); cancelErr != nil {
			return nil, cancelErr
		}
		return nil, message.New(message.EngineSupportCollectFailed).WithCause(err)
	}

	size, err := util.FileSize(packagePath)
	if err != nil || size < 0 {
		return nil, message.New(message.EngineSupportCollectFailed).WithCause(err)
	}

	return &PackageResult{
		PackagePath:      packagePath,
		PackageSizeBytes: size,
	}, nil
}
