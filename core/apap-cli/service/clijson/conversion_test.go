// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package clijson

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Arm-Debug/apap-cli/apap-cli/service/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	engine_target "github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

type unknownJSONTarget struct{}

func (unknownJSONTarget) Type() engine_target.TargetType {
	return "unknown"
}

func TestCLIRunDescriptionFromProto(t *testing.T) {
	sourceCodePaths := []string{"/foo", "/bar/baz"}
	sourceCode := &apapproto.HostSourceCodePaths{Paths: sourceCodePaths}
	mockDesc := apapproto.RunDescription{
		Metadata: &apapproto.RunMetadata{
			Name:       "abcd1234",
			TargetName: "named_target",
			Target: &apapproto.Target{
				Connection: &apapproto.Target_SshConfig{
					SshConfig: &apapproto.SSHConnectionConfig{
						Hosts: []*apapproto.SSHHostConfig{
							{},
						},
						HostKeyPolicy: apapproto.SSHHostKeyPolicy_SSH_HOST_KEY_POLICY_ADD_IF_MISSING.Enum(),
					},
				},
			},
		},
		HostSourceCodePaths: sourceCode,
		Extra:               nil,
	}

	t.Run("sucessfully converts Meta and Extra", func(t *testing.T) {
		// Metadata only
		desc, err := CLIRunDescriptionFromProto("abcd1234", &mockDesc)
		assert.NoError(t, err)
		assert.Equal(t, desc.Name, mockDesc.Metadata.Name)
		assert.Equal(t, "named_target", desc.TargetName)
		assert.Empty(t, desc.RendererOutput)

		// Source code check
		expected := []string{"/foo", "/bar/baz"}
		assert.Equal(t, expected, desc.HostSourceCodePaths.Paths)

		// Metadata + RunExtra_Value
		mockDesc.Extra = []*apapproto.RunExtraOrError{
			{
				Some: &apapproto.RunExtraOrError_Value{
					Value: &apapproto.RunExtra{
						Extra: map[string]*apapproto.TableWithDescription{
							"some_table": {
								Description: &apapproto.TableDescription{
									Columns: &apapproto.Columns{
										Columns: []*apapproto.ColumnDescription{
											{
												Name: "some_column",
											},
										},
									},
								},
								Chunk: &apapproto.StructTableChunk{
									Rows: []*structpb.Struct{
										{
											Fields: map[string]*structpb.Value{
												"some_field": structpb.NewBoolValue(false),
											},
										},
									},
								},
							},
						},
					},
				},
			},
		}

		desc, err = CLIRunDescriptionFromProto("abcd1234", &mockDesc)
		assert.NoError(t, err)
		assert.Contains(t, desc.RendererOutput, "some_table")

		// Metadata + RunExtra_Error
		mockDesc.Extra = []*apapproto.RunExtraOrError{
			{
				Some: &apapproto.RunExtraOrError_Error{
					Error: &apapproto.Error{
						Message: "some reason",
					},
				},
			},
		}

		desc, err = CLIRunDescriptionFromProto("abcd1234", &mockDesc)
		assert.NoError(t, err)
		assert.Contains(t, desc.RendererOutput, "run_extra_error")
	})
}

