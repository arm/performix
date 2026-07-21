// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipe

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/proto"

	"github.com/Arm-Debug/apap-cli/apap-engine/util"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

func TestConvertRecipeStruct(t *testing.T) {
	tests := []struct {
		name          string
		targetSupport bool
		input         *apapproto.ParseRecipeResponse
		expected      TargetSupportJSON
	}{
		{
			name:          "Supported",
			targetSupport: true,
			input: &apapproto.ParseRecipeResponse{
				Name: "Test",
				PlatformSupport: []*apapproto.PlatformSupport{
					{
						Platform: &apapproto.PlatformConfiguration{
							Arch: "x86_64",
							Os:   "Linux",
						},
						Result: apapproto.PlatformSupportResult_PLATFORM_SUPPORTED,
					},
				},
			},
			expected: TargetSupportJSON{Result: "supported"},
		},
		{
			name:          "Unsupported",
			targetSupport: true,
			input: &apapproto.ParseRecipeResponse{
				Name: "Test",
				PlatformSupport: []*apapproto.PlatformSupport{
					{
						Platform: &apapproto.PlatformConfiguration{
							Arch: "x86_64",
							Os:   "Linux",
						},
						Result: apapproto.PlatformSupportResult_PLATFORM_UNSUPPORTED,
					},
				},
			},
			expected: TargetSupportJSON{Result: "unsupported"},
		},
		{
			name:          "Conditionally Supported with conditions",
			targetSupport: true,
			input: &apapproto.ParseRecipeResponse{
				Name: "Test",
				PlatformSupport: []*apapproto.PlatformSupport{
					{
						Platform: &apapproto.PlatformConfiguration{
							Arch: "x86_64",
							Os:   "Linux",
						},
						Result: apapproto.PlatformSupportResult_PLATFORM_CONDITIONALLY_SUPPORTED,
						ConditionList: []*apapproto.PlatformSupportRequirementSpec{
							{
								Type: "os",
								Parameters: map[string]string{
									"param1": "val1",
									"param2": "val2",
								},
							},
						},
					},
				},
			},
			expected: TargetSupportJSON{
				Result: "conditionally_supported",
				Conditions: []RequirementSpecJSON{
					{
						Type: "os",
						Parameters: map[string]string{
							"param1": "val1",
							"param2": "val2",
						},
					},
				},
			},
		},
		{
			name:          "Unknown",
			targetSupport: true,
			input: &apapproto.ParseRecipeResponse{
				Name: "Test",
				PlatformSupport: []*apapproto.PlatformSupport{
					{
						Platform: &apapproto.PlatformConfiguration{
							Arch: "x86_64",
							Os:   "Linux",
						},
						Result: 99,
					},
				},
			},
			expected: TargetSupportJSON{Result: "unknown"},
		},
		{
			name:          "Nil PlatformSupport",
			targetSupport: true,
			input: &apapproto.ParseRecipeResponse{
				Name: "Test",
			},
			expected: TargetSupportJSON{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := ConvertRecipeStruct(tt.input, tt.targetSupport)
			assert.Equal(t, tt.expected, out.TargetSupport)
		})
	}
	t.Run("includes MCP guidance in JSON mode", func(t *testing.T) {
		input := &apapproto.ParseRecipeResponse{
			Name:        "asct",
			McpGuidance: proto.String("Use timeout 600 for default benchmark runs."),
		}
		out := ConvertRecipeStruct(input, false)
		assert.Equal(t, "Use timeout 600 for default benchmark runs.", out.MCPGuidance)
	})
	t.Run("displays multiple platforms in non-target mode", func(t *testing.T) {
		input := &apapproto.ParseRecipeResponse{
			Name: "Test",
			PlatformSupport: []*apapproto.PlatformSupport{
				{
					Platform: &apapproto.PlatformConfiguration{
						Arch: "x86_64",
						Os:   "Linux",
					},
					Result: apapproto.PlatformSupportResult_PLATFORM_SUPPORTED,
				},
				{
					Platform: &apapproto.PlatformConfiguration{
						Arch: "aarch64",
						Os:   "Linux",
					},
					Result: apapproto.PlatformSupportResult_PLATFORM_SUPPORTED,
				},
				{
					Platform: &apapproto.PlatformConfiguration{
						Arch: "aarch64",
						Os:   "Windows",
					},
					Result: apapproto.PlatformSupportResult_PLATFORM_UNSUPPORTED,
				},
			},
		}
		out := ConvertRecipeStruct(input, false)
		expected := []SupportedPlatformsJSON{
			{
				Platform:   PlatformConfigurationJSON{Architecture: "x86_64", OS: "Linux"},
				Result:     "supported",
				Conditions: []RequirementSpecJSON(nil),
			},
			{
				Platform:   PlatformConfigurationJSON{Architecture: "aarch64", OS: "Linux"},
				Result:     "supported",
				Conditions: []RequirementSpecJSON(nil),
			},
		}
		assert.Equal(t, expected, out.SupportedPlatforms)
	})
	t.Run("preserves conditions for supported platforms", func(t *testing.T) {
		input := &apapproto.ParseRecipeResponse{
			Name: "Test",
			PlatformSupport: []*apapproto.PlatformSupport{
				{
					Platform: &apapproto.PlatformConfiguration{
						Arch: "aarch64",
						Os:   "Linux",
					},
					Result: apapproto.PlatformSupportResult_PLATFORM_CONDITIONALLY_SUPPORTED,
					ConditionList: []*apapproto.PlatformSupportRequirementSpec{
						{
							Type:       "kernel_driver",
							Parameters: map[string]string{"driver": "arm_spe_pmu"},
						},
					},
				},
			},
		}
		out := ConvertRecipeStruct(input, false)
		expected := []SupportedPlatformsJSON{
			{
				Platform: PlatformConfigurationJSON{Architecture: "aarch64", OS: "Linux"},
				Result:   "conditionally_supported",
				Conditions: []RequirementSpecJSON{
					{
						Type:       "kernel_driver",
						Parameters: map[string]string{"driver": "arm_spe_pmu"},
					},
				},
			},
		}
		assert.Equal(t, expected, out.SupportedPlatforms)
	})
}

