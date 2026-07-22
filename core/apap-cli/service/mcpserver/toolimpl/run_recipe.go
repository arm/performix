// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// run_recipe.go implements the run_recipe MCP tool, which orchestrates a single Performix recipe
// run against a target: it validates the request, resolves the workload mode (launch, attach_to_pid,
// or system), prepares the target (auto-deploying mandatory tools when missing), validates any
// supplied recipe parameters, then issues the recipe (auto-deploying any recipe-specific tools as
// part of the run) and drains the run stream to completion. Operational problems are reported
// through the structured result (run_status, error) rather than as tool errors.

package toolimpl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Arm-Debug/apap-cli/apap-engine/grpcserver"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

type RunRecipeTool struct{}

const defaultRunTimeoutSeconds uint32 = 10

// Status values reported in runRecipeResult.Status.
const (
	runStatusCompleted = "completed"
	runStatusFailed    = "failed"
	runStatusError     = "error"
)

// Exactly one workload option should be non-nil.
type runRecipeInput struct {
	Recipe        string             `json:"recipe"`
	Target        string             `json:"target"`
	Timeout       *uint32            `json:"timeout,omitempty"`
	Parameters    map[string]any     `json:"parameters,omitempty"`
	Launch        *launchOpts        `json:"launch,omitempty"`
	AndroidLaunch *androidLaunchOpts `json:"android_launch,omitempty"`
	Attach        *attachOpts        `json:"attach_to_pid,omitempty"`
	System        *systemOpts        `json:"system,omitempty"`
}

