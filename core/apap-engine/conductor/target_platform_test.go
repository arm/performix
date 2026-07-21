// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package conductor

import (
	"errors"
	"fmt"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
)

func setMockCommandRunnerRunCommand(cmd string, stdout string, stderr string, err error) CommandRunner {
	mcr := &MockCommandRunner{}
	mcr.On("RunCommand", cmd).Return(stdout, stderr, err)
	return mcr
}

// TestRunLinuxCommand tests the concrete implementation of LinuxTargetActions.RunCommand
func TestRunLinuxCommand(t *testing.T) {
	cmd := "cd \"/working-dir\"; my-command"
	t.Run("RunCommand executes successfully", func(t *testing.T) {

		linuxTargetActions := LinuxTargetActions{BaseTargetActions: BaseTargetActions{CmdRunner: setMockCommandRunnerRunCommand(cmd, "valid output", "warnings", nil)}}
		output, err := linuxTargetActions.RunCommand(cmd)
		assert.NoError(t, err)
		assert.Equal(t, output.ReturnCode, 0)
		assert.Equal(t, output.Stdout, "valid output")
		assert.Equal(t, output.Stderr, "warnings")
	})
	t.Run("RunCommand returns RC when the command is invalid for remote command", func(t *testing.T) {
		var err error
		// Run a simple exec command to exit 5. We cannot easily fake this.
		_, err = exec.Command("sh", "-c", "exit 5").Output()
		assert.Error(t, err)
		exitErr, ok := err.(*exec.ExitError)
		assert.True(t, ok)

		linuxTargetActions := LinuxTargetActions{BaseTargetActions: BaseTargetActions{CmdRunner: setMockCommandRunnerRunCommand(cmd, "valid output", "", exitErr)}}
		output, err := linuxTargetActions.RunCommand(cmd)
		assert.NoError(t, err)
		assert.Equal(t, output.ReturnCode, 5)
	})
	t.Run("RunCommand returns RC when the command is invalid for local command", func(t *testing.T) {
		fakeErr := FakeExitError{Code: 5}
		linuxTargetActions := LinuxTargetActions{BaseTargetActions: BaseTargetActions{CmdRunner: setMockCommandRunnerRunCommand(cmd, "valid output", "", fakeErr)}}
		output, err := linuxTargetActions.RunCommand(cmd)
		assert.NoError(t, err)
		assert.Equal(t, output.ReturnCode, 5)
	})
	t.Run("RunCommand fails when there is an ssh connection error", func(t *testing.T) {
		linuxTargetActions := LinuxTargetActions{BaseTargetActions: BaseTargetActions{CmdRunner: setMockCommandRunnerRunCommand(cmd, "", "", fmt.Errorf("rekt"))}}
		_, err := linuxTargetActions.RunCommand(cmd)
		assert.EqualError(t, err, "rekt")
	})
	t.Run("RunCommand succeeds and correctly wraps the command in 'sudo bash', when the command is ran as admin", func(t *testing.T) {
		expectedCmd := "sudo bash -c 'cd \"/working-dir\"; my-command'"
		linuxTargetActions := LinuxTargetActions{BaseTargetActions: BaseTargetActions{CmdRunner: setMockCommandRunnerRunCommand(expectedCmd, "valid output", "", nil)}}
		output, err := linuxTargetActions.RunCommandAsAdmin(cmd)
		assert.NoError(t, err)
		assert.Equal(t, output.Stdout, "valid output")
	})

	t.Run("HasAdminPerms fails when CommandRunner fails", func(t *testing.T) {
		rcErr := errors.New("no perms")
		expectedCmd := "sudo -n true"
		linuxTargetActions := LinuxTargetActions{BaseTargetActions: BaseTargetActions{CmdRunner: setMockCommandRunnerRunCommand(expectedCmd, "", "", rcErr)}}
		hasAdminPerms, err := linuxTargetActions.HasAdminPerms()
		assert.False(t, hasAdminPerms)
		assert.Equal(t, err, rcErr)
	})

	t.Run("HasAdminPerms returns false when RC is not 0", func(t *testing.T) {
		fakeErr := FakeExitError{Code: 5}
		expectedCmd := "sudo -n true"
		linuxTargetActions := LinuxTargetActions{BaseTargetActions: BaseTargetActions{CmdRunner: setMockCommandRunnerRunCommand(expectedCmd, "", "", fakeErr)}}
		hasAdminPerms, err := linuxTargetActions.HasAdminPerms()
		assert.False(t, hasAdminPerms)
		assert.NoError(t, err)
	})

	t.Run("HasAdminPerms returns true when RC is 0", func(t *testing.T) {
		fakeErr := FakeExitError{Code: 0}
		expectedCmd := "sudo -n true"
		linuxTargetActions := LinuxTargetActions{BaseTargetActions: BaseTargetActions{CmdRunner: setMockCommandRunnerRunCommand(expectedCmd, "", "", fakeErr)}}
		hasAdminPerms, err := linuxTargetActions.HasAdminPerms()
		assert.True(t, hasAdminPerms)
		assert.NoError(t, err)
	})

	t.Run("RemoveDir executes successfully", func(t *testing.T) {
		dir := "/test/directory"
		rmDirCmd := "rm -rf /test/directory"
		linuxTargetActions := LinuxTargetActions{BaseTargetActions: BaseTargetActions{CmdRunner: setMockCommandRunnerRunCommand(rmDirCmd, "", "", nil)}}
		err := linuxTargetActions.RemoveDir(dir)
		assert.NoError(t, err)
	})

	t.Run("RemoveDir returns error when command fails", func(t *testing.T) {
		dir := "/test/directory"
		rmDirCmd := "rm -rf /test/directory"
		cmdErr := errors.New("permission denied")
		linuxTargetActions := LinuxTargetActions{BaseTargetActions: BaseTargetActions{CmdRunner: setMockCommandRunnerRunCommand(rmDirCmd, "", "stderr output", cmdErr)}}
		err := linuxTargetActions.RemoveDir(dir)
		assert.Error(t, err)
		assert.Equal(t, cmdErr, err)
	})

	t.Run("RemoveDir handles special characters in path", func(t *testing.T) {
		dir := "/test/directory with spaces"
		rmDirCmd := "rm -rf /test/directory with spaces"
		linuxTargetActions := LinuxTargetActions{BaseTargetActions: BaseTargetActions{CmdRunner: setMockCommandRunnerRunCommand(rmDirCmd, "", "", nil)}}
		err := linuxTargetActions.RemoveDir(dir)
		assert.NoError(t, err)
	})
}

