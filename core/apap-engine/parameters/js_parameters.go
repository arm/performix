// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package parameters

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/dop251/goja"

	"github.com/Arm-Debug/apap-cli/apap-engine/apiversion"
	"github.com/Arm-Debug/apap-cli/apap-engine/cdf/semver"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
)

// ParameterConfigType identifies the type of a parameter config block.
type ParameterConfigType string

const (
	ParameterConfigTypeInput        ParameterConfigType = "input"
	ParameterConfigTypeRadio        ParameterConfigType = "radio"
	ParameterConfigTypeSingleSelect ParameterConfigType = "single_select"
	ParameterConfigTypeMultiSelect  ParameterConfigType = "multi_select"
	ParameterConfigTypeCheckbox     ParameterConfigType = "checkbox"
)

// RecipeParameterDependency represents a dependency for showing a parameter
// in the JS recipe definition.
type RecipeParameterDependency struct {
	ParameterID string
	Value       string
}

// SingleSelectConfig maps to the JS representation of a single-select parameter configuration.
type SingleSelectConfig struct {
	Type         ParameterConfigType
	DefaultValue string
	Options      any
}

// MultiSelectConfig maps to the JS representation of a multi-select parameter configuration.
type MultiSelectConfig struct {
	Type         ParameterConfigType
	DefaultValue []string
	Options      any
}

// CheckBoxConfig maps to the JS representation of a checkbox parameter configuration.
type CheckBoxConfig struct {
	Type         ParameterConfigType
	DefaultValue bool
}

// InputConfig maps to the JS representation of an input parameter configuration.
type InputConfig struct {
	Type         ParameterConfigType
	DefaultValue string
	Custom       map[string]string
}

// RadioConfig maps to the JS representation of a radio parameter configuration.
type RadioConfig struct {
	Type         ParameterConfigType
	DefaultValue string
	Options      any
	Custom       map[string]string
}

// ParameterOption maps to the JS representation of an option object used by radio parameters.
type ParameterOption struct {
	Value       string
	Label       string
	Description string
}

// ParameterDefinition is the JS mapping of a parameter definition and contains all the common properties.
type ParameterDefinition struct {
	ID           string
	Config       any
	Label        string
	Description  string
	Required     bool
	Visible_When []RecipeParameterDependency
}

type RenderParameterConfigDefinition struct {
	Type any
}

type RenderParameterDefinition struct {
	ID     string
	Config RenderParameterConfigDefinition
}

// ParameterOptionsFunction captures a dynamic options provider for a parameter.
// ParameterIndex references the index within the matching slice on Parameters
// (Select, Radio, etc) so that consumers can correlate functions with their parameters.
type ParameterOptionsFunction struct {
	ParameterID    string
	ParameterType  ParameterConfigType
	ParameterIndex int
	Callback       func(goja.FunctionCall) goja.Value
}

// ExtractRecipeParameters converts JS-defined ParameterDefinition parameters into engine Parameters
// and returns any options functions. Returned errors are recipe-specific.
func ExtractRecipeParameters(params []ParameterDefinition, source string, apiVersion semver.SemVer) (Parameters, []ParameterOptionsFunction, error) {
	return extractParametersWithMessages(params, source, apiVersion, RecipeParameterMessages)
}

// ExtractRenderParameters converts JS-defined RenderParameterDefinition parameters into engine RenderParameters.
// Returned errors are recipe-specific.
func ExtractRenderParameters(params []RenderParameterDefinition, source string) (RenderParameters, error) {
	return extractRenderParameters(params, source)
}

// ExtractToolIntegrationParameters converts JS-defined ParameterDefinition parameters into engine Parameters
// and returns any options functions. Returned errors are tool-integration specific.
func ExtractToolIntegrationParameters(params []ParameterDefinition, source string) (Parameters, []ParameterOptionsFunction, error) {
	return extractParametersWithMessages(params, source, apiversion.LegacyRecipeRadioStringOptionsAPIVersion, ToolIntegrationParameterMessages)
}

