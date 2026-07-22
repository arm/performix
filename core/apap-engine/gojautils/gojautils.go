// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package gojautils

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/eventloop"
	"github.com/mitchellh/mapstructure"
	log "github.com/sirupsen/logrus"

	"github.com/Arm-Debug/apap-cli/apap-engine/logging/logx"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/atperf-version/versions"
)

// SetPerformixMetadata attaches shared Performix runtime metadata to a JS object.
func SetPerformixMetadata(obj *goja.Object) error {
	return obj.Set("engineVersion", versions.GetVersion())
}

// SetPerformixGlobal exposes shared Performix runtime metadata to top-level JS code.
func SetPerformixGlobal(vm *goja.Runtime) error {
	performix := vm.NewObject()
	if err := SetPerformixMetadata(performix); err != nil {
		return err
	}
	return vm.Set("performix", performix)
}

// ParseObjectFromJS parses a Goja value into a Go struct. All fields must match, unused or unset will produce an error.
func ParseObjectFromJS(arg goja.Value, dest interface{}) error {
	return ParseObjectFromJSWithRegex(arg, dest, []*regexp.Regexp{}, []*regexp.Regexp{})
}

// ParseObjectFromJS parses a Goja value into a Go struct.
// allowedUnset defines fields that are allowed to be unset.
// allowedUnused defines fields that are allowed to be unused.
func ParseObjectFromJSWithRegex(arg goja.Value, dest interface{}, allowedUnset []*regexp.Regexp, allowedUnused []*regexp.Regexp) error {
	if arg == nil {
		return fmt.Errorf("argument is nil")
	}
	argValue := arg.Export()
	if argValue == nil {
		return fmt.Errorf("type mismatch")
	}

	return ParseObjectWithRegex(argValue, dest, allowedUnset, allowedUnused)
}

func ParseObjectWithRegex(val interface{}, dest interface{}, allowedUnset []*regexp.Regexp, allowedUnused []*regexp.Regexp) error {
	config := &mapstructure.DecoderConfig{
		Metadata:         &mapstructure.Metadata{Unset: []string{}},
		DecodeHook:       nil,
		WeaklyTypedInput: true,
		ErrorUnused:      false,
		ErrorUnset:       false,
		Result:           &dest,
		TagName:          "json",
	}
	decoder, err := mapstructure.NewDecoder(config)
	if err != nil {
		log.Warnf("unable to construct decoder: %v", err)
		return err
	}
	err = decoder.Decode(val)
	if err != nil {
		log.Warnf("unable to decode recipe properties: %v", err)
		return err
	}
	// Unset values will produce an error unless they match an input regex
	unset := []string{}
	for _, field := range config.Metadata.Unset {
		matched := false
		for _, pattern := range allowedUnset {
			if pattern.MatchString(field) {
				matched = true
				break
			}
		}
		if !matched {
			unset = append(unset, field)
		}
	}
	if len(unset) > 0 {
		err = fmt.Errorf("has unset fields: %s", strings.Join(unset, ","))
	}

	// Unused values will produce an error unless they match an input regex
	unused := []string{}
	for _, field := range config.Metadata.Unused {
		matched := false
		for _, pattern := range allowedUnused {
			if pattern.MatchString(field) {
				matched = true
				break
			}
		}
		if !matched {
			unused = append(unused, field)
		}
	}
	if len(unused) > 0 {
		return errors.Join(err, fmt.Errorf("has unused fields: %s", strings.Join(unused, ",")))
	}

	return err
}

// GoObjectToJS converts a Go struct to a Goja value by serializing to JSON and back.
func GoObjectToJS(vm *goja.Runtime, src any) (goja.Value, error) {
	data, err := json.Marshal(src)
	if err != nil {
		return goja.Undefined(), err
	}
	var jsObj map[string]any
	err = json.Unmarshal(data, &jsObj)
	if err != nil {
		return goja.Undefined(), err
	}
	return vm.ToValue(jsObj), nil
}

