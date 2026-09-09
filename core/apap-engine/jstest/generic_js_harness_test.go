// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package jstest

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/dop251/goja"
	gojarequire "github.com/dop251/goja_nodejs/require"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/gojautils"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

type testRecordRequest struct {
	Label  string `json:"label"`
	Values []int  `json:"values"`
}

type testRecorder struct {
	received testRecordRequest
}

func (r *testRecorder) Record(request testRecordRequest) int {
	r.received = request

	total := 0
	for _, value := range request.Values {
		total += value
	}
	return total
}

func useTestJSFiles(t *testing.T, files map[string]string) string {
	t.Helper()

	directory := t.TempDir()
	for name, source := range files {
		filePath := filepath.Join(directory, name)
		require.NoError(t, os.WriteFile(filePath, []byte(source), 0o600))
	}

	originalResolveJSFilePath := resolveJSFilePath
	resolveJSFilePath = func(_ *testing.T, relativePath string) string {
		return util.CanonicalPath(filepath.Join(directory, relativePath))
	}
	t.Cleanup(func() {
		resolveJSFilePath = originalResolveJSFilePath
	})

	return "entry.js"
}

func loadTestJSModule(t *testing.T, source string) *GenericJSHarness {
	t.Helper()

	modulePath := filepath.Join(t.TempDir(), "entry.js")
	require.NoError(t, os.WriteFile(modulePath, []byte(source), 0o600))

	registry := gojarequire.NewRegistry()
	harness := newGenericJSHarness(t, filepath.Base(modulePath), 0, registry, commonJSModule)
	err := harness.asyncHelper.RunOnLoopBlock(func(_ *goja.Runtime) error {
		exports, err := harness.requireModule.Require(modulePath)
		harness.exports = exports
		return err
	})
	require.NoError(t, err)

	return harness
}

