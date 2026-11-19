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
	for _, id := range request.Required {
		if _, supported := capabilities.LookupDefinition(id); !supported && !capabilities.IsNativeCPUCounterID(id) {
			return invalidCounterCollectionError(fmt.Errorf("unsupported capability %q", id))
		}
	}
	for _, id := range request.Optional {
		if _, supported := capabilities.LookupDefinition(id); !supported {
			return invalidCounterCollectionError(fmt.Errorf("unsupported optional capability %q", id))
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

func prepareCounterCollection(ic *IntegrationContext) error {
	if ic.CapabilityRequest == nil {
		return nil
	}
	request := ic.CapabilityRequest
	if !ic.Discovery.Available {
		for _, advice := range ic.Discovery.Advice {
			if advice.Level != "error" {
				continue
			}
			msg := message.New(message.MessageCode(advice.MessageCode))
			if msg != nil {
				return msg.WithMetadata(advice.Metadata).WithCause(errors.New(advice.Cause))
			}
		}
		return invalidCounterCollectionError(fmt.Errorf("tool %s is not ready", ic.Name))
	}
	if err := validateCPUDiscovery(*ic.Discovery, true, requiresLogicalDiscovery(request)); err != nil {
		return err
	}
	for _, id := range request.Required {
		capability, err := capabilities.ParseCPUCounterCapabilityValue(id, ic.Discovery.Capabilities[id], false)
		if err != nil {
			return invalidCounterCollectionError(err)
		}
		if capability.State != capabilities.CPUCounterStateAvailable {
			return message.New(message.EngineToolRequiredCapabilityUnavailable).
				WithMetadata(map[string]string{"capability": id})
		}
	}
	return nil
}

func requiresLogicalDiscovery(request *capabilities.CapabilityRequest) bool {
	// Request validation allows optional selections only for logical counters.
	if request == nil || len(request.Optional) > 0 {
		return true
	}
	for _, id := range request.Required {
		if _, logical := capabilities.LookupDefinition(id); logical {
			return true
		}
	}
	return false
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
	native := make(map[string]capabilities.NativeCPUCounterValue)
	for id, value := range result.Capabilities {
		raw, err := json.Marshal(value)
		if err != nil {
			return invalidCounterCollectionError(err)
		}
		var descriptor struct {
			ComponentType struct {
				Name string `json:"name"`
			} `json:"componentType"`
		}
		if json.Unmarshal(raw, &descriptor) != nil || descriptor.ComponentType.Name != capabilities.NativeCPUCounter {
			continue
		}
		parsed, err := capabilities.ParseNativeCPUCounterValue(id, value, false)
		if err != nil {
			return invalidCounterCollectionError(err)
		}
		native[id] = parsed
	}
	if err := capabilities.ValidateNativeCPUCounterDiscovery(native); err != nil {
		return invalidCounterCollectionError(err)
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
				return invalidCounterCollectionError(fmt.Errorf("counter discovery did not return %s", id))
			}
			capability, err := capabilities.ParseCPUCounterCapabilityValue(id, value, false)
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
	published map[string]capabilities.CPUCounterCapabilityValue
	staged    map[string]stagedCounterOutcome
}

type stagedCounterOutcome struct {
	value   capabilities.CPUCounterCapabilityValue
	publish func(capabilities.CPUCounterCapabilityValue) error
}

// StageCPUCounterOutcome keeps a collected outcome private until the complete
// record and reformat lifecycle has succeeded.
func (ic *IntegrationContext) StageCPUCounterOutcome(id string, value capabilities.CPUCounterCapabilityValue, publish func(capabilities.CPUCounterCapabilityValue) error) bool {
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
			value = value.Incomplete(failureReason)
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
			value, err := capabilities.ParseCPUCounterCapabilityValue(id, ic.Discovery.Capabilities[id], false)
			if err == nil {
				value = value.Incomplete(failureReason)
				var contents []byte
				contents, err = value.MarshalOutcome()
				if err == nil {
					loc := ic.DefaultEngineLocality
					err = PublishJSONComponent(loc.Engine, loc.CapabilityPublisher, ic.OutputEntityDir,
						cdf.ComponentType{Name: value.ComponentType.Name, SchemaVersion: value.ComponentType.Version},
						filepath.Join("capabilities", id+".json"), string(contents))
				}
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
func (ic *IntegrationContext) RecordCPUCounterOutcome(id string, value capabilities.CPUCounterCapabilityValue) {
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
			return invalidCounterCollectionError(fmt.Errorf("capability %s finished with %s", id, outcome.Reason()))
		}
	}
	for _, id := range ic.CapabilityRequest.Required {
		if outcome := ic.counterOutcomes.published[id]; !outcome.SatisfiesRequiredSelection() {
			return invalidCounterCollectionError(fmt.Errorf("required capability %s finished with %s", id, outcome.State))
		}
	}
	return nil
}
