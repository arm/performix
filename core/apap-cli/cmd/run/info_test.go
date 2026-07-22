// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/mocks"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/clijson"
	"github.com/Arm-Debug/apap-cli/apap-cli/test"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	engine_target "github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
	apapprotomocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
)

func makeRequest(runID string) *apapproto.GetRunDescriptionRequest {
	return &apapproto.GetRunDescriptionRequest{
		Id: &apapproto.RunId{Value: runID},
		ExtrasRequestStd: []apapproto.StandardRunDescriptionExtras{
			apapproto.StandardRunDescriptionExtras_EXTRA_TARGET_INFO,
		},
	}
}

func makeCmdJSON(t *testing.T, cc *mocks.MockAutostartClientConnector, svc *mocks.MockInfo, runID string) (*bytes.Buffer, *cobra.Command) {
	cmd := NewInfoCmd(cc, svc)
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	test.SetViperJSON(t, true)
	t.Cleanup(func() { test.SetViperJSON(t, false) })
	cmd.SetArgs([]string{runID})
	return buf, cmd
}

func runInfoText(t *testing.T, desc clijson.CLIRunDescription) string {
	t.Helper()

	client := apapprotomocks.NewApapClient(t)
	cc := &mocks.MockAutostartClientConnector{}
	cc.SetClient(client, nil)

	svc := &mocks.MockInfo{}
	svc.On("ListRun", client, makeRequest(desc.ID)).Return(desc, nil)

	cmd := NewInfoCmd(cc, svc)
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	test.SetViperJSON(t, false)
	t.Cleanup(func() { test.SetViperJSON(t, false) })
	cmd.SetArgs([]string{desc.ID})

	require.NoError(t, cmd.Execute())
	return buf.String()
}

