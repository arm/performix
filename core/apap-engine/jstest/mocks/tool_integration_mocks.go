// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package mocks

import (
	"github.com/dop251/goja"
	"github.com/stretchr/testify/mock"

	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
	tool_goja "github.com/Arm-Debug/apap-cli/apap-engine/tool/goja"
)

// ToolEngine defines the engine interface exposed to tool integrations.
type ToolEngine interface {
	ExecCommand(command []string, opts tool_goja.ExecOptions) goja.Value     // returns promise to process.CommandResult
	StartProcess(command []string, opts tool_goja.ProcessOptions) goja.Value // returns promise to a JS process handle (see ToolIntegrationJSHarness.ToJSProcessHandle)
	MonotonicNow() (float64, error)
	CreateTempDir() goja.Value                                  // returns promise to string
	PreserveTempDir(path string) goja.Value                     // returns promise to void
	MkDir(path string) goja.Value                               // returns promise to void
	Rm(path string, recursive bool, force bool) goja.Value      // returns promise to void
	MakeWritable(path string, recursive bool) goja.Value        // returns promise to void
	Chown(path string, owner string, recursive bool) goja.Value // returns promise to void
	EmitOutput(path string, relativePath string, metadata tool_goja.OutputMetadata, transferOptions tool.TransferOptions) error
	AddToolCapability(capabilityID string, metadata tool_goja.OutputMetadata, capabilityData tool_goja.CapabilityData) goja.Value // returns promise to void
	IsFullCaptureSupportEnabled() (bool, error)
	IsNeoprofTimelineEnabled() (bool, error)
	CreateRunFile(relativePath string, metadata tool_goja.OutputMetadata) goja.Value // returns promise to tool.FileHandle
	ReadHostFile(path string) goja.Value                                             // returns promise to string
	Log(level string, message string) error
	WriteUserMessage(level string, message string) error
	StartProgressTracker(id string) error
	UpdateProgress(id, message string, percent float64) error
	EndProgress(id string) error
	WithLocality(name string) (ToolEngine, error)
	GetLocality() (string, error)
	ToolsRoot() (string, error)
	CopyFrom(sourceLocality, sourcePath, destinationPath string) goja.Value // returns promise to void
	GetPlatform() (conductor.PlatformConfiguration, error)
	NotifyCollectionFinished() error
}

type MockToolEngine struct {
	mock.Mock
}

func (m *MockToolEngine) ExecCommand(command []string, opts tool_goja.ExecOptions) goja.Value {
	return m.Called(command, opts).Get(0).(goja.Value)
}

func (m *MockToolEngine) StartProcess(command []string, opts tool_goja.ProcessOptions) goja.Value {
	return m.Called(command, opts).Get(0).(goja.Value)
}

func (m *MockToolEngine) MonotonicNow() (float64, error) {
	args := m.Called()
	return args.Get(0).(float64), args.Error(1)
}

func (m *MockToolEngine) CreateTempDir() goja.Value {
	return m.Called().Get(0).(goja.Value)
}

func (m *MockToolEngine) PreserveTempDir(path string) goja.Value {
	return m.Called(path).Get(0).(goja.Value)
}

func (m *MockToolEngine) MkDir(path string) goja.Value {
	return m.Called(path).Get(0).(goja.Value)
}

func (m *MockToolEngine) Rm(path string, recursive bool, force bool) goja.Value {
	return m.Called(path, recursive, force).Get(0).(goja.Value)
}

func (m *MockToolEngine) MakeWritable(path string, recursive bool) goja.Value {
	return m.Called(path, recursive).Get(0).(goja.Value)
}

func (m *MockToolEngine) Chown(path string, owner string, recursive bool) goja.Value {
	return m.Called(path, owner, recursive).Get(0).(goja.Value)
}

func (m *MockToolEngine) EmitOutput(path string, relativePath string, metadata tool_goja.OutputMetadata, transferOptions tool.TransferOptions) error {
	return m.Called(path, relativePath, metadata, transferOptions).Error(0)
}

func (m *MockToolEngine) AddToolCapability(capabilityID string, metadata tool_goja.OutputMetadata, capabilityData tool_goja.CapabilityData) goja.Value {
	return m.Called(capabilityID, metadata, capabilityData).Get(0).(goja.Value)
}

func (m *MockToolEngine) IsFullCaptureSupportEnabled() (bool, error) {
	args := m.Called()
	return args.Bool(0), args.Error(1)
}

func (m *MockToolEngine) IsNeoprofTimelineEnabled() (bool, error) {
	args := m.Called()
	return args.Bool(0), args.Error(1)
}

func (m *MockToolEngine) CreateRunFile(relativePath string, metadata tool_goja.OutputMetadata) goja.Value {
	return m.Called(relativePath, metadata).Get(0).(goja.Value)
}

func (m *MockToolEngine) ReadHostFile(path string) goja.Value {
	return m.Called(path).Get(0).(goja.Value)
}

func (m *MockToolEngine) Log(level string, message string) error {
	return m.Called(level, message).Error(0)
}

func (m *MockToolEngine) WriteUserMessage(level string, message string) error {
	return m.Called(level, message).Error(0)
}

func (m *MockToolEngine) StartProgressTracker(id string) error {
	return m.Called(id).Error(0)
}

func (m *MockToolEngine) UpdateProgress(id string, message string, percent float64) error {
	return m.Called(id, message, percent).Error(0)
}

func (m *MockToolEngine) EndProgress(id string) error {
	return m.Called(id).Error(0)
}

func (m *MockToolEngine) WithLocality(name string) (ToolEngine, error) {
	args := m.Called(name)
	return args.Get(0).(ToolEngine), args.Error(1)
}

func (m *MockToolEngine) GetLocality() (string, error) {
	args := m.Called()
	return args.String(0), args.Error(1)
}

func (m *MockToolEngine) ToolsRoot() (string, error) {
	args := m.Called()
	return args.String(0), args.Error(1)
}

func (m *MockToolEngine) CopyFrom(sourceLocality string, sourcePath string, destinationPath string) goja.Value {
	return m.Called(sourceLocality, sourcePath, destinationPath).Get(0).(goja.Value)
}

func (m *MockToolEngine) GetPlatform() (conductor.PlatformConfiguration, error) {
	args := m.Called()
	return args.Get(0).(conductor.PlatformConfiguration), args.Error(1)
}

func (m *MockToolEngine) NotifyCollectionFinished() error {
	return m.Called().Error(0)
}

// ToolFileHandle defines the FileHandle interface as exposed to JS.
type ToolFileHandle interface {
	Append(chunk string) goja.Value // returns promise to void
	Close() goja.Value              // returns promise to void
	Path() (string, error)
}

type MockToolFileHandle struct {
	mock.Mock
}

func (m *MockToolFileHandle) Append(chunk string) goja.Value {
	return m.Called(chunk).Get(0).(goja.Value)
}

func (m *MockToolFileHandle) Close() goja.Value {
	return m.Called().Get(0).(goja.Value)
}

func (m *MockToolFileHandle) Path() (string, error) {
	args := m.Called()
	return args.String(0), args.Error(1)
}

// MockToolEngineIgnoreLogs creates a new mock tool engine,
// with the "Log" method pre-mocked to accept & ignore all
// calls.
func MockToolEngineIgnoreLogs() *MockToolEngine {
	m := MockToolEngine{}
	m.On("Log", mock.Anything, mock.Anything).Return(nil)
	return &m
}
