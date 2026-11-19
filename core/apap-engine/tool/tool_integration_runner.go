// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package tool

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Arm-Debug/apap-cli/apap-engine/capabilities"
	"github.com/Arm-Debug/apap-cli/apap-engine/logging/logx"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

var errToolRunCancelled = errors.New("tool run cancelled")

// timeoutFallbackGracePeriod gives a tool time to terminate after its configured timeout
// or after it reports that collection has finished.
// Declared as var (not const) to enable tests to override it with a shorter duration.
var timeoutFallbackGracePeriod = 10 * time.Second

// drainErrChan drains a CLOSED error channel and folds errors into one using errors.Join.
// If runErr is non-nil, it's included.
func drainErrChan(runErr error, errCh <-chan error) error {
	finalErr := runErr
	for err := range errCh {
		if err == nil {
			continue
		}
		if finalErr == nil {
			finalErr = err
		} else {
			finalErr = errors.Join(finalErr, err)
		}
	}
	return finalErr
}

// RunToolIntegration runs a single tool integration, waits for it to complete,
// and handles stop and cancel requests.
//
// Integrations that report collection completion get timeoutFallbackGracePeriod to
// terminate. Other integrations get the configured timeout plus that grace period.
func RunToolIntegration(ctx context.Context, stopCh, cancelCh <-chan struct{}, timeout time.Duration, tr ToolIntegration) error {
	select {
	case <-cancelCh:
		props := tr.Properties()
		logx.FromContext(ctx).
			WithField("name", props.Name).
			WithField("version", props.Version).
			Info("Cancelling tool")
		return errToolRunCancelled
	default:
	}

	var wg sync.WaitGroup
	// We may record up to 2 errors: one from Stop, one from Cancel.
	errCh := make(chan error, 2)
	runDone := make(chan struct{})

	closure, err := tr.StartRuntime()
	if err != nil {
		return fmt.Errorf("failed to start tool runtime: %w", err)
	}
	defer closure()

	props := tr.Properties()

	// Listen for stop requests until run completes or a cancel arrives.
	wg.Add(1)
	go func() {
		defer wg.Done()
		log := logx.FromContext(ctx).
			WithField("name", props.Name).
			WithField("version", props.Version)

		collectionFinishedCh := tr.CollectionFinished()
		var timer *time.Timer
		var timeoutC <-chan time.Time
		if collectionFinishedCh == nil && timeout > 0 {
			timer = time.NewTimer(timeoutFallbackGracePeriod + timeout)
			timeoutC = timer.C
		}

		defer func() {
			if timer != nil {
				timer.Stop()
			}
		}()

		for {
			select {
			case <-collectionFinishedCh:
				collectionFinishedCh = nil
				timer = time.NewTimer(timeoutFallbackGracePeriod)
				timeoutC = timer.C
			case <-stopCh:
				log.Info("Stopping tool")
				errCh <- tr.Stop()
				return
			case <-timeoutC:
				log.Warn("Tool termination timeout elapsed with tool still running; engine fallback stop requested")
				errCh <- tr.Stop()
				return
			case <-runDone:
				return
			case <-cancelCh:
				return
			}
		}
	}()

	// Listen for cancel requests until run completes.
	wg.Add(1)
	go func() {
		defer wg.Done()
		select {
		case <-cancelCh:
			logx.FromContext(ctx).
				WithField("name", props.Name).
				WithField("version", props.Version).
				Info("Cancelling tool")
			// Combine the primary error with any tool error.
			errCh <- errors.Join(errToolRunCancelled, tr.Cancel())
		case <-runDone:
		}
	}()

	runErr := tr.Run()
	close(runDone)

	wg.Wait()
	close(errCh)
	return drainErrChan(runErr, errCh)
}

// RunAndReformatToolIntegrations runs all tools concurrently, waits for them to finish,
// then reformats successful runs. It returns a per-tool error slice (index-aligned with input).
func RunAndReformatToolIntegrations(
	stopCh, cancelCh chan struct{},
	intCtxs []IntegrationContext,
	reg *Registry,
	manifestUpdater *run.RunManifestUpdater,
) []error {
	prepared, errs := PrepareToolIntegrations(intCtxs, reg, cancelCh)
	if errors.Join(errs...) != nil {
		return errs
	}
	return RunAndReformatPreparedToolIntegrations(stopCh, cancelCh, prepared, manifestUpdater)
}

