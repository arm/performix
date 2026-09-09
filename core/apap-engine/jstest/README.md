<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# JavaScript Unit Tests

The `jstest` package provides Go harnesses for testing the JavaScript under `core/apap-cli` with Goja. The harnesses load the requested JavaScript, manage the Goja event loop for the lifetime of the test, convert values between Go and JavaScript, and return JavaScript exceptions as Go errors.

## Choosing a Harness

| JavaScript under test | Loader | Harness |
| --- | --- | --- |
| Functions exported by a CommonJS module | `LoadJSModule` | `GenericJSHarness` |
| Functions defined in the global scope of a plain script | `LoadJSScript` | `GenericJSHarness` |
| A recipe and its lifecycle stages | `LoadRecipe` | `RecipeJSHarness` |
| A tool integration and its lifecycle methods | `LoadToolIntegration` | `ToolIntegrationJSHarness` |

Paths passed to `LoadJSModule` and `LoadJSScript` are relative to `core/apap-cli`. `LoadRecipe` and `LoadToolIntegration` take a recipe or tool integration name without the `.js` extension. All loaders take the current `*testing.T` and stop their event loop automatically during test cleanup.

The usual test structure is:

1. Load the JavaScript with the appropriate harness.
2. Create a mock execution context or tool engine and configure its expected calls.
3. Use the harness conversion helpers for mocked methods that return JavaScript values or promises.
4. Call the JavaScript function or lifecycle method.
5. Assert on the typed output or error, then verify the mock expectations.

## Generic JavaScript Harness

`GenericJSHarness` tests individual functions without applying recipe or tool-integration lifecycle rules. Use `LoadJSModule` for functions in `module.exports`; non-exported functions are not visible through this loader. Use `LoadJSScript` for functions declared in a script's global scope.

Both loaders accept the optional `WithPerformixGlobal` load option when the script needs `globalThis.performix` metadata.

### Methods

| Method | Purpose |
| --- | --- |
| `Call[T](t, functionName, args...)` | Calls a function and exports its immediate return value to `T`. It does not await a returned promise. |
| `CallAwait[T](t, functionName, args...)` | Calls a function, awaits its returned value if necessary, and exports the resolved value to `T`. |
| `ToJSValue(t, value)` | Converts a Go value to a `goja.Value` owned by this harness's runtime. |
| `ToJSValPromise(t, value, err)` | Creates a promise that resolves to `value`, or rejects with `err`. |
| `ToJSValPromiseWithDelay(t, value, err, delay)` | Creates the same value promise after a delay. |
| `ToJSOKPromise(t, err)` | Creates a promise for an operation with no result; it resolves successfully or rejects with `err`. |
| `ToJSOKPromiseWithDelay(t, err, delay)` | Creates the same no-result promise after a delay. |
| `ToJSCustomPromise(t, function)` | Creates a promise whose result is controlled by a supplied Go function. |

Use a concrete result type with `Call[T]` or `CallAwait[T]` whenever the expected shape is known. Use `any` for Goja's natural export types: for example, JavaScript arrays become `[]any` and objects become `map[string]any`. If the call returns an error, the result is the zero value of `T`.

This example tests an asynchronous CommonJS export and mocks a JavaScript-facing engine method that returns a promise:

```go
func TestResolveLoginName(t *testing.T) {
	harness := jstest.LoadJSModule(t, "tool-integrations/utils.js")
	engine := &mocks.MockToolEngine{}
	engine.On("ExecCommand", []string{"logname"}, tool_goja.ExecOptions{}).
		Return(harness.ToJSValPromise(t, process.CommandResult{
			Rc:     0,
			Stdout: "test-user",
		}, nil))

	result, err := harness.CallAwait[string](t, "resolveLoginName", engine)

	require.NoError(t, err)
	require.Equal(t, "test-user", result)
	engine.AssertExpectations(t)
}
```

Use `Call` instead when the function is synchronous:

```go
result, err := harness.Call[bool](t, "isElevatePrivilegeError", errors.New("failed"))
require.NoError(t, err)
require.False(t, result)
```

## Recipe Harness

`RecipeJSHarness` loads a recipe from `core/apap-cli/recipes/<name>.js`. Its lifecycle methods use the same parsing, stage ordering, and output aggregation rules as the recipe runtime. It embeds `GenericJSHarness`, so all the generic methods described above, including `Call[T]`, `CallAwait[T]`, and the conversion helpers, are also available for testing global helper functions in the recipe file.

Recipe lifecycle callbacks are synchronous. If a callback returns a promise, the harness returns an error matching `jstest.ErrPromiseNotSupported`.

### Methods

| Method | Purpose |
| --- | --- |
| `RecipeProperties()` | Returns the recipe's name, title, versions, status, description, MCP guidance, deployments, parameters, and render parameters. |
| `RecipeReady(t, context)` | Runs the ready stages in order and returns their combined `recipe.ReadyOutput`. |
| `RecipeRun(t, context)` | Runs the run stages in order and returns the first error. |
| `RecipeRender(t, context)` | Runs the render stages in order, combines their outputs, and validates the combined render output. |
| `RecipeValidateParameters(t, context)` | Runs parameter validation and returns typed validation errors. It returns an empty slice when the recipe has no validation function. |
| `ComputeOptions(t, parameterID, context)` | Runs the dynamic options function for a select, multi-select, or radio parameter and returns typed options. |

