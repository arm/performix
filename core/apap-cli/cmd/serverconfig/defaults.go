// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package serverconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spf13/viper"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/userdirs"
)

var Jobs = runtime.NumCPU()

const stdout = "stdout"

// DefaultServerHostname is the default listen address of the daemon gRPC server
// Use 127.0.0.1 instead of "localhost" as otherwise Linux may try to perform hostname resolution, the traffic for which
// may be routed over a real net interface. Resolution might be impossible for the current net setup.
// (at least, I think this is what's happening - not totally sure).
// Using 127.0.0.1 instead results in the loopback interface being selected (see output of `ip route get 127.0.0.1`)
const DefaultServerHostname = "127.0.0.1"
const DefaultServerPort = 9000
const DefaultAuthPort = 9001
const DefaultHTTPPort = 0
const DefaultHTTPChunkBytes = 1024 * 1024

const DefaultLogLevel = "info"
const DefaultLogFile = stdout
const DefaultADBPath = "adb"

const DefaultEnableOnDemandPrivilege = true
const DefaultAgentUseGroupController = true

const DefaultEnableFullCaptureSupport = true
const DefaultEnableRerendering = true
const DefaultEnableExperimentalRecipes = false
const DefaultEnableSecondaryRunPaths = true
const DefaultEnableTransferManager = true
const DefaultEnableAndroidTargets = false
const DefaultEnableRenderDBSandbox = true
const DefaultEnableNeoprofTimeline = false

const EnableAndroidTargetsConfigKey = "enable-android-targets"

const minPort = 1
const maxPort = 65535

func defaultDataDir() string {
	dir, err := userdirs.DataDir()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
	return dir
}

// defaultSrcToolsDirectory returns the absolute path to the tools directory,
// prefixing it with the path where the binary is being executed; this supports
// tool deployment when the binary is called from outside the directory that
// contains it (e.g. 'dir1/dir2/atperf --deploy-tools').
func defaultSrcToolsDirectory() string {
	execPath, err := os.Executable()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
	return filepath.Join(filepath.Dir(execPath), "tools")
}

var DefaultDataDir = defaultDataDir()

func defaultConfigDir() string {
	dir, err := userdirs.ConfigDir()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
	return dir
}

var DefaultConfigDir = defaultConfigDir()
var DefaultSrcToolsDirectory = defaultSrcToolsDirectory()

// DefaultToolsDeploymentDirectory is intentionally empty so the appropriate path gets resolved when connecting to the target.
var DefaultToolsDeploymentDirectory = ""

func init() {
	viper.SetDefault("server-port", DefaultServerPort)
	viper.SetDefault("auth-port", DefaultAuthPort)
}

func ValidatePort(name string, port int) error {
	if port < minPort || port > maxPort {
		return message.New(message.CliCmdValidationInvalidPortRange).WithMetadata(map[string]string{
			"name":     name,
			"min":      fmt.Sprint(minPort),
			"max":      fmt.Sprint(maxPort),
			"provided": fmt.Sprint(port),
		})
	}
	return nil
}
