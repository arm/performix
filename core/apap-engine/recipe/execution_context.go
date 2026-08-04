// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipe

import (
	"context"
	"fmt"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/apap-engine/agent"
	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/cmdsync"
	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/locality"
	"github.com/Arm-Debug/apap-cli/apap-engine/logging/logx"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/notifiers"
	"github.com/Arm-Debug/apap-cli/apap-engine/packages"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/targetsession"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool/deployer"
	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

// ExecutionContext is the minimal surface the script-side interface requires.
//
// Keep this list as short as possible; add methods only when new facilities demand it.
type ExecutionContext interface {
	RunCommand(context context.Context, cmdState *cmdsync.CommandState, cmd conductor.RunCommandSpecificType) (conductor.RunCommandOutput, error)
	QueueFileRetrieval(src, dst string, componentType cdf.ComponentType, transferOptions tool.TransferOptions) error
	StoreComponent(dst string, componentType cdf.ComponentType) (string, error)
	ReadHostFile(path string) ([]byte, error)
	LogInfo(context context.Context, msg string)
	LogWarn(context context.Context, msg string)
	WriteUserMessage(context context.Context, level string, message string)
	GetRecipeCtx() *RecipeCtx
	TargetInfo() *target.Description
	GetRunDescriptions() []*run.RunDescription
	GetRunModels() []cdf.ModelView
	GetToolCapabilities(runIndex int, toolName string, invocationIndex int) (run.ToolCapabilities, error)
	GetTool(toolInfo tool.ToolInfo) string
	ToolsDir() string
	RunToolIntegrations(context context.Context, cmdStateCh *cmdsync.CommandStateChannel, intCtxs []tool.IntegrationContext) (func(), []error)
	ProbeToolsFromIntegrations(context context.Context, cmdStateCh *cmdsync.CommandStateChannel, intCtxs []tool.IntegrationContext) ([]tool.ProbeResult, []error)
	IsFullCaptureSupportEnabled() bool
	IsRerenderingEnabled() bool
	IsNeoprofTimelineEnabled() bool
	ToolVersions() map[string]string
}

// RunExecutionContext holds any information required when a run stage is executed.
type RunExecutionContext struct {
	TargetPlatform                TargetPlatformSupplier
	Collector                     *Collector
	RecipeCtx                     *RecipeCtx
	TargetInfoSupplier            TargetDescriptionSupplier
	FileHandler                   conductor.FileHandler
	AgentSupplier                 AgentConnSupplier
	RunDescriptions               []*run.RunDescription
	RunModels                     []cdf.ModelView
	RunCapabilities               []run.RunCapabilities
	PlatformConfigurationSupplier PlatformConfigurationSupplier
	ToolPathsSupplier             ToolDeploymentPathsSupplier
	TargetFilesystemSupplier      TargetFilesystemSupplier
	StageNotifier                 notifiers.StageNotifier
	DeferredActions               *notifiers.DeferredActions
	PackageManager                *packages.PackageManager
	TargetSessions                targetsession.TargetSessionProvider
	RootWorkerEnabled             bool
	FullCaptureSupport            bool
	RerenderingEnabled            bool
	NeoprofTimelineEnabled        bool
	UsrMessageWriter              run.UserMessageWriter
}

func (c *RunExecutionContext) RunCommand(context context.Context, cmdState *cmdsync.CommandState, cmd conductor.RunCommandSpecificType) (conductor.RunCommandOutput, error) {
	return cmd.Execute(context, c.TargetPlatform(), c.RecipeCtx.OutputDir)
}

func (c *RunExecutionContext) QueueFileRetrieval(src, dst string, componentType cdf.ComponentType, transferOptions tool.TransferOptions) error {
	if c.Collector == nil {
		return fmt.Errorf("cannot collect file '%s': no Collector configured for run", src)
	}
	return c.Collector.QueueFileRetrieval(c.TargetPlatform(), c.AgentSupplier, c.RecipeCtx.OutputDir, src, dst, componentType, transferOptions)
}

func (c *RunExecutionContext) StoreComponent(dst string, componentType cdf.ComponentType) (string, error) {
	if c.Collector == nil {
		return "", fmt.Errorf("cannot store component '%s': no Collector configured for run", dst)
	}
	return c.Collector.StoreComponent(dst, componentType)
}

func (c *RunExecutionContext) ReadHostFile(path string) ([]byte, error) {
	return c.FileHandler.ReadHostFile(path)
}

func (c *RunExecutionContext) LogInfo(context context.Context, msg string) {
	logx.FromContext(context).Info(msg)
}

func (c *RunExecutionContext) LogWarn(context context.Context, msg string) {
	logx.FromContext(context).Warn(msg)
}

func (c *RunExecutionContext) WriteUserMessage(context context.Context, level string, message string) {
	loggerWithFields := logx.FromContext(context).WithFields(log.Fields{
		"messageLevel": level,
		"message":      message,
	})
	if c.UsrMessageWriter == nil {
		loggerWithFields.Error("user message requested with no user message writer configured")
		return
	}
	loggerWithFields.Info("writing user message")
	c.UsrMessageWriter.Write(level, message)
}

func (c *RunExecutionContext) GetRecipeCtx() *RecipeCtx {
	return c.RecipeCtx
}

func (c *RunExecutionContext) TargetInfo() *target.Description {
	if c.TargetInfoSupplier == nil {
		return nil
	}

	return c.TargetInfoSupplier()
}

func (c *RunExecutionContext) GetRunDescriptions() []*run.RunDescription {
	return c.RunDescriptions
}

func (c *RunExecutionContext) GetRunModels() []cdf.ModelView {
	return c.RunModels
}

func (c *RunExecutionContext) GetToolCapabilities(runIndex int, toolName string, invocationIndex int) (run.ToolCapabilities, error) {
	if runIndex < 0 || runIndex >= len(c.RunCapabilities) || runIndex >= len(c.RunDescriptions) {
		return nil, fmt.Errorf("GetToolCapabilities: run index %v out of bounds for %v runs", runIndex, len(c.RunCapabilities))
	}

	if c.RunDescriptions[runIndex].ToolsUsed != nil {
		// Verify that the specified tool name and invocation exists
		// Some very old runs may not have `toolsUsed` registered; in this case, we skip this check. Skipping
		// this does not affect correctness; the returned ToolCapabilities struct will still record only the
		// capabilities that are present in the run, we just can't fail early

		// Check if tool name has been migrated
		migratedToolName, wasMigrated, err := cdf.MigrateToolName(toolName, c.RunCapabilities[runIndex].Migrations)
		if err != nil {
			return nil, err
		}
		var toolFound bool
		toolsForRun := c.RunDescriptions[runIndex].ToolsUsed
		for _, toolUsed := range toolsForRun {
			// Check both requested path and migrated path (if exists)
			if (toolUsed.Tool == toolName || (wasMigrated && toolUsed.Tool == migratedToolName)) &&
				toolUsed.Invocation == invocationIndex {
				toolFound = true
				break
			}
		}
		if !toolFound {
			return nil, fmt.Errorf("GetToolCapabilities: tool %q invocation %v not found for run index %v", toolName, invocationIndex, runIndex)
		}
	}

	// Try unmigrated path first
	invocationPath := fmt.Sprintf("tool/%s/%d", toolName, invocationIndex)
	if capabilities, ok := c.RunCapabilities[runIndex].CapabilitiesPerTool[invocationPath]; ok {
		return capabilities, nil
	}

	// Attempt to migrate requested path
	migratedPath, wasMigrated, err := cdf.MigratePath(invocationPath, c.RunCapabilities[runIndex].Migrations)
	if err != nil {
		return nil, err
	}
	if wasMigrated {
		if capabilities, ok := c.RunCapabilities[runIndex].CapabilitiesPerTool[migratedPath]; ok {
			return capabilities, nil
		}
	}

	// We don't error here because a particular tool invocation may not have any capabilities recorded - this
	// isn't an error case
	return run.ToolCapabilities{}, nil
}

func (c *RunExecutionContext) GetTool(toolInfo tool.ToolInfo) string {
	platformConfig := c.PlatformConfigurationSupplier()
	paths := c.toolPaths()
	return deployer.GetToolPath(toolInfo, platformConfig, paths, c.TargetFilesystemSupplier())
}

func (c *RunExecutionContext) ToolsDir() string {
	return c.toolPaths().DeployedToolsDirectory
}

func (c *RunExecutionContext) RunToolIntegrations(ctx context.Context, cmdStateCh *cmdsync.CommandStateChannel, intCtxs []tool.IntegrationContext) (func(), []error) {
	sharedExecCtx, defaultLocality, localityResolver, engineCleanup := c.newEngineLocalities(ctx, c.RecipeCtx.NoCleanupWorkingArea)

	for i := range intCtxs {
		intCtxs[i].Ctx = sharedExecCtx
		oe := c.RecipeCtx.GetNextOutputEntity(intCtxs[i].Name)
		intCtxs[i].OutputEntityDir = oe.RelativePath
		intCtxs[i].DefaultEngineLocality = defaultLocality
		intCtxs[i].ResolveLocality = localityResolver
	}
	monitorStopCh, cleanup := c.setupWorkloadMonitor(sharedExecCtx, engineCleanup, intCtxs)
	tr, err := c.PackageManager.FindToolIntegrations()
	if err != nil {
		// Response expects an error for each integration context, replicate the error for all contexts
		errs := make([]error, len(intCtxs))
		for i := range errs {
			errs[i] = err
		}
		return cleanup, errs
	}

	errs := tool.RunAndReformatToolIntegrations(
		// Merge the monitor stop signal with the user stop request so either one
		// interrupts the integrations.
		mergeStopChannels(sharedExecCtx, cmdStateCh.StopChan, monitorStopCh),
		cmdStateCh.CancelChan,
		intCtxs,
		tr,
		c.Collector.CollectionState.RunManifestUpdater,
	)
	// Ensure the monitor goroutine is cancelled and drained alongside the cleanup provided by the agent.
	return cleanup, errs
}

func (c *RunExecutionContext) IsFullCaptureSupportEnabled() bool {
	return c.FullCaptureSupport
}

func (c *RunExecutionContext) IsRerenderingEnabled() bool {
	return c.RerenderingEnabled
}

func (c *RunExecutionContext) IsNeoprofTimelineEnabled() bool {
	return c.NeoprofTimelineEnabled
}

func (c *RunExecutionContext) ToolVersions() map[string]string {
	return c.RecipeCtx.ToolVersions
}

func (c *RunExecutionContext) toolPaths() deployer.BaseToolDeploymentPaths {
	return c.ToolPathsSupplier()
}

// ProbeToolsFromIntegrations will execute tool probes for all integration contexts received.
// Upon cancellation request the probe will be ended prematurely with an error
func (c *RunExecutionContext) ProbeToolsFromIntegrations(ctx context.Context, cmdStateCh *cmdsync.CommandStateChannel, intCtxs []tool.IntegrationContext) ([]tool.ProbeResult, []error) {
	sharedExecCtx, defaultLocality, localityResolver, cleanup := c.newEngineLocalities(ctx, false)

	for i := range intCtxs {
		intCtxs[i].Ctx = sharedExecCtx
		intCtxs[i].DefaultEngineLocality = defaultLocality
		intCtxs[i].ResolveLocality = localityResolver
	}

	defer cleanup()

	tr, err := c.PackageManager.FindToolIntegrations()
	if err != nil {
		// Response expects an error for each integration context, replicate the error for all contexts
		errs := make([]error, len(intCtxs))
		for i := range errs {
			errs[i] = err
		}
		return nil, errs
	}
	return tool.ProbeTools(
		intCtxs,
		tr,
	)
}

// newEngineLocalities creates the default target locality and a resolver for other localities.
func (c *RunExecutionContext) newEngineLocalities(runCtx context.Context, preserveTemporaryDirs bool) (context.Context, tool.EngineLocality, tool.EngineLocalityResolver, func()) {
	agentConn := c.AgentSupplier()
	targetPlatform := c.TargetPlatform()

	// sharedExecCtx controls the lifetime of active tool integrations.
	// It's bound to the runCtx, so it's cancelled when the run is cancelled.
	// Agent contexts are also attached so that it's cancelled when any locality agent disconnects.
	sharedExecCtx, cancelSharedExec := context.WithCancelCause(runCtx)

	targetLocality, targetCleanup := c.newEngineLocality(
		locality.Target,
		sharedExecCtx,
		cancelSharedExec,
		agentConn,
		targetPlatform,
		c.toolPaths().DeployedToolsDirectory,
		preserveTemporaryDirs,
	)
	localityResolver, cleanup := tool.NewEngineLocalityResolver(
		targetLocality,
		targetCleanup,
		func() (tool.EngineLocality, func(), error) {
			hostSession, err := c.TargetSessions.HostSession()
			if err != nil {
				return tool.EngineLocality{}, nil, err
			}
			if _, err := hostSession.Connect(sharedExecCtx,
				targetsession.ConnectOptions{PlatformGate: conductor.HostSupported}); err != nil {
				return tool.EngineLocality{}, nil, err
			}
			hostPlatform, err := hostSession.TargetPlatform()
			if err != nil {
				return tool.EngineLocality{}, nil, err
			}
			agentConn, err := hostSession.TargetAgent(sharedExecCtx)
			if err != nil {
				return tool.EngineLocality{}, nil, err
			}

			hostLocality, hostCleanup := c.newEngineLocality(
				locality.Host,
				sharedExecCtx,
				cancelSharedExec,
				agentConn,
				hostPlatform,
				hostSession.ResolveToolsDir(),
				preserveTemporaryDirs,
			)
			return hostLocality, hostCleanup, nil
		},
	)

	return sharedExecCtx, targetLocality, localityResolver, func() {
		cancelSharedExec(nil)
		cleanup()
	}
}

// newEngineLocality creates an engine locality bound to sharedExecCtx.
func (c *RunExecutionContext) newEngineLocality(
	localityName string,
	sharedExecCtx context.Context,
	cancelSharedExec context.CancelCauseFunc,
	agentConn *agent.AgentConn,
	targetPlatform *conductor.TargetPlatform,
	toolsRoot string,
	preserveTemporaryDirs bool,
) (tool.EngineLocality, func()) {
	// Bind cleanup to the agent lifetime without inheriting cancellation from
	// the execution context so teardown can still release agent-side resources
	// after the run is cancelled.
	stop := context.AfterFunc(agentConn.AgentContext(), func() {
		cancelSharedExec(context.Cause(agentConn.AgentContext()))
	})
	agentCleanup, cancelAgentCleanup := agentConn.BindContext(context.WithoutCancel(sharedExecCtx))
	engine, engineCleanup := tool.NewAgentEngine(
		sharedExecCtx,
		agentCleanup,
		agentConn.Client,
		c.StageNotifier,
		c.UsrMessageWriter,
		preserveTemporaryDirs,
		c.RootWorkerEnabled,
		targetPlatform.PlatformConfiguration,
	)

	return tool.EngineLocality{
			Name:   localityName,
			Engine: engine,
			FileCollector: NewRecipeFileCollector(
				c.Collector,
				*targetPlatform,
				func() *agent.AgentConn { return agentConn },
			),
			ToolsRoot: toolsRoot,
			CopyFrom: func(sourceLocality string, sourcePath string, destinationPath string) error {
				return c.copyFile(sourceLocality, localityName, sourcePath, destinationPath)
			},
		}, func() {
			engineCleanup()
			cancelAgentCleanup(nil)
			stop()
		}
}

func (c *RunExecutionContext) copyFile(sourceLocality string, destinationLocality string, sourcePath string, destinationPath string) error {
	if sourceLocality != locality.Target {
		return message.New(message.EngineToolCopyFromUnsupportedSourceLocality).WithMetadata(map[string]string{
			"sourceLocality":      sourceLocality,
			"destinationLocality": destinationLocality,
		})
	}
	if destinationLocality != locality.Host {
		return message.New(message.EngineToolCopyFromUnsupportedDestinationLocality).WithMetadata(map[string]string{
			"sourceLocality":      sourceLocality,
			"destinationLocality": destinationLocality,
		})
	}

	retriever, ok := c.Collector.FileRetriever.(*TransferManagerRetriever)
	if !ok {
		return message.New(message.EngineToolCopyFromTransferManagerDisabled)
	}

	sourcePath = c.TargetPlatform().Path.GetFullPath(sourcePath, "")
	return retriever.TransferManager.CopyFromTargetAndWait(conductor.FileTransfer{
		RemotePath: sourcePath,
		LocalPath:  destinationPath,
	}, c.AgentSupplier)
}

// Structure holding the set of workload targets to monitor for liveness.
type workloadMonitorTargets struct {
	pids map[int32]struct{}
}

var workloadMonitorPollInterval = time.Second

// collectMonitorTargets inspects integration workloads and returns the set of PIDs that should be tracked for liveness.
// Returns nil when there is nothing to monitor.
func collectMonitorTargets(intCtxs []tool.IntegrationContext) *workloadMonitorTargets {
	targets := &workloadMonitorTargets{
		pids: make(map[int32]struct{}),
	}

	for i := range intCtxs {
		switch w := intCtxs[i].Workload.(type) {
		case *tool.WorkloadAttach:
			targets.pids[w.PID] = struct{}{}
		}
	}

	if len(targets.pids) == 0 {
		return nil
	}
	return targets
}

// setupWorkloadMonitor inspects the workloads and, if needed, starts the
// background monitor. It returns the stop channel to merge into the tool runner
// along with a cleanup function that handles monitor shutdown.
func (c *RunExecutionContext) setupWorkloadMonitor(ctx context.Context, cleanup func(), intCtxs []tool.IntegrationContext) (<-chan struct{}, func()) {
	targets := collectMonitorTargets(intCtxs)
	if targets == nil {
		noOpCancel := func() {}
		done := closedChannel()
		composed := composeCleanup(cleanup, noOpCancel, done)
		var once sync.Once
		return nil, func() { once.Do(composed) }
	}

	stopCh, cancel, done := startWorkloadMonitor(ctx, c.AgentSupplier().Client, targets)
	composed := composeCleanup(cleanup, cancel, done)
	var once sync.Once
	return stopCh, func() { once.Do(composed) }
}

// startWorkloadMonitor polls the agent for running processes and emits a stop signal when
// every tracked workload (attached PIDs) has exited. The returned channel closes
// to request a graceful stop, while the cancel function and done channel allow callers to
// terminate and wait for the monitor goroutine during cleanup.
func startWorkloadMonitor(ctx context.Context, client targetagentproto.TargetAgentClient, targets *workloadMonitorTargets) (<-chan struct{}, context.CancelFunc, <-chan struct{}) {
	if targets == nil {
		ch := closedChannel()
		noOpCancel := func() {}
		return ch, noOpCancel, ch
	}

	stopCh := make(chan struct{})
	monitorCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	var once sync.Once

	triggerStop := func(reason string) {
		once.Do(func() {
			logx.FromContext(ctx).WithField("reason", reason).Info("Workload monitor stopping tools")
			close(stopCh)
		})
	}

	poll := func() bool {
		procList, err := client.ListProcesses(monitorCtx, &emptypb.Empty{})
		if err != nil {
			logx.FromContext(ctx).WithError(err).Warn("Failed to list processes while monitoring workload")
			return false
		}
		if shouldStopForMonitorTargets(targets, procList.Processes) {
			triggerStop("profiled workload ended")
			return true
		}
		return false
	}

	go func() {
		defer close(done)
		if poll() {
			return
		}

		ticker := time.NewTicker(workloadMonitorPollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-monitorCtx.Done():
				return
			case <-ticker.C:
				if poll() {
					return
				}
			}
		}
	}()

	return stopCh, cancel, done
}

