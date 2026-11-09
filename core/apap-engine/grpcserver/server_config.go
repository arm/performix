// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/spf13/viper"

	"github.com/Arm-Debug/apap-cli/apap-engine/featureflags"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
)

type ApapServerConfig struct {
	ParallelJobs         uint               `json:"jobs"`
	DataDirectory        string             `json:"data-dir"`
	FeatureFlags         featureflags.Flags `json:"-"`
	ServerHostname       string             `json:"server-hostname"`
	ServerGRPCPort       int                `json:"server-port"`
	ServerHTTPPort       int                `json:"http-port"`
	ServerHTTPChunkBytes int                `json:"http-chunk-bytes"`
	LogLevel             string             `json:"log-level"`
	LogFile              string             `json:"log-file"`
	SourceToolsDir       string             `json:"source-tools-dir"`
	ConfigDirectory      string             `json:"config-dir"`
	ADBPath              string             `json:"adb-path"`

	ConfigFile          string `json:"-"`
	TelemetryEnabled    bool   `json:"-"`
	TelemetryConfigured bool   `json:"-"`
	telemetryMu         sync.RWMutex
}

func (c *ApapServerConfig) GetTelemetryPreference() (enabled, configured bool) {
	c.telemetryMu.RLock()
	defer c.telemetryMu.RUnlock()
	return c.TelemetryEnabled, c.TelemetryConfigured
}

func (c *ApapServerConfig) SetTelemetryPreference(enabled bool) error {
	c.telemetryMu.Lock()
	defer c.telemetryMu.Unlock()
	if err := saveTelemetryPreference(c.ConfigFile, enabled); err != nil {
		return err
	}
	c.TelemetryEnabled = enabled
	c.TelemetryConfigured = true
	return nil
}

func saveTelemetryPreference(path string, enabled bool) error {
	// Load only file values so saving cannot persist CLI or environment overrides.
	config := viper.New()
	config.SetConfigFile(path)
	if err := config.ReadInConfig(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), perms.LocalDirPerm); err != nil {
		return err
	}
	config.Set("telemetry-enabled", enabled)
	return config.WriteConfigAs(path)
}
