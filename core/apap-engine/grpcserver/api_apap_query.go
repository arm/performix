// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/query"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

func marshalTableColumns(table query.TableAccessor) *apapproto.Columns {
	result := make([]*apapproto.ColumnDescription, table.ColumnCount())
	for i := range len(result) {
		result[i] = &apapproto.ColumnDescription{Name: table.ColumnDescription(i).Name}
	}
	return &apapproto.Columns{Columns: result}
}

func executeOptionsForFormat(format query.TableFormat) (query.ExecuteOptions, error) {
	var settings query.FormatSettings
	switch format {
	case query.TableFormatProtobufStruct:
		settings = &query.ProtobufStructSettings{}
	case query.TableFormatArrowIPC:
		settings = &query.ArrowIPCSettings{}
	default:
		return query.ExecuteOptions{}, fmt.Errorf("query failed: unsupported format: '%s'", format)
	}

	return query.ExecuteOptions{
		Format:   format,
		Settings: settings,
	}, nil
}

func streamProtoStructTable(in *apapproto.QueryRequest, out apapproto.Apap_QueryServer, table query.ProtoStructTableAccessor) error {
	format := ProtoTableFormatFromInternal(table.Format())

	// Send empty chunk with column headers
	resp := &apapproto.QueryResponse{
		SubResponse: &apapproto.QueryResponse_Description{Description: &apapproto.TableDescription{
			Format:  format,
			Command: &in.QuerySql,
			Columns: marshalTableColumns(table),
		}},
	}

	err := out.Send(resp)
	if err != nil {
		return err
	}

	for {
		chunk, err := table.NextChunk()
		if err == io.EOF {
			break
		} else if err != nil {
			return err
		}

		if len(chunk) == 0 {
			continue
		}

		msg := &apapproto.QueryResponse{
			SubResponse: &apapproto.QueryResponse_Chunk{
				Chunk: &apapproto.TableChunk{
					Format: apapproto.TableFormat_PROTOBUF_STRUCT,
					Chunk: &apapproto.TableChunk_StructChunk{
						StructChunk: &apapproto.StructTableChunk{
							Rows: chunk,
						},
					},
				},
			},
		}

		if err := out.Send(msg); err != nil {
			if status.Code(err) == codes.ResourceExhausted {
				return message.New(message.EngineGrpcserverApiApapQueryMessageSizeExceeded)
			}
			return err
		}
	}
	return nil
}

func streamByteStreamTable(in *apapproto.QueryRequest, out apapproto.Apap_QueryServer, table query.ByteStreamTableAccessor) error {
	format := ProtoTableFormatFromInternal(table.Format())

	resp := &apapproto.QueryResponse{
		SubResponse: &apapproto.QueryResponse_Description{Description: &apapproto.TableDescription{
			Format:  format,
			Command: &in.QuerySql,
			Columns: marshalTableColumns(table),
		}},
	}

	if err := out.Send(resp); err != nil {
		return err
	}

	reader, err := table.OpenReader()
	if err != nil {
		return err
	}
	defer reader.Close()

	const chunkSize = 1 * 1024 * 1024
	buf := bytes.Buffer{}

	msg := &apapproto.QueryResponse{
		SubResponse: &apapproto.QueryResponse_Chunk{
			Chunk: &apapproto.TableChunk{
				Format: format,
				Chunk: &apapproto.TableChunk_BinaryChunk{
					BinaryChunk: &apapproto.BinaryTableChunk{Bytes: nil},
				},
			},
		},
	}

	for {
		buf.Reset()
		n, readErr := io.CopyN(&buf, reader, chunkSize)
		if n > 0 {
			msg.GetChunk().GetBinaryChunk().Bytes = buf.Bytes()

			if err := out.Send(msg); err != nil {
				if status.Code(err) == codes.ResourceExhausted {
					return message.New(message.EngineGrpcserverApiApapQueryMessageSizeExceeded)
				}
				return err
			}
		}

		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}

	return nil
}

func (s *ApapServer) Query(in *apapproto.QueryRequest, out apapproto.Apap_QueryServer) error {
	sessionAccess, err := s.sessions.GetSessionByID(in.SessionId)
	if err != nil {
		return err
	}
	defer sessionAccess.Done()

	format, err := InternalTableFormatFromProto(in.TableFormat)
	if err != nil {
		return err
	}

	opts, err := executeOptionsForFormat(format)
	if err != nil {
		return err
	}

	table, err := query.Execute(out.Context(), sessionAccess.S.Database(), in.QuerySql, opts)
	if err != nil {
		return err
	}
	defer table.Close()

	switch table := table.(type) {
	case query.ProtoStructTableAccessor:
		return streamProtoStructTable(in, out, table)
	case query.ByteStreamTableAccessor:
		return streamByteStreamTable(in, out, table)
	default:
		return fmt.Errorf("query failed: unexpected table format %T returned from query execution", table)
	}
}

