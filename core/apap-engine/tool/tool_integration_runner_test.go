// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package tool

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/deploymentsupport"
	"github.com/Arm-Debug/apap-cli/apap-engine/logging/logx"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	run_mocks "github.com/Arm-Debug/apap-cli/apap-engine/run/mocks"
)

/* ---------- Test Doubles ---------- */

type FakeToolInstance struct {
	ctx           *IntegrationContext
	done          chan struct{}
	once          sync.Once
	ProbeCalled   atomic.Bool
	Ran           atomic.Bool
	Reformatted   atomic.Bool
	Stopped       atomic.Bool
	Cancelled     atomic.Bool
	immediateExit bool
	probeError    error
	runError      error
	cancelError   error
	stopError     error
	reformatError error
	running       *sync.WaitGroup // Signalled when Run is called
}

// Option for configuring FakeToolInstance
type FakeToolOption func(*FakeToolInstance)

func NewFakeToolInstance(opts ...FakeToolOption) *FakeToolInstance {
	f := &FakeToolInstance{done: make(chan struct{})}
	for _, opt := range opts {
		opt(f)
	}
	return f
}

/* ---------- Option functions ---------- */

func WithImmediateExit() FakeToolOption {
	return func(f *FakeToolInstance) { f.immediateExit = true }
}

func WithRunError(err error) FakeToolOption {
	return func(f *FakeToolInstance) { f.runError = err }
}

func WithProbeError(err error) FakeToolOption {
	return func(f *FakeToolInstance) { f.probeError = err }
}

func WithCancelError(err error) FakeToolOption {
	return func(f *FakeToolInstance) { f.cancelError = err }
}

func WithStopError(err error) FakeToolOption {
	return func(f *FakeToolInstance) { f.stopError = err }
}

func WithReformatError(err error) FakeToolOption {
	return func(f *FakeToolInstance) { f.reformatError = err }
}

func (f *FakeToolInstance) Properties() IntegrationProperties {
	return IntegrationProperties{
		Name:             "fake-tool",
		Version:          "1.0",
		ShortDescription: "Fake tool for testing",
		LongDescription:  "This tool is a fake implementation for test purposes.",
	}
}

func (f *FakeToolInstance) StartRuntime() (func(), error) {
	return func() {}, nil
}

func (f *FakeToolInstance) Probe() (ProbeResult, error) {
	f.ProbeCalled.Store(true)
	return ProbeResult{}, f.probeError
}

func (f *FakeToolInstance) Run() error {
	f.Ran.Store(true)
	if f.running != nil {
		f.running.Done()
	}

	if f.immediateExit {
		return f.runError
	}

	<-f.done
	return f.runError
}

func (f *FakeToolInstance) Stop() error {
	f.Stopped.Store(true)
	f.once.Do(func() { close(f.done) })
	return f.stopError
}

func (f *FakeToolInstance) Cancel() error {
	f.Cancelled.Store(true)
	f.once.Do(func() { close(f.done) })
	return f.cancelError
}

func (f *FakeToolInstance) Reformat() error {
	f.Reformatted.Store(true)
	return f.reformatError
}

type FakeToolFactory struct {
	createInstance func() *FakeToolInstance
	createError    error
}

func (f *FakeToolFactory) NewIntegration(ctx *IntegrationContext) (ToolIntegration, error) {
	inst := f.createInstance()
	if inst != nil {
		inst.ctx = ctx
	}
	return inst, f.createError
}
func (f *FakeToolFactory) Name() string                                           { return "fake-tool" }
func (f *FakeToolFactory) Version() string                                        { return "1.0" }
func (f *FakeToolFactory) Deployments() []deploymentsupport.DeploymentDeclaration { return nil }
func (f *FakeToolFactory) GetMigrations() []Migration                             { return nil }

func CreateFakeToolFactory(createInstance func() *FakeToolInstance) (*Registry, *[]*FakeToolInstance) {
	tr := NewToolRegistry()
	instances := make([]*FakeToolInstance, 0, 2)
	tr.RegisterTool(&FakeToolFactory{
		createInstance: func() *FakeToolInstance {
			inst := createInstance()
			instances = append(instances, inst)
			return inst
		},
	})
	return tr, &instances
}

func newTwoCtx() []IntegrationContext {
	return []IntegrationContext{
		{Name: "fake-tool", Version: "1.0", Ctx: context.Background()},
		{Name: "fake-tool", Version: "1.0", Ctx: context.Background()},
	}
}

