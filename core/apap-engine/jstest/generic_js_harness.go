// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package jstest

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/eventloop"
	gojarequire "github.com/dop251/goja_nodejs/require"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/gojautils"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	tool_goja "github.com/Arm-Debug/apap-cli/apap-engine/tool/goja"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

// ------------------------
// Harness definition
// ------------------------

// GenericJSHarness allows calling global/exposed functions from a
// JavaScript file.
type GenericJSHarness struct {
	asyncHelper   *gojautils.AsyncHelper
	requireModule *gojarequire.RequireModule
	exports       goja.Value
	fileType      jsFileType

	// Goja runtimes are not goroutine-safe.
	callMu sync.Mutex
}

func newGenericJSHarness(
	t *testing.T,
	sourceFileName string,
	lineOffset int,
	registry *gojarequire.Registry,
	fileType jsFileType,
) *GenericJSHarness {
	t.Helper()

	loop := eventloop.NewEventLoop()
	asyncHelper := &gojautils.AsyncHelper{
		Loop:           loop,
		Ctx:            t.Context(),
		SourceFileName: sourceFileName,
		LineOffset:     lineOffset,
	}
	harness := &GenericJSHarness{
		asyncHelper: asyncHelper,
		fileType:    fileType,
	}

	loop.Start()
	t.Cleanup(func() {
		asyncHelper.StopLoop()
	})

	err := asyncHelper.RunOnLoopBlock(func(vm *goja.Runtime) error {
		asyncHelper.Vm = vm
		vm.SetFieldNameMapper(&tool_goja.JsonFieldNameMapper{LowercaseMethodNames: true})
		harness.requireModule = registry.Enable(vm)
		return nil
	})
	require.NoError(t, err, "failed to initialize JavaScript test harness")

	return harness
}

// ------------------------
// Harness calling methods
// ------------------------

// getFunction gets a named JavaScript function. The caller must hold gh.callMu.
func (gh *GenericJSHarness) getFunction(t *testing.T, functionName string) (func(goja.FunctionCall) goja.Value, error) {
	t.Helper()

	var jsFunction func(goja.FunctionCall) goja.Value
	err := gh.asyncHelper.RunOnLoopBlock(func(vm *goja.Runtime) error {
		var functionValue goja.Value
		if gh.fileType == plainScript {
			functionValue = vm.Get(functionName)
		} else {
			if goja.IsUndefined(gh.exports) || goja.IsNull(gh.exports) {
				functionValue = nil // this will be picked up by the following error case
			} else {
				functionValue = gh.exports.ToObject(vm).Get(functionName)
			}
		}
		if functionValue == nil ||
			goja.IsUndefined(functionValue) ||
			goja.IsNull(functionValue) {
			if gh.fileType == plainScript {
				return fmt.Errorf("JavaScript script does not define %q", functionName)
			}
			return fmt.Errorf("JavaScript module does not export %q", functionName)
		}

		var ok bool
		jsFunction, ok = functionValue.Export().(func(goja.FunctionCall) goja.Value)
		if !ok {
			if gh.fileType == plainScript {
				return fmt.Errorf("JavaScript global %q is not callable", functionName)
			}
			return fmt.Errorf("JavaScript export %q is not callable", functionName)
		}

		return nil
	})
	require.NoError(t, err)

	return jsFunction, nil
}

// callFunction invokes a JavaScript function. The caller must hold gh.callMu.
func (gh *GenericJSHarness) callFunction(
	t *testing.T,
	jsFunction func(goja.FunctionCall) goja.Value,
	args ...any,
) (goja.Value, error) {
	t.Helper()
	require.NotNil(t, jsFunction, "JavaScript function is nil")

	var jsArgs []goja.Value
	err := gh.asyncHelper.RunOnLoopBlock(func(vm *goja.Runtime) error {
		jsArgs = make([]goja.Value, len(args))
		for index, arg := range args {
			jsArgs[index] = vm.ToValue(arg)
		}

		return nil
	})
	require.NoError(t, err, "failed to convert JavaScript function arguments")

	receiver := goja.Undefined()

	if gh.fileType == commonJSModule {
		receiver = gh.exports
	}

	result, err := gh.asyncHelper.ExecuteScriptedFunctionOnLoopWithReceiver(
		jsFunction,
		jsArgs,
		receiver,
	)
	return gh.normalizeCallError(result, err)
}

func (gh *GenericJSHarness) normalizeCallError(result goja.Value, err error) (goja.Value, error) {
	if err == nil {
		return result, nil
	}

	if msg := message.IsMessage(err); msg != nil {
		return result, msg
	}
	if se, ok := err.(*gojautils.ScriptError); ok {
		if message.CodeExists(se.Code, message.LocaleEnglish) {
			// We have a valid message code, return a message error
			return result, message.New(se.Code).WithMetadata(se.Metadata).WithCause(errors.New(tool_goja.CombineMessageAndStack(se.Cause, se.FormatStack())))
		}
	}

	return result, err
}

