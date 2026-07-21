// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package support

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
)

// collectTargets dumps out all targets from the target config to the 'target' subdirectory in the support package.
func collectTargets(pkgRoot string) error {
	cfg, err := target.NewDefaultTargetManager().ReadTargetConfig()
	if err != nil {
		return err
	}

	destDir := filepath.Join(pkgRoot, targetsDirName)
	if err := os.MkdirAll(destDir, perms.LocalDirPerm); err != nil {
		return err
	}

	payload := struct {
		SchemaVersion string                       `json:"schema_version,omitempty"`
		Default       string                       `json:"default"`
		Targets       map[string]target.JSONTarget `json:"targets"`
	}{
		SchemaVersion: cfg.SchemaVersion,
		Default:       cfg.Default,
		Targets:       make(map[string]target.JSONTarget, len(cfg.Targets)),
	}

	for name, tgt := range cfg.Targets {
		jsonTgt, convErr := target.JSONTargetFromEngine(tgt)
		if convErr != nil {
			return convErr
		}
		payload.Targets[name] = jsonTgt
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}

	dest := filepath.Join(destDir, target.DefaultTargetFilename)
	return os.WriteFile(dest, data, perms.LocalFilePerm)
}
