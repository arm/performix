// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/query"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

func TestExecuteOptionsForFormat(t *testing.T) {
	opts, err := executeOptionsForFormat(query.TableFormatProtobufStruct)
	require.NoError(t, err)
	require.Equal(t, query.TableFormatProtobufStruct, opts.Format)

	opts, err = executeOptionsForFormat(query.TableFormatArrowIPC)
	require.NoError(t, err)
	require.Equal(t, query.TableFormatArrowIPC, opts.Format)

	_, err = executeOptionsForFormat("bad-format")
	require.Error(t, err)
}

type mockQueryTable struct {
	mock.Mock
}

func (m *mockQueryTable) Format() query.TableFormat {
	args := m.Called()
	return args.Get(0).(query.TableFormat)
}

func (m *mockQueryTable) ColumnCount() int {
	args := m.Called()
	return args.Int(0)
}

func (m *mockQueryTable) ColumnDescription(i int) query.ColumnDescription {
	args := m.Called(i)
	return args.Get(0).(query.ColumnDescription)
}

func (m *mockQueryTable) Close() error {
	args := m.Called()
	return args.Error(0)
}

type mockProtoStructTable struct {
	mock.Mock
}

func (m *mockProtoStructTable) Format() query.TableFormat {
	args := m.Called()
	return args.Get(0).(query.TableFormat)
}

func (m *mockProtoStructTable) NextChunk() ([]*structpb.Struct, error) {
	args := m.Called()
	if result := args.Get(0); result != nil {
		return result.([]*structpb.Struct), args.Error(1)
	} else {
		return nil, args.Error(1)
	}
}

func (m *mockProtoStructTable) ColumnCount() int {
	args := m.Called()
	return args.Int(0)
}

func (m *mockProtoStructTable) ColumnDescription(i int) query.ColumnDescription {
	args := m.Called(i)
	return args.Get(0).(query.ColumnDescription)
}

func (m *mockProtoStructTable) Close() error {
	args := m.Called()
	return args.Error(0)
}

type mockByteStreamTable struct {
	mock.Mock
}

func (m *mockByteStreamTable) Format() query.TableFormat {
	args := m.Called()
	return args.Get(0).(query.TableFormat)
}

func (m *mockByteStreamTable) ColumnCount() int {
	args := m.Called()
	return args.Int(0)
}

func (m *mockByteStreamTable) ColumnDescription(i int) query.ColumnDescription {
	args := m.Called(i)
	return args.Get(0).(query.ColumnDescription)
}

func (m *mockByteStreamTable) OpenReader() (io.ReadCloser, error) {
	args := m.Called()
	return args.Get(0).(io.ReadCloser), args.Error(1)
}

func (m *mockByteStreamTable) Close() error {
	args := m.Called()
	return args.Error(0)
}

type mockQueryStream struct {
	mock.Mock
	grpc.ServerStream
}

func (m *mockQueryStream) Send(resp *apapproto.QueryResponse) error {
	args := m.Called(resp)
	return args.Error(0)
}

type errReader struct{}

func (errReader) Read(p []byte) (int, error) { return 0, errors.New("boom") }

func TestMarshalTableColumns(t *testing.T) {
	mockTable := new(mockQueryTable) // implement query.TableAccessor as a mock

	mockTable.On("ColumnCount").Return(2)
	mockTable.On("ColumnDescription", 0).Return(query.ColumnDescription{Name: "id"})
	mockTable.On("ColumnDescription", 1).Return(query.ColumnDescription{Name: "name"})
	mockTable.On("Close").Return(nil)

	result := marshalTableColumns(mockTable)

	assert.Len(t, result.Columns, 2)
	assert.Equal(t, "id", result.Columns[0].Name)
	assert.Equal(t, "name", result.Columns[1].Name)
}

func TestStreamProtoStructTable(t *testing.T) {
	chunks := [][]*structpb.Struct{
		{
			structpb.NewStructValue(&structpb.Struct{Fields: map[string]*structpb.Value{
				"id": structpb.NewNumberValue(1),
			}}).GetStructValue(),
		},
		{
			structpb.NewStructValue(&structpb.Struct{Fields: map[string]*structpb.Value{
				"id": structpb.NewNumberValue(2),
			}}).GetStructValue(),
		},
	}

	mockTable := new(mockProtoStructTable)
	mockTable.On("NextChunk").Return(chunks[0], nil).Once()
	mockTable.On("NextChunk").Return(chunks[1], nil).Once()
	mockTable.On("NextChunk").Return(nil, io.EOF).Once()
	mockTable.On("ColumnCount").Return(1)
	mockTable.On("ColumnDescription", 0).Return(query.ColumnDescription{Name: "id"})
	mockTable.On("Format").Return(query.TableFormatProtobufStruct)

	mockStream := new(mockQueryStream)
	var sentMessages []*apapproto.QueryResponse
	mockStream.On("Send", mock.Anything).Return(nil).Run(func(args mock.Arguments) {
		sentMessages = append(sentMessages, args.Get(0).(*apapproto.QueryResponse))
	})

	req := &apapproto.QueryRequest{QuerySql: "SELECT * FROM test"}

	err := streamProtoStructTable(req, mockStream, mockTable)
	assert.NoError(t, err)
	assert.Len(t, sentMessages, 3)
	assert.NotNil(t, sentMessages[0].GetDescription())
	assert.Len(t, sentMessages[0].GetDescription().Columns.Columns, 1)
	assert.Equal(t, "id", sentMessages[0].GetDescription().Columns.Columns[0].Name)

	assert.NotNil(t, sentMessages[1].GetChunk().GetStructChunk())
	assert.Equal(t, sentMessages[1].GetChunk().GetStructChunk().Rows, chunks[0])

	assert.NotNil(t, sentMessages[2].GetChunk().GetStructChunk())
	assert.Equal(t, sentMessages[2].GetChunk().GetStructChunk().Rows, chunks[1])

	mockTable.AssertExpectations(t)
}