// callAwaitFunction invokes a JavaScript function and awaits its result. The
// caller must hold gh.callMu.
func (gh *GenericJSHarness) callAwaitFunction(
	t *testing.T,
	jsFunction func(goja.FunctionCall) goja.Value,
	args ...any,
) (goja.Value, error) {
	t.Helper()

	result, err := gh.callFunction(t, jsFunction, args...)
	if err != nil {
		return result, err
	}
	result, err = gh.asyncHelper.Await(result)
	return gh.normalizeCallError(result, err)
}

func (gh *GenericJSHarness) call(
	t *testing.T,
	functionName string,
	await bool,
	args ...any,
) (goja.Value, error) {
	jsFunction, err := gh.getFunction(t, functionName)
	if err != nil {
		return nil, err
	}

	if await {
		return gh.callAwaitFunction(t, jsFunction, args...)
	}
	return gh.callFunction(t, jsFunction, args...)
}

func (gh *GenericJSHarness) callAndExport[T any](
	t *testing.T,
	functionName string,
	await bool,
	args ...any,
) (T, error) {
	t.Helper()

	gh.callMu.Lock()
	defer gh.callMu.Unlock()

	result, err := gh.call(t, functionName, await, args...)
	if err != nil {
		var zero T
		return zero, err
	}

	var exportedResult T
	exportErr := gh.asyncHelper.RunOnLoopBlock(func(vm *goja.Runtime) error {
		return vm.ExportTo(result, &exportedResult)
	})
	require.NoErrorf(t, exportErr, "failed to export result from JavaScript function %q", functionName)

	return exportedResult, nil
}

// Call calls the function with the specified function name and passes in the
// provided arguments. It exports the return value to T and returns it along
// with any error thrown by the function.
//
// Call does not await promises. Use CallAwait when the function's resolved
// value or rejection is required.
//
// Use T = any for Goja's default export type. Common conversions are:
// - undefined and null to nil
// - booleans and strings to bool and string
// - integer numbers to int64, and other numbers to float64
// - ordinary arrays to []any
// - ordinary objects to map[string]any
//
// Errors thrown from JS which follow the structure of a catalog message
// are converted into Go `message.Message`s. Non-`Message` errors are
// returned as plain `gojautils.ScriptError`s.
//
// Wrapped Go values and other specialized JavaScript values use Goja's
// specific export types.
func (gh *GenericJSHarness) Call[T any](t *testing.T, functionName string, args ...any) (T, error) {
	t.Helper()
	return gh.callAndExport[T](t, functionName, false, args...)
}

// CallAwait is equivalent to Call, but awaits the returned promise and exports
// its resolved value.
func (gh *GenericJSHarness) CallAwait[T any](t *testing.T, functionName string, args ...any) (T, error) {
	t.Helper()
	return gh.callAndExport[T](t, functionName, true, args...)
}

// ------------------------
// Helpers
// ------------------------

// ToJSValue converts a Go value into a goja.Value owned by this harness's
// runtime. Use it when mocking a method which returns an immediate JS value.
func (gh *GenericJSHarness) ToJSValue(t *testing.T, value any) goja.Value {
	t.Helper()

	var jsValue goja.Value
	err := gh.asyncHelper.RunOnLoopBlock(func(vm *goja.Runtime) error {
		jsValue = vm.ToValue(value)
		return nil
	})
	require.NoError(t, err)
	return jsValue
}

// ToJSValPromise takes a value and an error, and constructs a new goja.Promise
// which resolves to the specified value if err == nil, or is rejected with the
// specified error if err != nil.
//
// Use this helper when mocking a method which returns a promise to a value.
func (gh *GenericJSHarness) ToJSValPromise(t *testing.T, val any, err error) goja.Value {
	return gh.ToJSValPromiseWithDelay(t, val, err, 0)
}

// ToJSValPromiseWithDelay works in the same way as ToJSValPromise, but waits for
// the specified delay duration before resolving or rejecting.
func (gh *GenericJSHarness) ToJSValPromiseWithDelay(t *testing.T, val any, err error, delay time.Duration) goja.Value {
	var promise goja.Value

	runErr := gh.asyncHelper.RunOnLoopBlock(func(_ *goja.Runtime) error {
		promise = gh.asyncHelper.AsyncVal(func() (any, error) {
			time.Sleep(delay)
			return val, err
		})
		return nil
	})
	assert.NoError(t, runErr)
	return promise
}

// ToJSOKPromise takes an error, and constructs a new goja.Promise which resolves
// to true if err == nil, or is rejected with the specified error if err != nil.
//
// Use this helper when mocking a method which returns a promise to `void`.
func (gh *GenericJSHarness) ToJSOKPromise(t *testing.T, err error) goja.Value {
	return gh.ToJSOKPromiseWithDelay(t, err, 0)
}

