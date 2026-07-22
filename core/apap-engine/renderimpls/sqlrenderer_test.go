// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/targetsession"
)

type sqlTestSession struct {
	id         string
	content    *render.ContentMap
	manifest   *render.Manifest
	db         *render.Database
	visSources *render.WidgetDataSources
}

func (s *sqlTestSession) ID() string                  { return s.id }
func (s *sqlTestSession) DatabaseKey() string         { return "" }
func (s *sqlTestSession) Close()                      {}
func (s *sqlTestSession) Content() *render.ContentMap { return s.content }
func (s *sqlTestSession) Manifest() *render.Manifest  { return s.manifest }
func (s *sqlTestSession) Database() *render.Database  { return s.db }
func (s *sqlTestSession) WidgetDataSources() *render.WidgetDataSources {
	return s.visSources
}
func (s *sqlTestSession) Reference() render.Hub { return nil }
func (s *sqlTestSession) TargetSessions() targetsession.TargetSessionProvider {
	return nil
}
func (s *sqlTestSession) Rerender() render.SessionRenderFS { return nil }

func newSQLTestSession(t *testing.T, model cdf.ModelView, runID string) *sqlTestSession {
	t.Helper()

	db, err := (&render.DuckDBFactory{}).Connect(t.Name())
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	manifest := render.NewManifest()
	return &sqlTestSession{
		id: "session-sql-view",
		content: &render.ContentMap{
			Entries: []render.ContentMapEntry{{
				ID:    run.RunID{Value: runID},
				Model: model,
			}},
		},
		manifest:   &manifest,
		db:         db,
		visSources: render.NewWidgetDataSources(),
	}
}

func requireSQLRendererMessage(
	t *testing.T,
	err error,
	code message.MessageCode,
	metadata map[string]string,
	causeContains string,
) {
	t.Helper()

	var msg message.Message
	require.ErrorAs(t, err, &msg)
	require.Equal(t, code, msg.Code())
	require.Equal(t, metadata, msg.Metadata())

	if causeContains != "" {
		cause := errors.Unwrap(err)
		require.Error(t, cause)
		require.ErrorContains(t, cause, causeContains)
	}
}

func TestSQLRendererConfigureBuildsSpecsFromConfig(t *testing.T) {
	renderer := &SQLRenderer{}

	err := renderer.Configure(&render.Config{
		Identity: render.RendererIdentity{Name: "SQLRenderer"},
		JSON: `{
			"sql": "SELECT * FROM {{table:source}}",
			"inputs": [
				{
					"name": "source",
					"cardinality": "one",
					"component_type": {"name": "flat_table", "schema_version": "1.0"}
				}
			],
			"output": {
				"name": "result",
				"cardinality": "one",
				"component_type": {"name": "flat_table", "schema_version": "1.0"}
			}
		}`,
	})
	require.NoError(t, err)

	inputSpec := renderer.GetInputSpec()
	require.Len(t, inputSpec.Ports, 1)
	require.Equal(t, "source", inputSpec.Ports[0].Name)
	require.Equal(t, render.CardinalityOne, inputSpec.Ports[0].Cardinality)
	require.Equal(t, cdf.ComponentType{Name: "flat_table", SchemaVersion: "1.0"}, inputSpec.Ports[0].ComponentType)

	outputSpec := renderer.GetOutputSpec()
	require.Len(t, outputSpec.Ports, 1)
	require.Equal(t, "result", outputSpec.Ports[0].Name)
	require.Equal(t, render.CardinalityOne, outputSpec.Ports[0].Cardinality)
	require.Equal(t, cdf.ComponentType{Name: "flat_table", SchemaVersion: "1.0"}, outputSpec.Ports[0].ComponentType)
}

func TestSQLRendererConfigureInvalidJSONReturnsCatalogMessage(t *testing.T) {
	renderer := &SQLRenderer{}

	err := renderer.Configure(&render.Config{
		Identity: render.RendererIdentity{Name: "SQLRenderer"},
		JSON:     `{"sql": `,
	})

	requireSQLRendererMessage(t, err, message.EngineRenderSqlRendererConfigInvalidJson, map[string]string{}, "unexpected end of JSON input")
}

