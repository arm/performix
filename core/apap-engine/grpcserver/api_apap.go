// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"

	"github.com/google/go-cmp/cmp"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/afero"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/cmdsync"
	"github.com/Arm-Debug/apap-cli/apap-engine/collector"
	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/deploymentsupport"
	"github.com/Arm-Debug/apap-cli/apap-engine/grpclogging"
	"github.com/Arm-Debug/apap-cli/apap-engine/logging/logx"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/packages"
	"github.com/Arm-Debug/apap-cli/apap-engine/parameters"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/progress"
	"github.com/Arm-Debug/apap-cli/apap-engine/query"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe/runtime"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipeparser"
	"github.com/Arm-Debug/apap-cli/apap-engine/recovery"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/render/sessionfactory"
	"github.com/Arm-Debug/apap-cli/apap-engine/renderimpls"
	run "github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/ssh"
	"github.com/Arm-Debug/apap-cli/apap-engine/support"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/targetsession"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool/deployer"
	"github.com/Arm-Debug/apap-cli/apap-engine/userdirs"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
	"github.com/Arm-Debug/apap-cli/atperf-compatibility/compatibility"
	"github.com/Arm-Debug/apap-cli/atperf-version/versions"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

type ApapServerConfig struct {
	ParallelJobs              uint   `json:"jobs"`
	DataDirectory             string `json:"data-dir"`
	IsRootWorkerEnabled       bool   `json:"enable-on-demand-privilege"`
	EnableRerendering         bool   `json:"enable-rerendering"`
	EnableFullCaptureSupport  bool   `json:"enable-full-capture-support"`
	EnableExperimentalRecipes bool   `json:"enable-experimental-recipes"`
	EnableSecondaryRunPaths   bool   `json:"enable-secondary-run-paths"`
	EnableTransferManager     bool   `json:"enable-transfer-manager"`
	EnableRenderDBSandbox     bool   `json:"enable-render-db-sandbox"`
	EnableNeoprofTimeline     bool   `json:"enable-neoprof-timeline"`
	ServerHostname            string `json:"server-hostname"`
	ServerGRPCPort            int    `json:"server-port"`
	ServerAuthPort            int    `json:"auth-port"`
	ServerHTTPPort            int    `json:"http-port"`
	ServerHTTPChunkBytes      int    `json:"http-chunk-bytes"`
	LogLevel                  string `json:"log-level"`
	LogFile                   string `json:"log-file"`
	SourceToolsDir            string `json:"source-tools-dir"`
	ConfigDirectory           string `json:"config-dir"`
}

type ApapServer struct {
	apapproto.UnimplementedApapServer
	shutdownCb           func()
	config               ApapServerConfig
	deploymentPaths      deployer.BaseToolDeploymentPaths
	runs                 *run.RunCollection
	databaseFactory      render.DatabaseFactory
	rendererRegistry     *renderimpls.RendererRegistry
	localFs              afero.Fs
	sessions             render.SessionStorage
	recipeCommandMap     cmdsync.CommandStateMap
	targetAccess         target.TargetAccess
	targetSessions       targetsession.TargetSessionProvider
	execPath             string
	recipeFinder         func(recipeName string) (*recipe.Recipe, error)
	parameterPipeline    runtime.ParameterPipeline
	recipeReader         recipeparser.RecipeReader
	packageManager       *packages.PackageManager
	compatibilityChecker compatibility.CompatibilityChecker
}

func NewApapServer(ctx context.Context, config ApapServerConfig, deploymentPaths deployer.BaseToolDeploymentPaths, shutdownCb func()) (*ApapServer, error) {
	runDir := filepath.Join(config.DataDirectory, run.RunDirName)

	secondaryPaths := []string{}
	if config.EnableSecondaryRunPaths {
		secondaryPaths = make([]string, 0, len(userdirs.LegacyDaemonDirName))
		for _, path := range userdirs.LegacyDaemonDirName {
			dataDir, _ := userdirs.DefaultDataDir(path) // Ignore legacy path errors
			legacyDataDir := filepath.Join(dataDir, run.RunDirName)
			if exists, _ := util.PathExists(legacyDataDir); exists {
				secondaryPaths = append(secondaryPaths, legacyDataDir)
			}
		}
	}
	rc, err := run.NewRunCollectionWithSecondaryPaths(runDir, secondaryPaths)
	if err != nil {
		return nil, message.New(message.EngineLifecycleStartupFailed).WithCause(err)
	}

	execPath, err := os.Executable()
	if err != nil {
		return nil, message.New(message.EngineLifecycleStartupFailed).WithCause(err)
	}

	executableDir := filepath.Dir(execPath)
	extensionsDir, err := packages.GetExtensionsDir()
	if err != nil {
		return nil, message.New(message.EngineLifecycleStartupFailed).WithCause(err)
	}
	if err := os.MkdirAll(extensionsDir, perms.LocalDirPerm); err != nil {
		log.WithError(err).Warn("failed to ensure extensions directory exists")
	}

	packageManager := packages.NewPackageManager(executableDir, extensionsDir)

	optionsEvaluator := &runtime.ParameterOptionsEvaluatorConcrete{}
	validator := &runtime.RecipeParameterValidatorConcrete{
		OptionsEvaluator: optionsEvaluator,
	}
	apapServer := &ApapServer{
		shutdownCb:       shutdownCb,
		config:           config,
		runs:             rc,
		databaseFactory:  &render.DuckDBFactory{},
		rendererRegistry: renderimpls.NewRegistry(),
		localFs:          afero.NewOsFs(),
		sessions:         render.NewSessionStorage(),
		deploymentPaths:  deploymentPaths,
		recipeCommandMap: cmdsync.NewCommandStateMap(),
		targetSessions:   targetsession.NewTargetSessionProvider(deploymentPaths.DeployedToolsDirectory, config.IsRootWorkerEnabled),
		execPath:         execPath,
		recipeFinder: func(recipeName string) (*recipe.Recipe, error) {
			return recipeparser.ParseRecipeHelper(recipeparser.FileRecipeReader{}, recipeName)
		},
		parameterPipeline: &runtime.ParameterPipelineConcrete{
			OptionsEvaluator: optionsEvaluator,
			Validator:        validator,
		},
		recipeReader:         &recipeparser.FileRecipeReader{},
		packageManager:       packageManager,
		compatibilityChecker: &compatibility.ConcreteCompatibilityChecker{},
	}

	// Recovery
	recoveryDeps := recovery.RecoveryDeps{RunCollection: rc}
	rm := recovery.NewRecoveryManager(recoveryDeps)
	err = rm.Run(ctx)
	if err != nil {
		return nil, message.New(message.EngineLifecycleStartupFailed).WithCause(err)
	}

	return apapServer, nil
}

