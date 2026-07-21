// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package support

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

// collectRuns collects the runs from the runs directory by exporting them to the 'runs' subdirectory in the support package.
func collectRuns(ctx context.Context, runs *run.RunCollection, tempDir string, runIDs []run.RunID) error {
	runDir := filepath.Join(tempDir, runsDirName)
	if err := os.MkdirAll(runDir, perms.LocalDirPerm); err != nil {
		return fmt.Errorf("failed to create runs directory %q: %w", runDir, err)
	}

	for _, id := range runIDs {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := runs.ExportRun(ctx, id, runDir); err != nil {
			return err
		}
	}
	return nil
}

// collectRunSummaries writes a JSON snapshot of all known runs into the support package.
func collectRunSummaries(ctx context.Context, runs *run.RunCollection, pkgRoot string) error {
	summaries, err := runs.RunDescriptionsForExport(ctx)
	if err != nil {
		return err
	}

	runDir := filepath.Join(pkgRoot, runsDirName)
	if err := os.MkdirAll(runDir, perms.LocalDirPerm); err != nil {
		return err
	}

	data, err := json.MarshalIndent(map[string]any{"runs": summaries}, "", "  ")
	if err != nil {
		return err
	}

	dest := filepath.Join(runDir, runSummariesFile)
	return os.WriteFile(dest, data, perms.LocalFilePerm)
}
