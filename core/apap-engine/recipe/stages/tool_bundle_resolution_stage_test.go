// Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
//
// SPDX-License-Identifier: Apache-2.0

package stages

import (
	"context"
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
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
)

func TestToolBundleResolutionStage(t *testing.T) {
	tempDir := t.TempDir()
	executableDir := filepath.Join(tempDir, terminology.GetProductBinaryName())
	require.NoError(t, os.MkdirAll(filepath.Join(executableDir, "tools"), perms.LocalDirPerm))

	targetPlatform := conductor.PlatformConfiguration{
		OS:           conductor.Linux,
		Architecture: conductor.AArch64,
	}

	targetPlatformSupplier := func() conductor.PlatformConfiguration { return targetPlatform }

	t.Run("resolves tool bundle dependencies to target locality by default", func(t *testing.T) {
		pm := packages.NewPackageManager(executableDir, "")
		stage := NewToolBundleResolutionStage(
			pm,
			targetPlatformSupplier,
			nil,
			&recipe.Recipe{
				Deployments: []ds.DeploymentDeclaration{{
					Dependencies: []ds.Dependency{{
						Type:         ds.DependencyTypeToolBundle,
						Name:         "recipe-bundle",
						Version:      "1.0",
						RequiredWhen: ds.RequirementSpec{Type: ds.RequirementTypeAlways},
					}},
				}},
			},
		)

		cleanup, err := stage.Execute(&recipe.StageContext{Context: context.Background()})
		require.NoError(t, err)
		assert.Nil(t, cleanup)
		assert.Equal(t, []ds.ToolBundleInfo{{
			Name:     "recipe-bundle",
			Version:  "1.0",
			Locality: ds.DeploymentLocalityTarget,
		}}, stage.ToolBundlesSupplier())
	})

	t.Run("resolves host locality tool bundle dependencies", func(t *testing.T) {
		pm := packages.NewPackageManager(executableDir, "")
		stage := NewToolBundleResolutionStage(
			pm,
			targetPlatformSupplier,
			nil,
			&recipe.Recipe{
				Deployments: []ds.DeploymentDeclaration{{
					Dependencies: []ds.Dependency{{
						Type:         ds.DependencyTypeToolBundle,
						Name:         "host-bundle",
						Version:      "2.0",
						Locality:     ds.DeploymentLocalityHost,
						RequiredWhen: ds.RequirementSpec{Type: ds.RequirementTypeAlways},
					}},
				}},
			},
		)

		cleanup, err := stage.Execute(&recipe.StageContext{Context: context.Background()})
		require.NoError(t, err)
		assert.Nil(t, cleanup)
		assert.Equal(t, []ds.ToolBundleInfo{{
			Name:     "host-bundle",
			Version:  "2.0",
			Locality: ds.DeploymentLocalityHost,
		}}, stage.ToolBundlesSupplier())
	})

	t.Run("fails when a required tool integration is missing", func(t *testing.T) {
		pm := packages.NewPackageManager(executableDir, "")
		stage := NewToolBundleResolutionStage(
			pm,
			targetPlatformSupplier,
			nil,
			&recipe.Recipe{
				Name: "my-recipe",
				Deployments: []ds.DeploymentDeclaration{{
					Dependencies: []ds.Dependency{{
						Type:         ds.DependencyTypeTool,
						Name:         "missing-tool",
						Version:      "1.0",
						RequiredWhen: ds.RequirementSpec{Type: ds.RequirementTypeAlways},
					}},
				}},
			},
		)

		cleanup, err := stage.Execute(&recipe.StageContext{Context: context.Background()})
		require.Nil(t, cleanup)
		require.Error(t, err)
		assert.Equal(t, message.New(message.EngineRecipeFindToolIntegration).WithMetadata(map[string]string{
			"toolIntegration": "missing-tool",
			"version":         "1.0",
			"recipe":          "my-recipe",
		}), err)
	})
}
