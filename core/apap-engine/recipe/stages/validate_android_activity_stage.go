// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package stages

import (
	"fmt"
	"strings"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
	"github.com/Arm-Debug/apap-cli/api/targetagentproto"
)

// ValidateAndroidActivityStage checks an explicit Android component without launching it.
type ValidateAndroidActivityStage struct {
	agentSupplier recipe.AgentConnSupplier
	workload      *tool.Workload
}

func NewValidateAndroidActivityStage(agentSupplier recipe.AgentConnSupplier, workload *tool.Workload) *ValidateAndroidActivityStage {
	return &ValidateAndroidActivityStage{agentSupplier: agentSupplier, workload: workload}
}

func (s *ValidateAndroidActivityStage) Name() string { return "Checking Android activity" }
func (s *ValidateAndroidActivityStage) ErrorType() run.RunResult {
	return run.RecipeFailureWorkloadOptions
}
func (s *ValidateAndroidActivityStage) AlwaysExecute() bool { return false }

func (s *ValidateAndroidActivityStage) Execute(ctx *recipe.StageContext) (func(), error) {
	w := (*s.workload).(*tool.WorkloadAndroidLaunch)
	packageName := w.PackageName
	activityName := w.ActivityName
	metadata := map[string]string{"package": packageName, "activity": activityName}
	result, err := s.agentSupplier().Client.ExecCommand(ctx.Context, &targetagentproto.ExecCommandRequest{
		Command: []string{"cmd", "package", "resolve-activity", "--brief", "-n", packageName + "/" + activityName},
	})
	var failure *message.MessageImpl
	if err != nil {
		failure = message.New(message.EngineRecipeStagesValidateAndroidActivityStageCheckFailed).WithMetadata(metadata).WithCause(err)
	} else if result.Rc != 0 {
		failure = message.New(message.EngineRecipeStagesValidateAndroidActivityStageCheckFailed).WithMetadata(metadata).
			WithCause(fmt.Errorf("activity resolution exited with code %d: %s", result.Rc, result.Stderr))
	} else if strings.TrimSpace(result.Stdout) == "No activity found" {
		failure = message.New(message.EngineRecipeStagesValidateAndroidActivityStageActivityUnresolved).WithMetadata(metadata)
	} else if !resolvedAndroidActivityMatches(result.Stdout, packageName, activityName) {
		failure = message.New(message.EngineRecipeStagesValidateAndroidActivityStageCheckFailed).WithMetadata(metadata).
			WithCause(fmt.Errorf("unexpected activity resolution output: %q", result.Stdout))
	}
	if failure != nil {
		ctx.ReadinessNotifier.OnReadinessProbed(recipe.ConvertMessageToReadyOutput(failure))
	}
	return nil, nil
}

func resolvedAndroidActivityMatches(output, packageName, activityName string) bool {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) > 2 || (len(lines) == 2 && !strings.HasPrefix(strings.TrimSpace(lines[0]), "priority=")) {
		return false
	}
	resolvedPackage, resolvedActivity, found := strings.Cut(strings.TrimSpace(lines[len(lines)-1]), "/")
	if !found || resolvedPackage != packageName || resolvedActivity == "" {
		return false
	}
	expand := func(name string) string {
		if strings.HasPrefix(name, ".") {
			return packageName + name
		}
		return name
	}
	return expand(resolvedActivity) == expand(activityName)
}
