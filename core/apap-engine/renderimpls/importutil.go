// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"fmt"
	"strings"

	"github.com/Arm-Debug/apap-cli/apap-engine/render"
)

var largeJSONObjectSizeLimitBytes = fmt.Sprintf("%d", 1024*1024*250)

// ReadJSONAutoWithLargeObjectSize is a temporary solution to the issue of too much data being loaded into a single cell
// in a database table. This occurs because we are reading a file format that's not super friendly for us - DuckDB's
// read_json_auto function reads it all into a single cell, and then we have to extract the parts of it that we want.
// We could change the file reading process to be less fragile, but a long term solution of a C API on top of
// streamline-native is under construction, so instead we have this quick fix - increase the object size limit
// (the default is 16M).
func ReadJSONAutoWithLargeObjectSize(filename string, db *render.Database, tableName string) error {
	query := fmt.Sprint(`CREATE OR REPLACE TABLE `, tableName, ` AS SELECT * FROM read_json_auto(?, maximum_object_size=`, largeJSONObjectSizeLimitBytes, `)`)
	_, err := db.Conn.ExecContext(context.Background(), query, filename)

	if err != nil {
		return err
	}

	return nil
}

// ReadVarcharDoubleCSVIntoDB reads the specified CSV into the db with the specified tableName; all columns are
// interpreted as double, except those columns in varcharColumns, which are interpreted as varchar
func ReadVarcharDoubleCSVIntoDB(filename string, varcharColumns []string, db *render.Database, tableName string) error {
	quoted := make([]string, len(varcharColumns))
	for i := range varcharColumns {
		quoted[i] = sqlQuoteString(varcharColumns[i])
	}

	// Replace types of all columns with double, except those we are instructed to use varchar for
	query := fmt.Sprint(
		`SELECT UNNEST(LIST_TRANSFORM(
				Columns, 
				c -> case when c.name in (`, strings.Join(quoted, ", "), `) then 'varchar' else 'double' end
		  )) from sniff_csv(?)`,
	)
	result, err := db.Conn.QueryContext(context.Background(), query, filename)
	if err != nil {
		return err
	}
	defer result.Close()

	var types []string
	for result.Next() {
		var typeName string
		err := result.Scan(&typeName)
		if err != nil {
			return err
		}
		types = append(types, typeName)
	}

	query = fmt.Sprint(
		`CREATE TABLE `, tableName, ` AS 
			SELECT * 
			FROM read_csv(
				?, 
				types := [`, strings.Join(types, ", "), `])`,
	)
	_, err = db.Conn.ExecContext(context.Background(), query, filename)
	if err != nil {
		return err
	}

	return nil
}
