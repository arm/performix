// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package query

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/render/sessionfactory"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

func TestQueryHandlesVariousTypes(t *testing.T) {
	type testCase struct {
		name        string
		columnDef   string
		insertStmt  string
		expectedRow map[string]interface{}
	}

	cases := []testCase{
		{
			name:        "INT column",
			columnDef:   "val INTEGER",
			insertStmt:  "INSERT INTO test VALUES (42)",
			expectedRow: map[string]interface{}{"val": float64(42)},
		},
		{
			name:        "BIGINT[] array",
			columnDef:   "val BIGINT[]",
			insertStmt:  "INSERT INTO test VALUES (ARRAY[1, 2, 3])",
			expectedRow: map[string]interface{}{"val": []interface{}{float64(1), float64(2), float64(3)}},
		},
		{
			name:        "BIGINT[] empty array",
			columnDef:   "val BIGINT[]",
			insertStmt:  "INSERT INTO test VALUES (ARRAY[])",
			expectedRow: map[string]interface{}{"val": []interface{}{}},
		},
		{
			name:        "VARCHAR[] array",
			columnDef:   "val VARCHAR[]",
			insertStmt:  "INSERT INTO test VALUES (ARRAY['x', 'y'])",
			expectedRow: map[string]interface{}{"val": []interface{}{"x", "y"}},
		},
		{
			name:        "NULL VARCHAR[]",
			columnDef:   "val VARCHAR[]",
			insertStmt:  "INSERT INTO test VALUES (NULL)",
			expectedRow: map[string]interface{}{"val": nil},
		},
		{
			name:        "DOUBLE with NULL",
			columnDef:   "val DOUBLE",
			insertStmt:  "INSERT INTO test VALUES (NULL)",
			expectedRow: map[string]interface{}{"val": nil},
		},
		{
			name:        "BOOLEAN column",
			columnDef:   "val BOOLEAN",
			insertStmt:  "INSERT INTO test VALUES (TRUE)",
			expectedRow: map[string]interface{}{"val": true},
		},
		// Nested values not supported yet
		//{
		//	name:        "STRUCT with name and age",
		//	columnDef:   "val STRUCT(name VARCHAR, age INTEGER)",
		//	insertStmt:  "INSERT INTO test VALUES (STRUCT_PACK(name := 'Alice', age := 30))",
		//	expectedRow: map[string]interface{}{"val": map[string]interface{}{"name": "Alice", "age": float64(30)}},
		//},
		//{
		//	name:        "STRUCT with NULL age",
		//	columnDef:   "val STRUCT(name VARCHAR, age INTEGER)",
		//	insertStmt:  "INSERT INTO test VALUES (STRUCT_PACK(name := 'Bob', age := NULL))",
		//	expectedRow: map[string]interface{}{"val": map[string]interface{}{"name": "Bob", "age": nil}},
		//},
		//{
		//	name:        "STRUCT with nested array",
		//	columnDef:   "val STRUCT(name VARCHAR, scores INTEGER[])",
		//	insertStmt:  "INSERT INTO test VALUES (STRUCT_PACK(name := 'Charlie', scores := ARRAY[100, 95]))",
		//	expectedRow: map[string]interface{}{"val": map[string]interface{}{"name": "Charlie", "scores": []interface{}{float64(100), float64(95)}}},
		//},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			loader := render.MockRunLoader{}
			loader.On("LoadRun", mock.Anything).Return(&cdf.OnDiskModel{}, nil)

			renderer := &render.MockRenderer{}
			renderer.On("Configure", &render.Config{}).Return(nil)
			renderer.On("Initialize", mock.Anything, mock.Anything).Return(nil)
			renderer.On("GetInputSpec").Return(render.InputSpec{})
			renderer.On("GetOutputSpec").Return(render.OutputSpec{})

			rendererFactory := render.MockRendererFactory{}
			rendererFactory.On("NewRenderer", mock.Anything).Return(renderer, nil)

			sessionStorage := render.NewSessionStorage()
			session, invocationErrors, err := render.StartRenderSession(
				context.Background(),
				&sessionfactory.Impl{},
				&sessionStorage,
				&rendererFactory,
				&loader,
				[]run.RunID{{Value: "test-run"}},
				render.RendererConfigList{{}},
				render.WidgetConfigList{{}},
				&render.DuckDBFactory{},
				nil,
				nil,
			)
			defer sessionStorage.CloseAllRenderSessions()
			assert.NoError(t, err)
			assert.NoError(t, errors.Join(invocationErrors...))

			db := session.Database().Conn
			_, err = db.ExecContext(context.Background(), fmt.Sprintf("CREATE TABLE test (%s)", tc.columnDef))
			assert.NoError(t, err)
			_, err = db.ExecContext(context.Background(), tc.insertStmt)
			assert.NoError(t, err)

			result, err := NewProtoStructTableAccessor(session.Database(), "SELECT * FROM test", ProtobufStructSettings{})
			assert.NoError(t, err)
			if err != nil {
				return
			}
			defer result.Close()

			rows, err := result.NextChunk()
			assert.NoError(t, err)
			if err != nil {
				return
			}
			assert.Len(t, rows, 1)

			actual := rows[0].AsMap()
			assert.EqualValues(t, tc.expectedRow, actual)
		})
	}
}
