// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package parameters

import (
	"errors"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/apiversion"
	"github.com/Arm-Debug/apap-cli/apap-engine/cdf/semver"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
)

const (
	testRecipeSource = "test-recipe"
	testToolSource   = "test-tool"
)

func TestFindValue(t *testing.T) {
	params := Parameters{
		Input:        []InputParameter{{Parameter: Parameter{ID: "input"}, DefaultValue: "a"}},
		Checkbox:     []CheckboxParameter{{Parameter: Parameter{ID: "check"}, DefaultValue: true}},
		SingleSelect: []SingleSelectParameter{{Parameter: Parameter{ID: "single"}, DefaultValue: "one"}},
		MultiSelect:  []MultiSelectParameter{{Parameter: Parameter{ID: "select"}, DefaultValue: []string{"b", "c"}}},
		Radio:        []RadioParameter{{Parameter: Parameter{ID: "radio"}, DefaultValue: "rad"}},
	}
	bound, err := BindRecipeParameters(map[string]any{}, params, testRecipeSource)
	require.NoError(t, err)
	val, ok := bound.FindValue("input")
	assert.True(t, ok)
	assert.Equal(t, "a", val)
	val, ok = bound.FindValue("check")
	assert.True(t, ok)
	assert.Equal(t, true, val)
	val, ok = bound.FindValue("single")
	assert.True(t, ok)
	assert.Equal(t, "one", val)
	val, ok = bound.FindValue("select")
	assert.True(t, ok)
	assert.Equal(t, []string{"b", "c"}, val)
	val, ok = bound.FindValue("radio")
	assert.True(t, ok)
	assert.Equal(t, "rad", val)

	_, ok = bound.FindValue("radioA")
	assert.False(t, ok)
}

func TestCollapseToMap(t *testing.T) {
	params := Parameters{
		Input:        []InputParameter{{Parameter: Parameter{ID: "input"}, DefaultValue: "a"}},
		Checkbox:     []CheckboxParameter{{Parameter: Parameter{ID: "check"}, DefaultValue: true}},
		SingleSelect: []SingleSelectParameter{{Parameter: Parameter{ID: "single"}, DefaultValue: "one"}},
		MultiSelect:  []MultiSelectParameter{{Parameter: Parameter{ID: "multi"}, DefaultValue: []string{"b", "c"}}},
		Radio:        []RadioParameter{{Parameter: Parameter{ID: "radio"}, DefaultValue: "rad"}},
	}
	bound, err := BindRecipeParameters(map[string]any{}, params, testRecipeSource)
	require.NoError(t, err)
	mapVals := bound.CollapseToMap()
	assert.Equal(t, map[string]any{
		"input":  "a",
		"check":  true,
		"multi":  []string{"b", "c"},
		"single": "one",
		"radio":  "rad",
	}, mapVals)

	t.Run("omits empty single-select values", func(t *testing.T) {
		params := Parameters{
			SingleSelect: []SingleSelectParameter{{Parameter: Parameter{ID: "single"}}},
		}
		bound, err := BindRecipeParameters(map[string]any{}, params, testRecipeSource)
		require.NoError(t, err)
		assert.NotContains(t, bound.CollapseToMap(), "single")
	})
}

