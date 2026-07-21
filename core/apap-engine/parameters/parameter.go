// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package parameters

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

// ParameterDepency belongs to a parameter and defines a dependency on another parameter.
// The related parameter must be set to the given value for this parameter to be visible.
type ParameterDependency struct {
	ParameterID string
	Value       string
}

// SingleSelectParameter represents a single option from a predefined list.
type SingleSelectParameter struct {
	Parameter
	DefaultValue string
	// Options contains the value-only option list used for validation and legacy
	// recipe support. Prefer OptionItems when exposing options to users or API clients.
	Options []string
	// OptionItems contains rich option metadata including value, label, and
	// description. This is the preferred representation for user-facing option data.
	OptionItems []ParameterOption
}

// MultiSelectParameter represents zero or more options from a predefined list.
type MultiSelectParameter struct {
	Parameter
	DefaultValue []string
	// Options contains the value-only option list used for validation and legacy
	// recipe support. Prefer OptionItems when exposing options to users or API clients.
	Options []string
	// OptionItems contains rich option metadata including value, label, and
	// description. This is the preferred representation for user-facing option data.
	OptionItems []ParameterOption
}

// CheckboxParameter represents a boolean parameter state.
type CheckboxParameter struct {
	Parameter
	DefaultValue bool
}

// InputParameter represents a free-form string input.
type InputParameter struct {
	Parameter
	DefaultValue string
	Custom       map[string]string
}

// RadioParameter represents a single choice from a set of options.
type RadioParameter struct {
	Parameter
	DefaultValue string
	// Options contains the value-only option list used for validation and legacy
	// recipe support. Prefer OptionItems when exposing options to users or API clients.
	Options []string
	// OptionItems contains rich option metadata including value, label, and
	// description. This is the preferred representation for user-facing option data.
	OptionItems []ParameterOption
}

// ParameterParameters holds the common fields of all parameter types.
type Parameter struct {
	ID          string
	Label       string
	Description string
	Required    bool
	Order       int
	VisibleWhen []ParameterDependency
}

type Parameters struct {
	SingleSelect []SingleSelectParameter
	MultiSelect  []MultiSelectParameter
	Checkbox     []CheckboxParameter
	Input        []InputParameter
	Radio        []RadioParameter
}

type RenderParameterValueType string

const (
	RenderParameterValueTypeString RenderParameterValueType = "string"
	RenderParameterValueTypeNumber RenderParameterValueType = "number"
)

type RenderParameter struct {
	ID      string
	Type    RenderParameterValueType
	IsArray bool
	Order   int
}

type RenderParameters []RenderParameter

type BoundRenderParameters struct {
	Parameters RenderParameters
	Values     map[string]any
}

func (brp BoundRenderParameters) CollapseToMap() map[string]any {
	out := make(map[string]any, len(brp.Values))
	for k, v := range brp.Values {
		out[k] = v
	}
	return out
}

// BoundParameters bring together parameter definitions and values
// to be utilised when running a recipe
type BoundParameters struct {
	Parameters Parameters
	Values     ParameterValues
}

func (bd *BoundParameters) FindValue(paramID string) (any, bool) {
	for i, p := range bd.Parameters.Checkbox {
		if p.ID == paramID {
			return bd.Values.Checkbox[i], true
		}
	}
	for i, p := range bd.Parameters.Input {
		if p.ID == paramID {
			return bd.Values.Input[i], true
		}
	}
	for i, p := range bd.Parameters.SingleSelect {
		if p.ID == paramID {
			return bd.Values.SingleSelect[i], true
		}
	}
	for i, p := range bd.Parameters.MultiSelect {
		if p.ID == paramID {
			return bd.Values.MultiSelect[i], true
		}
	}
	for i, p := range bd.Parameters.Radio {
		if p.ID == paramID {
			return bd.Values.Radio[i], true
		}
	}
	return nil, false
}

func (bd *BoundParameters) CollapseToMap() map[string]any {
	params := make(map[string]any, 0)
	for k, v := range bd.Parameters.Checkbox {
		params[v.ID] = bd.Values.Checkbox[k]
	}
	for k, v := range bd.Parameters.Input {
		params[v.ID] = bd.Values.Input[k]
	}
	for k, v := range bd.Parameters.SingleSelect {
		value := bd.Values.SingleSelect[k]
		if value != "" {
			params[v.ID] = value
		}
	}
	for k, v := range bd.Parameters.MultiSelect {
		params[v.ID] = bd.Values.MultiSelect[k]
	}
	for k, v := range bd.Parameters.Radio {
		params[v.ID] = bd.Values.Radio[k]
	}
	return params
}