func (s *ApapServer) GetVersion(ctx context.Context, in *emptypb.Empty) (*apapproto.ServiceVersion, error) {
	return &apapproto.ServiceVersion{Version: versions.GetVersion()}, nil
}

func (s *ApapServer) recipeAllowed(recipeInfo recipe.Recipe) bool {
	if recipeInfo.Status == recipe.RecipeStatusExperimental {
		return s.config.EnableExperimentalRecipes
	}
	return true
}

func (s *ApapServer) getFullCaptureSupport() bool {
	return s.config.EnableFullCaptureSupport || s.config.EnableRerendering
}

// newBaseStageConfiguration returns a StageConfiguration with server-owned config, shared by all stage workflows.
// This includes all feature flags.
func (s *ApapServer) newBaseStageConfiguration() *runtime.StageConfiguration {
	return &runtime.StageConfiguration{
		ToolBasePaths:          s.getDeploymentPaths(),
		TargetSessions:         s.targetSessions,
		IsRootWorkerEnabled:    s.config.IsRootWorkerEnabled,
		IsFullCaptureEnabled:   s.getFullCaptureSupport(),
		RerenderingEnabled:     s.config.EnableRerendering,
		TransferManagerEnabled: s.config.EnableTransferManager,
		NeoprofTimelineEnabled: s.config.EnableNeoprofTimeline,
		PackageManager:         s.packageManager,
	}
}

func (s *ApapServer) getRecipe(recipeName string) (*recipe.Recipe, error) {
	parsedRecipe, err := s.recipeFinder(recipeName)
	if err != nil {
		return nil, err
	}
	if !s.recipeAllowed(*parsedRecipe) {
		return nil, message.New(message.EngineRecipeDoesNotExist).WithMetadata(map[string]string{"recipe": recipeName})
	}
	return parsedRecipe, nil
}

func (s *ApapServer) Shutdown(ctx context.Context, in *emptypb.Empty) (*emptypb.Empty, error) {
	s.shutdownCb()

	s.sessions.CloseAllRenderSessions()

	if s.targetSessions != nil {
		_ = s.targetSessions.Shutdown()
	}

	return &emptypb.Empty{}, nil
}

func unmarshalContentSelection(request *apapproto.InvokeRenderRequest) []run.RunID {
	var result []run.RunID
	for _, entry := range request.Content.Runs {
		result = append(result, run.RunID{Value: entry.Value})
	}
	return result
}

func MarshalResolvedVisualizationsToProto(session render.Session) *apapproto.VisualizationResolvedTablesList {
	// Marshal Visualization Resolved Tables
	protoList := &apapproto.VisualizationResolvedTablesList{
		Entries: make([]*apapproto.VisualizationResolvedTables, 0),
	}
	if session == nil {
		return protoList
	}

	for visId, dataSources := range session.WidgetDataSources().Get() {
		tableMap := map[string]*apapproto.StringArray{}
		for name, tables := range dataSources {
			arr := &apapproto.StringArray{}
			for _, ref := range tables {
				arr.Values = append(arr.Values, ref.Name)
			}
			tableMap[name] = arr
		}
		visResolvedTables := &apapproto.VisualizationResolvedTables{
			Id:     &apapproto.VisualizationId{Value: visId},
			Tables: tableMap,
		}
		if dataSources.IsPending() {
			visResolvedTables.Pending = &apapproto.Pending{}
		}
		protoList.Entries = append(protoList.Entries, visResolvedTables)
	}

	return protoList
}

func MarshalRenderManifestToJSON(session render.Session, multiline bool) []byte {
	proto := marshalRenderManifest(session)
	return []byte(protojson.MarshalOptions{Multiline: multiline}.Format(proto))
}

func MarshalResolvedVisualizationsToJSON(session render.Session, multiline bool) []byte {
	proto := MarshalResolvedVisualizationsToProto(session)
	return []byte(protojson.MarshalOptions{Multiline: multiline}.Format(proto))
}

func sortManifestEntries(manifest *apapproto.RenderManifest) {
	sort.Slice(manifest.Entry, func(i, j int) bool { return manifest.Entry[i].TableName < manifest.Entry[j].TableName })
}

func sortResolvedVisualizationEntries(list *apapproto.VisualizationResolvedTablesList) {
	sort.Slice(list.Entries, func(i, j int) bool {
		return list.Entries[i].Id.Value < list.Entries[j].Id.Value
	})
}

func JSONToResolvedVisualizationsDiff(jsonLHS []byte, sessionRHS render.Session) (string, error) {
	lhs := new(apapproto.VisualizationResolvedTablesList)
	if err := protojson.Unmarshal(jsonLHS, lhs); err != nil {
		return "", err
	}

	rhs := MarshalResolvedVisualizationsToProto(sessionRHS)

	sortResolvedVisualizationEntries(lhs)
	sortResolvedVisualizationEntries(rhs)
	return cmp.Diff(lhs, rhs, protocmp.Transform()), nil
}

func JSONToManifestDiff(jsonLHS []byte, sessionRHS render.Session) (string, error) {
	// protojson package says it's better to compare in-memory unmarshalled objects than to compare JSON
	// byte-for-byte

	lhs := new(apapproto.RenderManifest)
	if err := protojson.Unmarshal(jsonLHS, lhs); err != nil {
		return "", err
	}

	rhs := marshalRenderManifest(sessionRHS)

	sortManifestEntries(lhs)
	sortManifestEntries(rhs)
	return cmp.Diff(lhs, rhs, protocmp.Transform()), nil
}