func TestCLIRunSummaryFromRunDescription(t *testing.T) {
	t.Run("converts display fields and target host", func(t *testing.T) {
		summary := CLIRunSummaryFromRunDescription(CLIRunDescription{
			ID:         "abcd1234",
			Name:       "example run",
			StartTime:  "2026-05-08T10:00:00Z",
			EndTime:    "2026-05-08T10:01:00Z",
			RecipeName: "cpu_hotspots",
			Cmdline:    "/path/to/test_case_03 --iterations 10",
			RunResult:  "success",
			Target: CLITarget{
				JSONTarget: engine_target.JSONTarget{
					Value: &engine_target.JSONSSHTarget{
						Jumps: []engine_target.JSONSSHHostConfig{
							{Host: "1.2.3.4", Port: 22, Username: "jump"},
							{Host: "5.6.7.8", Port: 2222, Username: "final_target"},
						},
					},
				},
			},
		})

		assert.Equal(t, CLIRunSummary{
			ID:         "abcd1234",
			Name:       "example run",
			StartTime:  "2026-05-08T10:00:00Z",
			EndTime:    "2026-05-08T10:01:00Z",
			RecipeName: "cpu_hotspots",
			Cmdline:    "/path/to/test_case_03 --iterations 10",
			RunResult:  "success",
			Target:     "final_target@5.6.7.8:2222",
		}, summary)
	})

	t.Run("uses list error as display name", func(t *testing.T) {
		summary := CLIRunSummaryFromRunDescription(CLIRunDescription{
			ID:               "bad-run",
			LoadErrorMessage: "failed to read manifest",
		})

		assert.Equal(t, "bad-run", summary.ID)
		assert.Equal(t, "failed to read manifest", summary.Name)
		assert.Empty(t, summary.Cmdline)
		assert.Empty(t, summary.Target)
	})

	t.Run("formats an Android package activity as the workload summary", func(t *testing.T) {
		summary := CLIRunSummaryFromRunDescription(CLIRunDescription{
			WorkloadType:        "Android Launch",
			AndroidPackageName:  "com.example.app",
			AndroidActivityName: ".MainActivity",
		})

		assert.Equal(t, "com.example.app/.MainActivity", summary.Cmdline)
	})

	t.Run("uses target conversion error as display target", func(t *testing.T) {
		summary := CLIRunSummaryFromRunDescription(CLIRunDescription{
			ID:   "bad-target",
			Name: "bad target run",
			Target: CLITarget{
				JSONTarget: engine_target.JSONTarget{
					Value: unknownJSONTarget{},
				},
			},
		})

		assert.Equal(t, "bad-target", summary.ID)
		assert.Contains(t, summary.Target, "<ERROR: unknown target type:")
	})
}