func TestSQLRendererConfigureRejectsUnsupportedCardinalityWithMetadata(t *testing.T) {
	renderer := &SQLRenderer{}

	err := renderer.Configure(&render.Config{
		Identity: render.RendererIdentity{Name: "SQLRenderer"},
		JSON: `{
			"sql": "SELECT 1",
			"inputs": [
				{
					"name": "source",
					"cardinality": "many",
					"component_type": {"name": "flat_table", "schema_version": "1.0"}
				}
			],
			"output": {
				"name": "result",
				"component_type": {"name": "flat_table", "schema_version": "1.0"}
			}
		}`,
	})

	requireSQLRendererMessage(t, err, message.EngineRenderSqlRendererPortCardinalityUnsupported, map[string]string{
		"portName":    "source",
		"cardinality": "many",
	}, "")
}

func TestSQLRendererRejectsMultipleStatements(t *testing.T) {
	model := cdf.NewOnDiskModel(t.TempDir(), &cdf.Manifest{}, cdf.Metadata{})
	session := newSQLTestSession(t, model, "run1")

	renderer := &SQLRenderer{}
	err := renderer.Configure(&render.Config{
		Identity: render.RendererIdentity{Name: "SQLRenderer"},
		JSON: `{
			"sql": "SELECT 1; SELECT 2",
			"output": {
				"name": "result",
				"component_type": {"name": "flat_table", "schema_version": "1.0"}
			}
		}`,
	})
	require.NoError(t, err)

	err = renderer.Initialize(session, nil)
	requireSQLRendererMessage(t, err, message.EngineRenderSqlRendererSqlMultiStatement, map[string]string{}, "multi-statement query")
}

func TestSQLRendererInitializeRejectsUnboundTablePlaceholderWithMetadata(t *testing.T) {
	model := cdf.NewOnDiskModel(t.TempDir(), &cdf.Manifest{}, cdf.Metadata{})
	session := newSQLTestSession(t, model, "run1")

	renderer := &SQLRenderer{}
	err := renderer.Configure(&render.Config{
		Identity: render.RendererIdentity{Name: "SQLRenderer"},
		JSON: `{
			"sql": "SELECT * FROM {{table:source}}",
			"output": {
				"name": "result",
				"component_type": {"name": "flat_table", "schema_version": "1.0"}
			}
		}`,
	})
	require.NoError(t, err)

	err = renderer.Initialize(session, nil)
	requireSQLRendererMessage(t, err, message.EngineRenderSqlRendererTablePlaceholderUnbound, map[string]string{
		"placeholderName": "source",
	}, "")
}

func TestSQLRendererInitializeRejectsMalformedTablePlaceholderBeforeDatasourceLookup(t *testing.T) {
	model := cdf.NewOnDiskModel(t.TempDir(), &cdf.Manifest{}, cdf.Metadata{})
	session := newSQLTestSession(t, model, "run1")

	renderer := &SQLRenderer{}
	err := renderer.Configure(&render.Config{
		Identity: render.RendererIdentity{Name: "SQLRenderer"},
		JSON: `{
			"sql": "SELECT * FROM {{table:source:0:extra}}",
			"output": {
				"name": "result",
				"component_type": {"name": "flat_table", "schema_version": "1.0"}
			}
		}`,
	})
	require.NoError(t, err)

	err = renderer.Initialize(session, nil)
	requireSQLRendererMessage(t, err, message.EngineRenderSqlRendererTablePlaceholderFormatInvalid, map[string]string{
		"placeholder": "source:0:extra",
	}, "")
}

func TestSQLRendererInitializeWrapsPathPlaceholderResolutionFailureWithMetadata(t *testing.T) {
	model := cdf.NewOnDiskModel(t.TempDir(), &cdf.Manifest{}, cdf.Metadata{})
	session := newSQLTestSession(t, model, "run1")

	renderer := &SQLRenderer{}
	err := renderer.Configure(&render.Config{
		Identity: render.RendererIdentity{Name: "SQLRenderer"},
		JSON: `{
			"sql": "SELECT * FROM read_csv({{path:../outside.csv}}, header=true)",
			"output": {
				"name": "result",
				"component_type": {"name": "flat_table", "schema_version": "1.0"}
			}
		}`,
	})
	require.NoError(t, err)

	err = renderer.Initialize(session, nil)
	requireSQLRendererMessage(t, err, message.EngineRenderSqlRendererPathPlaceholderComponentNotFound, map[string]string{
		"path": "../outside.csv",
	}, "component not found in manifest")
}

