// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/api/apapproto"
)

func TestProductTelemetryPreference(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "apx.yml")
	server := &ApapServer{config: &ApapServerConfig{ConfigFile: path, TelemetryEnabled: true}}
	initial, err := server.GetProductTelemetryPreference(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	assert.True(t, initial.Enabled)
	assert.False(t, initial.Configured)

	for _, enabled := range []bool{false, true} {
		_, err := server.SetProductTelemetryPreference(context.Background(), &apapproto.ProductTelemetryPreference{Enabled: enabled})
		require.NoError(t, err)
		current, err := server.GetProductTelemetryPreference(context.Background(), &emptypb.Empty{})
		require.NoError(t, err)
		assert.Equal(t, enabled, current.Enabled)
		assert.True(t, current.Configured)

		saved := viper.New()
		saved.SetConfigFile(path)
		require.NoError(t, saved.ReadInConfig())
		assert.True(t, saved.InConfig("telemetry-enabled"))
		assert.Equal(t, enabled, saved.GetBool("telemetry-enabled"))
	}
}

func TestSaveTelemetryPreferencePreservesFileValuesOnly(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("jobs", 32)
	viper.SetDefault("default-only", true)
	viper.SetEnvPrefix("APXD")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()
	t.Setenv("APXD_JOBS", "64")
	path := filepath.Join(t.TempDir(), "custom.yml")
	require.NoError(t, os.WriteFile(path, []byte("jobs: 4\n"), 0o600))

	require.NoError(t, saveTelemetryPreference(path, false))
	saved := viper.New()
	saved.SetConfigFile(path)
	require.NoError(t, saved.ReadInConfig())
	assert.Equal(t, map[string]any{"jobs": 4, "telemetry-enabled": false}, saved.AllSettings())
}

func TestProductTelemetryPreferenceSaveFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apx.yml")
	require.NoError(t, os.WriteFile(path, []byte("jobs: ["), 0o600))
	server := &ApapServer{config: &ApapServerConfig{ConfigFile: path, TelemetryEnabled: true}}
	_, err := server.SetProductTelemetryPreference(context.Background(), &apapproto.ProductTelemetryPreference{Enabled: false})
	var msg message.Message
	require.ErrorAs(t, err, &msg)
	assert.Equal(t, message.EngineProductTelemetryPreferenceUpdateFailed, msg.Code())
	current, err := server.GetProductTelemetryPreference(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	assert.True(t, current.Enabled)
	assert.False(t, current.Configured)
}

func TestProductTelemetryPreferenceConcurrentAccess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apx.yml")
	config := &ApapServerConfig{ConfigFile: path, TelemetryEnabled: true}
	var wg sync.WaitGroup
	for range 4 {
		server := &ApapServer{config: config}
		wg.Go(func() {
			for i := range 10 {
				_, err := server.SetProductTelemetryPreference(context.Background(), &apapproto.ProductTelemetryPreference{Enabled: i%2 == 0})
				if !assert.NoError(t, err) {
					return
				}
				current, err := server.GetProductTelemetryPreference(context.Background(), &emptypb.Empty{})
				if !assert.NoError(t, err) {
					return
				}
				assert.True(t, current.Configured)
				assert.NotEmpty(t, server.buildSupportPackageConfig())
			}
		})
	}
	wg.Wait()

	enabled, configured := config.GetTelemetryPreference()
	assert.True(t, configured)
	saved := viper.New()
	saved.SetConfigFile(path)
	require.NoError(t, saved.ReadInConfig())
	assert.Equal(t, enabled, saved.GetBool("telemetry-enabled"))
}
