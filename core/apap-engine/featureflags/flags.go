// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// Package featureflags defines the CLI and engine feature switches, independently of
// configuration loading and the services that consume them.
package featureflags

// Flags contains the feature settings used by the CLI and engine.
// JSON tags preserve their configuration keys in support-package diagnostics.
type Flags struct {
	// AndroidTargets controls CLI Android support; it is not used by the engine.
	AndroidTargets bool `json:"enable-android-targets"`

	FullCaptureSupport       bool `json:"enable-full-capture-support"`
	Rerendering              bool `json:"enable-rerendering"`
	ExperimentalRecipes      bool `json:"enable-experimental-recipes"`
	SecondaryRunPaths        bool `json:"enable-secondary-run-paths"`
	GPURecipe                bool `json:"enable-gpu-recipe"`
	RenderDBSandbox          bool `json:"enable-render-db-sandbox"`
	NeoprofTimeline          bool `json:"enable-neoprof-timeline"`
	NeoprofSystemUtilization bool `json:"enable-neoprof-system-utilization"`
	JfrCapture               bool `json:"enable-jfr-capture"`
	RunComponentCompression  bool `json:"enable-run-component-compression"`
}

// Resolve applies dependencies between configured feature settings and returns
// the values used by the engine.
func Resolve(configured Flags) Flags {
	configured.FullCaptureSupport = configured.FullCaptureSupport || configured.Rerendering
	return configured
}
