// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package runtime

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/grpcconnection"
	"github.com/Arm-Debug/apap-cli/apap-engine/packages"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/targetsession"
	targetsessionmocks "github.com/Arm-Debug/apap-cli/apap-engine/targetsession/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool/deployer"
	"github.com/Arm-Debug/apap-cli/atperf-version/versions"
)

type stubTargetConnection struct {
	runner conductor.CommandRunner
	fs     afero.Fs
}

func (c *stubTargetConnection) CheckHealth() error                     { return nil }
func (c *stubTargetConnection) Close() error                           { return nil }
func (c *stubTargetConnection) CommandRunner() conductor.CommandRunner { return c.runner }
func (c *stubTargetConnection) Filesystem() conductor.TargetFilesystem {
	return conductor.NewAferoTargetFilesystemWithHostFS(c.fs, afero.NewOsFs())
}
func (c *stubTargetConnection) Dialer() grpcconnection.TCPDialer { return nil }

type stubTargetSessionProvider struct {
	session targetsession.TargetSession
}

func (p *stubTargetSessionProvider) TargetSession(target target.Target) (targetsession.TargetSession, error) {
	return p.session, nil
}
func (p *stubTargetSessionProvider) Shutdown() error { return nil }

type stubTarget struct{ username string }

func (t stubTarget) DisplayHost() string                       { return "stub" }
func (t stubTarget) String() string                            { return "stub" }
func (t stubTarget) GetUserDataDirectoryName() (string, error) { return t.username, nil }
func (t stubTarget) Validate(name string) error                { return nil }

func writeToolBundle(t *testing.T, root string, toolInfo tool.ToolInfo, platform conductor.PlatformConfiguration) {
	t.Helper()

	name := toolInfo.Name + "-" + string(platform.OS) + "-" + string(platform.Architecture) + ".tar.gz"
	path := filepath.Join(root, "tools", toolInfo.Name, toolInfo.Version, name)
	osFs := afero.NewOsFs()
	require.NoError(t, osFs.MkdirAll(filepath.Dir(path), perms.LocalDirPerm))
	require.NoError(t, afero.WriteFile(osFs, path, []byte{}, perms.LocalFilePerm))
}

func writeToolIntegration(t *testing.T, root string, toolInfo tool.ToolInfo, platform conductor.PlatformConfiguration) {
	t.Helper()

	src := `
		let tool = {
			name: "` + toolInfo.Name + `",
			version: "` + toolInfo.Version + `",
			description: { short: "Dummy", long: "Dummy" },
			deployments: [{
				appliesTo: [{architecture: "` + string(platform.Architecture) + `", os: "` + string(platform.OS) + `"}],
				dependencies: [{
					type: "tool_bundle",
					name: "` + toolInfo.Name + `",
					version: "` + toolInfo.Version + `",
					toolBundle: {name: "` + toolInfo.Name + `", version: "` + toolInfo.Version + `"},
					requiredWhen: {type: "always"},
				}],
			}],
		};`
	path := filepath.Join(root, "tool-integrations", toolInfo.Name+"-"+toolInfo.Version+".js")
	osFs := afero.NewOsFs()
	require.NoError(t, osFs.MkdirAll(filepath.Dir(path), perms.LocalDirPerm))
	require.NoError(t, afero.WriteFile(osFs, path, []byte(src), perms.LocalFilePerm))
}

func TestDeployMandatoryTools_ReturnsDeployWhenMissing(t *testing.T) {
	testCases := []struct {
		name     string
		platform conductor.PlatformConfiguration
		path     conductor.PathUtilities
	}{
		{
			name:     "Linux",
			platform: conductor.PlatformConfiguration{OS: conductor.Linux, Architecture: conductor.AArch64},
			path:     &conductor.LinuxPathUtils{},
		},
		{
			name:     "Android",
			platform: conductor.PlatformConfiguration{OS: conductor.Android, Architecture: conductor.AArch64},
			path:     &conductor.AndroidPathUtils{},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			tmpDir := t.TempDir()

			mandatoryTools := []tool.ToolInfo{
				{Name: terminology.GetAgentBinaryName(), Version: versions.GetVersion()},
			}
			toolsDir := filepath.Join(tmpDir, "tools")
			for _, ti := range mandatoryTools {
				writeToolBundle(t, toolsDir, ti, testCase.platform)
				writeToolIntegration(t, toolsDir, ti, testCase.platform)
			}

			pm := packages.NewPackageManager(toolsDir, "")
			fs := afero.NewMemMapFs()
			cmdRunner := &conductor.LocalCommandRunner{}
			tp := &conductor.TargetPlatform{
				PlatformConfiguration: testCase.platform,
				Path:                  testCase.path,
				Actions:               nil,
			}

			session := &targetsessionmocks.MockTargetSession{}
			conn := &stubTargetConnection{runner: cmdRunner, fs: fs}
			session.On("Connect", mock.Anything).Return(conn, nil).Once()
			session.On("TargetPlatform").Return(tp, nil).Twice()
			basePaths := deployer.BaseToolDeploymentPaths{DeployedToolsDirectory: toolsDir}
			session.On("ResolveToolsDir").Return(basePaths.DeployedToolsDirectory).Maybe()
			sessionProvider := &stubTargetSessionProvider{session: session}

			result, err := DeployMandatoryTools(context.Background(), stubTarget{username: "runner"}, basePaths, deployer.ToolDeployCHECK, pm, sessionProvider)

			require.NoError(t, err)
			require.Equal(t, deployer.Deploy, result)
		})
	}
}