// PreparedToolIntegrations contains integrations which have completed all
// validation, discovery and construction needed before collection starts.
type PreparedToolIntegrations struct {
	contexts     []IntegrationContext
	integrations []ToolIntegration
}

// PrepareToolIntegrations completes all work which can reject an integration
// before collection starts.
func PrepareToolIntegrations(intCtxs []IntegrationContext, reg *Registry, cancelCh <-chan struct{}) (*PreparedToolIntegrations, []error) {
	errs := make([]error, len(intCtxs))
	for i := range intCtxs {
		if err := normalizeCapabilityRequest(&intCtxs[i]); err != nil {
			errs[i] = err
			continue
		}
		if err := validateCapabilityRequest(intCtxs[i], reg); err != nil {
			errs[i] = err
			continue
		}
		if intCtxs[i].Discovery == nil {
			results, probeErrs := ProbeToolDiscovery([]IntegrationContext{intCtxs[i]}, reg, cancelCh)
			if err := errors.Join(probeErrs...); err != nil {
				errs[i] = err
				continue
			}
			intCtxs[i].Discovery = &results[0]
		}
		if err := prepareCounterCollection(&intCtxs[i]); err != nil {
			errs[i] = err
		}
	}
	if errors.Join(errs...) != nil {
		return nil, errs
	}

	toolInts := make([]ToolIntegration, len(intCtxs))
	if ConstructToolIntegrations(intCtxs, reg, errs, toolInts) {
		return nil, errs
	}
	results, errs := checkToolInstances(intCtxs, toolInts, cancelCh, validateTool)
	for i, intCtx := range intCtxs {
		if errs[i] != nil {
			continue
		}
		errs[i] = handleToolAdvice(intCtx.DefaultEngineLocality.Engine, results[i].Advice)
	}
	if errors.Join(errs...) != nil {
		return nil, errs
	}
	return &PreparedToolIntegrations{contexts: intCtxs, integrations: toolInts}, errs
}

func handleToolAdvice(engine Engine, advice []ProbeAdvice) error {
	for _, item := range advice {
		msg := message.New(item.MessageCode).WithMetadata(item.Metadata)
		if item.Cause != "" {
			msg = msg.WithCause(errors.New(item.Cause))
		}
		if item.Level == "error" {
			return msg
		}
		catalogMessage, _ := message.LookupMessage(msg)
		level := item.Level
		if level == "warning" {
			level = "warn"
		}
		engine.WriteUserMessage(level, catalogMessage.Text())
	}
	return nil
}

// RunAndReformatPreparedToolIntegrations launches integrations which have
// already passed PrepareToolIntegrations.
func RunAndReformatPreparedToolIntegrations(
	stopCh, cancelCh chan struct{},
	prepared *PreparedToolIntegrations,
	manifestUpdater *run.RunManifestUpdater,
) []error {
	intCtxs := prepared.contexts
	toolInts := prepared.integrations
	errs := make([]error, len(intCtxs))
	for i := range intCtxs {
		intCtxs[i].counterOutcomes = nil
		if request := intCtxs[i].CapabilityRequest; request != nil && len(request.Required)+len(request.Optional) > 0 {
			intCtxs[i].counterOutcomes = &counterOutcomeSet{
				published: make(map[string]capabilities.CPUCounterCapabilityValue),
				staged:    make(map[string]stagedCounterOutcome),
			}
		}
	}
	// Run all tool instances concurrently
	runWg := sync.WaitGroup{}
	runWg.Add(len(intCtxs))
	for i, toolInstance := range toolInts {
		go func(indx int) {
			errs[indx] = RunToolIntegration(
				intCtxs[indx].Ctx,
				stopCh,
				cancelCh,
				time.Duration(intCtxs[indx].Timeout)*time.Second,
				toolInstance,
			)
			runWg.Done()
		}(i)
	}
	runWg.Wait()
	for i := range intCtxs {
		errs[i] = finishCapabilityFailure(&intCtxs[i], cancelCh, errs[i], capabilities.CPUCounterReasonRecordFailed)
	}

	// Reformat only those that ran without error.
	var refmtWg sync.WaitGroup
	for i, ti := range toolInts {
		if errs[i] != nil {
			continue
		}
		// Explicit requests are registered before resolution so their intent remains
		// readable on failure. Legacy invocations keep the previous success-gated
		// toolsUsed behaviour.
		if intCtxs[i].CapabilityRequest == nil && manifestUpdater != nil {
			if err := manifestUpdater.AddToolOutput(intCtxs[i].Name, intCtxs[i].Version, intCtxs[i].InvocationIndex); err != nil {
				errs[i] = fmt.Errorf("failed to write manifest: %w", err)
				continue
			}
		}

		errs[i] = finishCapabilityFailure(&intCtxs[i], cancelCh, nil, capabilities.CPUCounterReasonReformatFailed)
		if errs[i] != nil {
			continue
		}
		closure, err := ti.StartRuntime()
		if err != nil {
			errs[i] = finishCapabilityFailure(&intCtxs[i], cancelCh,
				fmt.Errorf("failed to start tool runtime: %w", err), capabilities.CPUCounterReasonReformatFailed)
			continue
		}
		defer closure()

		refmtWg.Add(1)
		go func(idx int, tool ToolIntegration) {
			defer refmtWg.Done()
			errs[idx] = finishCapabilityFailure(&intCtxs[idx], cancelCh, tool.Reformat(), capabilities.CPUCounterReasonReformatFailed)
			if errs[idx] != nil {
				return
			}
			errs[idx] = intCtxs[idx].finalizeCounterOutcomes("")
			if errs[idx] == nil {
				errs[idx] = intCtxs[idx].validateCounterOutcomes()
			}
		}(i, ti)
	}
	refmtWg.Wait()

	return errs
}

