// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package parameters

import (
	"fmt"

	"github.com/Arm-Debug/apap-cli/apap-engine/apiversion"
	"github.com/Arm-Debug/apap-cli/apap-engine/cdf/semver"
)

// ConvertOptionValues converts options returned from JS into their string values.
// Supported option item shapes:
// - string
// - object containing a string `value` field
func ConvertOptionValues(data []interface{}) []string {
	values, _ := ConvertOptionValuesAndItems(data)
	return values
}

// ConvertOptionValuesAndItems converts options returned from JS into value and
// rich option entries. For string entries, value and label are set to the
// string while description remains empty.
func ConvertOptionValuesAndItems(data []interface{}) ([]string, []ParameterOption) {
	results := make([]string, len(data))
	items := make([]ParameterOption, len(data))
	for i, v := range data {
		if s, ok := v.(string); ok {
			results[i] = s
			items[i] = ParameterOption{Value: s, Label: s}
			continue
		}

		option, ok := v.(map[string]interface{})
		if !ok {
			return nil, nil
		}
		value, ok := option["value"].(string)
		if !ok {
			return nil, nil
		}
		results[i] = value
		item := ParameterOption{Value: value}
		if label, ok := option["label"].(string); ok {
			item.Label = label
		}
		if desc, ok := option["description"].(string); ok {
			item.Description = desc
		}
		items[i] = item
	}
	return results, items
}

func convertRecipeOptionValuesAndItems(data any, apiVersion semver.SemVer, legacyVersion semver.SemVer, paramType string) ([]string, []ParameterOption, error) {
	switch v := data.(type) {
	case []string:
		if semver.Cmp(apiVersion, legacyVersion) > 0 {
			return nil, nil, fmt.Errorf("%s options for recipe api_version %s must use option objects", paramType, apiVersion.String())
		}
		values := append([]string(nil), v...)
		items := make([]ParameterOption, len(v))
		for i, sv := range v {
			items[i] = ParameterOption{Value: sv, Label: sv}
		}
		return values, items, nil
	case []interface{}:
		results := make([]string, len(v))
		items := make([]ParameterOption, len(v))
		for i, itemValue := range v {
			if s, ok := itemValue.(string); ok {
				if semver.Cmp(apiVersion, legacyVersion) > 0 {
					return nil, nil, fmt.Errorf("%s options for recipe api_version %s must use option objects", paramType, apiVersion.String())
				}
				results[i] = s
				items[i] = ParameterOption{Value: s, Label: s}
				continue
			}

			option, ok := itemValue.(map[string]interface{})
			if !ok {
				return nil, nil, fmt.Errorf("%s option at index %d must be a string or object", paramType, i)
			}
			value, ok := option["value"].(string)
			if !ok {
				return nil, nil, fmt.Errorf("%s option object at index %d must contain a string value field", paramType, i)
			}
			results[i] = value
			item := ParameterOption{Value: value}
			if label, ok := option["label"].(string); ok {
				item.Label = label
			}
			if desc, ok := option["description"].(string); ok {
				item.Description = desc
			}
			items[i] = item
		}
		return results, items, nil
	default:
		return nil, nil, nil
	}
}

// ConvertRecipeSelectOptionValuesAndItems converts static or dynamic select
// options for recipe parsing. String option arrays are accepted only for legacy
// recipe api_version 1.0.0; newer recipe versions must provide option objects.
func ConvertRecipeSelectOptionValuesAndItems(data any, apiVersion semver.SemVer) ([]string, []ParameterOption, error) {
	return convertRecipeOptionValuesAndItems(data, apiVersion, apiversion.LegacyRecipeSelectStringOptionsAPIVersion, "select")
}

// ConvertRecipeRadioOptionValuesAndItems converts static or dynamic radio
// options for recipe parsing. String option arrays are accepted only for legacy
// recipe api_version 1.0.0; newer recipe versions must provide option objects.
func ConvertRecipeRadioOptionValuesAndItems(data any, apiVersion semver.SemVer) ([]string, []ParameterOption, error) {
	return convertRecipeOptionValuesAndItems(data, apiVersion, apiversion.LegacyRecipeRadioStringOptionsAPIVersion, "radio")
}
