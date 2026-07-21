// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/bmatcuk/doublestar"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
)

func TestExpandGlob_GlobBase_NoEffectWithNoGlob(t *testing.T) {
	got, err := RemapGlobbedPath(
		filepath.FromSlash("tool/0/dest.txt"),
		filepath.FromSlash("expanded/base/tool/0/src.txt"),
		filepath.FromSlash("expanded/base/tool/0/src.txt"),
	)
	require.NoError(t, err)
	assert.Equal(t, filepath.FromSlash("tool/0/dest.txt"), got)
}

func TestIsChildPath(t *testing.T) {
	base := filepath.FromSlash("/tmp/runs")

	testCases := []struct {
		name      string
		childPath string
		want      bool
	}{
		{
			name:      "direct child",
			childPath: filepath.Join(base, "abcdef123456"),
			want:      true,
		},
		{
			name:      "nested child",
			childPath: filepath.Join(base, "abcdef123456", "metadata.json"),
			want:      true,
		},
		{
			name:      "hidden file child",
			childPath: filepath.Join(base, ".myFile"),
			want:      true,
		},
		{
			name:      "filename begins with two dots",
			childPath: filepath.Join(base, "..abc"),
			want:      true,
		},
		{
			name:      "filename contains backslash after two dots",
			childPath: filepath.Join(base, `..\abc`),
			want:      runtime.GOOS != "windows",
		},
		{
			name:      "nested hidden file child",
			childPath: filepath.Join(base, "abcdef123456", ".metadata.json"),
			want:      true,
		},
		{
			name:      "base path itself",
			childPath: base,
			want:      false,
		},
		{
			name:      "parent path",
			childPath: filepath.Dir(base),
			want:      false,
		},
		{
			name:      "sibling with shared prefix",
			childPath: filepath.FromSlash("/tmp/runs-not-really/abcdef123456"),
			want:      false,
		},
		{
			name:      "escaped path",
			childPath: filepath.Clean(filepath.Join(base, "..", "other")),
			want:      false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, IsChildPath(base, tc.childPath))
		})
	}
}

func TestMatchesAny(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		patterns []string
		want     bool
		wantErr  error
	}{
		{
			name:     "returns false when there are no patterns",
			filePath: "/a/b/c.txt",
			patterns: []string{},
			want:     false,
		},
		{
			name:     "returns true when any pattern matches",
			filePath: "/a/b/c.txt",
			patterns: []string{"/x/**/*.txt", "/a/**/*.txt"},
			want:     true,
		},
		{
			name:     "returns true for an exact match",
			filePath: "/a/b/c.txt",
			patterns: []string{"/nope", "/a/b/c.txt"},
			want:     true,
		},
		{
			name:     "returns false when no pattern matches",
			filePath: "/a/b/c.txt",
			patterns: []string{"/a/*.txt", "/x/**/*.txt"},
			want:     false,
		},
		{
			name:     "cleans path and pattern before matching",
			filePath: "a/b/../c.txt",
			patterns: []string{"a//c.txt"},
			want:     true,
		},
		{
			name:     "normalises backslash separators before matching",
			filePath: `a\b\c.txt`,
			patterns: []string{"a/**/c.txt"},
			want:     true,
		},
		{
			name:     "backslash case 2",
			filePath: `C:\\a\b\c.txt`,
			patterns: []string{`C:\\a\**\*`},
			want:     true,
		},
		{
			name:     "returns doublestar error for invalid pattern",
			filePath: "/a/b/c.txt",
			patterns: []string{"{"},
			want:     false,
			wantErr:  doublestar.ErrBadPattern,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := MatchesAny(tc.filePath, tc.patterns)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tc.want, got)
		})
	}
}

func TestExpandGlob_GlobBase_RemapsMatchingWildcardTemplate(t *testing.T) {
	got, err := RemapGlobbedPath(
		filepath.FromSlash("tool/0/**/*"),
		filepath.FromSlash("expanded/base/tool/0/inner/myFile"),
		filepath.FromSlash("expanded/base/tool/0/**/*"),
	)
	require.NoError(t, err)
	assert.Equal(t, filepath.FromSlash("tool/0/inner/myFile"), got)
}

func TestExpandGlob_GlobBase_SameSuffixRemaps(t *testing.T) {
	got, err := RemapGlobbedPath(
		filepath.FromSlash("tool/output/deeper/**/*"),
		filepath.FromSlash("expanded/base/deeper/stuff.txt"),
		filepath.FromSlash("expanded/base/deeper/**/*"),
	)
	require.NoError(t, err)
	assert.Equal(t, filepath.FromSlash("tool/output/deeper/stuff.txt"), got)
}

