// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package tool_goja

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/dop251/goja"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/gojautils"
	"github.com/Arm-Debug/apap-cli/apap-engine/logging/logx"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
)

// BoundEngineContext exposes engine to JS with async helpers.
type BoundEngineContext struct {
	eng                     tool.Engine
	ic                      *tool.IntegrationContext
	asyncHelper             *gojautils.AsyncHelper
	fileCollector           tool.FileCollector
	registeredCapabilityIDs *capabilityIDRegistry
}

type capabilityIDRegistry struct {
	mu  sync.Mutex
	ids map[string]struct{}
}

func newCapabilityIDRegistry() *capabilityIDRegistry {
	return &capabilityIDRegistry{ids: make(map[string]struct{})}
}

func (r *capabilityIDRegistry) reserve(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.ids[id]; exists {
		return false
	}
	r.ids[id] = struct{}{}
	return true
}

func (r *capabilityIDRegistry) release(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.ids, id)
}

// Execution/Process option bags (JSON from JS).
type ExecOptions struct {
	AsPrivileged     bool              `json:"asPrivileged"`
	Affinity         []string          `json:"affinity"`
	WorkingDirectory string            `json:"workingDirectory"`
	Environment      map[string]string `json:"environment"`
}

type StreamRedirect struct {
	Redirect string `json:"redirect"`
	Path     string `json:"path"`
}

type ProcessOptions struct {
	AsPrivileged     bool              `json:"asPrivileged"`
	Affinity         []string          `json:"affinity"`
	WorkingDirectory string            `json:"workingDirectory"`
	Environment      map[string]string `json:"environment"`
	StdinOpen        bool              `json:"stdinOpen"`
	Stdout           StreamRedirect    `json:"stdout"`
	Stderr           StreamRedirect    `json:"stderr"`
}

// NewBoundEngineContext wires an engine + helper.
func NewBoundEngineContext(eng tool.Engine, asyncHelper *gojautils.AsyncHelper, ic *tool.IntegrationContext, fileCollector tool.FileCollector, registeredCapabilityIDs *capabilityIDRegistry) *BoundEngineContext {
	return &BoundEngineContext{
		eng:                     eng,
		asyncHelper:             asyncHelper,
		ic:                      ic,
		fileCollector:           fileCollector,
		registeredCapabilityIDs: registeredCapabilityIDs,
	}
}

// IsFullCaptureSupportEnabled returns whether the full capture support is enabled for this tool integration.
func (b *BoundEngineContext) IsFullCaptureSupportEnabled() bool {
	return b.ic.IsFullCaptureSupportEnabled
}

// IsNeoprofTimelineEnabled returns whether the neoprof timeline feature is enabled for this tool integration.
func (b *BoundEngineContext) IsNeoprofTimelineEnabled() bool {
	return b.ic.IsNeoprofTimelineEnabled
}

// ExecCommand runs a short-lived command via engine.
func (b *BoundEngineContext) ExecCommand(cmd goja.Value, opts goja.Value) (goja.Value, error) {
	eo := &ExecOptions{}
	if err := gojautils.ParseObjectFromJSWithRegex(opts, eo, []*regexp.Regexp{allowAllUnset}, nil); err != nil {
		err = fmt.Errorf("invalid value for argument 'opts': %w", err)
		panic(err)
	}
	response, err := b.eng.ExecCommand(&process.LaunchCommand{
		Command:          parseCommandArguments(cmd),
		AsPrivileged:     eo.AsPrivileged,
		Affinity:         eo.Affinity,
		WorkingDirectory: eo.WorkingDirectory,
		Environment:      eo.Environment,
	})
	if err != nil {
		return nil, err
	}
	var res *goja.Object
	err = b.asyncHelper.RunOnLoopBlock(func(vm *goja.Runtime) error {
		res = vm.NewObject()
		_ = res.Set("rc", int(response.Rc))
		_ = res.Set("stdout", response.Stdout)
		_ = res.Set("stderr", response.Stderr)
		return nil
	})
	return res, err
}

// BoundProcessHandle exposes a running process to JS.
type BoundProcessHandle struct {
	ph           tool.ProcessHandle
	asyncHelper  *gojautils.AsyncHelper
	stdoutReader BoundReader
	stderrReader BoundReader
	stdinOpen    bool
}

// BoundReader reads stream chunks asynchronously.
type BoundReader struct {
	asyncHelper *gojautils.AsyncHelper
	reader      io.Reader
	buffer      []byte
}

