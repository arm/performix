// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package gojautils

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"testing"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/eventloop"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/atperf-version/versions"
)

type ParseTest struct {
	Name    string
	Options []string
	Enabled bool
}

func TestParseObjectFromJS(t *testing.T) {

	t.Run("valid input", func(t *testing.T) {
		vm := goja.New()
		validJS := `
			({
					Name: "testName",
					Options: ["a", "b"],
					Enabled: true
			})`
		val1, err := vm.RunString(validJS)
		require.NoError(t, err, "should run JS code without error")

		var dest1 ParseTest
		err = ParseObjectFromJS(val1, &dest1)
		require.NoError(t, err)
		assert.Equal(t, ParseTest{Name: "testName", Options: []string{"a", "b"}, Enabled: true}, dest1)
	})

	t.Run("Invalid input, missing Options and Enabled, extra field", func(t *testing.T) {
		vm := goja.New()
		invalidJS := `
			({
					Name: "testName",
					Extra: "extra",
			})`
		val2, err := vm.RunString(invalidJS)
		require.NoError(t, err)

		var dest2 ParseTest
		err = ParseObjectFromJS(val2, &dest2)
		require.Error(t, err)
	})

	t.Run("Input with allowedUnset regex (optional)", func(t *testing.T) {
		vm := goja.New()
		invalidJS2 := `
			({
					Name: "testName",
					Extra: "T",
			})`
		val3, err := vm.RunString(invalidJS2)
		require.NoError(t, err)

		var dest3 ParseTest
		err = ParseObjectFromJSWithRegex(
			val3,
			&dest3,
			[]*regexp.Regexp{regexp.MustCompile(`Options|Enabled`)}, // allow missing
			[]*regexp.Regexp{regexp.MustCompile("Extra")},           // allow unused
		)
		assert.NoError(t, err, "should succeed when unset fields are allowed via regex")
	})
}

func TestGoObjectToJS(t *testing.T) {

	t.Run("lower case fields are accessible", func(t *testing.T) {
		vm := goja.New()
		customStruct := struct {
			Level   string `json:"level"`
			Message string `json:"message"`
		}{
			Level:   "info",
			Message: "test message",
		}
		obj, err := GoObjectToJS(vm, &customStruct)
		require.NoError(t, err)
		_ = vm.Set("inject", obj)
		out, err := vm.RunString(`inject`)
		require.NoError(t, err)
		assert.Equal(t, "info", out.ToObject(vm).Get("level").String())
		assert.Equal(t, "test message", out.ToObject(vm).Get("message").String())
	})

	t.Run("array values are accessible", func(t *testing.T) {
		vm := goja.New()
		customStruct := []struct {
			Level   string `json:"level"`
			Message string `json:"message"`
		}{
			{Level: "info", Message: "test message"},
			{Level: "info2", Message: "test message again"},
		}
		obj, err := GoArrayToJS(vm, customStruct)
		require.NoError(t, err)
		_ = vm.Set("inject", obj)
		out, err := vm.RunString(`inject[0]`)
		require.NoError(t, err)
		assert.Equal(t, "info", out.ToObject(vm).Get("level").String())
		assert.Equal(t, "test message", out.ToObject(vm).Get("message").String())
		out, err = vm.RunString(`inject[1]`)
		require.NoError(t, err)
		assert.Equal(t, "info2", out.ToObject(vm).Get("level").String())
		assert.Equal(t, "test message again", out.ToObject(vm).Get("message").String())
	})
}

