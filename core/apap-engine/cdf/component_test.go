// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package cdf

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"
)

func TestComponentReadAll(t *testing.T) {
	t.Run("reads an uncompressed component beneath a directory with glob characters", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "runs[ssd]{archive}")
		require.NoError(t, os.MkdirAll(dir, 0o700))
		path := filepath.Join(dir, "data.json")
		require.NoError(t, os.WriteFile(path, []byte(`{"value":1}`), 0o600))

		data, err := (Component{RelativePath: "data.json", AbsolutePath: path}).ReadAll()
		require.NoError(t, err)
		require.JSONEq(t, `{"value":1}`, string(data))
	})

	t.Run("decompresses a component beneath a directory with glob characters", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "runs[ssd]{archive}")
		require.NoError(t, os.MkdirAll(dir, 0o700))
		path := filepath.Join(dir, "data.json.zst")
		encoder, err := zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1))
		require.NoError(t, err)
		compressed := encoder.EncodeAll([]byte(`{"value":2}`), nil)
		encoder.Close()
		require.NoError(t, os.WriteFile(path, compressed, 0o600))

		data, err := (Component{RelativePath: "data.json", AbsolutePath: path, Compressed: true}).ReadAll()
		require.NoError(t, err)
		require.JSONEq(t, `{"value":2}`, string(data))
	})

	t.Run("reports corrupt compressed data", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "data.json.zst")
		require.NoError(t, os.WriteFile(path, []byte("not zstd"), 0o600))

		_, err := (Component{RelativePath: "data.json", AbsolutePath: path, Compressed: true}).ReadAll()
		require.Error(t, err)
	})

	t.Run("does not expand wildcard paths", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "a.csv"), []byte("data"), 0o600))
		_, err := (Component{RelativePath: "[ab].csv", AbsolutePath: filepath.Join(dir, "[ab].csv")}).ReadAll()
		require.ErrorIs(t, err, os.ErrNotExist)
	})
}

func TestComponentGlobPath(t *testing.T) {
	compressed := Component{AbsolutePath: "/run/source.csv.zst", Compressed: true}
	require.Equal(t, "/run/source.csv-*.csv.zst", compressed.GlobPath("-*.csv"))

	plain := Component{AbsolutePath: "/run/source.csv"}
	require.Equal(t, "/run/source.csv-*.csv", plain.GlobPath("-*.csv"))
}

func TestManifestEntryStoragePath(t *testing.T) {
	require.Equal(t, "output/data.json", (ManifestEntry{Path: "output/data.json"}).StoragePath())
	require.Equal(t, "output/data.json.zst", (ManifestEntry{Path: "output/data.json", Compressed: true}).StoragePath())
}
