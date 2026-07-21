// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

func createUpdateTestRun(t *testing.T, runCollection *RunCollection) RunID {
	t.Helper()
	builder, err := runCollection.RunBuilder()
	require.NoError(t, err)
	builder.AddEntity("fakeEntity")
	runID, err := runCollection.CreateRun(builder, &cdf.Metadata{TargetConfig: emptySSHTargetConfig})
	require.NoError(t, err)
	return runID
}

func TestUpdateRun(t *testing.T) {
	t.Run("updates source code paths", func(t *testing.T) {
		runCollection := NewTestRunCollection(t, t.TempDir())
		runID := createUpdateTestRun(t, runCollection)

		err := runCollection.UpdateRun(context.Background(), runID, RunUpdate{
			Operations: []RunUpdateOperation{
				SetHostSourceCodePaths{HostSourceCodePaths: HostSourceCodePath{Paths: []string{"/foo", "/bar"}}},
			},
		})
		require.NoError(t, err)

		sourcePaths, err := ReadHostSourceCodePath(runCollection.getSourceCodePath(runID))
		require.NoError(t, err)
		assert.Equal(t, []string{"/foo", "/bar"}, sourcePaths.Paths)
	})

	t.Run("clears source code paths", func(t *testing.T) {
		runCollection := NewTestRunCollection(t, t.TempDir())
		runID := createUpdateTestRun(t, runCollection)
		require.NoError(t, WriteHostSourceCodePath(runCollection.getSourceCodePath(runID), &HostSourceCodePath{Paths: []string{"/foo"}}))

		err := runCollection.UpdateRun(context.Background(), runID, RunUpdate{
			Operations: []RunUpdateOperation{ClearHostSourceCodePaths{}},
		})
		require.NoError(t, err)

		sourcePaths, err := ReadHostSourceCodePath(runCollection.getSourceCodePath(runID))
		require.NoError(t, err)
		assert.Equal(t, []string{}, sourcePaths.Paths)
	})

	t.Run("sets group and preserves tags", func(t *testing.T) {
		runCollection := NewTestRunCollection(t, t.TempDir())
		runID := createUpdateTestRun(t, runCollection)
		require.NoError(t, WriteRunCategorization(runCollection.getCategorizationPath(runID), &RunCategorization{
			Group: "old-group",
			Tags:  []string{"tag-a", "tag-b"},
		}))

		err := runCollection.UpdateRun(context.Background(), runID, RunUpdate{
			Operations: []RunUpdateOperation{SetRunGroup{Group: "  new-group  "}},
		})
		require.NoError(t, err)

		categorization, err := ReadRunCategorization(runCollection.getCategorizationPath(runID))
		require.NoError(t, err)
		assert.Equal(t, "new-group", categorization.Group)
		assert.Equal(t, []string{"tag-a", "tag-b"}, categorization.Tags)
	})

	t.Run("clears group and preserves tags", func(t *testing.T) {
		runCollection := NewTestRunCollection(t, t.TempDir())
		runID := createUpdateTestRun(t, runCollection)
		require.NoError(t, WriteRunCategorization(runCollection.getCategorizationPath(runID), &RunCategorization{
			Group: "old-group",
			Tags:  []string{"tag-a"},
		}))

		err := runCollection.UpdateRun(context.Background(), runID, RunUpdate{
			Operations: []RunUpdateOperation{ClearRunGroup{}},
		})
		require.NoError(t, err)

		categorization, err := ReadRunCategorization(runCollection.getCategorizationPath(runID))
		require.NoError(t, err)
		assert.Empty(t, categorization.Group)
		assert.Equal(t, []string{"tag-a"}, categorization.Tags)
	})

	t.Run("sets tags and preserves group", func(t *testing.T) {
		runCollection := NewTestRunCollection(t, t.TempDir())
		runID := createUpdateTestRun(t, runCollection)
		require.NoError(t, WriteRunCategorization(runCollection.getCategorizationPath(runID), &RunCategorization{
			Group: "group-a",
			Tags:  []string{"old-tag"},
		}))

		err := runCollection.UpdateRun(context.Background(), runID, RunUpdate{
			Operations: []RunUpdateOperation{SetRunTags{Tags: []string{"tag-a", " tag-b ", "tag-a"}}},
		})
		require.NoError(t, err)

		categorization, err := ReadRunCategorization(runCollection.getCategorizationPath(runID))
		require.NoError(t, err)
		assert.Equal(t, "group-a", categorization.Group)
		assert.Equal(t, []string{"tag-a", "tag-b"}, categorization.Tags)
	})

	t.Run("adds tags without duplicating existing tags", func(t *testing.T) {
		runCollection := NewTestRunCollection(t, t.TempDir())
		runID := createUpdateTestRun(t, runCollection)
		require.NoError(t, WriteRunCategorization(runCollection.getCategorizationPath(runID), &RunCategorization{
			Tags: []string{"tag-a", "tag-b"},
		}))

		err := runCollection.UpdateRun(context.Background(), runID, RunUpdate{
			Operations: []RunUpdateOperation{AddRunTags{Tags: []string{"tag-b", "tag-c", "tag-c"}}},
		})
		require.NoError(t, err)

		categorization, err := ReadRunCategorization(runCollection.getCategorizationPath(runID))
		require.NoError(t, err)
		assert.Equal(t, []string{"tag-a", "tag-b", "tag-c"}, categorization.Tags)
	})

	t.Run("removes tags", func(t *testing.T) {
		runCollection := NewTestRunCollection(t, t.TempDir())
		runID := createUpdateTestRun(t, runCollection)
		require.NoError(t, WriteRunCategorization(runCollection.getCategorizationPath(runID), &RunCategorization{
			Tags: []string{"tag-a", "tag-b", "tag-c"},
		}))

		err := runCollection.UpdateRun(context.Background(), runID, RunUpdate{
			Operations: []RunUpdateOperation{RemoveRunTags{Tags: []string{"tag-b", "tag-z"}}},
		})
		require.NoError(t, err)

		categorization, err := ReadRunCategorization(runCollection.getCategorizationPath(runID))
		require.NoError(t, err)
		assert.Equal(t, []string{"tag-a", "tag-c"}, categorization.Tags)
	})

	t.Run("clears tags", func(t *testing.T) {
		runCollection := NewTestRunCollection(t, t.TempDir())
		runID := createUpdateTestRun(t, runCollection)
		require.NoError(t, WriteRunCategorization(runCollection.getCategorizationPath(runID), &RunCategorization{
			Group: "group-a",
			Tags:  []string{"tag-a"},
		}))

		err := runCollection.UpdateRun(context.Background(), runID, RunUpdate{
			Operations: []RunUpdateOperation{ClearRunTags{}},
		})
		require.NoError(t, err)

		categorization, err := ReadRunCategorization(runCollection.getCategorizationPath(runID))
		require.NoError(t, err)
		assert.Equal(t, "group-a", categorization.Group)
		assert.Equal(t, []string{}, categorization.Tags)
	})

	t.Run("returns categorized error when categorization write fails", func(t *testing.T) {
		runCollection := NewTestRunCollection(t, t.TempDir())
		runID := createUpdateTestRun(t, runCollection)
		categorizationPath := runCollection.getCategorizationPath(runID)
		require.NoError(t, WriteRunCategorization(categorizationPath, &RunCategorization{Group: "old-group"}))
		readOnlyOwnerPerm := os.FileMode(0o400)
		require.NoError(t, os.Chmod(categorizationPath, readOnlyOwnerPerm))

		err := runCollection.UpdateRun(context.Background(), runID, RunUpdate{
			Operations: []RunUpdateOperation{SetRunGroup{Group: "new-group"}},
		})

		var msg message.Message
		require.True(t, errors.As(err, &msg))
		assert.Equal(t, message.EngineRunUpdateCategorization, msg.Code())
		assert.Equal(t, map[string]string{
			"runID": runID.Value,
			"path":  categorizationPath,
		}, msg.Metadata())
		assert.Error(t, msg.Unwrap())
	})

	t.Run("applies multiple tag operations in order", func(t *testing.T) {
		runCollection := NewTestRunCollection(t, t.TempDir())
		runID := createUpdateTestRun(t, runCollection)
		require.NoError(t, WriteRunCategorization(runCollection.getCategorizationPath(runID), &RunCategorization{
			Tags: []string{"one", "two", "three"},
		}))

		err := runCollection.UpdateRun(context.Background(), runID, RunUpdate{
			Operations: []RunUpdateOperation{
				RemoveRunTags{Tags: []string{"two", "three"}},
				AddRunTags{Tags: []string{"two", "four"}},
			},
		})
		require.NoError(t, err)

		categorization, err := ReadRunCategorization(runCollection.getCategorizationPath(runID))
		require.NoError(t, err)
		assert.Equal(t, []string{"one", "two", "four"}, categorization.Tags)
	})

	t.Run("rejects empty group and tags", func(t *testing.T) {
		_, err := RunUpdate{Operations: []RunUpdateOperation{SetRunGroup{Group: "   "}}}.Normalize()
		assert.Equal(t, message.New(message.EngineRunInvalidGroup).WithMetadata(map[string]string{"group": `"   "`}), err)

		_, err = RunUpdate{Operations: []RunUpdateOperation{AddRunTags{Tags: []string{"tag-a", " "}}}}.Normalize()
		expectedTags := util.DisplayErrorStringSlice([]string{"tag-a", " "})
		assert.Equal(t, message.New(message.EngineRunInvalidTags).WithMetadata(map[string]string{"tags": expectedTags}), err)
	})
}

func TestUpdateRuns(t *testing.T) {
	t.Run("updates available runs and returns per-run errors", func(t *testing.T) {
		runCollection := NewTestRunCollection(t, t.TempDir())
		runID1 := createUpdateTestRun(t, runCollection)
		runID2 := createUpdateTestRun(t, runCollection)

		cleanup, err := runCollection.LockRun(context.Background(), runID2)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()

		missingRunID := RunID{Value: "i-dont-exist"}
		errs := runCollection.UpdateRuns(ctx, []RunID{runID1, runID2, missingRunID}, RunUpdate{
			Operations: []RunUpdateOperation{SetRunGroup{Group: "batch-group"}},
		})

		require.Len(t, errs, 3)
		assert.NoError(t, errs[0])
		assert.Equal(t, message.New(message.EngineRunBusy).WithMetadata(map[string]string{"runID": runID2.Value}), errs[1])
		assert.Equal(t, message.New(message.EngineRunDoesNotExist).WithMetadata(map[string]string{"runID": missingRunID.Value}), errs[2])

		categorization, err := ReadRunCategorization(runCollection.getCategorizationPath(runID1))
		require.NoError(t, err)
		assert.Equal(t, "batch-group", categorization.Group)

		require.NoError(t, cleanup())
	})
}
