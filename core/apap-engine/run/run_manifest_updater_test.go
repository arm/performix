// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"errors"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
)

type recordingRunWriter struct {
	writeManifestErr error
	manifestWrites   []RunBuilder
	entityDirWrites  []RunBuilder
}

func (w *recordingRunWriter) WriteManifest(builder RunBuilder) error {
	w.manifestWrites = append(w.manifestWrites, builder.Clone())
	return w.writeManifestErr
}

func (w *recordingRunWriter) WriteEntityDirs(builder RunBuilder) error {
	w.entityDirWrites = append(w.entityDirWrites, builder.Clone())
	return nil
}

func newManifestUpdaterTestBuilder() RunBuilder {
	return RunBuilder{
		runID:    RunID{Value: "run-123"},
		basePath: "/tmp/run-collection",
		runPath:  "/tmp/run-collection/run-123",
	}
}

func TestRunManifestUpdater(t *testing.T) {
	componentType := cdf.ComponentType{Name: "test-component", SchemaVersion: "v1"}

	t.Run("writes manifest for component lifecycle", func(t *testing.T) {
		builder := newManifestUpdaterTestBuilder()
		writer := &recordingRunWriter{}
		updater := NewRunManifestUpdater(&builder, writer)

		require.NoError(t, updater.AddComponentWithFlags("entity/output.txt", componentType, ComponentFlags{Pending: true}))
		require.NoError(t, updater.ClearPending("\\entity\\output.txt"))
		require.NoError(t, updater.RemoveComponent("/entity/output.txt/"))

		require.Len(t, writer.manifestWrites, 3)
		assert.Len(t, writer.entityDirWrites, 0)

		pendingEntry := writer.manifestWrites[0].buildManifest().Lookup("entity/output.txt")
		require.NotNil(t, pendingEntry)
		assert.True(t, pendingEntry.Pending)
		assert.Equal(t, componentType, pendingEntry.ComponentType)

		completeEntry := writer.manifestWrites[1].buildManifest().Lookup("entity/output.txt")
		require.NotNil(t, completeEntry)
		assert.False(t, completeEntry.Pending)

		assert.Nil(t, writer.manifestWrites[2].buildManifest().Lookup("entity/output.txt"))
		assert.Equal(t, 0, builder.ComponentCount())
	})

	t.Run("preserves compression while clearing pending", func(t *testing.T) {
		builder := newManifestUpdaterTestBuilder()
		writer := &recordingRunWriter{}
		updater := NewRunManifestUpdater(&builder, writer)

		require.NoError(t, updater.AddComponentWithFlags("entity/output.csv", componentType, ComponentFlags{Pending: true, Compressed: true}))
		require.NoError(t, updater.ClearPending("entity/output.csv"))

		entry := builder.buildManifest().Lookup("entity/output.csv")
		require.NotNil(t, entry)
		require.False(t, entry.Pending)
		require.True(t, entry.Compressed)
		require.Equal(t, filepath.Join(builder.runPath, "entity", "output.csv")+cdf.ZstdSuffix, builder.components[0].AbsolutePath)
	})

	t.Run("write entity dirs uses current builder without writing manifest", func(t *testing.T) {
		builder := newManifestUpdaterTestBuilder()
		writer := &recordingRunWriter{}
		updater := NewRunManifestUpdater(&builder, writer)

		require.NoError(t, updater.AddComponent("entity/output.txt", componentType))
		require.NoError(t, updater.WriteEntityDirs())

		require.Len(t, writer.manifestWrites, 1)
		require.Len(t, writer.entityDirWrites, 1)
		assert.NotNil(t, writer.entityDirWrites[0].buildManifest().Lookup("entity/output.txt"))
	})

	t.Run("add tool output writes manifest", func(t *testing.T) {
		builder := newManifestUpdaterTestBuilder()
		writer := &recordingRunWriter{}
		updater := NewRunManifestUpdater(&builder, writer)

		require.NoError(t, updater.AddToolOutput("neoprof", "1.1.0", 0))

		require.Len(t, writer.manifestWrites, 1)
		manifest := writer.manifestWrites[0].buildManifest()
		require.Len(t, manifest.ToolsUsed, 1)
		assert.Equal(t, cdf.ToolUsed{
			Tool:       "neoprof",
			Version:    "1.1.0",
			Invocation: 0,
		}, manifest.ToolsUsed[0])
	})

	t.Run("does not commit builder when manifest write fails", func(t *testing.T) {
		builder := newManifestUpdaterTestBuilder()
		writer := &recordingRunWriter{writeManifestErr: errors.New("write failed")}
		updater := NewRunManifestUpdater(&builder, writer)

		err := updater.AddComponent("entity/output.txt", componentType)

		require.Error(t, err)
		require.Len(t, writer.manifestWrites, 1)
		assert.NotNil(t, writer.manifestWrites[0].buildManifest().Lookup("entity/output.txt"))
		assert.Equal(t, 0, builder.ComponentCount())
		assert.Nil(t, builder.buildManifest().Lookup("entity/output.txt"))
	})

	t.Run("remove pending component only removes pending entry", func(t *testing.T) {
		builder := newManifestUpdaterTestBuilder()
		writer := &recordingRunWriter{}
		updater := NewRunManifestUpdater(&builder, writer)

		require.NoError(t, updater.AddComponent("entity/complete.txt", componentType))
		require.NoError(t, updater.RemovePendingComponent("entity/complete.txt"))
		assert.NotNil(t, builder.buildManifest().Lookup("entity/complete.txt"))

		require.NoError(t, updater.AddComponentWithFlags("entity/pending.txt", componentType, ComponentFlags{Pending: true}))
		require.NoError(t, updater.RemovePendingComponent("entity/pending.txt"))
		require.Len(t, writer.manifestWrites, 4)
		assert.Nil(t, builder.buildManifest().Lookup("entity/pending.txt"))
		assert.NotNil(t, builder.buildManifest().Lookup("entity/complete.txt"))
	})
}

