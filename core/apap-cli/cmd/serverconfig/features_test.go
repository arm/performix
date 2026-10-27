// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package serverconfig

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/featureflags"
)

func TestFeatureDefinitionsCoverEveryField(t *testing.T) {
	var featureFlags featureflags.Flags
	// Map the actual field addresses to their diagnostic configuration keys.
	// Checking addresses also catches pointers into the wrong Flags instance.
	value := reflect.ValueOf(&featureFlags).Elem()
	expected := map[any]string{}
	for i := 0; i < value.NumField(); i++ {
		expected[value.Field(i).Addr().Interface()] = value.Type().Field(i).Tag.Get("json")
	}
	definitions := featureDefinitions(&featureFlags)
	require.Len(t, definitions, len(expected))
	keys := map[string]bool{}
	for _, def := range definitions {
		require.False(t, keys[def.key], "duplicate key %s", def.key)
		keys[def.key] = true
		field := reflect.ValueOf(def.field)
		require.True(t, field.IsValid(), "missing field for %s", def.key)
		require.Equal(t, reflect.Pointer, field.Kind(), def.key)
		require.False(t, field.IsNil(), def.key)
		key, exists := expected[def.field]
		require.True(t, exists, "duplicate or foreign field for %s", def.key)
		require.Equal(t, key, def.key, "JSON key differs")
		delete(expected, def.field)
	}
	require.Empty(t, expected, "fields missing from registry")
}

// Produce non-default inputs for every supported type, so future non-boolean
// entries automatically participate in the configuration tests.
func differentValue(value any) any {
	switch value := value.(type) {
	case bool:
		return !value
	case int:
		return value + 1
	case string:
		return value + "-override"
	default:
		panic(fmt.Sprintf("unsupported test value: %T", value))
	}
}

func TestFeatureDefaults(t *testing.T) {
	v := viper.New()
	RegisterFeatureDefaults(v)
	require.Equal(t, featureflags.Flags{
		FullCaptureSupport:      true,
		Rerendering:             true,
		SecondaryRunPaths:       true,
		RenderDBSandbox:         true,
		RunComponentCompression: true,
	}, LoadFeatureFlags(v))
}

func TestFeatureConfigurationPrecedence(t *testing.T) {
	var featureFlags featureflags.Flags
	for _, def := range featureDefinitions(&featureFlags) {
		t.Run(def.key, func(t *testing.T) {
			v := viper.New()
			RegisterFeatureDefaults(v)
			v.SetConfigType("yaml")
			v.SetEnvPrefix("APXD")
			v.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
			v.AutomaticEnv()
			envKey := "APXD_" + strings.ToUpper(strings.ReplaceAll(def.key, "-", "_"))
			t.Setenv(envKey, "")

			yaml, err := json.Marshal(map[string]any{def.key: differentValue(def.defaultValue)})
			require.NoError(t, err)
			require.NoError(t, v.ReadConfig(strings.NewReader(string(yaml))))
			featureFlags = LoadFeatureFlags(v)
			require.Equal(t, differentValue(def.defaultValue), reflect.ValueOf(def.field).Elem().Interface(), "YAML overrides default")

			t.Setenv(envKey, fmt.Sprint(def.defaultValue))
			featureFlags = LoadFeatureFlags(v)
			require.Equal(t, def.defaultValue, reflect.ValueOf(def.field).Elem().Interface(), "environment overrides YAML")
		})
	}
}

func TestFeatureDefinitionTypes(t *testing.T) {
	// Exercise int/string support without adding fictitious engine features.
	var enabled bool
	var mode int
	var backend string
	cases := []struct {
		def   featureDefinition
		value any
	}{
		{defineFeature("enabled", true, &enabled), false},
		{defineFeature("mode", 2, &mode), 0},
		{defineFeature("backend", "standard", &backend), ""},
		{defineFeature("backend", "standard", &backend), "fast backend=custom"},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s=%v", tc.def.key, tc.value), func(t *testing.T) {
			def := tc.def
			v := viper.New()
			v.SetDefault(def.key, def.defaultValue)
			def.load(v)
			require.Equal(t, def.defaultValue, reflect.ValueOf(def.field).Elem().Interface())
			v.Set(def.key, tc.value)
			def.load(v)
			require.Equal(t, tc.value, reflect.ValueOf(def.field).Elem().Interface())
		})
	}
}

func TestFeatureDefinitionsKeepInstancesIndependent(t *testing.T) {
	var first, second featureflags.Flags
	firstDefinitions := featureDefinitions(&first)
	secondDefinitions := featureDefinitions(&second)
	v := viper.New()
	RegisterFeatureDefaults(v)
	for _, def := range firstDefinitions {
		def.load(v)
	}
	require.Equal(t, LoadFeatureFlags(v), first)
	require.Equal(t, featureflags.Flags{}, second)
	v.Set("enable-jfr-capture", true)
	for _, def := range secondDefinitions {
		def.load(v)
	}
	require.True(t, second.JfrCapture)
	require.False(t, first.JfrCapture)
}
