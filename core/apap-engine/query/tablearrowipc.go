// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package query

import (
	"context"
	"fmt"
	"io"

	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/Arm-Debug/apap-cli/apap-engine/render"
)

// tableArrowIPC is a concrete type implementing Table. It implements ByteStreamTable.
type tableArrowIPC struct {
	reader   array.RecordReader
	settings ArrowIPCSettings
}

func newTableArrowIPC(ctx context.Context, db *render.Database, sql string, settings ArrowIPCSettings) (TableAccessorCloser, error) {
	reader, err := QueryViaArrow(ctx, db, sql)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}

	return &tableArrowIPC{
		reader:   reader,
		settings: settings,
	}, nil
}

func (t *tableArrowIPC) ColumnCount() int {
	return len(t.reader.Schema().Fields())
}

func (t *tableArrowIPC) ColumnDescription(i int) ColumnDescription {
	return ColumnDescription{Name: t.reader.Schema().Fields()[i].Name}
}

func (t *tableArrowIPC) Format() TableFormat {
	return TableFormatArrowIPC
}

func (t *tableArrowIPC) OpenReader() (io.ReadCloser, error) {
	return ExportArrowToIPCStream(t.reader)
}

func (t *tableArrowIPC) Close() error {
	t.reader.Release()
	return nil
}