func TestParseAndAdjustFrame(t *testing.T) {
	t.Run("parses unix style frame and prefers source file name", func(t *testing.T) {
		ah := &AsyncHelper{
			SourceFileName: "/tmp/dummy",
			LineOffset:     HelperInjectedLineCount(),
		}

		frame, ok := ah.parseAndAdjustFrame("\tat run (/tmp/dummy:29:4)")
		require.True(t, ok)
		assert.Equal(t, "dummy", frame.File)
		assert.Equal(t, "run", frame.Function)
		assert.Equal(t, 29-HelperInjectedLineCount(), frame.Line)
		assert.Equal(t, 4, frame.Column)
	})

	t.Run("parses windows backslash frame and prefers basename from windows source path", func(t *testing.T) {
		ah := &AsyncHelper{
			SourceFileName: `D:\a\performix\performix\dummy`,
			LineOffset:     HelperInjectedLineCount(),
		}

		frame, ok := ah.parseAndAdjustFrame("\tat run (D:\\a\\performix\\performix\\dummy:29:4)")
		require.True(t, ok)
		assert.Equal(t, "dummy", frame.File)
		assert.Equal(t, "run", frame.Function)
		assert.Equal(t, 29-HelperInjectedLineCount(), frame.Line)
		assert.Equal(t, 4, frame.Column)
	})

	t.Run("parses windows slash frame and prefers basename from windows source path", func(t *testing.T) {
		ah := &AsyncHelper{
			SourceFileName: `D:\a\performix\performix\dummy`,
			LineOffset:     HelperInjectedLineCount(),
		}

		frame, ok := ah.parseAndAdjustFrame("\tat run (D:/a/performix/performix/dummy:29:4)")
		require.True(t, ok)
		assert.Equal(t, "dummy", frame.File)
		assert.Equal(t, "run", frame.Function)
		assert.Equal(t, 29-HelperInjectedLineCount(), frame.Line)
		assert.Equal(t, 4, frame.Column)
	})
}

func TestSetPerformixGlobal(t *testing.T) {
	vm := goja.New()
	require.NoError(t, SetPerformixGlobal(vm))

	out, err := vm.RunString(`performix.engineVersion`)
	require.NoError(t, err)
	assert.Equal(t, versions.GetVersion(), out.String())
}

func TestCallScriptedFunction(t *testing.T) {
	t.Run("basic scripted function runs as expected", func(t *testing.T) {
		ah := &AsyncHelper{
			Loop: eventloop.NewEventLoop(),
			Ctx:  context.Background(),
		}
		ah.Loop.Run(func(r *goja.Runtime) { ah.Vm = r })

		_, err := ah.Vm.RunString(InjectAsyncHelpers(`
		function testFunc() {
			return "hello"
		}
		`))
		require.NoError(t, err)

		ah.StartLoop()
		defer ah.StopLoop()

		testFunc := ah.Vm.Get("testFunc").Export().(func(goja.FunctionCall) goja.Value)
		out, err := ah.CallScriptedFunction(testFunc, nil)
		require.NoError(t, err)

		assert.Equal(t, "hello", out.Export())
	})

	t.Run("asnychronous scripted function runs as expected", func(t *testing.T) {
		ah := &AsyncHelper{
			Loop: eventloop.NewEventLoop(),
			Ctx:  context.Background(),
		}
		ah.Loop.Run(func(r *goja.Runtime) { ah.Vm = r })

		_, err := ah.Vm.RunString(InjectAsyncHelpers(`
		async function asyncFunc() {
		  return "hello"
	  }

		async function testFunc() {
			return await asyncFunc()
		}
		`))
		require.NoError(t, err)

		ah.StartLoop()
		defer ah.StopLoop()

		testFunc := ah.Vm.Get("testFunc").Export().(func(goja.FunctionCall) goja.Value)
		res, err := ah.CallScriptedFunction(testFunc, nil)
		require.NoError(t, err)

		assert.Equal(t, "hello", res.Export())
	})

	t.Run("ScriptError type errors are preserved", func(t *testing.T) {
		ah := &AsyncHelper{
			Loop: eventloop.NewEventLoop(),
			Ctx:  context.Background(),
		}
		ah.Loop.Run(func(r *goja.Runtime) { ah.Vm = r })

		// This emulates a recipe/tool-integration throwing a ScriptError
		_, err := ah.Vm.RunString(InjectAsyncHelpers(`
		function testFunc() {
			throw {
				code: 'tool_integration.common.TOOL_NOT_DEPLYOED',
				cause: 'ToolNotDeployed',
				metadata: { tool: 'neoprof' }
			}
		}
		`))
		require.NoError(t, err)

		ah.StartLoop()
		defer ah.StopLoop()

		testFunc := ah.Vm.Get("testFunc").Export().(func(goja.FunctionCall) goja.Value)
		_, err = ah.CallScriptedFunction(testFunc, nil)
		require.Error(t, err)

		var scriptErr *ScriptError
		require.True(t, errors.As(err, &scriptErr), "error should be of type ScriptError")

		assert.Equal(t, "tool_integration.common.TOOL_NOT_DEPLYOED", scriptErr.Code)
		assert.Equal(t, "ToolNotDeployed", scriptErr.Cause)
		assert.Equal(t, "neoprof", scriptErr.Metadata["tool"])
	})
}

