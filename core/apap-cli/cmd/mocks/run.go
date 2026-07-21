// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package mocks

import (
	"github.com/stretchr/testify/mock"

	"github.com/Arm-Debug/apap-cli/apap-cli/service/clijson"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

type MockRunResolver struct {
	mock.Mock
}

func (m *MockRunResolver) ResolveRun(client apapproto.ApapClient, name string) (*apapproto.RunId, error) {
	mockArgs := m.Called(client, name)
	mockResult := mockArgs.Get(0).(*apapproto.RunId)
	return mockResult, mockArgs.Error(1)
}

type MockLister struct {
	mock.Mock
}

func (m *MockLister) ListRuns(client apapproto.ApapClient) (clijson.CLIRunListing, error) {
	mockArgs := m.Called(client)
	mockResult := mockArgs.Get(0).(clijson.CLIRunListing)
	return mockResult, mockArgs.Error(1)
}

type MockInfo struct {
	mock.Mock
}

func (m *MockInfo) ListRun(client apapproto.ApapClient, entry *apapproto.GetRunDescriptionRequest) (clijson.CLIRunDescription, error) {
	mockArgs := m.Called(client, entry)
	mockResult := mockArgs.Get(0).(clijson.CLIRunDescription)
	return mockResult, mockArgs.Error(1)
}

type MockRenameService struct {
	mock.Mock
}

func (m *MockRenameService) RenameRun(client apapproto.ApapClient, entry *apapproto.RunId, newName string) (*apapproto.RunRenameResponse, error) {
	mockArgs := m.Called(client, entry, newName)
	mockResult := mockArgs.Get(0).(*apapproto.RunRenameResponse)
	return mockResult, mockArgs.Error(1)
}

type MockUpdateService struct {
	mock.Mock
}

func (m *MockUpdateService) UpdateRuns(client apapproto.ApapClient, request *apapproto.UpdateRunsRequest) (*apapproto.UpdateRunsResponse, error) {
	mockArgs := m.Called(client, request)
	mockResult := mockArgs.Get(0).(*apapproto.UpdateRunsResponse)
	return mockResult, mockArgs.Error(1)
}

type MockDeleter struct {
	mock.Mock
}

func (m *MockDeleter) DeleteRuns(client apapproto.ApapClient, request *apapproto.DeleteRunsRequest) (*apapproto.DeleteRunsResponse, error) {
	mockArgs := m.Called(client, request)
	mockResult := mockArgs.Get(0).(*apapproto.DeleteRunsResponse)
	return mockResult, mockArgs.Error(1)
}

type MockExporter struct {
	mock.Mock
}

func (m *MockExporter) ExportRun(client apapproto.ApapClient, request *apapproto.RunExportRequest) (*apapproto.RunExportResponse, error) {
	mockArgs := m.Called(client, request)
	mockResult := mockArgs.Get(0).(*apapproto.RunExportResponse)
	return mockResult, mockArgs.Error(1)
}

type MockImporter struct {
	mock.Mock
}

func (m *MockImporter) ImportRun(client apapproto.ApapClient, request *apapproto.RunImportRequest) (*apapproto.RunImportResponse, error) {
	mockArgs := m.Called(client, request)
	mockResult := mockArgs.Get(0).(*apapproto.RunImportResponse)
	return mockResult, mockArgs.Error(1)
}