func TestInfoCommand(t *testing.T) {
	t.Run("error is raised when no arguments specified", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)

		info := mocks.MockInfo{}

		cmd := NewInfoCmd(cc, &info)

		_, err := cmd.ExecuteC()

		require.Error(t, err)

		expectedErr := "accepts 1 arg(s), received 0"
		assert.Equal(t, expectedErr, err.Error())
	})

	t.Run("error is raised when too many arguments specified", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)

		info := mocks.MockInfo{}

		cmd := NewInfoCmd(cc, &info)
		cmd.SetArgs([]string{"abcdef123456", "fedcba654321"})

		_, err := cmd.ExecuteC()

		require.Error(t, err)

		expectedErr := "accepts 1 arg(s), received 2"
		assert.Equal(t, expectedErr, err.Error())
	})

	t.Run("lists runs when run id specified", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)

		runID := "abcdef123456"
		expectedName := "nameToSearch"
		Run := clijson.CLIRunDescription{ID: runID, Name: expectedName, Target: clijson.CLITarget{}}

		// Prepare the request struct
		runExtrasRequest := []apapproto.StandardRunDescriptionExtras{
			apapproto.StandardRunDescriptionExtras_EXTRA_TARGET_INFO,
		}

		runDesc := &apapproto.GetRunDescriptionRequest{
			Id:               &apapproto.RunId{Value: runID},
			ExtrasRequestStd: runExtrasRequest,
		}

		info := mocks.MockInfo{}
		info.On("ListRun", client, runDesc).Return(Run, nil)

		cmd := NewInfoCmd(cc, &info)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		cmd.SetArgs([]string{runID})
		err := cmd.Execute()

		assert.NoError(t, err)
		assert.Contains(t, cmdBuf.String(), runID)
	})

	t.Run("shows source code paths in the expected format when run id specified", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)

		runID := "abcdef123456"
		expectedPaths := []string{"/alpha/bravo", "/charlie/delta", "echo", "foxtrot"}
		source := run.HostSourceCodePath{Paths: expectedPaths}

		Run := clijson.CLIRunDescription{
			ID:                  runID,
			Name:                "irrelevant-for-this-test",
			Target:              clijson.CLITarget{}, // we don't care about target here
			HostSourceCodePaths: source,
		}

		runExtras := []apapproto.StandardRunDescriptionExtras{
			apapproto.StandardRunDescriptionExtras_EXTRA_TARGET_INFO,
		}
		runDescReq := &apapproto.GetRunDescriptionRequest{
			Id:               &apapproto.RunId{Value: runID},
			ExtrasRequestStd: runExtras,
		}

		infoSvc := mocks.MockInfo{}
		infoSvc.On("ListRun", client, runDescReq).Return(Run, nil)

		cmd := NewInfoCmd(cc, &infoSvc)
		buf := &bytes.Buffer{}
		cmd.SetOut(buf)
		test.SetViperJSON(t, true)
		cmd.SetArgs([]string{runID})

		err := cmd.Execute()
		assert.NoError(t, err)

		var resp clijson.CliJSONResponse[clijson.CLIRunDescription]
		err = json.Unmarshal(buf.Bytes(), &resp)
		assert.NoError(t, err)

		assert.Equal(t, expectedPaths, resp.Data.HostSourceCodePaths.Paths)
	})

	t.Run("shows detailed target info in the expected format when run id specified", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)

		runID := "abcdef123456"
		expectedName := "nameToSearch"
		expectedTargetName := "a_target"
		expectedEngineVersion := "1.2.3"
		Run := clijson.CLIRunDescription{
			ID:            runID,
			Name:          expectedName,
			EngineVersion: expectedEngineVersion,
			TargetName:    expectedTargetName,
			Target: clijson.CLITarget{
				JSONTarget: engine_target.JSONTarget{
					Value: &engine_target.JSONSSHTarget{
						Jumps: []engine_target.JSONSSHHostConfig{
							{
								Host:               "a_host",
								Port:               1234,
								Username:           "nobody",
								PrivateKeyFilename: "definitely/a/real/file",
							},
						},
					},
				},
			}}

		// Prepare the request struct
		runExtrasRequest := []apapproto.StandardRunDescriptionExtras{
			apapproto.StandardRunDescriptionExtras_EXTRA_TARGET_INFO,
		}

		runDesc := &apapproto.GetRunDescriptionRequest{
			Id:               &apapproto.RunId{Value: runID},
			ExtrasRequestStd: runExtrasRequest,
		}

		info := mocks.MockInfo{}
		info.On("ListRun", client, runDesc).Return(Run, nil)

		cmd := NewInfoCmd(cc, &info)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		test.SetViperJSON(t, true)
		cmd.SetArgs([]string{runID})
		fmt.Println(cmd.Args)
		err := cmd.Execute()

		assert.NoError(t, err)

		var unmarshalled clijson.CliJSONResponse[clijson.CLIRunDescription]
		err = json.Unmarshal(cmdBuf.Bytes(), &unmarshalled)
		assert.NoError(t, err)

		ngin, err := engine_target.EngineTargetFromJSON(unmarshalled.Data.Target.JSONTarget)
		require.NoError(t, err)

		expectedTarget := engine_target.SSHTarget{
			Jumps: []engine_target.SSHHostConfig{
				{
					Host:               "a_host",
					Port:               1234,
					Username:           "nobody",
					PrivateKeyFilename: "definitely/a/real/file",
				},
			}}

		assert.Equal(t, &expectedTarget, ngin)
		assert.Contains(t, cmdBuf.String(), runID)
		assert.Equal(t, expectedEngineVersion, unmarshalled.Data.EngineVersion)
		assert.Equal(t, expectedTargetName, unmarshalled.Data.TargetName)
	})

	t.Run("shows detailed target info without JSON output", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)

		runID := "abcdef123456"
		expectedName := "nameToSearch"
		expectedTargetName := "a_target"
		expectedEngineVersion := "1.2.3"
		Run := clijson.CLIRunDescription{
			ID:            runID,
			Name:          expectedName,
			EngineVersion: expectedEngineVersion,
			TargetName:    expectedTargetName,
			Target: clijson.CLITarget{
				JSONTarget: engine_target.JSONTarget{
					Value: &engine_target.JSONSSHTarget{
						Jumps: []engine_target.JSONSSHHostConfig{
							{
								Host:               "a_host",
								Port:               1234,
								Username:           "nobody",
								PrivateKeyFilename: "definitely/a/real/file",
							},
						},
					},
				},
			}}

		// Prepare the request struct
		runExtrasRequest := []apapproto.StandardRunDescriptionExtras{
			apapproto.StandardRunDescriptionExtras_EXTRA_TARGET_INFO,
		}

		runDesc := &apapproto.GetRunDescriptionRequest{
			Id:               &apapproto.RunId{Value: runID},
			ExtrasRequestStd: runExtrasRequest,
		}

		info := mocks.MockInfo{}
		info.On("ListRun", client, runDesc).Return(Run, nil)

		cmd := NewInfoCmd(cc, &info)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		test.SetViperJSON(t, false)
		cmd.SetArgs([]string{runID})
		err := cmd.Execute()

		assert.NoError(t, err)
		output := cmdBuf.String()
		assert.Contains(t, output, runID)
		assert.Contains(t, output, expectedEngineVersion)
		assert.Contains(t, output, expectedTargetName)
		assert.Contains(t, output, "a_host")
	})

	t.Run("returns error when client connector fails", func(t *testing.T) {
		serverAddress := "127.0.0.1:50051"
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(nil, message.New(message.EngineGrpcconnectionCreateClient).
			WithMetadata(map[string]string{"serverAddress": serverAddress}))

		cmd := NewInfoCmd(cc, &mocks.MockInfo{})
		cmd.SetArgs([]string{"abcdef123456"})
		err := cmd.Execute()
		require.Error(t, err)

		var m message.Message
		require.True(t, errors.As(err, &m))
		metadata := map[string]string{"serverAddress": serverAddress}
		expectedErr := message.New(message.EngineGrpcconnectionCreateClient).WithMetadata(metadata)
		assert.Equal(t, expectedErr, err)
		assert.Equal(t, serverAddress, m.Metadata()["serverAddress"])
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})

	t.Run("returns error when failing to list a run", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)

		runName := "123"
		runID := &apapproto.RunId{Value: runName}

		// Prepare the request struct
		runExtrasRequest := []apapproto.StandardRunDescriptionExtras{
			apapproto.StandardRunDescriptionExtras_EXTRA_TARGET_INFO,
		}

		runDesc := &apapproto.GetRunDescriptionRequest{
			Id:               runID,
			ExtrasRequestStd: runExtrasRequest,
		}

		info := &mocks.MockInfo{}
		info.On("ListRun", client, runDesc).Return(clijson.CLIRunDescription{},
			message.New(message.EngineRunDoesNotExist).WithMetadata(map[string]string{"runID": runName}))

		cmd := NewInfoCmd(cc, info)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		cmd.SetArgs([]string{runName})
		err := cmd.Execute()
		require.Error(t, err)

		var m message.Message
		require.True(t, errors.As(err, &m))
		expectedErr := message.New(message.EngineRunDoesNotExist).WithMetadata(map[string]string{"runID": runName})
		assert.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})

	t.Run("returns error on failing to marshal the JSON response", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)

		runID := "json-marshal-fail"
		req := makeRequest(runID)

		bad := clijson.CLITableWithDescription{
			Description: []string{"value"},
			Chunk: []map[string]interface{}{
				// Causes marshal error
				{"value": math.NaN()},
			},
		}
		desc := clijson.CLIRunDescription{
			ID:             runID,
			Name:           "anything",
			RendererOutput: map[string]clijson.CLITableWithDescription{"bad": bad},
		}

		_, marshalCause := json.Marshal(desc)
		require.Error(t, marshalCause)

		svc := &mocks.MockInfo{}
		svc.On("ListRun", client, req).Return(desc, nil)

		_, cmd := makeCmdJSON(t, cc, svc, runID)

		err := cmd.Execute()
		require.Error(t, err)
		expectedErr := message.New(message.CliCmdCommonJsonMarshalFailed).WithCause(marshalCause)
		assert.Equal(t, expectedErr, err)
	})

	t.Run("prints inline error when target parse fails", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)

		runID := "bad-target"
		desc := clijson.CLIRunDescription{
			ID:   runID,
			Name: "badTarget",
			Target: clijson.CLITarget{
				JSONTarget: engine_target.JSONTarget{Value: nil},
			},
		}
		req := makeRequest(runID)

		svc := &mocks.MockInfo{}
		svc.On("ListRun", client, req).Return(desc, nil)

		cmd := NewInfoCmd(cc, svc)
		buf := &bytes.Buffer{}
		cmd.SetOut(buf)
		test.SetViperJSON(t, false)
		cmd.SetArgs([]string{runID})

		err := cmd.Execute()
		require.NoError(t, err)

		out := buf.String()
		assert.Contains(t, out, "<ERROR: unknown target type: nil>")
	})

	t.Run("shows workload working directory and environment variables used", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)

		runID := "abcdef123456"
		expectedName := "nameToSearch"
		Run := clijson.CLIRunDescription{
			ID:           runID,
			Name:         expectedName,
			WorkloadType: "Launch",
			WorkingDir:   "/abc/123",
			Env:          map[string]string{"FOO": "bar", "BAZ": "abc"},
		}

		runExtrasRequest := []apapproto.StandardRunDescriptionExtras{
			apapproto.StandardRunDescriptionExtras_EXTRA_TARGET_INFO,
		}

		runDesc := &apapproto.GetRunDescriptionRequest{
			Id:               &apapproto.RunId{Value: runID},
			ExtrasRequestStd: runExtrasRequest,
		}

		info := mocks.MockInfo{}
		info.On("ListRun", client, runDesc).Return(Run, nil)

		cmd := NewInfoCmd(cc, &info)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		test.SetViperJSON(t, false)
		cmd.SetArgs([]string{runID})
		err := cmd.Execute()

		assert.NoError(t, err)
		output := cmdBuf.String()
		assert.Contains(t, output, runID)
		assert.Contains(t, output, "Working Directory: /abc/123")
		assert.Contains(t, output, "Environment Variables:")
		assert.Contains(t, output, "- FOO: bar")
		assert.Contains(t, output, "- BAZ: abc")
	})

	t.Run("doesn't shows working directory and environment variables used if workload type is non-launch", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)

		runID := "abcdef123456"
		expectedName := "nameToSearch"
		Run := clijson.CLIRunDescription{
			ID:           runID,
			Name:         expectedName,
			WorkloadType: "abc",
			WorkingDir:   "/abc/123",
			Env:          map[string]string{"FOO": "bar", "BAZ": "abc"},
		}

		runExtrasRequest := []apapproto.StandardRunDescriptionExtras{
			apapproto.StandardRunDescriptionExtras_EXTRA_TARGET_INFO,
		}

		runDesc := &apapproto.GetRunDescriptionRequest{
			Id:               &apapproto.RunId{Value: runID},
			ExtrasRequestStd: runExtrasRequest,
		}

		info := mocks.MockInfo{}
		info.On("ListRun", client, runDesc).Return(Run, nil)

		cmd := NewInfoCmd(cc, &info)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		test.SetViperJSON(t, false)
		cmd.SetArgs([]string{runID})
		err := cmd.Execute()

		assert.NoError(t, err)
		output := cmdBuf.String()
		assert.Contains(t, output, runID)
		assert.NotContains(t, output, "Working Directory: /abc/123")
		assert.NotContains(t, output, "Environment Variables:")
		assert.NotContains(t, output, "- FOO: bar")
		assert.NotContains(t, output, "- BAZ: abc")
	})

	t.Run("does not print empty renderer table names", func(t *testing.T) {
		output := runInfoText(t, clijson.CLIRunDescription{
			ID:        "empty-renderer-output",
			RunResult: "success",
			Target: clijson.CLITarget{
				JSONTarget: engine_target.JSONTarget{Value: &engine_target.JSONLocalTarget{}},
			},
			RendererOutput: map[string]clijson.CLITableWithDescription{
				"empty_table": {
					Description: []string{"value"},
					Chunk:       []map[string]interface{}{},
				},
			},
		})

		assert.NotContains(t, output, "empty_table")
	})

	t.Run("prints compact target info extras", func(t *testing.T) {
		output := runInfoText(t, clijson.CLIRunDescription{
			ID:        "target-info-summary",
			RunResult: "success",
			Target: clijson.CLITarget{
				JSONTarget: engine_target.JSONTarget{Value: &engine_target.JSONLocalTarget{}},
			},
			RendererOutput: map[string]clijson.CLITableWithDescription{
				"target_info_os": {
					Description: []string{"os_family", "os_description", "kernel_version"},
					Chunk: []map[string]interface{}{{
						"os_family":      1,
						"os_description": "Ubuntu 24.04 LTS",
						"kernel_version": "6.8.0",
					}},
				},
				"target_info_cluster": {
					Description: []string{"id", "name"},
					Chunk: []map[string]interface{}{
						{"id": 0, "name": "Cluster 0"},
						{"id": 1, "name": "Cluster 1"},
						{"id": 2, "name": "Cluster 2"},
					},
				},
				"target_info_cpus": {
					Description: []string{"core_number", "cluster_id", "midr", "name"},
					Chunk: []map[string]interface{}{
						{"core_number": 0, "cluster_id": 0, "midr": "0x001", "name": "Cortex-A"},
						{"core_number": 1, "cluster_id": 0, "midr": "0x001", "name": "Cortex-A"},
						{"core_number": 2, "cluster_id": 1, "midr": "0x001", "name": "Cortex-A"},
						{"core_number": 3, "cluster_id": 2, "midr": "0x002", "name": "Cortex-B"},
					},
				},
			},
		})

		assert.Contains(t, output, "Ubuntu 24.04 LTS")
		assert.Contains(t, output, "Kernel Version: 6.8.0")
		assert.Contains(t, output, "Cortex-A")
		assert.Contains(t, output, "Cortex-B")
		assert.Contains(t, output, "MIDR 0x001")
		assert.Contains(t, output, "MIDR 0x002")
		assert.NotContains(t, output, "core_number")
		assert.NotContains(t, output, "target_info_cpus")
		assert.NotContains(t, output, "target_info_cluster")
	})

	t.Run("orders CPU summary entries by name, MIDR, then cluster", func(t *testing.T) {
		output := runInfoText(t, clijson.CLIRunDescription{
			ID:        "ordered-cpu-summary",
			RunResult: "success",
			Target: clijson.CLITarget{
				JSONTarget: engine_target.JSONTarget{Value: &engine_target.JSONLocalTarget{}},
			},
			RendererOutput: map[string]clijson.CLITableWithDescription{
				"target_info_cpus": {
					Description: []string{"core_number", "cluster_id", "midr", "name"},
					Chunk: []map[string]interface{}{
						{"core_number": 0, "cluster_id": 0, "midr": "0x002", "name": "Cortex-A"},
						{"core_number": 1, "cluster_id": 0, "midr": "0x001", "name": "Cortex-B"},
						{"core_number": 2, "cluster_id": 1, "midr": "0x001", "name": "Cortex-A"},
						{"core_number": 3, "cluster_id": 0, "midr": "0x001", "name": "Cortex-A"},
					},
				},
			},
		})

		first := strings.Index(output, "Cortex-A: 1 core, MIDR 0x001, clusterID 0")
		second := strings.Index(output, "Cortex-A: 1 core, MIDR 0x001, clusterID 1")
		third := strings.Index(output, "Cortex-A: 1 core, MIDR 0x002, clusterID 0")
		fourth := strings.Index(output, "Cortex-B: 1 core, MIDR 0x001, clusterID 0")

		require.NotEqual(t, -1, first)
		require.NotEqual(t, -1, second)
		require.NotEqual(t, -1, third)
		require.NotEqual(t, -1, fourth)
		assert.Less(t, first, second)
		assert.Less(t, second, third)
		assert.Less(t, third, fourth)
	})
}
