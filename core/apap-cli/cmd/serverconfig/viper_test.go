// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package serverconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/featureflags"
	"github.com/Arm-Debug/apap-cli/apap-engine/grpcserver"
)

func TestGetLogPath(t *testing.T) {
	t.Run("if log file isn't stdout, it returns an absolute path", func(t *testing.T) {
		cwd, err := os.Getwd()
		require.NoError(t, err)
		defer func() {
			err = os.Chdir(cwd)
			require.NoError(t, err)
		}()

		tempDir := t.TempDir()
		err = os.Chdir(tempDir)
		require.NoError(t, err)

		logFile := "./apxd.log"
		viper.Set("log-file", logFile)
		logPath := getLogPath()
		assert.True(t, filepath.IsAbs(logPath))
		assert.Contains(t, logPath, filepath.Join(tempDir, "apxd.log"))
	})

	t.Run("if log file is stdout, it returns the path unmodified", func(t *testing.T) {
		viper.Set("log-file", stdout)
		logPath := getLogPath()
		assert.Equal(t, "stdout", logPath)
	})
}

func TestFromViper(t *testing.T) {
	t.Run("uses automatic port defaults", func(t *testing.T) {
		viper.Reset()
		t.Cleanup(viper.Reset)

		config := FromViper()

		assert.Equal(t, AutomaticPort, config.Port)
		assert.Zero(t, config.HttpPort)
	})

	t.Run("returns server config built from viper configuration", func(t *testing.T) {
		viper.Reset()
		t.Cleanup(viper.Reset)
		host := "example.com"
		viper.Set("server-hostname", host)
		port := 123
		viper.Set("server-port", port)
		httpPort := 456
		viper.Set("http-port", httpPort)
		httpChunkBytes := 1048576
		viper.Set("http-chunk-bytes", httpChunkBytes)
		jobs := 2
		viper.Set("jobs", jobs)
		dataDir, _ := filepath.Abs("my-data")
		viper.Set("data-dir", dataDir)
		logFile, _ := filepath.Abs(filepath.Join("var", "log", "apxd.log"))
		viper.Set("log-file", logFile)
		logLevel := "debug"
		viper.Set("log-level", logLevel)
		viper.Set("enable-experimental-recipes", true)
		viper.Set("enable-secondary-run-paths", false)
		viper.Set(EnableGPURecipeConfigKey, true)
		viper.Set("enable-render-db-sandbox", false)
		viper.Set("enable-neoprof-timeline", true)
		viper.Set("enable-neoprof-system-utilization", true)
		viper.Set("enable-jfr-capture", true)
		viper.Set("enable-run-component-compression", false)

		config := FromViper()

		assert.Equal(t, grpcserver.GrpcServerConfig{
			Host:           host,
			Port:           port,
			HttpPort:       httpPort,
			HttpChunkBytes: httpChunkBytes,
			ParallelJobs:   uint(jobs), //nolint:gosec // jobs is expected to be a small positive value
			DataDirectory:  dataDir,
			LogPath:        logFile,
			LogLevel:       logLevel,
			FeatureFlags: featureflags.Flags{
				ExperimentalRecipes:      true,
				SecondaryRunPaths:        false,
				GPURecipe:                true,
				RenderDBSandbox:          false,
				NeoprofTimeline:          true,
				NeoprofSystemUtilization: true,
				JfrCapture:               true,
				RunComponentCompression:  false,
			},
			ConfigDirectory: DefaultConfigDir,
		}, config)
	})
}

func TestBackgroundFromViper(t *testing.T) {
	t.Run("if log file isn't stdout, it returns unaltered config", func(t *testing.T) {
		logFile := "/var/log/apxd.log"
		viper.Set("log-file", logFile)

		config := FromViperForBackground()

		assert.Equal(t, FromViper(), config)
	})

	t.Run("if log file is stdout, it updates config with new log file", func(t *testing.T) {
		viper.Set("log-file", "stdout")

		config := FromViperForBackground()

		// Generated log file paths are time dependant, so we're smoke testing for matching value
		assert.NotEqual(t, "stdout", config.LogPath)
		assert.True(t, strings.HasSuffix(config.LogPath, ".log"))
	})
}

func TestFromViperResolvesDeploymentToolsDir(t *testing.T) {
	t.Cleanup(func() {
		viper.Reset()
		viper.SetDefault("server-port", DefaultServerPort)
		viper.Set("server-port", DefaultServerPort)
		viper.Set("data-dir", "./testdata/data")
		viper.Set("deployment-tools-dir", "./relative/tools")

		cfg := FromViper()

		expected := filepath.Clean("./relative/tools")
		if filepath.Clean(cfg.DeploymentToolsDir) != expected {
			t.Fatalf("expected %s, got %s", expected, cfg.DeploymentToolsDir)
		}
	})
}