// finishCapabilityFailure preserves the phase error and fills only missing
// outcomes. Cancellation leaves no new outcome and discards staged successes.
func finishCapabilityFailure(ic *IntegrationContext, cancelCh <-chan struct{}, phaseErr error, reason string) error {
	// Directly constructed integrations may omit the execution context.
	var contextErr error
	if ic.Ctx != nil {
		contextErr = ic.Ctx.Err()
	}
	cancelled := errors.Is(phaseErr, errToolRunCancelled) || errors.Is(phaseErr, context.Canceled) || contextErr != nil
	select {
	case <-cancelCh:
		cancelled = true
	default:
	}
	if cancelled {
		ic.discardStagedCounterOutcomes()
		return errors.Join(phaseErr, errToolRunCancelled, contextErr)
	}
	if phaseErr == nil {
		return nil
	}
	if publishErr := ic.finalizeCounterOutcomes(reason); publishErr != nil {
		return errors.Join(phaseErr, publishErr)
	}
	return phaseErr
}

// ProbeTools discovers capabilities and validates the selected configuration.
func ProbeTools(
	probeRequests []IntegrationContext,
	reg *Registry,
	cancelCh <-chan struct{},
) ([]ProbeResult, []error) {
	results, errs := ProbeToolDiscovery(probeRequests, reg, cancelCh)
	if errors.Join(errs...) != nil {
		return results, errs
	}
	for i := range probeRequests {
		probeRequests[i].Discovery = &results[i]
	}
	return ValidateTools(probeRequests, reg, cancelCh)
}

// ProbeToolDiscovery returns validated discovery with integration advice only.
// Callers caching these results must apply request-specific advice after cloning
// the cached result, using ValidateTools.
func ProbeToolDiscovery(probeRequests []IntegrationContext, reg *Registry, cancelCh <-chan struct{}) ([]ProbeResult, []error) {
	return checkToolIntegrations(probeRequests, reg, cancelCh, func(ti ToolIntegration, ic IntegrationContext) (ProbeResult, error) {
		result, err := ti.Probe()
		if err == nil {
			factory := reg.FindTool(ic.Name, ic.Version)
			optedIn := CapabilityInterfaceVersion(factory) == capabilities.CPUCounterVersion
			required := requiresLogicalDiscovery(ic.CapabilityRequest)
			err = validateCPUDiscovery(result, optedIn, required)
		}
		return result, err
	})
}

// ValidateTools checks current requests without changing the discovery results.
// Each context must contain discovery for the same target and configuration.
func ValidateTools(requests []IntegrationContext, reg *Registry, cancelCh <-chan struct{}) ([]ProbeResult, []error) {
	return checkToolIntegrations(requests, reg, cancelCh, validateTool)
}

func validateTool(ti ToolIntegration, ic IntegrationContext) (ProbeResult, error) {
	if ic.Discovery == nil {
		return ProbeResult{}, fmt.Errorf("tool %s has no discovery for validation", ic.Name)
	}
	result := *ic.Discovery
	result.Advice = append([]ProbeAdvice{}, result.Advice...)
	advice, err := ti.Validate()
	if err != nil {
		return result, err
	}
	result.Advice = append(result.Advice, advice...)
	for _, item := range advice {
		if item.Level == "error" {
			result.Available = false
		}
	}
	err = AddCapabilitySelectionAdvice(&result, ic.CapabilityRequest)
	return result, err
}