func TestExpandGlob_RemoteNotUnderBase_Err(t *testing.T) {
	_, err := RemapGlobbedPath(
		filepath.FromSlash("tool/0/**/*"),
		filepath.FromSlash("elsewhere/tool/0/inner/myFile"),
		filepath.FromSlash("expanded/base/tool/0/**/*"),
	)
	expected := message.New(message.EnginePathRemapRemotePathPatternMismatch).WithMetadata(map[string]string{
		"remotePath": "elsewhere/tool/0/inner/myFile",
		"remoteBase": "expanded/base/tool/0/**/*",
	})
	require.ErrorIs(t, err, expected)
	require.NoError(t, message.ValidateMetadataPlaceholders(err))
}

func TestExpandGlob_AbsolutePath(t *testing.T) {
	if runtime.GOOS == "windows" {
		got, err := RemapGlobbedPath(
			filepath.FromSlash("c:/local/**/*"),
			filepath.FromSlash("/r/base/fixed/deeper/file.txt"),
			filepath.FromSlash("/r/base/fixed/**/*"),
		)
		require.NoError(t, err)
		assert.Equal(t, filepath.FromSlash("c:/local/deeper/file.txt"), got)
	} else {
		got, err := RemapGlobbedPath(
			filepath.FromSlash("/local/**/*"),
			filepath.FromSlash("/r/base/fixed/deeper/file.txt"),
			filepath.FromSlash("/r/base/fixed/**/*"),
		)
		require.NoError(t, err)
		assert.Equal(t, filepath.FromSlash("/local/deeper/file.txt"), got)
	}
}

