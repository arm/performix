// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package parameters

import (
	"testing"

	"github.com/dop251/goja"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/apiversion"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
)

func TestExtractRecipeParametersSuccess(t *testing.T) {
	runtime := goja.New()
	selectOptions := func(call goja.FunctionCall) goja.Value {
		return runtime.ToValue([]string{"dynA", "dynB"})
	}

	defs := []ParameterDefinition{
		{
			ID:          "metrics_group",
			Label:       "Metrics group",
			Description: "Select metrics",
			Required:    true,
			Visible_When: []RecipeParameterDependency{{
				ParameterID: "mode",
				Value:       "metrics",
			}},
			Config: map[string]any{
				"type":         "multi_select",
				"defaultValue": []string{"basic", "l1"},
				"options":      []string{"basic", "l1", "l2"},
			},
		},
		{
			ID:          "single_select_test",
			Label:       "Single choice",
			Description: "Choose one value",
			Required:    true,
			Config: map[string]any{
				"type":         "single_select",
				"defaultValue": "balanced",
				"options":      []string{"balanced", "full"},
			},
		},
		{
			ID:          "radio_test",
			Label:       "Radio choice",
			Description: "Choose one",
			Required:    true,
			Config: map[string]any{
				"type":         "radio",
				"defaultValue": "20",
				"options": []any{
					map[string]any{"value": "10", "label": "Low"},
					map[string]any{"value": "20", "label": "Normal"},
					map[string]any{"value": "30", "label": "High", "description": "Most detailed"},
				},
			},
		},
		{
			ID:          "toggle",
			Label:       "Toggle it",
			Description: "Enable behaviour",
			Config: map[string]any{
				"type":         "checkbox",
				"defaultValue": true,
			},
		},
		{
			ID:          "note",
			Label:       "Notes",
			Description: "Optional note",
			Config: map[string]any{
				"type":         "input",
				"defaultValue": "n/a",
				"custom":       map[string]string{"fieldType": "text"},
			},
		},
		{
			ID:          "dynamic_select",
			Label:       "Dynamic",
			Description: "Dynamic options",
			Config: map[string]any{
				"type":         "multi_select",
				"defaultValue": []string{"dynA"},
				"options":      selectOptions,
			},
		},
	}

	radioConfig, ok := defs[2].Config.(map[string]any)
	require.True(t, ok)
	radioOptions, ok := radioConfig["options"].([]any)
	require.True(t, ok)
	require.Len(t, radioOptions, 3)

	firstRadioOption, ok := radioOptions[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "10", firstRadioOption["value"])
	assert.Equal(t, "Low", firstRadioOption["label"])
	assert.NotContains(t, firstRadioOption, "description")

	secondRadioOption, ok := radioOptions[1].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "20", secondRadioOption["value"])
	assert.Equal(t, "Normal", secondRadioOption["label"])
	assert.NotContains(t, secondRadioOption, "description")

	thirdRadioOption, ok := radioOptions[2].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "30", thirdRadioOption["value"])
	assert.Equal(t, "High", thirdRadioOption["label"])
	assert.Equal(t, "Most detailed", thirdRadioOption["description"])

	params, optionFns, err := ExtractRecipeParameters(defs, "test-recipe", apiversion.LegacyRecipeRadioStringOptionsAPIVersion)
	require.NoError(t, err)

	require.Len(t, params.SingleSelect, 1)
	require.Len(t, params.MultiSelect, 2)
	require.Len(t, params.Radio, 1)
	require.Len(t, params.Checkbox, 1)
	require.Len(t, params.Input, 1)

	assert.Equal(t, MultiSelectParameter{
		Parameter: Parameter{
			ID:          "metrics_group",
			Label:       "Metrics group",
			Description: "Select metrics",
			Required:    true,
			Order:       0,
			VisibleWhen: []ParameterDependency{{ParameterID: "mode", Value: "metrics"}},
		},
		DefaultValue: []string{"basic", "l1"},
		Options:      []string{"basic", "l1", "l2"},
		OptionItems: []ParameterOption{
			{Value: "basic", Label: "basic"},
			{Value: "l1", Label: "l1"},
			{Value: "l2", Label: "l2"},
		},
	}, params.MultiSelect[0])

	assert.Equal(t, RadioParameter{
		Parameter: Parameter{
			ID:          "radio_test",
			Label:       "Radio choice",
			Description: "Choose one",
			Required:    true,
			Order:       2,
			VisibleWhen: []ParameterDependency{},
		},
		DefaultValue: "20",
		Options:      []string{"10", "20", "30"},
		OptionItems: []ParameterOption{
			{Value: "10", Label: "Low"},
			{Value: "20", Label: "Normal"},
			{Value: "30", Label: "High", Description: "Most detailed"},
		},
	}, params.Radio[0])

	assert.Equal(t, CheckboxParameter{
		Parameter: Parameter{
			ID:          "toggle",
			Label:       "Toggle it",
			Description: "Enable behaviour",
			Order:       3,
			VisibleWhen: []ParameterDependency{},
		},
		DefaultValue: true,
	}, params.Checkbox[0])

	assert.Equal(t, InputParameter{
		Parameter: Parameter{
			ID:          "note",
			Label:       "Notes",
			Description: "Optional note",
			Order:       4,
			VisibleWhen: []ParameterDependency{},
		},
		DefaultValue: "n/a",
		Custom:       map[string]string{"fieldType": "text"},
	}, params.Input[0])

	assert.Equal(t, SingleSelectParameter{
		Parameter: Parameter{
			ID:          "single_select_test",
			Label:       "Single choice",
			Description: "Choose one value",
			Required:    true,
			Order:       1,
			VisibleWhen: []ParameterDependency{},
		},
		DefaultValue: "balanced",
		Options:      []string{"balanced", "full"},
		OptionItems: []ParameterOption{
			{Value: "balanced", Label: "balanced"},
			{Value: "full", Label: "full"},
		},
	}, params.SingleSelect[0])

	assert.Equal(t, MultiSelectParameter{
		Parameter: Parameter{
			ID:          "dynamic_select",
			Label:       "Dynamic",
			Description: "Dynamic options",
			Order:       5,
			VisibleWhen: []ParameterDependency{},
		},
		DefaultValue: []string{"dynA"},
		Options:      []string{},
		OptionItems:  []ParameterOption{},
	}, params.MultiSelect[1])

	require.Len(t, optionFns, 1)
	assert.Equal(t, "dynamic_select", optionFns[0].ParameterID)
	assert.Equal(t, ParameterConfigTypeMultiSelect, optionFns[0].ParameterType)
	assert.Equal(t, 1, optionFns[0].ParameterIndex)
	require.NotNil(t, optionFns[0].Callback)
}

