// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
)

func TestManifest(t *testing.T) {
	t.Run("add manifest entry", func(t *testing.T) {
		manifest := NewManifest()

		info := ManifestEntryInfo{
			componentType: cdf.ComponentType{Name: "a", SchemaVersion: "b"},
		}
		tableName := manifest.AddEntry(info)

		assert.NotNil(t, tableName)

		assert.Equal(t, 1, len(manifest.Entries()))
		assert.Equal(t, tableName, manifest.Entries()[0].TableName())
		assert.Equal(t, info, *manifest.Entries()[0].Info())
		assert.Equal(t, false, manifest.Entries()[0].IsHidden())
		assert.Equal(t, "a", manifest.Entries()[0].TableName())
	})

	t.Run("add manifest with invalid chars in component name", func(t *testing.T) {
		manifest := NewManifest()

		info := ManifestEntryInfo{
			componentType: cdf.ComponentType{Name: "  abcdefghijklmnopqrstuvwxyz-ABCDEFGHIJKLMNOPQRSTUVWXYZ+0123456789_ ", SchemaVersion: "b"},
		}
		tableName := manifest.AddEntry(info)

		assert.NotNil(t, tableName)

		assert.Equal(t, 1, len(manifest.Entries()))
		assert.Equal(t, tableName, manifest.Entries()[0].TableName())
		assert.Equal(t, info, *manifest.Entries()[0].Info())
		assert.Equal(t, false, manifest.Entries()[0].IsHidden())
		assert.Equal(t, "__abcdefghijklmnopqrstuvwxyz_ABCDEFGHIJKLMNOPQRSTUVWXYZ_0123456789__", manifest.Entries()[0].TableName())
	})

	t.Run("add multiple manifest entries of same component type", func(t *testing.T) {
		manifest := NewManifest()

		tableName0 := manifest.AddEntry(ManifestEntryInfo{
			componentType: cdf.ComponentType{Name: "a", SchemaVersion: "b"},
		})
		tableName1 := manifest.AddEntry(ManifestEntryInfo{
			componentType: cdf.ComponentType{Name: "a", SchemaVersion: "b"},
		})
		tableName2 := manifest.AddEntry(ManifestEntryInfo{
			componentType: cdf.ComponentType{Name: "boo", SchemaVersion: "b"},
		})

		assert.NotNil(t, tableName0)
		assert.NotNil(t, tableName1)
		assert.NotNil(t, tableName2)

		assert.Equal(t, 3, len(manifest.Entries()))
		assert.Equal(t, tableName0, manifest.Entries()[0].TableName())
		assert.Equal(t, tableName1, manifest.Entries()[1].TableName())
		assert.Equal(t, tableName2, manifest.Entries()[2].TableName())

		assert.Equal(t, "a", manifest.Entries()[0].TableName())
		assert.Equal(t, "a_1", manifest.Entries()[1].TableName())
		assert.Equal(t, "boo", manifest.Entries()[2].TableName())
	})

	t.Run("add temp table creates temp manifest entry", func(t *testing.T) {
		manifest := NewManifest()

		tableName0 := manifest.AddTempTable()
		tableName1 := manifest.AddTempTable()
		assert.Equal(t, "anonymous", tableName0)
		assert.Equal(t, "anonymous_1", tableName1)

		assert.Equal(t, 2, len(manifest.Entries()))
		assert.Equal(t, true, manifest.Entries()[0].IsTemp())
		assert.Equal(t, true, manifest.Entries()[1].IsTemp())
	})

	t.Run("temp table names returns only temp entries", func(t *testing.T) {
		manifest := NewManifest()

		visibleTableName := manifest.AddEntry(ManifestEntryInfo{
			componentType: cdf.ComponentType{Name: "visible", SchemaVersion: "1.0.0"},
		})
		_ = manifest.AddEntryHidden(ManifestEntryInfo{
			componentType: cdf.ComponentType{Name: "hidden", SchemaVersion: "1.0.0"},
		})
		tempTableName := manifest.AddTempTable()

		assert.NotContains(t, manifest.TempTableNames(), visibleTableName)
		assert.ElementsMatch(t, []string{tempTableName}, manifest.TempTableNames())
	})
}
