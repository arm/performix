// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package mocks

import (
	"context"
	"io"

	"github.com/stretchr/testify/mock"
	"google.golang.org/grpc/metadata"

	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

type MockAgentTether struct {
	mock.Mock
}

func (m *MockAgentTether) Start() error {
	args := m.Called()
	return args.Error(0)
}
func (m *MockAgentTether) OnTransportFailure(fn func(error)) {
	m.Called(fn)
}
func (m *MockAgentTether) Close() {
	m.Called()
}

type MockAgentLogger struct {
	mock.Mock
}

func (m *MockAgentLogger) Start() error {
	args := m.Called()
	return args.Error(0)
}
func (m *MockAgentLogger) Close() {
	m.Called()
}

// --- Mock stream implementing the generated interface ---
// Many gRPC stream client interfaces embed grpc.ClientStream, so we
// implement those methods as harmless stubs in addition to Recv().

type MockRetrieveFileStream struct {
	mock.Mock
}

// Recv returns the next FileChunk or an error.
func (m *MockRetrieveFileStream) Recv() (*targetagentproto.FileContent, error) {
	args := m.Called()
	chunk, _ := args.Get(0).(*targetagentproto.FileContent)
	return chunk, args.Error(1)
}

// ---- grpc.ClientStream compatibility stubs ----
func (m *MockRetrieveFileStream) Header() (metadata.MD, error) { return metadata.MD{}, nil }
func (m *MockRetrieveFileStream) Trailer() metadata.MD         { return metadata.MD{} }
func (m *MockRetrieveFileStream) CloseSend() error             { return nil }
func (m *MockRetrieveFileStream) Context() context.Context     { return context.Background() }
func (m *MockRetrieveFileStream) SendMsg(interface{}) error    { return nil }
func (m *MockRetrieveFileStream) RecvMsg(a interface{}) error  { return m.Called(a).Error(0) }

type ChunkErr struct {
	Bytes []byte
	Err   error
}

// MakeStream is aHelper to build a stream that will yield the provided sequence of chunks, then io.EOF.
func MakeStream(chunks ...[]byte) *MockRetrieveFileStream {
	ms := &MockRetrieveFileStream{}
	var err error
	for _, ch := range chunks {
		ms.On("RecvMsg", mock.Anything).Run(func(args mock.Arguments) {
			args.Get(0).(*targetagentproto.FileContent).Content = ch
		}).Return(err).Once().Return(nil)
	}
	ms.On("RecvMsg", mock.Anything).Run(func(args mock.Arguments) {
		args.Get(0).(*targetagentproto.FileContent).Content = []byte{}
	}).Return(io.EOF).Once().Return(nil)
	return ms
}

func SetStreamRecv(stream *MockRetrieveFileStream, content string, err error) {
	stream.On("RecvMsg", mock.Anything).Return(err).Run(func(args mock.Arguments) {
		args.Get(0).(*targetagentproto.FileContent).Content = []byte(content)
	}).Once()
}

type StubLogStream struct {
	Entries []any // either *targetagentproto.LogEntry or nil to simulate nil entry
	i       int
	Err     error // final error
}

func (s *StubLogStream) Recv() (*targetagentproto.LogEntry, error) {
	if s.i >= len(s.Entries) {
		if s.Err != nil {
			return nil, s.Err
		}
		return nil, context.Canceled
	}
	v := s.Entries[s.i]
	s.i++
	if v == nil {
		return nil, nil
	}
	return v.(*targetagentproto.LogEntry), nil
}
func (s *StubLogStream) Context() context.Context     { return context.Background() }
func (s *StubLogStream) Header() (metadata.MD, error) { return metadata.MD{}, nil }
func (s *StubLogStream) Trailer() metadata.MD         { return metadata.MD{} }
func (s *StubLogStream) SendMsg(interface{}) error    { return nil }
func (s *StubLogStream) RecvMsg(interface{}) error    { return nil }
func (s *StubLogStream) CloseSend() error             { return nil }

type MockHoldLockStream struct {
	mock.Mock
	Ctx    context.Context
	RecvFn func() (*targetagentproto.LockGranted, error)
}

func (m *MockHoldLockStream) Recv() (*targetagentproto.LockGranted, error) {
	if m.RecvFn != nil {
		return m.RecvFn()
	}
	args := m.Called()
	ev, _ := args.Get(0).(*targetagentproto.LockGranted)
	return ev, args.Error(1)
}

func (m *MockHoldLockStream) Header() (metadata.MD, error) { return nil, nil }
func (m *MockHoldLockStream) Trailer() metadata.MD         { return nil }
func (m *MockHoldLockStream) CloseSend() error             { return nil }
func (m *MockHoldLockStream) Context() context.Context {
	if m.Ctx == nil {
		return context.Background()
	}
	return m.Ctx
}

func (m *MockHoldLockStream) SendMsg(interface{}) error { return nil }
func (m *MockHoldLockStream) RecvMsg(interface{}) error { return nil }