func marshalInvokeRenderResponse(in *apapproto.InvokeRenderRequest, session render.Session, invocationErrors []error) (*apapproto.InvokeRenderResponse, error) {
	var response apapproto.InvokeRenderResponse
	response.Manifest = marshalRenderManifest(session)

	if session != nil {
		response.SessionId = session.ID()
	}

	response.InvocationStatuses = make([]*apapproto.RendererInvocationStatus, len(in.RendererConfig))
	for i := range len(response.InvocationStatuses) {
		if i > len(invocationErrors) {
			return nil, fmt.Errorf("internal API error: invocation statuses returned from render system had wrong length")
		}

		status := &apapproto.RendererInvocationStatus{
			Id: in.RendererConfig[i].Id,
		}

		if invocationErrors[i] == nil {
			status.Status = &apapproto.RendererInvocationStatus_Success{Success: &apapproto.Success{}}
		} else if errors.Is(invocationErrors[i], cdf.ErrComponentPending) {
			status.Status = &apapproto.RendererInvocationStatus_Pending{Pending: &apapproto.Pending{}}
		} else {
			status.Status = &apapproto.RendererInvocationStatus_Error{
				Error: &apapproto.Error{Message: fmt.Sprintf("%v", invocationErrors[i])},
			}
		}

		response.InvocationStatuses[i] = status
	}
	response.VisualizationResolvedTables = MarshalResolvedVisualizationsToProto(session)
	return &response, nil
}

func (s *ApapServer) PrepareRender(ctx context.Context, in *apapproto.PrepareRenderRequest) (*apapproto.PrepareRenderResponse, error) {
	runIDs := []run.RunID{}
	runDescriptions := []*run.RunDescription{}
	for _, entry := range in.Content.Runs {
		runID := run.RunID{Value: entry.Value}
		desc, err := s.runs.RunDescription(ctx, runID)
		if err != nil {
			return nil, err
		}
		runIDs = append(runIDs, runID)
		runDescriptions = append(runDescriptions, desc)
	}

	opts, err := RecipeSelectionOptionsFromProto(in.RecipeSelectionPolicy)
	if err != nil {
		return nil, message.New(message.CommonUnknownError).WithCause(err)
	}

	recipeName, err := recipe.SelectRenderSourceRecipe(s.runs, opts, runIDs)
	if err != nil {
		return nil, err
	}

	compatibilityWarning, err := render.CheckRunCompatibility(ctx, s.runs, opts.Policy, runIDs, s.compatibilityChecker)
	if err != nil {
		return nil, err
	}

	parsedRecipe, err := s.getRecipe(recipeName)
	if err != nil {
		return nil, err
	}

	convertedParams, err := ProtoMapToAnyMap(in.GetRenderParameters())
	if err != nil {
		return nil, err
	}

	convertedParams, err = parameters.ConvertRenderParameterInputs(convertedParams, parsedRecipe.RenderParameters, parsedRecipe.Name)
	if err != nil {
		return nil, err
	}

	renderBound, err := parameters.BindRenderParameters(convertedParams, parsedRecipe.RenderParameters, parsedRecipe.Name)
	if err != nil {
		return nil, err
	}

	content, _, err := render.LoadContent(ctx, s.runs, runIDs, s.packageManager)
	if err != nil {
		return nil, err
	}

	runModels := make([]cdf.ModelView, len(content.Entries))
	for i, entry := range content.Entries {
		runModels[i] = entry.Model
	}

	buildRenderConfig := func(renderParams map[string]any) *runtime.StageConfiguration {
		config := s.newBaseStageConfiguration()
		config.Recipe = parsedRecipe
		config.RunModels = runModels
		config.Ctx = &recipe.RecipeCtx{
			RenderParamValues: renderParams,
			RecipeMetadata: recipe.RecipeMetadata{
				Name:       parsedRecipe.Name,
				Version:    parsedRecipe.Version,
				APIVersion: parsedRecipe.APIVersion,
			},
		}
		return config
	}

	renderConfig := buildRenderConfig(renderBound.CollapseToMap())
	baselineSpec, err := runtime.GetRendererSpec(ctx, renderConfig, runDescriptions)
	if err != nil {
		return nil, err
	}

	convertedVisualizationParams, err := ProtoMapToAnyMap(in.GetVisualizationParameters())
	if err != nil {
		return nil, err
	}

	rendererSpec := baselineSpec
	mappedVisualizationParams, err := render.MapVisualizationParamsToRenderParams(convertedVisualizationParams, baselineSpec.Widgets, parsedRecipe.RenderParameters)
	if err != nil {
		return nil, err
	}
	if len(mappedVisualizationParams) > 0 {
		// MVP restriction: explicit render params stay authoritative; mapped visualization params only fill gaps.
		mergedParams := render.MergeRenderParams(convertedParams, mappedVisualizationParams)
		mergedParams, err = parameters.ConvertRenderParameterInputs(mergedParams, parsedRecipe.RenderParameters, parsedRecipe.Name)
		if err != nil {
			return nil, err
		}

		renderBound, err = parameters.BindRenderParameters(mergedParams, parsedRecipe.RenderParameters, parsedRecipe.Name)
		if err != nil {
			return nil, err
		}

		// Re-run render stages with mapped params, then ensure topology/bindings are unchanged.
		renderConfig = buildRenderConfig(renderBound.CollapseToMap())
		rendererSpec, err = runtime.GetRendererSpec(ctx, renderConfig, runDescriptions)
		if err != nil {
			return nil, err
		}

		if err := render.ValidateRenderOutputStability(baselineSpec, rendererSpec); err != nil {
			return nil, err
		}
	}

	response, err := ConvertRendererOutputToGRPC(rendererSpec, renderBound, compatibilityWarning)
	if err != nil {
		return nil, message.New(message.CommonUnknownError).WithCause(err)
	}
	return response, nil
}

func (s *ApapServer) buildSupportPackageConfig() map[string]any {
	// Preserve the server config values in the support package. Utilise struct marshalling so that changes to the config
	// are automatically included without needing to manually update this function.
	// Ignore the error to simplify the signature, the error will rise if config is amended to include unmarshalable fields, unit tests will catch it.
	vars, _ := util.StructToMap(&s.config)
	vars["deployment-tools-dir"] = s.deploymentPaths.DeployedToolsDirectory
	return vars
}