func TestGenericJSHarness(t *testing.T) {
	t.Run("loads CommonJS export and awaits typed result", func(t *testing.T) {
		type result struct {
			Name  string `json:"name"`
			Count int    `json:"count"`
		}
		entryPath := useTestJSFiles(t, map[string]string{
			"entry.js": `
				module.exports = {
					makeResult: async (name, count) => ({ name, count }),
				};
			`,
		})
		harness := LoadJSModule(t, entryPath)
		destination, err := harness.CallAwait[result](t, "makeResult", "a", 1)

		require.NoError(t, err)
		require.Equal(t, result{Name: "a", Count: 1}, destination)
	})

	t.Run("loads plain script and calls method on Go argument", func(t *testing.T) {
		entryPath := useTestJSFiles(t, map[string]string{
			"entry.js": `
				function callRecorder(recorder) {
					return recorder.record({ label: "a", values: [1, 2] });
				}
			`,
		})
		harness := LoadJSScript(t, entryPath)
		recorder := &testRecorder{}

		result, err := harness.Call[int64](t, "callRecorder", recorder)

		require.NoError(t, err)
		require.Equal(t, int64(3), result)
		require.Equal(t, testRecordRequest{Label: "a", Values: []int{1, 2}}, recorder.received)
	})

	t.Run("resolves CommonJS dependency independently of working directory", func(t *testing.T) {
		entryPath := useTestJSFiles(t, map[string]string{
			"entry.js": `
				const value = require("./value");
				module.exports = { getValue: () => value };
			`,
			"value.js": `module.exports = 42;`,
		})
		t.Chdir(t.TempDir())
		harness := LoadJSModule(t, entryPath)

		result, err := harness.Call[int64](t, "getValue")

		require.NoError(t, err)
		require.Equal(t, int64(42), result)
	})

	t.Run("preserves error location from CommonJS dependency", func(t *testing.T) {
		entryPath := useTestJSFiles(t, map[string]string{
			"entry.js":      `const dependency = require("./dependency"); module.exports = { fail: dependency.fail };`,
			"dependency.js": `module.exports = { fail: () => { throw new Error("dependency boom"); } };`,
		})
		harness := LoadJSModule(t, entryPath)

		result, err := harness.Call[any](t, "fail")

		require.Nil(t, result)
		require.Error(t, err)
		var scriptErr *gojautils.ScriptError
		require.ErrorAs(t, err, &scriptErr)
		require.NotEmpty(t, scriptErr.Stack)
		require.Equal(t, "dependency.js", filepath.Base(scriptErr.Stack[0].File))
		require.Equal(t, 1, scriptErr.Stack[0].Line)
	})

	t.Run("resolves plain script dependency independently of working directory", func(t *testing.T) {
		entryPath := useTestJSFiles(t, map[string]string{
			"entry.js": `
				const value = require("./value");
				function getValue() { return value; }
			`,
			"value.js": `module.exports = 42;`,
		})
		t.Chdir(t.TempDir())
		harness := LoadJSScript(t, entryPath)

		result, err := harness.Call[int64](t, "getValue")

		require.NoError(t, err)
		require.Equal(t, int64(42), result)
	})

	t.Run("does not expose Performix global by default", func(t *testing.T) {
		entryPath := useTestJSFiles(t, map[string]string{
			"entry.js": `
				function hasPerformix() {
					return typeof performix !== "undefined";
				}
			`,
		})
		harness := LoadJSScript(t, entryPath)
		hasPerformix, err := harness.Call[bool](t, "hasPerformix")

		require.NoError(t, err)
		require.False(t, hasPerformix)
	})

	t.Run("exposes Performix global when enabled", func(t *testing.T) {
		entryPath := useTestJSFiles(t, map[string]string{
			"entry.js": `
				module.exports = {
					hasPerformix: () => typeof performix !== "undefined",
				};
			`,
		})
		harness := LoadJSModule(t, entryPath, WithPerformixGlobal)
		hasPerformix, err := harness.Call[bool](t, "hasPerformix")

		require.NoError(t, err)
		require.True(t, hasPerformix)
	})

	t.Run("round-trips returned JavaScript function", func(t *testing.T) {
		entryPath := useTestJSFiles(t, map[string]string{
			"entry.js": `
				module.exports = {
					makeAdder: (left) => (right) => left + right,
					apply: (fn, value) => fn(value),
				};
			`,
		})
		harness := LoadJSModule(t, entryPath)

		result, err := harness.Call[any](t, "makeAdder", 20)
		require.NoError(t, err)
		_, ok := result.(func(goja.FunctionCall) goja.Value)
		require.True(t, ok)

		result, err = harness.Call[any](t, "apply", result, 22)

		require.NoError(t, err)
		require.Equal(t, int64(42), result)
	})
}