func TestCallScriptedFunction_PanicReporting(t *testing.T) {
	t.Run("synchronous engine error is reported", func(t *testing.T) {
		ah := &AsyncHelper{
			Loop:           eventloop.NewEventLoop(),
			Ctx:            context.Background(),
			SourceFileName: "testFile",
			LineOffset:     HelperInjectedLineCount(),
		}
		ah.Loop.Run(func(r *goja.Runtime) { ah.Vm = r })

		_, err := ah.Vm.RunString(InjectAsyncHelpers(`
		function testFunc() {
			let out = goFunc()
		}
		`))
		require.NoError(t, err)
		require.NoError(t, ah.Vm.Set("goFunc", func() goja.Value {
			panic(ah.Vm.NewGoError(fmt.Errorf("sync exploded")))
		}))

		ah.StartLoop()
		defer ah.StopLoop()

		testFunc := ah.Vm.Get("testFunc").Export().(func(goja.FunctionCall) goja.Value)
		_, err = ah.CallScriptedFunction(testFunc, nil)
		require.ErrorContains(t, err, "sync exploded")
	})

	t.Run("asynchronous engine error is reported", func(t *testing.T) {
		ah := &AsyncHelper{
			Loop:           eventloop.NewEventLoop(),
			Ctx:            context.Background(),
			SourceFileName: "testFile.js",
			LineOffset:     HelperInjectedLineCount(),
		}
		ah.Loop.Run(func(r *goja.Runtime) { ah.Vm = r })

		_, err := ah.Vm.RunString(InjectAsyncHelpers(
			`async function testFunc() {
			
			let out = goFunc()
		}`))
		require.NoError(t, err)

		require.NoError(t, ah.Vm.Set("goFunc", func() goja.Value {
			panic(ah.Vm.NewGoError(fmt.Errorf("sync exploded")))
		}))

		ah.StartLoop()
		defer ah.StopLoop()

		testFunc := ah.Vm.Get("testFunc").Export().(func(goja.FunctionCall) goja.Value)
		_, err = ah.CallScriptedFunction(testFunc, nil)
		require.Contains(t, err.Error(), "sync exploded testFile.js:testFunc:3")
	})

	t.Run("catalog error messages are preserved", func(t *testing.T) {
		ah := &AsyncHelper{
			Loop:           eventloop.NewEventLoop(),
			Ctx:            context.Background(),
			SourceFileName: "testFile.js",
			LineOffset:     HelperInjectedLineCount(),
		}
		ah.Loop.Run(func(r *goja.Runtime) { ah.Vm = r })

		_, err := ah.Vm.RunString(InjectAsyncHelpers(
			`async function testFunc() { const out = goFunc() }`))
		require.NoError(t, err)

		expectedMsg := message.New("tool_integration.common.TOOL_NOT_DEPLYOED").
			WithMetadata(map[string]string{"tool": "neoprof"}).
			WithCause(fmt.Errorf("ToolNotDeployed"))

		require.NoError(t, ah.Vm.Set("goFunc", func() goja.Value {
			// This emulates Engine panicking with a error during an async call
			// Happens when the Agent gRPC call returns MessageImpl error
			panic(ah.Vm.NewGoError(expectedMsg))
		}))

		ah.StartLoop()
		defer ah.StopLoop()

		testFunc := ah.Vm.Get("testFunc").Export().(func(goja.FunctionCall) goja.Value)
		_, err = ah.CallScriptedFunction(testFunc, nil)
		require.Error(t, err)

		var msgErr *message.MessageImpl
		require.True(t, errors.As(err, &msgErr), "error should be of type MessageImpl")

		assert.Equal(t, expectedMsg.Code(), msgErr.Code())
		assert.Equal(t, expectedMsg.Metadata(), msgErr.Metadata())
		assert.Equal(t, expectedMsg.Reason(), msgErr.Reason())
	})
}

type errOnceReader struct{ err error }

func (e errOnceReader) Read(p []byte) (int, error) { return 0, e.err }