// checkToolIntegrations shares runtime and cancellation handling between checks.
func checkToolIntegrations(probeRequests []IntegrationContext, reg *Registry, cancelCh <-chan struct{}, check func(ToolIntegration, IntegrationContext) (ProbeResult, error)) ([]ProbeResult, []error) {
	errs := make([]error, len(probeRequests))
	probeResults := make([]ProbeResult, len(probeRequests))
	for i := range probeRequests {
		if err := normalizeCapabilityRequest(&probeRequests[i]); err != nil {
			errs[i] = err
			continue
		}
		errs[i] = validateCapabilityRequest(probeRequests[i], reg)
	}
	if errors.Join(errs...) != nil {
		return probeResults, errs
	}
	// Keep same length as inputs; nil where creation failed.
	toolInts := make([]ToolIntegration, len(probeRequests))

	if ConstructToolIntegrations(probeRequests, reg, errs, toolInts) {
		return probeResults, errs
	}
	return checkToolInstances(probeRequests, toolInts, cancelCh, check)
}

func checkToolInstances(requests []IntegrationContext, toolInts []ToolIntegration, cancelCh <-chan struct{}, check func(ToolIntegration, IntegrationContext) (ProbeResult, error)) ([]ProbeResult, []error) {
	errs := make([]error, len(requests))
	results := make([]ProbeResult, len(requests))

	// Run all tool checks concurrently.
	checkWg := sync.WaitGroup{}
	checkWg.Add(len(requests))
	for i, toolInstance := range toolInts {
		go func(indx int) {
			defer checkWg.Done()
			select {
			case <-requests[indx].Ctx.Done():
				errs[indx] = requests[indx].Ctx.Err()
				return
			case <-cancelCh:
				errs[indx] = context.Canceled
				return
			default:
			}

			checkComplete := make(chan error, 1)

			// Observe cancellation while the check runs.
			go func() {
				cleanUp, err := toolInstance.StartRuntime()
				if err != nil {
					checkComplete <- err
					return
				}

				results[indx], err = check(toolInstance, requests[indx])
				cleanUp()
				checkComplete <- err
			}()

			// Cancellation must settle the check before its runtime and shared
			// locality resources can be cleaned up by the caller.
			var cancellationErr error
			select {
			case <-requests[indx].Ctx.Done():
				cancellationErr = requests[indx].Ctx.Err()
			case <-cancelCh:
				cancellationErr = context.Canceled
			case err := <-checkComplete:
				errs[indx] = err
				return
			}
			select {
			case err := <-checkComplete:
				errs[indx] = err
				return
			default:
			}
			cancelErr := toolInstance.Cancel()
			checkErr := <-checkComplete
			errs[indx] = cancellationErr
			if cancelErr != nil || checkErr != nil {
				errs[indx] = errors.Join(errs[indx], cancelErr, checkErr)
			}
		}(i)
	}
	checkWg.Wait()

	return results, errs
}

// ConstructToolIntegrations constructs tool integrations to be used for tool probing or running.
func ConstructToolIntegrations(
	integrationContexts []IntegrationContext,
	reg *Registry,
	errs []error,
	toolInts []ToolIntegration) bool {

	errorEncountered := false

	for i := range integrationContexts {
		factory := reg.FindTool(integrationContexts[i].Name, integrationContexts[i].Version)
		if factory == nil {
			errs[i] = fmt.Errorf("tool %s not found in tool registry", integrationContexts[i].Name)
			errorEncountered = true
			continue
		}
		ti, err := factory.NewIntegration(&integrationContexts[i])
		if err != nil {
			errs[i] = fmt.Errorf("failed to create tool integration for %s: %w", integrationContexts[i].Name, err)
			errorEncountered = true
			continue
		}
		toolInts[i] = ti
	}
	return errorEncountered
}

// FindToolIntegration searches for a tool integration by name and version in the provided slice.
// It returns the first matching tool integration or nil if not found.
func FindToolIntegration(toolIntegrations []ToolIntegration, name, version string) ToolIntegration {
	for _, ti := range toolIntegrations {
		props := ti.Properties()
		if props.Name == name && props.Version == version {
			return ti
		}
	}
	return nil
}