func (s *ApapServer) executeQueryInternal(ctx context.Context, req *apapproto.QueryRequest) ([]*structpb.Struct, *apapproto.TableDescription, error) {
	sessionAccess, err := s.sessions.GetSessionByID(req.SessionId)
	if err != nil {
		return nil, nil, err
	}
	defer sessionAccess.Done()

	format, err := InternalTableFormatFromProto(req.TableFormat)
	if err != nil {
		return nil, nil, err
	}

	if format != query.TableFormatProtobufStruct {
		return nil, nil, fmt.Errorf("unsupported format: %s", req.TableFormat)
	}

	opts, err := executeOptionsForFormat(format)
	if err != nil {
		return nil, nil, err
	}

	table, err := query.Execute(ctx, sessionAccess.S.Database(), req.QuerySql, opts)
	if err != nil {
		return nil, nil, err
	}
	defer table.Close()

	typedTable, ok := table.(query.ProtoStructTableAccessor)
	if !ok {
		return nil, nil, fmt.Errorf("unexpected table format %T", table)
	}

	var allRows []*structpb.Struct
	for {
		chunk, err := typedTable.NextChunk()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, err
		}
		allRows = append(allRows, chunk...)
	}

	desc := &apapproto.TableDescription{
		Format:  ProtoTableFormatFromInternal(typedTable.Format()),
		Command: &req.QuerySql,
		Columns: marshalTableColumns(typedTable),
	}
	return allRows, desc, nil
}

func (s *ApapServer) QueryRunExtra(
	ctx context.Context,
	runIDs []*apapproto.RunId,
	renderer string,
	tableFilter func(*apapproto.RenderManifestEntry) bool,
	invokeRender func(context.Context, *apapproto.InvokeRenderRequest) (*apapproto.InvokeRenderResponse, error),
) *apapproto.RunExtraOrError {
	extra, err := s.queryRunExtraFailOnError(ctx, runIDs, renderer, tableFilter, invokeRender)
	if err != nil {
		return &apapproto.RunExtraOrError{Some: &apapproto.RunExtraOrError_Error{
			Error: &apapproto.Error{
				Message: fmt.Sprintf("failed to query run extra with renderer '%s': %v", renderer, err),
			},
		}}
	}

	return &apapproto.RunExtraOrError{Some: &apapproto.RunExtraOrError_Value{
		Value: extra,
	}}
}

func (s *ApapServer) queryRunExtraFailOnError(
	ctx context.Context,
	runIDs []*apapproto.RunId,
	renderer string,
	tableFilter func(*apapproto.RenderManifestEntry) bool,
	invokeRender func(context.Context, *apapproto.InvokeRenderRequest) (*apapproto.InvokeRenderResponse, error),
) (*apapproto.RunExtra, error) {
	const maxRowsPerTable = 256

	invokeReq := &apapproto.InvokeRenderRequest{
		Content:        &apapproto.ContentSelection{Runs: runIDs},
		RendererConfig: []*apapproto.RendererConfig{{Renderer: renderer}},
	}

	invokeResp, err := invokeRender(ctx, invokeReq)
	if err != nil {
		return nil, fmt.Errorf("failed to invoke renderer %q: %w", renderer, err)
	}
	defer s.sessions.CloseRenderSession(invokeResp.SessionId)

	extra := &apapproto.RunExtra{
		Extra: make(map[string]*apapproto.TableWithDescription),
	}

	for _, entry := range invokeResp.GetManifest().GetEntry() {
		tableName := entry.GetTableName()
		if tableFilter != nil && !tableFilter(entry) {
			continue
		}

		queryReq := &apapproto.QueryRequest{
			SessionId:   invokeResp.GetSessionId(),
			QuerySql:    fmt.Sprintf("SELECT * FROM \"%s\"", tableName),
			TableFormat: apapproto.TableFormat_PROTOBUF_STRUCT,
		}

		rows, desc, err := s.executeQueryInternal(ctx, queryReq)
		if err != nil {
			return nil, fmt.Errorf("failed to query table %q: %w", tableName, err)
		}

		// Truncate rows if over the limit
		if len(rows) > maxRowsPerTable {
			rows = rows[:maxRowsPerTable]
		}

		extra.Extra[tableName] = &apapproto.TableWithDescription{
			Description: desc,
			Chunk:       &apapproto.StructTableChunk{Rows: rows},
		}
	}

	return extra, nil
}
