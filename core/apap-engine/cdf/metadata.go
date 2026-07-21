// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package cdf

import (
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

// Metadata represents useful metadata stored for a Run.
type Metadata struct {
	EngineVersion       string              `json:"engine.version"`
	Name                string              `json:"run.name"`
	StartTime           util.UTCRFC3339Time `json:"run.start_time"`
	EndTime             util.UTCRFC3339Time `json:"run.end_time"`
	RecipeName          string              `json:"run.recipe_name"`
	Parameters          map[string]any      `json:"run.parameters"`
	WorkloadType        string              `json:"run.workload.type"`
	Cmdline             string              `json:"run.workload.cmdline"`
	WorkingDir          string              `json:"run.workload.working_dir"`
	Env                 map[string]string   `json:"run.workload.env"`
	UseShell            bool                `json:"run.workload.use_shell"`
	AndroidPackageName  string              `json:"run.workload.android.package_name,omitempty"`
	AndroidActivityName string              `json:"run.workload.android.activity_name,omitempty"`
	Pid                 int64               `json:"run.workload.pid"`
	Timeout             uint32              `json:"run.timeout"`
	RunResult           string              `json:"run.result"`
	RunError            string              `json:"run.error"`
	TargetName          string              `json:"target.name"`
	TargetConfig        target.JSONTarget   `json:"target.config"`
}