func TestBindValues(t *testing.T) {
	t.Run("happy assign", func(t *testing.T) {

		params := Parameters{
			Input:        []InputParameter{{Parameter: Parameter{ID: "input"}}},
			Checkbox:     []CheckboxParameter{{Parameter: Parameter{ID: "check"}}},
			SingleSelect: []SingleSelectParameter{{Parameter: Parameter{ID: "selectB"}}},
			MultiSelect:  []MultiSelectParameter{{Parameter: Parameter{ID: "select"}}},
			Radio:        []RadioParameter{{Parameter: Parameter{ID: "radio"}}},
		}
		bound, err := BindRecipeParameters(map[string]any{
			"check":   true,
			"radio":   "val",
			"select":  []string{"opA", "opB"},
			"selectB": "opC",
			"input":   "inA",
		}, params, testRecipeSource)
		require.NoError(t, err)
		assert.Equal(t, bound.Values.Checkbox[0], true)
		assert.Equal(t, bound.Values.Input[0], "inA")
		assert.Equal(t, bound.Values.MultiSelect[0], []string{"opA", "opB"})
		assert.Equal(t, bound.Values.SingleSelect[0], "opC")
		assert.Equal(t, bound.Values.Radio[0], "val")
	})

	t.Run("happy default", func(t *testing.T) {

		params := Parameters{
			Input:       []InputParameter{{Parameter: Parameter{ID: "input"}, DefaultValue: "a"}},
			Checkbox:    []CheckboxParameter{{Parameter: Parameter{ID: "check"}, DefaultValue: true}},
			MultiSelect: []MultiSelectParameter{{Parameter: Parameter{ID: "select"}, DefaultValue: []string{"b", "c"}}},
			Radio:       []RadioParameter{{Parameter: Parameter{ID: "radio"}, DefaultValue: "rad"}},
		}
		bound, err := BindRecipeParameters(map[string]any{}, params, testRecipeSource)
		require.NoError(t, err)
		assert.Equal(t, bound.Values.Checkbox[0], true)
		assert.Equal(t, bound.Values.Input[0], "a")
		assert.Equal(t, bound.Values.MultiSelect[0], []string{"b", "c"})
		assert.Equal(t, bound.Values.Radio[0], "rad")
	})

	t.Run("invalid param reports error", func(t *testing.T) {
		_, err := BindRecipeParameters(map[string]any{"check": true}, Parameters{}, testRecipeSource)
		expectedErr := message.New(message.EngineParametersInvalidParam).WithMetadata(map[string]string{"paramName": "check", "source": testRecipeSource})
		assert.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})
	t.Run("multiple invalid params reports error", func(t *testing.T) {
		_, err := BindRecipeParameters(map[string]any{"check": true, "check2": "thing"}, Parameters{}, testRecipeSource)
		var msg message.Message
		assert.True(t, errors.As(err, &msg))
		assert.Equal(t, message.EngineParametersInvalidParams, msg.Code())
		// since we provide a map, order is unknown, so we just have to check that output contains both
		assert.Contains(t, msg.Metadata()["paramNames"], "`check`")
		assert.Contains(t, msg.Metadata()["paramNames"], "`check2`")
		assert.Contains(t, msg.Metadata()["paramNames"], "`, `")
		assert.Equal(t, testRecipeSource, msg.Metadata()["source"])
	})
	t.Run("input value wrong type", func(t *testing.T) {
		params := Parameters{
			Input: []InputParameter{{Parameter: Parameter{ID: "input"}}},
		}
		_, err := BindRecipeParameters(map[string]any{"input": false}, params, testRecipeSource)
		expectedMetadata := map[string]string{
			"paramName": "input",
			"value":     "false",
			"source":    testRecipeSource,
		}
		expectedErr := message.New(message.EngineParametersInvalidInputValue).WithMetadata(expectedMetadata)
		assert.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})
	t.Run("radio value wrong type", func(t *testing.T) {
		params := Parameters{
			Radio: []RadioParameter{{Parameter: Parameter{ID: "radio"}}},
		}
		_, err := BindRecipeParameters(map[string]any{"radio": false}, params, testRecipeSource)
		expectedMetadata := map[string]string{
			"paramName": "radio",
			"value":     "false",
			"source":    testRecipeSource,
		}
		expectedErr := message.New(message.EngineParametersInvalidRadioInputValue).WithMetadata(expectedMetadata)
		assert.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})
	t.Run("checkbox value can't cast", func(t *testing.T) {
		params := Parameters{
			Checkbox: []CheckboxParameter{{Parameter: Parameter{ID: "check"}}},
		}
		_, err := BindRecipeParameters(map[string]any{"check": "1"}, params, testRecipeSource)
		expectedMetadata := map[string]string{
			"paramName": "check",
			"value":     "1",
			"source":    testRecipeSource,
		}
		expectedErr := message.New(message.EngineParametersInvalidCheckboxValue).WithMetadata(expectedMetadata)
		assert.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})
	t.Run("checkbox value wrong type", func(t *testing.T) {
		params := Parameters{
			Checkbox: []CheckboxParameter{{Parameter: Parameter{ID: "check"}}},
		}
		_, err := BindRecipeParameters(map[string]any{"check": []string{"1"}}, params, testRecipeSource)
		expectedMetadata := map[string]string{
			"paramName": "check",
			"value":     "[1]",
			"source":    testRecipeSource,
		}
		expectedErr := message.New(message.EngineParametersInvalidCheckboxValue).WithMetadata(expectedMetadata)
		assert.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})
	t.Run("checkbox value parses string boolean", func(t *testing.T) {
		params := Parameters{
			Checkbox: []CheckboxParameter{{Parameter: Parameter{ID: "check"}}},
		}
		bound, err := BindRecipeParameters(map[string]any{"check": "TRUE"}, params, testRecipeSource)
		require.NoError(t, err)
		assert.Equal(t, true, bound.Values.Checkbox[0])

		paramsDefaultTrue := Parameters{
			Checkbox: []CheckboxParameter{{Parameter: Parameter{ID: "check"}, DefaultValue: true}},
		}
		bound, err = BindRecipeParameters(map[string]any{"check": "false"}, paramsDefaultTrue, testRecipeSource)
		require.NoError(t, err)
		assert.Equal(t, false, bound.Values.Checkbox[0])
	})
	t.Run("select value wrong type", func(t *testing.T) {
		params := Parameters{
			SingleSelect: []SingleSelectParameter{{Parameter: Parameter{ID: "select"}}},
		}
		_, err := BindRecipeParameters(map[string]any{"select": false}, params, testRecipeSource)
		expectedMetadata := map[string]string{
			"paramName": "select",
			"value":     "false",
			"source":    testRecipeSource,
		}
		expectedErr := message.New(message.EngineParametersInvalidSingleSelectValue).WithMetadata(expectedMetadata)
		assert.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})
	t.Run("single select rejects multiple values", func(t *testing.T) {
		params := Parameters{
			SingleSelect: []SingleSelectParameter{{Parameter: Parameter{ID: "select"}}},
		}
		_, err := BindRecipeParameters(map[string]any{"select": []string{"a", "b"}}, params, testRecipeSource)
		expectedMetadata := map[string]string{
			"paramName": "select",
			"value":     "[a b]",
			"source":    testRecipeSource,
		}
		expectedErr := message.New(message.EngineParametersInvalidSingleSelectValue).WithMetadata(expectedMetadata)
		assert.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})
	t.Run("multi select value wrong type", func(t *testing.T) {
		params := Parameters{
			MultiSelect: []MultiSelectParameter{{Parameter: Parameter{ID: "select"}}},
		}
		_, err := BindRecipeParameters(map[string]any{"select": false}, params, testRecipeSource)
		expectedMetadata := map[string]string{
			"paramName": "select",
			"value":     "false",
			"source":    testRecipeSource,
		}
		expectedErr := message.New(message.EngineParametersInvalidMultiSelectValue).WithMetadata(expectedMetadata)
		assert.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})
}