func TestManifestCompressionRegistration(t *testing.T) {
	componentType := cdf.ComponentType{Name: "data", SchemaVersion: "1.0"}
	for _, firstCompressed := range []bool{false, true} {
		t.Run(strconv.FormatBool(firstCompressed), func(t *testing.T) {
			builder := newManifestUpdaterTestBuilder()
			writer := &recordingRunWriter{}
			updater := NewRunManifestUpdater(&builder, writer)
			require.NoError(t, updater.AddComponentWithFlags("entity/*.csv", componentType, ComponentFlags{Pending: true, Compressed: firstCompressed}))
			require.Error(t, updater.AddComponentWithFlags("entity/foo.csv", componentType, ComponentFlags{Pending: true, Compressed: !firstCompressed}))
			require.Len(t, writer.manifestWrites, 1)
			require.Len(t, builder.buildManifest().Entries, 1)
			require.True(t, builder.buildManifest().Entries[0].Pending)
			require.Equal(t, firstCompressed, builder.buildManifest().Entries[0].Compressed)
			// A failed transfer can leave files, so removing its entry does not release its declaration.
			require.NoError(t, updater.RemoveComponent("entity/*.csv"))
			require.Error(t, updater.AddComponentWithFlags("entity/foo.csv", componentType, ComponentFlags{Compressed: !firstCompressed}))
		})
	}
}

func TestManifestCompressionRegistrationConcurrent(t *testing.T) {
	builder := newManifestUpdaterTestBuilder()
	updater := NewRunManifestUpdater(&builder, &recordingRunWriter{})
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, compressed := range []bool{false, true} {
		go func(compressed bool) {
			<-start
			results <- updater.AddComponentWithFlags("entity/*", cdf.ComponentType{}, ComponentFlags{Compressed: compressed})
		}(compressed)
	}
	close(start)
	first, second := <-results, <-results
	if first == nil {
		require.Error(t, second)
	} else {
		require.NoError(t, second)
	}
	require.Len(t, builder.buildManifest().Entries, 1)
}

func TestManifestFailedWriteDoesNotReserveCompression(t *testing.T) {
	builder := newManifestUpdaterTestBuilder()
	writer := &recordingRunWriter{writeManifestErr: errors.New("write failed")}
	updater := NewRunManifestUpdater(&builder, writer)
	require.Error(t, updater.AddComponentWithFlags("entity/*", cdf.ComponentType{}, ComponentFlags{Compressed: true}))
	writer.writeManifestErr = nil
	require.NoError(t, updater.AddComponent("entity/*", cdf.ComponentType{}))
	require.Len(t, builder.buildManifest().Entries, 1)
}

func TestManifestCompressionChecksInitialComponents(t *testing.T) {
	builder := newManifestUpdaterTestBuilder()
	builder.AddComponent(cdf.ComponentType{}, "entity/foo.csv.zst")
	updater := NewRunManifestUpdater(&builder, &recordingRunWriter{})
	require.Error(t, updater.AddComponentWithFlags("entity/foo.csv", cdf.ComponentType{}, ComponentFlags{Compressed: true}))
	require.Len(t, builder.buildManifest().Entries, 1)
}

func TestManifestAllowsDisjointMixedCompressionGlobs(t *testing.T) {
	builder := newManifestUpdaterTestBuilder()
	updater := NewRunManifestUpdater(&builder, &recordingRunWriter{})
	require.NoError(t, updater.AddComponentWithFlags("entity/*.csv", cdf.ComponentType{}, ComponentFlags{Compressed: true}))
	require.NoError(t, updater.AddComponent("entity/*.parquet", cdf.ComponentType{}))
	require.Len(t, builder.buildManifest().Entries, 2)
}
