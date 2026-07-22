// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package reference

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

func newDuckDB(t *testing.T) *render.Database {
	factory := render.DuckDBFactory{}
	// Intentionally use the same connection key for tests to share the same in-memory DB
	db, err := factory.Connect("measurements_test")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

func newSvcWithDb(t *testing.T) (*measurements, *render.Database) {
	tdb := newDuckDB(t)
	svc, err := newMeasurementsService(tdb)
	require.NoError(t, err)
	return svc, tdb
}

func newSvc(t *testing.T) *measurements {
	svc, _ := newSvcWithDb(t)
	return svc
}

func spec(id, name string, tags []string, aliases map[string]string, colrefs ...render.ColumnRef) render.MeasurementSpec {
	return render.MeasurementSpec{
		Identifier:       render.SlugIdentifier(id),
		Name:             name,
		Description:      "desc:" + name,
		ShortDescription: "short:" + name,
		Units:            "samples",
		Tags:             tags,
		Aliases:          aliases,
		ColumnRefs:       colrefs,
	}
}

func TestEnsureSchema_Idempotent(t *testing.T) {
	ctx := context.Background()

	svc, db := newSvcWithDb(t)
	require.NoError(t, svc.ensureSchema(ctx))

	// call again to ensure idempotency
	require.NoError(t, svc.ensureSchema(ctx))

	// sanity: core table exists and is empty
	var cnt int
	require.NoError(t, db.Conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM ref_measurements`).Scan(&cnt))
	require.Equal(t, 0, cnt)
}

func TestUpsert_HappyPath_OrderAndFields(t *testing.T) {
	ctx := context.Background()
	svc := newSvc(t)

	specs := []render.MeasurementSpec{
		spec("cpu.samples.total", "CPU Samples",
			[]string{" total ", " ", "kind:count"},
			map[string]string{"telemetry": "cpu.samples.total"},
			render.ColumnRef{Table: "tbl_a", Column: "col_x", RendererID: util.Ptr("  rendererA  ")}),
		spec("gpu.util", "GPU Util",
			[]string{"util", "kind:percent"},
			map[string]string{"legacy": "GPU_UTIL"},
			render.ColumnRef{Table: "tbl_b", Column: "col_y"}), // nil renderer
	}

	ids, err := svc.Upsert(ctx, specs)
	require.NoError(t, err)
	require.Len(t, ids, 2)

	// stable input ordering → ids[0] corresponds to first spec
	got0, err := svc.GetByID(ctx, ids[0])
	require.NoError(t, err)
	require.Equal(t, render.SlugIdentifier("cpu.samples.total"), got0.Identifier)
	require.Equal(t, "CPU Samples", got0.Name)
	require.Equal(t, "short:CPU Samples", got0.ShortDescription)
	require.ElementsMatch(t, []string{"total", "kind:count"}, got0.Tags) // trimmed + blank drop
	require.Equal(t, map[string]string{"telemetry": "cpu.samples.total"}, got0.Aliases)
	require.Len(t, got0.ColumnRefs, 1)
	require.Equal(t, "tbl_a", got0.ColumnRefs[0].Table)
	require.Equal(t, "col_x", got0.ColumnRefs[0].Column)
	require.NotNil(t, got0.ColumnRefs[0].RendererID)
	require.Equal(t, "rendererA", *got0.ColumnRefs[0].RendererID)

	// Lookup by identifier
	id, err := svc.LookupIDByIdentifier(ctx, "cpu.samples.total")
	require.NoError(t, err)
	require.Equal(t, ids[0], id)
}

func TestUpsert_MergeAcrossCalls_CoreFieldsReplace(t *testing.T) {
	ctx := context.Background()
	svc := newSvc(t)

	// initial insert
	_, err := svc.Upsert(ctx, []render.MeasurementSpec{
		spec("net.rx", "Net RX",
			[]string{"network"},
			map[string]string{"legacy": "NET_RX"},
			render.ColumnRef{Table: "t1", Column: "c1"}),
	})
	require.NoError(t, err)

	// update + merge
	ids, err := svc.Upsert(ctx, []render.MeasurementSpec{
		spec("net.rx", "Network Receive",
			[]string{"network", "telemetry:v1"},
			map[string]string{"telemetry": "net.rx.v1"},
			render.ColumnRef{Table: "t2", Column: "c2"}),
	})
	require.NoError(t, err)
	require.Len(t, ids, 1)

	got, err := svc.GetByID(ctx, ids[0])
	require.NoError(t, err)
	require.Equal(t, "Network Receive", got.Name) // core fields replaced
	require.Equal(t, "short:Network Receive", got.ShortDescription)
	require.ElementsMatch(t, []string{"network", "telemetry:v1"}, got.Tags)
	require.Equal(t, map[string]string{"legacy": "NET_RX", "telemetry": "net.rx.v1"}, got.Aliases)
	require.ElementsMatch(t, []render.ColumnRef{
		{Table: "t1", Column: "c1", RendererID: nil},
		{Table: "t2", Column: "c2", RendererID: nil},
	}, got.ColumnRefs)
}

func TestUpsert_LastOneWinsWithinSameCall_DuplicatedIdentifier(t *testing.T) {
	ctx := context.Background()
	svc := newSvc(t)

	ids, err := svc.Upsert(ctx, []render.MeasurementSpec{
		spec("dup.id", "First", []string{"a"}, map[string]string{"s": "A"}),
		spec("dup.id", "Second", []string{"b"}, map[string]string{"s": "B"}),
	})
	require.NoError(t, err)
	require.Len(t, ids, 2)

	// Both inputs return IDs (same persisted row), but persisted core should be from the last one.
	got, err := svc.GetByID(ctx, ids[1])
	require.NoError(t, err)
	require.Equal(t, "Second", got.Name)
	require.Equal(t, "short:Second", got.ShortDescription)
	// Tags/Aliases should merge across the staging set
	require.ElementsMatch(t, []string{"a", "b"}, got.Tags)
	require.Equal(t, map[string]string{"s": "B"}, got.Aliases) // single value per source
}

func TestLookupIDByIdentifier_NotFound_ErrorMessage(t *testing.T) {
	ctx := context.Background()
	svc := newSvc(t)

	_, err := svc.LookupIDByIdentifier(ctx, "missing.id")
	require.Error(t, err)
	require.Contains(t, err.Error(), "measurement identifier 'missing.id' not found")
}

func TestLookupIDByName_NotFound_And_Multiple(t *testing.T) {
	ctx := context.Background()
	svc := newSvc(t)

	_, err := svc.Upsert(ctx, []render.MeasurementSpec{
		spec("id.a", "Shared", nil, nil),
		spec("id.b", "Shared", nil, nil),
	})
	require.NoError(t, err)

	_, err = svc.LookupIDByName(ctx, "Nope")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")

	_, err = svc.LookupIDByName(ctx, "Shared")
	require.Error(t, err)
	require.Contains(t, err.Error(), "multiple measurements")
}

func TestUpsertAlias_And_checkAlias(t *testing.T) {
	ctx := context.Background()
	svc := newSvc(t)

	ids, err := svc.Upsert(ctx, []render.MeasurementSpec{spec("x.id", "X", nil, nil)})
	require.NoError(t, err)
	require.Len(t, ids, 1)

	// Good insert
	require.NoError(t, svc.UpsertAlias(ctx, ids[0], "legacy", "X_ALIAS"))
	// Duplicate is a no-op
	require.NoError(t, svc.UpsertAlias(ctx, ids[0], "legacy", "X_ALIAS"))

	// Bad inputs via checkAlias
	require.Error(t, checkAlias("  ", "x"))
	require.Error(t, checkAlias("src", "   "))

	// Visible in GetByID
	got, err := svc.GetByID(ctx, ids[0])
	require.NoError(t, err)
	require.Equal(t, map[string]string{"legacy": "X_ALIAS"}, got.Aliases)
}

func TestCreateViewByTableRefs_FiltersByTables(t *testing.T) {
	ctx := context.Background()
	svc, db := newSvcWithDb(t)

	_, err := svc.Upsert(ctx, []render.MeasurementSpec{
		spec("m1", "M1", nil, nil, render.ColumnRef{Table: "T_A", Column: "c1"}),
		spec("m2", "M2", nil, nil, render.ColumnRef{Table: "T_B", Column: "c2"}),
	})
	require.NoError(t, err)

	err = svc.CreateViewByTableRefs(ctx, "vw_test", []string{"T_A", "T_A"})
	require.NoError(t, err)

	rows, err := db.Conn.QueryContext(ctx, `SELECT identifier FROM vw_test ORDER BY identifier`)
	require.NoError(t, err)
	defer rows.Close()

	var got []string
	for rows.Next() {
		var id string
		require.NoError(t, rows.Scan(&id))
		got = append(got, id)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []string{"m1"}, got)
}

func TestUpsert_Concurrent(t *testing.T) {
	ctx := context.Background()
	svc, db := newSvcWithDb(t)

	const (
		Goroutines   = 6  // number of concurrent writers
		BatchSize    = 40 // per-writer unique ids
		SharedIDsNum = 3  // ids intentionally shared across writers
	)

	// Pre-build shared identifiers
	sharedIDs := make([]string, 0, SharedIDsNum)
	for i := 0; i < SharedIDsNum; i++ {
		sharedIDs = append(sharedIDs, fmt.Sprintf("con.shared.%02d", i))
	}

	start := make(chan struct{})
	var eg errgroup.Group

	for g := 0; g < Goroutines; g++ {
		g := g
		eg.Go(func() error {
			<-start

			// Disjoint identifiers for this goroutine
			specs := make([]render.MeasurementSpec, 0, BatchSize+SharedIDsNum)

			for i := 0; i < BatchSize; i++ {
				id := fmt.Sprintf("con.g%02d.%03d", g, i)
				specs = append(specs, render.MeasurementSpec{
					Identifier:  render.SlugIdentifier(id),
					Name:        fmt.Sprintf("Name-%d-%d", g, i),
					Description: "concurrent insert",
					Units:       "samples",
					Tags:        []string{fmt.Sprintf("g%d", g), "kind:test"},
					Aliases:     map[string]string{"telemetry": id},
					ColumnRefs:  []render.ColumnRef{{Table: "tbl_c", Column: "col_c"}},
				})
			}

			// Add shared identifiers with a tag unique to this goroutine.
			for _, sid := range sharedIDs {
				specs = append(specs, render.MeasurementSpec{
					Identifier:  render.SlugIdentifier(sid),
					Name:        fmt.Sprintf("SharedName-g%d", g), // non-deterministic final value
					Description: "shared",
					Units:       "samples",
					Tags:        []string{fmt.Sprintf("g%d", g)},
					Aliases:     map[string]string{"telemetry": sid},
					ColumnRefs:  []render.ColumnRef{{Table: "tbl_s", Column: "col_s"}},
				})
			}

			_, err := svc.Upsert(ctx, specs)
			return err
		})
	}

	close(start)
	require.NoError(t, eg.Wait())

	// Verify total number of unique measurement rows:
	// Goroutines*BatchSize disjoint + SharedIDsNum (shared are single logical rows).
	var count int
	require.NoError(t, db.Conn.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM ref_measurements`).Scan(&count))
	require.Equal(t, Goroutines*BatchSize+SharedIDsNum, count,
		"all disjoint ids inserted once; shared ids collapse to one each")

	// Spot-check a few random disjoint ids exist and round-trip
	checkID := func(identifier string) {
		id, err := svc.LookupIDByIdentifier(ctx, render.SlugIdentifier(identifier))
		require.NoError(t, err, "lookup should succeed for %s", identifier)
		spec, err := svc.GetByID(ctx, id)
		require.NoError(t, err)
		require.Equal(t, render.SlugIdentifier(identifier), spec.Identifier)
	}
	checkID(fmt.Sprintf("con.g%02d.%03d", 0, 0))
	checkID(fmt.Sprintf("con.g%02d.%03d", Goroutines-1, BatchSize-1))

	// For each shared id, tags should be the union of all writers: {"g0","g1",...}
	expectedTags := make([]string, 0, Goroutines)
	for g := 0; g < Goroutines; g++ {
		expectedTags = append(expectedTags, fmt.Sprintf("g%d", g))
	}

	for _, sid := range sharedIDs {
		id, err := svc.LookupIDByIdentifier(ctx, render.SlugIdentifier(sid))
		require.NoError(t, err, "shared id must exist: %s", sid)

		spec, err := svc.GetByID(ctx, id)
		require.NoError(t, err)

		// Tags contain the full union (order not guaranteed)
		require.ElementsMatch(t, expectedTags, spec.Tags)

		// ColumnRefs merged (dedup by (measurement_id, table, column))
		require.NotEmpty(t, spec.ColumnRefs)
		// we inserted (tbl_s,col_s) from every writer; uniqueness keeps just one
		require.Equal(t, "tbl_s", spec.ColumnRefs[0].Table)
		require.Equal(t, "col_s", spec.ColumnRefs[0].Column)

		// Name is last-writer-wins; just assert it's one of the contenders.
		// (We don’t know which goroutine wrote last.)
		allowed := false
		for g := 0; g < Goroutines; g++ {
			if spec.Name == fmt.Sprintf("SharedName-g%d", g) {
				allowed = true
				break
			}
		}
		require.True(t, allowed, "final name must be one of the concurrent writers")
	}
}