// GoArrayToJS converts a Go slice to a Goja value by serializing to JSON and back.
func GoArrayToJS[T any](vm *goja.Runtime, src []T) (goja.Value, error) {
	data, err := json.Marshal(src)
	if err != nil {
		return goja.Undefined(), err
	}
	var jsObj []map[string]interface{}
	err = json.Unmarshal(data, &jsObj)
	if err != nil {
		return goja.Undefined(), err
	}
	return vm.ToValue(jsObj), nil
}

// AsyncHelper wraps and provides helpers for constructing promises in a Goja runtime.
type AsyncHelper struct {
	Loop      *eventloop.EventLoop
	Vm        *goja.Runtime
	PromiseWG sync.WaitGroup
	Ctx       context.Context
	// Source information to support converting stacks back to acurate files and lines.
	// If we add support for imported files, this should be updated to a source map
	SourceFileName string
	LineOffset     int
}

func (g *AsyncHelper) StartLoop() {
	g.Loop.Start()
}

func (g *AsyncHelper) StopLoop() {
	g.PromiseWG.Wait()
	g.Loop.Stop()
}

func runAndCatchAny(fn func() (any, error)) (out any, err error) {
	defer func() {
		if r := recover(); r != nil {
			// Preserve the error type if possible
			if e, ok := r.(error); ok {
				err = e
				return
			}
			err = fmt.Errorf("%v", r)
		}
	}()
	return fn()
}

// AsyncVal wraps a function that returns a value into a promise that resolves to the value if the function succeeds,
// or rejects with the error message if it fails.
func (g *AsyncHelper) AsyncVal(fn func() (any, error)) goja.Value {
	promise, resolve, reject := g.Vm.NewPromise()
	g.PromiseWG.Add(1)

	go func() {
		res, err := runAndCatchAny(fn)
		g.Loop.RunOnLoop(func(vm *goja.Runtime) {
			defer g.PromiseWG.Done()
			if err != nil {
				rejectErr := reject(vm.NewGoError(err))
				if rejectErr != nil {
					logx.FromContext(g.Ctx).WithError(rejectErr).Error("failed to reject promise")
				}
				return
			}
			resolveErr := resolve(vm.ToValue(res))
			if resolveErr != nil {
				logx.FromContext(g.Ctx).WithError(resolveErr).Error("failed to resolve promise")
			}
		})
	}()

	return g.Vm.ToValue(promise)
}

// AsyncOk wraps a function that returns an error into promise that resolves to true if the function succeeds,
// or rejects with the error message if it fails.
func (g *AsyncHelper) AsyncOK(fn func() error) goja.Value {
	return g.AsyncVal(func() (any, error) {
		if err := fn(); err != nil {
			return nil, err
		}
		return true, nil
	})
}

// ensureAsyncIteratorSymbol creates a global Symbol.asyncIterator if goja doesn't provide it.
func ensureAsyncIteratorSymbol(vm *goja.Runtime) (*goja.Symbol, error) {
	symObj := vm.Get("Symbol").ToObject(vm)
	v := symObj.Get("asyncIterator")
	if s, ok := v.(*goja.Symbol); ok {
		return s, nil
	}
	ai := goja.NewSymbol("Symbol.asyncIterator") // create our own well-known-like symbol
	if err := symObj.Set("asyncIterator", ai); err != nil {
		return nil, err
	}
	return ai, nil
}

// RunOnLoopBlock runs a function on the event loop and blocks until it completes,
// returning any error raised
func (g *AsyncHelper) RunOnLoopBlock(callback func(vm *goja.Runtime) error) error {
	wg := sync.WaitGroup{}
	wg.Add(1)
	var err error

	if !g.Loop.RunOnLoop(func(r *goja.Runtime) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("%v", r)
			}
		}()
		defer wg.Done()
		err = callback(r)
	}) {
		return errors.New("event loop is not running")
	}

	wg.Wait()
	return err
}

