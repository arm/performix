// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package tool

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/Arm-Debug/apap-cli/apap-engine/capabilities"
	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
)

func invalidCounterCollectionError(err error) error {
	return message.New(message.EngineToolCounterCollectionInvalid).WithCause(err)
}

func normalizeCapabilityRequest(ic *IntegrationContext) error {
	if ic.CapabilityRequest == nil {
		return nil
	}
	request, err := capabilities.ParseCapabilityRequest(ic.CapabilityRequest)
	if err != nil {
		return invalidCounterCollectionError(err)
	}
	ic.CapabilityRequest = request
	return nil
}

// PersistCounterRequest records normalized intent immediately after invocation allocation.
func PersistCounterRequest(ic *IntegrationContext) error {
	if ic.CapabilityRequest == nil {
		return nil
	}
	request, err := capabilities.ParseCapabilityRequest(ic.CapabilityRequest)
	if err != nil {
		return invalidCounterCollectionError(err)
	}
	ic.CapabilityRequest = request
	data, err := json.Marshal(request)
	if err != nil {
		return err
	}
	loc := ic.DefaultEngineLocality
	return PublishJSONComponent(loc.Engine, loc.CapabilityPublisher, ic.OutputEntityDir,
		cdf.ComponentType{Name: capabilities.CounterCollectionRequestType, SchemaVersion: capabilities.CPUCounterVersion},
		filepath.Join("requests", "counter_collection.json"), string(data))
}

// PublishJSONComponent closes the complete JSON file before publishing its manifest entry.
func PublishJSONComponent(engine Engine, publisher CapabilityPublisher, entity string, typ cdf.ComponentType, relative, contents string) error {
	return publisher.PublishCapabilityComponent(entity, typ, relative, func(path string) error {
		handle, err := engine.CreateRunFile(path)
		if err != nil {
			return err
		}
		return errors.Join(handle.Append(contents+"\n"), handle.Close())
	})
}

// validateCapabilityRequest rejects explicit requests before an integration is
// constructed. Omitted requests retain the integration's default behaviour.
func validateCapabilityRequest(ic IntegrationContext, reg *Registry) error {
	if ic.CapabilityRequest == nil {
		return nil
	}
	request := ic.CapabilityRequest
	for _, id := range append(append([]string{}, request.Required...), request.Optional...) {
		if _, supported := capabilities.LookupDefinition(id); !supported {
			return invalidCounterCollectionError(fmt.Errorf("unsupported capability %q", id))
		}
	}
	factory := reg.FindTool(ic.Name, ic.Version)
	if factory == nil {
		return invalidCounterCollectionError(fmt.Errorf("tool %s not found", ic.Name))
	}
	if CapabilityInterfaceVersion(factory) != capabilities.CPUCounterVersion {
		return invalidCounterCollectionError(fmt.Errorf("tool %s does not support capability interface version %s", ic.Name, capabilities.CPUCounterVersion))
	}
	return nil
}

func prepareCounterCollection(ic *IntegrationContext, reg *Registry) error {
	if err := validateCapabilityRequest(*ic, reg); err != nil {
		return err
	}
	if ic.CapabilityRequest == nil {
		return nil
	}
	request := ic.CapabilityRequest
	if ic.Discovery == nil {
		probeContext := *ic
		// A non-empty selection needs the full catalogue. An explicit-empty
		// selection keeps its request so the integration can skip enumeration
		// while still performing normal readiness checks.
		if len(request.Required)+len(request.Optional) > 0 {
			probeContext.CapabilityRequest = nil
		}
		results, errs := ProbeTools([]IntegrationContext{probeContext}, reg, nil)
		if err := errors.Join(errs...); err != nil {
			return err
		}
		ic.Discovery = &results[0]
	}
	if !ic.Discovery.Available {
		return invalidCounterCollectionError(fmt.Errorf("tool %s is not ready", ic.Name))
	}
	if err := validateCPUDiscovery(*ic.Discovery, true, len(request.Required)+len(request.Optional) > 0); err != nil {
		return err
	}
	for _, id := range request.Required {
		capability, err := capabilities.ParseCPUCounterValue(id, ic.Discovery.Capabilities[id], false)
		if err != nil {
			return invalidCounterCollectionError(err)
		}
		if capability.State != capabilities.CPUCounterStateAvailable {
			return message.New(message.EngineToolRequiredCapabilityUnavailable).
				WithMetadata(map[string]string{"capability": id})
		}
	}
	if len(request.Required)+len(request.Optional) == 0 {
		// Readiness is established, but an empty selection has no catalogue to
		// pass into collection and must not trigger integration enumeration.
		ic.Discovery = nil
	}
	return nil
}