// shouldStopForMonitorTargets checks the current process list against the tracked workloads and
// reports whether the capture should be stopped.
func shouldStopForMonitorTargets(targets *workloadMonitorTargets, processes []*targetagentproto.ProcessInfo) bool {
	if len(targets.pids) == 0 {
		return false
	}

	runningPIDs := make(map[int32]struct{}, len(processes))

	for _, proc := range processes {
		runningPIDs[proc.Pid] = struct{}{}
	}

	// Check attach workloads are still running
	for pid := range targets.pids {
		if _, ok := runningPIDs[pid]; !ok {
			return true
		}
	}

	return false
}

// mergeStopChannels combines the recipe stop channel with the monitor channel so the tool
// runner reacts when either source requests a stop.
func mergeStopChannels(ctx context.Context, primary chan struct{}, secondary <-chan struct{}) chan struct{} {
	if secondary == nil {
		return primary
	}
	merged := make(chan struct{})
	go func() {
		defer close(merged)
		select {
		case <-ctx.Done():
		case <-primary:
		case <-secondary:
		}
	}()
	return merged
}

// composeCleanup returns a cleanup routine that first shuts down the workload monitor
// (if present) before invoking the provided cleanup function.
func composeCleanup(cleanup func(), cancel context.CancelFunc, done <-chan struct{}) func() {
	return func() {
		if cancel != nil {
			cancel()
		}
		if done != nil {
			<-done
		}
		cleanup()
	}
}

// closedChannel returns a channel that has already been closed, useful for optional flows.
func closedChannel() chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}