type BoundHostFile struct {
	handle      tool.FileHandle
	asyncHelper *gojautils.AsyncHelper
	closeErr    error
}

func (h *BoundHostFile) isClosed() bool {
	return h.closeErr != nil
}

// append writes data to the host file.
func (h *BoundHostFile) append(data string) goja.Value {
	return h.asyncHelper.AsyncOK(func() error {
		if h.isClosed() {
			return fmt.Errorf("host file handle is closed")
		}
		return h.handle.Append(data)
	})
}

// close closes the host file.
func (h *BoundHostFile) close() goja.Value {
	return h.asyncHelper.AsyncOK(func() error {
		if h.isClosed() {
			return h.closeErr
		}
		err := h.handle.Close()
		h.closeErr = err
		return err
	})
}

// wait resolves when the process exits.
func (b *BoundProcessHandle) wait() goja.Value {
	promise, resolve, reject := b.asyncHelper.Vm.NewPromise()
	b.asyncHelper.PromiseWG.Add(1)

	go func() {
		rc, err := b.ph.Wait()
		b.asyncHelper.Loop.RunOnLoop(func(vm *goja.Runtime) {
			defer b.asyncHelper.PromiseWG.Done()
			if err != nil {
				// Preserve the error type with NewGoError
				if rejectErr := reject(vm.NewGoError(err)); rejectErr != nil {
					logx.
						FromContext(b.asyncHelper.Ctx).
						WithError(rejectErr).
						Error("failed to reject promise")
				}
				return
			}
			exitCodeVal := vm.NewObject()
			_ = exitCodeVal.Set("exitCode", rc)
			if resolveErr := resolve(vm.ToValue(exitCodeVal)); resolveErr != nil {
				logx.FromContext(b.asyncHelper.Ctx).WithError(resolveErr).Error("failed to resolve promise")
			}
		})
	}()

	return b.asyncHelper.Vm.ToValue(promise)
}

// jsRedirectToStream maps JS redirect string → process mode.
func jsRedirectToStream(redirect string) process.RedirectMode {
	switch strings.ToLower(redirect) {
	case "":
		return process.None
	case "none":
		return process.None
	case "file":
		return process.File
	case "stream":
		return process.Stream
	case "both":
		return process.Both
	default:
		return -1 // Blank is valid, but unrecognized is not
	}
}

var allowAllUnset = regexp.MustCompile(`(.*?)`)

// StartProcess launches a long-running process via engine.
func (b *BoundEngineContext) StartProcess(cmd goja.Value, opts goja.Value) (goja.Value, error) {
	po := &ProcessOptions{}
	err := gojautils.ParseObjectFromJSWithRegex(opts, po, []*regexp.Regexp{allowAllUnset}, nil)
	if err != nil {
		err = fmt.Errorf("invalid value for argument 'opts': %w", err)
		panic(err)
	}

	stdoutRedirectMode := jsRedirectToStream(po.Stdout.Redirect)
	if stdoutRedirectMode == -1 {
		panic(fmt.Sprintf("stdout.redirect invalid mode: %s", po.Stdout.Redirect))
	}
	stderrRedirectMode := jsRedirectToStream(po.Stderr.Redirect)
	if stderrRedirectMode == -1 {
		panic(fmt.Sprintf("stderr.redirect invalid mode: %s", po.Stderr.Redirect))
	}

	stdinMode := func() process.StdinMode {
		if po.StdinOpen {
			return process.StdinBuffer
		}
		return process.StdinNone
	}()

	response, err := b.eng.StartProcess(&process.StartProcess{
		LaunchCommand: process.LaunchCommand{
			Command:          parseCommandArguments(cmd),
			AsPrivileged:     po.AsPrivileged,
			Affinity:         po.Affinity,
			WorkingDirectory: po.WorkingDirectory,
			Environment:      po.Environment,
		},
		Ctx:    b.asyncHelper.Ctx,
		Stdin:  stdinMode,
		Stdout: process.StreamRedirect{Mode: stdoutRedirectMode, FilePath: po.Stdout.Path},
		Stderr: process.StreamRedirect{Mode: stderrRedirectMode, FilePath: po.Stderr.Path},
	})
	if err != nil {
		panic(err)
	}

	bph := BoundProcessHandle{
		ph:          response,
		asyncHelper: b.asyncHelper,
		stdoutReader: BoundReader{
			asyncHelper: b.asyncHelper,
			reader:      response.Stdout(),
			buffer:      make([]byte, 1024),
		},
		stderrReader: BoundReader{
			asyncHelper: b.asyncHelper,
			reader:      response.Stderr(),
			buffer:      make([]byte, 1024),
		},
		stdinOpen: po.StdinOpen,
	}

	var processHandleBind *goja.Object

	// VM access needed, run on the event loop.
	err = b.asyncHelper.RunOnLoopBlock(func(vm *goja.Runtime) error {
		var bindErr error

		var stdoutIterator, stderrIterator goja.Value
		if process.IsStreamModeEnabled(stdoutRedirectMode) {
			stdoutIterator, bindErr = b.asyncHelper.RegisterAsyncIterator(vm, bph.stdoutReader.reader)
			if bindErr != nil {
				return bindErr
			}
		}
		if process.IsStreamModeEnabled(stderrRedirectMode) {
			stderrIterator, bindErr = b.asyncHelper.RegisterAsyncIterator(vm, bph.stderrReader.reader)
			if bindErr != nil {
				return bindErr
			}
		}

		processHandleBind = vm.NewObject()
		// Expose methods/props with lower-case names for JS.
		for _, ef := range []exposedFunction{
			{jsName: "pid", fn: vm.ToValue(response.PID)},
			{jsName: "kill", fn: vm.ToValue(bph.kill)},
			{jsName: "interrupt", fn: vm.ToValue(bph.interrupt)},
			{jsName: "wait", fn: vm.ToValue(bph.wait)},
			{jsName: "stdout", fn: stdoutIterator},
			{jsName: "stderr", fn: stderrIterator},
			{jsName: "writeStdin", fn: vm.ToValue(bph.writeStdin)},
		} {
			if bindErr = processHandleBind.Set(ef.jsName, ef.fn); bindErr != nil {
				return bindErr
			}
		}
		return nil
	})

	return processHandleBind, err
}

