// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	log "github.com/sirupsen/logrus"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Arm-Debug/apap-cli/apap-engine/deploymentsupport"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/parameters"
	"github.com/Arm-Debug/apap-cli/apap-engine/query"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipeparser"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	run "github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/ssh"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool/deployer"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

func SSHConnectErrorToProto(err error) *apapproto.TargetTestConnection {
	if err != nil {
		return &apapproto.TargetTestConnection{
			Status: apapproto.ConnectionStatus_CONNECTION_STATUS_ERROR,
			Error:  message.BuildErrorChain(err),
		}
	}
	return &apapproto.TargetTestConnection{
		Status: apapproto.ConnectionStatus_CONNECTION_STATUS_OK,
		Error:  nil,
	}
}

func ToProto(rr deployer.ReconcileResult) apapproto.TargetPrepareResult {
	switch rr {
	case deployer.Deployed:
		return apapproto.TargetPrepareResult_DEPLOYED
	case deployer.Deploy:
		return apapproto.TargetPrepareResult_DEPLOY
	default:
		return apapproto.TargetPrepareResult_NO_ACTION
	}
}

func hostKeyBehaviourToPolicy(hostBehaviour *apapproto.SSHHostKeyPolicy) target.HostKeyPolicy {
	switch *hostBehaviour {
	case apapproto.SSHHostKeyPolicy_SSH_HOST_KEY_POLICY_IGNORE_IF_MISSING:
		return target.IgnoreHostKey
	case apapproto.SSHHostKeyPolicy_SSH_HOST_KEY_POLICY_ADD_IF_MISSING:
		return target.AcceptNewHost
	case apapproto.SSHHostKeyPolicy_SSH_HOST_KEY_POLICY_ASK_IF_MISSING:
		return target.AskNewHost
	default:
		return target.RejectHostKeyIfMissing
	}
}

func policyToHostKeyBehaviour(policy target.HostKeyPolicy) apapproto.SSHHostKeyPolicy {
	switch policy {
	case target.IgnoreHostKey:
		return apapproto.SSHHostKeyPolicy_SSH_HOST_KEY_POLICY_IGNORE_IF_MISSING
	case target.AcceptNewHost:
		return apapproto.SSHHostKeyPolicy_SSH_HOST_KEY_POLICY_ADD_IF_MISSING
	case target.AskNewHost:
		return apapproto.SSHHostKeyPolicy_SSH_HOST_KEY_POLICY_ASK_IF_MISSING
	default:
		return apapproto.SSHHostKeyPolicy_SSH_HOST_KEY_POLICY_REJECT_IF_MISSING
	}
}

// authMethodFromProto converts apapproto.SSHAuthMethod protobuf to target.SSHAuthMethod
func authMethodFromProto(authMethod *apapproto.SSHAuthMethod) target.SSHAuthMethod {
	// Default to key-based authentication if authMethod is not provided
	if authMethod == nil {
		return target.SSHAuthMethodKey
	}
	switch *authMethod {
	case apapproto.SSHAuthMethod_SSH_AUTH_METHOD_PASSWORD:
		return target.SSHAuthMethodPassword
	default:
		return target.SSHAuthMethodKey
	}
}

// authMethodToProto converts target.SSHAuthMethod to apapproto.SSHAuthMethod protobuf
func authMethodToProto(method target.SSHAuthMethod) apapproto.SSHAuthMethod {
	switch method {
	case target.SSHAuthMethodPassword:
		return apapproto.SSHAuthMethod_SSH_AUTH_METHOD_PASSWORD
	default:
		return apapproto.SSHAuthMethod_SSH_AUTH_METHOD_KEY
	}
}

func TargetFromProto(in *apapproto.Target) (target.Target, error) {
	if in == nil {
		stringErr := errors.New("missing target protobuf")
		return nil, message.New(message.CommonUnknownError).WithCause(stringErr)
	}

	switch conn := in.Connection.(type) {
	case *apapproto.Target_SshConfig:
		sshConfig := conn.SshConfig
		metadata := map[string]string{
			"targetString": in.String(),
		}

		if sshConfig == nil {
			stringErr := errors.New("no ssh config found")
			return nil, message.New(message.EngineGrpcserverConversionSshConfigurationInvalid).WithCause(stringErr).WithMetadata(metadata)
		}
		if len(sshConfig.Hosts) == 0 {
			stringErr := errors.New("no hosts defined")
			return nil, message.New(message.EngineGrpcserverConversionSshConfigurationInvalid).WithCause(stringErr).WithMetadata(metadata)
		}

		jumps := make([]target.SSHHostConfig, len(sshConfig.Hosts))
		for i, host := range sshConfig.Hosts {
			jumps[i] = target.SSHHostConfig{
				Host:               host.Host,
				Port:               host.Port,
				Username:           host.Username,
				PrivateKeyFilename: host.PrivateKeyFilename,
				AuthMethod:         authMethodFromProto(host.AuthMethod),
				HostKeyPolicy:      hostKeyBehaviourToPolicy(sshConfig.HostKeyPolicy),
			}
		}

		return &target.SSHTarget{Jumps: jumps}, nil

	case *apapproto.Target_LocalConfig:
		return &target.LocalTarget{}, nil

	case *apapproto.Target_AndroidConfig:
		androidConfig := conn.AndroidConfig
		if androidConfig == nil {
			stringErr := errors.New("no android config found")
			return nil, message.New(message.CommonUnknownError).WithCause(stringErr)
		}
		return &target.AndroidTarget{
			SerialNumber:    androidConfig.SerialNumber,
			DeviceIPAddress: androidConfig.DeviceIpAddress,
		}, nil

	default:
		stringErr := fmt.Errorf("unsupported target connection type: %T", in.Connection)
		return nil, message.New(message.CommonUnknownError).WithCause(stringErr)
	}
}