func (s *ApapServer) newSessionFactory() render.SessionFactory {
	flags := render.DefaultSessionFlags()
	flags.EnableDuckDBSandbox = s.config.EnableRenderDBSandbox
	return &sessionfactory.Impl{Flags: &flags}
}

func (s *ApapServer) InvokeRender(ctx context.Context, in *apapproto.InvokeRenderRequest) (*apapproto.InvokeRenderResponse, error) {
	// Seed logx context logger with rpcID for intermediate log traceability
	ctx = logx.CtxWithLogger(ctx, grpclogging.LoggerFromContext(ctx))

	rendererConfigList, visConfigList, err := unmarshalRendererConfigList(in)
	if err != nil {
		return nil, err
	}

	session, invocationErrors, err := render.StartRenderSession(
		ctx,
		s.newSessionFactory(),
		&s.sessions,
		s.rendererRegistry,
		s.runs,
		unmarshalContentSelection(in),
		rendererConfigList,
		visConfigList,
		s.databaseFactory,
		s.packageManager,
		s.targetSessions,
	)
	if err != nil {
		return nil, err
	}

	return marshalInvokeRenderResponse(in, session, invocationErrors)
}

func (s *ApapServer) ListRenders(ctx context.Context, in *emptypb.Empty) (*apapproto.RenderListing, error) {
	sessionIds := s.sessions.GetAllSessionIds()
	renderListing := apapproto.RenderListing{}
	sqlStr := "SELECT (SUM(MEMORY_USAGE_BYTES)/(1024^3)) AS 'total memory usage' FROM duckdb_memory()"
	dbUsageByKey := make(map[string]float64)

	for _, id := range sessionIds {
		session, err := s.sessions.GetSessionByID(id)
		if err != nil {
			return &apapproto.RenderListing{}, fmt.Errorf("session with id '%s' does not exist", id)
		}
		defer session.Done()

		dbKey := session.S.DatabaseKey()

		accessor, err := query.NewNativeRowTableAccessor(session.S.Database(), sqlStr, query.NativeRowSettings{RowsPerBatch: 1})
		if err != nil {
			// If connection is closed, ignore this session - this can happen in certain circumstances, see APAP-1923
			// https://jira.arm.com/browse/APAP-1923
			if errors.Is(err, sql.ErrConnDone) {
				continue
			}
			return &apapproto.RenderListing{}, err
		}
		defer accessor.Close()

		chunk, err := accessor.NextChunk()
		if err == io.EOF {
			return &apapproto.RenderListing{}, errors.New("query returned 0 rows")
		} else if err != nil {
			return &apapproto.RenderListing{}, err
		}
		if len(chunk) > 1 {
			return &apapproto.RenderListing{}, fmt.Errorf("more than 1 row returned by query %v", sqlStr)
		}

		memoryUsage := chunk[0]["total memory usage"]
		floatMemoryUsage, ok := memoryUsage.(float64)
		if !ok {
			return &apapproto.RenderListing{}, fmt.Errorf("couldn't convert %v to float64", memoryUsage)
		}
		dbUsageByKey[dbKey] = floatMemoryUsage
		sessionInfo := apapproto.SessionInfo{
			SessionId: id,
			DbKey:     dbKey,
		}
		renderListing.Sessions = append(renderListing.Sessions, &sessionInfo)
	}

	for dbKey, usage := range dbUsageByKey {
		renderListing.DbInstances = append(renderListing.DbInstances, &apapproto.DbInstanceInfo{
			DbKey:          dbKey,
			MemoryUsageGib: usage,
		})
	}
	return &renderListing, nil
}

func (s *ApapServer) CloseRender(ctx context.Context, in *apapproto.CloseRenderRequest) (*emptypb.Empty, error) {
	exists := s.sessions.SessionRegistered(in.SessionId)
	if !exists {
		log.WithField("Id", in.SessionId).Warn("attempted to close render session that does not exist")
		return &emptypb.Empty{}, fmt.Errorf("session with id '%s' does not exist", in.SessionId)
	}

	s.sessions.CloseRenderSession(in.SessionId)

	return &emptypb.Empty{}, nil
}

func (s *ApapServer) TargetTest(ctx context.Context, in *apapproto.TargetTestRequest) (*apapproto.TargetTestResponse, error) {
	tgt, err := TargetFromProto(in.Target)
	if err != nil {
		log.WithError(err).Error("invalid target")
		return nil, message.New(message.CommonUnknownError).WithCause(err)
	}

	targetSession, err := s.targetSessions.TargetSession(tgt)
	if err != nil {
		return nil, err
	}
	_, connError := targetSession.Connect(ctx)
	connection := SSHConnectErrorToProto(connError)

	return &apapproto.TargetTestResponse{Connection: connection}, nil
}

func (s *ApapServer) TargetPrepare(ctx context.Context, in *apapproto.TargetPrepareRequest) (*apapproto.TargetPrepareResponse, error) {
	tgt, err := TargetFromProto(in.Target)
	if err != nil {
		log.WithError(err).Error("invalid target")
		return nil, message.New(message.CommonUnknownError).WithCause(err)
	}

	lock := s.targetAccess.LockWithCancellation(tgt, "target prepare", ctx.Done())
	if lock == nil {
		return nil, message.New(message.EngineCommonUserCancellationError)
	}
	defer lock.Unlock()

	result, err := runtime.DeployMandatoryTools(ctx, tgt, s.getDeploymentPaths(), deployer.ToolDeploymentMode(in.DeploymentType), s.packageManager, s.targetSessions)
	response := apapproto.TargetPrepareResponse{Result: ToProto(result)}
	return &response, err
}