func TestExtractRenderParameters(t *testing.T) {
	t.Run("accepts scalar number type", func(t *testing.T) {
		defs := []RenderParameterDefinition{
			{
				ID: "thread_id",
				Config: RenderParameterConfigDefinition{
					Type: "number",
				},
			},
		}

		params, err := ExtractRenderParameters(defs, "test-recipe")

		require.NoError(t, err)
		require.Len(t, params, 1)
		assert.Equal(t, RenderParameter{
			ID:      "thread_id",
			Type:    RenderParameterValueTypeNumber,
			IsArray: false,
			Order:   0,
		}, params[0])
	})

	t.Run("accepts scalar string type", func(t *testing.T) {
		defs := []RenderParameterDefinition{
			{
				ID: "thread_name",
				Config: RenderParameterConfigDefinition{
					Type: "string",
				},
			},
		}

		params, err := ExtractRenderParameters(defs, "test-recipe")

		require.NoError(t, err)
		require.Len(t, params, 1)
		assert.Equal(t, RenderParameter{
			ID:      "thread_name",
			Type:    RenderParameterValueTypeString,
			IsArray: false,
			Order:   0,
		}, params[0])
	})

	t.Run("accepts number array type", func(t *testing.T) {
		defs := []RenderParameterDefinition{
			{
				ID: "thread_ids",
				Config: RenderParameterConfigDefinition{
					Type: []any{"number"},
				},
			},
		}

		params, err := ExtractRenderParameters(defs, "test-recipe")

		require.NoError(t, err)
		require.Len(t, params, 1)
		assert.Equal(t, RenderParameter{
			ID:      "thread_ids",
			Type:    RenderParameterValueTypeNumber,
			IsArray: true,
			Order:   0,
		}, params[0])
	})

	t.Run("accepts string array type", func(t *testing.T) {
		defs := []RenderParameterDefinition{
			{
				ID: "thread_names",
				Config: RenderParameterConfigDefinition{
					Type: []any{"string"},
				},
			},
		}

		params, err := ExtractRenderParameters(defs, "test-recipe")

		require.NoError(t, err)
		require.Len(t, params, 1)
		assert.Equal(t, RenderParameter{
			ID:      "thread_names",
			Type:    RenderParameterValueTypeString,
			IsArray: true,
			Order:   0,
		}, params[0])
	})

	t.Run("preserves declaration order", func(t *testing.T) {
		defs := []RenderParameterDefinition{
			{
				ID: "thread_id",
				Config: RenderParameterConfigDefinition{
					Type: "number",
				},
			},
			{
				ID: "thread_names",
				Config: RenderParameterConfigDefinition{
					Type: []any{"string"},
				},
			},
		}

		params, err := ExtractRenderParameters(defs, "test-recipe")

		require.NoError(t, err)
		require.Len(t, params, 2)
		assert.Equal(t, RenderParameter{
			ID:      "thread_id",
			Type:    RenderParameterValueTypeNumber,
			IsArray: false,
			Order:   0,
		}, params[0])
		assert.Equal(t, RenderParameter{
			ID:      "thread_names",
			Type:    RenderParameterValueTypeString,
			IsArray: true,
			Order:   1,
		}, params[1])
	})

	t.Run("rejects empty array type", func(t *testing.T) {
		defs := []RenderParameterDefinition{
			{
				ID: "empty_array",
				Config: RenderParameterConfigDefinition{
					Type: []any{},
				},
			},
		}

		_, err := ExtractRenderParameters(defs, "test-recipe")

		require.Error(t, err)

		var msg *message.MessageImpl
		require.ErrorAs(t, err, &msg)
		assert.Equal(t, RecipeParameterMessages.InvalidParam, msg.Code())
	})

	t.Run("rejects array type with multiple elements", func(t *testing.T) {
		defs := []RenderParameterDefinition{
			{
				ID: "multi_type_array",
				Config: RenderParameterConfigDefinition{
					Type: []any{"number", "string"},
				},
			},
		}

		_, err := ExtractRenderParameters(defs, "test-recipe")

		require.Error(t, err)

		var msg *message.MessageImpl
		require.ErrorAs(t, err, &msg)
		assert.Equal(t, RecipeParameterMessages.InvalidParam, msg.Code())
	})

	t.Run("rejects array type with non-string element", func(t *testing.T) {
		defs := []RenderParameterDefinition{
			{
				ID: "invalid_type_array",
				Config: RenderParameterConfigDefinition{
					Type: []any{123},
				},
			},
		}

		_, err := ExtractRenderParameters(defs, "test-recipe")

		require.Error(t, err)

		var msg *message.MessageImpl
		require.ErrorAs(t, err, &msg)
		assert.Equal(t, RecipeParameterMessages.InvalidParam, msg.Code())
	})

	t.Run("rejects unsupported scalar type", func(t *testing.T) {
		defs := []RenderParameterDefinition{
			{
				ID: "unsupported_scalar",
				Config: RenderParameterConfigDefinition{
					Type: "boolean",
				},
			},
		}

		_, err := ExtractRenderParameters(defs, "test-recipe")

		require.Error(t, err)

		var msg *message.MessageImpl
		require.ErrorAs(t, err, &msg)
		assert.Equal(t, RecipeParameterMessages.InvalidParam, msg.Code())
	})

	t.Run("rejects non-string non-array type", func(t *testing.T) {
		defs := []RenderParameterDefinition{
			{
				ID: "invalid_type",
				Config: RenderParameterConfigDefinition{
					Type: 123,
				},
			},
		}

		_, err := ExtractRenderParameters(defs, "test-recipe")

		require.Error(t, err)

		var msg *message.MessageImpl
		require.ErrorAs(t, err, &msg)
		assert.Equal(t, RecipeParameterMessages.InvalidParam, msg.Code())
	})
}