func TestBindToolIntegrationParametersSelectInterfaceSlice(t *testing.T) {
	params := Parameters{
		MultiSelect: []MultiSelectParameter{{Parameter: Parameter{ID: "select"}}},
	}
	input := map[string]any{
		"select": []interface{}{"alpha", "beta"},
	}

	bound, err := BindToolIntegrationParameters(input, params, testToolSource)
	require.NoError(t, err)
	assert.Equal(t, []string{"alpha", "beta"}, bound.Values.MultiSelect[0])
}

func TestBindRenderParameters(t *testing.T) {
	t.Run("defaults missing values to nil", func(t *testing.T) {
		params := RenderParameters{
			{ID: "thread_id", Type: RenderParameterValueTypeNumber},
		}

		bound, err := BindRenderParameters(map[string]any{}, params, testRecipeSource)

		require.NoError(t, err)
		assert.Equal(t, params, bound.Parameters)
		require.Contains(t, bound.Values, "thread_id")
		assert.Nil(t, bound.Values["thread_id"])
	})

	t.Run("rejects unknown input", func(t *testing.T) {
		params := RenderParameters{
			{ID: "thread_id", Type: RenderParameterValueTypeNumber},
		}

		_, err := BindRenderParameters(map[string]any{"unknown": "whatever"}, params, testRecipeSource)

		expectedErr := message.New(message.EngineParametersInvalidParam).WithMetadata(map[string]string{"paramName": "unknown", "source": testRecipeSource})
		assert.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})

	t.Run("accepts string value", func(t *testing.T) {
		params := RenderParameters{
			{ID: "process_name", Type: RenderParameterValueTypeString},
		}

		bound, err := BindRenderParameters(map[string]any{"process_name": "process"}, params, testRecipeSource)

		require.NoError(t, err)
		assert.Equal(t, params, bound.Parameters)
		require.Contains(t, bound.Values, "process_name")
		assert.Equal(t, "process", bound.Values["process_name"])
	})

	t.Run("accepts number value", func(t *testing.T) {
		params := RenderParameters{
			{ID: "thread_id", Type: RenderParameterValueTypeNumber},
		}

		bound, err := BindRenderParameters(map[string]any{"thread_id": float64(123)}, params, testRecipeSource)
		require.NoError(t, err)
		assert.Equal(t, params, bound.Parameters)
		require.Contains(t, bound.Values, "thread_id")
		assert.Equal(t, float64(123), bound.Values["thread_id"])
	})

	t.Run("accepts explicit nil as unset", func(t *testing.T) {
		params := RenderParameters{
			{ID: "thread_id", Type: RenderParameterValueTypeNumber},
		}

		bound, err := BindRenderParameters(map[string]any{"thread_id": nil}, params, testRecipeSource)

		require.NoError(t, err)
		require.Contains(t, bound.Values, "thread_id")
		assert.Nil(t, bound.Values["thread_id"])
	})

	t.Run("rejects wrong scalar type", func(t *testing.T) {
		params := RenderParameters{
			{ID: "thread_id", Type: RenderParameterValueTypeNumber},
		}

		_, err := BindRenderParameters(map[string]any{"thread_id": "not a number"}, params, testRecipeSource)
		expectedMetadata := map[string]string{
			"paramName": "thread_id",
			"value":     "not a number",
			"source":    testRecipeSource,
		}

		var msg message.Message
		require.ErrorAs(t, err, &msg)
		assert.Equal(t, message.EngineParametersInvalidParam, msg.Code())
		assert.Equal(t, expectedMetadata, msg.Metadata())
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})

	t.Run("accepts number array", func(t *testing.T) {
		params := RenderParameters{
			{ID: "time_range", Type: RenderParameterValueTypeNumber, IsArray: true},
		}

		bound, err := BindRenderParameters(map[string]any{"time_range": []any{float64(1), float64(2)}}, params, testRecipeSource)

		require.NoError(t, err)
		assert.Equal(t, params, bound.Parameters)
		require.Contains(t, bound.Values, "time_range")
		assert.Equal(t, []any{float64(1), float64(2)}, bound.Values["time_range"])
	})

	t.Run("rejects scalar for array", func(t *testing.T) {
		params := RenderParameters{
			{ID: "time_range", Type: RenderParameterValueTypeNumber, IsArray: true},
		}

		_, err := BindRenderParameters(map[string]any{"time_range": float64(123)}, params, testRecipeSource)

		require.Error(t, err)
		var msg message.Message
		require.ErrorAs(t, err, &msg)
		assert.Equal(t, message.EngineParametersInvalidParam, msg.Code())
		assert.Equal(t, map[string]string{"paramName": "time_range", "value": "123", "source": testRecipeSource}, msg.Metadata())
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})

	t.Run("rejects mixed array values", func(t *testing.T) {
		params := RenderParameters{
			{ID: "time_range", Type: RenderParameterValueTypeNumber, IsArray: true},
		}

		_, err := BindRenderParameters(map[string]any{"time_range": []any{float64(1), "2"}}, params, testRecipeSource)
		require.Error(t, err)
		var msg message.Message
		require.ErrorAs(t, err, &msg)
		assert.Equal(t, message.EngineParametersInvalidParam, msg.Code())
		assert.Equal(t, "time_range", msg.Metadata()["paramName"])
		assert.Equal(t, testRecipeSource, msg.Metadata()["source"])
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})

	t.Run("rejects non-finite number value", func(t *testing.T) {
		params := RenderParameters{
			{ID: "thread_id", Type: RenderParameterValueTypeNumber},
		}

		for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
			_, err := BindRenderParameters(map[string]any{"thread_id": value}, params, testRecipeSource)

			require.Error(t, err)
			var msg message.Message
			require.ErrorAs(t, err, &msg)
			assert.Equal(t, message.EngineParametersInvalidParam, msg.Code())
			assert.Equal(t, "thread_id", msg.Metadata()["paramName"])
			assert.Equal(t, testRecipeSource, msg.Metadata()["source"])
			assert.NoError(t, message.ValidateMetadataPlaceholders(err))
		}
	})
}