// TargetInfoCollector will send the platform probe request and translate the platform probe response
// into the TargetInfoCollectResponse and back to the CLI
// Additional tool data can be collected by using the collector tags:
// sl-collect-target-info - Collect basic target system info, os & version, arch
// sl-collect-target-pids - Collect a list of running processes
func (s *ApapServer) TargetInfoCollector(ctx context.Context, in *apapproto.TargetInfoRequest) (*apapproto.TargetInfoResponse, error) {
	tgt, err := TargetFromProto(in.Target)
	if err != nil {
		log.WithError(err).Error("invalid target")
		return nil, message.New(message.CommonUnknownError).WithCause(err)
	}

	lock := s.targetAccess.LockWithCancellation(tgt, "target info", ctx.Done())
	if lock == nil {
		return nil, message.New(message.EngineCommonUserCancellationError)
	}
	defer lock.Unlock()
	targetSession, err := s.targetSessions.TargetSession(tgt)
	if err != nil {
		return nil, err
	}
	_, err = targetSession.Connect(ctx)
	if err != nil {
		return nil, err
	}
	agentConn, err := targetSession.TargetAgent(ctx)
	if err != nil {
		return nil, err
	}

	stream, err := agentConn.Client.HoldLock(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, message.New(message.EngineCommonUserCancellationError)
	}
	_, err = stream.Recv()
	if err != nil {
		return nil, err
	}

	collectors := map[string]collector.AgentCollector{
		"sl-collect-target-info": collector.NewAgentTargetInfoCollector(),
		"sl-collect-target-pids": &collector.AgentPIDCollector{},
	}

	// Iterate over CLI specified collectors finding the approriate one
	targetInfoResp := apapproto.TargetInfoResponse{Info: make(map[string]*apapproto.TargetInfo)}
	type collectorDone struct {
		name string
		err  error
		resp *apapproto.TargetInfo
	}

	collectorDoneChannel := make(chan collectorDone, len(in.Collectors))
	startedCollectors := 0

	for _, v := range in.Collectors {
		if collector, ok := collectors[v]; ok {
			collectorName, col := v, collector // Capture the variables for the goroutine
			startedCollectors++
			go func() {
				resp, err := col.Collect(ctx, agentConn.Client)
				collectorDoneChannel <- collectorDone{name: collectorName, err: err, resp: resp}
			}()
		}
	}

	var joinedErrors error

	for i := 0; i < startedCollectors; i++ {
		collectorResult := <-collectorDoneChannel
		targetInfoResp.Info[collectorResult.name] = collectorResult.resp
		if collectorResult.err != nil {
			joinedErrors = errors.Join(joinedErrors, collectorResult.err)
		}
	}

	return &targetInfoResp, joinedErrors
}

func (s *ApapServer) RecipeIssueCommand(in *apapproto.RecipeCommand, out apapproto.Apap_RecipeIssueCommandServer) error {
	var err error
	switch command := in.SpecificCommand.(type) {
	case *apapproto.RecipeCommand_StartCommand:
		hook, ctx := runtime.InitializeRunLog(out.Context())
		defer hook.Close()
		logx.FromContext(ctx).Infof("Starting run using %v Engine %v", terminology.GetProductFullName(), versions.GetVersion())

		// Create stage notifier
		grpcNotifier := &progress.GRPCRecipeStageNotifier{Out: out}
		stageNotifier := recipe.NewCompositeStageNotifier(
			grpcNotifier,
			recipe.NewLoggingStageNotifier(logx.FromContext(ctx)),
		)

		parsedRecipe, err := s.getRecipe(command.StartCommand.GetName())
		if err != nil {
			logx.FromContext(ctx).WithError(err).Error("failed to parse recipe")
			grpcNotifier.SendRecipeFinishMessage(out, apapproto.StatusCode_ERROR, message.BuildErrorChain(err))
			return err
		}

		recipeCtx, err := RecipeCtxFromProto(command.StartCommand)
		if err != nil {
			logx.FromContext(ctx).WithError(err).Error("failed to construct recipe context")
			grpcNotifier.SendRecipeFinishMessage(out, apapproto.StatusCode_ERROR, message.BuildErrorChain(err))
			return err
		}

		convertedParams, err := ProtoMapToAnyMap(command.StartCommand.Parameters)
		if err != nil {
			return err
		}

		recipeCtx.ParamValues, err = parameters.BindRecipeParameters(convertedParams, parsedRecipe.Parameters, parsedRecipe.Name)
		if err != nil {
			logx.FromContext(ctx).WithError(err).Error("parameter setup failed")
			grpcNotifier.SendRecipeFinishMessage(out, apapproto.StatusCode_ERROR, message.BuildErrorChain(err))
			return err
		}
		recipeCtx.RecipeMetadata.Version = parsedRecipe.Version
		recipeCtx.RecipeMetadata.APIVersion = parsedRecipe.APIVersion
		recipeCtx.ToolVersions = parsedRecipe.ToolVersions

		// Run the recipe.
		stageConfig := s.newBaseStageConfiguration()
		stageConfig.Recipe = parsedRecipe
		stageConfig.Ctx = recipeCtx
		stageConfig.RunCollection = s.runs
		stageConfig.ToolDeploymentType = deployer.ToolDeploymentMode(command.StartCommand.DeploymentType)
		stageConfig.UsrMessageWriter = &run.ConcreteUserMessageWriter{}
		stageConfig.CollectionState = &recipe.CollectionState{}
		err = runtime.RunRecipe(
			ctx,
			hook,
			stageConfig,
			&runtime.RunStageFactory{},
			stageNotifier,
			s.recipeCommandMap,
		)

		// When the run completes, stream the finish message (and result) back to the client
		returnCode := apapproto.StatusCode_SUCCESS
		if err != nil {
			logx.FromContext(ctx).WithError(err).Error("recipe run failed")
			returnCode = apapproto.StatusCode_ERROR
		}
		grpcNotifier.SendRecipeFinishMessage(out, returnCode, message.BuildErrorChain(err))

	case *apapproto.RecipeCommand_CancelCommand:
		// Write CANCEL flag
		logx.FromContext(out.Context()).
			WithField("runID", command.CancelCommand.Id.Value).
			Info("Cancel request received")
		runID := run.RunID{Value: command.CancelCommand.Id.Value}
		err = s.recipeCommandMap.Write(runID, cmdsync.CommandCancel)

	case *apapproto.RecipeCommand_StopCommand:
		// Write STOP flag
		logx.FromContext(out.Context()).
			WithField("runID", command.StopCommand.Id.Value).
			Info("Stop request received")
		runID := run.RunID{Value: command.StopCommand.Id.Value}
		err = s.recipeCommandMap.Write(runID, cmdsync.CommandStop)
	}

	return err
}

