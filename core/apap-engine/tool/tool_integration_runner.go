// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package tool

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Arm-Debug/apap-cli/apap-engine/logging/logx"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

var errToolRunCancelled = errors.New("tool run cancelled")

// timeoutFallbackGracePeriod is added on top of the configured collection timeout before
// the engine requests a graceful stop. This accounts for collector startup overhead and
// ensures the tool has had a fair chance to self-terminate via its own timeout mechanism.
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
// If timeout is non-zero, the engine will request a graceful stop after
// (timeout + timeoutFallbackGracePeriod) has elapsed without the tool completing.
// This is a fallback for collectors that fail to self-terminate on their own timeout.
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
	// When a timeout is configured, a fallback timer is also armed: if the tool has not
	// completed by (timeout + timeoutFallbackGracePeriod) the engine requests a graceful
	// stop using the same mechanism as the GUI Stop button.
	//
	// timeoutC is nil when no timeout is configured. In Go, a receive on a nil channel
	// blocks forever and is never selected, so the timer case below is simply inert when
	// there is no timeout — no separate code path is needed.
	var timeoutC <-chan time.Time
	if timeout > 0 {
		timer := time.NewTimer(timeout + timeoutFallbackGracePeriod)
		defer timer.Stop()
		timeoutC = timer.C
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		log := logx.FromContext(ctx).
			WithField("name", props.Name).
			WithField("version", props.Version)
		select {
		case <-stopCh:
			log.Info("Stopping tool")
		case <-timeoutC:
			log.Warn("Collection timeout elapsed with tool still running; engine fallback stop requested")
		case <-runDone:
			return
		case <-cancelCh:
			return
		}
		errCh <- tr.Stop()
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
	errs := make([]error, len(intCtxs))
	// Keep same length as inputs; nil where creation failed.
	toolInts := make([]ToolIntegration, len(intCtxs))

	if ConstructToolIntegrations(intCtxs, reg, errs, toolInts) {
		return errs
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

	// Reformat only those that ran without error.
	var refmtWg sync.WaitGroup
	for i, ti := range toolInts {
		if errs[i] != nil {
			continue
		}

		// Add to the manifest the tool integration's name and version (hardcode invocation to 0)
		if err := manifestUpdater.AddToolOutput(intCtxs[i].Name, intCtxs[i].Version, 0); err != nil {
			errs[i] = fmt.Errorf("failed to write manifest: %w", err)
			continue
		}

		closure, err := ti.StartRuntime()
		if err != nil {
			errs[i] = fmt.Errorf("failed to start tool runtime: %w", err)
		}
		defer closure()

		refmtWg.Add(1)
		go func(idx int, tool ToolIntegration) {
			defer refmtWg.Done()
			errs[idx] = tool.Reformat()
		}(i, ti)
	}
	refmtWg.Wait()

	return errs
}

// ProbeTools runs the Probe method of all provided tool integrations concurrently.
// It returns a slice of ProbeResult and a slice of errors (index-aligned with input).
// Each probe result represents the capabilities of the corresponding tool integration.
func ProbeTools(
	probeRequests []IntegrationContext,
	reg *Registry,
) ([]ProbeResult, []error) {
	errs := make([]error, len(probeRequests))
	probeResults := make([]ProbeResult, len(probeRequests))
	// Keep same length as inputs; nil where creation failed.
	toolInts := make([]ToolIntegration, len(probeRequests))

	if ConstructToolIntegrations(probeRequests, reg, errs, toolInts) {
		return probeResults, errs
	}

	// Run all tool probes concurrently
	probeWg := sync.WaitGroup{}
	probeWg.Add(len(probeRequests))
	for i, toolInstance := range toolInts {
		go func(indx int) {
			defer probeWg.Done()

			probeComplete := make(chan error, 1)

			// Run tool probe on a goroutine to allow support cancellation observation
			go func() {
				cleanUp, err := toolInstance.StartRuntime()
				if err != nil {
					probeComplete <- err
					return
				}

				defer cleanUp()
				probeResults[indx], err = toolInstance.Probe()
				probeComplete <- err
			}()

			// Finish when the first of cancellation or probe completion occurs
			select {
			case <-probeRequests[indx].Ctx.Done():
				errs[indx] = probeRequests[indx].Ctx.Err()
			case err := <-probeComplete:
				errs[indx] = err
			}
		}(i)
	}
	probeWg.Wait()

	return probeResults, errs
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
