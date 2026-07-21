// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package packages

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
)

type inMemoryLogHook struct {
	entries []*log.Entry
}

func (h *inMemoryLogHook) Levels() []log.Level {
	return []log.Level{log.WarnLevel}
}

func (h *inMemoryLogHook) Fire(e *log.Entry) error {
	h.entries = append(h.entries, e)
	return nil
}

func TestGetExtensionsDir(t *testing.T) {
	t.Run("test no error", func(t *testing.T) {
		dir, err := GetExtensionsDir()
		require.NoError(t, err)
		require.NotEmpty(t, dir)
	})
}

func TestPackageManager(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "performix-test-")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(tempDir)) }()

	// Create temporary files to emulate an installation
	// Tool bundles
	executableDir := filepath.Join(tempDir, terminology.GetProductBinaryName())
	extensionsDir := filepath.Join(tempDir, "extensions")
	relativePaths := []string{
		filepath.Join("core-tool-1", "1.1", "core-tool-1.tar.gz"),
		filepath.Join("user-tool-1", "1.2", "user-tool-1.tar.gz"),
		filepath.Join("user-tool-2", "1.3", "user-tool-2.tar.gz"),
	}
	absolutePaths := []string{
		filepath.Join(executableDir, "tools", relativePaths[0]),
		filepath.Join(extensionsDir, "extension-1", "tools", relativePaths[1]),
		filepath.Join(extensionsDir, "extension-2", "tools", relativePaths[2]),
	}
	for _, path := range absolutePaths {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), perms.LocalDirPerm))
		require.NoError(t, os.WriteFile(path, nil, perms.LocalFilePerm))
	}
	// Tool integrations
	toolIntRelPaths := []string{
		filepath.Join("core-tool-integration-1.js"),
		filepath.Join("user-tool-integration-1.js"),
		filepath.Join("user-tool-integration-2.js"),
	}
	toolIntAbsPaths := []string{
		filepath.Join(executableDir, "tool-integrations", toolIntRelPaths[0]),
		filepath.Join(extensionsDir, "extension-1", "tool-integrations", toolIntRelPaths[1]),
		filepath.Join(extensionsDir, "extension-2", "tool-integrations", toolIntRelPaths[2]),
	}
	toolIntNames := []string{
		"core-tool-1",
		"user-tool-1",
		"user-tool-2",
	}
	toolIntVersions := []string{
		"0.1",
		"0.2",
		"0.3",
	}
	toolIntSource := `let tool = {	name: "%s",	version: "%s", };`
	for i, path := range toolIntAbsPaths {
		data := []byte(fmt.Sprintf(toolIntSource, toolIntNames[i], toolIntVersions[i]))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), perms.LocalDirPerm))
		require.NoError(t, os.WriteFile(path, data, perms.LocalFilePerm))
	}

	t.Run("test ListToolBundles", func(t *testing.T) {
		pm := NewPackageManager(executableDir, extensionsDir)
		tools, err := pm.ListToolBundles()
		require.NoError(t, err)
		require.Equal(t, len(relativePaths), len(tools))
		for _, tool := range tools {
			absPathIndex := slices.Index(absolutePaths, tool.Path())
			relPathIndex := slices.Index(relativePaths, tool.ID())
			require.NotEqual(t, absPathIndex, -1)
			require.Equal(t, absPathIndex, relPathIndex)
		}
	})

	t.Run("test ListToolIntegrations", func(t *testing.T) {
		pm := NewPackageManager(executableDir, extensionsDir)
		tools, err := pm.ListToolIntegrations()
		require.NoError(t, err)
		require.Equal(t, len(toolIntRelPaths), len(tools))
		for _, tool := range tools {
			absPathIndex := slices.Index(toolIntAbsPaths, tool.Path())
			relPathIndex := slices.Index(toolIntRelPaths, tool.ID())
			require.NotEqual(t, absPathIndex, -1)
			require.Equal(t, absPathIndex, relPathIndex)
		}
	})

	t.Run("test GetToolBundle", func(t *testing.T) {
		pm := NewPackageManager(executableDir, extensionsDir)
		for i := range absolutePaths {
			tool, err := pm.GetToolBundle(relativePaths[i])
			require.NoError(t, err)
			require.Equal(t, absolutePaths[i], tool.Path())
			require.Equal(t, relativePaths[i], tool.ID())
		}
	})

	t.Run("tool bundle with incorrect path format is ignored and causes a warning", func(t *testing.T) {
		invalidPaths := []string{
			filepath.Join(executableDir, "tools", "core-tool-2", "core-tool-2.tar.gz"),
			filepath.Join(executableDir, "tools", "core-tool-2", "core-tool-2"),
			filepath.Join(executableDir, "tools", "core-tool-2", "tar.gz"),
		}
		for _, path := range invalidPaths {
			require.NoError(t, os.MkdirAll(filepath.Dir(path), perms.LocalDirPerm))
			require.NoError(t, os.WriteFile(path, nil, perms.LocalFilePerm))
		}
		defer func() {
			for _, path := range invalidPaths {
				require.NoError(t, os.RemoveAll(path))
			}
		}()

		logHook := &inMemoryLogHook{}
		log.AddHook(logHook)

		pm := NewPackageManager(executableDir, extensionsDir)
		tools, err := pm.ListToolBundles()
		require.NoError(t, err)
		require.Equal(t, len(logHook.entries), 3)
		for _, entry := range logHook.entries {
			require.Contains(t, entry.Message, " does not match the expected path format: ")
		}
		require.Equal(t, len(relativePaths), len(tools)) // File with incorrect path format is ignored
	})

	t.Run("duplicate tool is ignored and causes a warning", func(t *testing.T) {
		relativePath := relativePaths[1]
		packageDir := filepath.Join(extensionsDir, "extension-dup")
		absolutePath := filepath.Join(packageDir, "tools", relativePath)
		require.NoError(t, os.MkdirAll(filepath.Dir(absolutePath), perms.LocalDirPerm))
		require.NoError(t, os.WriteFile(absolutePath, nil, perms.LocalFilePerm))
		require.NoError(t, os.MkdirAll(filepath.Join(packageDir, "tool-integrations"), perms.LocalDirPerm))
		require.NoError(t, os.WriteFile(filepath.Join(packageDir, "tool-integrations", "user-tool-integration-3.js"), nil, perms.LocalFilePerm))
		defer func() { require.NoError(t, os.RemoveAll(packageDir)) }()

		logHook := &inMemoryLogHook{}
		log.AddHook(logHook)

		pm := NewPackageManager(executableDir, extensionsDir)
		tool, err := pm.GetToolBundle(relativePath)
		require.NoError(t, err)
		require.Equal(t, len(logHook.entries), 1)
		require.Contains(t, logHook.entries[0].Message, " also exists in package ")
		require.Equal(t, relativePath, tool.ID())
		require.Equal(t, absolutePaths[1], tool.Path()) // Found tool with same ID in different package
	})

	t.Run("extensions directory is optional", func(t *testing.T) {
		pm := NewPackageManager(executableDir, "")
		tools, err := pm.ListToolBundles()
		require.NoError(t, err)
		require.Equal(t, 1, len(tools))
		require.Equal(t, relativePaths[0], tools[0].ID())
		require.Equal(t, absolutePaths[0], tools[0].Path())
	})

	t.Run("missing extensions directory causes a warning", func(t *testing.T) {
		logHook := &inMemoryLogHook{}
		log.AddHook(logHook)

		pm := NewPackageManager(executableDir, "missing-extensions")
		_, err = pm.ListToolBundles()
		require.NoError(t, err)
		_, err = pm.GetToolBundle(relativePaths[0])
		require.NoError(t, err)
		_, err = pm.ListToolIntegrations()
		require.NoError(t, err)
		_, err = pm.FindToolIntegrations()
		require.NoError(t, err)

		require.Equal(t, len(logHook.entries), 4)
		for _, entry := range logHook.entries {
			require.Contains(t, entry.Message, "failed to read extensions directory")
		}
	})

	t.Run("GetToolBundle with unknown ID returns an error", func(t *testing.T) {
		pm := NewPackageManager(executableDir, extensionsDir)
		_, err = pm.GetToolBundle("unknownID")
		expectedMetadata := map[string]string{
			"toolName":           "unknownID",
			"executableToolsDir": filepath.Join(executableDir, subDir[toolBundle]),
			"extensionsToolsDir": filepath.Join(extensionsDir, subDir[toolBundle]),
		}
		expectedErr := message.New(message.EnginePackagesLoadToolBundle).WithMetadata(expectedMetadata)
		assert.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})

	t.Run("test FindToolIntegrations", func(t *testing.T) {
		pm := NewPackageManager(executableDir, extensionsDir)
		registry, err := pm.FindToolIntegrations()
		require.NoError(t, err)
		require.Equal(t, len(toolIntRelPaths), len(registry.Tools))
		for i := range toolIntNames {
			require.NotNil(t, registry.FindTool(toolIntNames[i], toolIntVersions[i]))
		}
	})

	t.Run("test FindToolIntegrations fails when no files found", func(t *testing.T) {
		pm := NewPackageManager("", "")
		_, err := pm.FindToolIntegrations()
		require.Error(t, err)
	})

	t.Run("test invalid tool integration script is ignored and warns", func(t *testing.T) {
		path := filepath.Join(executableDir, "tool-integrations", "invalid-tool.js")
		require.NoError(t, os.MkdirAll(filepath.Dir(path), perms.LocalDirPerm))
		require.NoError(t, os.WriteFile(path, nil, perms.LocalFilePerm))
		defer func() { require.NoError(t, os.RemoveAll(path)) }()

		logHook := &inMemoryLogHook{}
		log.AddHook(logHook)

		pm := NewPackageManager(executableDir, extensionsDir)
		_, err := pm.FindToolIntegrations()
		require.NoError(t, err)
		require.Equal(t, len(logHook.entries), 1)
		require.Contains(t, logHook.entries[0].Message, "failed to load tool integration from source")
	})
}