func TargetToProto(in target.Target) *apapproto.Target {
	switch t := in.(type) {
	case *target.SSHTarget:
		hostKeyPolicy := apapproto.SSHHostKeyPolicy_SSH_HOST_KEY_POLICY_REJECT_IF_MISSING
		hosts := make([]*apapproto.SSHHostConfig, len(t.Jumps))
		for i, jump := range t.Jumps {
			authMethod := authMethodToProto(jump.AuthMethod)
			hosts[i] = &apapproto.SSHHostConfig{
				Host:               jump.Host,
				Port:               int32(jump.Port),
				Username:           jump.Username,
				PrivateKeyFilename: jump.PrivateKeyFilename,
				AuthMethod:         &authMethod,
			}
			if i == 0 {
				hostKeyPolicy = policyToHostKeyBehaviour(jump.HostKeyPolicy)
			}
		}

		// Build the SSHConnectionConfig.
		sshConfig := &apapproto.SSHConnectionConfig{
			Hosts:         hosts,
			HostKeyPolicy: &hostKeyPolicy,
		}
		return &apapproto.Target{
			Connection: &apapproto.Target_SshConfig{SshConfig: sshConfig},
		}

	case *target.LocalTarget:
		localConfig := &apapproto.LocalConnectionConfig{}
		return &apapproto.Target{
			Connection: &apapproto.Target_LocalConfig{LocalConfig: localConfig},
		}

	case *target.AndroidTarget:
		androidConfig := &apapproto.AndroidConnectionConfig{
			SerialNumber:    t.SerialNumber,
			DeviceIpAddress: t.DeviceIPAddress,
		}
		return &apapproto.Target{
			Connection: &apapproto.Target_AndroidConfig{AndroidConfig: androidConfig},
		}

	default:
		return nil
	}
}

// NewList constructs a ListValue from a general-purpose Go slice.
// The slice elements are converted using NewValue.
func StringSliceToProto(v []string) *structpb.Value {
	return &structpb.Value{Kind: &structpb.Value_ListValue{
		ListValue: &structpb.ListValue{Values: util.Map(v, func(s string) *structpb.Value {
			return structpb.NewStringValue(s)
		})},
	}}
}

// AnyToProto converts a general-purpose Go type to a structpb.Value, excluding maps and structures.
// Slices support string and float64 values.
func AnyToProto(in any) (*structpb.Value, error) {
	if in == nil {
		return structpb.NewNullValue(), nil
	}
	switch v := in.(type) {
	case string:
		return structpb.NewStringValue(v), nil
	case []string:
		return StringSliceToProto(v), nil
	case int, int32, int64:
		return structpb.NewNumberValue(float64(v.(int64))), nil
	case float64, float32:
		return structpb.NewNumberValue(v.(float64)), nil
	case bool:
		return structpb.NewBoolValue(v), nil
	case []any:
		res := make([]*structpb.Value, len(v))
		for i, anyVal := range v {
			switch item := anyVal.(type) {
			case string:
				res[i] = structpb.NewStringValue(item)
			case float64:
				res[i] = structpb.NewNumberValue(item)
			default:
				stringErr := fmt.Errorf("unsupported list item type at %d, expected string or number, got %T", i, anyVal)
				return nil, stringErr
			}
		}
		return structpb.NewListValue(&structpb.ListValue{Values: res}), nil
	default:
		stringErr := fmt.Errorf("unsupported type %T for conversion to proto", in)
		return nil, stringErr
	}
}

// ProtoToAny converts a structpb.Value to a general-purpose Go type.
// Structs and maps are not supported.
func ProtoToAny(in *structpb.Value) (any, error) {
	if in == nil {
		return nil, nil
	}

	switch v := in.Kind.(type) {
	case *structpb.Value_NullValue:
		return nil, nil
	case *structpb.Value_NumberValue:
		return v.NumberValue, nil
	case *structpb.Value_StringValue:
		return v.StringValue, nil
	case *structpb.Value_BoolValue:
		return v.BoolValue, nil
	case *structpb.Value_ListValue:
		list := make([]any, len(v.ListValue.Values))
		for i, item := range v.ListValue.Values {
			var err error
			list[i], err = ProtoToAny(item)
			if err != nil {
				return nil, fmt.Errorf("unsupported list item type at %d: %w", i, err)
			}
		}
		return list, nil
	}
	return nil, fmt.Errorf("unsupported structpb.Value kind: %T", in.Kind)
}

