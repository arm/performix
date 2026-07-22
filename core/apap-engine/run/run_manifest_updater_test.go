// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"errors"
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

		require.NoError(t, updater.AddPendingComponent("entity/output.txt", componentType))
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

		require.NoError(t, updater.AddPendingComponent("entity/pending.txt", componentType))
		require.NoError(t, updater.RemovePendingComponent("entity/pending.txt"))
		require.Len(t, writer.manifestWrites, 4)
		assert.Nil(t, builder.buildManifest().Lookup("entity/pending.txt"))
		assert.NotNil(t, builder.buildManifest().Lookup("entity/complete.txt"))
	})
}