func TestSQLRendererInitializeRejectsMalformedPathPlaceholderBeforeComponentLookup(t *testing.T) {
	model := cdf.NewOnDiskModel(t.TempDir(), &cdf.Manifest{}, cdf.Metadata{})
	session := newSQLTestSession(t, model, "run1")

	renderer := &SQLRenderer{}
	err := renderer.Configure(&render.Config{
		Identity: render.RendererIdentity{Name: "SQLRenderer"},
		JSON: `{
			"sql": "SELECT * FROM read_csv({{path:0:inputs:numbers.csv}}, header=true)",
			"output": {
				"name": "result",
				"component_type": {"name": "flat_table", "schema_version": "1.0"}
			}
		}`,
	})
	require.NoError(t, err)

	err = renderer.Initialize(session, nil)
	requireSQLRendererMessage(t, err, message.EngineRenderSqlRendererPathPlaceholderFormatInvalid, map[string]string{
		"placeholder": "0:inputs:numbers.csv",
	}, "")
}

func TestSQLRendererCreatesViewFromDatasourceTablePlaceholder(t *testing.T) {
	model := cdf.NewOnDiskModel(t.TempDir(), &cdf.Manifest{}, cdf.Metadata{})
	session := newSQLTestSession(t, model, "run1")

	sourceTableName := session.Manifest().AddEntry(render.NewManifestEntryInfo(
		cdf.ComponentType{Name: "flat_table", SchemaVersion: "1.0"},
		render.RendererIdentity{Name: "upstream"},
		[]run.RunID{{Value: "run1"}},
	))
	_, err := session.Database().Conn.ExecContext(context.Background(),
		`CREATE TABLE `+sourceTableName+` (value INTEGER);
		 INSERT INTO `+sourceTableName+` VALUES (7), (9)`)
	require.NoError(t, err)

	renderer := &SQLRenderer{}
	err = renderer.Configure(&render.Config{
		Identity: render.RendererIdentity{Name: "SQLRenderer"},
		JSON: `{
			"sql": "SELECT value FROM {{table:source}} ORDER BY value",
			"inputs": [
				{
					"name": "source",
					"component_type": {"name": "flat_table", "schema_version": "1.0"}
				}
			],
			"output": {
				"name": "result",
				"component_type": {"name": "flat_table", "schema_version": "1.0"}
			}
		}`,
	})
	require.NoError(t, err)

	err = renderer.Initialize(session, map[string][]render.TableRef{
		"source": {{Name: sourceTableName}},
	})
	require.NoError(t, err)

	entries := session.Manifest().Entries()
	require.Len(t, entries, 2)
	viewName := entries[1].TableName()
	require.Equal(t, "flat_table_1", viewName)

	//nolint:gosec
	rows, err := session.Database().Conn.QueryContext(context.Background(), `SELECT value FROM `+viewName+` ORDER BY value`)
	require.NoError(t, err)
	defer rows.Close()

	var values []int
	for rows.Next() {
		var value int
		require.NoError(t, rows.Scan(&value))
		values = append(values, value)
	}
	require.Equal(t, []int{7, 9}, values)
}

func TestSQLRendererCreatesViewFromRunPathPlaceholder(t *testing.T) {
	tmpDir := t.TempDir()
	csvPath := filepath.Join(tmpDir, "inputs", "numbers.csv")
	require.NoError(t, os.MkdirAll(filepath.Dir(csvPath), 0o755))
	require.NoError(t, os.WriteFile(csvPath, []byte("value\n3\n5\n"), 0o644))

	model := cdf.NewOnDiskModel(tmpDir, &cdf.Manifest{
		Entries: []cdf.ManifestEntry{{
			Path:          "inputs/numbers.csv",
			ComponentType: cdf.ComponentType{Name: "test_csv", SchemaVersion: "1.0"},
		}},
	}, cdf.Metadata{})
	session := newSQLTestSession(t, model, "run1")

	renderer := &SQLRenderer{}
	err := renderer.Configure(&render.Config{
		Identity: render.RendererIdentity{Name: "SQLRenderer"},
		JSON: `{
			"sql": "SELECT value FROM read_csv({{path:inputs/numbers.csv}}, header=true) ORDER BY value",
			"output": {
				"name": "result",
				"component_type": {"name": "flat_table", "schema_version": "1.0"}
			}
		}`,
	})
	require.NoError(t, err)

	err = renderer.Initialize(session, nil)
	require.NoError(t, err)

	rows, err := session.Database().Conn.QueryContext(context.Background(), `SELECT value FROM flat_table ORDER BY value`)
	require.NoError(t, err)
	defer rows.Close()

	var values []int
	for rows.Next() {
		var value int
		require.NoError(t, rows.Scan(&value))
		values = append(values, value)
	}
	require.Equal(t, []int{3, 5}, values)
}

