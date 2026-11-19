// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package producttelemetry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/Arm-Debug/apap-cli/apap-engine/clientcontext"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/atperf-agent/systeminfo"
)

const queueCapacity = 128

// Host metadata is collected once by the engine, independently of its clients.
var hostOS = sync.OnceValue(func() string {
	description, err := systeminfo.NewSystemInfo().GetOSDescription()
	if err != nil || strings.TrimSpace(description) == "" {
		return runtime.GOOS
	}
	return strings.TrimSpace(description)
})

// Event contains only event-specific data. Common metadata comes from the RPC context.
type Event struct {
	Name       string
	Properties map[string]any
}

// EnvelopeSender sends a complete Application Insights envelope. Tests supply a local sink.
type EnvelopeSender func(ctx context.Context, payload []byte) error

type queuedEvent struct {
	payload  []byte
	feedback bool
}

// Service applies consent and identity policy to both internal and gRPC submissions.
// Accepted events are best effort: they are held in memory, not persisted to disk.
type Service struct {
	directory string
	enabled   func() bool
	send      EnvelopeSender
	queue     chan queuedEvent
	done      chan struct{}
	cancel    context.CancelFunc
	mu        sync.Mutex
	closed    bool
}

func NewService(directory string, enabled func() bool, send EnvelopeSender) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{
		directory: directory, enabled: enabled, send: send,
		queue: make(chan queuedEvent, queueCapacity), done: make(chan struct{}), cancel: cancel,
	}
	go s.processQueue(ctx)
	return s
}

// SendTelemetryEvent queues an event when telemetry is enabled.
func (s *Service) SendTelemetryEvent(ctx context.Context, event Event) error {
	if !s.enabled() {
		return nil
	}
	return s.enqueue(ctx, event, false)
}

// SendFeedbackEvent queues recognised feedback independently of the telemetry preference.
func (s *Service) SendFeedbackEvent(ctx context.Context, event Event) error {
	switch event.Name {
	case "feedback.after-run", "feedback.insight":
		return s.enqueue(ctx, event, true)
	default:
		return message.New(message.EngineProductTelemetryEventInvalid).WithCause(errors.New("unrecognised feedback event"))
	}
}

func (s *Service) enqueue(ctx context.Context, event Event, feedback bool) error {
	prepared, err := s.prepareEvent(ctx, event)
	if err != nil {
		return err
	}
	payload, err := envelope(prepared)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return message.New(message.EngineProductTelemetryEventSubmissionFailed).WithCause(errors.New("telemetry service closed"))
	}
	select {
	case s.queue <- queuedEvent{payload: payload, feedback: feedback}:
		return nil
	default:
		return message.New(message.EngineProductTelemetryEventSubmissionFailed).WithCause(errors.New("telemetry queue full"))
	}
}

func (s *Service) processQueue(ctx context.Context) {
	defer close(s.done)
	for event := range s.queue {
		if ctx.Err() != nil {
			return
		}
		if err := s.sendWithRetry(ctx, event); err != nil {
			// Do not emit another telemetry event or log feedback contents on failure.
			log.WithError(err).Debug("Product event send failed")
		}
	}
}

func (s *Service) sendWithRetry(ctx context.Context, event queuedEvent) error {
	var err error
	for attempt := range 3 {
		if attempt > 0 {
			timer := time.NewTimer(time.Duration(attempt) * time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
		// Consent applies to queued events and every retry, before the shared sender.
		if !event.feedback && !s.enabled() {
			return nil
		}
		err = s.send(ctx, event.payload)
		var transient retryableError
		if !errors.As(err, &transient) {
			return err
		}
	}
	return err
}

// Close stops accepting events and drains the queue until the deadline. A timeout
// cancels the active HTTP request and discards remaining events.
func (s *Service) Close(ctx context.Context) error {
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		close(s.queue)
	}
	s.mu.Unlock()
	defer s.cancel()
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		s.cancel()
		<-s.done
		return ctx.Err()
	}
}

type preparedEvent struct {
	name          string
	userID        string
	clientContext clientcontext.Info
	properties    map[string]string
	measurements  map[string]float64
}