type expectedFlags struct {
	cancelled   bool // Cancel was called
	stopped     bool // Stop was called
	ran         bool // Run was called
	reformatted bool // Reformat was called
}

func assertFlags(t *testing.T, f *FakeToolInstance, flags expectedFlags) {
	t.Helper()
	assert.Equalf(t, flags.cancelled, f.Cancelled.Load(), "cancelled flag was not as expected")
	assert.Equalf(t, flags.stopped, f.Stopped.Load(), "stopped flag was not as expected")
	assert.Equalf(t, flags.ran, f.Ran.Load(), "ran flag was not as expected")
	assert.Equalf(t, flags.reformatted, f.Reformatted.Load(), "reformatted flag was not as expected")
}

type MockFileCollector struct {
	mock.Mock
}

func (m *MockFileCollector) QueueFileRetrieval(outputEntityDir string, targetPath string, destRelativePath string, componentType cdf.ComponentType, transferOptions TransferOptions) error {
	args := m.Called(outputEntityDir, targetPath, destRelativePath, componentType, transferOptions)
	return args.Error(0)
}

func (m *MockFileCollector) AddComponent(outputEntityDir string, componentType cdf.ComponentType, file string) (string, error) {
	args := m.Called(outputEntityDir, componentType, file)
	return args.String(0), args.Error(1)
}

func setupMockFileCollector(contexts []IntegrationContext) {
	mockFC := newMockFileCollector()
	for i := range contexts {
		contexts[i].DefaultEngineLocality.FileCollector = mockFC
	}
}