func TestCall(t *testing.T) {
	t.Run("passes arguments and exports return value", func(t *testing.T) {
		harness := loadTestJSModule(t, `
			module.exports = {
				add: (left, right) => left + right,
			};
		`)

		result, err := harness.Call[int64](t, "add", 20, 22)

		require.NoError(t, err)
		require.Equal(t, int64(42), result)
	})

	t.Run("optionally awaits promise", func(t *testing.T) {
		harness := loadTestJSModule(t, `
			module.exports = {
				getValue: async () => 42,
			};
		`)

		result, err := harness.Call[any](t, "getValue")

		require.NoError(t, err)
		require.IsType(t, (*goja.Promise)(nil), result)

		resolvedResult, err := harness.CallAwait[int64](t, "getValue")

		require.NoError(t, err)
		require.Equal(t, int64(42), resolvedResult)
	})

	t.Run("uses CommonJS exports as function receiver", func(t *testing.T) {
		harness := loadTestJSModule(t, `
			function getValue() {
				return this.value;
			}

			module.exports = {
				value: 42,
				getValue,
			};
		`)

		result, err := harness.Call[int64](t, "getValue")

		require.NoError(t, err)
		require.Equal(t, int64(42), result)
	})

	t.Run("returns JavaScript error", func(t *testing.T) {
		harness := loadTestJSModule(t, `
			module.exports = {
				fail: () => { throw new Error("boom"); },
			};
		`)

		result, err := harness.Call[any](t, "fail")

		require.Nil(t, result)
		require.Error(t, err)
		var scriptErr *gojautils.ScriptError
		require.ErrorAs(t, err, &scriptErr)
		require.Equal(t, "boom", scriptErr.Message)
	})

	t.Run("converts structured JavaScript error into Message", func(t *testing.T) {
		metadata := map[string]string{
			"deployPath": "/a",
			"locality":   "target",
			"tool":       "a",
		}
		harness := loadTestJSModule(t, `
			module.exports = {
				fail: () => {
					throw {
						code: "tool_integrations.common.TOOL_NOT_DEPLOYED",
						cause: "missing tool",
						metadata: {
							deployPath: "/a",
							locality: "target",
							tool: "a",
						},
					};
				},
			};
		`)
		expectedErr := message.New(message.ToolIntegrationsCommonToolNotDeployed).
			WithMetadata(metadata).
			WithCause(errors.New("missing tool"))

		result, err := harness.Call[any](t, "fail")

		require.Nil(t, result)
		require.Error(t, err)
		require.Equal(t, expectedErr, err)
		require.NoError(t, message.ValidateMetadataPlaceholders(expectedErr))
	})

	t.Run("converts structured asynchronous JavaScript error into Message", func(t *testing.T) {
		metadata := map[string]string{
			"deployPath": "/a",
			"locality":   "target",
			"tool":       "a",
		}
		harness := loadTestJSModule(t, `
			module.exports = {
				fail: async () => {
					await Promise.resolve();
					throw {
						code: "tool_integrations.common.TOOL_NOT_DEPLOYED",
						cause: "missing tool",
						metadata: {
							deployPath: "/a",
							locality: "target",
							tool: "a",
						},
					};
				},
			};
		`)
		expectedErr := message.New(message.ToolIntegrationsCommonToolNotDeployed).
			WithMetadata(metadata).
			WithCause(errors.New("missing tool"))

		result, err := harness.CallAwait[any](t, "fail")

		require.Nil(t, result)
		require.Error(t, err)
		require.Equal(t, expectedErr, err)
		require.NoError(t, message.ValidateMetadataPlaceholders(expectedErr))
	})
}

func TestTypedCall(t *testing.T) {
	t.Run("exports object into requested type", func(t *testing.T) {
		type result struct {
			Name  string `json:"name"`
			Count int    `json:"count"`
		}
		harness := loadTestJSModule(t, `
			module.exports = {
				makeResult: (name, count) => ({ name, count }),
			};
		`)
		destination, err := harness.Call[result](t, "makeResult", "a", 1)

		require.NoError(t, err)
		require.Equal(t, result{Name: "a", Count: 1}, destination)
	})

	t.Run("exports promise without awaiting", func(t *testing.T) {
		harness := loadTestJSModule(t, `
			module.exports = {
				getValue: async () => 42,
			};
		`)
		destination, err := harness.Call[any](t, "getValue")

		require.NoError(t, err)
		require.IsType(t, (*goja.Promise)(nil), destination)
	})

	t.Run("returns Message with zero result", func(t *testing.T) {
		type result struct {
			Value string `json:"value"`
		}
		harness := loadTestJSModule(t, `
			module.exports = {
				fail: () => failWithMessage(),
			};
		`)
		expectedErr := message.New(message.EngineRecipeStagesHostAgentConnectionFailed).
			WithCause(errors.New("connection failed"))
		err := harness.asyncHelper.RunOnLoopBlock(func(vm *goja.Runtime) error {
			return vm.Set("failWithMessage", func() {
				panic(vm.NewGoError(expectedErr))
			})
		})
		require.NoError(t, err)
		destination, err := harness.Call[result](t, "fail")

		require.Error(t, err)
		require.Equal(t, expectedErr, err)
		require.NoError(t, message.ValidateMetadataPlaceholders(expectedErr))
		require.Equal(t, result{}, destination)
	})
}

