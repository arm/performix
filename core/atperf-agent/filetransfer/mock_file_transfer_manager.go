// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package filetransfer

import (
	"context"
	"io"

	"github.com/stretchr/testify/mock"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/emptypb"

	targetagentproto "github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

type MockFileTransferManager struct {
	mock.Mock
}

func (m *MockFileTransferManager) StoreFile(stream targetagentproto.TargetAgent_StoreFileServer) error {
	args := m.Called(stream)
	return args.Error(0)
}

func (m *MockFileTransferManager) RetrieveFile(req *targetagentproto.FileRequest, stream targetagentproto.TargetAgent_RetrieveFileServer) error {
	args := m.Called(req, stream)
	return args.Error(0)
}

type MockStoreFileClient struct {
	mock.Mock
}

func (m *MockStoreFileClient) Send(req *targetagentproto.StoreRequest) error {
	args := m.Called(req)
	return args.Error(0)
}

func (m *MockStoreFileClient) CloseAndRecv() (*emptypb.Empty, error) {
	args := m.Called()
	return args.Get(0).(*emptypb.Empty), args.Error(1)
}

func (m *MockStoreFileClient) Context() context.Context     { return context.Background() }
func (m *MockStoreFileClient) Header() (metadata.MD, error) { return metadata.MD{}, nil }
func (m *MockStoreFileClient) Trailer() metadata.MD         { return metadata.MD{} }
func (m *MockStoreFileClient) SendMsg(interface{}) error    { return nil }
func (m *MockStoreFileClient) RecvMsg(interface{}) error    { return nil }
func (m *MockStoreFileClient) CloseSend() error             { return nil }

type MockStoreFileServer struct {
	mock.Mock
	Reqs []*targetagentproto.StoreRequest
	Pos  int
}

func (m *MockStoreFileServer) Recv() (*targetagentproto.StoreRequest, error) {
	if m.Pos >= len(m.Reqs) {
		return nil, io.EOF
	}
	req := m.Reqs[m.Pos]
	m.Pos++
	return req, nil
}

func (m *MockStoreFileServer) SendAndClose(resp *emptypb.Empty) error {
	args := m.Called(resp)
	return args.Error(0)
}

func (m *MockStoreFileServer) Context() context.Context     { return context.Background() }
func (m *MockStoreFileServer) SendMsg(interface{}) error    { return nil }
func (m *MockStoreFileServer) RecvMsg(interface{}) error    { return nil }
func (m *MockStoreFileServer) Header() (metadata.MD, error) { return metadata.MD{}, nil }
func (m *MockStoreFileServer) Trailer() metadata.MD         { return metadata.MD{} }
func (m *MockStoreFileServer) SendHeader(metadata.MD) error { return nil }
func (m *MockStoreFileServer) SetHeader(metadata.MD) error  { return nil }
func (m *MockStoreFileServer) SetTrailer(metadata.MD)       {}

type MockRetrieveFileClient struct {
	mock.Mock
	Chunks []*targetagentproto.FileContent
	Pos    int
}

func (m *MockRetrieveFileClient) Recv() (*targetagentproto.FileContent, error) {
	if m.Pos >= len(m.Chunks) {
		return nil, io.EOF
	}
	resp := m.Chunks[m.Pos]
	m.Pos++
	return resp, nil
}

func (m *MockRetrieveFileClient) Context() context.Context     { return context.Background() }
func (m *MockRetrieveFileClient) Header() (metadata.MD, error) { return metadata.MD{}, nil }
func (m *MockRetrieveFileClient) Trailer() metadata.MD         { return metadata.MD{} }
func (m *MockRetrieveFileClient) SendMsg(interface{}) error    { return nil }
func (m *MockRetrieveFileClient) RecvMsg(interface{}) error    { return nil }
func (m *MockRetrieveFileClient) CloseSend() error             { return nil }

type MockRetrieveFileServer struct {
	mock.Mock
	ReceivedChunks [][]byte
}

func (m *MockRetrieveFileServer) Send(resp *targetagentproto.FileContent) error {
	m.ReceivedChunks = append(m.ReceivedChunks, resp.Content)
	args := m.Called(resp)
	return args.Error(0)
}

func (m *MockRetrieveFileServer) Context() context.Context     { return context.Background() }
func (m *MockRetrieveFileServer) SendMsg(interface{}) error    { return nil }
func (m *MockRetrieveFileServer) RecvMsg(interface{}) error    { return nil }
func (m *MockRetrieveFileServer) Header() (metadata.MD, error) { return metadata.MD{}, nil }
func (m *MockRetrieveFileServer) Trailer() metadata.MD         { return metadata.MD{} }
func (m *MockRetrieveFileServer) SendHeader(metadata.MD) error { return nil }
func (m *MockRetrieveFileServer) SetHeader(metadata.MD) error  { return nil }
func (m *MockRetrieveFileServer) SetTrailer(metadata.MD)       {}
