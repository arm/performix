// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipe

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	ds "github.com/Arm-Debug/apap-cli/apap-engine/deploymentsupport"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/packages"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
)

const toolDescr1 = `
	let tool = {
		name: "core-tool-1",
		version: "0.1",
	    description: {
		  short: "Short description",
		  long: "Long description",
	    },
	    deployments: [{
		  appliesTo: [
		    {architecture: "x86_64", os: "Linux"}
		  ],
	    }],
	};`

var linuxOS = conductor.Linux
var x86 = conductor.X86_64

func TestGetSupportedPlatforms(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "performix-test-")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(tempDir)) }()

	// Create a single tool bundle file
	executableDir := filepath.Join(tempDir, terminology.GetProductBinaryName())
	toolBundlePath := filepath.Join(executableDir, "tools", "core-tool-1", "1.1", "core-tool-1.tar.gz")
	require.NoError(t, os.MkdirAll(filepath.Dir(toolBundlePath), perms.LocalDirPerm))
	require.NoError(t, os.WriteFile(toolBundlePath, nil, perms.LocalFilePerm))

	// Create a single tool integration file
	toolIntPath := filepath.Join(executableDir, "tool-integrations", "core-tool-integration-1.js")
	toolIntData := []byte(toolDescr1)
	require.NoError(t, os.MkdirAll(filepath.Dir(toolIntPath), perms.LocalDirPerm))
	require.NoError(t, os.WriteFile(toolIntPath, toolIntData, perms.LocalFilePerm))

	t.Run("GetSupportedPlatforms shows supported platforms", func(t *testing.T) {
		recipeDeployments := []ds.DeploymentDeclaration{}
		deployment := ds.DeploymentDeclaration{
			AppliesTo: []ds.PlatformConfigurationFilter{
				{OS: &linuxOS, Architecture: &x86},
			},
			Dependencies: []ds.Dependency{
				{Type: ds.DependencyTypeTool, Name: "core-tool-1", Version: "0.1", RequiredWhen: ds.RequirementSpec{Type: ds.RequirementTypeAlways}},
			},
		}
		recipeDeployments = append(recipeDeployments, deployment)
		pm := packages.NewPackageManager(executableDir, "")
		supportedPlatforms, err := GetSupportedPlatforms(pm, &Recipe{Deployments: recipeDeployments})
		assert.NoError(t, err)
		assert.Equal(t, len(supportedPlatforms), 7) // There are 7 platform combinations defined in conductor.PlatformCombinations
		// Check that only the Linux x86_64 platform is supported
		for _, sp := range supportedPlatforms {
			if sp.Platform.OS == conductor.Linux && sp.Platform.Architecture == conductor.X86_64 {
				assert.Equal(t, sp.Result, ds.PlatformIsSupported)
			} else {
				assert.Equal(t, sp.Result, ds.PlatformNotSupported)
			}
		}
	})
	t.Run("GetSupportedPlatforms fails when cannot find tool integration", func(t *testing.T) {
		recipeDeployments := []ds.DeploymentDeclaration{}
		pm := packages.NewPackageManager(executableDir, "")
		deployment := ds.DeploymentDeclaration{
			AppliesTo: []ds.PlatformConfigurationFilter{
				{OS: &linuxOS, Architecture: &x86},
			},
			Dependencies: []ds.Dependency{
				{Type: ds.DependencyTypeTool, Name: "invalid-tool", Version: "0.1", RequiredWhen: ds.RequirementSpec{Type: ds.RequirementTypeAlways}},
			},
		}
		recipeDeployments = append(recipeDeployments, deployment)
		// Use an empty package manager with no tool integrations
		_, err := GetSupportedPlatforms(pm, &Recipe{Name: "my-recipe", Deployments: recipeDeployments})
		expectedMetadata := map[string]string{
			"toolIntegration": "invalid-tool",
			"recipe":          "my-recipe",
			"version":         "0.1",
		}
		expectedErr := message.New(message.EngineRecipeFindToolIntegration).WithMetadata(expectedMetadata)
		assert.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})
}