// DescToProto converts a run.RunDescription to an apapproto.RunDescription
// The returned structure includes:
//
//	Metadata: Populated by copying fields from the input `in`
//	Extra: Always set to nil
func DescToProto(in *run.RunDescription) (*apapproto.RunDescription, error) {

	metaParams := map[string]*structpb.Value{}
	var err error
	for k, v := range in.Parameters {
		metaParams[k], err = AnyToProto(v)
		if err != nil {
			stringErr := fmt.Errorf("failed to convert run description to proto: %w", err)
			return nil, message.New(message.CommonUnknownError).WithCause(stringErr)
		}
	}

	meta := &apapproto.RunMetadata{
		Name:          in.Name,
		EngineVersion: in.EngineVersion,
		StartTime:     in.StartTime,
		EndTime:       in.EndTime,
		RecipeName:    in.RecipeName,
		Parameters:    metaParams,
		WorkloadType:  in.WorkloadType,
		Cmdline:       in.Cmdline,
		WorkingDir:    in.WorkingDir,
		Env:           in.Env,
		UseShell:      in.UseShell,
		Group:         in.Group,
		Tags:          in.Tags,
		Pid:           in.Pid,
		Target:        TargetToProto(in.Target),
		TargetName:    in.TargetName,
		Timeout:       in.Timeout,
		RunResult:     in.RunResult,
		RunError:      in.RunError,
	}
	if in.WorkloadType == "Android Launch" {
		meta.AndroidLaunchWorkload = &apapproto.AndroidLaunchWorkload{
			PackageName:  in.AndroidPackageName,
			ActivityName: in.AndroidActivityName,
		}
	}

	return &apapproto.RunDescription{
		Metadata: meta,
		Extra:    nil,
	}, nil
}

func SourceToProto(in *run.HostSourceCodePath) *apapproto.HostSourceCodePaths {
	if in == nil {
		return &apapproto.HostSourceCodePaths{Paths: []string{}}
	}
	return &apapproto.HostSourceCodePaths{Paths: in.Paths}
}

// Helper to convert map[string]interface{} to map[string]string
func toStringMap(m map[string]interface{}) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		if str, ok := v.(string); ok {
			out[k] = str
		} else {
			out[k] = fmt.Sprintf("%v", v)
		}
	}
	return out
}

// RecipeWorkloadFromProto converts a protobuf RecipeWorkload to a tool.Workload.
func RecipeWorkloadFromProto(in *apapproto.RecipeWorkload) (tool.Workload, error) {
	if in == nil {
		return nil, nil
	}

	switch workloadType := in.SpecificWorkload.(type) {
	case *apapproto.RecipeWorkload_AttachWorkload:
		return &tool.WorkloadAttach{PID: *workloadType.AttachWorkload.Pid}, nil
	case *apapproto.RecipeWorkload_SystemWideWorkload:
		return &tool.WorkloadSystemWide{}, nil
	case *apapproto.RecipeWorkload_LaunchWorkload:
		return &tool.WorkloadLaunch{
			RawCommand:  workloadType.LaunchWorkload.Command,
			Command:     util.WorkloadStringToSlice(workloadType.LaunchWorkload.Command),
			Environment: workloadType.LaunchWorkload.Environment,
			WorkingDir:  workloadType.LaunchWorkload.WorkingDir,
			UseShell:    workloadType.LaunchWorkload.UseShell,
		}, nil
	case *apapproto.RecipeWorkload_AndroidLaunchWorkload:
		return &tool.WorkloadAndroidLaunch{
			PackageName:  workloadType.AndroidLaunchWorkload.PackageName,
			ActivityName: workloadType.AndroidLaunchWorkload.ActivityName,
		}, nil
	}

	return nil, message.New(message.EngineRecipeWorkloadTypeUnknown)
}

func validateRecipeWorkload(workload tool.Workload, recipeName string) error {
	switch workload := workload.(type) {
	case *tool.WorkloadLaunch:
		if len(workload.Command) == 0 {
			return message.New(message.EngineRecipeWorkloadCommandEmpty).
				WithMetadata(map[string]string{"recipe": recipeName})
		}
	case *tool.WorkloadAndroidLaunch:
		if strings.TrimSpace(workload.PackageName) == "" || strings.TrimSpace(workload.ActivityName) == "" {
			return message.New(message.EngineRecipeWorkloadCommandEmpty).
				WithMetadata(map[string]string{"recipe": recipeName})
		}
	}

	return nil
}