func TestEnsureManifest_AddsCoreEntries(t *testing.T) {
	svc := newSvc(t)

	m := render.NewManifest()
	svc.EnsureManifest(&m)

	entries := m.Entries()
	require.Len(t, entries, 6)

	wantNames := []string{
		"ref_measurements",
		"ref_measurement_tags",
		"ref_measurement_aliases",
		"ref_measurement_column_refs",
		"ref_measurement_groups",
		"ref_measurement_group_links",
	}

	for i, name := range wantNames {
		e := entries[i]
		require.Equal(t, name, e.Info().ComponentType().Name, "component name")
		require.Equal(t, "1.0", e.Info().ComponentType().SchemaVersion, "schema version")
		require.Equal(t, -1, e.Info().RendererIndex(), "renderer index")
		require.Empty(t, e.Info().AssociatedContent(), "associated content must be empty")
		require.Equal(t, name, e.TableName(), "first instance uses bare component name as table")
		require.False(t, e.IsHidden(), "entries added by EnsureManifest should not be hidden")

		// Sanity: lookup by table name should succeed.
		got, err := m.GetEntry(name)
		require.NoError(t, err)
		require.Equal(t, name, got.TableName())
	}
}

func TestCreateDrilldownMeasurementsViewByTableRefs_ManifestAndDBOnly(t *testing.T) {
	ctx := context.Background()
	svc, db := newSvcWithDb(t)

	manifest := render.NewManifest()
	name, err := svc.CreateDrilldownMeasurementsViewByTableRefs(ctx, &manifest, []string{"T_A"}, render.RendererIdentity{Index: 123, Name: "foo"}, []run.RunID{{Value: "456"}})
	require.NoError(t, err)

	// Exactly one manifest entry added.
	entries := manifest.Entries()
	require.Len(t, entries, 1)

	entry := entries[0]
	require.Equal(t, "drilldown_measurements", name)
	require.Equal(t, "drilldown_measurements", entry.Info().ComponentType().Name)
	require.Equal(t, "2.1", entry.Info().ComponentType().SchemaVersion)
	require.Equal(t, 123, entry.Info().RendererIndex())
	require.False(t, entry.IsHidden())
	require.Equal(t, "drilldown_measurements", entry.TableName(), "first instance gets unsuffixed name")

	// Manifest should resolve the entry by table name.
	got, err := manifest.GetEntry(entry.TableName())
	require.NoError(t, err)
	require.Equal(t, entry.TableName(), got.TableName())

	// The view should exist in DuckDB. (DuckDB lists views in information_schema.tables)
	var count int
	require.NoError(t, db.Conn.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM information_schema.tables
		WHERE table_name = ?`, entry.TableName()).Scan(&count))
	require.Equal(t, 1, count, "drilldown view must exist in DB")
}

func TestUpsertGroups_HappyPath(t *testing.T) {
	ctx := context.Background()
	svc := newSvc(t)

	groups := []render.MeasurementGroup{
		{Name: "Backstreet Boys", Description: "Backstreet's back, alright!"},
		{Name: "N Sync", Description: "Bye Bye Bye"},
	}

	ids, err := svc.UpsertGroups(ctx, groups)
	require.NoError(t, err)

	// Ensure IDs are unique by using a set
	idSet := make(map[render.MeasurementGroupID]struct{})
	for _, id := range ids {
		idSet[id] = struct{}{}
	}
	require.Len(t, idSet, len(groups))
}
func TestGetGroupByID_HappyPath(t *testing.T) {
	ctx := context.Background()
	svc := newSvc(t)

	groups := []render.MeasurementGroup{
		{Name: "Backstreet Boys", Description: "Backstreet's back, alright!"},
		{Name: "N Sync", Description: "Bye Bye Bye"},
	}
	ids, err := svc.UpsertGroups(ctx, groups)
	require.NoError(t, err)
	require.Len(t, ids, len(groups))

	got_group_0, err := svc.GetGroupByID(ctx, ids[0])
	require.NoError(t, err)
	require.Equal(t, "Backstreet Boys", got_group_0.Name)
	require.Equal(t, "Backstreet's back, alright!", got_group_0.Description)

	got_group_1, err := svc.GetGroupByID(ctx, ids[1])
	require.NoError(t, err)
	require.Equal(t, "N Sync", got_group_1.Name)
	require.Equal(t, "Bye Bye Bye", got_group_1.Description)
}

func TestLookupGroupByName_HappyPath(t *testing.T) {
	ctx := context.Background()
	svc := newSvc(t)

	groups := []render.MeasurementGroup{
		{Name: "Backstreet Boys", Description: "Backstreet's back, alright!"},
		{Name: "N Sync", Description: "Bye Bye Bye"},
	}

	ids, err := svc.UpsertGroups(ctx, groups)
	require.NoError(t, err)
	require.Len(t, ids, len(groups))

	foundID, err := svc.LookupGroupByName(ctx, "Backstreet Boys")
	require.NoError(t, err)
	require.Equal(t, ids[0], foundID)

	foundID, err = svc.LookupGroupByName(ctx, "N Sync")
	require.NoError(t, err)
	require.Equal(t, ids[1], foundID)
}

func TestGetGroupByID_NotFound(t *testing.T) {
	ctx := context.Background()
	svc := newSvc(t)

	_, err := svc.GetGroupByID(ctx, render.MeasurementGroupID(9999))
	require.Error(t, err)
	require.Contains(t, err.Error(), "measurement group id 9999 not found")
}

func TestLookupGroupByName_NotFound(t *testing.T) {
	ctx := context.Background()
	svc := newSvc(t)

	_, err := svc.LookupGroupByName(ctx, "Non Existent Group")
	require.Error(t, err)
	require.Contains(t, err.Error(), "measurement group name 'Non Existent Group' not found")
}

func TestUpsertGroups_EmptyInput(t *testing.T) {
	ctx := context.Background()
	svc := newSvc(t)
	ids, err := svc.UpsertGroups(ctx, []render.MeasurementGroup{})
	require.NoError(t, err)
	require.Len(t, ids, 0)
}

func TestLookupGroupByName_EmptyName(t *testing.T) {
	ctx := context.Background()
	svc := newSvc(t)

	_, err := svc.LookupGroupByName(ctx, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "measurement group name '' not found")
}

func TestGetByIDs(t *testing.T) {
	t.Run("retrieves entries from DB correctly", func(t *testing.T) {
		ctx := context.Background()
		svc := newSvc(t)

		specs := []render.MeasurementSpec{
			spec("cpu.samples.total", "CPU Samples",
				[]string{" total ", " ", "kind:count"},
				map[string]string{"telemetry": "cpu.samples.total"},
				render.ColumnRef{Table: "tbl_a", Column: "col_x", RendererID: util.Ptr("  rendererA  ")}),
			spec("gpu.util", "GPU Util",
				[]string{"util", "kind:percent"},
				map[string]string{"legacy": "GPU_UTIL"},
				render.ColumnRef{Table: "tbl_b", Column: "col_y"}), // nil renderer
			spec("something.else", "Something a bit different!",
				[]string{"my_tag", "kind:total"},
				map[string]string{"last_week": "something.different"},
				render.ColumnRef{Table: "tbl_c", Column: "col_z"}), // nil renderer
		}

		ids, err := svc.Upsert(ctx, specs)
		require.NoError(t, err)
		require.Len(t, ids, 3)

		result, err := svc.GetByIDs(context.Background(), []render.MeasurementID{ids[0], ids[2]})
		require.NoError(t, err)
		assert.Len(t, result, 2)

		got0, ok := result[ids[0]]
		assert.True(t, ok)
		require.Equal(t, render.SlugIdentifier("cpu.samples.total"), got0.Identifier)
		require.Equal(t, "CPU Samples", got0.Name)
		require.Equal(t, "short:CPU Samples", got0.ShortDescription)
		require.ElementsMatch(t, []string{"total", "kind:count"}, got0.Tags) // trimmed + blank drop
		require.Equal(t, map[string]string{"telemetry": "cpu.samples.total"}, got0.Aliases)
		require.Len(t, got0.ColumnRefs, 1)
		require.Equal(t, "tbl_a", got0.ColumnRefs[0].Table)
		require.Equal(t, "col_x", got0.ColumnRefs[0].Column)
		require.NotNil(t, got0.ColumnRefs[0].RendererID)
		require.Equal(t, "rendererA", *got0.ColumnRefs[0].RendererID)

		got2, ok := result[ids[2]]
		assert.True(t, ok)
		require.Equal(t, render.SlugIdentifier("something.else"), got2.Identifier)
		require.Equal(t, "Something a bit different!", got2.Name)
		require.Equal(t, "short:Something a bit different!", got2.ShortDescription)
		require.ElementsMatch(t, []string{"my_tag", "kind:total"}, got2.Tags) // trimmed + blank drop
		require.Equal(t, map[string]string{"last_week": "something.different"}, got2.Aliases)
		require.Len(t, got2.ColumnRefs, 1)
		require.Equal(t, "tbl_c", got2.ColumnRefs[0].Table)
		require.Equal(t, "col_z", got2.ColumnRefs[0].Column)
		require.Nil(t, got2.ColumnRefs[0].RendererID)
	})
	t.Run("returns an empty map if no ids are provided", func(t *testing.T) {
		ctx := context.Background()
		svc := newSvc(t)

		specs := []render.MeasurementSpec{
			spec("cpu.samples.total", "CPU Samples",
				[]string{" total ", " ", "kind:count"},
				map[string]string{"telemetry": "cpu.samples.total"},
				render.ColumnRef{Table: "tbl_a", Column: "col_x", RendererID: util.Ptr("  rendererA  ")}),
			spec("gpu.util", "GPU Util",
				[]string{"util", "kind:percent"},
				map[string]string{"legacy": "GPU_UTIL"},
				render.ColumnRef{Table: "tbl_b", Column: "col_y"}), // nil renderer
		}

		ids, err := svc.Upsert(ctx, specs)
		require.NoError(t, err)
		require.Len(t, ids, 2)

		result, err := svc.GetByIDs(context.Background(), []render.MeasurementID{})
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Len(t, result, 0)
	})
}
