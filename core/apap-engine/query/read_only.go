// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package query

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
)

type serializedSQL struct {
	Error        bool              `json:"error"`
	ErrorMessage string            `json:"error_message"`
	Statements   []json.RawMessage `json:"statements"`
}

// validateReadOnlySQL uses DuckDB's parser to verify the SQL without executing
// it. json_serialize_sql serializes only SELECT statements, including WITH and
// DESCRIBE SELECT forms.
func validateReadOnlySQL(ctx context.Context, database *render.Database, sql string) error {
	var serialized any
	if err := database.Conn.QueryRowContext(
		ctx,
		"SELECT json_serialize_sql(CAST(? AS VARCHAR))",
		sql,
	).Scan(&serialized); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return readOnlySQLRequired(err)
	}

	encoded, err := json.Marshal(serialized)
	if err != nil {
		return readOnlySQLRequired(fmt.Errorf("encode serialized SQL: %w", err))
	}
	var parsed serializedSQL
	if err := json.Unmarshal(encoded, &parsed); err != nil {
		return readOnlySQLRequired(fmt.Errorf("decode serialized SQL: %w", err))
	}
	if parsed.Error {
		cause := errors.New("DuckDB could not parse the query")
		if parsed.ErrorMessage != "" {
			cause = errors.New(parsed.ErrorMessage)
		}
		return readOnlySQLRequired(cause)
	}
	if len(parsed.Statements) != 1 {
		return readOnlySQLRequired(fmt.Errorf("expected one SELECT statement, found %d", len(parsed.Statements)))
	}

	return nil
}

func readOnlySQLRequired(cause error) error {
	return message.New(message.EngineGrpcserverApiApapQueryReadOnlySqlRequired).WithCause(cause)
}