func TestConvertRenderParameterInputs(t *testing.T) {
	t.Run("converts number string", func(t *testing.T) {
		params := RenderParameters{
			{ID: "thread_id", Type: RenderParameterValueTypeNumber},
		}

		converted, err := ConvertRenderParameterInputs(map[string]any{"thread_id": "123"}, params, testRecipeSource)
		require.NoError(t, err)
		assert.Equal(t, float64(123), converted["thread_id"])
	})

	t.Run("keeps string value", func(t *testing.T) {
		params := RenderParameters{
			{ID: "process_name", Type: RenderParameterValueTypeString},
		}

		converted, err := ConvertRenderParameterInputs(map[string]any{"process_name": "1234"}, params, testRecipeSource)
		require.NoError(t, err)
		assert.Equal(t, "1234", converted["process_name"])
	})

	t.Run("converts number array JSON string", func(t *testing.T) {
		params := RenderParameters{
			{ID: "time_range", Type: RenderParameterValueTypeNumber, IsArray: true},
		}

		converted, err := ConvertRenderParameterInputs(map[string]any{"time_range": "[1, 2]"}, params, testRecipeSource)
		require.NoError(t, err)
		assert.Equal(t, []any{float64(1), float64(2)}, converted["time_range"])
	})

	t.Run("converts string array JSON string", func(t *testing.T) {
		params := RenderParameters{
			{ID: "thread_ids", Type: RenderParameterValueTypeString, IsArray: true},
		}

		converted, err := ConvertRenderParameterInputs(map[string]any{"thread_ids": `["thread1", "thread\"two\""]`}, params, testRecipeSource)
		require.NoError(t, err)
		assert.Equal(t, []any{"thread1", "thread\"two\""}, converted["thread_ids"])
	})

	t.Run("rejects invalid number string", func(t *testing.T) {
		params := RenderParameters{
			{ID: "thread_id", Type: RenderParameterValueTypeNumber},
		}

		_, err := ConvertRenderParameterInputs(map[string]any{"thread_id": "not a number"}, params, testRecipeSource)
		require.Error(t, err)
		var msg message.Message
		require.ErrorAs(t, err, &msg)
		assert.Equal(t, message.EngineParametersInvalidParam, msg.Code())
		assert.Equal(t, map[string]string{"paramName": "thread_id", "value": "not a number", "source": testRecipeSource}, msg.Metadata())
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})

	t.Run("rejects non-finite number string", func(t *testing.T) {
		params := RenderParameters{
			{ID: "thread_id", Type: RenderParameterValueTypeNumber},
		}

		for _, value := range []string{"NaN", "Inf", "-Inf"} {
			_, err := ConvertRenderParameterInputs(map[string]any{"thread_id": value}, params, testRecipeSource)

			require.Error(t, err)
			var msg message.Message
			require.ErrorAs(t, err, &msg)
			assert.Equal(t, message.EngineParametersInvalidParam, msg.Code())
			assert.Equal(t, "thread_id", msg.Metadata()["paramName"])
			assert.Equal(t, value, msg.Metadata()["value"])
			assert.Equal(t, testRecipeSource, msg.Metadata()["source"])
			assert.NoError(t, message.ValidateMetadataPlaceholders(err))
		}
	})

	t.Run("rejects invalid array JSON string", func(t *testing.T) {
		params := RenderParameters{
			{ID: "thread_ids", Type: RenderParameterValueTypeString, IsArray: true},
		}

		_, err := ConvertRenderParameterInputs(map[string]any{"thread_ids": `["thread1", "thread2"`}, params, testRecipeSource)
		require.Error(t, err)
		var msg message.Message
		require.ErrorAs(t, err, &msg)
		assert.Equal(t, message.EngineParametersInvalidParam, msg.Code())
		assert.Equal(t, map[string]string{"paramName": "thread_ids", "value": `["thread1", "thread2"`, "source": testRecipeSource}, msg.Metadata())
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})
}