// extractParametersWithMessages converts JS-defined ParameterDefinition parameters into engine Parameters
// and returns any options functions.
func extractParametersWithMessages(params []ParameterDefinition, source string, apiVersion semver.SemVer, msgs ParameterMessages) (Parameters, []ParameterOptionsFunction, error) {
	extracted := Parameters{
		Input:        make([]InputParameter, 0, len(params)),
		SingleSelect: make([]SingleSelectParameter, 0, len(params)),
		MultiSelect:  make([]MultiSelectParameter, 0, len(params)),
		Radio:        make([]RadioParameter, 0, len(params)),
		Checkbox:     make([]CheckboxParameter, 0, len(params)),
	}
	optionFunctions := []ParameterOptionsFunction{}

	for i, p := range params {
		visibleWhen := make([]ParameterDependency, len(p.Visible_When))
		for idx, dep := range p.Visible_When {
			visibleWhen[idx] = ParameterDependency(dep)
		}
		paramOut := Parameter{
			ID:          p.ID,
			Label:       p.Label,
			Description: p.Description,
			Required:    p.Required,
			Order:       i,
			VisibleWhen: visibleWhen,
		}

		var optionsFunction func(goja.FunctionCall) goja.Value
		optionsValue := []string{}
		selectOptionItems := []ParameterOption{}
		radioOptionItems := []ParameterOption{}

		if configTypeFields, ok := p.Config.(map[string]interface{}); ok {
			if configType, ok := configTypeFields["type"].(string); ok {
				opt := configTypeFields["options"]
				switch ParameterConfigType(configType) {
				case ParameterConfigTypeSingleSelect, ParameterConfigTypeMultiSelect:
					switch v := opt.(type) {
					case func(goja.FunctionCall) goja.Value:
						optionsFunction = v
					default:
						var err error
						optionsValue, selectOptionItems, err = ConvertRecipeSelectOptionValuesAndItems(v, apiVersion)
						if err != nil {
							return Parameters{}, nil, message.New(msgs.InvalidParam).
								WithMetadata(map[string]string{"paramName": p.ID, "source": source}).
								WithCause(err)
						}
					}
					configTypeFields["options"] = []string{}
				case ParameterConfigTypeRadio:
					switch v := opt.(type) {
					case func(goja.FunctionCall) goja.Value:
						optionsFunction = v
					default:
						var err error
						optionsValue, radioOptionItems, err = ConvertRecipeRadioOptionValuesAndItems(v, apiVersion)
						if err != nil {
							return Parameters{}, nil, message.New(msgs.InvalidParam).
								WithMetadata(map[string]string{"paramName": p.ID, "source": source}).
								WithCause(err)
						}
					}
					configTypeFields["options"] = []string{}
				}
			}
		}

		// We've replaced the options field with a []string, we can now map it onto one of the expected config types, Re-marshal then unmarshal
		data, err := json.Marshal(p.Config)
		if err != nil {
			return Parameters{}, nil, message.New(msgs.InvalidParam).WithMetadata(map[string]string{"paramName": p.ID, "source": source})
		}

		var ic InputConfig
		if err := json.Unmarshal(data, &ic); err == nil && ic.Type == ParameterConfigTypeInput {
			extracted.Input = append(extracted.Input, InputParameter{
				Parameter:    paramOut,
				DefaultValue: ic.DefaultValue,
				Custom:       ic.Custom,
			})
			continue
		}

		var rc RadioConfig
		if err := json.Unmarshal(data, &rc); err == nil && rc.Type == ParameterConfigTypeRadio {
			extracted.Radio = append(extracted.Radio, RadioParameter{
				Parameter:    paramOut,
				DefaultValue: rc.DefaultValue,
				Options:      optionsValue,
				OptionItems:  radioOptionItems,
			})

			if len(optionsValue) > 0 && rc.DefaultValue != "" && !slices.Contains(optionsValue, rc.DefaultValue) {
				return Parameters{}, nil, message.New(msgs.InvalidDefaultParamValue).WithMetadata(map[string]string{
					"paramName": p.ID,
					"value":     rc.DefaultValue,
					"source":    source,
				})
			}

			if optionsFunction != nil {
				optionFunctions = append(optionFunctions, ParameterOptionsFunction{
					ParameterID:    p.ID,
					ParameterType:  rc.Type,
					ParameterIndex: len(extracted.Radio) - 1,
					Callback:       optionsFunction,
				})
			} else {
				if len(optionsValue) == 0 {
					metadata := map[string]string{
						"paramName": paramOut.ID,
						"source":    source,
					}
					return Parameters{}, nil, message.New(msgs.EmptyOptions).WithMetadata(metadata)
				}
			}
			continue
		}

		configType := getConfigType(p.Config)

		var ssc SingleSelectConfig
		if err := json.Unmarshal(data, &ssc); err == nil && ssc.Type == ParameterConfigTypeSingleSelect {
			extracted.SingleSelect = append(extracted.SingleSelect, SingleSelectParameter{
				Parameter:    paramOut,
				DefaultValue: ssc.DefaultValue,
				Options:      optionsValue,
				OptionItems:  selectOptionItems,
			})
			if len(optionsValue) > 0 {
				if ssc.DefaultValue != "" && !slices.Contains(optionsValue, ssc.DefaultValue) {
					return Parameters{}, nil, message.New(msgs.InvalidDefaultParamValue).WithMetadata(map[string]string{
						"paramName": p.ID,
						"value":     ssc.DefaultValue,
						"source":    source,
					})
				}
			}
			if optionsFunction != nil {
				optionFunctions = append(optionFunctions, ParameterOptionsFunction{
					ParameterID:    p.ID,
					ParameterType:  ssc.Type,
					ParameterIndex: len(extracted.SingleSelect) - 1,
					Callback:       optionsFunction,
				})
			} else if len(optionsValue) == 0 {
				metadata := map[string]string{
					"paramName": paramOut.ID,
					"source":    source,
				}
				return Parameters{}, nil, message.New(msgs.EmptyOptions).WithMetadata(metadata)
			}
			continue
		} else if err != nil && configType == "single_select" {
			return Parameters{}, nil, message.New(msgs.InvalidDefaultParamValue).WithMetadata(map[string]string{
				"paramName": p.ID,
				"value":     fmt.Sprintf("%v", getConfigDefaultValue(p.Config)),
				"source":    source,
			})
		}

		var msc MultiSelectConfig
		if err := json.Unmarshal(data, &msc); err == nil && msc.Type == ParameterConfigTypeMultiSelect {
			var defaultValue []string
			if msc.DefaultValue != nil {
				defaultValue = append([]string{}, msc.DefaultValue...)
			}

			extracted.MultiSelect = append(extracted.MultiSelect, MultiSelectParameter{
				Parameter:    paramOut,
				DefaultValue: defaultValue,
				Options:      optionsValue,
				OptionItems:  selectOptionItems,
			})
			if len(optionsValue) > 0 {
				for _, dv := range defaultValue {
					if !slices.Contains(optionsValue, dv) {
						return Parameters{}, nil, message.New(msgs.InvalidDefaultParamValue).WithMetadata(map[string]string{
							"paramName": p.ID,
							"value":     dv,
							"source":    source,
						})
					}
				}
			}
			if optionsFunction != nil {
				optionFunctions = append(optionFunctions, ParameterOptionsFunction{
					ParameterID:    p.ID,
					ParameterType:  msc.Type,
					ParameterIndex: len(extracted.MultiSelect) - 1,
					Callback:       optionsFunction,
				})
			} else if len(optionsValue) == 0 {
				metadata := map[string]string{
					"paramName": paramOut.ID,
					"source":    source,
				}
				return Parameters{}, nil, message.New(msgs.EmptyOptions).WithMetadata(metadata)
			}
			continue
		} else if err != nil && configType == "multi_select" {
			return Parameters{}, nil, message.New(msgs.InvalidDefaultParamValue).WithMetadata(map[string]string{
				"paramName": p.ID,
				"value":     fmt.Sprintf("%v", getConfigDefaultValue(p.Config)),
				"source":    source,
			})
		}

		var cc CheckBoxConfig
		if err := json.Unmarshal(data, &cc); err == nil && cc.Type == ParameterConfigTypeCheckbox {
			extracted.Checkbox = append(extracted.Checkbox, CheckboxParameter{
				Parameter:    paramOut,
				DefaultValue: cc.DefaultValue,
			})
			continue
		}
		return Parameters{}, nil, message.New(msgs.InvalidParam).
			WithMetadata(map[string]string{"paramName": p.ID, "source": source})
	}

	return extracted, optionFunctions, nil
}

