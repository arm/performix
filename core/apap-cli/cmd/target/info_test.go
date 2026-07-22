// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package target

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/mocks"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/target"
	targetloginmocks "github.com/Arm-Debug/apap-cli/apap-cli/service/targetlogin/mocks"
	"github.com/Arm-Debug/apap-cli/apap-cli/utils"
	engine_target "github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
	apapprotomocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
)

type mockTargetInfoCollector struct {
	mock.Mock
}

type mockTargetInfoFormatter struct {
	mock.Mock
}

func (tic *mockTargetInfoCollector) CollectTargetInfo(client apapproto.ApapClient, t engine_target.Target, collectors []string) (*apapproto.TargetInfoResponse, error) {
	mockArgs := tic.Called(client, t, collectors)
	return mockArgs.Get(0).(*apapproto.TargetInfoResponse), mockArgs.Error(1)
}

func (formatter *mockTargetInfoFormatter) FormatTargetInfo(out io.Writer, response *apapproto.TargetInfoResponse) {
	f := formatTargetInfo{}
	f.FormatTargetInfo(out, response)
}

func TestTargetInfo(t *testing.T) {
	tgt := &engine_target.SSHTarget{Jumps: []engine_target.SSHHostConfig{{}}}

	mtm := &target.MockTargetManager{}
	mtm.On("GetTarget", mock.Anything).Return(tgt, nil)

	client := apapprotomocks.NewApapClient(t)
	cc := &mocks.MockAutostartClientConnector{}
	cc.SetClient(client, nil)

	kernelVersion := "1.2.3.4"
	systemResp := apapproto.TargetInfoSystemResponse{Os: &apapproto.TargetInfoOS{Family: 2, KernelVersion: &kernelVersion}}
	info := apapproto.TargetInfo{Info: &apapproto.TargetInfo_System{System: &systemResp}}
	targetInfoResponse := &apapproto.TargetInfoResponse{Info: make(map[string]*apapproto.TargetInfo)}
	targetInfoResponse.Info["sl-collect-target-info"] = &info

	t.Run("target info succeeds with --pids flag", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t).WithLogin(t, tgt, nil)
		col := &mockTargetInfoCollector{}
		col.On("CollectTargetInfo", mock.Anything, tgt, []string{"sl-collect-target-pids"}).Return(targetInfoResponse, nil)

		// Mock the formatter, but it expects to get the same targetInfoResponse
		formatter := mockTargetInfoFormatter{}
		formatter.On("FormatTargetInfo", io.Discard, targetInfoResponse)

		cmd := newInfoCommand(cc, mtm, col, &formatter, loginService)
		cmd.SetArgs([]string{mock.Anything, "--pids"})
		err := cmd.Execute()
		assert.NoError(t, err)
	})

	t.Run("valid target succeeds", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t).WithLogin(t, tgt, nil)
		col := &mockTargetInfoCollector{}
		col.Mock.On("CollectTargetInfo", mock.Anything, tgt, []string{"sl-collect-target-info"}).Return(targetInfoResponse, nil)

		// Mock the formatter, but it expects to get the same targetInfoResponse
		formatter := mockTargetInfoFormatter{}
		formatter.On("FormatTargetInfo", io.Discard, targetInfoResponse)

		cmd := newInfoCommand(cc, mtm, col, &formatter, loginService)
		cmd.SetArgs([]string{mock.Anything})
		err := cmd.Execute()
		assert.NoError(t, err)
	})

	t.Run("target info fails when collector fails", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t).WithLogin(t, tgt, nil)
		rektError := errors.New("rekt")
		col := &mockTargetInfoCollector{}
		col.Mock.On("CollectTargetInfo", mock.Anything, tgt, []string{"sl-collect-target-info"}).Return(targetInfoResponse, rektError)

		formatter := mockTargetInfoFormatter{}
		formatter.On("FormatTargetInfo", io.Discard, targetInfoResponse)

		cmd := newInfoCommand(cc, mtm, col, &formatter, loginService)
		cmd.SetArgs([]string{mock.Anything})
		err := cmd.Execute()
		assert.Error(t, err)
		assert.Equal(t, rektError, err)
	})

	t.Run("target info fails with invalid target", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t)
		rektError := errors.New("rekt")
		mtm := &target.MockTargetManager{}
		mtm.On("GetTarget", mock.Anything).Return(tgt, rektError)

		col := &mockTargetInfoCollector{}
		col.Mock.On("CollectTargetInfo", mock.Anything, tgt, []string{"sl-collect-target-info"}).Return(targetInfoResponse, rektError)

		formatter := mockTargetInfoFormatter{}
		formatter.On("FormatTargetInfo", io.Discard, targetInfoResponse)

		cmd := newInfoCommand(cc, mtm, col, &formatter, loginService)
		cmd.SetArgs([]string{mock.Anything})
		err := cmd.Execute()
		assert.Error(t, err)
		assert.ErrorContains(t, err, "rekt")
	})
}

func makeTargetInfoResponse(info *apapproto.TargetInfo) *apapproto.TargetInfoResponse {
	targetInfoResponse := &apapproto.TargetInfoResponse{Info: make(map[string]*apapproto.TargetInfo, 1)}
	targetInfoResponse.Info["sl-collect-target-info"] = info
	return targetInfoResponse
}