func RecipeCtxFromProto(in *apapproto.RecipeStartCommand) (*recipe.RecipeCtx, error) {
	// Here we process the recipe information along with the workload, output and parameters.
	wl, err := RecipeWorkloadFromProto(in.Workload)
	if err != nil {
		return nil, err
	}

	if err := validateRecipeWorkload(wl, in.Name); err != nil {
		return nil, err
	}

	recipeCtx := &recipe.RecipeCtx{OrigWorkload: wl}

	tgt, err := TargetFromProto(in.Target)
	if err != nil {
		return nil, message.New(message.CommonUnknownError).WithCause(err)
	}

	recipeCtx.RecipePath, err = recipeparser.GetRecipePath(in.Name)
	if err != nil {
		return nil, err
	}

	recipeCtx.Target = tgt
	recipeCtx.TargetName = in.GetTargetName()

	// Pack data into a RecipeCtx struct to be used for later stages
	recipeCtx.OutputDir = filepath.ToSlash(filepath.ToSlash(in.OutputDirectory))
	recipeCtx.Timeout = in.GetTimeout()

	if in.HostSourceCodePaths != nil {
		recipeCtx.SourceCodePaths.Paths = in.HostSourceCodePaths.Paths
	}

	recipeCtx.NoCleanupWorkingArea = in.GetNoCleanupWorkingArea()

	recipeCtx.RecipeMetadata = recipe.RecipeMetadata{
		Name: in.Name,
	}

	return recipeCtx, nil
}

// ProtoTargetFromJSON converts from the configuration JSON type to the protobuf type
func ProtoTargetFromJSON(t target.JSONTarget) (*apapproto.Target, error) {
	ngin, err := target.EngineTargetFromJSON(t)
	if err != nil {
		return nil, err
	}

	return TargetToProto(ngin), nil
}

// JSONTargetFromProto converts from the protobuf type to the configuration JSON type
func JSONTargetFromProto(t *apapproto.Target) (target.JSONTarget, error) {
	ngin, err := TargetFromProto(t)
	if err != nil {
		return target.JSONTarget{}, err
	}

	return target.JSONTargetFromEngine(ngin)
}

func ParameterToProto(r *parameters.Parameter) *apapproto.RecipeParameterBase {
	return &apapproto.RecipeParameterBase{
		ID:          r.ID,
		Label:       r.Label,
		Description: r.Description,
		Required:    r.Required,
		VisibleWhen: util.Map(r.VisibleWhen, func(dep parameters.ParameterDependency) *apapproto.ParameterDependency {
			return &apapproto.ParameterDependency{
				ParameterId: dep.ParameterID,
				Value:       dep.Value,
			}
		}),
	}
}

func parameterOptionItemsToProto(items []parameters.ParameterOption) []*apapproto.ParameterOption {
	if len(items) == 0 {
		return nil
	}
	out := make([]*apapproto.ParameterOption, len(items))
	for i, item := range items {
		proto := &apapproto.ParameterOption{
			Value: item.Value,
			Label: item.Label,
		}
		if item.Description != "" {
			proto.Description = &item.Description
		}
		out[i] = proto
	}
	return out
}

func optionItemsOrDefault(defaultItems []parameters.ParameterOption, defaultValues []string, paramIndex int, allItems [][]parameters.ParameterOption) []parameters.ParameterOption {
	items := parameters.GetParameterOptionItemsOrDefault(defaultItems, paramIndex, allItems)
	if len(items) > 0 {
		return items
	}
	values := parameters.GetParameterOptionValuesOrDefault(defaultValues, paramIndex, allItems)
	if len(values) == 0 {
		return nil
	}
	items = make([]parameters.ParameterOption, len(values))
	for i, v := range values {
		items[i] = parameters.ParameterOption{Value: v, Label: v}
	}
	return items
}

