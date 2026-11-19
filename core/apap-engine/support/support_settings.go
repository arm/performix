// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package support

import (
	"context"
	"os"
	"path/filepath"

	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/settings"
)

// collectSettings copies the saved file as-is, including any invalid JSON that could help explain a problem.
func collectSettings(ctx context.Context, pkgRoot, configDir string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if configDir == "" {
		return nil
	}

	// Do not take the settings lock: collecting a support package should not wait for any ongoing settings updates.
	data, err := os.ReadFile(filepath.Join(configDir, settings.Filename))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	destDir := filepath.Join(pkgRoot, settingsDirName)
	if err := os.MkdirAll(destDir, perms.LocalDirPerm); err != nil {
		return err
	}
	// #nosec G703 -- The destination is inside the temporary support package; setting names are not used as paths.
	return os.WriteFile(filepath.Join(destDir, settings.Filename), data, perms.LocalFilePerm)
}