func getConfigDefaultValue(config any) any {
	fields, ok := config.(map[string]any)
	if !ok {
		return config
	}
	return fields["defaultValue"]
}

func getConfigType(config any) string {
	fields, ok := config.(map[string]any)
	if !ok {
		return ""
	}
	configType, _ := fields["type"].(string)
	return configType
}

func extractRenderParameters(params []RenderParameterDefinition, source string) (RenderParameters, error) {
	renderParams := make(RenderParameters, 0, len(params))
	for i, p := range params {
		valueType, isArray, err := parseRenderParameterType(p.Config.Type)
		if err != nil {
			return nil, message.New(RecipeParameterMessages.InvalidParam).
				WithMetadata(map[string]string{"paramName": p.ID, "source": source}).
				WithCause(err)
		}
		renderParams = append(renderParams, RenderParameter{
			ID:      p.ID,
			Type:    valueType,
			IsArray: isArray,
			Order:   i,
		})
	}
	return renderParams, nil
}

func parseRenderParameterType(t any) (RenderParameterValueType, bool, error) {
	switch v := t.(type) {
	case string:
		valueType, err := parseRenderParameterScalarType(v)
		return valueType, false, err
	case []interface{}:
		if len(v) != 1 {
			return "", false, fmt.Errorf("array render parameter type must have exactly one element, got %d", len(v))
		}
		scalar, ok := v[0].(string)
		if !ok {
			return "", false, fmt.Errorf("render parameter type array element must be a string")
		}
		valueType, err := parseRenderParameterScalarType(scalar)
		return valueType, true, err

	default:
		return "", false, fmt.Errorf("invalid render parameter type: %v", t)
	}
}

func parseRenderParameterScalarType(t string) (RenderParameterValueType, error) {
	switch RenderParameterValueType(t) {
	case RenderParameterValueTypeNumber:
		return RenderParameterValueTypeNumber, nil
	case RenderParameterValueTypeString:
		return RenderParameterValueTypeString, nil
	default:
		return "", fmt.Errorf("unknown render parameter type: %s", t)
	}
}

func convertToStringSlice(data []interface{}) []string {
	results := make([]string, len(data))
	for i, v := range data {
		s, ok := v.(string)
		if !ok {
			return nil
		}
		results[i] = s
	}
	return results
}
