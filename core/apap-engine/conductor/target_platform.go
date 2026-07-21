// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package conductor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	log "github.com/sirupsen/logrus"
)

// EnvVar describes an environment variable, as name-value pair
type EnvVar struct {
	Name  string
	Value string
}

type PathUtilities interface {
	GenerateChdirCommandLine(pwd string, cmd string) string
	GetPathEnvFromVenv(venv string, pwd string) EnvVar
	GetFullPath(dir string, pwd string) string
	ToOSPath(path string) string
	GenerateCommandLineWithEnv(cmd string, env EnvVar) string
	GenerateRunScriptCommand(scriptFileName string, workingDir string) string
	FormatPathForShell(path string) string
	GetScriptExtension() string
	IsAbs(string) bool
	GetEnvPathSep() string
	GetVenvBinDir() string
}

// TargetActions is an interface for performing target actions
type TargetActions interface {
	Stat(s string) (os.FileInfo, error)
	RemoveDir(dir string) error
	CommandRunner() CommandRunner // accessor method
	HasAdminPerms() (bool, error)
	RunCommand(cmd string) (RunCommandOutput, error)
	RunCommandAsAdmin(cmd string) (RunCommandOutput, error)
}

// TargetPlatform defines platform specific functionality (i.e for Windows, Linux, MacOs),
// such as crafting commands for changing dir.
type TargetPlatform struct {
	PlatformConfiguration
	Path    PathUtilities
	Actions TargetActions
}

// CreateVenvIfRequired will create a venv if one does not already exist at the venvPath location.
func CreateVenvIfRequired(platform *TargetPlatform, pwd string, venvPath string) error {
	if venvPath == "" {
		// venv field is not supplied
		return nil
	}
	// Check if the venv exists
	_, err := platform.Actions.Stat(platform.Path.GetFullPath(venvPath, pwd))
	if err != nil {
		// venv does not exist, create it here:
		cmdRunner := platform.Actions.CommandRunner()
		venvPath = platform.Path.FormatPathForShell(venvPath)
		createVenvCmd := platform.Path.GenerateChdirCommandLine(pwd, fmt.Sprintf("python3 -m venv %s", venvPath))
		_, stderr, err := cmdRunner.RunCommand(createVenvCmd)
		rc, err := ExtractRCFromError(err)
		if err != nil {
			return err
		}
		if rc != 0 {
			return fmt.Errorf("failed to create venv: %v", stderr)
		}
	}
	return nil
}

// GetFullPath returns the os specific absolute path, as follows:
// - if dirPath is already absolute, it returns as is.
// - if dirPath is a relative path, it is appended to pwd.
func getFullPath(p PathUtilities, dirPath string, pwd string) string {
	if dirPath == "" {
		return ""
	}
	if pwd != "" {
		// Normalize the working directory path
		pwd = filepath.ToSlash(pwd)
	}
	// First normalize the path
	dirPath = filepath.ToSlash(dirPath)

	// Convert to absolute path if the given path is a relative path.
	if !p.IsAbs(dirPath) {
		dirPath = filepath.Join(pwd, dirPath)
	}
	// Convert to the OS specific format
	return p.ToOSPath(dirPath)
}

// getPathEnvFromVenv returns an EnvVar for a PATH env which points to the venv.
// If venv is a relative path, then pwd will be appended to it. Otherwise the absolute venv
// path will be used.
func getPathEnvFromVenv(p PathUtilities, venv string, pwd string) EnvVar {
	if venv == "" {
		return EnvVar{}
	}

	if pwd != "" {
		pwd = filepath.ToSlash(pwd)
	}
	venv = filepath.ToSlash(venv)

	// Convert to absolute path if the given path is a relative path.
	if !p.IsAbs(venv) { // this works specifically with unix filepaths ("/")
		venv = filepath.Join(pwd, venv)
	}

	venvPath := filepath.Join(venv, p.GetVenvBinDir())
	newPathEnv := fmt.Sprintf("%s%s$PATH", venvPath, p.GetEnvPathSep())
	envVar := EnvVar{Name: "PATH", Value: p.FormatPathForShell(newPathEnv)}

	return envVar
}

// formatPathForShell prepares a path to be used in a shell command:
// - first it is converted to the OS specific format
// - then it is surrounded by quotes, to allow spaces within the path string.
//
// The function is idempotent --- it can be called multiple times on the same string.
func formatPathForShell(p PathUtilities, path string) string {
	newPath := p.ToOSPath(path)
	if strings.HasPrefix(newPath, "\"") && strings.HasSuffix(newPath, "\"") {
		return newPath
	}
	return fmt.Sprintf("\"%s\"", newPath)
}

// Base struct for <OS>TargetActions - contains common functionality but only children implement TargetActions
type BaseTargetActions struct {
	CmdRunner CommandRunner
	FS        TargetFilesystem
}

func (p *BaseTargetActions) CommandRunner() CommandRunner {
	return p.CmdRunner
}

func (p *BaseTargetActions) Stat(path string) (os.FileInfo, error) {
	return p.FS.FileStat(path)
}

// RunCommand runs a command on the target and returns a RunCommandOutput. This is a wrapper on top of CommandRunner.RunCommand
// which also extracts the RC from the error, to distinguish between a command RC error and a target connection error
func (p *BaseTargetActions) RunCommand(cmd string) (RunCommandOutput, error) {
	log.WithField("cmd", cmd).Debugf("Running target command")

	stdout, stderr, err := p.CmdRunner.RunCommand(cmd)
	rc, err := ExtractRCFromError(err)
	if err != nil {
		return RunCommandOutput{}, err
	}

	return RunCommandOutput{ReturnCode: rc, Stdout: stdout, Stderr: stderr}, err
}