func TestPromiseConversions(t *testing.T) {
	loadPromiseHarness := func(t *testing.T) *GenericJSHarness {
		t.Helper()

		return loadTestJSModule(t, `
			module.exports = {
				awaitPromise: async (promise) => await promise,
			};
		`)
	}
	promiseState := func(t *testing.T, harness *GenericJSHarness, promise goja.Value) goja.PromiseState {
		t.Helper()

		var state goja.PromiseState
		err := harness.asyncHelper.RunOnLoopBlock(func(_ *goja.Runtime) error {
			state = promise.Export().(*goja.Promise).State()
			return nil
		})
		require.NoError(t, err)
		return state
	}
	requireRejectedWith := func(t *testing.T, result any, err error, message string) {
		t.Helper()

		require.Nil(t, result)
		require.Error(t, err)
		var scriptErr *gojautils.ScriptError
		require.ErrorAs(t, err, &scriptErr)
		require.Equal(t, message, scriptErr.Message)
	}

	t.Run("value promise resolves with supplied value", func(t *testing.T) {
		harness := loadPromiseHarness(t)
		promise := harness.ToJSValPromise(t, "a", nil)

		result, err := harness.CallAwait[string](t, "awaitPromise", promise)

		require.NoError(t, err)
		require.Equal(t, "a", result)
	})

	t.Run("value promise rejects with supplied error", func(t *testing.T) {
		harness := loadPromiseHarness(t)
		promise := harness.ToJSValPromise(t, nil, errors.New("boom"))

		result, err := harness.CallAwait[any](t, "awaitPromise", promise)

		requireRejectedWith(t, result, err, "boom")
	})

	t.Run("OK promise resolves true", func(t *testing.T) {
		harness := loadPromiseHarness(t)
		promise := harness.ToJSOKPromise(t, nil)

		result, err := harness.CallAwait[bool](t, "awaitPromise", promise)

		require.NoError(t, err)
		require.Equal(t, true, result)
	})

	t.Run("OK promise rejects with supplied error", func(t *testing.T) {
		harness := loadPromiseHarness(t)
		promise := harness.ToJSOKPromise(t, errors.New("boom"))

		result, err := harness.CallAwait[any](t, "awaitPromise", promise)

		requireRejectedWith(t, result, err, "boom")
	})

	t.Run("custom promise remains pending then resolves its value", func(t *testing.T) {
		harness := loadPromiseHarness(t)
		callbackStarted := make(chan struct{})
		releaseCallback := make(chan struct{})
		var releaseOnce sync.Once
		release := func() {
			releaseOnce.Do(func() { close(releaseCallback) })
		}
		t.Cleanup(release)
		promise := harness.ToJSCustomPromise(t, func() (any, error) {
			close(callbackStarted)
			<-releaseCallback
			return "a", nil
		})
		<-callbackStarted
		require.Equal(t, goja.PromiseStatePending, promiseState(t, harness, promise))
		release()

		result, err := harness.CallAwait[string](t, "awaitPromise", promise)

		require.NoError(t, err)
		require.Equal(t, "a", result)
	})

	t.Run("custom promise remains pending then rejects its error", func(t *testing.T) {
		harness := loadPromiseHarness(t)
		callbackStarted := make(chan struct{})
		releaseCallback := make(chan struct{})
		var releaseOnce sync.Once
		release := func() {
			releaseOnce.Do(func() { close(releaseCallback) })
		}
		t.Cleanup(release)
		promise := harness.ToJSCustomPromise(t, func() (any, error) {
			close(callbackStarted)
			<-releaseCallback
			return nil, errors.New("boom")
		})
		<-callbackStarted
		require.Equal(t, goja.PromiseStatePending, promiseState(t, harness, promise))
		release()

		result, err := harness.CallAwait[any](t, "awaitPromise", promise)

		requireRejectedWith(t, result, err, "boom")
	})

	t.Run("custom promise executes callback and resolves its value", func(t *testing.T) {
		harness := loadPromiseHarness(t)
		var callbackCalls atomic.Int32
		promise := harness.ToJSCustomPromise(t, func() (any, error) {
			callbackCalls.Add(1)
			return "a", nil
		})

		result, err := harness.CallAwait[string](t, "awaitPromise", promise)

		require.NoError(t, err)
		require.Equal(t, "a", result)
		require.Equal(t, int32(1), callbackCalls.Load())
	})

	t.Run("custom promise rejects callback error", func(t *testing.T) {
		harness := loadPromiseHarness(t)
		promise := harness.ToJSCustomPromise(t, func() (any, error) {
			return nil, errors.New("boom")
		})

		result, err := harness.CallAwait[any](t, "awaitPromise", promise)

		requireRejectedWith(t, result, err, "boom")
	})
}
