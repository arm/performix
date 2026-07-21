// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipe

import (
	"fmt"
	"sync"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/parameters"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
	"github.com/Arm-Debug/apap-cli/atperf-version/versions"
)

// RecipeMetadata contains metadata about the recipe itself
// such as name, version and API version
type RecipeMetadata struct {
	Name       string
	Version    string
	APIVersion string
}

// RecipeCtx contains configuration data about the recipe invocation
type RecipeCtx struct {
	RecipePath           string
	OutputDir            string
	NoCleanupWorkingArea bool
	OrigWorkload         tool.Workload // Stores the original workload provided by the user
	ResolvedWorkload     tool.Workload // The updated workload as set by the WorkloadOptionsStage, if executed
	Target               target.Target
	TargetName           string
	ParamValues          parameters.BoundParameters
	RenderParamValues    map[string]any
	Timeout              uint32
	ToolInvocationCounts map[string]int
	toolCountLock        sync.Mutex
	SourceCodePaths      run.HostSourceCodePath
	RecipeMetadata       RecipeMetadata
	ToolVersions         map[string]string
}

// GetAndIncrementToolInvocationCount is used to keep track of how many times a particular tool has been invoked.
// There are no preconditions. Return value is the value prior to increment - so the first call will return 0.
func (r *RecipeCtx) GetAndIncrementToolInvocationCount(toolName string) int {
	r.toolCountLock.Lock()
	defer r.toolCountLock.Unlock()

	if r.ToolInvocationCounts == nil {
		r.ToolInvocationCounts = make(map[string]int)
	}
	oldCount := r.ToolInvocationCounts[toolName]
	count := oldCount + 1
	r.ToolInvocationCounts[toolName] = count
	return oldCount
}

// GetNextOutputEntity returns an entity with a relative tool path. For each invocation,
// we get a new path, i.e: /tool/mytool/0 , /tool/mytool/1
func (r *RecipeCtx) GetNextOutputEntity(toolName string) cdf.Entity {
	count := r.GetAndIncrementToolInvocationCount(toolName)
	return cdf.Entity{RelativePath: fmt.Sprintf("tool/%s/%d", toolName, count)}
}

// CreateMetadata is a utility function to create initial metadata based on the
// run ID and recipe context.
func (r *RecipeCtx) CreateMetadata(runBuilder *run.RunBuilder, boundParams parameters.BoundParameters) (*cdf.Metadata, error) {

	tgt, err := target.JSONTargetFromEngine(r.Target)
	if err != nil {
		return nil, err
	}

	metadata := cdf.Metadata{
		EngineVersion: versions.GetVersion(),
		Name:          runBuilder.GenerateRunName(r.RecipeMetadata.Name),
		StartTime:     util.CurrentTime(),
		EndTime:       util.InvalidTime(), // Filled in later after recipe run
		RecipeName:    r.RecipeMetadata.Name,
		Timeout:       r.Timeout,
		Parameters:    boundParams.CollapseToMap(),
		RunResult:     string(run.RecipeInProgress),
		TargetName:    r.TargetName,
		TargetConfig:  tgt,
	}

	// Capture in the metadata the workload details
	switch w := r.OrigWorkload.(type) {
	case *tool.WorkloadLaunch:
		metadata.WorkloadType = "Launch"
		metadata.Cmdline = w.RawCommand
		metadata.Env = w.Environment
		// Note that working dir is not populated here, since it is sometimes overridden later as the user's home dir
		metadata.WorkingDir = ""
		metadata.UseShell = w.UseShell
		metadata.Pid = -2
	case *tool.WorkloadAndroidLaunch:
		metadata.WorkloadType = "Android Launch"
		metadata.AndroidPackageName = w.PackageName
		metadata.AndroidActivityName = w.ActivityName
		metadata.Cmdline = ""
		metadata.Env = nil
		metadata.WorkingDir = ""
		metadata.UseShell = false
		metadata.Pid = int64(-2)
	case *tool.WorkloadAttach:
		metadata.WorkloadType = "Attach"
		metadata.Cmdline = ""
		metadata.Env = nil
		metadata.WorkingDir = ""
		metadata.UseShell = false
		metadata.Pid = int64(w.PID)
	case *tool.WorkloadSystemWide:
		metadata.WorkloadType = "System Wide"
		metadata.Cmdline = ""
		metadata.Env = nil
		metadata.WorkingDir = ""
		metadata.UseShell = false
		metadata.Pid = int64(-1)
	}

	// Add this metadata to the manifest
	_ = runBuilder.AddComponent(cdf.ComponentType{Name: cdf.TypeMetadata, SchemaVersion: "1.0.0"}, "metadata.json")

	return &metadata, nil
}