// TestRemapGlobbedPath verifies that local and remoteBase share the same wildcard-containing suffix
// starting at the first wildcard-containing segment, and only their concrete prefixes may differ.
func TestRemapGlobbedPath(t *testing.T) {
	testCases := []struct {
		name       string
		local      string
		remote     string
		remoteBase string
		want       string
		wantErr    string
		wantMsg    message.MessageCode
		wantMeta   map[string]string
	}{
		{
			name:       "fails if any arg contains unsupported metachars",
			local:      "",
			remote:     "one/two/three",
			remoteBase: "one/two/?",
			wantErr:    "contains unsupported meta character",
		},
		{
			name:       "fails if any arg is a directory",
			local:      "a/*",
			remote:     "one/two/three/",
			remoteBase: "one/two/*",
			wantErr:    "is a directory",
		},
		{
			name:       "fails if remote is not a concrete path",
			local:      "a/*",
			remote:     "one/two/three/*",
			remoteBase: "one/two/*",
			wantErr:    "must be concrete",
		},
		{
			name:       "fails if remote and remote base are both concrete, but not the same",
			local:      "a/*",
			remote:     "one/two/three/four",
			remoteBase: "one/two/three",
			wantErr:    "not the same",
		},
		{
			name:       "fails if remote base is concrete, but local is globbed",
			local:      "a/*",
			remote:     "one/two/three",
			remoteBase: "one/two/three",
			wantErr:    "remote base is concrete, but local path",
		},
		{
			name:       "fails if remote base is globbed, but local has no wildcards",
			local:      "a/b",
			remote:     "one/two/three/four",
			remoteBase: "one/two/three/*",
			wantMsg:    message.EnginePathRemapWildcardSuffixMismatch,
			wantMeta: map[string]string{
				"localPath":  "a/b",
				"remoteBase": "one/two/three/*",
			},
		},
		{
			name:       "fails if local has extra wildcards",
			local:      "a/b/**/*",
			remote:     "one/two/three/four",
			remoteBase: "one/two/three/*",
			wantMsg:    message.EnginePathRemapWildcardSuffixMismatch,
			wantMeta: map[string]string{
				"localPath":  "a/b/**/*",
				"remoteBase": "one/two/three/*",
			},
		},
		{
			name:       "fails if wildcard sequence differs for mid-path wildcards",
			local:      "a/b/*",
			remote:     "one/two/three/four",
			remoteBase: "one/*/three/*",
			wantMsg:    message.EnginePathRemapWildcardSuffixMismatch,
			wantMeta: map[string]string{
				"localPath":  "a/b/*",
				"remoteBase": "one/*/three/*",
			},
		},
		{
			name:       "fails if remote doesn't match remoteBase (1)",
			local:      "a/b/*",
			remote:     "x/y",
			remoteBase: "one/two/*",
			wantMsg:    message.EnginePathRemapRemotePathPatternMismatch,
			wantMeta: map[string]string{
				"remotePath": "x/y",
				"remoteBase": "one/two/*",
			},
		},
		{
			name:       "fails if remote doesn't match remoteBase (2)",
			local:      "a/b/*",
			remote:     "one/two/three/four",
			remoteBase: "one/two/*",
			wantMsg:    message.EnginePathRemapRemotePathPatternMismatch,
			wantMeta: map[string]string{
				"remotePath": "one/two/three/four",
				"remoteBase": "one/two/*",
			},
		},
		{
			name:       "fails when single star would need to cross path segments",
			local:      "out/*/b",
			remote:     "a/u/v/b",
			remoteBase: "a/*/b",
			wantMsg:    message.EnginePathRemapRemotePathPatternMismatch,
			wantMeta: map[string]string{
				"remotePath": "a/u/v/b",
				"remoteBase": "a/*/b",
			},
		},
		{
			name:       "fails when wildcard capture contains parent traversal",
			local:      "out/*",
			remote:     "..",
			remoteBase: "*",
			wantMsg:    message.EnginePathRemapPathTraversal,
			wantMeta: map[string]string{
				"remappedPath":  "out/..",
				"pathComponent": "..",
			},
		},
		{
			name:       "fails when remote and remote base use different coordinate spaces",
			local:      "mirror/**/counter.parquet",
			remote:     "/tmp/out/counter.parquet",
			remoteBase: "**/counter.parquet",
			wantMsg:    message.EnginePathRemapCoordinateSpaceMismatch,
			wantMeta: map[string]string{
				"remotePath": "/tmp/out/counter.parquet",
				"remoteBase": "**/counter.parquet",
			},
		},
		{
			name:       "fails when double star capture contains parent traversal segment",
			local:      "out/**/counter.parquet",
			remote:     "../safe/counter.parquet",
			remoteBase: "**/counter.parquet",
			wantMsg:    message.EnginePathRemapPathTraversal,
			wantMeta: map[string]string{
				"remappedPath":  "out/../safe/counter.parquet",
				"pathComponent": "../safe/counter.parquet",
			},
		},
		{
			name:       "fails when relative local template resolves outside its base path",
			local:      "../mirror/**/*",
			remote:     "out/x/y",
			remoteBase: "out/**/*",
			wantMsg:    message.EnginePathRemapRelativeTemplateBaseEscape,
			wantMeta: map[string]string{
				"remappedPath":  "../mirror/x/y",
				"localTemplate": "../mirror/**/*",
			},
		},
		{
			name:       "remaps mirrored single star suffix",
			local:      "output/sources-capture-periodic_sampling*",
			remote:     "tmp/out/sources-capture-periodic_sampling-libc.so.6.csv",
			remoteBase: "tmp/out/sources-capture-periodic_sampling*",
			want:       "output/sources-capture-periodic_sampling-libc.so.6.csv",
		},
		{
			name:       "remaps mirrored doublestar suffix",
			local:      "capture.apc/**/*",
			remote:     "tmp/out/db/image-metadata/bash/functions.bin",
			remoteBase: "tmp/out/**/*",
			want:       "capture.apc/db/image-metadata/bash/functions.bin",
		},
		{
			name:       "remaps root-anchored wildcard suffix into relative local prefix",
			local:      "mirror/**/*",
			remote:     "/x/y",
			remoteBase: "/**/*",
			want:       "mirror/x/y",
		},
		{
			name:       "remaps mirrored structured suffix",
			local:      "output/parquet/timeline/series_id=*/bin_duration=*/counter.parquet",
			remote:     "tmp/out/report-new/apx/timeline/series_id=22/bin_duration=1000000000/counter.parquet",
			remoteBase: "tmp/out/report-new/apx/timeline/series_id=*/bin_duration=*/counter.parquet",
			want:       "output/parquet/timeline/series_id=22/bin_duration=1000000000/counter.parquet",
		},
		{
			name:       "remaps mirrored doublestar with zero dirs",
			local:      "mirror/**/target",
			remote:     "out/target",
			remoteBase: "out/**/target",
			want:       "mirror/target",
		},
		{
			name:       "fails when local wildcard suffix is concrete",
			local:      "output/result.csv",
			remote:     "tmp/out/result.csv",
			remoteBase: "tmp/out/*.csv",
			wantMsg:    message.EnginePathRemapWildcardSuffixMismatch,
			wantMeta: map[string]string{
				"localPath":  "output/result.csv",
				"remoteBase": "tmp/out/*.csv",
			},
		},
		{
			name:       "fails when local wildcard suffix text differs",
			local:      "output/bar-*.csv",
			remote:     "tmp/out/foo-a.csv",
			remoteBase: "tmp/out/foo-*.csv",
			wantMsg:    message.EnginePathRemapWildcardSuffixMismatch,
			wantMeta: map[string]string{
				"localPath":  "output/bar-*.csv",
				"remoteBase": "tmp/out/foo-*.csv",
			},
		},
		{
			name:       "fails when wildcard segment layout differs",
			local:      "capture.apc/*",
			remote:     "tmp/out/db/image-metadata/bash/functions.bin",
			remoteBase: "tmp/out/**/*",
			wantMsg:    message.EnginePathRemapWildcardSuffixMismatch,
			wantMeta: map[string]string{
				"localPath":  "capture.apc/*",
				"remoteBase": "tmp/out/**/*",
			},
		},
		{
			name:       "fails when local changes trailing literal after wildcard segment",
			local:      "output/a/*/c",
			remote:     "tmp/out/a/value/b",
			remoteBase: "tmp/out/a/*/b",
			wantMsg:    message.EnginePathRemapWildcardSuffixMismatch,
			wantMeta: map[string]string{
				"localPath":  "output/a/*/c",
				"remoteBase": "tmp/out/a/*/b",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := RemapGlobbedPath(filepath.FromSlash(tc.local), filepath.FromSlash(tc.remote), filepath.FromSlash(tc.remoteBase))
			if tc.wantMsg != "" {
				expected := message.New(tc.wantMsg).WithMetadata(tc.wantMeta)
				require.ErrorIs(t, err, expected)
				require.NoError(t, message.ValidateMetadataPlaceholders(err))
			} else if tc.wantErr != "" {
				require.Error(t, err, "expected error, got nil")
				require.ErrorContains(t, err, tc.wantErr)
			} else {
				require.NoError(t, err, "expected no error, got '%v'", err)
			}

			if tc.want != "" {
				assert.Equal(t, filepath.FromSlash(tc.want), got)
			}
		})
	}
}