func TestGetParameterOptionsOrDefault(t *testing.T) {
	defaults := []string{"a", "b"}
	vals := [][]string{
		{"c", "d"},
		{},
	}
	assert.Equal(t, []string{"c", "d"}, GetParameterOptionsOrDefault(defaults, 0, vals))
	assert.Equal(t, []string{}, GetParameterOptionsOrDefault(defaults, 1, vals))
	assert.Equal(t, []string{"a", "b"}, GetParameterOptionsOrDefault(defaults, 2, vals))
}

func TestGetParameterOptionValuesOrDefault(t *testing.T) {
	defaults := []string{"a", "b"}
	items := [][]ParameterOption{
		{
			{Value: "c", Label: "C"},
			{Value: "d", Label: "D"},
		},
		{},
		{
			{Value: "e", Label: "E"},
		},
	}
	assert.Equal(t, []string{"c", "d"}, GetParameterOptionValuesOrDefault(defaults, 0, items))
	assert.Equal(t, []string{}, GetParameterOptionValuesOrDefault(defaults, 1, items))
	assert.Equal(t, []string{"e"}, GetParameterOptionValuesOrDefault(defaults, 2, items))
	assert.Equal(t, []string{"a", "b"}, GetParameterOptionValuesOrDefault(defaults, 3, items))
}