// ParameterValues contains the values for all parameters of a recipe
// instantiation. The value soure is either a user input or a default value
type ParameterValues struct {
	Checkbox     []bool
	Radio        []string
	SingleSelect []string
	MultiSelect  [][]string
	Input        []string
}

type ParameterMessages struct {
	InvalidParam             message.MessageCode
	InvalidParams            message.MessageCode
	InvalidCheckboxValue     message.MessageCode
	InvalidRadioValue        message.MessageCode
	InvalidSingleSelectValue message.MessageCode
	InvalidMultiSelectValue  message.MessageCode
	InvalidInputValue        message.MessageCode
	InvalidDefaultParamValue message.MessageCode
	EmptyOptions             message.MessageCode
}

var (
	RecipeParameterMessages = ParameterMessages{
		InvalidParam:             message.EngineParametersInvalidParam,
		InvalidParams:            message.EngineParametersInvalidParams,
		InvalidCheckboxValue:     message.EngineParametersInvalidCheckboxValue,
		InvalidRadioValue:        message.EngineParametersInvalidRadioInputValue,
		InvalidSingleSelectValue: message.EngineParametersInvalidSingleSelectValue,
		InvalidMultiSelectValue:  message.EngineParametersInvalidMultiSelectValue,
		InvalidInputValue:        message.EngineParametersInvalidInputValue,
		InvalidDefaultParamValue: message.EngineParametersInvalidDefaultParamValue,
		EmptyOptions:             message.EngineParametersOptionsEmptyStatic,
	}

	ToolIntegrationParameterMessages = ParameterMessages{
		InvalidParam:             message.EngineIntegrationParametersInvalidParam,
		InvalidParams:            message.EngineIntegrationParametersInvalidParams,
		InvalidCheckboxValue:     message.EngineIntegrationParametersInvalidCheckboxValue,
		InvalidRadioValue:        message.EngineIntegrationParametersInvalidRadioInputValue,
		InvalidSingleSelectValue: message.EngineIntegrationParametersInvalidSingleSelectValue,
		InvalidMultiSelectValue:  message.EngineIntegrationParametersInvalidMultiSelectValue,
		InvalidInputValue:        message.EngineIntegrationParametersInvalidInputValue,
		InvalidDefaultParamValue: message.EngineParametersInvalidDefaultParamValue,
		EmptyOptions:             message.EngineIntegrationParametersOptionsEmptyStatic,
	}
)

// BindRecipeParameters generates bound parameters from the given parameter definitions and input values for a recipe.
func BindRecipeParameters(parameterInputs map[string]any, params Parameters, source string) (BoundParameters, error) {
	return bindParameters(parameterInputs, params, RecipeParameterMessages, source)
}

// BindToolIntegrationParameters generates bound parameters from the given parameter definitions and input values for a tool integration.
func BindToolIntegrationParameters(parameterInputs map[string]any, params Parameters, source string) (BoundParameters, error) {
	return bindParameters(parameterInputs, params, ToolIntegrationParameterMessages, source)
}