func TestRemapGlobbedPath_CrossPlatformSuffix(t *testing.T) {
	var local, remote, remoteBase, want string
	if runtime.GOOS == "windows" {
		local = `C:\one\two\**\*`
		remote = `C:\one\two\three\four\five`
		remoteBase = `C:\one\two\**\*`
		want = `C:\one\two\three\four\five`
	} else {
		local = "/one/two/**/*"
		remote = "/one/two/three/four/five"
		remoteBase = "/one/two/**/*"
		want = "/one/two/three/four/five"
	}
	got, err := RemapGlobbedPath(local, remote, remoteBase)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestRemapGlobbedPath_MixedSeparatorSuffix(t *testing.T) {
	var local, remote, remoteBase, want string
	if runtime.GOOS == "windows" {
		local = `C:\mirror/**/*`
		remote = "mirror\\dir/sub\\file.txt"
		remoteBase = "mirror/**/*"
		want = filepath.FromSlash("C:/mirror/dir/sub/file.txt")
	} else {
		local = "/mirror/**/*"
		remote = "mirror\\dir/sub\\file.txt"
		remoteBase = "mirror/**/*"
		want = filepath.FromSlash("/mirror/dir/sub/file.txt")
	}
	got, err := RemapGlobbedPath(local, remote, remoteBase)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestValidateRemappedLocalPath(t *testing.T) {
	t.Run("fails when relative local template resolves to an absolute path", func(t *testing.T) {
		err := validateRemappedLocalPath("**/*", "/x/y", []string{"safe"})
		expected := message.New(message.EnginePathRemapRelativeTemplateAbsolutePath).WithMetadata(map[string]string{
			"remappedPath":  "/x/y",
			"localTemplate": "**/*",
		})
		require.ErrorIs(t, err, expected)
		require.NoError(t, message.ValidateMetadataPlaceholders(err))
	})
}

func TestIsAbsolutePath(t *testing.T) {
	t.Run("treats slash-rooted paths as absolute", func(t *testing.T) {
		require.True(t, isAbsolutePath("/x/y"))
	})

	t.Run("treats drive-rooted paths as absolute", func(t *testing.T) {
		require.True(t, isAbsolutePath("C:/x/y"))
	})
}

func TestValidateRemappedLocalPath_DriveAbsolute(t *testing.T) {
	err := validateRemappedLocalPath("**/*", "C:/x/y", []string{"safe"})
	expected := message.New(message.EnginePathRemapRelativeTemplateAbsolutePath).WithMetadata(map[string]string{
		"remappedPath":  "C:/x/y",
		"localTemplate": "**/*",
	})
	require.ErrorIs(t, err, expected)
	require.NoError(t, message.ValidateMetadataPlaceholders(err))
}