func RecipeInfoToProto(r *recipe.Recipe, po recipe.ParameterOptions, includeRenderParameters bool, platformSupport ...deploymentsupport.PlatformSupport) (*apapproto.ParseRecipeResponse, error) {

	// Add the platform support information
	var supportProtoList []*apapproto.PlatformSupport = nil

	for _, ps := range platformSupport {
		supportProto := &apapproto.PlatformSupport{}
		supportProto.Platform = &apapproto.PlatformConfiguration{Os: string(ps.Platform.OS), Arch: string(ps.Platform.Architecture)}
		supportProto.Result = apapproto.PlatformSupportResult(ps.Result)
		// Now conditions
		var protoConditions []*apapproto.PlatformSupportRequirementSpec
		for _, cond := range ps.ConditionList {
			protoCond := &apapproto.PlatformSupportRequirementSpec{
				Type:       string(cond.Type),
				Parameters: toStringMap(cond.Parameters),
			}
			protoConditions = append(protoConditions, protoCond)
		}
		supportProto.ConditionList = protoConditions
		supportProtoList = append(supportProtoList, supportProto)
	}

	status, err := RecipeStatusToProto(r.Status)
	if err != nil {
		return nil, err
	}

	renderParamCount := len(r.RenderParameters)
	prr := &apapproto.ParseRecipeResponse{
		Name:            r.Name,
		Title:           r.Title,
		Description:     r.Description,
		McpGuidance:     optionalString(r.MCPGuidance),
		Version:         r.Version,
		Status:          status,
		Parameters:      make([]*apapproto.ParameterList, len(r.Parameters.Checkbox)+len(r.Parameters.SingleSelect)+len(r.Parameters.MultiSelect)+len(r.Parameters.Input)+len(r.Parameters.Radio)),
		PlatformSupport: supportProtoList,
	}
	if includeRenderParameters && renderParamCount > 0 {
		prr.RenderParameters = make([]*apapproto.RenderParameter, renderParamCount)
	}

	for _, p := range r.Parameters.Checkbox {
		prr.Parameters[p.Order] = &apapproto.ParameterList{Parameter: &apapproto.ParameterList_Checkbox{Checkbox: &apapproto.CheckboxParameter{
			Base:         ParameterToProto(&p.Parameter),
			DefaultValue: p.DefaultValue,
		}}}
	}

	for i, p := range r.Parameters.SingleSelect {
		prr.Parameters[p.Order] = &apapproto.ParameterList{Parameter: &apapproto.ParameterList_SingleSelect{SingleSelect: &apapproto.SingleSelectParameter{
			Base:         ParameterToProto(&p.Parameter),
			DefaultValue: p.DefaultValue,
			Options:      parameterOptionItemsToProto(optionItemsOrDefault(p.OptionItems, p.Options, i, po.SingleSelectOptions)),
		}}}
	}

	for i, p := range r.Parameters.MultiSelect {
		prr.Parameters[p.Order] = &apapproto.ParameterList{Parameter: &apapproto.ParameterList_MultiSelect{MultiSelect: &apapproto.MultiSelectParameter{
			Base:         ParameterToProto(&p.Parameter),
			DefaultValue: p.DefaultValue,
			Options:      parameterOptionItemsToProto(optionItemsOrDefault(p.OptionItems, p.Options, i, po.MultiSelectOptions)),
		}}}
	}

	for _, p := range r.Parameters.Input {
		prr.Parameters[p.Order] = &apapproto.ParameterList{Parameter: &apapproto.ParameterList_Input{Input: &apapproto.InputParameter{
			Base:         ParameterToProto(&p.Parameter),
			DefaultValue: p.DefaultValue,
			Custom:       p.Custom,
		}}}
	}

	for i, p := range r.Parameters.Radio {
		prr.Parameters[p.Order] = &apapproto.ParameterList{Parameter: &apapproto.ParameterList_Radio{Radio: &apapproto.RadioParameter{
			Base:         ParameterToProto(&p.Parameter),
			DefaultValue: p.DefaultValue,
			Options:      parameterOptionItemsToProto(optionItemsOrDefault(p.OptionItems, p.Options, i, po.RadioOptions)),
		}}}
	}

	if includeRenderParameters {
		for _, p := range r.RenderParameters {
			switch p.Type {
			case parameters.RenderParameterValueTypeNumber:
				prr.RenderParameters[p.Order] = &apapproto.RenderParameter{
					Id:      p.ID,
					Type:    apapproto.RenderParameterType_RENDER_PARAMETER_TYPE_NUMBER,
					IsArray: p.IsArray,
				}
			case parameters.RenderParameterValueTypeString:
				prr.RenderParameters[p.Order] = &apapproto.RenderParameter{
					Id:      p.ID,
					Type:    apapproto.RenderParameterType_RENDER_PARAMETER_TYPE_STRING,
					IsArray: p.IsArray,
				}
			default:
				return nil, fmt.Errorf("unsupported render parameter type %q for parameter %q", p.Type, p.ID)
			}
		}
	}

	return prr, nil
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func RecipeStatusToProto(status recipe.RecipeStatus) (apapproto.RecipeStatus, error) {
	switch status {
	case recipe.RecipeStatusStable:
		return apapproto.RecipeStatus_RECIPE_STATUS_STABLE, nil
	case recipe.RecipeStatusExperimental:
		return apapproto.RecipeStatus_RECIPE_STATUS_EXPERIMENTAL, nil
	case recipe.RecipeStatusPreview:
		return apapproto.RecipeStatus_RECIPE_STATUS_PREVIEW, nil
	default:
		return apapproto.RecipeStatus_RECIPE_STATUS_STABLE, fmt.Errorf("invalid recipe status %q", status)
	}
}

func ProtoTableFormatFromInternal(format query.TableFormat) apapproto.TableFormat {
	switch format {
	case query.TableFormatArrowIPC:
		return apapproto.TableFormat_ARROW_IPC_STREAM
	case query.TableFormatProtobufStruct:
		return apapproto.TableFormat_PROTOBUF_STRUCT
	default:
		return apapproto.TableFormat_UNKNOWN
	}
}

func InternalTableFormatFromProto(format apapproto.TableFormat) (query.TableFormat, error) {
	switch format {
	case apapproto.TableFormat_UNKNOWN:
		return "", errors.New("unknown table format")
	case apapproto.TableFormat_PROTOBUF_STRUCT:
		return query.TableFormatProtobufStruct, nil
	case apapproto.TableFormat_ARROW_IPC_STREAM:
		return query.TableFormatArrowIPC, nil
	default:
		return "", errors.New("unrecognized table format")
	}
}

// ConvertRendererOutputToGRPC takes a RenderOutput and a compatibility warning, and converts the data into a
// grpc proto message
func ConvertRendererOutputToGRPC(r recipe.RenderOutput, renderBound parameters.BoundRenderParameters, compatibilityWarning message.Message) (*apapproto.PrepareRenderResponse, error) {
	output := &apapproto.PrepareRenderResponse{CompatibilityWarning: message.BuildErrorChain(compatibilityWarning)}
	for _, renderer := range r.Renderers {
		cfg, err := structpb.NewStruct(renderer.Config)
		if err != nil {
			return nil, err
		}
		protoRenderer := &apapproto.RendererConfig{
			Renderer: renderer.Type,
			Config:   cfg,
			Id:       &apapproto.RendererId{Value: renderer.ID},
		}
		output.Renderers = append(output.Renderers, protoRenderer)
	}

	for _, visualizer := range r.Widgets {
		cfg, err := structpb.NewStruct(visualizer.Config)
		if err != nil {
			return nil, err
		}

		protoVisualization := &apapproto.VisualizationConfig{
			Id:                &apapproto.VisualizationId{Value: visualizer.ID},
			Type:              visualizer.Type,
			Title:             &visualizer.Title,
			Description:       &visualizer.Description,
			RendererId:        &apapproto.RendererId{Value: visualizer.RendererID},
			Config:            cfg,
			ParameterBindings: visualizer.ParameterBindings,
			Placement:         &visualizer.Placement,
		}
		if visualizer.Disabled != nil {
			protoVisualization.Disabled = &apapproto.WidgetDisabledState{Reason: visualizer.Disabled.Reason}
		}
		output.Visualizations = append(output.Visualizations, protoVisualization)
	}

	renderDetails, err := buildRenderParameterDetails(renderBound)
	if err != nil {
		return nil, err
	}
	output.RenderParameters = renderDetails
	output.VisualizationParameters = buildVisualizationParameterBindings(r.Widgets)

	return output, nil
}

func unmarshalRendererConfigList(request *apapproto.InvokeRenderRequest) (render.RendererConfigList, render.WidgetConfigList, error) {
	var rendererResult render.RendererConfigList
	var widgetResult render.WidgetConfigList

	for _, item := range request.RendererConfig {
		var id *string
		if item.Id != nil {
			id = &item.Id.Value
		}

		json, err := util.MarshalStructToJSON(item.Config)
		if err != nil {
			return nil, nil, err
		}

		rendererResult = append(rendererResult, render.RendererConfig{Name: item.Renderer, ConfigJSON: string(json), ID: id})
	}
	for _, item := range request.VisualizationConfig {
		var id *string
		if item.Id != nil {
			id = &item.Id.Value
		}
		json, err := util.MarshalStructToJSON(item.Config)
		if err != nil {
			return nil, nil, err
		}
		widgetResult = append(widgetResult, render.WidgetConfig{ID: id, ConfigJSON: string(json)})
	}
	return rendererResult, widgetResult, nil
}

func marshalRenderManifest(session render.Session) *apapproto.RenderManifest {
	result := &apapproto.RenderManifest{}

	if session == nil {
		return result
	}

	for _, run := range session.Manifest().Entries() {
		if run.IsHidden() {
			continue
		}

		protoEntry := apapproto.RenderManifestEntry{
			ComponentType:          run.Info().ComponentType().Name,
			ComponentSchemaVersion: run.Info().ComponentType().SchemaVersion,
			RendererIndex:          &apapproto.NumericIndex{Value: int64(run.Info().RendererIndex())},
			AssociatedContent:      make([]*apapproto.RunId, len(run.Info().AssociatedContent())),
			TableName:              run.TableName(),
		}

		id := run.Info().RendererIdentity().ID
		if id != nil {
			protoEntry.RendererId = &apapproto.RendererId{Value: *id}
		}

		for i := range len(run.Info().AssociatedContent()) {
			protoEntry.AssociatedContent[i] = &apapproto.RunId{Value: run.Info().AssociatedContent()[i].Value}
		}

		if run.Info().Pending() {
			protoEntry.Pending = &apapproto.Pending{}
		}

		result.Entry = append(result.Entry, &protoEntry)
	}
	return result
}

// convertReadyOutputToGRPC takes a ReadyOutput and converts it to a grpc response (for communication with the CLI)
func convertReadyOutputToGRPC(readyOutput *recipe.ReadyOutput) *apapproto.RecipeReadyResponse {
	var readyStatus apapproto.ReadyStatus
	switch readyOutput.Status {
	case recipe.ReadyStatusUnknown:
		readyStatus = apapproto.ReadyStatus_READY_STATUS_UNKNOWN
	case recipe.ReadyStatusReady:
		readyStatus = apapproto.ReadyStatus_READY_STATUS_READY
	case recipe.ReadyStatusWarning:
		readyStatus = apapproto.ReadyStatus_READY_STATUS_WARNING
	case recipe.ReadyStatusError:
		readyStatus = apapproto.ReadyStatus_READY_STATUS_ERROR
	default:
		log.Warnf("Invalid ready status: %v", readyOutput.Status)
		readyStatus = apapproto.ReadyStatus_READY_STATUS_WARNING
	}

	var apapAdviceList = []*apapproto.Advice{}
	for _, advice := range readyOutput.Advice {
		var adviceSeverity apapproto.AdviceSeverity
		switch advice.AdviceSeverity {
		case recipe.AdviceSeverityError:
			adviceSeverity = apapproto.AdviceSeverity_ADVICE_SEVERITY_ERROR
		case recipe.AdviceSeverityUnknown:
			adviceSeverity = apapproto.AdviceSeverity_ADVICE_SEVERITY_UNKNOWN
		case recipe.AdviceSeverityWarning:
			adviceSeverity = apapproto.AdviceSeverity_ADVICE_SEVERITY_WARNING
		case recipe.AdviceSeverityMessage:
			adviceSeverity = apapproto.AdviceSeverity_ADVICE_SEVERITY_MESSAGE
		default:
			log.Warnf("Invalid advice severity: %v", advice.AdviceSeverity)
			adviceSeverity = apapproto.AdviceSeverity_ADVICE_SEVERITY_UNKNOWN
		}

		apapAdvice := &apapproto.Advice{ToolName: advice.ToolName, AdviceMessage: message.BuildErrorChain(advice.AdviceMessage), AdviceSeverity: adviceSeverity}
		apapAdviceList = append(apapAdviceList, apapAdvice)
	}
	return &apapproto.RecipeReadyResponse{ReadyStatus: readyStatus, AdviceMessages: apapAdviceList}
}

func RecipeSelectionOptionsFromProto(policy *apapproto.RecipeSelectionPolicyOptions) (recipe.SelectionOptions, error) {
	// Default to UseInstalledVersion if not provided
	if policy == nil {
		return recipe.SelectionOptions{
			Policy: recipe.UseInstalledVersion,
		}, nil
	}

	switch policy.Policy {
	case apapproto.RecipeSelectionPolicyType_USE_INSTALLED_VERSION:
		return recipe.SelectionOptions{
			Policy: recipe.UseInstalledVersion,
		}, nil

	case apapproto.RecipeSelectionPolicyType_FROM_CONTENT:
		return recipe.SelectionOptions{
			Policy: recipe.FromContent,
		}, nil

	case apapproto.RecipeSelectionPolicyType_OVERRIDE_BY_NAME:
		if policy.GetOverrideName() == "" {
			return recipe.SelectionOptions{}, fmt.Errorf("override_name must be set when using OVERRIDE_BY_NAME policy")
		}
		return recipe.SelectionOptions{
			Policy:       recipe.OverrideByName,
			OverrideName: *policy.OverrideName,
		}, nil

	default:
		return recipe.SelectionOptions{}, fmt.Errorf("unknown recipe selection policy: %v", policy.Policy)
	}
}

// ProtoMapToAnyMap converts a map of string to structpb.Value to a map of string to any.
// maps and structs are not supported, lists must contain only strings.
func ProtoMapToAnyMap(inputParams map[string]*structpb.Value) (map[string]any, error) {
	out := make(map[string]any)

	var err error
	for k, v := range inputParams {
		out[k], err = ProtoToAny(v)
		if err != nil {
			return nil, message.New(message.CommonUnknownError).WithCause(fmt.Errorf("error converting value at key %q: %w", k, err))
		}
	}

	return out, nil
}

func DeleteRunsListFromProto(request *apapproto.DeleteRunsRequest) []run.RunID {
	if request == nil {
		return []run.RunID{}
	}
	ids := make([]run.RunID, len(request.Ids))
	for i, id := range request.Ids {
		ids[i] = run.RunID{Value: id}
	}
	return ids
}

func DeleteRunsErrsToProto(ids []run.RunID, errs []error) (*apapproto.DeleteRunsResponse, error) {
	if len(errs) != len(ids) {
		cause := fmt.Errorf("length of errors slice doesn't match number of runs to delete; len(ids) = %v, len(errs) = %v", len(ids), len(errs))
		return nil, message.New(message.CommonUnknownError).WithCause(cause)
	}

	statuses := make([]*apapproto.RunDeletionStatus, len(errs))
	for i, err := range errs {
		statuses[i] = &apapproto.RunDeletionStatus{
			Id:    ids[i].Value,
			Error: message.BuildErrorChain(err),
		}
	}

	return &apapproto.DeleteRunsResponse{Statuses: statuses}, nil
}

func RunUpdateFromProto(patch *apapproto.RunUpdatePatch) (run.RunUpdate, error) {
	if patch == nil {
		return run.RunUpdate{}, nil
	}

	update := run.RunUpdate{Operations: make([]run.RunUpdateOperation, 0, len(patch.GetOperations()))}
	for _, operation := range patch.GetOperations() {
		if operation == nil {
			return run.RunUpdate{}, message.New(message.EngineRunInvalidUpdate)
		}
		switch op := operation.GetOperation().(type) {
		case *apapproto.RunUpdateOperation_SetHostSourceCodePaths:
			update.Operations = append(update.Operations, run.SetHostSourceCodePaths{
				HostSourceCodePaths: run.HostSourceCodePath{
					Paths: op.SetHostSourceCodePaths.GetValue().GetPaths(),
				},
			})
		case *apapproto.RunUpdateOperation_ClearHostSourceCodePaths:
			update.Operations = append(update.Operations, run.ClearHostSourceCodePaths{})
		case *apapproto.RunUpdateOperation_SetGroup:
			update.Operations = append(update.Operations, run.SetRunGroup{Group: op.SetGroup.GetValue()})
		case *apapproto.RunUpdateOperation_ClearGroup:
			update.Operations = append(update.Operations, run.ClearRunGroup{})
		case *apapproto.RunUpdateOperation_SetTags:
			update.Operations = append(update.Operations, run.SetRunTags{Tags: op.SetTags.GetValue().GetValues()})
		case *apapproto.RunUpdateOperation_AddTags:
			update.Operations = append(update.Operations, run.AddRunTags{Tags: op.AddTags.GetValue().GetValues()})
		case *apapproto.RunUpdateOperation_RemoveTags:
			update.Operations = append(update.Operations, run.RemoveRunTags{Tags: op.RemoveTags.GetValue().GetValues()})
		case *apapproto.RunUpdateOperation_ClearTags:
			update.Operations = append(update.Operations, run.ClearRunTags{})
		default:
			return run.RunUpdate{}, message.New(message.EngineRunInvalidUpdate)
		}
	}

	return update.Normalize()
}

func UpdateRunsListFromProto(request *apapproto.UpdateRunsRequest) []run.RunID {
	if request == nil {
		return []run.RunID{}
	}
	ids := make([]run.RunID, len(request.RunIds))
	for i, id := range request.RunIds {
		ids[i] = run.RunID{Value: id.Value}
	}
	return ids
}

func UpdateRunsErrsToProto(ids []run.RunID, errs []error) (*apapproto.UpdateRunsResponse, error) {
	if len(errs) != len(ids) {
		cause := fmt.Errorf("length of errors slice doesn't match number of runs to update; len(ids) = %v, len(errs) = %v", len(ids), len(errs))
		return nil, message.New(message.CommonUnknownError).WithCause(cause)
	}

	statuses := make([]*apapproto.RunUpdateStatus, len(errs))
	for i, err := range errs {
		statuses[i] = &apapproto.RunUpdateStatus{
			Id:    ids[i].Value,
			Error: message.BuildErrorChain(err),
		}
	}

	return &apapproto.UpdateRunsResponse{Statuses: statuses}, nil
}

// SSHKeyInfoToProto converts a slice of SSHKeyInfo to a PrivateSSHKeyListing proto message
func SSHKeyInfoToProto(keys []ssh.SSHKeyInfo) *apapproto.PrivateSSHKeyListing {
	protoKeys := make([]*apapproto.PrivateSSHKey, len(keys))
	for i, key := range keys {
		protoKeys[i] = &apapproto.PrivateSSHKey{
			Path:          key.Path,
			HasPassphrase: key.HasPassphrase,
		}
	}
	return &apapproto.PrivateSSHKeyListing{Keys: protoKeys}
}

// SSHKeyResponseFromProto converts a PrivateSSHKeyListing proto message to a slice of SSHKeyInfo
func SSHKeyResponseFromProto(response *apapproto.PrivateSSHKeyListing) []ssh.SSHKeyInfo {
	if response == nil {
		return []ssh.SSHKeyInfo{}
	}
	keys := make([]ssh.SSHKeyInfo, len(response.Keys))
	for i, protoKey := range response.Keys {
		keys[i] = ssh.SSHKeyInfo{
			Path:          protoKey.Path,
			HasPassphrase: protoKey.HasPassphrase,
		}
	}
	return keys
}
