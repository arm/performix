// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package jstest

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/dop251/goja"
	gojarequire "github.com/dop251/goja_nodejs/require"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/gojautils"
	"github.com/Arm-Debug/apap-cli/apap-engine/jstest/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
	tool_goja "github.com/Arm-Debug/apap-cli/apap-engine/tool/goja"
)

// ------------------------
// Harness definition
// ------------------------

// ToolIntegrationJSHarness allows accessing the properties and calling
// the methods of a tool defined in a tool integration file, as well as
// any global functions.
type ToolIntegrationJSHarness struct {
	*GenericJSHarness
	binding tool_goja.GojaToolBinding
}

// ------------------------
// Harness calling methods
// ------------------------

// ToolProperties returns the declarative properties of the tool.
func (th *ToolIntegrationJSHarness) ToolProperties() tool.IntegrationProperties {
	return tool.IntegrationProperties{
		Name:                   th.binding.Name,
		Version:                th.binding.Version,
		SupportsWorkloadLaunch: th.binding.SupportsWorkloadLaunch,
		ShortDescription:       th.binding.Description.Short,
		LongDescription:        th.binding.Description.Long,
		Deployments:            th.binding.Deployments,
		Migrations:             th.binding.Migrations,
	}
}

// ToolProbe calls the tool's probe method, supplying the provided tool engine
// and tool context as arguments. It returns the tool's ProbeResult response,
// as well as any exception that may have been thrown.
func (th *ToolIntegrationJSHarness) ToolProbe(t *testing.T, engine mocks.ToolEngine, toolContext tool_goja.ToolContext) (tool.ProbeResult, error) {
	t.Helper()

	th.callMu.Lock()
	defer th.callMu.Unlock()

	var probeResult tool.ProbeResult
	result, err := th.callAwaitFunction(t, th.binding.Probe, engine, toolContext)
	if err != nil {
		return probeResult, err
	}

	err = th.asyncHelper.RunOnLoopBlock(func(_ *goja.Runtime) error {
		allowedUnset := []*regexp.Regexp{regexp.MustCompile(`metadata|cause`)}
		return gojautils.ParseObjectFromJSWithRegex(result, &probeResult, allowedUnset, nil)
	})
	require.NoError(t, err, "failed to convert tool probe result")

	return probeResult, nil
}

// ToolRun calls the tool's run method, supplying the provided tool engine
// and tool context as arguments. It returns any exception that may have
// been thrown.
func (th *ToolIntegrationJSHarness) ToolRun(t *testing.T, engine mocks.ToolEngine, toolContext tool_goja.ToolContext) error {
	t.Helper()

	th.callMu.Lock()
	defer th.callMu.Unlock()

	_, err := th.callAwaitFunction(t, th.binding.Run, engine, toolContext)
	return err
}

// ToolReformat calls the tool's reformat method, supplying the provided tool
// engine and tool context as arguments. It returns any exception that may have
// been thrown.
func (th *ToolIntegrationJSHarness) ToolReformat(t *testing.T, engine mocks.ToolEngine, toolContext tool_goja.ToolContext) error {
	t.Helper()

	th.callMu.Lock()
	defer th.callMu.Unlock()

	_, err := th.callAwaitFunction(t, th.binding.Reformat, engine, toolContext)
	return err
}

// ToolStop calls the tool's stop method, supplying the provided tool engine
// and tool context as arguments. It returns any exception that may have
// been thrown.
func (th *ToolIntegrationJSHarness) ToolStop(t *testing.T, engine mocks.ToolEngine, toolContext tool_goja.ToolContext) error {
	t.Helper()

	th.callMu.Lock()
	defer th.callMu.Unlock()

	_, err := th.callAwaitFunction(t, th.binding.OnStop, engine, toolContext)
	return err
}

// ToolCancel calls the tool's cancel method, supplying the provided tool
// engine and tool context as arguments. It returns any exception that may
// have been thrown.
func (th *ToolIntegrationJSHarness) ToolCancel(t *testing.T, engine mocks.ToolEngine, toolContext tool_goja.ToolContext) error {
	t.Helper()

	th.callMu.Lock()
	defer th.callMu.Unlock()

	_, err := th.callAwaitFunction(t, th.binding.OnCancel, engine, toolContext)
	return err
}

// ------------------------
// Helpers
// ------------------------

// ToJSProcessHandle converts an engine process handle into the JavaScript
// ProcessHandle shape.
//
// When mocking a method whose JS definition returns a ProcessHandle, mock
// out an engine process handle, and use this helper to convert this into
// the correct return type.
//
// Note this method does *not* return a promise to the JS ProcessHandle, it
// returns the handle itself. If you need to return a promise to the handle,
// combine this method with GenericJSHarness.ToJSValPromise.
func (th *ToolIntegrationJSHarness) ToJSProcessHandle(
	t *testing.T,
	handle tool.ProcessHandle,
	options tool_goja.ProcessHandleBindingOptions,
) goja.Value {
	obj, err := tool_goja.BindProcessHandle(th.asyncHelper, handle, options)
	assert.NoError(t, err, "failed to construct JS process handle from engine process handle")
	return obj
}

// ToJSProcessHandleNoOptions works in the same way as ToJSProcessHandle, but
// supplies empty options. This means the `stdout` and `stderr` properties of
// the process handle will not be defined, and writeStdin will reject promises
// immediately.
//
// Use this method when mocking a process handle for which stdout, stderr and
// stdin are not needed.
func (th *ToolIntegrationJSHarness) ToJSProcessHandleNoOptions(
	t *testing.T,
	handle tool.ProcessHandle,
) goja.Value {
	return th.ToJSProcessHandle(t, handle, tool_goja.ProcessHandleBindingOptions{})
}

// ------------------------
// Loader
// ------------------------

// LoadToolIntegration loads a tool integration of the specified name from the
// `tool-integrations` directory. It returns a harness which allows calling of
// any global functions in the tool integration file, as well as curated methods
// for accessing the tool integration's properties and calling its methods.
func LoadToolIntegration(t *testing.T, toolIntegrationName string) *ToolIntegrationJSHarness {
	t.Helper()

	// Load tool int
	resolvedPath := resolveJSFilePath(t, filepath.Join("tool-integrations", toolIntegrationName+".js"))
	data, err := os.ReadFile(resolvedPath)
	require.NoError(t, err, "failed to read requested tool integration")

	scriptedSource, err := tool_goja.LoadFromSource(string(data), resolvedPath)
	require.NoError(t, err, "failed to parse requested tool integration")

	// Create new generic JS harness to support arbitrary function execution
	registry := gojarequire.NewRegistry()
	genericHarness := newGenericJSHarness(
		t,
		scriptedSource.FileName,
		scriptedSource.LineOffset,
		registry,
		plainScript,
	)
	harness := &ToolIntegrationJSHarness{GenericJSHarness: genericHarness}

	// Execute tool int + get tool object
	err = genericHarness.asyncHelper.RunOnLoopBlock(func(vm *goja.Runtime) error {
		binding, err := scriptedSource.LoadBinding(vm)
		if err != nil {
			return err
		}
		harness.binding = binding

		return nil
	})
	require.NoError(t, err, "failed to load requested tool integration")

	return harness
}
