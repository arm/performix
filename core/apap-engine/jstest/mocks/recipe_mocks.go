// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package mocks

import (
	"errors"

	"github.com/dop251/goja"
	"github.com/stretchr/testify/mock"

	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipeparser"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
)

type RunCommandArg struct {
	Type       conductor.RunCommandType `json:"type"`
	Cmd        string                   `json:"cmd"`
	Venv       string                   `json:"venv"`
	RunAsAdmin bool                     `json:"runAsAdmin"`
}

func (command RunCommandArg) Validate() error {
	if command.Cmd == "" {
		return errors.New("run command cmd is required")
	}

	switch command.Type {
	case conductor.TypeExec:
		if command.Venv != "" {
			return errors.New("venv is not valid for exec commands")
		}
	case conductor.TypePython:
	default:
		return errors.New("invalid run command type")
	}

	return nil
}

// ReadyExecutionContext defines the recipe API exposed to ready stages.
type ReadyExecutionContext interface {
	ProbeTools(config recipeparser.RunToolConfigurationsArg) ([]tool.ProbeResult, error)
	GetWorkload() (recipeparser.WorkloadArg, error)
	GetParameter(id string) (any, error)
	GetTool(name string, version string) (recipeparser.ToolProperties, error)
	LogWarn(message string) error
	LogInfo(message string) error
	ReadHostFile(path string) (string, error)
	GetTelemetrySpecification(cpuName string) (goja.Value, error)
	TargetInfo() (goja.Value, error)
	RunCommand(command RunCommandArg) (conductor.RunCommandOutput, error)
	IsFullCaptureSupportEnabled() (bool, error)
}

type MockReadyExecutionContext struct {
	mock.Mock
}

func (m *MockReadyExecutionContext) ProbeTools(config recipeparser.RunToolConfigurationsArg) ([]tool.ProbeResult, error) {
	args := m.Called(config)
	result := args.Get(0)
	if result == nil {
		return nil, args.Error(1)
	}
	return result.([]tool.ProbeResult), args.Error(1)
}

func (m *MockReadyExecutionContext) GetWorkload() (recipeparser.WorkloadArg, error) {
	args := m.Called()
	return args.Get(0).(recipeparser.WorkloadArg), args.Error(1)
}

func (m *MockReadyExecutionContext) GetParameter(id string) (any, error) {
	args := m.Called(id)
	return args.Get(0), args.Error(1)
}

func (m *MockReadyExecutionContext) GetTool(name string, version string) (recipeparser.ToolProperties, error) {
	args := m.Called(name, version)
	return args.Get(0).(recipeparser.ToolProperties), args.Error(1)
}

func (m *MockReadyExecutionContext) LogWarn(message string) error {
	return m.Called(message).Error(0)
}

func (m *MockReadyExecutionContext) LogInfo(message string) error {
	return m.Called(message).Error(0)
}

func (m *MockReadyExecutionContext) ReadHostFile(path string) (string, error) {
	args := m.Called(path)
	return args.String(0), args.Error(1)
}

func (m *MockReadyExecutionContext) GetTelemetrySpecification(cpuName string) (goja.Value, error) {
	args := m.Called(cpuName)
	return args.Get(0).(goja.Value), args.Error(1)
}

func (m *MockReadyExecutionContext) TargetInfo() (goja.Value, error) {
	args := m.Called()
	return args.Get(0).(goja.Value), args.Error(1)
}

func (m *MockReadyExecutionContext) RunCommand(command RunCommandArg) (conductor.RunCommandOutput, error) {
	if err := command.Validate(); err != nil {
		return conductor.RunCommandOutput{}, err
	}
	args := m.Called(command)
	return args.Get(0).(conductor.RunCommandOutput), args.Error(1)
}

func (m *MockReadyExecutionContext) IsFullCaptureSupportEnabled() (bool, error) {
	args := m.Called()
	return args.Bool(0), args.Error(1)
}

// RunExecutionContext defines the recipe API exposed to run stages.
type RunExecutionContext interface {
	GetWorkload() (recipeparser.WorkloadArg, error)
	GetParameter(id string) (any, error)
	GetTool(name string, version string) (recipeparser.ToolProperties, error)
	RunTools(config recipeparser.RunToolConfigurationsArg) error
	TargetInfo() (goja.Value, error)
	LogWarn(message string) error
	LogInfo(message string) error
	WriteUserMessage(level string, message string) error
	ReadHostFile(path string) (string, error)
	GetTelemetrySpecification(cpuName string) (goja.Value, error)
	RetrieveFile(file recipeparser.FileArg) error
	RunCommand(command RunCommandArg) (conductor.RunCommandOutput, error)
	IsFullCaptureSupportEnabled() (bool, error)
}

type MockRunExecutionContext struct {
	mock.Mock
}

func (m *MockRunExecutionContext) GetWorkload() (recipeparser.WorkloadArg, error) {
	args := m.Called()
	return args.Get(0).(recipeparser.WorkloadArg), args.Error(1)
}

func (m *MockRunExecutionContext) GetParameter(id string) (any, error) {
	args := m.Called(id)
	return args.Get(0), args.Error(1)
}

func (m *MockRunExecutionContext) GetTool(name string, version string) (recipeparser.ToolProperties, error) {
	args := m.Called(name, version)
	return args.Get(0).(recipeparser.ToolProperties), args.Error(1)
}