func validateCPUDiscovery(result ProbeResult, optedIn, required bool) error {
	if !optedIn {
		return nil
	}
	for id := range capabilities.Definitions() {
		if value, ok := result.Capabilities[id]; ok {
			if _, err := capabilities.ParseCPUCounterValue(id, value, false); err != nil {
				return invalidCounterCollectionError(err)
			}
		} else if required {
			return invalidCounterCollectionError(fmt.Errorf("CPU discovery did not return %s", id))
		}
	}
	return nil
}

// AddCapabilitySelectionAdvice applies selection policy to a caller-owned discovery result.
func AddCapabilitySelectionAdvice(result *ProbeResult, request *capabilities.CapabilityRequest) error {
	if request == nil || !result.Available {
		return nil
	}
	for _, selection := range []struct {
		ids   []string
		level string
		code  message.MessageCode
	}{
		{request.Required, "error", message.EngineToolRequiredCapabilityUnavailable},
		{request.Optional, "warning", message.EngineToolOptionalCapabilityUnavailable},
	} {
		for _, id := range selection.ids {
			value, ok := result.Capabilities[id]
			if !ok {
				continue
			}
			capability, err := capabilities.ParseCPUCounterValue(id, value, false)
			if err != nil {
				return invalidCounterCollectionError(err)
			}
			if capability.State == capabilities.CPUCounterStateUnavailable {
				result.Advice = append(result.Advice, ProbeAdvice{
					Level:       selection.level,
					MessageCode: string(selection.code),
					Metadata:    map[string]string{"capability": id},
				})
			}
		}
	}
	return nil
}

// counterOutcomeSet belongs to one collection invocation and is shared by its localities.
type counterOutcomeSet struct {
	mu        sync.Mutex
	published map[string]capabilities.CPUCounterValue
	staged    map[string]stagedCounterOutcome
}

type stagedCounterOutcome struct {
	value   capabilities.CPUCounterValue
	publish func(capabilities.CPUCounterValue) error
}

// StageCPUCounterOutcome keeps a collected outcome private until the complete
// record and reformat lifecycle has succeeded.
func (ic *IntegrationContext) StageCPUCounterOutcome(id string, value capabilities.CPUCounterValue, publish func(capabilities.CPUCounterValue) error) bool {
	if ic.counterOutcomes == nil {
		return false
	}
	ic.counterOutcomes.mu.Lock()
	defer ic.counterOutcomes.mu.Unlock()
	ic.counterOutcomes.staged[id] = stagedCounterOutcome{value: value, publish: publish}
	return true
}

