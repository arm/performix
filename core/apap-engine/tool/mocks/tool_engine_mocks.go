// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package tool_mocks

import (
	"io"

	"github.com/stretchr/testify/mock"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
)

type MockEngineContext struct {
	mock.Mock
}

func (m *MockEngineContext) ExecCommand(opts *process.LaunchCommand) (*process.CommandResult, error) {
	args := m.Called(opts)
	return args.Get(0).(*process.CommandResult), args.Error(1)
}

func (m *MockEngineContext) StartProcess(opts *process.StartProcess) (tool.ProcessHandle, error) {
	args := m.Called(opts)
	return args.Get(0).(tool.ProcessHandle), args.Error(1)
}

func (m *MockEngineContext) CreateTempDir() (string, error) {
	args := m.Called()
	return args.String(0), args.Error(1)
}

func (m *MockEngineContext) CreateRunFile(path string) (tool.FileHandle, error) {
	args := m.Called(path)
	return args.Get(0).(tool.FileHandle), args.Error(1)
}

func (m *MockEngineContext) ReadHostFile(path string) (string, error) {
	args := m.Called(path)
	return args.String(0), args.Error(1)
}

func (m *MockEngineContext) Mkdir(path string) error { return m.Called(path).Error(0) }
func (m *MockEngineContext) Rm(path string, recursive, force bool) error {
	return m.Called(path, recursive, force).Error(0)
}
func (m *MockEngineContext) MakeWritable(path string, recursive bool) error {
	return m.Called(path, recursive).Error(0)
}
func (m *MockEngineContext) Chown(path, owner string, recursive bool) error {
	return m.Called(path, owner, recursive).Error(0)
}
func (m *MockEngineContext) Log(level string, message string)              { m.Called(level, message) }
func (m *MockEngineContext) WriteUserMessage(level string, message string) { m.Called(level, message) }
func (m *MockEngineContext) StartProgressTracker(id string) error          { return m.Called(id).Error(0) }
func (m *MockEngineContext) UpdateProgress(id, message string, percent float64) error {
	return m.Called(id, message, percent).Error(0)
}
func (m *MockEngineContext) EndProgress(id string) error { return m.Called(id).Error(0) }

func (m *MockEngineContext) GetPlatform() conductor.PlatformConfiguration {
	args := m.Called()
	return args.Get(0).(conductor.PlatformConfiguration)
}

type MockProcessHandle struct {
	mock.Mock
}

func (m *MockProcessHandle) PID() int         { return m.Called().Int(0) }
func (m *MockProcessHandle) Kill() error      { return m.Called().Error(0) }
func (m *MockProcessHandle) Interrupt() error { return m.Called().Error(0) }
func (m *MockProcessHandle) Wait() (int, error) {
	args := m.Called()
	return args.Int(0), args.Error(1)
}

func (m *MockProcessHandle) Stdout() io.Reader {
	reader, _ := m.Called().Get(0).(io.Reader)
	return reader
}

func (m *MockProcessHandle) Stderr() io.Reader {
	reader, _ := m.Called().Get(0).(io.Reader)
	return reader
}

func (m *MockProcessHandle) WriteStdin(data string) error { return m.Called(data).Error(0) }

type MockFileHandle struct {
	mock.Mock
	PathValue string
}

func (m *MockFileHandle) Append(data string) error {
	return m.Called(data).Error(0)
}

func (m *MockFileHandle) Close() error {
	return m.Called().Error(0)
}

func (m *MockFileHandle) Path() string {
	return m.PathValue
}

type ToolIntegrationMock struct {
	mock.Mock
}

func (m *ToolIntegrationMock) Properties() tool.IntegrationProperties {
	args := m.Called()
	return args.Get(0).(tool.IntegrationProperties)
}

func (m *ToolIntegrationMock) Probe() (tool.ProbeResult, error) {
	args := m.Called()
	return args.Get(0).(tool.ProbeResult), args.Error(1)
}

func (m *ToolIntegrationMock) StartRuntime() (cleanup func(), err error) {
	args := m.Called()
	return args.Get(0).(func()), args.Error(1)
}

func (m *ToolIntegrationMock) Run() error {
	args := m.Called()
	return args.Error(0)
}

func (m *ToolIntegrationMock) Stop() error {
	args := m.Called()
	return args.Error(0)
}

func (m *ToolIntegrationMock) Cancel() error {
	args := m.Called()
	return args.Error(0)
}

func (m *ToolIntegrationMock) Reformat() error {
	args := m.Called()
	return args.Error(0)
}

type MockFileCollector struct {
	mock.Mock
}

func (m *MockFileCollector) QueueFileRetrieval(outputEntityDir string, targetPath string, destRelativePath string, componentType cdf.ComponentType, transferOptions tool.TransferOptions) error {
	args := m.Called(outputEntityDir, targetPath, destRelativePath, componentType, transferOptions)
	return args.Error(0)
}

func (m *MockFileCollector) AddComponent(outputEntityDir string, componentType cdf.ComponentType, file string) (string, error) {
	args := m.Called(outputEntityDir, componentType, file)
	return args.String(0), args.Error(1)
}