func (m *MockRunExecutionContext) RunTools(config recipeparser.RunToolConfigurationsArg) error {
	return m.Called(config).Error(0)
}

func (m *MockRunExecutionContext) TargetInfo() (goja.Value, error) {
	args := m.Called()
	return args.Get(0).(goja.Value), args.Error(1)
}

func (m *MockRunExecutionContext) LogWarn(message string) error {
	return m.Called(message).Error(0)
}

func (m *MockRunExecutionContext) LogInfo(message string) error {
	return m.Called(message).Error(0)
}

func (m *MockRunExecutionContext) WriteUserMessage(level string, message string) error {
	return m.Called(level, message).Error(0)
}

func (m *MockRunExecutionContext) ReadHostFile(path string) (string, error) {
	args := m.Called(path)
	return args.String(0), args.Error(1)
}

func (m *MockRunExecutionContext) GetTelemetrySpecification(cpuName string) (goja.Value, error) {
	args := m.Called(cpuName)
	return args.Get(0).(goja.Value), args.Error(1)
}

func (m *MockRunExecutionContext) RetrieveFile(file recipeparser.FileArg) error {
	return m.Called(file).Error(0)
}

func (m *MockRunExecutionContext) RunCommand(command RunCommandArg) (conductor.RunCommandOutput, error) {
	if err := command.Validate(); err != nil {
		return conductor.RunCommandOutput{}, err
	}
	args := m.Called(command)
	return args.Get(0).(conductor.RunCommandOutput), args.Error(1)
}

func (m *MockRunExecutionContext) IsFullCaptureSupportEnabled() (bool, error) {
	args := m.Called()
	return args.Bool(0), args.Error(1)
}

// RenderExecutionContext defines the recipe API exposed to render stages.
type RenderExecutionContext interface {
	GetRunDescriptions() ([]recipeparser.RunDescription, error)
	GetPrimaryCpuName(runIndex int) (string, error)
	GetFirstSupportedCpuName(runIndex int) (goja.Value, error)
	ListRunComponents(runIndex int, componentGlob string) ([]recipeparser.RunComponentDescription, error)
	GetToolCapabilities(runIndex int, toolInvocation recipeparser.ToolInvocation) (recipeparser.JSToolCapabilities, error)
	GetRenderParameter(id string) (any, error)
	GetRenderParameters() (map[string]any, error)
	SetDefaultRenderParameter(id string, value any) error
	LogInfo(message string) error
	LogWarn(message string) error
	IsRerenderingEnabled() (bool, error)
	IsNeoprofTimelineEnabled() (bool, error)
}

type MockRenderExecutionContext struct {
	mock.Mock
}

func (m *MockRenderExecutionContext) GetRunDescriptions() ([]recipeparser.RunDescription, error) {
	args := m.Called()
	result := args.Get(0)
	if result == nil {
		return nil, args.Error(1)
	}
	return result.([]recipeparser.RunDescription), args.Error(1)
}

func (m *MockRenderExecutionContext) GetPrimaryCpuName(runIndex int) (string, error) {
	args := m.Called(runIndex)
	return args.String(0), args.Error(1)
}

func (m *MockRenderExecutionContext) GetFirstSupportedCpuName(runIndex int) (goja.Value, error) {
	args := m.Called(runIndex)
	return args.Get(0).(goja.Value), args.Error(1)
}

func (m *MockRenderExecutionContext) ListRunComponents(runIndex int, componentGlob string) ([]recipeparser.RunComponentDescription, error) {
	args := m.Called(runIndex, componentGlob)
	result := args.Get(0)
	if result == nil {
		return nil, args.Error(1)
	}
	return result.([]recipeparser.RunComponentDescription), args.Error(1)
}

func (m *MockRenderExecutionContext) GetToolCapabilities(runIndex int, toolInvocation recipeparser.ToolInvocation) (recipeparser.JSToolCapabilities, error) {
	args := m.Called(runIndex, toolInvocation)
	result := args.Get(0)
	if result == nil {
		return nil, args.Error(1)
	}
	return result.(recipeparser.JSToolCapabilities), args.Error(1)
}

func (m *MockRenderExecutionContext) GetRenderParameter(id string) (any, error) {
	args := m.Called(id)
	return args.Get(0), args.Error(1)
}

func (m *MockRenderExecutionContext) GetRenderParameters() (map[string]any, error) {
	args := m.Called()
	result := args.Get(0)
	if result == nil {
		return nil, args.Error(1)
	}
	return result.(map[string]any), args.Error(1)
}

func (m *MockRenderExecutionContext) SetDefaultRenderParameter(id string, value any) error {
	return m.Called(id, value).Error(0)
}

func (m *MockRenderExecutionContext) LogInfo(message string) error {
	return m.Called(message).Error(0)
}

func (m *MockRenderExecutionContext) LogWarn(message string) error {
	return m.Called(message).Error(0)
}

func (m *MockRenderExecutionContext) IsRerenderingEnabled() (bool, error) {
	args := m.Called()
	return args.Bool(0), args.Error(1)
}

func (m *MockRenderExecutionContext) IsNeoprofTimelineEnabled() (bool, error) {
	args := m.Called()
	return args.Bool(0), args.Error(1)
}
