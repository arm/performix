// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package query

import (
	"context"
	"fmt"

	"github.com/Arm-Debug/apap-cli/apap-engine/render"
)

// Execute is the single entry point for creating a table in the desired format.
// It dispatches to the appropriate constructor logic based on "format."
func Execute(ctx context.Context, database *render.Database, sql string, opts ExecuteOptions) (TableAccessorCloser, error) {
	switch opts.Format {

	case TableFormatNativeRow:
		return NewNativeRowTableAccessor(database, sql, *opts.Settings.(*NativeRowSettings))

	case TableFormatProtobufStruct:
		return NewProtoStructTableAccessor(database, sql, *opts.Settings.(*ProtobufStructSettings))

	case TableFormatArrowIPC:
		return newTableArrowIPC(ctx, database, sql, *opts.Settings.(*ArrowIPCSettings))

	default:
		return nil, fmt.Errorf("unsupported format: '%s'", opts.Format)
	}
}