func TestCLIRunDescriptionFromProtoIncludesAndroidLaunch(t *testing.T) {
	desc, err := CLIRunDescriptionFromProto("android-run", &apapproto.RunDescription{
		Metadata: &apapproto.RunMetadata{
			WorkloadType: "Android Launch",
			Target: &apapproto.Target{
				Connection: &apapproto.Target_LocalConfig{
					LocalConfig: &apapproto.LocalConnectionConfig{},
				},
			},
			AndroidLaunchWorkload: &apapproto.AndroidLaunchWorkload{
				PackageName:  "com.example.app",
				ActivityName: ".MainActivity",
			},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "com.example.app", desc.AndroidPackageName)
	assert.Equal(t, ".MainActivity", desc.AndroidActivityName)
}

func TestCLIRunSummaryListingFromRunListing(t *testing.T) {
	listing := CLIRunSummaryListingFromRunListing(CLIRunListing{
		Runs: []CLIRunDescription{
			{
				ID:      "first-run",
				Name:    "first run",
				Cmdline: "/path/to/first_workload",
			},
			{
				ID:   "second-run",
				Name: "second run",
			},
		},
	})

	assert.Equal(t, CLIRunSummaryListing{
		Runs: []CLIRunSummary{
			{ID: "first-run", Name: "first run", Cmdline: "/path/to/first_workload"},
			{ID: "second-run", Name: "second run"},
		},
	}, listing)
}

func TestSortCLIRunSummariesNewestFirst(t *testing.T) {
	runs := []CLIRunSummary{
		{ID: "oldest", StartTime: "2026-05-08T09:00:00Z"},
		{ID: "missing-start-time"},
		{ID: "newest", StartTime: "2026-05-08T10:00:00Z"},
	}

	SortCLIRunSummariesNewestFirst(runs)

	assert.Equal(t, []CLIRunSummary{
		{ID: "newest", StartTime: "2026-05-08T10:00:00Z"},
		{ID: "oldest", StartTime: "2026-05-08T09:00:00Z"},
		{ID: "missing-start-time"},
	}, runs)
}

func TestTestTargetResponseToJSON(t *testing.T) {
	orig := LookupMsg
	t.Cleanup(func() { LookupMsg = orig })
	t.Run("conversion preserves all information", func(t *testing.T) {
		LookupMsg = func(err error) (*message.CatalogMessage, error) {
			return &message.CatalogMessage{
				Code:        "engine.conductor.ssh.MISSING_KEY_FOR_JUMP",
				Severity:    "Error",
				Message:     "FromCatalog",
				Explanation: "X happened",
				Advice:      "Do Y",
			}, nil
		}

		err1 := errors.New("test error")
		err2 := message.New(message.EngineConductorSshMissingKeyForJump).WithCause(err1).WithMetadata(map[string]string{"jumpNode": "madeUp"})
		conn := target.TargetTestConnection{
			ConnectionStatus: apapproto.ConnectionStatus_CONNECTION_STATUS_ERROR,
			Error:            err2,
		}

		response := target.TestTargetResponse{
			ConnectionStatus: conn,
		}

		converted := TestTargetResponseToJSON(response)
		assert.Equal(t, apapproto.ConnectionStatus_CONNECTION_STATUS_ERROR, converted.ConnectionStatus.ConnectionStatus)
		assert.Equal(t, "engine.conductor.ssh.MISSING_KEY_FOR_JUMP", converted.ConnectionStatus.Error.Code)
		assert.Equal(t, "madeUp", converted.ConnectionStatus.Error.Metadata["jumpNode"])
		assert.Equal(t, "test error", converted.ConnectionStatus.Error.Children[0].Message)
	})
}

func TestValidateParamsResponseToJSON(t *testing.T) {
	t.Run("conversion preserves all information", func(t *testing.T) {
		msg1 := message.New(message.EngineRecipeparserJsRecipeStageInvalidRadioValue).WithMetadata(map[string]string{"value": "invalid1"})
		msg2 := message.New(message.EngineRecipeparserJsRecipeStageInvalidSingleSelectValue).WithMetadata(map[string]string{"value": "invalid2"})

		res1 := &apapproto.ParameterValidationResult{
			ParameterId: "radioParam",
			Message:     message.BuildErrorChain(msg1),
		}
		res2 := &apapproto.ParameterValidationResult{
			ParameterId: "selectParam",
			Message:     message.BuildErrorChain(msg2),
		}

		response := &apapproto.RecipeValidateParametersResponse{Messages: []*apapproto.ParameterValidationResult{res1, res2}}

		converted := ValidateParamsResponseToJSON(response)
		assert.Equal(t, 2, len(converted.Messages))
		convMsg1 := converted.Messages[0]
		convMsg2 := converted.Messages[1]

		assert.Equal(t, res1.ParameterId, convMsg1.ParameterId)
		assert.Equal(t, BuildErrorTree(msg1), convMsg1.Message)
		assert.Equal(t, res2.ParameterId, convMsg2.ParameterId)
		assert.Equal(t, BuildErrorTree(msg2), convMsg2.Message)
	})
}

func TestDeleteRunsResponseToJSON(t *testing.T) {
	err1 := message.New(message.EngineRunDoesNotExist)
	err2 := errors.New("what a random failure")
	testCases := []struct {
		name     string
		input    *apapproto.DeleteRunsResponse
		expected RunDeletionStatusesJSON
	}{
		{
			name:     "handles nil response",
			input:    nil,
			expected: RunDeletionStatusesJSON{},
		},
		{
			name: "converts response correctly",
			input: &apapproto.DeleteRunsResponse{Statuses: []*apapproto.RunDeletionStatus{
				{Id: "a", Error: nil},
				{Id: "c", Error: message.BuildErrorChain(err1)},
				{Id: "b", Error: message.BuildErrorChain(err2)},
				{Id: "d", Error: message.BuildErrorChain(nil)},
			}},
			expected: RunDeletionStatusesJSON{Statuses: []RunDeletionStatusJSON{
				{ID: "a", Error: nil},
				{ID: "c", Error: BuildErrorTree(err1)},
				{ID: "b", Error: BuildErrorTree(err2)},
				{ID: "d", Error: nil},
			}},
		},
		{
			name:     "handles empty response",
			input:    &apapproto.DeleteRunsResponse{Statuses: []*apapproto.RunDeletionStatus{}},
			expected: RunDeletionStatusesJSON{Statuses: []RunDeletionStatusJSON{}},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got := DeleteRunsResponseToJSON(testCase.input)
			assert.Equal(t, testCase.expected, got)
		})
	}
}
