// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package support

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/atperf-version/versions"
)

// formatSupportTimestamp formats a timestamp for support package names. It removes the T and Z designators from the
// timestamp to make it more user friendly, and adds a _ to separate the date and time.
func formatSupportTimestamp(t time.Time) string {
	return t.Format("2006-01-02_15-04-05")
}

// writeMetadataFile writes a metadata file to the given directory.
func writeMetadataFile(rootDir string, generatedAt time.Time) error {
	payload := map[string]string{
		"generated_at": generatedAt.Format(time.RFC3339),
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal support metadata: %w", err)
	}

	metadataPath := filepath.Join(rootDir, metadataFilename)
	if err := os.WriteFile(metadataPath, data, perms.LocalFilePerm); err != nil {
		return fmt.Errorf("failed to write support metadata: %w", err)
	}
	return nil
}

// writeVersionFile writes a file containing the engine and GUI versions to the given directory.
func writeVersionFile(rootDir, cliVersion, guiVersion string) error {
	payload := struct {
		CLIVersion    string `json:"cli_version"`
		EngineVersion string `json:"engine_version"`
		GUIVersion    string `json:"gui_version"`
	}{
		CLIVersion:    cliVersion,
		EngineVersion: versions.GetVersion(),
		GUIVersion:    guiVersion,
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal version data: %w", err)
	}

	versionPath := filepath.Join(rootDir, versionsFilename)
	if err := os.WriteFile(versionPath, data, perms.LocalFilePerm); err != nil {
		return fmt.Errorf("failed to write version file: %w", err)
	}
	return nil
}