// ToJSOKPromiseWithDelay works in the same way as ToJSOKPromise, but waits for
// the specified delay duration before resolving or rejecting.
func (gh *GenericJSHarness) ToJSOKPromiseWithDelay(t *testing.T, err error, delay time.Duration) goja.Value {
	var promise goja.Value

	runErr := gh.asyncHelper.RunOnLoopBlock(func(_ *goja.Runtime) error {
		promise = gh.asyncHelper.AsyncOK(func() error {
			time.Sleep(delay)
			return err
		})
		return nil
	})
	assert.NoError(t, runErr)
	return promise
}

// ToJSCustomPromise takes a custom promise resolution func, and constructs a new
// goja.Promise whose resolution and rejection behaviour is controlled by the
// provided promise func.
//
// Use this helper when granular control over promise resolution behaviour is needed.
func (gh *GenericJSHarness) ToJSCustomPromise(t *testing.T, promiseFunc func() (any, error)) goja.Value {
	var promise goja.Value

	runErr := gh.asyncHelper.RunOnLoopBlock(func(_ *goja.Runtime) error {
		promise = gh.asyncHelper.AsyncVal(promiseFunc)
		return nil
	})
	assert.NoError(t, runErr)
	return promise
}

// ------------------------
// Loaders
// ------------------------

type jsFileType uint8

const (
	commonJSModule jsFileType = iota
	plainScript
)

type loadOptions struct {
	exposePerformixGlobal bool
}

// LoadOption configures how a JavaScript file is loaded.
type LoadOption func(*loadOptions)

// WithPerformixGlobal exposes Performix metadata through globalThis.performix.
var WithPerformixGlobal LoadOption = func(options *loadOptions) {
	options.exposePerformixGlobal = true
}

// LoadJSModule loads a CommonJS module and resolves functions from its exports.
// `relativePath` should be relative to the `apap-cli` directory.
// Use this loading function if the file in question exports functions using
// `module.exports = {...}`. Note that non-exported global functions will not be
// available.
func LoadJSModule(t *testing.T, relativePath string, options ...LoadOption) *GenericJSHarness {
	t.Helper()

	return loadJSFile(t, relativePath, commonJSModule, options...)
}

// LoadJSScript loads a plain JavaScript script and resolves functions from global scope.
// `relativePath` should be relative to the `apap-cli` directory.
// Use this loading function if the file in question does not export any functions. Any
// global functions will be available.
func LoadJSScript(t *testing.T, relativePath string, options ...LoadOption) *GenericJSHarness {
	t.Helper()

	return loadJSFile(t, relativePath, plainScript, options...)
}

func loadJSFile(
	t *testing.T,
	relativePath string,
	fileType jsFileType,
	optionFuncs ...LoadOption,
) *GenericJSHarness {
	t.Helper()

	options := loadOptions{}
	for _, option := range optionFuncs {
		option(&options)
	}

	// Load file
	resolvedPath := resolveJSFilePath(t, relativePath)
	sourceFileName := filepath.Base(resolvedPath)
	data, err := os.ReadFile(resolvedPath)
	require.NoError(t, err, "failed to read requested JS file")

	entrySource := []byte(
		gojautils.InjectAsyncHelpers(string(data)),
	)

	var registry *gojarequire.Registry
	var program *goja.Program
	switch fileType {
	case commonJSModule:
		loader := func(filename string) ([]byte, error) {
			if util.CanonicalPath(filename) == resolvedPath {
				return entrySource, nil
			}

			return gojarequire.DefaultSourceLoader(filename)
		}
		registry = gojarequire.NewRegistry(
			gojarequire.WithLoader(loader),
		)
	case plainScript:
		registry = gojarequire.NewRegistry()
		program, err = goja.Compile(resolvedPath, string(entrySource), false)
		require.NoError(t, err, "failed to compile requested JS file")
	default:
		require.FailNow(t, fmt.Sprintf("unsupported JavaScript file type: %v", fileType))
	}

	// Create generic JS harness
	harness := newGenericJSHarness(
		t,
		resolvedPath,
		gojautils.HelperInjectedLineCount(),
		registry,
		fileType,
	)

	// Execute the file according to its type.
	err = harness.asyncHelper.RunOnLoopBlock(func(vm *goja.Runtime) error {
		if options.exposePerformixGlobal {
			if err := gojautils.SetPerformixGlobal(vm); err != nil {
				return fmt.Errorf("failed to set Performix JavaScript metadata: %w", err)
			}
		}

		switch fileType {
		case commonJSModule:
			exports, err := harness.requireModule.Require(resolvedPath)
			if err != nil {
				return fmt.Errorf("failed to load JavaScript module %q: %w", sourceFileName, err)
			}
			harness.exports = exports
		case plainScript:
			// Run program to register globally-scoped functions
			if _, err := vm.RunProgram(program); err != nil {
				return fmt.Errorf("running JavaScript script %q: %w", sourceFileName, err)
			}
		}
		return nil
	})
	require.NoError(t, err)
	registerCoverageExport(t, harness)

	return harness
}