func TestStreamProtoStructTable_EmptyResultSet(t *testing.T) {
	mockTable := new(mockProtoStructTable)
	mockTable.On("NextChunk").Return([]*structpb.Struct{}, nil).Once() // first chunk: empty
	mockTable.On("NextChunk").Return(nil, io.EOF).Once()               // second call: EOF
	mockTable.On("ColumnCount").Return(1)
	mockTable.On("ColumnDescription", 0).Return(query.ColumnDescription{Name: "id"})
	mockTable.On("Format").Return(query.TableFormatProtobufStruct)

	mockStream := new(mockQueryStream)
	var sentMessages []*apapproto.QueryResponse
	mockStream.On("Send", mock.Anything).Return(nil).Run(func(args mock.Arguments) {
		sentMessages = append(sentMessages, args.Get(0).(*apapproto.QueryResponse))
	})

	req := &apapproto.QueryRequest{QuerySql: "SELECT * FROM test"}

	err := streamProtoStructTable(req, mockStream, mockTable)
	assert.NoError(t, err)

	// Should send one message (with columns but no rows)
	assert.Len(t, sentMessages, 1)
	first := sentMessages[0]
	description := first.GetDescription()
	assert.NotNil(t, description)
	assert.Len(t, description.Columns.Columns, 1)
	assert.Equal(t, "id", description.Columns.Columns[0].Name)

	mockTable.AssertExpectations(t)
}

func TestStreamProtoStructTable_ImmediateEOF(t *testing.T) {
	mockTable := new(mockProtoStructTable)
	mockTable.On("NextChunk").Return(nil, io.EOF).Once()
	mockTable.On("ColumnCount").Return(1)
	mockTable.On("ColumnDescription", 0).Return(query.ColumnDescription{Name: "id"})
	mockTable.On("Format").Return(query.TableFormatProtobufStruct)

	mockStream := new(mockQueryStream)
	var sentMessages []*apapproto.QueryResponse
	mockStream.On("Send", mock.Anything).Return(nil).Run(func(args mock.Arguments) {
		sentMessages = append(sentMessages, args.Get(0).(*apapproto.QueryResponse))
	})

	req := &apapproto.QueryRequest{QuerySql: "SELECT * FROM test"}

	err := streamProtoStructTable(req, mockStream, mockTable)
	assert.NoError(t, err)

	assert.Len(t, sentMessages, 1)
	first := sentMessages[0]

	description := first.GetDescription()
	assert.NotNil(t, description)
	assert.Len(t, description.Columns.Columns, 1)
	assert.Equal(t, "id", description.Columns.Columns[0].Name)

	mockTable.AssertExpectations(t)
}

func TestStreamProtoStructTable_MessageSizeExceeded(t *testing.T) {
	row, err := structpb.NewStruct(map[string]interface{}{"id": 1})
	require.NoError(t, err)

	mockTable := new(mockProtoStructTable)
	mockTable.On("NextChunk").Return([]*structpb.Struct{row}, nil).Once()
	mockTable.On("ColumnCount").Return(1)
	mockTable.On("ColumnDescription", 0).Return(query.ColumnDescription{Name: "id"})
	mockTable.On("Format").Return(query.TableFormatProtobufStruct)

	statusErr := status.Error(codes.ResourceExhausted, "stream chunk too large")

	mockStream := new(mockQueryStream)
	mockStream.On("Send", mock.Anything).Return(nil).Once()       // header
	mockStream.On("Send", mock.Anything).Return(statusErr).Once() // first chunk triggers failure

	req := &apapproto.QueryRequest{QuerySql: "SELECT * FROM test"}

	streamErr := streamProtoStructTable(req, mockStream, mockTable)
	require.Error(t, streamErr)

	msg := message.IsMessage(streamErr)
	require.NotNil(t, msg)
	assert.Equal(t, message.EngineGrpcserverApiApapQueryMessageSizeExceeded, msg.Code())

	mockTable.AssertExpectations(t)
	mockStream.AssertExpectations(t)
}

