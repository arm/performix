// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package support

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Arm-Debug/apap-cli/apap-engine/logging/logx"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/userdirs"
	"github.com/Arm-Debug/apap-cli/atperf-agent/systeminfo"
)

// hostOpts encapsulates host-OS-specific helpers. All host-dependent logic should flow through this struct;
// the rest of this file stays platform-agnostic orchestration.
type hostOpts struct {
	diskUsage func(ctx context.Context) ([]byte, error)
}

// collectHostInfoWithOpts collects host information and writes it to the 'system' subdirectory in the package.
func collectHostInfoWithOpts(ctx context.Context, pkgConfigValues map[string]any, pkgRoot string, opts hostOpts) error {
	systemDir := filepath.Join(pkgRoot, systemDirName)
	if err := os.MkdirAll(systemDir, perms.LocalDirPerm); err != nil {
		return err
	}

	hostname, err := os.Hostname()
	if err != nil || strings.TrimSpace(hostname) == "" {
		hostname = unknownValue
	}

	currentUser, err := user.Current()
	username := unknownValue
	if err == nil && currentUser != nil && currentUser.Username != "" {
		username = currentUser.Username
	}

	kernel := strings.TrimSpace(getKernelDescription(ctx))
	if kernel == "" {
		kernel = unknownValue
	}

	payload := map[string]any{
		"hostname": hostname,
		"user":     username,
		"os":       runtime.GOOS,
		"arch":     runtime.GOARCH,
		"kernel":   kernel,
	}

	configValues := map[string]any{}
	if derived := pkgConfigValues; derived != nil {
		for key, value := range derived {
			configValues[key] = value
		}
	}
	if dataDir, _ := userdirs.DataDir(); dataDir != "" {
		if _, exists := configValues["data-dir"]; !exists {
			configValues["data-dir"] = dataDir
		}
	}
	if stateDir, _ := userdirs.StateDir(); stateDir != "" {
		if _, exists := configValues["state-dir"]; !exists {
			configValues["state-dir"] = stateDir
		}
	}
	if configDir, _ := userdirs.ConfigDir(); configDir != "" {
		if _, exists := configValues["config-dir"]; !exists {
			configValues["config-dir"] = configDir
		}
	}
	if len(configValues) > 0 {
		payload["config"] = configValues
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}

	hostInfoPath := filepath.Join(systemDir, hostInfoFilename)
	if err := os.WriteFile(hostInfoPath, data, perms.LocalFilePerm); err != nil {
		return err
	}

	if diskData, err := opts.diskUsage(ctx); err == nil {
		if len(diskData) > 0 && diskData[len(diskData)-1] != '\n' {
			diskData = append(diskData, '\n')
		}
		diskPath := filepath.Join(systemDir, diskUsageFilename)
		if err := os.WriteFile(diskPath, diskData, perms.LocalFilePerm); err != nil {
			return err
		}
	} else {
		logx.FromContext(ctx).Warnf("failed to collect disk usage for support package: %v", err)
	}

	return nil
}

func getKernelDescription(ctx context.Context) string {
	info := systeminfo.NewSystemInfo()
	kernel, err := info.GetKernelVersion()
	if err != nil {
		logx.FromContext(ctx).Warnf("failed to collect kernel version for support package: %v", err)
		return ""
	}
	kernel = strings.TrimSpace(kernel)
	if kernel == "" {
		return ""
	}

	if osDesc, err := info.GetOSDescription(); err == nil {
		if desc := strings.TrimSpace(osDesc); desc != "" {
			return fmt.Sprintf("%s (%s)", desc, kernel)
		}
	}
	return kernel
}

// defaultCommandRunner runs a command and returns its output.
func defaultCommandRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.CombinedOutput()
}