// RegisterAsyncIterator exposes an io.Reader as a JS async iterator.
// This function MUST be called from an event loop function if the event loop is running
func (g *AsyncHelper) RegisterAsyncIterator(vm *goja.Runtime, reader io.Reader) (goja.Value, error) {
	// Build the iterator object in JS
	iter := vm.NewObject()

	// Keep a closed flag so 'return()' can stop iteration early.
	closed := false

	readBuffer := make([]byte, 4096) // Adjust buffer size as needed

	// next(): Promise<{value, done}>
	setErr := iter.Set("next", func(call goja.FunctionCall) goja.Value {
		return g.AsyncVal(func() (any, error) {
			read, err := reader.Read(readBuffer)
			if err == io.EOF {
				return map[string]interface{}{
					"done": true,
				}, nil
			} else if err != nil {
				return nil, err
			}

			return map[string]interface{}{
				"value": vm.ToValue(string(readBuffer[:read])), // or keep as ArrayBuffer if you prefer
				"done":  false,
			}, nil
		})
	})
	if setErr != nil {
		return nil, setErr
	}

	// return(): allow early termination (e.g., break out of for-await-of)
	setErr = iter.Set("return", func(call goja.FunctionCall) goja.Value {
		if !closed {
			closed = true
		}
		// Must return a Promise-like or {done:true}; Promise is friendlier.
		p, resolve, _ := vm.NewPromise()
		resolveErr := resolve(vm.ToValue(map[string]interface{}{"done": true}))
		if resolveErr != nil {
			logx.FromContext(g.Ctx).WithError(resolveErr).Error("failed to resolve promise")
			panic(resolveErr)
		}
		return vm.ToValue(p)
	})
	if setErr != nil {
		return nil, setErr
	}

	asyncSym, err := ensureAsyncIteratorSymbol(vm)
	if err != nil {
		return nil, err
	}
	if err := iter.SetSymbol(asyncSym, func(call goja.FunctionCall) goja.Value { return call.This }); err != nil {
		return nil, err
	}

	return iter, nil
}

const helperSource = `
	// Polyfill-like helper: consume an async-iterable without "for await...of".
	function forAwait(iterable, onValue) {
		const it = (iterable && iterable[Symbol && Symbol.asyncIterator] 
					? iterable[Symbol && Symbol.asyncIterator]() 
					: iterable); // allow passing an iterator directly

		return new Promise((resolve, reject) => {
			function next() {
				let p;
				try { p = it.next(); } catch (e) { reject(e); return; }

				Promise.resolve(p).then(rec => {
					if (!rec || rec.done) { resolve(); return; }
					Promise.resolve()
						.then(() => onValue(rec.value))
						.then(() => { next(); }, e => {
							if (typeof it.return === "function") {
								Promise.resolve(it.return()).finally(() => reject(e));
							} else reject(e);
						});
				}, reject);
			}
			next();
		});
	}
`

// InjectAsyncHelpers injects helper functions for async iteration into the provided JS source code.
// forAwait(iterable, onValue) consumes an async iterable by calling onValue for each value.
// Use in conjunction with RegisterAsyncIterator to consume Go io.Readers in JS.
func InjectAsyncHelpers(src string) string {
	return helperSource + src
}

// HelperInjectedLineCount returns the number of lines in the injected helper source.
func HelperInjectedLineCount() int {
	return strings.Count(helperSource, "\n")
}

// ExecuteScriptedFunction calls a goja function, catches and panics and converts
// them to errors
func (ah *AsyncHelper) ExecuteScriptedFunction(
	exec func(goja.FunctionCall) goja.Value,
	vm *goja.Runtime,
	jsContext []goja.Value,
) (out goja.Value, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
	}()
	gojaValue := vm.ToValue(exec)
	callableFunc, ok := goja.AssertFunction(gojaValue)
	if !ok {
		return nil, fmt.Errorf("exec function is not callable")
	}
	out, err = callableFunc(goja.Undefined(), jsContext...)
	if err != nil {
		if ex, ok := err.(*goja.Exception); ok {
			err := ah.processGojaError(ex.Value().ToObject(vm))
			if se, ok := err.(*ScriptError); ok {
				logx.FromContext(ah.Ctx).Error(se.FormatStack())
			}
			return nil, err
		}
		return nil, err // JS exception
	}
	return out, nil
}

