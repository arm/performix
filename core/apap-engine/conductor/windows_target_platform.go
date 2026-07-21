// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package conductor

import (
	"fmt"
	"path/filepath"
	"strings"

	log "github.com/sirupsen/logrus"
)

type WindowsPathUtils struct {
}

// GenerateCommandLineWithEnv will generate a new CLI with the env prefixed
// If env is empty, then it returns the CLI unchanged.
// Under Windows, this will leave the variable set during the lifetime of the shell
func (p *WindowsPathUtils) GenerateCommandLineWithEnv(cmd string, env EnvVar) string {
	cmd = p.ToOSPath(cmd)
	if env.Name == "" {
		return cmd
	}
	return fmt.Sprintf("cmd /C \"set %s=%s & %s\"", env.Name, env.Value, cmd)
}

// Returns the default script exension for Windows targets
func (p *WindowsPathUtils) GetScriptExtension() string {
	return "ps1"
}

// ToOSPath normalizes the path and converts it to the Windows
// format (by replacing the slashes)
func (p *WindowsPathUtils) ToOSPath(path string) string {
	norm := filepath.ToSlash(path)
	return strings.ReplaceAll(norm, "/", `\`)
}

// WindowsPathUtils returns a command line that will run the specified script file in the specified working directory
func (p *WindowsPathUtils) GenerateRunScriptCommand(scriptFileName string, workingDir string) string {
	cmd := fmt.Sprintf("powershell -NoProfile -WindowStyle Hidden -ExecutionPolicy Bypass -File %s", scriptFileName)
	if workingDir != "" {
		return p.GenerateChdirCommandLine(workingDir, cmd)
	}
	return cmd
}

// isAbs returns true if the path is absolute - the path is assumed normalized via path.ToSlash
func (p *WindowsPathUtils) IsAbs(path string) bool {
	if path == "" {
		return false
	}

	// 1) Any leading '/' is absolute on Windows:
	//    - UNC:  //server/share...
	//    - Root: /windows/system32
	if path[0] == '/' {
		return true
	}

	// 2) Drive-absolute: C:/...
	if len(path) >= 3 &&
		isASCIIAlpha(path[0]) &&
		path[1] == ':' &&
		path[2] == '/' {
		return true
	}

	return false
}

// GenerateChdirCommandLine builds a command that changes dir then runs cmd.
func (p *WindowsPathUtils) GenerateChdirCommandLine(pwd string, cmd string) string {
	return fmt.Sprintf("cd %s && %s", p.FormatPathForShell(pwd), cmd)
}

// Returns true if the byte is an ASCII alphabetic character
func isASCIIAlpha(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}

// GetEnvPathSep returns the path separator for environment variables
func (p *WindowsPathUtils) GetEnvPathSep() string {
	return ";"
}

// Wrappers for common functionality implemented in target_platform.go
func (p *WindowsPathUtils) GetFullPath(dir string, pwd string) string {
	return getFullPath(p, dir, pwd)
}

func (p *WindowsPathUtils) GetPathEnvFromVenv(venv string, pwd string) EnvVar {
	return getPathEnvFromVenv(p, venv, pwd)
}

func (p *WindowsPathUtils) FormatPathForShell(path string) string {
	return formatPathForShell(p, path)
}

func (p *WindowsPathUtils) GetVenvBinDir() string {
	return "Scripts"
}

// WindowsTargetActions implements TargetActions
type WindowsTargetActions struct {
	BaseTargetActions
}

func (p *WindowsTargetActions) RemoveDir(dir string) error {
	// the dir path is expected to have been sanitised before
	rmDirCmd := fmt.Sprintf("rmdir /s /q %s", dir)
	_, stderr, err := p.CmdRunner.RunCommand(rmDirCmd)
	if err != nil {
		log.WithError(err).Errorf("failed to run %s - output directory not removed: %s", rmDirCmd, stderr)
		return err
	}
	return nil
}

// Under Windows, there's no per command privilege elevation, so this is the same as RunCommand
func (p *WindowsTargetActions) RunCommandAsAdmin(cmd string) (RunCommandOutput, error) {
	return p.RunCommand(cmd)
}

// Under Windows, there's no per command privilege elevation, so we assume admin
func (p *WindowsTargetActions) HasAdminPerms() (bool, error) {
	return true, nil
}

func NewWindowsTargetPlatform(cmdRunner CommandRunner, fs TargetFilesystem, arch Architecture) *TargetPlatform {
	return &TargetPlatform{
		PlatformConfiguration: PlatformConfiguration{Architecture: arch, OS: Win},
		Path:                  &WindowsPathUtils{},
		Actions:               &WindowsTargetActions{BaseTargetActions{CmdRunner: cmdRunner, FS: fs}},
	}
}
