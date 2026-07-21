// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package stages

import (
	"context"
	"fmt"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/Arm-Debug/apap-cli/apap-engine/agent"
	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/targetsession"
	targetsessionmocks "github.com/Arm-Debug/apap-cli/apap-engine/targetsession/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
	targetagentmocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

func linuxArm64PathProvider() *conductor.TargetPlatform {
	return &conductor.TargetPlatform{
		PlatformConfiguration: conductor.PlatformConfiguration{Architecture: conductor.AArch64, OS: conductor.Linux},
		Path:                  &conductor.LinuxPathUtils{},
	}
}

var linuxArm64Ti = &targetagentproto.TargetInfo{
	Os:            "linux",
	OsDescription: "Ubuntu 24.04.3 LTS",
	Arch:          "arm64",
	KernelVersion: "6.8.0",
	IsRoot:        true,
	CpuTopology: &targetagentproto.CPUTopology{
		PrimaryCPUName: "Neoverse-V2",
		CPUs: []*targetagentproto.CPUDescription{
			{
				CoreNumber: 0,
				ClusterID:  0,
				Midr:       "0x410fd4f1",
				Name:       "Neoverse-V2",
			},
		},
		ClusterInfo: []*targetagentproto.ClusterDescription{
			{
				ClusterID: 0,
				Name:      "Cluster 0",
			},
		},
	},
}

var androidArm64Ti = &targetagentproto.TargetInfo{
	Os:            "android",
	OsDescription: "Android OS",
	Arch:          "arm64",
	KernelVersion: "6.8.0",
	IsRoot:        true,
	CpuTopology:   &targetagentproto.CPUTopology{},
}

func TestTargetInfoCollection_agent(t *testing.T) {
	t.Run("test successful targetprobe creates output files", func(t *testing.T) {
		var output util.Named[[]recipe.CollectorOutput]
		client := &targetagentmocks.TargetAgentClient{}

		sink := func(o util.Named[[]recipe.CollectorOutput]) {
			output = o
		}

		outputPathSupplier := func() []string {
			outputPath := []string{"/foo/bar", "/foo/baz", "/bar/taz"}
			return outputPath
		}

		agentSupplier := func() *agent.AgentConn {
			return &agent.AgentConn{Client: client}
		}

		fs := afero.NewMemMapFs()
		targetSession := &targetsessionmocks.MockTargetSession{}
		targetSession.On("ResolveToolsDir").Return("daemonPath").Maybe()
		stage := NewCollectTargetInfoStage(outputPathSupplier, sink, fs, agentSupplier, func() targetsession.TargetSession { return targetSession }, linuxArm64PathProvider)
		client.On("GetTargetInfo", mock.Anything, mock.Anything).Return(linuxArm64Ti, nil)

		// Execute returns no errors
		_, err := stage.Execute(&recipe.StageContext{Context: context.Background()})
		assert.NoError(t, err)

		// Output files exist and contain what we expect
		os := []byte(fmt.Sprintf(`{"os_family":%d,"os_description":"Ubuntu 24.04.3 LTS","kernel_version":"6.8.0"}`, apapproto.OsFamily_OS_FAMILY_LINUX))
		exists, _ := afero.FileContainsBytes(fs, outputPathSupplier()[0], os)
		assert.True(t, exists)

		// Config schema and name match
		assert.Equal(t, output.Value[0].ComponentType.SchemaVersion, TargetInfoOSSchemaVersion)
		assert.Equal(t, output.Value[0].ComponentType.Name, TargetInfoComponentName+"-os")

		assert.Equal(t, output.Value[1].ComponentType.SchemaVersion, TargetInfoCPUClusterSchemaVersion)
		assert.Equal(t, output.Value[1].ComponentType.Name, TargetInfoComponentName+"-cluster")

		assert.Equal(t, output.Value[2].ComponentType.SchemaVersion, TargetInfoCPUClusterSchemaVersion)
		assert.Equal(t, output.Value[2].ComponentType.Name, TargetInfoComponentName+"-cpus")
	})

	t.Run("writes Android OS family", func(t *testing.T) {
		client := &targetagentmocks.TargetAgentClient{}
		outputPathSupplier := func() []string {
			return []string{"/foo/os", "/foo/cluster", "/foo/cpu"}
		}
		agentSupplier := func() *agent.AgentConn {
			return &agent.AgentConn{Client: client}
		}

		fs := afero.NewMemMapFs()
		targetSession := &targetsessionmocks.MockTargetSession{}
		targetSession.On("ResolveToolsDir").Return("daemonPath").Maybe()
		stage := NewCollectTargetInfoStage(outputPathSupplier, nil, fs, agentSupplier, func() targetsession.TargetSession { return targetSession }, linuxArm64PathProvider)

		client.On("GetTargetInfo", mock.Anything, mock.Anything).Return(androidArm64Ti, nil)

		_, err := stage.Execute(&recipe.StageContext{Context: context.Background()})
		assert.NoError(t, err)

		expectedOS := []byte(fmt.Sprintf(`{"os_family":%d,"os_description":"Android OS","kernel_version":"6.8.0"}`, apapproto.OsFamily_OS_FAMILY_ANDROID))
		exists, _ := afero.FileContainsBytes(fs, outputPathSupplier()[0], expectedOS)
		assert.True(t, exists)
	})

}
