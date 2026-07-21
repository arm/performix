// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

func TestDummyRendererConfigure(t *testing.T) {
	renderer := &DummyRenderer{}

	err := renderer.Configure(&render.Config{
		JSON: `{
			"schema":[
				{"name":"name","type":"string"},
				{"name":"count","type":"int"},
				{"name":"ratio","type":"float"}
			],
			"content":[{"name":"first","count":1,"ratio":1.5}]
		}`,
	})
	require.NoError(t, err)

	assert.Equal(t, []dummyRendererColumn{
		{inputName: "name", name: "name", valueType: "string", sqlType: "VARCHAR"},
		{inputName: "count", name: "count", valueType: "int", sqlType: "BIGINT"},
		{inputName: "ratio", name: "ratio", valueType: "float", sqlType: "DOUBLE"},
	}, renderer.columns)
	assert.Equal(t, [][]interface{}{
		{"first", int64(1), 1.5},
	}, renderer.rows)
}

func TestDummyRendererConfigureRequiresSchema(t *testing.T) {
	renderer := &DummyRenderer{}

	err := renderer.Configure(&render.Config{
		JSON: `{"content":[]}`,
	})
	require.EqualError(t, err, "schema is required")
}

func TestDummyRendererConfigureRequiresContent(t *testing.T) {
	renderer := &DummyRenderer{}

	err := renderer.Configure(&render.Config{
		JSON: `{"schema":[]}`,
	})
	require.EqualError(t, err, "content is required")
}

func TestDummyRendererConfigureRejectsDuplicateColumnNames(t *testing.T) {
	renderer := &DummyRenderer{}

	err := renderer.Configure(&render.Config{
		JSON: `{
			"schema":[
				{"name":"first-column","type":"string"},
				{"name":"first_column","type":"string"}
			],
			"content":[]
		}`,
	})
	require.EqualError(t, err, "duplicate dummy renderer column name 'first_column'")
}

func TestDummyRendererConfigureRejectsInvalidContentValues(t *testing.T) {
	renderer := &DummyRenderer{}

	err := renderer.Configure(&render.Config{
		JSON: `{
			"schema":[
				{"name":"count","type":"int"}
			],
			"content":[{"count":1.5}]
		}`,
	})
	require.EqualError(t, err, "invalid value 1.5 for dummy renderer int column")
}

func TestDummyRendererMetadata(t *testing.T) {
	renderer := &DummyRenderer{}
	require.NoError(t, renderer.Configure(&render.Config{
		JSON: `{"schema":[{"name":"value","type":"string"}],"content":[]}`,
	}))

	assert.Equal(t, "DummyRenderer", renderer.Name())
	assert.Equal(t, "0.1.0", renderer.Version())
	assert.Len(t, renderer.GetInputSpec().Ports, 0)

	outputSpec := renderer.GetOutputSpec()
	require.Len(t, outputSpec.Ports, 1)
	assert.Equal(t, "table", outputSpec.Ports[0].Name)
	assert.Equal(t, render.CardinalityOne, outputSpec.Ports[0].Cardinality)
	assert.Equal(t, "custom_table", outputSpec.Ports[0].ComponentType.Name)
	assert.Equal(t, renderer.Version(), outputSpec.Ports[0].ComponentType.SchemaVersion)
}

func TestDummyRendererInitializeCreatesConfiguredTable(t *testing.T) {
	registry := NewRegistry()
	renderer, err := registry.NewRenderer("DummyRenderer")
	require.NoError(t, err)

	rendererID := "scratch_table"
	config := &render.Config{
		Identity: render.RendererIdentity{ID: &rendererID, Name: "DummyRenderer"},
		JSON: `{
			"schema":[
				{"name":"name","type":"string"},
				{"name":"count","type":"int"},
				{"name":"ratio","type":"float"},
				{"name":"bad-name","type":"string"}
			],
			"content":[
				{
					"name":"alpha",
					"count":7,
					"ratio":1.5,
					"bad-name":"sanitized",
					"ignored":"field"
				},
				{"name":"mainly null"}
			]
		}`,
	}
	require.NoError(t, renderer.Configure(config))

	runID := run.RunID{Value: "run1"}
	content := &render.ContentMap{
		Entries: []render.ContentMapEntry{
			{ID: runID},
		},
	}
	manifest := render.NewManifest()
	db, err := (&render.DuckDBFactory{}).Connect(t.Name())
	require.NoError(t, err)
	defer db.Close()

	session := &render.MockSession{}
	session.On("Content").Return(content)
	session.On("Manifest").Return(&manifest)
	session.On("Database").Return(db)

	require.NoError(t, renderer.Initialize(session, nil))

	entries := manifest.Entries()
	require.Len(t, entries, 1)
	require.Equal(t, "custom_table", entries[0].Info().ComponentType().Name)
	require.Equal(t, renderer.Version(), entries[0].Info().ComponentType().SchemaVersion)
	require.Equal(t, []run.RunID{runID}, entries[0].Info().AssociatedContent())

	rows, err := session.Database().Conn.QueryContext(
		context.Background(),
		fmt.Sprintf(`SELECT id, name, count, ratio, bad_name FROM %s ORDER BY id`, entries[0].TableName()),
	)
	require.NoError(t, err)
	defer rows.Close()

	var got []string
	for rows.Next() {
		var index int
		var name sql.NullString
		var count sql.NullInt64
		var ratio sql.NullFloat64
		var badName sql.NullString
		require.NoError(t, rows.Scan(&index, &name, &count, &ratio, &badName))
		got = append(got, formatScratchTableTestRow(index, name, count, ratio, badName))
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []string{
		"0:alpha:7:1.5:sanitized",
		"1:mainly null:NULL:NULL:NULL",
	}, got)
}

func formatScratchTableTestRow(
	index int,
	name sql.NullString,
	count sql.NullInt64,
	ratio sql.NullFloat64,
	badName sql.NullString,
) string {

	nullableStringToString := func(value sql.NullString) string {
		if !value.Valid {
			return "NULL"
		}
		return value.String
	}

	nullableIntToString := func(value sql.NullInt64) string {
		if !value.Valid {
			return "NULL"
		}
		return fmt.Sprint(value.Int64)
	}

	nullableFloatToString := func(value sql.NullFloat64) string {
		if !value.Valid {
			return "NULL"
		}
		return fmt.Sprint(value.Float64)
	}
	return fmt.Sprintf(
		"%d:%s:%s:%s:%s",
		index,
		nullableStringToString(name),
		nullableIntToString(count),
		nullableFloatToString(ratio),
		nullableStringToString(badName),
	)
}