func (s *ApapServer) ListRuns(ctx context.Context, _ *emptypb.Empty) (*apapproto.RunListing, error) {
	entries, err := s.runs.ListRuns(ctx)
	if err != nil {
		log.WithError(err).Error("failed to read run dir")
		return nil, err
	}

	runContents := apapproto.RunListing{}
	runContents.Runs = []*apapproto.ListedRun{}

	for _, e := range entries {
		listed := &apapproto.ListedRun{Id: e.Value}

		// Try to get run description
		desc, err := s.runs.RunDescription(ctx, run.RunID{Value: e.Value})
		if err != nil {
			listed.Item = &apapproto.ListedRun_ListError{
				ListError: &apapproto.Error{Message: fmt.Sprintf("failed to list run with ID %v: %v", e.Value, err)},
			}
			runContents.Runs = append(runContents.Runs, listed)
			continue
		}

		// Convert to apapproto.RunDescription
		outDesc, err := DescToProto(desc)
		if err != nil {
			return nil, err
		}
		listed.Item = &apapproto.ListedRun_Description{
			Description: outDesc,
		}
		runContents.Runs = append(runContents.Runs, listed)
	}
	return &runContents, nil
}

func (s *ApapServer) GetRunDescription(ctx context.Context, request *apapproto.GetRunDescriptionRequest) (*apapproto.RunDescription, error) {
	desc, err := s.runs.RunDescription(ctx, run.RunID{Value: request.Id.Value})
	if err != nil {
		return nil, err
	}

	// Convert to apapproto.RunDescription
	outDesc, err := DescToProto(desc)
	if err != nil {
		return nil, err
	}

	// To support backwards compatibility we allow this to fail
	if source, err := run.ReadHostSourceCodePath(filepath.Join(s.runs.GetRunPath(run.RunID{Value: request.Id.Value}), run.SourceCodeFilename)); err == nil {
		outDesc.HostSourceCodePaths = SourceToProto(source)
	}

	// Extra: Data tables generated from renderers
	allExtra := make([]*apapproto.RunExtraOrError, 0)
	for _, req := range request.ExtrasRequestStd {
		switch req {
		case apapproto.StandardRunDescriptionExtras_EXTRA_TARGET_INFO:
			extra := s.QueryRunExtra(
				ctx,
				[]*apapproto.RunId{request.Id},
				"TargetInfoRenderer",
				nil, // or a filtering function
				s.InvokeRender,
			)
			allExtra = append(allExtra, extra)
		default:
			stringErr := fmt.Errorf("unrecognized extra request: %v", req)
			return nil, message.New(message.CommonUnknownError).WithCause(stringErr)
		}
	}
	outDesc.Extra = allExtra

	return outDesc, nil
}

// RecipeReady connects to the target via ssh, and the sl-collect server via TCP in order to establish the readiness of the recipe & workload
func (s *ApapServer) RecipeReady(context context.Context, request *apapproto.RecipeReadyRequest) (*apapproto.RecipeReadyResponse, error) {
	parsedRecipe, err := s.getRecipe(request.GetRecipeInfo().GetName())
	if err != nil {
		return nil, err
	}

	recipeCtx, err := RecipeCtxFromProto(request.RecipeInfo)
	if err != nil {
		log.WithError(err).Error("failed to create recipe context")
		return nil, err
	}

	convertedParameters, err := ProtoMapToAnyMap(request.RecipeInfo.Parameters)
	if err != nil {
		return nil, err
	}

	if recipeCtx.ParamValues, err = parameters.BindRecipeParameters(convertedParameters, parsedRecipe.Parameters, parsedRecipe.Name); err != nil {
		return nil, err
	}

	recipeCtx.RecipeMetadata.Version = parsedRecipe.Version
	recipeCtx.RecipeMetadata.APIVersion = parsedRecipe.APIVersion
	recipeCtx.ToolVersions = parsedRecipe.ToolVersions

	stageConfig := s.newBaseStageConfiguration()
	stageConfig.Recipe = parsedRecipe
	stageConfig.Ctx = recipeCtx
	stageConfig.OperationName = fmt.Sprintf("recipe ready %s", recipeCtx.RecipeMetadata.Name)
	response, err := runtime.CheckRecipeReady(context, stageConfig)
	if err != nil {
		log.WithError(err).Error("failed to check recipe readiness")
		return nil, err
	}

	return convertReadyOutputToGRPC(response), nil
}

// ParseRecipe - parse the named recipe and return its definition, and platform support.
// If a target is provided, parameter value functions will be executed and the target info will be exposed. Additionally,
// platform suopport information will be specific to the target.
func (s *ApapServer) ParseRecipe(context context.Context, request *apapproto.ParseRecipeMessage) (*apapproto.ParseRecipeResponse, error) {
	parsedRecipe, err := s.getRecipe(request.GetName())
	if err != nil {
		return nil, err
	}
	targetSupportCollector := &runtime.TargetSupportCollector{}
	var platformSupport deploymentsupport.PlatformSupport

	if request.Target != nil {
		tgt, err := TargetFromProto(request.Target)
		if err != nil {
			return nil, err
		}

		config := s.newBaseStageConfiguration()
		config.Recipe = parsedRecipe
		config.Ctx = &recipe.RecipeCtx{Target: tgt, ToolVersions: parsedRecipe.ToolVersions}
		config.OperationName = fmt.Sprintf("parameter options %s", request.Name)
		if config.Ctx.ParamValues, err = parameters.BindRecipeParameters(map[string]any{}, parsedRecipe.Parameters, parsedRecipe.Name); err != nil {
			return nil, err
		}
		stageContext := &recipe.StageContext{
			Context:               context,
			CommandState:          &cmdsync.CommandState{},
			ReadinessNotifier:     &recipe.NullReadinessNotifier{},
			RendererNotifier:      &recipe.NullRenderNotifier{},
			StageNotifier:         &recipe.NullStageNotifier{},
			TargetSupportNotifier: targetSupportCollector,
			ParameterOptions: recipe.ParameterOptions{
				SingleSelectOptions: make([][]parameters.ParameterOption, len(parsedRecipe.Parameters.SingleSelect)),
				MultiSelectOptions:  make([][]parameters.ParameterOption, len(parsedRecipe.Parameters.MultiSelect)),
				RadioOptions:        make([][]parameters.ParameterOption, len(parsedRecipe.Parameters.Radio)),
			},
		}

		options := stageContext.ParameterOptions
		if s.parameterPipeline != nil {
			options, err = s.parameterPipeline.EvaluateOptions(context, config, stageContext)
			if err != nil {
				return nil, err
			}
		}
		// Retrieve the platform support information collected during parameter option stages
		platformSupport = targetSupportCollector.PlatformSupport
		return RecipeInfoToProto(parsedRecipe, options, s.config.EnableRerendering, platformSupport)
	} else {
		// No target provided, so get generic platform support from the recipe deployments
		supportedPlatforms, err := recipe.GetSupportedPlatforms(s.packageManager, parsedRecipe)
		if err != nil {
			return nil, err
		}
		return RecipeInfoToProto(parsedRecipe, recipe.ParameterOptions{}, s.config.EnableRerendering, supportedPlatforms...)
	}
}