// parseCommandArguments converts a string or []interface{} from JS into a []string
func parseCommandArguments(cmd goja.Value) []string {
	cmdArgs := []string{}
	switch s := cmd.Export().(type) {
	case string:
		cmdArgs = []string{s}
	case []interface{}:
		cmdArgs = make([]string, len(s))
		for i, arg := range s {
			switch st := arg.(type) {
			case string:
				cmdArgs[i] = st
			default:
				panic("cmd arguments must be a string or array of strings")
			}
		}
	}
	return cmdArgs
}

// kill sends SIGKILL (or platform equivalent).
func (g *BoundProcessHandle) kill() goja.Value {
	return g.asyncHelper.AsyncOK(func() error {
		return g.ph.Kill()
	})
}

// interrupt sends SIGINT (or platform equivalent).
func (g *BoundProcessHandle) interrupt() goja.Value {
	return g.asyncHelper.AsyncOK(func() error {
		return g.ph.Interrupt()
	})
}

// writeStdin writes to the process stdin if open.
func (g *BoundProcessHandle) writeStdin(stdin string) goja.Value {
	return g.asyncHelper.AsyncOK(func() error {
		if !g.stdinOpen {
			return fmt.Errorf("stdin is not open for this process")
		}
		return g.ph.WriteStdin(stdin)
	})
}

// EmitOutput publishes a file from the tool with metadata.
type OutputMetadata struct {
	ComponentType string `json:"name"`
	Version       string `json:"version"`
}

type TransferOptions struct {
	ImmediateRetrieval bool     `json:"immediateRetrieval"`
	Exclude            []string `json:"exclude"`
	BackgroundTransfer bool     `json:"backgroundTransfer"`
}

func (b *BoundEngineContext) EmitOutput(path, relativePath string, meta goja.Value, transferOptions goja.Value) error {
	om := &OutputMetadata{}
	if err := gojautils.ParseObjectFromJS(meta, &om); err != nil {
		return err
	}
	componentType := cdf.ComponentType{Name: om.ComponentType, SchemaVersion: om.Version}

	to := &TransferOptions{}
	allowedUnset := []*regexp.Regexp{
		regexp.MustCompile(`^immediateRetrieval$`),
		regexp.MustCompile(`^exclude$`),
		regexp.MustCompile(`^backgroundTransfer$`),
	}
	if transferOptions != nil {
		if err := gojautils.ParseObjectFromJSWithRegex(transferOptions, &to, allowedUnset, []*regexp.Regexp{}); err != nil {
			return err
		}
	}
	return b.fileCollector.QueueFileRetrieval(b.ic.OutputEntityDir, path, relativePath, componentType, tool.TransferOptions{
		ImmediateRetrieval: to.ImmediateRetrieval,
		Exclude:            to.Exclude,
		BackgroundTransfer: to.BackgroundTransfer,
	})
}