func newMockFileCollector() *MockFileCollector {
	mockFC := &MockFileCollector{}
	mockFC.On("AddComponent", mock.Anything, mock.Anything, mock.Anything).Return(os.TempDir()+"/version.json", nil)
	mockFC.On("QueueFileRetrieval", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	return mockFC
}

func runSync(tr *Registry) []error {
	twoCtx := newTwoCtx()
	dummyBuilder := &run.RunBuilder{}
	setupMockFileCollector(twoCtx)
	return RunAndReformatToolIntegrations(make(chan struct{}), make(chan struct{}), twoCtx, tr, newTestManifestUpdater(dummyBuilder))
}

func runAsync(tr *Registry, stop, cancel bool, waitToCancel *sync.WaitGroup) []error {
	stopCh := make(chan struct{})
	cancelCh := make(chan struct{})
	done := make(chan []error, 1)
	dummyBuilder := &run.RunBuilder{}

	if stop {
		close(stopCh)
	}

	// Cancel immediately if no wait group supplied
	if cancel && waitToCancel == nil {
		close(cancelCh)
	}

	twoCtx := newTwoCtx()
	setupMockFileCollector(twoCtx)
	go func() {
		done <- RunAndReformatToolIntegrations(stopCh, cancelCh, twoCtx, tr, newTestManifestUpdater(dummyBuilder))
	}()

	// Wait for signal before cancelling
	if cancel && waitToCancel != nil {
		waitToCancel.Wait()
		close(cancelCh)
	}

	return <-done
}

func newTestManifestUpdater(builder *run.RunBuilder) *run.RunManifestUpdater {
	runWriter := &run_mocks.MockRunWriter{}
	runWriter.On("WriteManifest", mock.Anything).Return(nil)
	return run.NewRunManifestUpdater(builder, runWriter)
}

func TestRunTool(t *testing.T) {
	dummyBuilder := &run.RunBuilder{}

	t.Run("Integration creation fails is reflected", func(t *testing.T) {
		tr := NewToolRegistry()
		// Wrap NewIntegration to simulate an error
		tr.RegisterTool(&FakeToolFactory{ // Custom factory that always errors.
			createInstance: func() *FakeToolInstance { return nil },
			createError:    errors.New("creation failed"),
		},
		)

		// Run with one context expecting "fake-tool" but integration creation fails.
		errs := RunAndReformatToolIntegrations(nil, nil, []IntegrationContext{{Name: "fake-tool", Version: "1.0"}}, tr, newTestManifestUpdater(dummyBuilder))

		require.Len(t, errs, 1)
		assert.ErrorContains(t, errs[0], "creation failed")
	})

	t.Run("Tool not found in registry returns error", func(t *testing.T) {
		// Run with a context referring to a non-existent tool.
		errs := RunAndReformatToolIntegrations(
			make(chan struct{}),
			make(chan struct{}),
			[]IntegrationContext{{Name: "missing-tool", Version: "9.9"}},
			NewToolRegistry(), // Empty registry — no tools registered.
			newTestManifestUpdater(dummyBuilder),
		)

		require.Len(t, errs, 1)
		assert.ErrorContains(t, errs[0], "missing-tool")
	})

	t.Run("Run completion for multiple tools triggers reformat", func(t *testing.T) {
		tr, inst := CreateFakeToolFactory(func() *FakeToolInstance { return NewFakeToolInstance(WithImmediateExit()) })

		errs := runSync(tr)
		require.Len(t, errs, 2)
		assert.NoError(t, errs[0])
		assert.NoError(t, errs[1])

		require.Len(t, *inst, 2)
		assertFlags(t, (*inst)[0], expectedFlags{cancelled: false, stopped: false, ran: true, reformatted: true})
		assertFlags(t, (*inst)[1], expectedFlags{cancelled: false, stopped: false, ran: true, reformatted: true})
	})

	t.Run("Run error is propagated; only successful tool reformats", func(t *testing.T) {
		first := true
		tr, inst := CreateFakeToolFactory(func() *FakeToolInstance {
			if first {
				first = false
				return NewFakeToolInstance(WithImmediateExit())
			}
			return NewFakeToolInstance(WithImmediateExit(), WithRunError(errors.New("run failed")))
		})

		errs := runSync(tr)
		require.Len(t, errs, 2)
		assert.NoError(t, errs[0])
		assert.ErrorContains(t, errs[1], "run failed")

		require.Len(t, *inst, 2)
		assertFlags(t, (*inst)[0], expectedFlags{cancelled: false, stopped: false, ran: true, reformatted: true})
		assertFlags(t, (*inst)[1], expectedFlags{cancelled: false, stopped: false, ran: true, reformatted: false})
	})

	t.Run("Cancel request are logged", func(t *testing.T) {
		logger, hook := test.NewNullLogger()
		ctx := logx.CtxWithLogger(context.Background(), logger)

		tr, _ := CreateFakeToolFactory(func() *FakeToolInstance { return NewFakeToolInstance() })

		stopCh := make(chan struct{})
		cancelCh := make(chan struct{})
		done := make(chan []error, 1)

		go func() {
			done <- RunAndReformatToolIntegrations(stopCh, cancelCh, []IntegrationContext{
				{
					Name:                  "fake-tool",
					Version:               "1.0",
					Ctx:                   ctx,
					DefaultEngineLocality: EngineLocality{FileCollector: newMockFileCollector()},
				},
			}, tr, newTestManifestUpdater(dummyBuilder))
		}()
		close(cancelCh)

		// Wait for cancel to be processed
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("Test timed out waiting for RunAndReformatToolIntegrations to complete")
		}

		found := false
		for _, entry := range hook.AllEntries() {
			fmt.Println(entry.Message)
			if entry.Message == "Cancelling tool" &&
				entry.Data["name"] == "fake-tool" &&
				entry.Data["version"] == "1.0" {
				found = true
				break
			}
		}
		assert.True(t, found)
	})

	t.Run("Stop requests are logged", func(t *testing.T) {
		logger, hook := test.NewNullLogger()
		ctx := logx.CtxWithLogger(context.Background(), logger)

		tr, _ := CreateFakeToolFactory(func() *FakeToolInstance { return NewFakeToolInstance() })

		stopCh := make(chan struct{})
		cancelCh := make(chan struct{})
		done := make(chan []error, 1)

		go func() {
			done <- RunAndReformatToolIntegrations(stopCh, cancelCh, []IntegrationContext{
				{
					Name:                  "fake-tool",
					Version:               "1.0",
					Ctx:                   ctx,
					DefaultEngineLocality: EngineLocality{FileCollector: newMockFileCollector()},
				},
			}, tr, newTestManifestUpdater(dummyBuilder))
		}()
		close(stopCh)

		// Wait for stop to be processed
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("Test timed out waiting for RunAndReformatToolIntegrations to complete")
		}

		found := false
		for _, entry := range hook.AllEntries() {
			fmt.Println(entry.Message)
			if entry.Message == "Stopping tool" &&
				entry.Data["name"] == "fake-tool" &&
				entry.Data["version"] == "1.0" {
				found = true
				break
			}
		}
		assert.True(t, found)
	})

	t.Run("Cancel error is propagated and prevents reformat", func(t *testing.T) {
		// Wait group will be signalled when Run is called
		var running sync.WaitGroup
		running.Add(2)
		tr, inst := CreateFakeToolFactory(func() *FakeToolInstance {
			f := NewFakeToolInstance()
			f.running = &running
			return f
		})

		errs := runAsync(tr, false, true, &running)
		require.Len(t, errs, 2)
		assert.ErrorIs(t, errs[0], errToolRunCancelled)
		assert.ErrorIs(t, errs[1], errToolRunCancelled)

		require.Len(t, *inst, 2)
		assertFlags(t, (*inst)[0], expectedFlags{cancelled: true, stopped: false, ran: true, reformatted: false})
		assertFlags(t, (*inst)[1], expectedFlags{cancelled: true, stopped: false, ran: true, reformatted: false})
	})

	t.Run("Cancel request is pending so tools do not run", func(t *testing.T) {
		tr, inst := CreateFakeToolFactory(func() *FakeToolInstance {
			return NewFakeToolInstance()
		})

		errs := runAsync(tr, false, true, nil)
		require.Len(t, errs, 2)
		assert.ErrorIs(t, errs[0], errToolRunCancelled)
		assert.ErrorIs(t, errs[1], errToolRunCancelled)

		require.Len(t, *inst, 2)
		assertFlags(t, (*inst)[0], expectedFlags{cancelled: false, stopped: false, ran: false, reformatted: false})
		assertFlags(t, (*inst)[1], expectedFlags{cancelled: false, stopped: false, ran: false, reformatted: false})
	})

	t.Run("Stop request stops tool driver; reformat called", func(t *testing.T) {
		tr, inst := CreateFakeToolFactory(func() *FakeToolInstance { return NewFakeToolInstance() })

		errs := runAsync(tr, true, false, nil)
		require.Len(t, errs, 2)
		for _, e := range errs {
			require.NoError(t, e)
		}

		require.Len(t, *inst, 2)
		assertFlags(t, (*inst)[0], expectedFlags{cancelled: false, stopped: true, ran: true, reformatted: true})
		assertFlags(t, (*inst)[1], expectedFlags{cancelled: false, stopped: true, ran: true, reformatted: true})
	})

	t.Run("Stop error is reflected; reformat not attempted", func(t *testing.T) {
		// Both tools return a stop error.
		tr, inst := CreateFakeToolFactory(func() *FakeToolInstance {
			return NewFakeToolInstance(WithStopError(errors.New("stop failed")))
		})

		errs := runAsync(tr, true, false, nil) // trigger stop
		require.Len(t, errs, 2)
		assert.ErrorContains(t, errs[0], "stop failed")
		assert.ErrorContains(t, errs[1], "stop failed")

		require.Len(t, *inst, 2)
		// We expect stop to have been requested and reformat not attempted.
		assertFlags(t, (*inst)[0], expectedFlags{cancelled: false, stopped: true, ran: true, reformatted: false})
		assertFlags(t, (*inst)[1], expectedFlags{cancelled: false, stopped: true, ran: true, reformatted: false})
	})

	t.Run("Reformat error is reflected", func(t *testing.T) {
		first := true
		// First tool: immediate successful completion but reformat fails.
		// Second tool: immediate successful completion and reformat fails as well.
		tr, inst := CreateFakeToolFactory(func() *FakeToolInstance {
			if first {
				first = false
				return NewFakeToolInstance(WithImmediateExit(), WithReformatError(errors.New("reformat failed A")))
			}
			return NewFakeToolInstance(WithImmediateExit(), WithReformatError(errors.New("reformat failed B")))
		})

		errs := runSync(tr)
		require.Len(t, errs, 2)
		assert.ErrorContains(t, errs[0], "reformat failed A")
		assert.ErrorContains(t, errs[1], "reformat failed B")

		require.Len(t, *inst, 2)
		assertFlags(t, (*inst)[0], expectedFlags{cancelled: false, stopped: false, ran: true, reformatted: true})
		assertFlags(t, (*inst)[1], expectedFlags{cancelled: false, stopped: false, ran: true, reformatted: true})
	})
}