// CallScriptedFunction runs a bound JS function on the event loop.
// The function may be asynchronous or synchronous.
func (ah *AsyncHelper) CallScriptedFunction(stage func(goja.FunctionCall) goja.Value, args []goja.Value) (goja.Value, error) {
	type result struct {
		out goja.Value
		err error
	}
	ch := make(chan result, 1)

	ah.Loop.RunOnLoop(func(vm *goja.Runtime) {
		out, err := ah.ExecuteScriptedFunction(stage, vm, args)
		ch <- result{out: out, err: err}

	})
	res := <-ch

	if res.err != nil {
		return res.out, res.err
	}

	return awaitPromise(ah.Ctx, ah, res.out)
}

// Matches JS frames like:
//
//	at fn (file.js:12:3)
//	at file.tsx:45:7
//	at testFunc (<eval>:29:20(2))
//	at someOther (<anonymous>:10:5)
//
// Captures: func (1), file (2), line (3), col (4).
var jsTopFrameRegex = regexp.MustCompile(
	`(?m)^\s*at\s+(?:(\S+)\s+\()?(.+?):(\d+):(\d+)(?:\(\d+\))?\)?$`,
)

// ScriptStackEntry is one JS stack frame.
type ScriptStackEntry struct {
	File     string
	Function string
	Line     int
	Column   int
}

// ScriptError is a rich error carrying a message and structured JS stack.
type ScriptError struct {
	Message  string
	Cause    string            // used for structured error message
	Code     string            // used for structured error message
	Metadata map[string]string // used for structured error message
	Stack    []ScriptStackEntry
}

// Error returns just the message; presentation/formatting is opt-in elsewhere.
func (se *ScriptError) Error() string {
	if len(se.Stack) > 0 {
		stackEntry := se.Stack[0]
		return fmt.Sprintf("%s %s:%s:%d:%d", se.Message, stackEntry.File, stackEntry.Function, stackEntry.Line, stackEntry.Column)
	}
	return se.Message
}

// FormatStack renders the structured stack as "file:function:line:col" lines.
func (se *ScriptError) FormatStack() string {
	var b strings.Builder
	for i, fr := range se.Stack {
		if i > 0 {
			b.WriteByte('\n')
		}
		// Keep the simple, machine-parsable format.
		fmt.Fprintf(&b, "%s:%s:%d:%d", fr.File, fr.Function, fr.Line, fr.Column)
	}
	return b.String()
}

