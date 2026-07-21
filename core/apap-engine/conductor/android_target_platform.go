// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package conductor

import (
	"errors"
	"fmt"
	"path/filepath"

	log "github.com/sirupsen/logrus"
)

const DefaultAndroidTempDir = "/data/local/tmp"

const androidAdminUnsupportedError = "admin commands are not supported on Android targets"

type AndroidPathUtils struct {
}

// GenerateCommandLineWithEnv will generate a new CLI with the env prefixed.
// If env is empty, then it returns the CLI unchanged.
func (p *AndroidPathUtils) GenerateCommandLineWithEnv(cmd string, env EnvVar) string {
	if env.Name == "" {
		return cmd
	}
	return fmt.Sprintf("%s=%s %s", env.Name, env.Value, cmd)
}

// GetScriptExtension returns the default script extension for Android targets.
func (p *AndroidPathUtils) GetScriptExtension() string {
	return "sh"
}

// GenerateRunScriptCommand returns a command line that will run the specified script file in the specified working directory.
func (p *AndroidPathUtils) GenerateRunScriptCommand(scriptFileName string, workingDir string) string {
	cmd := fmt.Sprintf("./%s", scriptFileName)
	if workingDir != "" {
		return p.GenerateChdirCommandLine(workingDir, cmd)
	}
	return cmd
}

// ToOSPath converts a normalized path to the OS-specific format.
func (p *AndroidPathUtils) ToOSPath(path string) string {
	return filepath.ToSlash(path)
}

// IsAbs returns true if the path is absolute. The path is assumed normalized via path.ToSlash.
func (p *AndroidPathUtils) IsAbs(path string) bool {
	if path == "" {
		return false
	}
	return path[0] == '/'
}

// GetEnvPathSep returns the path separator for environment variables.
func (p *AndroidPathUtils) GetEnvPathSep() string {
	return ":"
}

// GenerateChdirCommandLine builds a command that changes dir then runs cmd.
func (p *AndroidPathUtils) GenerateChdirCommandLine(pwd string, cmd string) string {
	return fmt.Sprintf("cd %s && %s", p.FormatPathForShell(pwd), cmd)
}

// Wrappers for common functionality implemented in target_platform.go.
func (p *AndroidPathUtils) GetFullPath(dir string, pwd string) string {
	return getFullPath(p, dir, pwd)
}

func (p *AndroidPathUtils) GetPathEnvFromVenv(venv string, pwd string) EnvVar {
	return getPathEnvFromVenv(p, venv, pwd)
}

func (p *AndroidPathUtils) FormatPathForShell(path string) string {
	return formatPathForShell(p, path)
}

func (p *AndroidPathUtils) GetVenvBinDir() string {
	return "bin"
}

// AndroidTargetActions implements TargetActions.
type AndroidTargetActions struct {
	BaseTargetActions
}

func (p *AndroidTargetActions) RemoveDir(dir string) error {
	// the dir path is expected to have been sanitised before
	rmDirCmd := fmt.Sprintf("rm -rf %s", dir)
	_, stderr, err := p.CmdRunner.RunCommand(rmDirCmd)
	if err != nil {
		log.WithError(err).Errorf("failed to run %s - output directory not removed: %s", rmDirCmd, stderr)
		return err
	}
	return nil
}

// RunCommandAsAdmin is not implemented for Android targets yet.
func (p *AndroidTargetActions) RunCommandAsAdmin(cmd string) (RunCommandOutput, error) {
	// TODO(android-root): Implement Android admin command execution.
	return RunCommandOutput{}, errors.New(androidAdminUnsupportedError)
}

func (p *AndroidTargetActions) HasAdminPerms() (bool, error) {
	// TODO(android-root): Implement Android admin permission detection.
	return false, errors.New(androidAdminUnsupportedError)
}

func NewAndroidTargetPlatform(cmdRunner CommandRunner, fs TargetFilesystem) *TargetPlatform {
	return &TargetPlatform{
		PlatformConfiguration: PlatformConfiguration{Architecture: AArch64, OS: Android},
		Path:                  &AndroidPathUtils{},
		Actions:               &AndroidTargetActions{BaseTargetActions{CmdRunner: cmdRunner, FS: fs}},
	}
}
