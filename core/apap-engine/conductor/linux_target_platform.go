// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package conductor

import (
	"fmt"
	"path/filepath"

	log "github.com/sirupsen/logrus"
)

type LinuxPathUtils struct {
}

// GenerateCommandLineWithEnv will generate a new CLI with the env prefixed
// If env is empty, then it returns the CLI unchanged.
func (p *LinuxPathUtils) GenerateCommandLineWithEnv(cmd string, env EnvVar) string {
	if env.Name == "" {
		return cmd
	}
	return fmt.Sprintf("%s=%s %s", env.Name, env.Value, cmd)
}

// GetScriptExtension returns the default script exension for Linux targets
func (p *LinuxPathUtils) GetScriptExtension() string {
	return "sh"
}

// GenerateRunScriptCommand returns a command line that will run the specified script file in the specified working directory
func (p *LinuxPathUtils) GenerateRunScriptCommand(scriptFileName string, workingDir string) string {
	cmd := fmt.Sprintf("./%s", scriptFileName)
	if workingDir != "" {
		return p.GenerateChdirCommandLine(workingDir, cmd)
	}
	return cmd
}

// ToOSPath converts a normalized path to the OS-specific format - by default
// that will be equivalent to the normalized path.
func (p *LinuxPathUtils) ToOSPath(path string) string {
	return filepath.ToSlash(path)
}

// isAbs returns true if the path is absolute - the path is assumed normalized via path.ToSlash
func (p *LinuxPathUtils) IsAbs(path string) bool {
	if path == "" {
		return false
	}
	return path[0] == '/'
}

// getEnvPathSep returns the path separator for environment variables
func (p *LinuxPathUtils) GetEnvPathSep() string {
	return ":"
}

// GenerateChdirCommandLine builds a command that changes dir then runs cmd.
func (p *LinuxPathUtils) GenerateChdirCommandLine(pwd string, cmd string) string {
	return fmt.Sprintf("cd %s && %s", p.FormatPathForShell(pwd), cmd)
}

// Wrappers for common functionality implemented in target_platform.go
func (p *LinuxPathUtils) GetFullPath(dir string, pwd string) string {
	return getFullPath(p, dir, pwd)
}

func (p *LinuxPathUtils) GetPathEnvFromVenv(venv string, pwd string) EnvVar {
	return getPathEnvFromVenv(p, venv, pwd)
}

func (p *LinuxPathUtils) FormatPathForShell(path string) string {
	return formatPathForShell(p, path)
}

func (p *LinuxPathUtils) GetVenvBinDir() string {
	return "bin"
}

// LinuxTargetActions implements TargetActions
type LinuxTargetActions struct {
	BaseTargetActions
}

func (p *LinuxTargetActions) RemoveDir(dir string) error {
	// the dir path is expected to have been sanitised before
	rmDirCmd := fmt.Sprintf("rm -rf %s", dir)
	_, stderr, err := p.CmdRunner.RunCommand(rmDirCmd)
	if err != nil {
		log.WithError(err).Errorf("failed to run %s - output directory not removed: %s", rmDirCmd, stderr)
		return err
	}
	return nil
}

// RunCommandAsAdmin wraps the command in a "sudo bash", elevating to Admin privileges, then calls the RunCommand.
// This works as long as the target user is set up with passwordless sudo permissions.
func (p *LinuxTargetActions) RunCommandAsAdmin(cmd string) (RunCommandOutput, error) {
	sudoPrefix := "sudo bash -c"
	// Wrap the command in single quotes. The command may already contain double quotes for paths, as per FormatPathForShell
	cmd = fmt.Sprintf("%s '%s'", sudoPrefix, cmd)

	return p.RunCommand(cmd)
}

func (p *LinuxTargetActions) HasAdminPerms() (bool, error) {
	cmd := "sudo -n true"
	output, err := p.RunCommand(cmd)
	if err != nil {
		return false, err
	}
	return output.ReturnCode == 0, nil
}

func NewLinuxTargetPlatform(cmdRunner CommandRunner, fs TargetFilesystem, arch Architecture) *TargetPlatform {
	return &TargetPlatform{
		PlatformConfiguration: PlatformConfiguration{Architecture: arch, OS: Linux},
		Path:                  &LinuxPathUtils{},
		Actions:               &LinuxTargetActions{BaseTargetActions{CmdRunner: cmdRunner, FS: fs}},
	}
}