// parseAndAdjustFrame parses a single line of a JS stack and applies file/line fixups.
// Returns (entry, true) if the line is a JS frame; otherwise (_, false).
func (ah *AsyncHelper) parseAndAdjustFrame(line string) (ScriptStackEntry, bool) {
	m := jsTopFrameRegex.FindStringSubmatch(line)
	if m == nil {
		return ScriptStackEntry{}, false
	}

	funcName := m[1]
	fileName := m[2]
	lineNo, _ := strconv.Atoi(m[3])
	colNo, _ := strconv.Atoi(m[4])

	// Offset injected lines for accurate source lines.
	adjustedLine := lineNo - ah.LineOffset
	if adjustedLine < 1 {
		adjustedLine = 1
	}

	// Prefer configured SourceFileName for display; avoid leaking build paths.
	src := ah.SourceFileName
	if src == "" {
		src = fileName
	} else {
		src = path.Base(strings.ReplaceAll(src, `\`, `/`))
	}

	return ScriptStackEntry{
		File:     src,
		Function: funcName,
		Line:     adjustedLine,
		Column:   colNo,
	}, true
}

// ExtractStructuredMessage extracts "code", metadata and "cause" fields from a goja object, if present.
// If the code contains nested structered messages (i.e. `MessageImpl`), we only extract
// the top one. This is not ideal, but is sufficient for now.
// TODO: See APAP-3110 Extend JavaScript error throwing to support nesting
func ExtractStructuredMessage(obj *goja.Object) (code, cause string, metadata map[string]string) {
	codeVal := obj.Get("code")
	causeVal := obj.Get("cause")
	metadataVal := obj.Get("metadata")

	if codeVal != nil && !goja.IsUndefined(codeVal) && !goja.IsNull(codeVal) {
		code = codeVal.String()
	}

	if causeVal != nil && !goja.IsUndefined(causeVal) && !goja.IsNull(causeVal) {
		cause = causeVal.String()
	}

	if metadataVal != nil && !goja.IsUndefined(metadataVal) && !goja.IsNull(metadataVal) {
		if mo, ok := metadataVal.(*goja.Object); ok {
			keys := mo.Keys()
			if len(keys) > 0 {
				md := make(map[string]string, len(keys))
				for _, k := range keys {
					v := mo.Get(k)
					if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
						continue
					}
					md[k] = v.String()
				}
				if len(md) > 0 {
					metadata = md
				}
			}
		}
	}

	return code, cause, metadata
}

// gojaObjectToMessageImpl attempts to extract a MessageImpl from a goja object.
// Returns nil if MessageImpl is not present.
func gojaObjectToMessageImpl(obj *goja.Object) *message.MessageImpl {
	if obj == nil {
		return nil
	}

	val := obj.Get("value")
	if val == nil || goja.IsUndefined(val) || goja.IsNull(val) {
		return nil
	}

	err, ok := val.Export().(error)
	if !ok {
		return nil
	}
	return message.IsMessage(err)
}

// processGojaError extracts the given goja object to build either
// a MessageImpl or a ScriptError type error.Error object
func (ah *AsyncHelper) processGojaError(obj *goja.Object) error {
	// Check for MessageImpl first
	if msgImpl := gojaObjectToMessageImpl(obj); msgImpl != nil {
		return msgImpl
	}

	// Extract the message (don't bake formatting into it).
	var msg string
	if m := obj.Get("message"); m != nil && m != goja.Undefined() && m != goja.Null() {
		msg = m.String()
	} else {
		// Fallback: stringify the object
		msg = obj.String()
	}

	se := &ScriptError{Message: strings.TrimSpace(msg)}

	// Check for structured message fields.
	se.Code, se.Cause, se.Metadata = ExtractStructuredMessage(obj)

	// Try to extract a structured stack if available.
	if st := obj.Get("stack"); st != nil && st != goja.Undefined() && st != goja.Null() {
		raw := st.String()
		for _, ln := range strings.Split(raw, "\n") {
			if fr, ok := ah.parseAndAdjustFrame(strings.TrimRight(ln, "\r")); ok {
				se.Stack = append(se.Stack, fr)
			}
		}
	}

	return se
}

// awaitPromise waits for a JS Promise (if v is one) to settle, returning its
// resolved value or an error built from the rejection. Non-promises are returned as-is.
func awaitPromise(ctx context.Context, ah *AsyncHelper, v goja.Value) (goja.Value, error) {
	// If not a promise, nothing to do
	if _, ok := v.Export().(*goja.Promise); !ok {
		return v, nil
	}

	type settled struct {
		val goja.Value
		err error
	}
	ch := make(chan settled, 1) // buffered so VM thread never blocks on send

	// Attach then/catch on the VM thread.
	if err := ah.RunOnLoopBlock(func(vm *goja.Runtime) error {
		resolve := vm.ToValue(func(call goja.FunctionCall) goja.Value {
			ch <- settled{val: call.Argument(0)}
			return goja.Undefined()
		})

		reject := vm.ToValue(func(call goja.FunctionCall) goja.Value {
			val := call.Argument(0)
			if val == nil {
				ch <- settled{err: nil}
			} else {
				err := ah.processGojaError(val.ToObject(vm))
				if se, ok := err.(*ScriptError); ok {
					logx.FromContext(ah.Ctx).Error(se.FormatStack())
				}
				ch <- settled{err: err}
			}
			return goja.Undefined()
		})

		// p.then(resolve, reject)
		thenVal := v.ToObject(vm).Get("then")
		thenFn, ok := goja.AssertFunction(thenVal)
		if !ok {
			return fmt.Errorf("returned Promise is missing then()")
		}
		_, err := thenFn(v, resolve, reject)
		return err
	}); err != nil {
		return goja.Undefined(), err
	}

	// Wait for the promise to settle, or cancellation request
	select {
	case r := <-ch:
		if r.err != nil {
			return goja.Undefined(), r.err
		}
		return r.val, nil
	case <-ctx.Done():
		return goja.Undefined(), ctx.Err()
	}
}
