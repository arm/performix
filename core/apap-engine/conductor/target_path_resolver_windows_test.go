// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package conductor

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/Arm-Debug/apap-cli/apap-engine/locality"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/atperf-agent/agentconfig"
	"github.com/stretchr/testify/assert"
)

func TestResolveToolsBaseDir_Windows(t *testing.T) {
	t.Run("uses default under LOCALAPPDATA when writable", func(t *testing.T) {
		cmdRunner := &MockCommandRunner{}
		localAppData := "C:/Users/runner/AppData/Local"
		cmdRunner.On("RunCommand", `powershell -NoProfile -NoLogo -WindowStyle Hidden -Command "echo $env:LOCALAPPDATA"`).Return("C:\\Users\\runner\\AppData\\Local\r\n", "", nil).Once()
		cmdRunner.On("RunCommand", fmt.Sprintf(`powershell -NoProfile -NoLogo -WindowStyle Hidden -Command "if (Test-Path '%s') { exit 0 } else { exit 1 }"`, localAppData)).
			Return("", "", nil).Once()
		cmdRunner.On("RunCommand", fmt.Sprintf(`powershell -NoProfile -NoLogo -WindowStyle Hidden -Command "$p = '%s'; $tmp = Join-Path $p '%v-writecheck.tmp'; try { Set-Content -Path $tmp -Value '' -ErrorAction Stop; Remove-Item $tmp -Force; exit 0 } catch { exit 1 }"`, localAppData, terminology.GetProductBinaryName())).
			Return("", "", nil).Once()

		baseDir, err := ResolveToolsBaseDir("", Win, cmdRunner, locality.Target)
		assert.NoError(t, err)
		expectedBaseDir := filepath.Join("C:/Users/runner/AppData/Local", terminology.GetProductBinaryName(), "tools")
		assert.Equal(t, filepath.ToSlash(expectedBaseDir), filepath.ToSlash(baseDir))
		cmdRunner.AssertExpectations(t)
	})

	t.Run("propagates missing localappdata errors", func(t *testing.T) {
		cmdRunner := &MockCommandRunner{}
		cmdRunner.On("RunCommand", `powershell -NoProfile -NoLogo -WindowStyle Hidden -Command "echo $env:LOCALAPPDATA"`).Return("", "", assert.AnError).Once()

		_, err := ResolveToolsBaseDir("", Win, cmdRunner, locality.Target)
		assert.Error(t, err)
		cmdRunner.AssertExpectations(t)
	})

	t.Run("errors when localappdata is empty", func(t *testing.T) {
		cmdRunner := &MockCommandRunner{}
		cmdRunner.On("RunCommand", `powershell -NoProfile -NoLogo -WindowStyle Hidden -Command "echo $env:LOCALAPPDATA"`).Return(" \n", "", nil).Once()

		_, err := ResolveToolsBaseDir("", Win, cmdRunner, locality.Target)
		var msgErr message.Message
		assert.ErrorAs(t, err, &msgErr)
		assert.Equal(t, message.EngineToolTargetPathWinLocalappdataUnavailable, msgErr.Code())
		cmdRunner.AssertExpectations(t)
	})

	t.Run("uses configured absolute directory when writable", func(t *testing.T) {
		cmdRunner := &MockCommandRunner{}
		base := `C:\custom\tools`
		cmdRunner.On("RunCommand", fmt.Sprintf(`powershell -NoProfile -NoLogo -WindowStyle Hidden -Command "if (Test-Path '%s') { exit 0 } else { exit 1 }"`, base)).
			Return("", "", nil).Once()
		cmdRunner.On("RunCommand", fmt.Sprintf(`powershell -NoProfile -NoLogo -WindowStyle Hidden -Command "$p = '%s'; $tmp = Join-Path $p '%v-writecheck.tmp'; try { Set-Content -Path $tmp -Value '' -ErrorAction Stop; Remove-Item $tmp -Force; exit 0 } catch { exit 1 }"`, base, terminology.GetProductBinaryName())).
			Return("", "", nil).Once()

		baseDir, err := ResolveToolsBaseDir(base, Win, cmdRunner, locality.Target)
		assert.NoError(t, err)
		assert.Equal(t, filepath.ToSlash(`C:\custom\tools\`+terminology.GetProductBinaryName()+`\tools`), filepath.ToSlash(baseDir))
		cmdRunner.AssertExpectations(t)
	})

	t.Run("uses configured absolute directory with forward slashes", func(t *testing.T) {
		cmdRunner := &MockCommandRunner{}
		base := "C:/custom/tools"
		cmdRunner.On("RunCommand", fmt.Sprintf(`powershell -NoProfile -NoLogo -WindowStyle Hidden -Command "if (Test-Path '%s') { exit 0 } else { exit 1 }"`, base)).
			Return("", "", nil).Once()
		cmdRunner.On("RunCommand", fmt.Sprintf(`powershell -NoProfile -NoLogo -WindowStyle Hidden -Command "$p = '%s'; $tmp = Join-Path $p '%v-writecheck.tmp'; try { Set-Content -Path $tmp -Value '' -ErrorAction Stop; Remove-Item $tmp -Force; exit 0 } catch { exit 1 }"`, base, terminology.GetProductBinaryName())).
			Return("", "", nil).Once()

		baseDir, err := ResolveToolsBaseDir(base, Win, cmdRunner, locality.Target)
		assert.NoError(t, err)
		assert.Equal(t, filepath.ToSlash(`C:\custom\tools\`+terminology.GetProductBinaryName()+`\tools`), filepath.ToSlash(baseDir))
		cmdRunner.AssertExpectations(t)
	})

	t.Run("uses forward-slash absolute directory when writable", func(t *testing.T) {
		cmdRunner := &MockCommandRunner{}
		base := "C:/custom/tools"
		cmdRunner.On("RunCommand", fmt.Sprintf(`powershell -NoProfile -NoLogo -WindowStyle Hidden -Command "if (Test-Path '%s') { exit 0 } else { exit 1 }"`, base)).
			Return("", "", nil).Once()
		cmdRunner.On("RunCommand", fmt.Sprintf(`powershell -NoProfile -NoLogo -WindowStyle Hidden -Command "$p = '%s'; $tmp = Join-Path $p '%v-writecheck.tmp'; try { Set-Content -Path $tmp -Value '' -ErrorAction Stop; Remove-Item $tmp -Force; exit 0 } catch { exit 1 }"`, base, terminology.GetProductBinaryName())).
			Return("", "", nil).Once()

		baseDir, err := ResolveToolsBaseDir(base, Win, cmdRunner, locality.Target)
		assert.NoError(t, err)
		assert.Equal(t, filepath.ToSlash(`C:\custom\tools\`+terminology.GetProductBinaryName()+`\tools`), filepath.ToSlash(baseDir))
		cmdRunner.AssertExpectations(t)
	})

	t.Run("errors when configured path is missing", func(t *testing.T) {
		cmdRunner := &MockCommandRunner{}
		base := `C:\missing\tools`
		cmdRunner.On("RunCommand", fmt.Sprintf(`powershell -NoProfile -NoLogo -WindowStyle Hidden -Command "if (Test-Path '%s') { exit 0 } else { exit 1 }"`, base)).
			Return("", "", assert.AnError).Once()

		_, err := ResolveToolsBaseDir(base, Win, cmdRunner, locality.Target)
		var msgErr message.Message
		assert.ErrorAs(t, err, &msgErr)
		assert.Equal(t, message.EngineToolTargetPathDirMissing, msgErr.Code())
		cmdRunner.AssertExpectations(t)
	})

	t.Run("errors when configured path is not writable", func(t *testing.T) {
		cmdRunner := &MockCommandRunner{}
		base := `C:\locked`
		cmdRunner.On("RunCommand", fmt.Sprintf(`powershell -NoProfile -NoLogo -WindowStyle Hidden -Command "if (Test-Path '%s') { exit 0 } else { exit 1 }"`, base)).
			Return("", "", nil).Once()
		cmdRunner.On("RunCommand", fmt.Sprintf(`powershell -NoProfile -NoLogo -WindowStyle Hidden -Command "$p = '%s'; $tmp = Join-Path $p '%v-writecheck.tmp'; try { Set-Content -Path $tmp -Value '' -ErrorAction Stop; Remove-Item $tmp -Force; exit 0 } catch { exit 1 }"`, base, terminology.GetProductBinaryName())).
			Return("", "", assert.AnError).Once()

		_, err := ResolveToolsBaseDir(base, Win, cmdRunner, locality.Target)
		var msgErr message.Message
		assert.ErrorAs(t, err, &msgErr)
		assert.Equal(t, message.EngineToolTargetPathNoWritableToolsPath, msgErr.Code())
		cmdRunner.AssertExpectations(t)
	})

	t.Run("expands relative path under LOCALAPPDATA", func(t *testing.T) {
		cmdRunner := &MockCommandRunner{}
		localAppData := "C:/Users/runner/AppData/Local"
		base := "rel/path"
		expanded := filepath.ToSlash(filepath.Join(localAppData, base))
		cmdRunner.On("RunCommand", `powershell -NoProfile -NoLogo -WindowStyle Hidden -Command "echo $env:LOCALAPPDATA"`).Return("C:\\Users\\runner\\AppData\\Local\r\n", "", nil).Once()
		cmdRunner.On("RunCommand", fmt.Sprintf(`powershell -NoProfile -NoLogo -WindowStyle Hidden -Command "if (Test-Path '%s') { exit 0 } else { exit 1 }"`, expanded)).
			Return("", "", nil).Once()
		cmdRunner.On("RunCommand", fmt.Sprintf(`powershell -NoProfile -NoLogo -WindowStyle Hidden -Command "$p = '%s'; $tmp = Join-Path $p '%v-writecheck.tmp'; try { Set-Content -Path $tmp -Value '' -ErrorAction Stop; Remove-Item $tmp -Force; exit 0 } catch { exit 1 }"`, expanded, terminology.GetProductBinaryName())).
			Return("", "", nil).Once()

		baseDir, err := ResolveToolsBaseDir(base, Win, cmdRunner, locality.Target)
		assert.NoError(t, err)
		expected := filepath.ToSlash(filepath.Join(expanded, terminology.GetProductBinaryName(), "tools"))
		assert.Equal(t, expected, filepath.ToSlash(baseDir))
		cmdRunner.AssertExpectations(t)
	})

	t.Run("expands tilde path under USERPROFILE", func(t *testing.T) {
		cmdRunner := &MockCommandRunner{}
		userProfile := "C:/Users/runner"
		base := `~\toolsbase`
		expanded := filepath.ToSlash(filepath.Join(userProfile, "toolsbase"))
		cmdRunner.On("RunCommand", `powershell -NoProfile -NoLogo -WindowStyle Hidden -Command "echo $env:USERPROFILE"`).Return("C:\\Users\\runner\r\n", "", nil).Once()
		cmdRunner.On("RunCommand", fmt.Sprintf(`powershell -NoProfile -NoLogo -WindowStyle Hidden -Command "if (Test-Path '%s') { exit 0 } else { exit 1 }"`, expanded)).
			Return("", "", nil).Once()
		cmdRunner.On("RunCommand", fmt.Sprintf(`powershell -NoProfile -NoLogo -WindowStyle Hidden -Command "$p = '%s'; $tmp = Join-Path $p '%v-writecheck.tmp'; try { Set-Content -Path $tmp -Value '' -ErrorAction Stop; Remove-Item $tmp -Force; exit 0 } catch { exit 1 }"`, expanded, terminology.GetProductBinaryName())).
			Return("", "", nil).Once()

		baseDir, err := ResolveToolsBaseDir(base, Win, cmdRunner, locality.Target)
		assert.NoError(t, err)
		expected := filepath.ToSlash(filepath.Join(expanded, terminology.GetProductBinaryName(), "tools"))
		assert.Equal(t, expected, filepath.ToSlash(baseDir))
		cmdRunner.AssertExpectations(t)
	})

	t.Run("expands tilde path with mixed slashes under USERPROFILE", func(t *testing.T) {
		cmdRunner := &MockCommandRunner{}
		userProfile := "C:/Users/runner"
		base := `~/tools\mixed`
		expanded := filepath.ToSlash(filepath.Join(userProfile, "tools", "mixed"))
		cmdRunner.On("RunCommand", `powershell -NoProfile -NoLogo -WindowStyle Hidden -Command "echo $env:USERPROFILE"`).Return("C:\\Users\\runner\r\n", "", nil).Once()
		cmdRunner.On("RunCommand", fmt.Sprintf(`powershell -NoProfile -NoLogo -WindowStyle Hidden -Command "if (Test-Path '%s') { exit 0 } else { exit 1 }"`, expanded)).
			Return("", "", nil).Once()
		cmdRunner.On("RunCommand", fmt.Sprintf(`powershell -NoProfile -NoLogo -WindowStyle Hidden -Command "$p = '%s'; $tmp = Join-Path $p '%v-writecheck.tmp'; try { Set-Content -Path $tmp -Value '' -ErrorAction Stop; Remove-Item $tmp -Force; exit 0 } catch { exit 1 }"`, expanded, terminology.GetProductBinaryName())).
			Return("", "", nil).Once()

		baseDir, err := ResolveToolsBaseDir(base, Win, cmdRunner, locality.Target)
		assert.NoError(t, err)
		expected := filepath.ToSlash(filepath.Join(expanded, terminology.GetProductBinaryName(), "tools"))
		assert.Equal(t, expected, filepath.ToSlash(baseDir))
		cmdRunner.AssertExpectations(t)
	})

	t.Run("expands relative path with backslashes under LOCALAPPDATA", func(t *testing.T) {
		cmdRunner := &MockCommandRunner{}
		localAppData := "C:/Users/runner/AppData/Local"
		base := `rel\back`
		expanded := filepath.ToSlash(filepath.Join(localAppData, "rel", "back"))
		cmdRunner.On("RunCommand", `powershell -NoProfile -NoLogo -WindowStyle Hidden -Command "echo $env:LOCALAPPDATA"`).Return("C:\\Users\\runner\\AppData\\Local\r\n", "", nil).Once()
		cmdRunner.On("RunCommand", fmt.Sprintf(`powershell -NoProfile -NoLogo -WindowStyle Hidden -Command "if (Test-Path '%s') { exit 0 } else { exit 1 }"`, expanded)).
			Return("", "", nil).Once()
		cmdRunner.On("RunCommand", fmt.Sprintf(`powershell -NoProfile -NoLogo -WindowStyle Hidden -Command "$p = '%s'; $tmp = Join-Path $p '%v-writecheck.tmp'; try { Set-Content -Path $tmp -Value '' -ErrorAction Stop; Remove-Item $tmp -Force; exit 0 } catch { exit 1 }"`, expanded, terminology.GetProductBinaryName())).
			Return("", "", nil).Once()

		baseDir, err := ResolveToolsBaseDir(base, Win, cmdRunner, locality.Target)
		assert.NoError(t, err)
		expected := filepath.ToSlash(filepath.Join(expanded, terminology.GetProductBinaryName(), "tools"))
		assert.Equal(t, expected, filepath.ToSlash(baseDir))
		cmdRunner.AssertExpectations(t)
	})

	t.Run("rejects lock directory conflict", func(t *testing.T) {
		cmdRunner := &MockCommandRunner{}
		lockDir := windowsJoin(agentconfig.GetDefaultLockRootDirectory("windows"))

		_, err := ResolveToolsBaseDir(lockDir, Win, cmdRunner, locality.Target)
		var msgErr message.Message
		assert.ErrorAs(t, err, &msgErr)
		assert.Equal(t, "engine.tool.target_path.LOCK_DIR_CONFLICT", msgErr.Code())
		cmdRunner.AssertExpectations(t)
	})

	t.Run("rejects lock directory conflict with backslashes", func(t *testing.T) {
		cmdRunner := &MockCommandRunner{}
		// lock dir expressed with backslashes should also conflict
		lockDir := `C:\tmp\` + terminology.GetProductBinaryName()

		_, err := ResolveToolsBaseDir(lockDir, Win, cmdRunner, locality.Target)
		var msgErr message.Message
		assert.ErrorAs(t, err, &msgErr)
		assert.Equal(t, "engine.tool.target_path.LOCK_DIR_CONFLICT", msgErr.Code())
		cmdRunner.AssertExpectations(t)
	})

	t.Run("rejects lock directory conflict without drive prefix", func(t *testing.T) {
		cmdRunner := &MockCommandRunner{}
		// Simulate a lock dir string without drive letter; normalize should still catch conflict
		lockDir := `\tmp\` + terminology.GetProductBinaryName()

		_, err := ResolveToolsBaseDir(lockDir, Win, cmdRunner, locality.Target)
		var msgErr message.Message
		assert.ErrorAs(t, err, &msgErr)
		assert.Equal(t, "engine.tool.target_path.LOCK_DIR_CONFLICT", msgErr.Code())
		cmdRunner.AssertExpectations(t)
	})

	t.Run("rejects lock directory conflict with forward slashes", func(t *testing.T) {
		cmdRunner := &MockCommandRunner{}
		lockDir := "C:/tmp/" + terminology.GetProductBinaryName()

		_, err := ResolveToolsBaseDir(lockDir, Win, cmdRunner, locality.Target)
		var msgErr message.Message
		assert.ErrorAs(t, err, &msgErr)
		assert.Equal(t, "engine.tool.target_path.LOCK_DIR_CONFLICT", msgErr.Code())
		cmdRunner.AssertExpectations(t)
	})
}
