// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

func TestRunCategorization(t *testing.T) {
	t.Run("component type uses categorization schema", func(t *testing.T) {
		assert.Equal(t, cdf.ComponentType{Name: "categorization", SchemaVersion: "1.0.0"}, CategorizationCT())
	})

	t.Run("writes empty categorization", func(t *testing.T) {
		target := filepath.Join(t.TempDir(), CategorizationFilename)

		require.NoError(t, WriteRunCategorization(target, nil))

		categorization, err := ReadRunCategorization(target)
		require.NoError(t, err)
		assert.Equal(t, "", categorization.Group)
		assert.Equal(t, []string{}, categorization.Tags)
	})

	t.Run("normalizes nil tags when writing", func(t *testing.T) {
		target := filepath.Join(t.TempDir(), CategorizationFilename)

		require.NoError(t, WriteRunCategorization(target, &RunCategorization{Group: "group-a"}))

		contents, err := os.ReadFile(target)
		require.NoError(t, err)
		assert.JSONEq(t, `{"group":"group-a","tags":[]}`, string(contents))
	})

	t.Run("missing file returns empty categorization", func(t *testing.T) {
		categorization, err := ReadRunCategorization(filepath.Join(t.TempDir(), CategorizationFilename))

		require.NoError(t, err)
		assert.Equal(t, RunCategorization{Tags: []string{}}, *categorization)
	})

	t.Run("invalid file returns error", func(t *testing.T) {
		target := filepath.Join(t.TempDir(), CategorizationFilename)
		require.NoError(t, os.WriteFile(target, []byte("{"), perms.LocalFilePerm))

		categorization, err := ReadRunCategorization(target)

		require.Error(t, err)
		assert.Equal(t, RunCategorization{}, *categorization)
	})
}

func TestNormalizeRunCategorizationFields(t *testing.T) {
	t.Run("normalizes group", func(t *testing.T) {
		group, err := normalizeRunGroup(" group-a ")

		require.NoError(t, err)
		assert.Equal(t, "group-a", group)
	})

	t.Run("rejects empty group", func(t *testing.T) {
		_, err := normalizeRunGroup(" ")

		assert.Equal(t, message.New(message.EngineRunInvalidGroup).WithMetadata(map[string]string{"group": `" "`}), err)
	})

	t.Run("normalizes tags", func(t *testing.T) {
		tags, err := normalizeRunTags([]string{" tag-a ", "tag-b"})

		require.NoError(t, err)
		assert.Equal(t, []string{"tag-a", "tag-b"}, tags)
	})

	t.Run("deduplicates tags after trimming", func(t *testing.T) {
		tags, err := normalizeRunTags([]string{" tag-a ", "tag-b", "tag-a"})

		require.NoError(t, err)
		assert.Equal(t, []string{"tag-a", "tag-b"}, tags)
	})

	t.Run("rejects empty tags", func(t *testing.T) {
		_, err := normalizeRunTags([]string{"tag-a", " "})

		expectedTags := util.DisplayErrorStringSlice([]string{"tag-a", " "})
		assert.Equal(t, message.New(message.EngineRunInvalidTags).WithMetadata(map[string]string{"tags": expectedTags}), err)
	})
}

func TestRunTagOperations(t *testing.T) {
	t.Run("adds tags without duplicating existing or new tags", func(t *testing.T) {
		tags := addRunTags([]string{"tag-a", "tag-b"}, []string{"tag-b", "tag-c", "tag-c"})

		assert.Equal(t, []string{"tag-a", "tag-b", "tag-c"}, tags)
	})
}
