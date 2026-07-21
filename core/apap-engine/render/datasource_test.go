// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

type testStruct struct {
	DataSource DataSource `json:"data_source"`
}

func TestIsPending(t *testing.T) {
	t.Run("returns true when any table ref is pending", func(t *testing.T) {
		dataSources := TableRefMap{
			"main": {
				{Name: "ready_table"},
				{Name: "pending_table", Pending: true},
			},
			"summary": {
				{Name: "summary_table"},
			},
		}

		require.True(t, dataSources.IsPending())
	})

	t.Run("returns false when no table refs are pending", func(t *testing.T) {
		dataSources := TableRefMap{
			"main": {
				{Name: "main_table"},
			},
			"summary": {
				{Name: "summary_table"},
			},
		}

		require.False(t, dataSources.IsPending())
	})

	t.Run("returns false for empty table ref map", func(t *testing.T) {
		dataSources := TableRefMap{}

		require.False(t, dataSources.IsPending())
	})
}

func TestDecodeJSONWithHook(t *testing.T) {
	t.Run("Decoding succeeds with TableRefSource when the json is valid", func(t *testing.T) {
		jsonStr := `{"data_source": {"name": "foo"}}`

		result, err := util.DecodeJSONWithHook[testStruct]([]byte(jsonStr), DataSourceDecodeHook)
		assert.NoError(t, err)
		dtr, ok := result.DataSource.(*TableRefSource)
		assert.True(t, ok)
		assert.Equal(t, "foo", dtr.Name)
	})
	t.Run("Decoding succeeds with OutputTableRef when the json is valid", func(t *testing.T) {
		jsonStr := `{"data_source": {"renderer_id": "foo", "output": "bar", "content_index": 1}}`

		result, err := util.DecodeJSONWithHook[testStruct]([]byte(jsonStr), DataSourceDecodeHook)
		assert.NoError(t, err)
		dtr, ok := result.DataSource.(*OutputTableRef)
		assert.True(t, ok)
		assert.Equal(t, "foo", dtr.RendererID)
		assert.Equal(t, "bar", dtr.Output)
		assert.Equal(t, 1, dtr.ContentIndex)
	})
	t.Run("Decoding fails with TableRefSource when the json is bad", func(t *testing.T) {
		jsonStr := `{"data_source": {"renderer_id": "foo", "bad_field": "bar", "index": 1}}`

		_, err := util.DecodeJSONWithHook[testStruct]([]byte(jsonStr), DataSourceDecodeHook)
		assert.ErrorContains(t, err, "invalid DataSource format")
	})
}

func TestParseDataSourcesFromConfig(t *testing.T) {
	t.Run("Parsing succeeds for OutputTableRef", func(t *testing.T) {
		jsonStr := `{"data_source": {"tables": {"default":[{"renderer_id": "foo", "output": "bar", "content_index": 1}]}}}`

		result, err := ParseDataSourcesFromConfig(jsonStr)
		assert.NoError(t, err)
		dtr, ok := result["default"][0].(*OutputTableRef)
		assert.True(t, ok)
		assert.Equal(t, "foo", dtr.RendererID)
		assert.Equal(t, "bar", dtr.Output)
		assert.Equal(t, 1, dtr.ContentIndex)
	})
}