// bindParameters generates bound parameters from the given parameter defintions and input values.
// Default parameter values are used when no explicit value is provided in parameterInputs.
// The type of each parameter value is validated.
// Error messages are customised via the msgs parameter.
func bindParameters(parameterInputs map[string]any, params Parameters, msgs ParameterMessages, source string) (BoundParameters, error) {
	mappedParams := make(map[string]struct{})

	recipeParams := BoundParameters{
		Parameters: params,
		Values: ParameterValues{
			Input:        make([]string, len(params.Input)),
			SingleSelect: make([]string, len(params.SingleSelect)),
			MultiSelect:  make([][]string, len(params.MultiSelect)),
			Checkbox:     make([]bool, len(params.Checkbox)),
			Radio:        make([]string, len(params.Radio)),
		},
	}

	makeErr := func(paramName string, value any, msgCode message.MessageCode) error {
		metadata := map[string]string{
			"paramName": paramName,
			"value":     fmt.Sprintf("%v", value),
			"source":    source,
		}
		return message.New(msgCode).WithMetadata(metadata)
	}

	for i := range params.Input {
		paramName := params.Input[i].ID
		if parameterInputs[paramName] != nil {
			mappedParams[paramName] = struct{}{}
			switch v := parameterInputs[paramName].(type) {
			case string:
				recipeParams.Values.Input[i] = v
			default:
				return recipeParams, makeErr(paramName, v, msgs.InvalidInputValue)
			}
		} else {
			recipeParams.Values.Input[i] = params.Input[i].DefaultValue
		}
	}

	for i := range params.SingleSelect {
		paramName := params.SingleSelect[i].ID
		if parameterInputs[paramName] != nil {
			mappedParams[paramName] = struct{}{}
			switch v := parameterInputs[paramName].(type) {
			case []interface{}:
				converted := convertToStringSlice(v)
				if converted == nil {
					return recipeParams, makeErr(paramName, v, msgs.InvalidSingleSelectValue)
				}
				if len(converted) > 1 {
					return recipeParams, makeErr(paramName, converted, msgs.InvalidSingleSelectValue)
				}
				if len(converted) == 1 {
					recipeParams.Values.SingleSelect[i] = converted[0]
				}
			case string:
				recipeParams.Values.SingleSelect[i] = v
			default:
				return recipeParams, makeErr(paramName, v, msgs.InvalidSingleSelectValue)
			}
		} else {
			recipeParams.Values.SingleSelect[i] = params.SingleSelect[i].DefaultValue
		}
	}

	for i := range params.MultiSelect {
		paramName := params.MultiSelect[i].ID
		if parameterInputs[paramName] != nil {
			mappedParams[paramName] = struct{}{}
			switch v := parameterInputs[paramName].(type) {
			case []interface{}:
				converted := convertToStringSlice(v)
				if converted == nil {
					return recipeParams, makeErr(paramName, v, msgs.InvalidMultiSelectValue)
				}
				recipeParams.Values.MultiSelect[i] = converted
			case string:
				recipeParams.Values.MultiSelect[i] = []string{v}
			case []string:
				recipeParams.Values.MultiSelect[i] = v
			default:
				return recipeParams, makeErr(paramName, v, msgs.InvalidMultiSelectValue)
			}
		} else {
			recipeParams.Values.MultiSelect[i] = params.MultiSelect[i].DefaultValue
		}
	}

	for i := range params.Checkbox {
		paramName := params.Checkbox[i].ID
		if parameterInputs[paramName] != nil {
			mappedParams[paramName] = struct{}{}
			switch b := parameterInputs[paramName].(type) {
			case bool:
				recipeParams.Values.Checkbox[i] = b
			case string:
				lowerB := strings.ToLower(b)
				if lowerB != "true" && lowerB != "false" {
					return recipeParams, makeErr(paramName, b, msgs.InvalidCheckboxValue)
				}
				recipeParams.Values.Checkbox[i] = lowerB == "true"
			default:
				return recipeParams, makeErr(paramName, b, msgs.InvalidCheckboxValue)
			}
		} else {
			recipeParams.Values.Checkbox[i] = params.Checkbox[i].DefaultValue
		}
	}
	for i := range params.Radio {
		paramName := params.Radio[i].ID
		if parameterInputs[paramName] != nil {
			mappedParams[paramName] = struct{}{}
			switch v := parameterInputs[paramName].(type) {
			case string:
				recipeParams.Values.Radio[i] = v
			default:
				return recipeParams, makeErr(paramName, v, msgs.InvalidRadioValue)
			}
		} else {
			recipeParams.Values.Radio[i] = params.Radio[i].DefaultValue
		}
	}

	// Error on any unmapped parameters
	var unmapped []string
	for key := range parameterInputs {
		if _, ok := mappedParams[key]; !ok {
			unmapped = append(unmapped, key)
		}
	}
	if len(unmapped) > 0 {
		if len(unmapped) > 1 {
			unmappedNames := util.DisplayErrorStringSlice(unmapped)
			return recipeParams, message.New(msgs.InvalidParams).WithMetadata(map[string]string{
				"paramNames": unmappedNames,
				"source":     source,
			})
		} else {
			return recipeParams, message.New(msgs.InvalidParam).WithMetadata(map[string]string{
				"paramName": unmapped[0],
				"source":    source,
			})
		}
	}

	return recipeParams, nil
}