func TestConvertRecipeStructParameters(t *testing.T) {
	tests := []struct {
		name     string
		input    *apapproto.ParseRecipeResponse
		expected RecipeJSON
	}{
		{
			name: "Input Parameter",
			input: &apapproto.ParseRecipeResponse{
				Parameters: []*apapproto.ParameterList{
					{Parameter: &apapproto.ParameterList_Input{Input: &apapproto.InputParameter{Base: &apapproto.RecipeParameterBase{ID: "input1"}}}},
				},
			},
			expected: RecipeJSON{Parameters: []ParameterJSON{{ParameterBaseJSON: ParameterBaseJSON{ID: "input1"}, Config: ParameterConfigJSON{Type: "input", DefaultValue: ""}}}},
		},
		{
			name: "Checkbox Parameter",
			input: &apapproto.ParseRecipeResponse{
				Parameters: []*apapproto.ParameterList{
					{Parameter: &apapproto.ParameterList_Checkbox{Checkbox: &apapproto.CheckboxParameter{Base: &apapproto.RecipeParameterBase{ID: "check1"}, DefaultValue: true}}},
				},
			},
			expected: RecipeJSON{Parameters: []ParameterJSON{{ParameterBaseJSON: ParameterBaseJSON{ID: "check1"}, Config: ParameterConfigJSON{Type: "checkbox", DefaultValue: true}}}},
		},
		{
			name: "Multi Select Parameter",
			input: &apapproto.ParseRecipeResponse{
				Parameters: []*apapproto.ParameterList{
					{Parameter: &apapproto.ParameterList_MultiSelect{
						MultiSelect: &apapproto.MultiSelectParameter{
							Base:         &apapproto.RecipeParameterBase{ID: "select1"},
							DefaultValue: []string{"a", "b"},
						},
					}},
				},
			},
			expected: RecipeJSON{Parameters: []ParameterJSON{{ParameterBaseJSON: ParameterBaseJSON{ID: "select1"}, Config: ParameterConfigJSON{Type: "multi_select", DefaultValue: []string{"a", "b"}}}}},
		},
		{
			name: "Single Select Parameter",
			input: &apapproto.ParseRecipeResponse{
				Parameters: []*apapproto.ParameterList{
					{Parameter: &apapproto.ParameterList_SingleSelect{
						SingleSelect: &apapproto.SingleSelectParameter{
							Base:         &apapproto.RecipeParameterBase{ID: "select2"},
							DefaultValue: "a",
						},
					}},
				},
			},
			expected: RecipeJSON{Parameters: []ParameterJSON{{ParameterBaseJSON: ParameterBaseJSON{ID: "select2"}, Config: ParameterConfigJSON{Type: "single_select", DefaultValue: "a"}}}},
		},
		{
			name: "Single Select Parameter With Option Items",
			input: &apapproto.ParseRecipeResponse{
				Parameters: []*apapproto.ParameterList{
					{Parameter: &apapproto.ParameterList_SingleSelect{SingleSelect: &apapproto.SingleSelectParameter{
						Base:         &apapproto.RecipeParameterBase{ID: "select1"},
						DefaultValue: "normal",
						Options: []*apapproto.ParameterOption{
							{Value: "low", Label: "Low"},
							{Value: "normal", Label: "Normal", Description: util.Ptr("Recommended")},
							{Value: "high", Label: "High"},
						},
					}}},
				},
			},
			expected: RecipeJSON{Parameters: []ParameterJSON{{
				ParameterBaseJSON: ParameterBaseJSON{ID: "select1"},
				Config: ParameterConfigJSON{
					Type:    "single_select",
					Options: []string{"low", "normal", "high"},
					OptionItems: []ParameterOptionJSON{
						{Value: "low", Label: "Low"},
						{Value: "normal", Label: "Normal", Description: "Recommended"},
						{Value: "high", Label: "High"},
					},
					DefaultValue: "normal",
				},
			}}},
		},
		{
			name: "Radio Parameter",
			input: &apapproto.ParseRecipeResponse{
				Parameters: []*apapproto.ParameterList{
					{Parameter: &apapproto.ParameterList_Radio{Radio: &apapproto.RadioParameter{Base: &apapproto.RecipeParameterBase{ID: "radio1"}}}},
				},
			},
			expected: RecipeJSON{Parameters: []ParameterJSON{{ParameterBaseJSON: ParameterBaseJSON{ID: "radio1"}, Config: ParameterConfigJSON{Type: "radio", DefaultValue: ""}}}},
		},
		{
			name: "Radio Parameter With Option Items",
			input: &apapproto.ParseRecipeResponse{
				Parameters: []*apapproto.ParameterList{
					{Parameter: &apapproto.ParameterList_Radio{Radio: &apapproto.RadioParameter{
						Base:         &apapproto.RecipeParameterBase{ID: "radio1"},
						DefaultValue: "normal",
						Options: []*apapproto.ParameterOption{
							{Value: "low", Label: "Low"},
							{Value: "normal", Label: "Normal", Description: util.Ptr("Recommended")},
							{Value: "high", Label: "High"},
						},
					}}},
				},
			},
			expected: RecipeJSON{Parameters: []ParameterJSON{{
				ParameterBaseJSON: ParameterBaseJSON{ID: "radio1"},
				Config: ParameterConfigJSON{
					Type:    "radio",
					Options: []string{"low", "normal", "high"},
					OptionItems: []ParameterOptionJSON{
						{Value: "low", Label: "Low"},
						{Value: "normal", Label: "Normal", Description: "Recommended"},
						{Value: "high", Label: "High"},
					},
					DefaultValue: "normal",
				},
			}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := ConvertRecipeStruct(tt.input, false)
			assert.Equal(t, tt.expected, out)
		})
	}
}

func TestConvertRecipeStructRenderParameters(t *testing.T) {
	tests := []struct {
		name     string
		input    *apapproto.ParseRecipeResponse
		expected RecipeJSON
	}{
		{
			name: "Number Render Parameter",
			input: &apapproto.ParseRecipeResponse{
				RenderParameters: []*apapproto.RenderParameter{
					{
						Id:      "thread_id",
						Type:    apapproto.RenderParameterType_RENDER_PARAMETER_TYPE_NUMBER,
						IsArray: false,
					},
				},
			},
			expected: RecipeJSON{RenderParameters: []RenderParameterJSON{{ID: "thread_id", Type: "number", IsArray: false}}},
		},
		{
			name: "String Render Parameter",
			input: &apapproto.ParseRecipeResponse{
				RenderParameters: []*apapproto.RenderParameter{
					{
						Id:      "thread_name",
						Type:    apapproto.RenderParameterType_RENDER_PARAMETER_TYPE_STRING,
						IsArray: false,
					},
				},
			},
			expected: RecipeJSON{RenderParameters: []RenderParameterJSON{{ID: "thread_name", Type: "string", IsArray: false}}},
		},
		{
			name: "Number Array Render Parameter",
			input: &apapproto.ParseRecipeResponse{
				RenderParameters: []*apapproto.RenderParameter{
					{
						Id:      "time_range",
						Type:    apapproto.RenderParameterType_RENDER_PARAMETER_TYPE_NUMBER,
						IsArray: true,
					},
				},
			},
			expected: RecipeJSON{RenderParameters: []RenderParameterJSON{
				{ID: "time_range", Type: "number", IsArray: true},
			}},
		},
		{
			name: "String Array Render Parameter",
			input: &apapproto.ParseRecipeResponse{
				RenderParameters: []*apapproto.RenderParameter{
					{
						Id:      "thread_names",
						Type:    apapproto.RenderParameterType_RENDER_PARAMETER_TYPE_STRING,
						IsArray: true,
					},
				},
			},
			expected: RecipeJSON{RenderParameters: []RenderParameterJSON{
				{ID: "thread_names", Type: "string", IsArray: true},
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := ConvertRecipeStruct(tt.input, false)
			assert.Equal(t, tt.expected, out)
		})
	}
}