func TestStreamByteStreamTable(t *testing.T) {
	mockTable := new(mockByteStreamTable)
	mockTable.On("Format").Return(query.TableFormatArrowIPC)
	mockTable.On("ColumnCount").Return(1)
	mockTable.On("ColumnDescription", 0).Return(query.ColumnDescription{Name: "col1"})
	mockTable.On("OpenReader").Return(io.NopCloser(bytes.NewBufferString("arrow-bytes")), nil)

	mockStream := new(mockQueryStream)
	var sentMessages []*apapproto.QueryResponse
	mockStream.On("Send", mock.Anything).Return(nil).Run(func(args mock.Arguments) {
		sentMessages = append(sentMessages, args.Get(0).(*apapproto.QueryResponse))
	})

	req := &apapproto.QueryRequest{QuerySql: "SELECT * FROM test"}

	err := streamByteStreamTable(req, mockStream, mockTable)
	assert.NoError(t, err)

	require.Len(t, sentMessages, 2)
	desc := sentMessages[0].GetDescription()
	require.NotNil(t, desc)
	assert.Equal(t, apapproto.TableFormat_ARROW_IPC_STREAM, desc.Format)
	require.NotNil(t, desc.Columns)
	require.Len(t, desc.Columns.Columns, 1)
	assert.Equal(t, "col1", desc.Columns.Columns[0].Name)

	chunk := sentMessages[1].GetChunk()
	require.NotNil(t, chunk)
	assert.Equal(t, apapproto.TableFormat_ARROW_IPC_STREAM, chunk.Format)
	assert.Equal(t, []byte("arrow-bytes"), chunk.GetBinaryChunk().Bytes)

	mockTable.AssertExpectations(t)
	mockStream.AssertExpectations(t)
}

func TestStreamByteStreamTable_MessageSizeExceeded(t *testing.T) {
	mockTable := new(mockByteStreamTable)
	mockTable.On("Format").Return(query.TableFormatArrowIPC)
	mockTable.On("ColumnCount").Return(1)
	mockTable.On("ColumnDescription", 0).Return(query.ColumnDescription{Name: "col1"})
	mockTable.On("OpenReader").Return(io.NopCloser(bytes.NewBuffer([]byte{0x1})), nil)

	statusErr := status.Error(codes.ResourceExhausted, "too large")
	mockStream := new(mockQueryStream)
	mockStream.On("Send", mock.Anything).Return(nil).Once()       // header
	mockStream.On("Send", mock.Anything).Return(statusErr).Once() // first chunk

	req := &apapproto.QueryRequest{QuerySql: "SELECT * FROM test"}

	err := streamByteStreamTable(req, mockStream, mockTable)
	require.Error(t, err)

	msg := message.IsMessage(err)
	require.NotNil(t, msg)
	assert.Equal(t, message.EngineGrpcserverApiApapQueryMessageSizeExceeded, msg.Code())

	mockTable.AssertExpectations(t)
	mockStream.AssertExpectations(t)
}

func TestStreamByteStreamTable_ReadError(t *testing.T) {
	mockTable := new(mockByteStreamTable)
	mockTable.On("Format").Return(query.TableFormatArrowIPC)
	mockTable.On("ColumnCount").Return(1)
	mockTable.On("ColumnDescription", 0).Return(query.ColumnDescription{Name: "col1"})
	mockTable.On("OpenReader").Return(io.NopCloser(errReader{}), nil)

	mockStream := new(mockQueryStream)
	mockStream.On("Send", mock.Anything).Return(nil) // header

	req := &apapproto.QueryRequest{QuerySql: "SELECT * FROM test"}

	err := streamByteStreamTable(req, mockStream, mockTable)
	require.Error(t, err)
	require.EqualError(t, err, "boom")

	mockTable.AssertExpectations(t)
	mockStream.AssertExpectations(t)
}

func TestQueryRunExtra(t *testing.T) {
	server := ApapServer{}

	t.Run("successfully returns RunExtra_Value", func(t *testing.T) {
		mockInvokeRender := func(context.Context, *apapproto.InvokeRenderRequest) (*apapproto.InvokeRenderResponse, error) {
			return &apapproto.InvokeRenderResponse{}, nil
		}

		ret := server.QueryRunExtra(
			context.Background(),
			[]*apapproto.RunId{{Value: "abcd1234"}},
			"TargetInfoRenderer",
			nil,
			mockInvokeRender,
		)
		assert.IsType(t, &apapproto.RunExtraOrError_Value{}, ret.GetSome())
	})

	t.Run("successfully returns RunExtra_Error", func(t *testing.T) {
		mockInvokeRender := func(context.Context, *apapproto.InvokeRenderRequest) (*apapproto.InvokeRenderResponse, error) {
			return &apapproto.InvokeRenderResponse{}, errors.New("some reason")
		}

		ret := server.QueryRunExtra(
			context.Background(),
			[]*apapproto.RunId{{Value: "abcd1234"}},
			"TargetInfoRenderer",
			nil,
			mockInvokeRender,
		)
		assert.IsType(t, &apapproto.RunExtraOrError_Error{}, ret.GetSome())
	})
}