func TestAsyncForAwait(t *testing.T) {
	t.Run("forAwait captures all reader values", func(t *testing.T) {
		loop := eventloop.NewEventLoop()
		var vm *goja.Runtime
		// Pull out the vm for convenience, safe to do as the loop hasn't sterted yet
		loop.Run(func(r *goja.Runtime) { vm = r })
		ah := AsyncHelper{
			Loop: loop,
			Vm:   vm,
			Ctx:  context.Background(),
		}

		readerSrc := io.MultiReader(bytes.NewReader([]byte("A")), bytes.NewReader([]byte("B")), bytes.NewReader([]byte("C")))
		asyncIterator, err := ah.RegisterAsyncIterator(vm, readerSrc)
		require.NoError(t, err)

		_, err = vm.RunString(InjectAsyncHelpers(`
		  out = []
			async function testFunc() {
			  await forAwait(asyncIt, c => out.push(c))
			}
			`))
		require.NoError(t, err)
		require.NoError(t, vm.Set("asyncIt", asyncIterator))
		testFunc := vm.Get("testFunc").Export().(func(goja.FunctionCall) goja.Value)

		ah.StartLoop()
		_, err = ah.CallScriptedFunction(testFunc, nil) // Run the js function
		ah.StopLoop()

		require.NoError(t, err)
		outVal := vm.Get("out").Export()
		assert.Equal(t, []interface{}{"A", "B", "C"}, outVal)
	})

	t.Run("forAwait handles a reader error", func(t *testing.T) {
		loop := eventloop.NewEventLoop()
		var vm *goja.Runtime
		loop.Run(func(r *goja.Runtime) { vm = r })
		ah := AsyncHelper{Loop: loop, Vm: vm, Ctx: context.Background()}

		// A reader that returns "A" then errors.
		errReader := errOnceReader{err: errors.New("boom")}

		readerSrc := io.MultiReader(
			bytes.NewReader([]byte("A")),
			errReader,                    // triggers error after first chunk
			bytes.NewReader([]byte("B")), // should never be reached
		)

		asyncIterator, err := ah.RegisterAsyncIterator(vm, readerSrc)
		require.NoError(t, err)

		_, err = vm.RunString(InjectAsyncHelpers(`
			out = []
			async function testFunc() {
				// Re-throw so Go sees the failure from CallStage
				return await forAwait(asyncIt, c => out.push(c))
			}
		`))
		require.NoError(t, err)

		require.NoError(t, vm.Set("asyncIt", asyncIterator))
		testFunc := vm.Get("testFunc").Export().(func(goja.FunctionCall) goja.Value)

		// Run the JS function; expect the reader error to propagate.
		ah.StartLoop()
		_, callErr := ah.CallScriptedFunction(testFunc, nil)
		ah.StopLoop()
		assert.Contains(t, callErr.Error(), "boom")

		// Ensure we captured values up to the error.
		outVal := vm.Get("out").Export()
		assert.Equal(t, []interface{}{"A"}, outVal)
	})

	t.Run("async iterator handles early return", func(t *testing.T) {
		loop := eventloop.NewEventLoop()
		var vm *goja.Runtime
		loop.Run(func(r *goja.Runtime) { vm = r })
		ah := AsyncHelper{Loop: loop, Vm: vm, Ctx: context.Background()}

		readerSrc := io.MultiReader(
			bytes.NewReader([]byte("A")),
			bytes.NewReader([]byte("B")), // should never be reached
		)

		asyncIterator, err := ah.RegisterAsyncIterator(vm, readerSrc)
		require.NoError(t, err)

		_, err = vm.RunString(InjectAsyncHelpers(`
			out = []
			async function testFunc() {
			i = 0
				await forAwait(asyncIt, chunk => {
					if (++i >= 2) throw "stop"; // simulate early exit
					out.push(chunk)
				});
			}
		`))
		require.NoError(t, err)

		require.NoError(t, vm.Set("asyncIt", asyncIterator))
		testFunc := vm.Get("testFunc").Export().(func(goja.FunctionCall) goja.Value)

		ah.StartLoop()
		_, callErr := ah.CallScriptedFunction(testFunc, nil)
		require.Error(t, callErr, "stop")
		ah.StopLoop()

		// Ensure we captured values up to the error.
		outVal := vm.Get("out").Export()
		assert.Equal(t, []interface{}{"A"}, outVal)
	})

	t.Run("forAwait awaits asynchronous callbacks in order", func(t *testing.T) {
		loop := eventloop.NewEventLoop()
		var vm *goja.Runtime
		loop.Run(func(r *goja.Runtime) { vm = r })
		ah := AsyncHelper{Loop: loop, Vm: vm, Ctx: context.Background()}

		readerSrc := io.MultiReader(bytes.NewReader([]byte("A")), bytes.NewReader([]byte("B")), bytes.NewReader([]byte("C")))
		asyncIterator, err := ah.RegisterAsyncIterator(vm, readerSrc)
		require.NoError(t, err)

		_, err = vm.RunString(InjectAsyncHelpers(`
		out = []
		async function testFunc() {
			let sequence = 0
			await forAwait(asyncIt, async chunk => {
				await new Promise(resolve => setTimeout(resolve, 0))
				if (sequence !== out.length) throw new Error("out of order")
				out.push(chunk)
				sequence++
			})
		}
	`))
		require.NoError(t, err)

		require.NoError(t, vm.Set("asyncIt", asyncIterator))
		testFunc := vm.Get("testFunc").Export().(func(goja.FunctionCall) goja.Value)

		ah.StartLoop()
		_, callErr := ah.CallScriptedFunction(testFunc, nil)
		ah.StopLoop()
		require.NoError(t, callErr)

		outVal := vm.Get("out").Export()
		assert.Equal(t, []interface{}{"A", "B", "C"}, outVal)
	})

	t.Run("forAwait propagates async callback errors", func(t *testing.T) {
		loop := eventloop.NewEventLoop()
		var vm *goja.Runtime
		loop.Run(func(r *goja.Runtime) { vm = r })
		ah := AsyncHelper{Loop: loop, Vm: vm, Ctx: context.Background()}

		readerSrc := io.MultiReader(
			bytes.NewReader([]byte("A")),
			bytes.NewReader([]byte("B")),
		)

		asyncIterator, err := ah.RegisterAsyncIterator(vm, readerSrc)
		require.NoError(t, err)

		_, err = vm.RunString(InjectAsyncHelpers(`
			out = []
			async function testFunc() {
				let i = 0
				await forAwait(asyncIt, async chunk => {
					out.push(chunk)
					if (++i >= 2) {
						await new Promise(resolve => setTimeout(resolve, 0))
						throw new Error("callback failed")
					}
				})
			}
		`))
		require.NoError(t, err)

		require.NoError(t, vm.Set("asyncIt", asyncIterator))
		testFunc := vm.Get("testFunc").Export().(func(goja.FunctionCall) goja.Value)

		ah.StartLoop()
		_, callErr := ah.CallScriptedFunction(testFunc, nil)
		require.Error(t, callErr)
		assert.Contains(t, callErr.Error(), "callback failed")
		ah.StopLoop()

		// Ensure we captured values up to the error.
		outVal := vm.Get("out").Export()
		assert.Equal(t, []interface{}{"A", "B"}, outVal)
	})

	t.Run("catalog error messages are preserved", func(t *testing.T) {
		ah := &AsyncHelper{
			Loop: eventloop.NewEventLoop(),
			Ctx:  context.Background(),
		}
		ah.Loop.Run(func(r *goja.Runtime) { ah.Vm = r })

		_, err := ah.Vm.RunString(InjectAsyncHelpers(`
		async function testFunc() {
			await goFunc()
		}
		`))
		require.NoError(t, err)

		expectedMsg := message.New("tool_integration.common.TOOL_NOT_DEPLYOED").
			WithMetadata(map[string]string{"tool": "neoprof"}).
			WithCause(fmt.Errorf("ToolNotDeployed"))

		require.NoError(t, ah.Vm.Set("goFunc", func() goja.Value {
			// This emulates the Engine panicking with a catalog error message on
			// await async function calls
			// Happens when the Agent gRPC call returns MessageImpl error
			return ah.AsyncOK(func() error {
				panic(expectedMsg)
			})
		}))

		ah.StartLoop()
		defer ah.StopLoop()

		testFunc := ah.Vm.Get("testFunc").Export().(func(goja.FunctionCall) goja.Value)
		_, err = ah.CallScriptedFunction(testFunc, nil)
		require.Error(t, err)

		var msgErr *message.MessageImpl
		require.True(t, errors.As(err, &msgErr), "error should be of type MessageImpl")

		assert.Equal(t, expectedMsg.Code(), msgErr.Code())
		assert.Equal(t, expectedMsg.Metadata(), msgErr.Metadata())
		assert.Equal(t, expectedMsg.Reason(), msgErr.Reason())
	})
}