Use `mocks.MockReadyExecutionContext`, `mocks.MockRunExecutionContext`, or `mocks.MockRenderExecutionContext` for the corresponding lifecycle. Configure only the calls made by the JavaScript path under test and verify them after the call.

```go
func TestCacheSharingRun(t *testing.T) {
	harness := jstest.LoadRecipe(t, "cache_sharing")
	context := &mocks.MockRunExecutionContext{}
	context.On("GetWorkload").Return(
		recipeparser.WorkloadArg{Type: "systemWide"}, nil,
	)
	context.On("GetParameter", "user_only").Return(true, nil)
	context.On("RunTools", mock.MatchedBy(
		func(arg recipeparser.RunToolConfigurationsArg) bool {
			return arg.ToolConfigs[0].Params["perfArgs"] == "c2c record -u"
		},
	)).Return(nil)

	err := harness.RecipeRun(t, context)

	require.NoError(t, err)
	context.AssertExpectations(t)
}
```

## Tool Integration Harness

`ToolIntegrationJSHarness` loads a tool integration from `core/apap-cli/tool-integrations/<name>.js`. Its lifecycle methods supply the engine and tool context in the same shape used at runtime, await asynchronous lifecycle functions, and convert their outputs to engine types. It also embeds `GenericJSHarness`, so all the generic call and conversion methods are available for directly testing global helper functions in the tool integration file.

### Methods

| Method | Purpose |
| --- | --- |
| `ToolProperties()` | Returns the tool integration's name, version, descriptions, deployments, migrations, and workload-launch support. |
| `ToolProbe(t, engine, context)` | Calls `probe`, awaits it, and converts its result to `tool.ProbeResult`. |
| `ToolRun(t, engine, context)` | Calls and awaits `run`. |
| `ToolReformat(t, engine, context)` | Calls and awaits `reformat`. |
| `ToolStop(t, engine, context)` | Calls and awaits `onStop`. |
| `ToolCancel(t, engine, context)` | Calls and awaits `onCancel`. |
| `ToJSProcessHandle(t, handle, options)` | Converts an engine process handle to the JavaScript `ProcessHandle` shape using the supplied stream and stdin options. |
| `ToJSProcessHandleNoOptions(t, handle)` | Converts a process handle without exposing stdout, stderr, or an open stdin. |

Start with `EmptyToolContext()` when the JavaScript expects context maps such as `params`, `env`, or `metadata` to exist, then set the fields needed by the test. Methods on `mocks.MockToolEngine` that represent asynchronous JavaScript APIs return `goja.Value`; build those return values with the promise helpers on the same harness.

```go
func TestLinuxPerfProbe(t *testing.T) {
	harness := jstest.LoadToolIntegration(t, "linux_perf")
	engine := &mocks.MockToolEngine{}
	engine.On("ExecCommand", []string{"perf", "--version"}, tool_goja.ExecOptions{}).
		Return(harness.ToJSValPromise(t, process.CommandResult{}, nil))
	engine.On("ExecCommand", []string{"python3", "--version"}, tool_goja.ExecOptions{}).
		Return(harness.ToJSValPromise(t, process.CommandResult{Rc: 5}, nil))

	result, err := harness.ToolProbe(t, engine, jstest.EmptyToolContext())

	require.NoError(t, err)
	require.False(t, result.Available)
	require.Len(t, result.Advice, 1)
	engine.AssertExpectations(t)
}
```

`ToJSProcessHandle` returns the JavaScript handle itself, not a promise. If a mocked method returns a promise to a process handle, combine the helpers:

```go
jsHandle := harness.ToJSProcessHandle(t, processHandle, options)
engine.On("StartProcess", command, optionsMatcher).
	Return(harness.ToJSValPromise(t, jsHandle, nil))
```

## Mock Matching and Type Conversion

Goja converts values as they cross the Go and JavaScript boundary. JavaScript-facing mock methods can accept a concrete Go type, `any` for the natural Go representation, or `goja.Value` when the test needs to handle conversion itself.

Use `DecodesTo` with `mock.MatchedBy` when a mock parameter has a broad type such as `any`, `[]any`, or `map[string]any`, but the test should compare it with a concrete Go value:

```go
engine.On(
	"SomeMethod",
	mock.MatchedBy(jstest.DecodesTo(expectedConfig)),
).Return(nil)
```

The `jstest/mocks` package supplies mock implementations for recipe ready, run, and render contexts, tool engines, and tool file handles. `MockToolEngineIgnoreLogs()` is convenient when log calls are not relevant to a tool-integration test.

## Errors

Exceptions thrown by a JavaScript function and promise rejections are returned as errors. Exceptions that follow the [CatalogMessage structure](../../apap-cli/recipes/docs/jsdocs.js) are converted to Go `message.Message` values. Other JavaScript errors are returned as `gojautils.ScriptError` values.

Assert the error as well as the zero-valued result when testing a failure from `Call[T]` or `CallAwait[T]`:

```go
result, err := harness.CallAwait[string](t, "functionThatRejects")
require.Empty(t, result)
require.Error(t, err)
```

## Running the Tests

From the repository root, run the focused JavaScript unit-test task:

```shell
task core:test:unit:engine:js
```

For a broader engine check, run `task core:test:unit:engine` as documented in the repository [development guide](../../../DEVELOPMENT.md).