// launchOpts launches a new workload on the target and profiles it.
type launchOpts struct {
	Command    string            `json:"command"`
	WorkingDir string            `json:"working_dir,omitempty"`
	UseShell   bool              `json:"use_shell,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
}

// androidLaunchOpts launches an Android package activity on the target.
type androidLaunchOpts struct {
	PackageName  string `json:"package_name"`
	ActivityName string `json:"activity_name"`
}

// attachOpts attaches to and profiles an already-running process on the target.
type attachOpts struct {
	PID int32 `json:"pid"`
}

// systemOpts profiles the whole system. It carries no options; its presence selects the mode.
type systemOpts struct{}

var runRecipeInputSchema = &jsonschema.Schema{
	Type:     "object",
	Required: []string{"recipe", "target"},
	// Exactly one workload mode must be supplied.
	OneOf: []*jsonschema.Schema{
		{Required: []string{"launch"}},
		{Required: []string{"android_launch"}},
		{Required: []string{"attach_to_pid"}},
		{Required: []string{"system"}},
	},
	Properties: map[string]*jsonschema.Schema{
		"recipe": {
			Type:        "string",
			Description: "The recipe to run. Use the list_recipes tool to discover currently available recipes.",
		},
		"target": {
			Type:        "string",
			Description: "Friendly name of the target to run on. Use the list_targets tool to discover available targets.",
		},
		"timeout": {
			Type:        "integer",
			Minimum:     jsonschema.Ptr(0.0),
			Description: fmt.Sprintf("Maximum number of seconds to profile for. Omitted uses a %d-second default. Set 0 for no timeout.", defaultRunTimeoutSeconds),
		},
		"parameters": {
			Type:                 "object",
			Description:          "Optional recipe-specific parameters that override the recipe's defaults, as name/value pairs (for example {\"collect_java_stacks\": true}). The available parameter names and value types depend on the chosen recipe.",
			AdditionalProperties: &jsonschema.Schema{},
		},
		"launch": {
			Type:        "object",
			Required:    []string{"command"},
			Description: "Launch a new workload on the target and profile it.",
			Properties: map[string]*jsonschema.Schema{
				"command": {
					Type:        "string",
					Description: "The command and any arguments required to launch the workload on the target.",
				},
				"working_dir": {
					Type:        "string",
					Description: "Working directory for the launched workload (defaults to the target user's home directory).",
				},
				"use_shell": {
					Type:        "boolean",
					Description: "Run the launched workload through the target's default shell.",
				},
				"env": {
					Type:                 "object",
					Description:          "Environment variables to expose to the launched workload.",
					AdditionalProperties: &jsonschema.Schema{Type: "string"},
				},
			},
		},
		"android_launch": {
			Type:        "object",
			Required:    []string{"package_name", "activity_name"},
			Description: "Launch an Android package activity on the target and profile it.",
			Properties: map[string]*jsonschema.Schema{
				"package_name": {
					Type:        "string",
					Description: "The Android application package to launch.",
				},
				"activity_name": {
					Type:        "string",
					Description: "The Android activity to launch.",
				},
			},
		},
		"attach_to_pid": {
			Type:        "object",
			Required:    []string{"pid"},
			Description: "Attach to and profile an already-running process on the target.",
			Properties: map[string]*jsonschema.Schema{
				"pid": {
					Type:        "integer",
					Minimum:     jsonschema.Ptr(1.0),
					Description: "Process ID of the already-running process to attach to and profile.",
				},
			},
		},
		"system": {
			Type:        "object",
			Description: "Profile the whole target system instead of a specific workload. Pass an empty object ({}).",
		},
	},
}

type runRecipeResult struct {
	RunID         string     `json:"run_id,omitempty"`
	Status        string     `json:"run_status"`
	PrepareResult string     `json:"target_status,omitempty"`
	Error         *toolError `json:"error,omitempty"`
}

var runRecipeOutputSchema = &jsonschema.Schema{
	Type:     "object",
	Required: []string{"run_status"},
	Properties: map[string]*jsonschema.Schema{
		"run_id": {
			Type:        "string",
			Description: "Identifier of the recipe run. Present once the run has started.",
		},
		"run_status": {
			Type: "string",
			Enum: []any{runStatusCompleted, runStatusFailed, runStatusError},
			Description: "Outcome of the tool: \"completed\" (run finished successfully), \"failed\" (run started but finished with an error), " +
				"or \"error\" (the run could not be started).",
		},
		"target_status": {
			Type:        "string",
			Description: "Result of preparing the target (for example DEPLOYED if tools were deployed, or NO_ACTION if it was already prepared).",
		},
		"error": toolErrorSchema(),
	},
}

func (RunRecipeTool) Register(server *mcp.Server, toolDeps ToolDependencies) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "run_recipe",
		Description: "Runs a " + terminology.GetProductFullName() + " recipe against a target: it prepares the target " +
			"(deploying mandatory tools if they are missing), then runs the recipe (deploying any recipe-specific tools as part of the run) and waits for completion. " +
			"If the recipe run succeeds, the result will contain the run_id. " +
			"Problems while attempting to prepare for and run a recipe are reported in the tool's output (run_status and error fields).",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint: false,
		},
		InputSchema:  runRecipeInputSchema,
		OutputSchema: runRecipeOutputSchema,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input runRecipeInput) (*mcp.CallToolResult, runRecipeResult, error) {
		workload, err := buildRecipeWorkload(input)
		if err != nil {
			return runRecipeReturn(runRecipeResult{Status: runStatusError, Error: newToolError(err)})
		}

		params, err := recipeParameters(input.Parameters)
		if err != nil {
			return runRecipeReturn(runRecipeResult{Status: runStatusError, Error: newToolError(err)})
		}

		if err := requireAvailableRecipe(ctx, toolDeps.Engine, input.Recipe); err != nil {
			return runRecipeReturn(runRecipeResult{Status: runStatusError, Error: newToolError(err)})
		}

		targetName := input.Target
		tgt, err := toolDeps.Targets.GetTarget(targetName)
		if err != nil {
			return runRecipeReturn(runRecipeResult{Status: runStatusError, Error: newToolError(err)})
		}
		protoTarget := grpcserver.TargetToProto(tgt)

		// Step 1: ensure the target is prepared. Auto-deploys mandatory tools only when they are
		// missing, so an already-prepared target is a no-op.
		prepareResp, err := toolDeps.Engine.TargetPrepare(ctx, &apapproto.TargetPrepareRequest{
			Target:         protoTarget,
			DeploymentType: apapproto.ToolDeploy_AUTO,
		})
		if err != nil {
			return runRecipeReturn(runRecipeResult{Status: runStatusError, Error: newToolError(err)})
		}
		switch prepareResp.GetResult() {
		case apapproto.TargetPrepareResult_DEPLOYED, apapproto.TargetPrepareResult_NO_ACTION:
			// Target is prepared (tools deployed now, or it was already prepared); proceed.
		case apapproto.TargetPrepareResult_DEPLOY:
			return runRecipeReturn(runRecipeResult{
				Status: runStatusError,
				Error:  newToolError(errors.New("target could not be prepared: mandatory tools still need to be deployed")),
			})
		default:
			return runRecipeReturn(runRecipeResult{
				Status: runStatusError,
				Error:  newToolError(fmt.Errorf("target preparation returned an unexpected result: %s", prepareResp.GetResult())),
			})
		}

		result := runRecipeResult{PrepareResult: prepareResp.GetResult().String()}

		// Step 2: validate any supplied recipe parameters up front, so the agent gets specific,
		// per-parameter errors before the recipe runs.
		if len(params) > 0 {
			validateResp, err := toolDeps.Engine.RecipeValidateParameters(ctx, &apapproto.RecipeValidateParametersRequest{
				RecipeName: input.Recipe,
				Parameters: params,
				Target:     protoTarget,
				TargetName: &targetName,
				Workload:   workload,
			})
			if err != nil {
				result.Status = runStatusError
				result.Error = newToolError(err)
				return runRecipeReturn(result)
			}
			if invalid := parameterValidationError(validateResp.GetMessages()); invalid != "" {
				result.Status = runStatusError
				result.Error = newToolError(errors.New(invalid))
				return runRecipeReturn(result)
			}
		}

		// Step 3: run the recipe and wait for it to finish. The run auto-deploys any recipe-specific
		// tools (ToolDeploy_AUTO) as part of its pipeline and is the authoritative gate: genuine
		// problems (an unsupported platform, a missing non-deployable dependency, or insufficient
		// privileges) surface as a run failure on the stream rather than via a separate readiness check.
		startCommand := &apapproto.RecipeStartCommand{
			Target:         protoTarget,
			TargetName:     &targetName,
			Name:           input.Recipe,
			DeploymentType: apapproto.ToolDeploy_AUTO,
			Workload:       workload,
			Parameters:     params,
			Timeout:        recipeTimeout(input),
		}

		stream, err := toolDeps.Engine.RecipeIssueCommand(ctx, &apapproto.RecipeCommand{
			SpecificCommand: &apapproto.RecipeCommand_StartCommand{StartCommand: startCommand},
		})
		if err != nil {
			result.Status = runStatusError
			result.Error = newToolError(err)
			return runRecipeReturn(result)
		}

		drainRecipeRun(stream, &result)
		return runRecipeReturn(result)
	})
}

func requireAvailableRecipe(ctx context.Context, engine apapproto.ApapClient, recipeName string) error {
	listing, err := engine.ListRecipes(ctx, &emptypb.Empty{})
	if err != nil {
		return err
	}
	for _, recipe := range listing.GetRecipeNames() {
		if recipe.GetName() == recipeName {
			return nil
		}
	}
	return message.New(message.EngineRecipeDoesNotExist).WithMetadata(map[string]string{"recipe": recipeName})
}

func recipeTimeout(input runRecipeInput) *uint32 {
	if input.Timeout != nil {
		return input.Timeout
	}
	timeout := defaultRunTimeoutSeconds
	return &timeout
}

// runRecipeReturn builds the MCP return tuple for a finished run. Any outcome other than a
// completed run sets IsError on the CallToolResult, so clients that only inspect content/IsError
// still register the failure, while the structured result is always returned for clients that
// reason over run_status and the structured error.
func runRecipeReturn(result runRecipeResult) (*mcp.CallToolResult, runRecipeResult, error) {
	if result.Status == runStatusCompleted {
		return nil, result, nil
	}
	return &mcp.CallToolResult{IsError: true}, result, nil
}

// buildRecipeWorkload converts the selected workload mode into the proto RecipeWorkload. The input
// schema's oneOf guarantees exactly one mode is set; the default case guards against direct callers.
func buildRecipeWorkload(input runRecipeInput) (*apapproto.RecipeWorkload, error) {
	switch {
	case input.Launch != nil:
		return &apapproto.RecipeWorkload{
			SpecificWorkload: &apapproto.RecipeWorkload_LaunchWorkload{
				LaunchWorkload: &apapproto.LaunchWorkload{
					Command:     input.Launch.Command,
					Environment: input.Launch.Env,
					WorkingDir:  input.Launch.WorkingDir,
					UseShell:    input.Launch.UseShell,
				},
			},
		}, nil
	case input.AndroidLaunch != nil:
		return &apapproto.RecipeWorkload{
			SpecificWorkload: &apapproto.RecipeWorkload_AndroidLaunchWorkload{
				AndroidLaunchWorkload: &apapproto.AndroidLaunchWorkload{
					PackageName:  input.AndroidLaunch.PackageName,
					ActivityName: input.AndroidLaunch.ActivityName,
				},
			},
		}, nil
	case input.Attach != nil:
		return &apapproto.RecipeWorkload{
			SpecificWorkload: &apapproto.RecipeWorkload_AttachWorkload{
				AttachWorkload: &apapproto.AttachWorkload{Pid: &input.Attach.PID},
			},
		}, nil
	case input.System != nil:
		return &apapproto.RecipeWorkload{
			SpecificWorkload: &apapproto.RecipeWorkload_SystemWideWorkload{
				SystemWideWorkload: &apapproto.SystemWideWorkload{},
			},
		}, nil
	default:
		return nil, errors.New(`provide exactly one workload mode: "launch", "attach_to_pid", or "system"`)
	}
}

// recipeParameters converts the JSON parameter map into the proto structpb representation the engine
// expects. JSON already carries value types (bool, number, string, array), so each entry maps
// directly via structpb.NewValue. Returns nil when no parameters were supplied.
func recipeParameters(in map[string]any) (map[string]*structpb.Value, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make(map[string]*structpb.Value, len(in))
	for name, value := range in {
		val, err := structpb.NewValue(value)
		if err != nil {
			return nil, fmt.Errorf("invalid value for recipe parameter %q: %w", name, err)
		}
		out[name] = val
	}
	return out, nil
}

// drainRecipeRun consumes the recipe run stream to completion, recording the run ID and final
// status on result. It deliberately does not render progress; the MCP tool only reports the
// final outcome.
func drainRecipeRun(stream apapproto.Apap_RecipeIssueCommandClient, result *runRecipeResult) {
	for {
		response, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			result.Status = runStatusError
			result.Error = newToolError(err)
			return
		}

		if id := response.GetId(); id != nil && id.GetValue() != "" {
			result.RunID = id.GetValue()
		}

		finish, ok := response.GetSubMessage().(*apapproto.RecipeResponse_RecipeFinish)
		if !ok {
			continue
		}
		if finish.RecipeFinish.GetReturnCode() != apapproto.StatusCode_SUCCESS {
			result.Status = runStatusFailed
			if runErr := message.ReconstructFromChain(finish.RecipeFinish.GetError()); runErr != nil {
				result.Error = newToolError(runErr)
			} else {
				result.Error = newToolError(errors.New("recipe run failed"))
			}
			return
		}
		result.Status = runStatusCompleted
		return
	}

	// The stream ended without an explicit RecipeFinish. A completed run is only reported when a
	// successful finish message is observed, so reaching EOF here (a truncated stream or an
	// engine-side exit before finishing) is surfaced as an error rather than an unverified success.
	if result.Status == "" {
		result.Status = runStatusError
		if result.RunID != "" {
			result.Error = newToolError(errors.New("recipe run started but the engine stream ended before reporting a result"))
		} else {
			result.Error = newToolError(errors.New("recipe run ended without a result"))
		}
	}
}

// chainMessage renders an ErrorChain into human-readable text, preferring the message catalog entry
// when one matches. It returns an empty string for a nil or empty chain.
func chainMessage(chain *apapproto.ErrorChain) string {
	reconstructed := message.ReconstructFromChain(chain)
	if reconstructed == nil {
		return ""
	}
	if catalogMsg, lookupErr := message.LookupMessage(reconstructed); lookupErr == nil {
		return catalogMsg.Text()
	}
	return reconstructed.Error()
}

// parameterValidationError summarizes recipe parameter validation failures into a single message,
// keyed by parameter. It returns an empty string when there are no validation messages.
func parameterValidationError(messages []*apapproto.ParameterValidationResult) string {
	parts := make([]string, 0, len(messages))
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		text := chainMessage(msg.GetMessage())
		if text == "" {
			text = "invalid parameter value"
		}
		parts = append(parts, fmt.Sprintf("%q: %s", msg.GetParameterId(), text))
	}
	if len(parts) == 0 {
		return ""
	}
	return "recipe parameter validation failed: " + strings.Join(parts, "; ")
}