func BindRenderParameters(parameterInputs map[string]any, params RenderParameters, source string) (BoundRenderParameters, error) {
	bound := BoundRenderParameters{
		Parameters: params,
		Values:     make(map[string]any, len(params)),
	}

	declared := map[string]RenderParameter{}
	for _, p := range params {
		declared[p.ID] = p
		bound.Values[p.ID] = nil
	}

	for id, value := range parameterInputs {
		param, ok := declared[id]
		if !ok {
			return bound, message.New(RecipeParameterMessages.InvalidParam).WithMetadata(map[string]string{
				"paramName": id,
				"source":    source,
			})
		}

		if value == nil {
			bound.Values[id] = nil
			continue
		}

		if err := validateRenderParameterValue(param, value); err != nil {
			return bound, message.New(RecipeParameterMessages.InvalidParam).WithMetadata(map[string]string{
				"paramName": id,
				"value":     fmt.Sprintf("%v", value),
				"source":    source,
			}).WithCause(err)
		}

		bound.Values[id] = value
	}

	return bound, nil
}

func ConvertRenderParameterInputs(inputs map[string]any, params RenderParameters, source string) (map[string]any, error) {
	declared := map[string]RenderParameter{}
	for _, p := range params {
		declared[p.ID] = p
	}

	converted := make(map[string]any, len(inputs))
	for id, value := range inputs {
		param, ok := declared[id]
		if !ok {
			converted[id] = value
			continue
		}

		convertedValue, err := convertRenderParameterInput(param, value)
		if err != nil {
			return nil, message.New(RecipeParameterMessages.InvalidParam).WithMetadata(map[string]string{
				"paramName": id,
				"value":     fmt.Sprintf("%v", value),
				"source":    source,
			}).WithCause(err)
		}

		converted[id] = convertedValue
	}
	return converted, nil
}

func convertRenderParameterInput(param RenderParameter, value any) (any, error) {
	if param.IsArray {
		return convertRenderParameterArrayInput(param, value)
	}

	if param.Type == RenderParameterValueTypeNumber {
		if s, ok := value.(string); ok {
			number, err := strconv.ParseFloat(s, 64)
			if err != nil {
				return nil, fmt.Errorf("expected number value for parameter %q: %w", param.ID, err)
			}
			if math.IsNaN(number) || math.IsInf(number, 0) {
				return nil, fmt.Errorf("expected finite number for parameter %q", param.ID)
			}
			return number, nil
		}
	}

	return value, nil
}

func convertRenderParameterArrayInput(param RenderParameter, value any) (any, error) {
	switch v := value.(type) {
	case string:
		var items []any
		if err := json.Unmarshal([]byte(v), &items); err != nil {
			return nil, fmt.Errorf("expected JSON array value for parameter %q: %w", param.ID, err)
		}
		return items, nil
	case []string:
		items := make([]any, len(v))
		for i, s := range v {
			items[i] = s
		}
		return items, nil
	default:
		return value, nil
	}
}

// ValidateToolIntegrationParameterValues validates the parameter values against their definitions for a tool integration.
func ValidateToolIntegrationParameterValues(params BoundParameters, source string) error {
	return validateParameterValues(params, ToolIntegrationParameterMessages, source)
}