// finalizeCounterOutcomes publishes staged results and, on failure only, fills
// missing requested outcomes. Successful invocations must supply their own results.
func (ic *IntegrationContext) finalizeCounterOutcomes(failureReason string) error {
	if ic.counterOutcomes == nil {
		return nil
	}
	ic.counterOutcomes.mu.Lock()
	defer ic.counterOutcomes.mu.Unlock()
	var errs []error
	for id, staged := range ic.counterOutcomes.staged {
		value := staged.value
		if failureReason != "" {
			value.State = capabilities.CPUCounterStateIncomplete
			value.Payload.Reason = &failureReason
		}
		if err := staged.publish(value); err != nil {
			errs = append(errs, err)
			continue
		}
		ic.counterOutcomes.published[id] = value
		delete(ic.counterOutcomes.staged, id)
	}
	if failureReason != "" {
		for _, id := range append(append([]string{}, ic.CapabilityRequest.Required...), ic.CapabilityRequest.Optional...) {
			if _, published := ic.counterOutcomes.published[id]; published {
				continue
			}
			if _, staged := ic.counterOutcomes.staged[id]; staged {
				// A failed publication must retain its original outcome for retry.
				continue
			}
			definition, _ := capabilities.LookupDefinition(id) // Validated during preparation.
			value := capabilities.CPUCounterValue{
				ComponentType: definition.ComponentType,
				State:         capabilities.CPUCounterStateIncomplete,
				Payload: capabilities.CPUCounterPayload{
					Title: definition.Title, Units: definition.Units,
					Bindings: []capabilities.CPUCounterBinding{}, Reason: &failureReason,
				},
			}
			contents, err := json.Marshal(struct {
				State   string                         `json:"state"`
				Payload capabilities.CPUCounterPayload `json:"payload"`
			}{value.State, value.Payload})
			if err == nil {
				loc := ic.DefaultEngineLocality
				err = PublishJSONComponent(loc.Engine, loc.CapabilityPublisher, ic.OutputEntityDir,
					cdf.ComponentType{Name: definition.ComponentType.Name, SchemaVersion: definition.ComponentType.Version},
					filepath.Join("capabilities", id+".json"), string(contents))
			}
			if err != nil {
				errs = append(errs, err)
				continue
			}
			ic.counterOutcomes.published[id] = value
		}
	}
	return errors.Join(errs...)
}

func (ic *IntegrationContext) discardStagedCounterOutcomes() {
	if ic.counterOutcomes == nil {
		return
	}
	ic.counterOutcomes.mu.Lock()
	defer ic.counterOutcomes.mu.Unlock()
	clear(ic.counterOutcomes.staged)
}

// RecordCPUCounterOutcome records a validated outcome only after publication succeeds.
func (ic *IntegrationContext) RecordCPUCounterOutcome(id string, value capabilities.CPUCounterValue) {
	// Probes and directly constructed integrations do not have a collection tracker.
	if ic.counterOutcomes == nil {
		return
	}
	ic.counterOutcomes.mu.Lock()
	defer ic.counterOutcomes.mu.Unlock()
	ic.counterOutcomes.published[id] = value
}

func (ic *IntegrationContext) validateCounterOutcomes() error {
	if ic.counterOutcomes == nil {
		return nil
	}
	ic.counterOutcomes.mu.Lock()
	defer ic.counterOutcomes.mu.Unlock()
	for _, id := range append(append([]string{}, ic.CapabilityRequest.Required...), ic.CapabilityRequest.Optional...) {
		outcome, ok := ic.counterOutcomes.published[id]
		if !ok {
			// Optional capabilities may lack data, but still need a final outcome.
			// Unsupported counters must publish unavailable; omitting the outcome
			// leaves the result unknown and violates the integration contract.
			return invalidCounterCollectionError(fmt.Errorf("successful tool invocation omitted final outcome for %s", id))
		}
		if outcome.FailsCollectionRegardlessOfPolicy() {
			return invalidCounterCollectionError(fmt.Errorf("capability %s finished with %s", id, *outcome.Payload.Reason))
		}
	}
	for _, id := range ic.CapabilityRequest.Required {
		if outcome := ic.counterOutcomes.published[id]; !outcome.SatisfiesRequiredSelection() {
			return invalidCounterCollectionError(fmt.Errorf("required capability %s finished with %s", id, outcome.State))
		}
	}
	return nil
}
