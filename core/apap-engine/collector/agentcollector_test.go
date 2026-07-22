// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package collector

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

func TestTargetInfoProtoToTargetDescription(t *testing.T) {
	targetInfoProto := &targetagentproto.TargetInfo{
		Os:            "Family",
		OsDescription: "Description",
		CpuTopology: &targetagentproto.CPUTopology{
			PrimaryCPUName: "PrimaryCPUName",
			CPUs: []*targetagentproto.CPUDescription{
				{
					CoreNumber: 1,
					ClusterID:  2,
					Midr:       "Midr",
					Name:       "Name",
				},
			},
			ClusterInfo: []*targetagentproto.ClusterDescription{
				{
					ClusterID: 2,
					Name:      "Name",
				},
			},
		},
	}
	expectedTargetDescription := &target.Description{
		Os: target.OsInfo{
			OSFamily:      "Family",
			OSDescription: "Description",
		},
		PrimaryCPUName: "PrimaryCPUName",
		CPUs: []target.CPUDescription{
			{
				CoreNumber: 1,
				ClusterID:  2,
				Midr:       "Midr",
				Name:       "Name",
			},
		},
		ClusterInfo: []target.ClusterDescription{
			{
				ClusterID: 2,
				Name:      "Name",
			},
		},
	}
	targetDescription := TargetInfoProtoToTargetDescription(targetInfoProto)
	require.Equal(t, expectedTargetDescription, targetDescription)
}

func strToPtr(str string) *string { return &str }
func uintToPtr(i uint32) *uint32  { return &i }

func TestTargetInfoAgentProtoToApapProto(t *testing.T) {
	t.Run("returns error if os is unknown", func(t *testing.T) {
		ti := &targetagentproto.TargetInfo{Os: "abc"}
		_, err := TargetInfoAgentProtoToApapProto(ti)
		expectedErr := message.New(message.EngineCommonUnsupportedTargetOs).WithMetadata(map[string]string{"os": "abc"})
		assert.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})
	t.Run("returns error if arch is unknown", func(t *testing.T) {
		ti := &targetagentproto.TargetInfo{Os: "windows", Arch: "abc"}
		_, err := TargetInfoAgentProtoToApapProto(ti)
		expectedErr := message.New(message.EngineCommonUnsupportedTargetArch).WithMetadata(map[string]string{"arch": "abc"})
		assert.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})
	t.Run("maps android OS family", func(t *testing.T) {
		ti := &targetagentproto.TargetInfo{
			Os:          "android",
			Arch:        "arm64",
			CpuTopology: &targetagentproto.CPUTopology{},
		}

		got, err := TargetInfoAgentProtoToApapProto(ti)

		assert.NoError(t, err)
		system := got.GetSystem()
		require.NotNil(t, system)
		assert.Equal(t, apapproto.OsFamily_OS_FAMILY_ANDROID, system.Os.Family)
	})
	t.Run("converts struct successfully", func(t *testing.T) {
		ti := &targetagentproto.TargetInfo{
			Os:            "windows",
			OsDescription: "Windows 10 Pro 25H2",
			Arch:          "arm64",
			KernelVersion: "10.0.26000",
			IsRoot:        false,
			CpuTopology: &targetagentproto.CPUTopology{
				PrimaryCPUName: "Cobalt 100",
				CPUs: []*targetagentproto.CPUDescription{
					{
						CoreNumber: 0,
						ClusterID:  0,
						Midr:       "",
						Name:       "Cobalt 100",
					},
					{
						CoreNumber: 1,
						ClusterID:  0,
						Midr:       "",
						Name:       "Cobalt 100",
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
		got, err := TargetInfoAgentProtoToApapProto(ti)
		assert.NoError(t, err)

		expected := &apapproto.TargetInfo{
			Info: &apapproto.TargetInfo_System{System: &apapproto.TargetInfoSystemResponse{
				Os: &apapproto.TargetInfoOS{
					Family:        apapproto.OsFamily_OS_FAMILY_WINDOWS,
					Description:   strToPtr("Windows 10 Pro 25H2"),
					KernelVersion: strToPtr("10.0.26000"),
				},
				Clusters: []*apapproto.TargetInfoCluster{
					{
						Id:   uintToPtr(0),
						Name: strToPtr("Cluster 0"),
					},
				},
				Cpus: []*apapproto.TargetInfoCPU{
					{
						CoreNumber: uintToPtr(0),
						ClusterId:  uintToPtr(0),
						Midr:       strToPtr(""),
						Name:       strToPtr("Cobalt 100"),
					},
					{
						CoreNumber: uintToPtr(1),
						ClusterId:  uintToPtr(0),
						Midr:       strToPtr(""),
						Name:       strToPtr("Cobalt 100"),
					},
				},
				Tool:    nil,
				CpuArch: apapproto.CpuArch_CPU_ARCH_AARCH64,
			}}}
		assert.Equal(t, expected, got)
	})
}