func TestFormatTargetInfo(t *testing.T) {
	formatter := formatTargetInfo{}

	strPtr := func(s string) *string { return &s }
	u32Ptr := func(v uint32) *uint32 { return &v }

	testCases := []struct {
		name               string
		response           *apapproto.TargetInfoResponse
		expectedSubstrings []string
	}{
		{
			name:               "handles nil response",
			response:           nil,
			expectedSubstrings: []string{"[Code]: common.UNKNOWN_ERROR"},
		},
		{
			name:     "handles nil info",
			response: makeTargetInfoResponse(nil),
		},
		{
			name:     "handles empty info map",
			response: &apapproto.TargetInfoResponse{},
		},
		{
			name: "prints system info with missing optional fields",
			response: makeTargetInfoResponse(&apapproto.TargetInfo{
				Info: &apapproto.TargetInfo_System{
					System: &apapproto.TargetInfoSystemResponse{
						Os:       &apapproto.TargetInfoOS{Family: apapproto.OsFamily_OS_FAMILY_UNKNOWN},
						Clusters: []*apapproto.TargetInfoCluster{},
						Cpus:     []*apapproto.TargetInfoCPU{},
						Tool:     []*apapproto.TargetInfoTool{},
					},
				},
			}),
			expectedSubstrings: []string{
				"OS Family", "Unknown", "CPU Architecture", "cluster_id", "core_number",
			},
		},
		{
			name: "prints system info with populated fields",
			response: makeTargetInfoResponse(&apapproto.TargetInfo{
				Info: &apapproto.TargetInfo_System{
					System: &apapproto.TargetInfoSystemResponse{
						Os: &apapproto.TargetInfoOS{
							Family:        apapproto.OsFamily_OS_FAMILY_LINUX,
							Description:   strPtr("Ubuntu 22.04"),
							KernelVersion: strPtr("6.6.0"),
						},
						Clusters: []*apapproto.TargetInfoCluster{
							{Id: u32Ptr(0), Name: strPtr("Cluster 0")},
						},
						Cpus: []*apapproto.TargetInfoCPU{
							{CoreNumber: u32Ptr(0), ClusterId: u32Ptr(0), Midr: strPtr("0x410fd0"), Name: strPtr("cpu0")},
						},
						Tool:    []*apapproto.TargetInfoTool{},
						CpuArch: apapproto.CpuArch_CPU_ARCH_AARCH64,
					},
				},
			}),
			expectedSubstrings: []string{"Linux", "Ubuntu 22.04", "6.6.0", "AArch64", "Cluster 0", "0x410fd0", "cpu0"},
		},
		{
			name: "prints Android OS family",
			response: makeTargetInfoResponse(&apapproto.TargetInfo{
				Info: &apapproto.TargetInfo_System{
					System: &apapproto.TargetInfoSystemResponse{
						Os: &apapproto.TargetInfoOS{
							Family:        apapproto.OsFamily_OS_FAMILY_ANDROID,
							Description:   strPtr("Android 15"),
							KernelVersion: strPtr("6.6.0"),
						},
						Clusters: []*apapproto.TargetInfoCluster{},
						Cpus:     []*apapproto.TargetInfoCPU{},
						Tool:     []*apapproto.TargetInfoTool{},
					},
				},
			}),
			expectedSubstrings: []string{"Android", "Android 15", "6.6.0"},
		},
		{
			name: "prints pid headers when process list is empty",
			response: &apapproto.TargetInfoResponse{
				Info: map[string]*apapproto.TargetInfo{
					"sl-collect-target-pids": {
						Info: &apapproto.TargetInfo_Pids{
							Pids: &apapproto.TargetInfoPIDResponse{Process: []*apapproto.TargetInfoPIDs{}},
						},
					},
				},
			},
			expectedSubstrings: []string{"PID", "name", "user name", "command line"},
		},
		{
			name: "prints pids when present",
			response: &apapproto.TargetInfoResponse{
				Info: map[string]*apapproto.TargetInfo{
					"sl-collect-target-pids": {
						Info: &apapproto.TargetInfo_Pids{
							Pids: &apapproto.TargetInfoPIDResponse{
								Process: []*apapproto.TargetInfoPIDs{
									{Pid: u32Ptr(1234), Name: strPtr("init"), Username: strPtr("root"), CommandLine: strPtr("/sbin/init")},
								},
							},
						},
					},
				},
			},
			expectedSubstrings: []string{"PID", "init", "root", "/sbin/init"},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			// Dummy command to allow us to reset the JSON flag
			cmd := &cobra.Command{}
			utils.SetPersistentFlags(cmd)

			cmdBuf := &bytes.Buffer{}
			formatter.FormatTargetInfo(cmdBuf, testCase.response)
			out := cmdBuf.String()
			if len(testCase.expectedSubstrings) == 0 {
				assert.Empty(t, out)
			} else {
				for _, substr := range testCase.expectedSubstrings {
					assert.Contains(t, out, substr)
				}
			}
		})
	}
}