// validateParameterValues validates the parameter values against their definitions.
// Each parameter value is checked for presence if required, and against allowed options where applicable.
func validateParameterValues(params BoundParameters, msgs ParameterMessages, source string) error {
	makeErr := func(paramName, value string, msgCode message.MessageCode) error {
		metadata := map[string]string{
			"paramName": paramName,
			"value":     value,
		}
		if source != "" {
			metadata["source"] = source
		}
		return message.New(msgCode).WithMetadata(metadata)
	}

	for i := range params.Parameters.Input {
		value := params.Values.Input[i]
		if params.Parameters.Input[i].Required && value == "" {
			return makeErr(params.Parameters.Input[i].ID, value, msgs.InvalidInputValue)
		}
	}
	for i := range params.Parameters.SingleSelect {
		value := params.Values.SingleSelect[i]
		if params.Parameters.SingleSelect[i].Required && value == "" {
			return makeErr(params.Parameters.SingleSelect[i].ID, "", msgs.InvalidSingleSelectValue)
		}
		if len(params.Parameters.SingleSelect[i].Options) > 0 && value != "" && !slices.Contains(params.Parameters.SingleSelect[i].Options, value) {
			return makeErr(params.Parameters.SingleSelect[i].ID, value, msgs.InvalidSingleSelectValue)
		}
	}
	for i := range params.Parameters.MultiSelect {
		values := params.Values.MultiSelect[i]
		if params.Parameters.MultiSelect[i].Required && len(values) == 0 {
			return makeErr(params.Parameters.MultiSelect[i].ID, "", msgs.InvalidMultiSelectValue)
		}
		if len(params.Parameters.MultiSelect[i].Options) > 0 {
			for _, entry := range values {
				if !slices.Contains(params.Parameters.MultiSelect[i].Options, entry) {
					return makeErr(params.Parameters.MultiSelect[i].ID, entry, msgs.InvalidMultiSelectValue)
				}
			}
		}
	}
	for i := range params.Parameters.Radio {
		value := params.Values.Radio[i]
		if params.Parameters.Radio[i].Required && value == "" {
			return makeErr(params.Parameters.Radio[i].ID, value, msgs.InvalidRadioValue)
		}
		if len(params.Parameters.Radio[i].Options) > 0 && value != "" && !slices.Contains(params.Parameters.Radio[i].Options, value) {
			return makeErr(params.Parameters.Radio[i].ID, value, msgs.InvalidRadioValue)
		}
	}

	return nil
}

func validateRenderParameterValue(param RenderParameter, value any) error {
	if param.IsArray {
		v, ok := value.([]any)
		if !ok {
			return fmt.Errorf("expected array value for parameter '%s'", param.ID)
		}
		for i, item := range v {
			if err := validateRenderParameterSingleValue(param, item); err != nil {
				return fmt.Errorf("invalid value at index %d: %w", i, err)
			}
		}
		return nil
	} else {
		return validateRenderParameterSingleValue(param, value)
	}
}

func validateRenderParameterSingleValue(param RenderParameter, value any) error {
	switch param.Type {
	case RenderParameterValueTypeString:
		if _, ok := value.(string); !ok {
			return fmt.Errorf("expected string value for parameter '%s'", param.ID)
		}
	case RenderParameterValueTypeNumber:
		number, ok := value.(float64)
		if !ok {
			return fmt.Errorf("expected number value for parameter '%s'", param.ID)
		}
		if math.IsNaN(number) || math.IsInf(number, 0) {
			return fmt.Errorf("expected finite number for parameter '%s'", param.ID)
		}
	default:
		return fmt.Errorf("unknown parameter type '%s' for parameter '%s'", param.Type, param.ID)
	}
	return nil
}

// GetParameterOptionsOrDefault returns the options for the given parameter index.
// If no specific options exist, it falls back to defaultOptions.
func GetParameterOptionsOrDefault(defaultOptions []string, paramIndex int, allOptions [][]string) []string {
	if allOptions != nil && paramIndex < len(allOptions) && allOptions[paramIndex] != nil {
		return allOptions[paramIndex]
	}
	return defaultOptions
}

// GetParameterOptionItemsOrDefault returns rich option metadata for a parameter index.
// If no specific options exist, it falls back to defaultItems.
func GetParameterOptionItemsOrDefault(defaultItems []ParameterOption, paramIndex int, allItems [][]ParameterOption) []ParameterOption {
	if allItems != nil && paramIndex < len(allItems) && allItems[paramIndex] != nil {
		return allItems[paramIndex]
	}
	return defaultItems
}

// GetParameterOptionValuesOrDefault returns option values for the given parameter
// index. If rich option items exist, their values are returned; otherwise it falls
// back to defaultOptions.
func GetParameterOptionValuesOrDefault(defaultOptions []string, paramIndex int, allItems [][]ParameterOption) []string {
	if allItems == nil || paramIndex >= len(allItems) || allItems[paramIndex] == nil {
		return defaultOptions
	}

	items := allItems[paramIndex]
	values := make([]string, len(items))
	for i, item := range items {
		values[i] = item.Value
	}
	return values
}