func (s *ApapServer) ListRecipes(ctx context.Context, in *emptypb.Empty) (*apapproto.RecipeNameListing, error) {
	recipeParseErrors := map[string]error{}
	errHandler := func(filename string, err error) {
		recipeParseErrors[filename] = err
	}

	recipes, err := s.recipeReader.ReadRecipes(errHandler)
	if err != nil {
		return nil, err
	} else if len(recipeParseErrors) > 0 {
		log.Warnf("failed to parse recipe files: %v", recipeParseErrors)
	}

	var entries []*apapproto.RecipeNameEntry
	for name := range recipes {
		if !s.recipeAllowed(recipes[name]) {
			continue
		}
		entries = append(entries, &apapproto.RecipeNameEntry{Identifier: &apapproto.RecipeNameEntry_Name{Name: name}})
	}

	for path, loadErr := range recipeParseErrors {
		entries = append(entries, &apapproto.RecipeNameEntry{Identifier: &apapproto.RecipeNameEntry_Path{Path: path}, LoadError: message.BuildErrorChain(loadErr)})
	}

	return &apapproto.RecipeNameListing{RecipeNames: entries}, nil
}

func (s *ApapServer) RenameRun(ctx context.Context, request *apapproto.RunRenameRequest) (*apapproto.RunRenameResponse, error) {
	runID := run.RunID{Value: request.RunId.Value}
	err := s.runs.RenameRun(ctx, runID, request.NewName)
	if err != nil {
		return &apapproto.RunRenameResponse{ReturnCode: apapproto.StatusCode_ERROR}, err
	}
	return &apapproto.RunRenameResponse{ReturnCode: apapproto.StatusCode_SUCCESS}, nil
}

func (s *ApapServer) DeleteRun(ctx context.Context, runID *apapproto.RunId) (*apapproto.DeleteRunResponse, error) {
	err := s.runs.DeleteRun(ctx, run.RunID{Value: runID.Value})
	if err != nil {
		return &apapproto.DeleteRunResponse{ReturnCode: apapproto.StatusCode_ERROR}, err
	}

	return &apapproto.DeleteRunResponse{ReturnCode: apapproto.StatusCode_SUCCESS}, nil
}

func (s *ApapServer) DeleteRuns(ctx context.Context, request *apapproto.DeleteRunsRequest) (*apapproto.DeleteRunsResponse, error) {
	if request != nil && request.GetDeleteAll() {
		ids, errs, err := s.runs.DeleteAllRuns(ctx)
		if err != nil {
			return nil, err
		}
		return DeleteRunsErrsToProto(ids, errs)
	}

	ids := DeleteRunsListFromProto(request)
	errs := s.runs.DeleteRuns(ctx, ids)
	return DeleteRunsErrsToProto(ids, errs)
}

func (s *ApapServer) ExportRun(ctx context.Context, exportRequest *apapproto.RunExportRequest) (*apapproto.RunExportResponse, error) {
	runID := run.RunID{Value: exportRequest.RunId.Value}
	destDirectory := exportRequest.TargetDirectory
	err := s.runs.ExportRun(ctx, runID, destDirectory)
	if err != nil {
		return &apapproto.RunExportResponse{ReturnCode: apapproto.StatusCode_ERROR.Enum()}, err
	}

	return &apapproto.RunExportResponse{ReturnCode: apapproto.StatusCode_SUCCESS.Enum()}, nil
}

func (s *ApapServer) ImportRun(ctx context.Context, importRequest *apapproto.RunImportRequest) (*apapproto.RunImportResponse, error) {
	logx.FromContext(ctx).Infof("Importing run: %s", importRequest.ExternalRunPath)

	runDir := importRequest.ExternalRunPath
	newID, err := s.runs.ImportRun(runDir)
	if err != nil {
		logx.FromContext(ctx).Infof("Failed to importing run: %s: %v", importRequest.ExternalRunPath, err)
		return &apapproto.RunImportResponse{ReturnCode: apapproto.StatusCode_ERROR.Enum()}, err
	}

	newIDResponse := &apapproto.RunId{Value: newID.Value}
	return &apapproto.RunImportResponse{ReturnCode: apapproto.StatusCode_SUCCESS.Enum(), NewId: newIDResponse}, nil
}

func (s *ApapServer) CreateSupportPackage(ctx context.Context, req *apapproto.CreateSupportPackageRequest) (*apapproto.CreateSupportPackageResponse, error) {
	options := support.PackageOptions{
		RunIDs:     util.Map(req.RunIds, func(id *apapproto.RunId) run.RunID { return run.RunID{Value: id.Value} }),
		OutputDir:  req.GetOutputDirectory(),
		CLIVersion: req.GetCliVersion(),
		GUIVersion: req.GetGuiVersion(), // Will be empty if the request did not come from the GUI client
		LogCount:   int(req.GetLogCount()),
		LogFile:    s.config.LogFile,
		GUILogDir:  req.GetGuiLogDirectory(),
	}

	result, err := support.CreateSupportPackage(ctx, options, s.buildSupportPackageConfig(), s.runs)
	if err != nil {
		return &apapproto.CreateSupportPackageResponse{}, err
	}

	return &apapproto.CreateSupportPackageResponse{
		PackagePath:      result.PackagePath,
		PackageSizeBytes: uint64(result.PackageSizeBytes), // #nosec G115
	}, nil
}

