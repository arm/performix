// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package conductor

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
)

func TestResolveToolsBaseDir_Posix(t *testing.T) {
	t.Run("uses configured directory when provided", func(t *testing.T) {
		cmdRunner := &MockCommandRunner{}
		cmdRunner.On("RunCommand", "test -e /custom/tools").Return("", "", nil).Once()
		cmdRunner.On("RunCommand", "test -w /custom/tools").Return("", "", nil).Once()

		baseDir, err := ResolveToolsBaseDir("/custom/tools", Linux, cmdRunner)
		assert.NoError(t, err)
		assert.Equal(t, "/custom/tools/"+terminology.GetProductBinaryName()+"/tools", filepath.ToSlash(baseDir))
		cmdRunner.AssertExpectations(t)
	})

	t.Run("expands configured path using target home", func(t *testing.T) {
		cmdRunner := &MockCommandRunner{}
		cmdRunner.On("RunCommand", "printenv HOME").Return("/home/testuser", "", nil).Once()
		cmdRunner.On("RunCommand", "test -e /home/testuser/custom/tools").Return("", "", nil).Once()
		cmdRunner.On("RunCommand", "test -w /home/testuser/custom/tools").Return("", "", nil).Once()

		baseDir, err := ResolveToolsBaseDir("~/custom/tools", Linux, cmdRunner)
		assert.NoError(t, err)
		assert.Equal(t, "/home/testuser/custom/tools/"+terminology.GetProductBinaryName()+"/tools", filepath.ToSlash(baseDir))
		cmdRunner.AssertExpectations(t)
	})

	t.Run("errors when configured path is not writable", func(t *testing.T) {
		cmdRunner := &MockCommandRunner{}
		cmdRunner.On("RunCommand", "test -e /custom/tools").Return("", "", nil).Once()
		cmdRunner.On("RunCommand", "test -w /custom/tools").Return("", "", assert.AnError).Once()

		_, err := ResolveToolsBaseDir("/custom/tools", Linux, cmdRunner)
		var msgErr message.Message
		assert.ErrorAs(t, err, &msgErr)
		assert.Equal(t, message.EngineToolTargetPathNoWritableToolsPath, msgErr.Code())
		cmdRunner.AssertExpectations(t)
	})

	t.Run("uses default under home when writable", func(t *testing.T) {
		cmdRunner := &MockCommandRunner{}
		cmdRunner.On("RunCommand", "printenv HOME").Return("/home/testuser", "", nil).Once()
		cmdRunner.On("RunCommand", "test -e /home/testuser").Return("", "", nil).Once()
		cmdRunner.On("RunCommand", "test -w /home/testuser").Return("", "", nil).Once()

		baseDir, err := ResolveToolsBaseDir("", Linux, cmdRunner)
		assert.NoError(t, err)
		defaultDir := fmt.Sprintf("/home/testuser/.local/share/%v/tools", terminology.GetProductBinaryName())
		assert.Equal(t, filepath.ToSlash(defaultDir), filepath.ToSlash(baseDir))
		cmdRunner.AssertExpectations(t)
	})

	t.Run("uses android tmp directory for Android default", func(t *testing.T) {
		cmdRunner := &MockCommandRunner{}
		cmdRunner.On("RunCommand", "test -e "+DefaultAndroidTempDir).Return("", "", nil).Once()
		cmdRunner.On("RunCommand", "test -w "+DefaultAndroidTempDir).Return("", "", nil).Once()

		baseDir, err := ResolveToolsBaseDir("", Android, cmdRunner)
		assert.NoError(t, err)
		assert.Equal(t, DefaultAndroidTempDir+"/"+terminology.GetProductBinaryName()+"/tools", filepath.ToSlash(baseDir))
		cmdRunner.AssertExpectations(t)
	})

	t.Run("errors when home is missing", func(t *testing.T) {
		cmdRunner := &MockCommandRunner{}
		cmdRunner.On("RunCommand", "printenv HOME").Return("", "", nil).Once()

		_, err := ResolveToolsBaseDir("", Linux, cmdRunner)
		assert.Error(t, err)
		cmdRunner.AssertExpectations(t)
	})

	t.Run("errors when default path is not writable", func(t *testing.T) {
		cmdRunner := &MockCommandRunner{}
		cmdRunner.On("RunCommand", "printenv HOME").Return("/home/testuser", "", nil).Once()
		cmdRunner.On("RunCommand", "test -e /home/testuser").Return("", "", nil).Once()
		cmdRunner.On("RunCommand", "test -w /home/testuser").Return("", "", assert.AnError).Once()

		_, err := ResolveToolsBaseDir("", Linux, cmdRunner)
		assert.Error(t, err)
		cmdRunner.AssertExpectations(t)
	})

	t.Run("errors when configured path is missing", func(t *testing.T) {
		cmdRunner := &MockCommandRunner{}
		cmdRunner.On("RunCommand", "test -e /missing/tools").Return("", "", assert.AnError).Once()

		_, err := ResolveToolsBaseDir("/missing/tools", Linux, cmdRunner)
		var msgErr message.Message
		assert.ErrorAs(t, err, &msgErr)
		assert.Equal(t, message.EngineToolTargetPathDirMissing, msgErr.Code())
		cmdRunner.AssertExpectations(t)
	})

	t.Run("expands relative path under home", func(t *testing.T) {
		cmdRunner := &MockCommandRunner{}
		cmdRunner.On("RunCommand", "printenv HOME").Return("/home/testuser", "", nil).Once()
		cmdRunner.On("RunCommand", "test -e /home/testuser/rel/path").Return("", "", nil).Once()
		cmdRunner.On("RunCommand", "test -w /home/testuser/rel/path").Return("", "", nil).Once()

		baseDir, err := ResolveToolsBaseDir("rel/path", Linux, cmdRunner)
		assert.NoError(t, err)
		assert.Equal(t, "/home/testuser/rel/path/"+terminology.GetProductBinaryName()+"/tools", filepath.ToSlash(baseDir))
		cmdRunner.AssertExpectations(t)
	})

	t.Run("expands tilde path with backslashes", func(t *testing.T) {
		cmdRunner := &MockCommandRunner{}
		cmdRunner.On("RunCommand", "printenv HOME").Return("/home/testuser", "", nil).Once()
		cmdRunner.On("RunCommand", mock.Anything).Return("", "", nil).Maybe()

		baseDir, err := ResolveToolsBaseDir(`~\tools\back`, Linux, cmdRunner)
		assert.NoError(t, err)
		assert.Equal(t, "/home/testuser/tools/back/"+terminology.GetProductBinaryName()+"/tools", filepath.ToSlash(baseDir))
		cmdRunner.AssertExpectations(t)
	})

	t.Run("expands relative path with backslashes", func(t *testing.T) {
		cmdRunner := &MockCommandRunner{}
		cmdRunner.On("RunCommand", "printenv HOME").Return("/home/testuser", "", nil).Once()
		cmdRunner.On("RunCommand", mock.Anything).Return("", "", nil).Maybe()

		baseDir, err := ResolveToolsBaseDir(`rel\back`, Linux, cmdRunner)
		assert.NoError(t, err)
		assert.Equal(t, "/home/testuser/rel/back/"+terminology.GetProductBinaryName()+"/tools", filepath.ToSlash(baseDir))
		cmdRunner.AssertExpectations(t)
	})

	t.Run("tilde path errors when home missing", func(t *testing.T) {
		cmdRunner := &MockCommandRunner{}
		cmdRunner.On("RunCommand", "printenv HOME").Return("", "", nil).Once()

		_, err := ResolveToolsBaseDir("~/custom/tools", Linux, cmdRunner)
		var msgErr message.Message
		assert.ErrorAs(t, err, &msgErr)
		assert.Equal(t, message.EngineToolTargetPathTargetHomeUnavailable, msgErr.Code())
		cmdRunner.AssertExpectations(t)
	})

	t.Run("rejects lock directory conflict", func(t *testing.T) {
		cmdRunner := &MockCommandRunner{}
		lockDir := "/tmp/" + terminology.GetProductBinaryName()

		_, err := ResolveToolsBaseDir(lockDir, Linux, cmdRunner)
		var msgErr message.Message
		assert.ErrorAs(t, err, &msgErr)
		assert.Equal(t, "engine.tool.target_path.LOCK_DIR_CONFLICT", msgErr.Code())
		cmdRunner.AssertExpectations(t)
	})

	t.Run("rejects lock directory conflict with backslashes", func(t *testing.T) {
		cmdRunner := &MockCommandRunner{}
		lockDir := `\tmp\` + terminology.GetProductBinaryName()

		_, err := ResolveToolsBaseDir(lockDir, Linux, cmdRunner)
		var msgErr message.Message
		assert.ErrorAs(t, err, &msgErr)
		assert.Equal(t, "engine.tool.target_path.LOCK_DIR_CONFLICT", msgErr.Code())
		cmdRunner.AssertExpectations(t)
	})
}