func TestProbeTool(t *testing.T) {

	t.Run("Integration creation fails is reflected", func(t *testing.T) {
		tr := NewToolRegistry()
		// Wrap NewIntegration to simulate an error
		tr.RegisterTool(&FakeToolFactory{ // Custom factory that always errors.
			createInstance: func() *FakeToolInstance { return nil },
			createError:    errors.New("creation failed"),
		},
		)

		// Probe with one context expecting "fake-tool" but integration creation fails.
		_, errs := ProbeTools([]IntegrationContext{{Name: "fake-tool", Version: "1.0"}}, tr)

		require.Len(t, errs, 1)
		assert.ErrorContains(t, errs[0], "creation failed")
	})

	t.Run("Tool not found in registry returns error", func(t *testing.T) {
		// Probe with a context referring to a non-existent tool.
		_, errs := ProbeTools(
			[]IntegrationContext{{Name: "missing-tool", Version: "9.9"}},
			NewToolRegistry(), // Empty registry — no tools registered.
		)

		require.Len(t, errs, 1)
		assert.ErrorContains(t, errs[0], "missing-tool")
	})

	t.Run("Probe error is propagated", func(t *testing.T) {
		first := true
		tr, inst := CreateFakeToolFactory(func() *FakeToolInstance {
			if first {
				first = false
				return NewFakeToolInstance(WithImmediateExit())
			}
			return NewFakeToolInstance(WithImmediateExit(), WithProbeError(errors.New("probe failed")))
		})

		twoCtx := newTwoCtx()
		_, errs := ProbeTools(twoCtx, tr)
		require.Len(t, errs, 2)
		assert.NoError(t, errs[0])
		assert.ErrorContains(t, errs[1], "probe failed")

		require.Len(t, *inst, 2)
		assert.True(t, (*inst)[0].ProbeCalled.Load())
	})
}

