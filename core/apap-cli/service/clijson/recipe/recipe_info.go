// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipe

import (
	"strings"

	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

type ParameterBaseJSON struct {
	ID          string `json:"id"`
	Required    bool   `json:"required"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

type ParameterOptionJSON struct {
	Value       string `json:"value"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// Config holds type-specific settings
type ParameterConfigJSON struct {
	Type         string                `json:"type"`
	Options      []string              `json:"options,omitempty"` // []string
	OptionItems  []ParameterOptionJSON `json:"optionItems,omitempty"`
	DefaultValue interface{}           `json:"defaultValue,omitempty"` // string, []string, bool
}

// Unified parameter type (instead of separate structs per type)
type ParameterJSON struct {
	ParameterBaseJSON
	Config ParameterConfigJSON `json:"config"`
}

type RenderParameterJSON struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	IsArray bool   `json:"is_array"`
}

type RequirementSpecJSON struct {
	Type       string            `json:"type"`
	Parameters map[string]string `json:"parameters"`
}

type TargetSupportJSON struct {
	Result     string                `json:"result"`
	Conditions []RequirementSpecJSON `json:"conditions,omitempty"`
}

type PlatformConfigurationJSON struct {
	Architecture string `json:"architecture"`
	OS           string `json:"os"`
}

type SupportedPlatformsJSON struct {
	Platform   PlatformConfigurationJSON `json:"platform"`
	Result     string                    `json:"result"`
	Conditions []RequirementSpecJSON     `json:"conditions,omitempty"`
}

type RecipeJSON struct {
	Name               string                   `json:"name"`
	Title              string                   `json:"title"`
	Description        string                   `json:"description"`
	MCPGuidance        string                   `json:"mcp_guidance,omitempty"`
	Version            string                   `json:"version"`
	Parameters         []ParameterJSON          `json:"parameters"`
	RenderParameters   []RenderParameterJSON    `json:"render_parameters,omitempty"`
	SupportedPlatforms []SupportedPlatformsJSON `json:"supported_platforms,omitempty"`
	TargetSupport      TargetSupportJSON        `json:"target_support,omitempty"`
}

func ConvertRecipeStruct(in *apapproto.ParseRecipeResponse, targetSupportMode bool) RecipeJSON {
	out := RecipeJSON{
		Name:        in.GetName(),
		Title:       in.GetTitle(),
		Description: in.GetDescription(),
		MCPGuidance: in.GetMcpGuidance(),
		Version:     in.GetVersion(),
	}

	for _, param := range in.GetParameters() {
		if converted, ok := convertParameterList(param); ok {
			out.Parameters = append(out.Parameters, converted)
		}
	}

	for _, param := range in.GetRenderParameters() {
		if converted, ok := convertRenderParameterList(param); ok {
			out.RenderParameters = append(out.RenderParameters, converted)
		}
	}

	if in.GetPlatformSupport() == nil {
		// No platform support information, return what we have
		return out
	}

	if targetSupportMode {
		// We expect exactly one platform support entry when a target is provided
		if len(in.GetPlatformSupport()) != 1 {
			return out
		}
		targetSupport := TargetSupportJSON{}
		targetSupport.Result = ConvertPlatformSupportResultToString(in.GetPlatformSupport()[0].GetResult(), false)
		for _, cond := range in.GetPlatformSupport()[0].GetConditionList() {
			condition := RequirementSpecJSON{Type: cond.GetType(), Parameters: cond.GetParameters()}
			targetSupport.Conditions = append(targetSupport.Conditions, condition)
		}
		out.TargetSupport = targetSupport
		return out
	}
	for _, ps := range in.GetPlatformSupport() {
		if ps == nil {
			// We don't expect to have nil entries, but just in case
			continue
		}
		if ps.GetResult() == apapproto.PlatformSupportResult_PLATFORM_UNSUPPORTED {
			// Skip unsupported platforms in non-target mode
			continue
		}
		// Add supported or conditionally supported platforms
		supportedPlatform := SupportedPlatformsJSON{Platform: PlatformConfigurationJSON{ps.GetPlatform().GetArch(), ps.GetPlatform().GetOs()}}
		supportedPlatform.Result = ConvertPlatformSupportResultToString(ps.GetResult(), false)
		for _, cond := range ps.GetConditionList() {
			condition := RequirementSpecJSON{Type: cond.GetType(), Parameters: cond.GetParameters()}
			supportedPlatform.Conditions = append(supportedPlatform.Conditions, condition)
		}
		out.SupportedPlatforms = append(out.SupportedPlatforms, supportedPlatform)
	}
	return out
}

func convertBase(base *apapproto.RecipeParameterBase) ParameterBaseJSON {
	if base == nil {
		return ParameterBaseJSON{}
	}
	return ParameterBaseJSON{
		ID:          base.GetID(),
		Required:    base.GetRequired(),
		Label:       base.GetLabel(),
		Description: base.GetDescription(),
	}
}

func convertParameterList(param *apapproto.ParameterList) (ParameterJSON, bool) {
	switch p := param.GetParameter().(type) {
	case *apapproto.ParameterList_SingleSelect:
		options := ConvertOptionItems(p.SingleSelect.GetOptions())
		return ParameterJSON{
			ParameterBaseJSON: convertBase(p.SingleSelect.GetBase()),
			Config: ParameterConfigJSON{
				Type:         "single_select",
				Options:      optionValues(options),
				OptionItems:  options,
				DefaultValue: p.SingleSelect.GetDefaultValue(),
			},
		}, true
	case *apapproto.ParameterList_MultiSelect:
		options := ConvertOptionItems(p.MultiSelect.GetOptions())
		return ParameterJSON{
			ParameterBaseJSON: convertBase(p.MultiSelect.GetBase()),
			Config: ParameterConfigJSON{
				Type:         "multi_select",
				Options:      optionValues(options),
				OptionItems:  options,
				DefaultValue: p.MultiSelect.GetDefaultValue(),
			},
		}, true
	case *apapproto.ParameterList_Radio:
		options := ConvertOptionItems(p.Radio.GetOptions())
		return ParameterJSON{
			ParameterBaseJSON: convertBase(p.Radio.GetBase()),
			Config: ParameterConfigJSON{
				Type:         "radio",
				Options:      optionValues(options),
				OptionItems:  options,
				DefaultValue: p.Radio.GetDefaultValue(),
			},
		}, true
	case *apapproto.ParameterList_Checkbox:
		return ParameterJSON{
			ParameterBaseJSON: convertBase(p.Checkbox.GetBase()),
			Config: ParameterConfigJSON{
				Type:         "checkbox",
				DefaultValue: p.Checkbox.GetDefaultValue(),
			},
		}, true
	case *apapproto.ParameterList_Input:
		return ParameterJSON{
			ParameterBaseJSON: convertBase(p.Input.GetBase()),
			Config: ParameterConfigJSON{
				Type:         "input",
				DefaultValue: p.Input.GetDefaultValue(),
			},
		}, true
	default:
		return ParameterJSON{}, false
	}
}

func convertRenderParameterList(param *apapproto.RenderParameter) (RenderParameterJSON, bool) {
	if param == nil {
		return RenderParameterJSON{}, false
	}

	paramType, ok := ConvertRenderParameterTypeToString(param.GetType())
	if !ok {
		return RenderParameterJSON{}, false
	}

	return RenderParameterJSON{
		ID:      param.GetId(),
		Type:    paramType,
		IsArray: param.GetIsArray(),
	}, true
}

func optionValues(options []ParameterOptionJSON) []string {
	if len(options) == 0 {
		return nil
	}
	values := make([]string, len(options))
	for i, option := range options {
		values[i] = option.Value
	}
	return values
}

func ConvertOptionItems(items []*apapproto.ParameterOption) []ParameterOptionJSON {
	if len(items) == 0 {
		return nil
	}
	out := make([]ParameterOptionJSON, len(items))
	for i, item := range items {
		out[i] = ParameterOptionJSON{
			Value:       item.GetValue(),
			Label:       item.GetLabel(),
			Description: item.GetDescription(),
		}
	}
	return out
}

func ConvertPlatformSupportResultToString(r apapproto.PlatformSupportResult, pretty bool) string {
	var label string
	switch r {
	case apapproto.PlatformSupportResult_PLATFORM_SUPPORTED:
		label = "Supported"
	case apapproto.PlatformSupportResult_PLATFORM_UNSUPPORTED:
		label = "Unsupported"
	case apapproto.PlatformSupportResult_PLATFORM_CONDITIONALLY_SUPPORTED:
		label = "Conditionally Supported"
	default:
		label = "Unknown"
	}
	if !pretty {
		return strings.ReplaceAll(strings.ToLower(label), " ", "_")
	}
	return label
}

func ConvertRenderParameterTypeToString(t apapproto.RenderParameterType) (string, bool) {
	switch t {
	case apapproto.RenderParameterType_RENDER_PARAMETER_TYPE_STRING:
		return "string", true
	case apapproto.RenderParameterType_RENDER_PARAMETER_TYPE_NUMBER:
		return "number", true
	default:
		return "unknown", false
	}
}