// CreateRunFile creates a file on the host for writing.
func (b *BoundEngineContext) CreateRunFile(relativePath string, meta goja.Value) (goja.Value, error) {
	om := &OutputMetadata{}
	if err := gojautils.ParseObjectFromJS(meta, &om); err != nil {
		return nil, err
	}
	componentType := cdf.ComponentType{Name: om.ComponentType, SchemaVersion: om.Version}
	path, err := b.fileCollector.AddComponent(b.ic.OutputEntityDir, componentType, relativePath)
	if err != nil {
		return nil, err
	}

	hostHandle, err := b.eng.CreateRunFile(path)
	if err != nil {
		return nil, err
	}

	handle := &BoundHostFile{
		handle:      hostHandle,
		asyncHelper: b.asyncHelper,
	}

	var res *goja.Object
	err = b.asyncHelper.RunOnLoopBlock(func(vm *goja.Runtime) error {
		res = vm.NewObject()
		_ = res.Set("append", vm.ToValue(handle.append))
		_ = res.Set("close", vm.ToValue(handle.close))
		_ = res.Set("path", hostHandle.Path())
		return nil
	})
	if err != nil {
		_ = hostHandle.Close()
		return nil, err
	}

	return res, nil
}

// ReadHostFile reads a file from the host and returns its contents.
func (b *BoundEngineContext) ReadHostFile(path string) (string, error) {
	return b.eng.ReadHostFile(path)
}

type CapabilityData struct {
	State   string         `json:"state"`
	Payload map[string]any `json:"payload"`
}

func (b *BoundEngineContext) AddToolCapability(capabilityId string, gojaComponentType goja.Value, gojaCapabilityData goja.Value) error {
	if err := b.addToolCapability(capabilityId, gojaComponentType, gojaCapabilityData); err != nil {
		return fmt.Errorf("addToolCapability: %w", err)
	}
	return nil
}

func (b *BoundEngineContext) addToolCapability(capabilityId string, gojaComponentType goja.Value, gojaCapabilityData goja.Value) (returnErr error) {
	// Validate and convert capability contents
	capabilityData := &CapabilityData{}
	err := gojautils.ParseObjectFromJS(gojaCapabilityData, capabilityData)
	if err != nil {
		return err
	}
	contentsBytes, err := util.EncodeJSON[CapabilityData](capabilityData)
	if err != nil {
		return err
	}
	contentsString := string(contentsBytes)

	// Validate capability ID
	if !isCapabilityIDValid(capabilityId) {
		return fmt.Errorf("invalid capability ID %q; must start with a letter or number and contain only letters, numbers, and the following symbols: ._-", capabilityId)
	}
	if !b.registeredCapabilityIDs.reserve(capabilityId) {
		return fmt.Errorf("capability ID %q has already been registered", capabilityId)
	}

	// Create manifest entry
	om := &OutputMetadata{}
	if err := gojautils.ParseObjectFromJS(gojaComponentType, &om); err != nil {
		b.registeredCapabilityIDs.release(capabilityId)
		return err
	}
	componentType := cdf.ComponentType{Name: om.ComponentType, SchemaVersion: om.Version}

	relativePath := filepath.Join("capabilities", fmt.Sprintf("%v.json", capabilityId))
	absolutePath, err := b.fileCollector.AddComponent(b.ic.OutputEntityDir, componentType, relativePath)
	if err != nil {
		b.registeredCapabilityIDs.release(capabilityId)
		return err
	}
	// note: from now on, we no longer release the ID on failure as the manifest entry has already been recorded

	// Create run file
	hostHandle, err := b.eng.CreateRunFile(absolutePath)
	if err != nil {
		return err
	}

	defer func() {
		returnErr = errors.Join(returnErr, hostHandle.Close())
	}()
	if err = hostHandle.Append(contentsString + "\n"); err != nil {
		return err
	}
	return nil
}

func isCapabilityIDValid(id string) bool {
	var validCapabilityID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	return validCapabilityID.MatchString(id)
}

func (b *BoundEngineContext) GetPlatform() map[string]any {
	platform := b.eng.GetPlatform()

	return map[string]any{
		"Architecture": string(platform.Architecture),
		"OS":           string(platform.OS),
	}
}