func TestExtractParametersDefaultValidation(t *testing.T) {
	t.Run("radio default missing from options", func(t *testing.T) {
		_, _, err := ExtractRecipeParameters([]ParameterDefinition{{
			ID: "radio",
			Config: map[string]any{
				"type":         "radio",
				"defaultValue": "foo",
				"options":      []string{"bar"},
			},
		}}, "test-recipe", apiversion.LegacyRecipeRadioStringOptionsAPIVersion)
		require.Error(t, err)
		msgErr, ok := err.(*message.MessageImpl)
		require.True(t, ok)
		assert.Equal(t, message.EngineParametersInvalidDefaultParamValue, msgErr.Code())
		assert.Equal(t, map[string]string{"paramName": "radio", "value": "foo", "source": "test-recipe"}, msgErr.Metadata())
	})

	t.Run("select default missing from options", func(t *testing.T) {
		_, _, err := ExtractRecipeParameters([]ParameterDefinition{{
			ID: "select",
			Config: map[string]any{
				"type":         "single_select",
				"defaultValue": "baz",
				"options":      []string{"bar"},
			},
		}}, "test-recipe", apiversion.LegacyRecipeRadioStringOptionsAPIVersion)
		require.Error(t, err)
		msgErr, ok := err.(*message.MessageImpl)
		require.True(t, ok)
		assert.Equal(t, message.EngineParametersInvalidDefaultParamValue, msgErr.Code())
		assert.Equal(t, map[string]string{"paramName": "select", "value": "baz", "source": "test-recipe"}, msgErr.Metadata())
	})

	t.Run("single select empty default is treated as unset", func(t *testing.T) {
		params, _, err := ExtractRecipeParameters([]ParameterDefinition{{
			ID:       "select",
			Required: false,
			Config: map[string]any{
				"type":         "single_select",
				"defaultValue": "",
				"options":      []string{"bar", "baz"},
			},
		}}, "test-recipe", apiversion.LegacyRecipeRadioStringOptionsAPIVersion)
		require.NoError(t, err)
		require.Len(t, params.SingleSelect, 1)
		assert.Equal(t, "", params.SingleSelect[0].DefaultValue)
		assert.Equal(t, []string{"bar", "baz"}, params.SingleSelect[0].Options)
	})

	t.Run("select default present in options", func(t *testing.T) {
		_, _, err := ExtractRecipeParameters([]ParameterDefinition{{
			ID: "select",
			Config: map[string]any{
				"type":         "multi_select",
				"defaultValue": []string{"bar"},
				"options":      []string{"bar", "baz"},
			},
		}}, "test-recipe", apiversion.LegacyRecipeRadioStringOptionsAPIVersion)
		assert.NoError(t, err)
	})

	t.Run("single select default rejects multiple values", func(t *testing.T) {
		_, _, err := ExtractRecipeParameters([]ParameterDefinition{{
			ID: "select",
			Config: map[string]any{
				"type":         "single_select",
				"defaultValue": []string{"bar", "baz"},
				"options":      []string{"bar", "baz"},
			},
		}}, "test-recipe", apiversion.LegacyRecipeRadioStringOptionsAPIVersion)
		require.Error(t, err)
		var msgErr *message.MessageImpl
		require.ErrorAs(t, err, &msgErr)
		assert.Equal(t, message.EngineParametersInvalidDefaultParamValue, msgErr.Code())
		assert.Equal(t, map[string]string{"paramName": "select", "value": "[bar baz]", "source": "test-recipe"}, msgErr.Metadata())
	})

	t.Run("select default invalid type", func(t *testing.T) {
		_, _, err := ExtractRecipeParameters([]ParameterDefinition{{
			ID: "select",
			Config: map[string]any{
				"type":         "single_select",
				"defaultValue": 123,
				"options":      []string{"bar"},
			},
		}}, "test-recipe", apiversion.LegacyRecipeRadioStringOptionsAPIVersion)
		require.Error(t, err)
		var msgErr *message.MessageImpl
		require.ErrorAs(t, err, &msgErr)
		assert.Equal(t, message.EngineParametersInvalidDefaultParamValue, msgErr.Code())
		assert.Equal(t, map[string]string{"paramName": "select", "value": "123", "source": "test-recipe"}, msgErr.Metadata())
	})

	t.Run("multi select default rejects string value", func(t *testing.T) {
		_, _, err := ExtractRecipeParameters([]ParameterDefinition{{
			ID: "select",
			Config: map[string]any{
				"type":         "multi_select",
				"defaultValue": "bar",
				"options":      []string{"bar", "baz"},
			},
		}}, "test-recipe", apiversion.LegacyRecipeRadioStringOptionsAPIVersion)
		require.Error(t, err)
		var msgErr *message.MessageImpl
		require.ErrorAs(t, err, &msgErr)
		assert.Equal(t, message.EngineParametersInvalidDefaultParamValue, msgErr.Code())
		assert.Equal(t, map[string]string{"paramName": "select", "value": "bar", "source": "test-recipe"}, msgErr.Metadata())
	})

	t.Run("radio string options are accepted for legacy api version", func(t *testing.T) {
		_, _, err := ExtractRecipeParameters([]ParameterDefinition{{
			ID: "radio",
			Config: map[string]any{
				"type":         "radio",
				"defaultValue": "bar",
				"options":      []string{"bar"},
			},
		}}, "test-recipe", apiversion.LegacyRecipeRadioStringOptionsAPIVersion)
		assert.NoError(t, err)
	})

	t.Run("radio string options are rejected for api versions after 1.0.0", func(t *testing.T) {
		_, _, err := ExtractRecipeParameters([]ParameterDefinition{{
			ID: "radio",
			Config: map[string]any{
				"type":         "radio",
				"defaultValue": "bar",
				"options":      []string{"bar"},
			},
		}}, "test-recipe", semver.SemVer{Major: 1, Minor: 0, Patch: 1})
		require.Error(t, err)
		var msgErr *message.MessageImpl
		require.ErrorAs(t, err, &msgErr)
		assert.Equal(t, message.EngineParametersInvalidParam, msgErr.Code())
		assert.Equal(t, map[string]string{"paramName": "radio", "source": "test-recipe"}, msgErr.Metadata())
		assert.ErrorContains(t, err, "radio options for recipe api_version 1.0.1 must use option objects")
	})

	t.Run("radio option objects with missing string value are rejected", func(t *testing.T) {
		_, _, err := ExtractRecipeParameters([]ParameterDefinition{{
			ID: "radio",
			Config: map[string]any{
				"type":         "radio",
				"defaultValue": "bar",
				"options":      []any{map[string]any{"label": "Bar"}},
			},
		}}, "test-recipe", apiversion.LegacyRecipeRadioStringOptionsAPIVersion)
		require.Error(t, err)
		var msgErr *message.MessageImpl
		require.ErrorAs(t, err, &msgErr)
		assert.Equal(t, message.EngineParametersInvalidParam, msgErr.Code())
		assert.Equal(t, map[string]string{"paramName": "radio", "source": "test-recipe"}, msgErr.Metadata())
		assert.ErrorContains(t, err, "radio option object at index 0 must contain a string value field")
	})
}

