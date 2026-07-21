// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package runtime

import (
	"github.com/spf13/afero"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/notifiers"
	"github.com/Arm-Debug/apap-cli/apap-engine/packages"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/targetsession"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool/deployer"
)

// StageConfiguration contains information required for configuring the execution stages
type StageConfiguration struct {
	Recipe                 *recipe.Recipe
	Ctx                    *recipe.RecipeCtx
	RunCollection          *run.RunCollection
	ToolBasePaths          deployer.BaseToolDeploymentPaths
	ToolDeploymentType     deployer.ToolDeploymentMode
	CollectionState        *recipe.CollectionState
	TargetSessions         targetsession.TargetSessionProvider
	OperationName          string
	IsRootWorkerEnabled    bool
	IsFullCaptureEnabled   bool
	RerenderingEnabled     bool
	TransferManagerEnabled bool
	NeoprofTimelineEnabled bool
	PackageManager         *packages.PackageManager
	DeferredActions        notifiers.DeferredActions
	UsrMessageWriter       *run.ConcreteUserMessageWriter
	RunModels              []cdf.ModelView
}

func (c *StageConfiguration) ConfigureRunManifestUpdater() {
	runWriter := &run.ConcreteRunWriter{RunCollection: c.RunCollection}
	c.CollectionState.RunManifestUpdater = run.NewRunManifestUpdater(&c.CollectionState.RunBuilder, runWriter)
}

func (c *StageConfiguration) NewRunExecutionContext(hostFs afero.Fs) *recipe.RunExecutionContext {
	return &recipe.RunExecutionContext{
		RecipeCtx:              c.Ctx,
		FileHandler:            conductor.FileHandler{HostFS: hostFs},
		PackageManager:         c.PackageManager,
		DeferredActions:        &c.DeferredActions,
		TargetSessions:         c.TargetSessions,
		RootWorkerEnabled:      c.IsRootWorkerEnabled,
		FullCaptureSupport:     c.IsFullCaptureEnabled,
		RerenderingEnabled:     c.RerenderingEnabled,
		NeoprofTimelineEnabled: c.NeoprofTimelineEnabled,
		UsrMessageWriter:       c.UsrMessageWriter,
		RunModels:              c.RunModels,
	}
}

// StageFactory constructs stages for use with a runtime method
type StageFactory interface {
	BuildStages(cfg *StageConfiguration, notifier notifiers.StageNotifier) ([]recipe.Stage, recipe.ExecutionContext)
}