// prepareEvent validates and sanitises client input, then adds engine-owned metadata.
func (s *Service) prepareEvent(ctx context.Context, event Event) (preparedEvent, error) {
	invalid := func(err error) (preparedEvent, error) {
		return preparedEvent{}, message.New(message.EngineProductTelemetryEventInvalid).WithCause(err)
	}
	if strings.TrimSpace(event.Name) == "" {
		return invalid(errors.New("missing event name"))
	}
	info, ok := clientcontext.FromContext(ctx)
	if !ok {
		return invalid(errors.New("missing client context"))
	}
	// Marshal once to accept ordinary Go numeric types as well as protobuf Struct
	// values. Match trackEvent(name, properties): numbers are measurements, while
	// booleans, objects and arrays are JSON strings in custom dimensions.
	encoded, err := json.Marshal(event.Properties)
	if err != nil {
		return invalid(err)
	}
	var values map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &values); err != nil {
		return invalid(err)
	}
	properties := make(map[string]string)
	measurements := make(map[string]float64)
	for key, value := range values {
		if len(key) > 150 || strings.TrimSpace(key) != key {
			return invalid(fmt.Errorf("invalid property name %q", key))
		}
		var number float64
		if string(value) != "null" && json.Unmarshal(value, &number) == nil {
			measurements[key] = number
		} else {
			text := string(value)
			if value[0] == '"' {
				_ = json.Unmarshal(value, &text)
			}
			properties[key] = truncate(strings.TrimSpace(text), 8192)
		}
	}
	// These dimensions are owned centrally and should not be set by a client, so we delete them here before they are set by the engine.
	for _, key := range []string{"client", "emissionSource", "userIdSource", "hostArch", "isArmInternal", "isCoreDev", "version", "runtime", "updateChannel", "legacyGuiUUID"} {
		delete(properties, key)
		delete(measurements, key)
	}
	for key, value := range map[string]string{
		"version": info.Version, "runtime": info.Runtime,
		"updateChannel": info.UpdateChannel, "legacyGuiUUID": info.LegacyGUIUUID,
	} {
		if value != "" {
			properties[key] = value
		}
	}
	id, err := GetOrCreateUserID(s.directory)
	if err != nil {
		return preparedEvent{}, message.New(message.EngineProductTelemetryIdentityAccessFailed).WithCause(err)
	}
	armInternal, coreDev := Classify(s.directory, time.Now())
	properties["client"] = string(info.Client)
	properties["emissionSource"] = "engine"
	properties["userIdSource"] = "engine"
	properties["hostArch"] = runtime.GOARCH
	properties["isArmInternal"] = fmt.Sprint(armInternal)
	properties["isCoreDev"] = fmt.Sprint(coreDev)
	return preparedEvent{
		name: truncate(strings.TrimSpace(event.Name), 1024), userID: id,
		clientContext: info,
		properties:    properties, measurements: measurements,
	}, nil
}

type eventEnvelope struct {
	Name string            `json:"name"`
	Time string            `json:"time"`
	IKey string            `json:"iKey"`
	Tags map[string]string `json:"tags"`
	Data eventData         `json:"data"`
}

type eventData struct {
	BaseType string        `json:"baseType"`
	BaseData eventBaseData `json:"baseData"`
}

type eventBaseData struct {
	Version      int                `json:"ver"`
	Name         string             `json:"name"`
	Properties   map[string]string  `json:"properties"`
	Measurements map[string]float64 `json:"measurements"`
}

// envelope encodes a prepared event in the Application Insights ingestion format.
func envelope(event preparedEvent) ([]byte, error) {
	tags := map[string]string{"ai.user.id": event.userID, "ai.device.osVersion": hostOS()}
	if event.clientContext.Client == clientcontext.GUI {
		tags["ai.device.type"] = "Browser"
	}
	for key, value := range map[string]string{
		"ai.operation.id":       event.clientContext.OperationID,
		"ai.operation.name":     event.clientContext.OperationName,
		"ai.operation.parentId": event.clientContext.OperationParentID,
	} {
		if value != "" {
			tags[key] = value
		}
	}

	payload, err := json.Marshal(eventEnvelope{
		Name: "Microsoft.ApplicationInsights." + strings.ReplaceAll(instrumentationKey, "-", "") + ".Event",
		Time: time.Now().UTC().Format(time.RFC3339Nano), IKey: instrumentationKey,
		Tags: tags,
		Data: eventData{BaseType: "EventData", BaseData: eventBaseData{
			Version: 2, Name: event.name, Properties: event.properties, Measurements: event.measurements,
		}},
	})
	if err != nil {
		return nil, message.New(message.EngineProductTelemetryEventInvalid).WithCause(err)
	}
	if len(payload) > 64*1024 {
		return nil, message.New(message.EngineProductTelemetryEventInvalid).WithCause(errors.New("event exceeds 64 KiB"))
	}
	return payload, nil
}

func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return value
}