func TestValidateToolIntegrationParameterValues(t *testing.T) {
	params := Parameters{
		Input: []InputParameter{
			{
				Parameter:    Parameter{ID: "notes", Required: true},
				DefaultValue: "default",
			},
		},
		Radio: []RadioParameter{
			{
				Parameter:    Parameter{ID: "mode", Required: true},
				Options:      []string{"samples", "metrics"},
				DefaultValue: "samples",
			},
		},
		MultiSelect: []MultiSelectParameter{
			{
				Parameter:    Parameter{ID: "metrics_group", Required: true},
				Options:      []string{"l1", "l2"},
				DefaultValue: []string{"l1"},
			},
		},
	}

	bound, err := BindToolIntegrationParameters(map[string]any{}, params, testToolSource)
	require.NoError(t, err)
	assert.NoError(t, ValidateToolIntegrationParameterValues(bound, testToolSource))

	bound.Values.Input[0] = ""
	err = ValidateToolIntegrationParameterValues(bound, testToolSource)
	require.Error(t, err)
	msg := message.IsMessage(err)
	require.NotNil(t, msg)
	assert.Equal(t, message.EngineIntegrationParametersInvalidInputValue, msg.Code())
	assert.Equal(t, map[string]string{"paramName": "notes", "value": "", "source": testToolSource}, msg.Metadata())

	bound.Values.Input[0] = "note"
	bound.Values.Radio[0] = "invalid"
	err = ValidateToolIntegrationParameterValues(bound, testToolSource)
	require.Error(t, err)
	msg = message.IsMessage(err)
	require.NotNil(t, msg)
	assert.Equal(t, message.EngineIntegrationParametersInvalidRadioInputValue, msg.Code())
	assert.Equal(t, map[string]string{"paramName": "mode", "value": "invalid", "source": testToolSource}, msg.Metadata())

	bound.Values.Radio[0] = "samples"
	bound.Values.MultiSelect[0] = []string{"bad"}
	err = ValidateToolIntegrationParameterValues(bound, testToolSource)
	require.Error(t, err)
	msg = message.IsMessage(err)
	require.NotNil(t, msg)
	assert.Equal(t, message.EngineIntegrationParametersInvalidMultiSelectValue, msg.Code())
	assert.Equal(t, map[string]string{"paramName": "metrics_group", "value": "bad", "source": testToolSource}, msg.Metadata())
}