// TestTimeoutFallback exercises the engine-level graceful timeout fallback added to
// RunToolIntegration. Tests call RunToolIntegration directly so that timeout and grace
// period durations can be set to sub-millisecond values, keeping the suite fast.
func TestTimeoutFallback(t *testing.T) {
	// Override the package-level grace period for the duration of this test function.
	// Restore the original value when done.
	originalGracePeriod := timeoutFallbackGracePeriod
	t.Cleanup(func() { timeoutFallbackGracePeriod = originalGracePeriod })

	t.Run("Fallback stop is requested when tool hangs past timeout", func(t *testing.T) {
		timeoutFallbackGracePeriod = time.Millisecond

		inst := NewFakeToolInstance() // blocks in Run() until Stop/Cancel
		stopCh := make(chan struct{})
		cancelCh := make(chan struct{})

		err := RunToolIntegration(context.Background(), stopCh, cancelCh, time.Millisecond, inst)

		assert.NoError(t, err)
		assertFlags(t, inst, expectedFlags{ran: true, stopped: true})
	})

	t.Run("No stop called when tool completes naturally before timeout", func(t *testing.T) {
		inst := NewFakeToolInstance(WithImmediateExit())
		stopCh := make(chan struct{})
		cancelCh := make(chan struct{})

		// Long timeout so the fallback timer never fires during the test.
		err := RunToolIntegration(context.Background(), stopCh, cancelCh, time.Hour, inst)

		assert.NoError(t, err)
		assertFlags(t, inst, expectedFlags{ran: true})
	})

	t.Run("Zero timeout; no fallback timer started; tool stops via stop channel", func(t *testing.T) {
		inst := NewFakeToolInstance()
		stopCh := make(chan struct{})
		cancelCh := make(chan struct{})
		close(stopCh)

		// timeout=0: the else-branch of the stop goroutine is used and no timer is created.
		err := RunToolIntegration(context.Background(), stopCh, cancelCh, 0, inst)

		assert.NoError(t, err)
		assertFlags(t, inst, expectedFlags{ran: true, stopped: true})
	})

	t.Run("User stop arrives before timeout; fallback never fires", func(t *testing.T) {
		inst := NewFakeToolInstance()
		stopCh := make(chan struct{})
		cancelCh := make(chan struct{})
		close(stopCh) // pre-close: user stop arrives immediately

		// Long timeout, but stop is requested immediately, so the fallback timer should never fire.
		err := RunToolIntegration(context.Background(), stopCh, cancelCh, time.Hour, inst)

		assert.NoError(t, err)
		assertFlags(t, inst, expectedFlags{ran: true, stopped: true})
	})
}
