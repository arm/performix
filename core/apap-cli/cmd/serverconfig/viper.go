// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package serverconfig

import (
	"github.com/spf13/viper"

	"github.com/Arm-Debug/apap-cli/apap-cli/resolvepath"
	"github.com/Arm-Debug/apap-cli/apap-engine/grpcserver"
	"github.com/Arm-Debug/apap-cli/apap-engine/logging"
)

func getLogPath() string {
	logPath := viper.GetString("log-file")
	if logPath == stdout {
		return logPath
	} else {
		return resolvepath.ResolvePath(logPath)
	}

}

// FromViper returns GrpcServerConfig built from viper configuration.
func FromViper() grpcserver.GrpcServerConfig {
	serverPort := viper.GetInt("server-port")
	if err := ValidatePort("server", serverPort); err != nil {
		panic(err)
	}
	authPort := viper.GetInt("auth-port")
	if err := ValidatePort("auth", authPort); err != nil {
		panic(err)
	}
	httpPort := viper.GetInt("http-port")
	if httpPort != 0 {
		if err := ValidatePort("http", httpPort); err != nil {
			panic(err)
		}
	}
	dataDirectory := resolvepath.ResolvePath(viper.GetString("data-dir"))

	return grpcserver.GrpcServerConfig{
		Host:                      viper.GetString("server-hostname"),
		Port:                      serverPort,
		AuthPort:                  authPort,
		HttpPort:                  httpPort,
		HttpChunkBytes:            viper.GetInt("http-chunk-bytes"),
		ParallelJobs:              uint(viper.GetInt("jobs")), //nolint:gosec // jobs is expected to be a small positive value
		DataDirectory:             dataDirectory,
		LogPath:                   getLogPath(),
		LogLevel:                  viper.GetString("log-level"),
		SrcToolsDirectory:         viper.GetString("source-tools-dir"),
		DeploymentToolsDir:        viper.GetString("deployment-tools-dir"),
		IsRootWorkerEnabled:       viper.GetBool("enable-on-demand-privilege"),
		EnableFullCaptureSupport:  viper.GetBool("enable-full-capture-support"),
		EnableRerendering:         viper.GetBool("enable-rerendering"),
		EnableExperimentalRecipes: viper.GetBool("enable-experimental-recipes"),
		EnableSecondaryRunPaths:   viper.GetBool("enable-secondary-run-paths"),
		EnableTransferManager:     viper.GetBool("enable-transfer-manager"),
		EnableRenderDBSandbox:     viper.GetBool("enable-render-db-sandbox"),
		EnableNeoprofTimeline:     viper.GetBool("enable-neoprof-timeline"),
		ConfigDirectory:           DefaultConfigDir,
	}
}

// FromViperForBackground returns GrpcServerConfig suitable for running server in the background,
// that's been built from viper configuration.
//
// It ensures that server will log to a file and not stdout.
func FromViperForBackground() grpcserver.GrpcServerConfig {
	config := FromViper()
	if config.LogPath == stdout {
		logFilePath, err := logging.GetNewLogFilePath()
		if err == nil {
			config.LogPath = logFilePath
		}
	}
	return config
}