func TestValidateToolIntegrationParameterValuesSingleSelect(t *testing.T) {
	params := Parameters{
		SingleSelect: []SingleSelectParameter{
			{
				Parameter:    Parameter{ID: "mode", Required: true},
				Options:      []string{"samples", "metrics"},
				DefaultValue: "",
			},
		},
	}

	bound, err := BindToolIntegrationParameters(map[string]any{}, params, testToolSource)
	require.NoError(t, err)

	err = ValidateToolIntegrationParameterValues(bound, testToolSource)
	require.Error(t, err)
	msg := message.IsMessage(err)
	require.NotNil(t, msg)
	assert.Equal(t, message.EngineIntegrationParametersInvalidSingleSelectValue, msg.Code())
	assert.Equal(t, map[string]string{"paramName": "mode", "value": "", "source": testToolSource}, msg.Metadata())

	bound.Values.SingleSelect[0] = "invalid"
	err = ValidateToolIntegrationParameterValues(bound, testToolSource)
	require.Error(t, err)
	msg = message.IsMessage(err)
	require.NotNil(t, msg)
	assert.Equal(t, message.EngineIntegrationParametersInvalidSingleSelectValue, msg.Code())
	assert.Equal(t, map[string]string{"paramName": "mode", "value": "invalid", "source": testToolSource}, msg.Metadata())
}
