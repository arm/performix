// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package insights

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	run "github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/targetsession"
)

type tableRows [][]any

type tableFixture struct {
	name   string
	schema string
	rows   tableRows
}

type manifestTableFixture struct {
	key                 string
	componentType       string
	schemaVersion       string
	visualizationSource string
}

// testSummarySession implements render.Session for testing summarizers that require a render session.
type testSummarySession struct {
	manifest             render.Manifest
	database             *render.Database
	visualizationSources *render.WidgetDataSources
}

func (s *testSummarySession) ID() string                  { return "test-session" }
func (s *testSummarySession) DatabaseKey() string         { return "test-db" }
func (s *testSummarySession) Close()                      {}
func (s *testSummarySession) Content() *render.ContentMap { return &render.ContentMap{} }
func (s *testSummarySession) Manifest() *render.Manifest  { return &s.manifest }
func (s *testSummarySession) Database() *render.Database  { return s.database }
func (s *testSummarySession) WidgetDataSources() *render.WidgetDataSources {
	return s.visualizationSources
}
func (s *testSummarySession) Reference() render.Hub                               { return nil }
func (s *testSummarySession) Rerender() render.SessionRenderFS                    { return nil }
func (s *testSummarySession) TargetSessions() targetsession.TargetSessionProvider { return nil }

func newTestSummarySession(t *testing.T) *testSummarySession {
	t.Helper()

	db, err := (&render.DuckDBFactory{}).Connect(t.Name())
	require.NoError(t, err)
	t.Cleanup(db.Close)

	return &testSummarySession{
		manifest:             render.NewManifest(),
		database:             db,
		visualizationSources: render.NewWidgetDataSources(),
	}
}

func addManifestTableFixtures(
	t *testing.T,
	session *testSummarySession,
	visualizationID string,
	fixtures []manifestTableFixture,
) map[string]string {
	t.Helper()

	tableNames := map[string]string{}
	dataSources := render.TableRefMap{}
	for _, fixture := range fixtures {
		tableName := session.manifest.AddEntry(render.NewManifestEntryInfo(
			cdf.ComponentType{Name: fixture.componentType, SchemaVersion: fixture.schemaVersion},
			render.RendererIdentity{},
			[]run.RunID{{Value: "run123"}},
		))
		tableNames[fixture.key] = tableName

		if fixture.visualizationSource != "" {
			dataSources[fixture.visualizationSource] = []render.TableRef{{Name: tableName}}
		}
	}

	require.NoError(t, session.visualizationSources.AddDataSources(visualizationID, dataSources))
	return tableNames
}

func insertTableFixtures(t *testing.T, db *render.Database, fixtures []tableFixture) {
	t.Helper()

	for _, fixture := range fixtures {
		if fixture.name == "" {
			continue
		}
		insertTableRows(t, db, fixture)
	}
}

func insertTableRows(t *testing.T, db *render.Database, fixture tableFixture) {
	t.Helper()

	_, err := db.Conn.ExecContext(context.Background(), fmt.Sprintf("CREATE TABLE %s %s", fixture.name, fixture.schema))
	require.NoError(t, err)

	if len(fixture.rows) == 0 {
		return
	}

	placeholders := make([]string, len(fixture.rows[0]))
	for i := range placeholders {
		placeholders[i] = "?"
	}
	insertQuery := fmt.Sprintf("INSERT INTO %s VALUES (%s)", fixture.name, strings.Join(placeholders, ", ")) // #nosec G201
	for _, row := range fixture.rows {
		require.Len(t, row, len(placeholders))
		_, err = db.Conn.ExecContext(context.Background(), insertQuery, row...)
		require.NoError(t, err)
	}
}
