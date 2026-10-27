// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package serverconfig

import (
	"github.com/spf13/viper"

	"github.com/Arm-Debug/apap-cli/apap-engine/featureflags"
)

type featureDefinition struct {
	key          string
	defaultValue any
	field        any
	load         func(*viper.Viper)
}

// featureDefinitions specifies where each feature value comes from, its default,
// and where it is stored in Flags. Features are configured through YAML or environment variables.
func featureDefinitions(featureFlags *featureflags.Flags) []featureDefinition {
	return []featureDefinition{
		defineFeature(EnableAndroidTargetsConfigKey, false, &featureFlags.AndroidTargets),
		defineFeature("enable-full-capture-support", true, &featureFlags.FullCaptureSupport),
		defineFeature("enable-rerendering", true, &featureFlags.Rerendering),
		defineFeature("enable-experimental-recipes", false, &featureFlags.ExperimentalRecipes),
		defineFeature("enable-secondary-run-paths", true, &featureFlags.SecondaryRunPaths),
		defineFeature(EnableGPURecipeConfigKey, false, &featureFlags.GPURecipe),
		defineFeature("enable-render-db-sandbox", true, &featureFlags.RenderDBSandbox),
		defineFeature("enable-neoprof-timeline", false, &featureFlags.NeoprofTimeline),
		defineFeature("enable-neoprof-system-utilization", false, &featureFlags.NeoprofSystemUtilization),
		defineFeature("enable-jfr-capture", false, &featureFlags.JfrCapture),
		defineFeature("enable-run-component-compression", true, &featureFlags.RunComponentCompression),
	}
}

// RegisterFeatureDefaults sets Viper's default value for every feature setting.
func RegisterFeatureDefaults(v *viper.Viper) {
	var featureFlags featureflags.Flags
	for _, def := range featureDefinitions(&featureFlags) {
		v.SetDefault(def.key, def.defaultValue)
	}
}

// LoadFeatureFlags reads the configured feature values from Viper.
func LoadFeatureFlags(v *viper.Viper) featureflags.Flags {
	var featureFlags featureflags.Flags
	for _, def := range featureDefinitions(&featureFlags) {
		def.load(v)
	}
	return featureFlags
}

// featureValue lists the exact types supported by the loader below.
// Named types are deliberately excluded so the pointer switch covers every case.
type featureValue interface {
	bool | int | string
}

// defineFeature checks the field and default types at compile time. The loader
// captures this settings instance's field pointer, so instances remain independent.
func defineFeature[T featureValue](key string, defaultValue T, field *T) featureDefinition {
	return featureDefinition{
		key:          key,
		defaultValue: defaultValue,
		field:        field,
		load: func(v *viper.Viper) {
			switch p := any(field).(type) {
			case *bool:
				*p = v.GetBool(key)
			case *int:
				*p = v.GetInt(key)
			case *string:
				*p = v.GetString(key)
			}
		},
	}
}
