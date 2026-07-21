// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package mocks

import (
	"github.com/stretchr/testify/mock"

	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

type MockQueryProcessor struct {
	mock.Mock
}

func (m *MockQueryProcessor) ExecuteQuery(client apapproto.ApapClient, sessionID string, format apapproto.TableFormat, queryStr string) ([]*apapproto.QueryResponse, error) {
	mockArgs := m.Called(client, sessionID, format, queryStr)
	return mockArgs.Get(0).([]*apapproto.QueryResponse), mockArgs.Error(1)
}

type MockRenderCloser struct {
	mock.Mock
}

func (m *MockRenderCloser) CloseRender(client apapproto.ApapClient, sessionID string) error {
	mockArgs := m.Called(client, sessionID)
	return mockArgs.Error(0)
}

type MockChunkWriterCloser struct {
	mock.Mock
}

func (m *MockChunkWriterCloser) WriteChunk(chunk *apapproto.TableChunk) error {
	args := m.Called(chunk)
	return args.Error(0)
}

func (m *MockChunkWriterCloser) Close() error {
	args := m.Called()
	return args.Error(0)
}