func TestSQLRendererResolvesRunPathPlaceholderAgainstBaseRunPathForOverlayModels(t *testing.T) {
	baseDir := t.TempDir()
	csvPath := filepath.Join(baseDir, "inputs", "numbers.csv")
	require.NoError(t, os.MkdirAll(filepath.Dir(csvPath), 0o755))
	require.NoError(t, os.WriteFile(csvPath, []byte("value\n11\n13\n"), 0o644))

	baseModel := cdf.NewOnDiskModel(baseDir, &cdf.Manifest{
		Entries: []cdf.ManifestEntry{{
			Path:          "inputs/numbers.csv",
			ComponentType: cdf.ComponentType{Name: "test_csv", SchemaVersion: "1.0"},
		}},
	}, cdf.Metadata{})
	overlayDir := filepath.Join(baseDir, "render", "overlay-id")
	require.NoError(t, os.MkdirAll(overlayDir, 0o755))
	overlayModel := cdf.NewOnDiskModel(overlayDir, &cdf.Manifest{}, cdf.Metadata{})

	model, err := cdf.NewOverlayModel(baseModel, overlayModel)
	require.NoError(t, err)

	session := newSQLTestSession(t, model, "run1")

	renderer := &SQLRenderer{}
	err = renderer.Configure(&render.Config{
		Identity: render.RendererIdentity{Name: "SQLRenderer"},
		JSON: `{
			"sql": "SELECT value FROM read_csv({{path:inputs/numbers.csv}}, header=true) ORDER BY value",
			"output": {
				"name": "result",
				"component_type": {"name": "flat_table", "schema_version": "1.0"}
			}
		}`,
	})
	require.NoError(t, err)

	err = renderer.Initialize(session, nil)
	require.NoError(t, err)

	rows, err := session.Database().Conn.QueryContext(context.Background(), `SELECT value FROM flat_table ORDER BY value`)
	require.NoError(t, err)
	defer rows.Close()

	var values []int
	for rows.Next() {
		var value int
		require.NoError(t, rows.Scan(&value))
		values = append(values, value)
	}
	require.Equal(t, []int{11, 13}, values)
}

func TestSQLRendererResolvesRunPathPlaceholderAgainstOverlayComponentWhenPresent(t *testing.T) {
	baseDir := t.TempDir()
	baseCsvPath := filepath.Join(baseDir, "inputs", "numbers.csv")
	require.NoError(t, os.MkdirAll(filepath.Dir(baseCsvPath), 0o755))
	require.NoError(t, os.WriteFile(baseCsvPath, []byte("value\n1\n2\n"), 0o644))

	baseModel := cdf.NewOnDiskModel(baseDir, &cdf.Manifest{
		Entries: []cdf.ManifestEntry{{
			Path:          "inputs/numbers.csv",
			ComponentType: cdf.ComponentType{Name: "test_csv", SchemaVersion: "1.0"},
		}},
	}, cdf.Metadata{})

	overlayDir := filepath.Join(baseDir, "render", "overlay-id")
	overlayCsvPath := filepath.Join(overlayDir, "inputs", "numbers.csv")
	require.NoError(t, os.MkdirAll(filepath.Dir(overlayCsvPath), 0o755))
	require.NoError(t, os.WriteFile(overlayCsvPath, []byte("value\n21\n34\n"), 0o644))
	overlayModel := cdf.NewOnDiskModel(overlayDir, &cdf.Manifest{
		Entries: []cdf.ManifestEntry{{
			Path:          "inputs/numbers.csv",
			ComponentType: cdf.ComponentType{Name: "test_csv", SchemaVersion: "1.0"},
		}},
	}, cdf.Metadata{})

	model, err := cdf.NewOverlayModel(baseModel, overlayModel)
	require.NoError(t, err)

	session := newSQLTestSession(t, model, "run1")

	renderer := &SQLRenderer{}
	err = renderer.Configure(&render.Config{
		Identity: render.RendererIdentity{Name: "SQLRenderer"},
		JSON: `{
			"sql": "SELECT value FROM read_csv({{path:inputs/numbers.csv}}, header=true) ORDER BY value",
			"output": {
				"name": "result",
				"component_type": {"name": "flat_table", "schema_version": "1.0"}
			}
		}`,
	})
	require.NoError(t, err)

	err = renderer.Initialize(session, nil)
	require.NoError(t, err)

	rows, err := session.Database().Conn.QueryContext(context.Background(), `SELECT value FROM flat_table ORDER BY value`)
	require.NoError(t, err)
	defer rows.Close()

	var values []int
	for rows.Next() {
		var value int
		require.NoError(t, rows.Scan(&value))
		values = append(values, value)
	}
	require.Equal(t, []int{21, 34}, values)
}