func (s *ApapServer) ListPrivateSSHKeys(ctx context.Context, _ *emptypb.Empty) (*apapproto.PrivateSSHKeyListing, error) {
	keys := ssh.ListHostPrivateKeys(afero.NewOsFs())
	return SSHKeyInfoToProto(keys), nil
}

func (s *ApapServer) FindSSHKeysForTarget(ctx context.Context, targetIn *apapproto.Target) (*apapproto.SSHKeyResponse, error) {
	tgt, err := TargetFromProto(targetIn)
	if err != nil {
		return nil, message.New(message.CommonUnknownError).WithCause(err)
	}
	sshT, ok := tgt.(*target.SSHTarget)
	if !ok {
		targetType := "nil"
		if tgt != nil {
			targetType = reflect.TypeOf(tgt).String()
		}
		return nil, message.New(message.CommonUnsupportedTargetType).WithMetadata(map[string]string{"targetType": targetType})
	}

	keys, err := conductor.FindSSHKeysForTarget(ctx, sshT)
	// return the error as a successful response with the error message, to allow the GUI to parse the ErrorChain
	// TODO: remove after GUI has implemented error decoding (https://jira.arm.com/browse/APAP-3771)
	if err != nil {
		return &apapproto.SSHKeyResponse{Error: message.BuildErrorChain(err)}, nil
	}
	return &apapproto.SSHKeyResponse{PrivateKeyPaths: &apapproto.StringArray{Values: keys}}, nil
}

func (s *ApapServer) getDeploymentPaths() deployer.BaseToolDeploymentPaths {
	deploymentPaths := deployer.BaseToolDeploymentPaths{
		DeployedToolsDirectory: s.deploymentPaths.DeployedToolsDirectory,
	}
	return deploymentPaths
}

func (s *ApapServer) RecipeValidateParameters(ctx context.Context, req *apapproto.RecipeValidateParametersRequest) (*apapproto.RecipeValidateParametersResponse, error) {
	parsedRecipe, err := s.getRecipe(req.RecipeName)
	if err != nil {
		return nil, err
	}

	stageConfig := s.newBaseStageConfiguration()
	stageConfig.Recipe = parsedRecipe
	stageConfig.RunCollection = s.runs

	// Validate the workload
	workload, err := RecipeWorkloadFromProto(req.Workload)
	if err != nil {
		return nil, err
	}

	if err := validateRecipeWorkload(workload, req.RecipeName); err != nil {
		return nil, err
	}

	stageConfig.Ctx = &recipe.RecipeCtx{
		OrigWorkload: workload,
		RecipeMetadata: recipe.RecipeMetadata{
			Name:       parsedRecipe.Name,
			Version:    parsedRecipe.Version,
			APIVersion: parsedRecipe.APIVersion,
		},
	}
	if req.TargetName != nil {
		stageConfig.Ctx.TargetName = *req.TargetName
	}

	if req.Target != nil {
		stageConfig.Ctx.Target, err = TargetFromProto(req.Target)
		if err != nil {
			return nil, err
		}
	} else {
		if len(parsedRecipe.ParameterOptionsStages) > 0 {
			return nil, message.New(message.EngineGrpcserverApiApapTargetRequired).WithMetadata(map[string]string{"recipeName": req.RecipeName})
		}
	}

	convertedParameters, err := ProtoMapToAnyMap(req.Parameters)
	if err != nil {
		return nil, err
	}

	if stageConfig.Ctx.ParamValues, err = parameters.BindRecipeParameters(convertedParameters, parsedRecipe.Parameters, parsedRecipe.Name); err != nil {
		return nil, err
	}

	// Options are computed via ParameterOptionsEvaluator before validation; skip option stages here to avoid double execution.
	includeOptionStages := false
	if s.parameterPipeline == nil {
		return nil, errors.New("parameter pipeline not configured")
	}
	validationResult, err := s.parameterPipeline.Validate(
		ctx,
		&runtime.ValidationStageFactoryImpl{IncludeOptionStages: &includeOptionStages},
		stageConfig,
	)
	if err != nil {
		return nil, err
	}
	return &apapproto.RecipeValidateParametersResponse{
		Messages: util.Map(validationResult.Errors, func(pve recipe.ParameterValidationError) *apapproto.ParameterValidationResult {
			return &apapproto.ParameterValidationResult{
				ParameterId: pve.ParameterId,
				Message:     message.BuildErrorChain(pve.Message),
			}
		}),
	}, nil
}

func (s *ApapServer) UpdateRuns(ctx context.Context, req *apapproto.UpdateRunsRequest) (*apapproto.UpdateRunsResponse, error) {
	update, err := RunUpdateFromProto(req.GetPatch())
	if err != nil {
		return nil, err
	}

	ids := UpdateRunsListFromProto(req)
	errs := s.runs.UpdateRuns(ctx, ids, update)
	return UpdateRunsErrsToProto(ids, errs)
}

func (s *ApapServer) LookupMessage(ctx context.Context, req *apapproto.LookupMessageRequest) (*apapproto.LookupMessageResponse, error) {
	// Rebuild the Message from the request
	msg := message.New(req.Msg.Code).WithMetadata(req.Msg.Metadata)
	catalogMsg, _ := message.LookupMessage(msg)

	// If LookupMessage failed, the catalog message will be
	rsp := &apapproto.CatalogMessage{
		Code:        catalogMsg.Code,
		Severity:    catalogMsg.Severity,
		Message:     catalogMsg.Message,
		Explanation: catalogMsg.Explanation,
		Advice:      catalogMsg.Advice,
	}

	return &apapproto.LookupMessageResponse{CatalogMsg: rsp}, nil
}

func (s *ApapServer) ListDirectories(ctx context.Context, in *emptypb.Empty) (*apapproto.ListDirectoriesResponse, error) {
	stateDir, stateErr := userdirs.StateDir()
	defaultDataDir, dataErr := userdirs.DefaultDataDir(terminology.GetDaemonDirName())

	return &apapproto.ListDirectoriesResponse{
		LogDir:         stateDir,
		DefaultDataDir: defaultDataDir,
	}, errors.Join(stateErr, dataErr)
}