// TestRunWindowsCommand tests the concrete implementation of WindowsTargetActions.RunCommand
func TestRunWindowsCommand(t *testing.T) {
	cmd := "dir"
	t.Run("RunCommand executes successfully", func(t *testing.T) {
		windowsTargetActions := WindowsTargetActions{BaseTargetActions: BaseTargetActions{CmdRunner: setMockCommandRunnerRunCommand(cmd, "valid output", "warnings", nil)}}
		output, err := windowsTargetActions.RunCommand(cmd)
		assert.NoError(t, err)
		assert.Equal(t, output.ReturnCode, 0)
		assert.Equal(t, output.Stdout, "valid output")
		assert.Equal(t, output.Stderr, "warnings")
	})

	t.Run("RunCommand returns RC when the command is invalid for remote command", func(t *testing.T) {
		// Use a fake exit error to simulate a remote non-zero exit status
		fakeErr := FakeExitError{Code: 5}
		windowsTargetActions := WindowsTargetActions{BaseTargetActions: BaseTargetActions{CmdRunner: setMockCommandRunnerRunCommand(cmd, "valid output", "", fakeErr)}}
		output, err := windowsTargetActions.RunCommand(cmd)
		assert.NoError(t, err)
		assert.Equal(t, output.ReturnCode, 5)
	})

	t.Run("RunCommand returns RC when the command is invalid for local command", func(t *testing.T) {
		fakeErr := FakeExitError{Code: 5}
		windowsTargetActions := WindowsTargetActions{BaseTargetActions: BaseTargetActions{CmdRunner: setMockCommandRunnerRunCommand(cmd, "valid output", "", fakeErr)}}
		output, err := windowsTargetActions.RunCommand(cmd)
		assert.NoError(t, err)
		assert.Equal(t, output.ReturnCode, 5)
	})

	t.Run("RunCommand fails when there is a connection error", func(t *testing.T) {
		windowsTargetActions := WindowsTargetActions{BaseTargetActions: BaseTargetActions{CmdRunner: setMockCommandRunnerRunCommand(cmd, "", "", fmt.Errorf("rekt"))}}
		_, err := windowsTargetActions.RunCommand(cmd)
		assert.EqualError(t, err, "rekt")
	})

	t.Run("RunCommandAsAdmin is same as RunCommand", func(t *testing.T) {
		windowsTargetActions := WindowsTargetActions{BaseTargetActions: BaseTargetActions{CmdRunner: setMockCommandRunnerRunCommand(cmd, "admin output", "", nil)}}
		output, err := windowsTargetActions.RunCommandAsAdmin(cmd)
		assert.NoError(t, err)
		assert.Equal(t, output.Stdout, "admin output")
	})

	t.Run("HasAdminPerms always returns true", func(t *testing.T) {
		windowsTargetActions := WindowsTargetActions{BaseTargetActions: BaseTargetActions{CmdRunner: setMockCommandRunnerRunCommand(cmd, "", "", nil)}}
		hasAdminPerms, err := windowsTargetActions.HasAdminPerms()
		assert.True(t, hasAdminPerms)
		assert.NoError(t, err)
	})

	t.Run("RemoveDir executes successfully", func(t *testing.T) {
		rmDirCmd := "rmdir /s /q C:/testdir"
		windowsTargetActions := WindowsTargetActions{BaseTargetActions: BaseTargetActions{CmdRunner: setMockCommandRunnerRunCommand(rmDirCmd, "", "", nil)}}
		err := windowsTargetActions.RemoveDir("C:/testdir")
		assert.NoError(t, err)
	})

	t.Run("RemoveDir returns error", func(t *testing.T) {
		rmDirCmd := "rmdir /s /q C:/testdir"
		windowsTargetActions := WindowsTargetActions{BaseTargetActions: BaseTargetActions{CmdRunner: setMockCommandRunnerRunCommand(rmDirCmd, "", "error", fmt.Errorf("fail"))}}
		err := windowsTargetActions.RemoveDir("C:/testdir")
		assert.Error(t, err)
	})

	t.Run("WindowsPathUtils methods", func(t *testing.T) {
		p := &WindowsPathUtils{}
		assert.Equal(t, p.GetScriptExtension(), "ps1")
		assert.Equal(t, p.GetEnvPathSep(), ";")
		assert.True(t, p.IsAbs("C:/Windows"))
		assert.False(t, p.IsAbs("relative/path"))
		assert.Equal(t, p.ToOSPath("C:/Windows/System32"), `C:\Windows\System32`)
		assert.Equal(t, p.FormatPathForShell(`C:\Test Path`), `"C:\Test Path"`)
		assert.Equal(t, p.GenerateChdirCommandLine("C:/dir", "echo hi"), `cd "C:\dir" && echo hi`)
		env := EnvVar{Name: "FOO", Value: "BAR"}
		assert.Contains(t, p.GenerateCommandLineWithEnv("echo hi", env), "set FOO=BAR & echo hi")
		assert.Equal(t, p.GenerateRunScriptCommand("script.ps1", "C:/dir"), `cd "C:\dir" && powershell -NoProfile -WindowStyle Hidden -ExecutionPolicy Bypass -File script.ps1`)
	})
}
