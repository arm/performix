// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package query

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/render"
)

func TestExecute_ArrowIPC_Success(t *testing.T) {
	db, err := (&render.DuckDBFactory{}).Connect(t.Name())
	require.NoError(t, err)
	defer db.Close()

	opts := ExecuteOptions{
		Format:   TableFormatArrowIPC,
		Settings: &ArrowIPCSettings{},
	}

	table, err := Execute(context.Background(), db, "select 1 as a, 2 as b", opts)
	require.NoError(t, err)
	defer table.Close()

	byteStream, ok := table.(ByteStreamTableAccessor)
	require.True(t, ok)

	stream, err := byteStream.OpenReader()
	require.NoError(t, err)
	defer stream.Close()

	// Drain the stream to completion to avoid races with the underlying RecordReader
	_, err = io.ReadAll(stream)
	require.NoError(t, err)
}

func TestExecute_UnsupportedFormat(t *testing.T) {
	db, err := (&render.DuckDBFactory{}).Connect(t.Name())
	require.NoError(t, err)
	defer db.Close()

	_, err = Execute(context.Background(), db, "select 1", ExecuteOptions{
		Format:   "unsupported",
		Settings: &ArrowIPCSettings{},
	})
	require.Error(t, err)
}

func TestNewTableArrowIPC_Error(t *testing.T) {
	db, err := (&render.DuckDBFactory{}).Connect(t.Name())
	require.NoError(t, err)
	defer db.Close()

	table, err := newTableArrowIPC(context.Background(), db, "select * from missing_table", ArrowIPCSettings{})
	require.Error(t, err)
	require.Nil(t, table)
}

func TestExecute_ArrowIPC_JSONObject(t *testing.T) {
	db, err := (&render.DuckDBFactory{}).Connect(t.Name())
	require.NoError(t, err)
	defer db.Close()

	opts := ExecuteOptions{
		Format:   TableFormatArrowIPC,
		Settings: &ArrowIPCSettings{},
	}

	table, err := Execute(context.Background(), db, "select json_object('a','b') as j", opts)
	require.NoError(t, err)
	defer table.Close()

	byteStream, ok := table.(ByteStreamTableAccessor)
	require.True(t, ok)

	stream, err := byteStream.OpenReader()
	require.NoError(t, err)
	defer stream.Close()

	ipcReader, err := ipc.NewReader(stream)
	require.NoError(t, err)
	defer ipcReader.Release()

	var rows []map[string]any

	for ipcReader.Next() {
		rec := ipcReader.RecordBatch()
		require.NotNil(t, rec)

		data, err := json.Marshal(rec)
		require.NoError(t, err)

		var batch []map[string]any
		require.NoError(t, json.Unmarshal(data, &batch))
		rows = append(rows, batch...)

		rec.Release()
	}

	require.Len(t, rows, 1)
	require.Equal(t, map[string]any{"j": `{"a":"b"}`}, rows[0])
}
