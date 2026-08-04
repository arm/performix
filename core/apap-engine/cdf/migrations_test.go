// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package cdf

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf/semver"
)

// Test rewriteViaMigration directly:

func TestRewriteViaMigration_NoPrefix(t *testing.T) {
	// pure‐rename migration, but path doesn’t even start with "tool/"
	var mig PathMigration = &ToolNameMigration{
		From: "old",
		To:   "new",
		Ver:  semver.SemVer{Major: 0, Minor: 0, Patch: 0},
	}
	if out, ok := rewriteViaMigration("notool/new/0/foo", mig); ok {
		t.Fatalf("expected no rewrite but got %q", out)
	}
}

func TestRewriteViaMigration_Simple(t *testing.T) {
	// at any version, tool folder legacy → modern
	var mig PathMigration = &ToolNameMigration{
		From: "legacy",
		To:   "modern",
		Ver:  semver.SemVer{Major: 0, Minor: 0, Patch: 0},
	}
	in := "tool/modern/42/path/to/file"
	want := "tool/legacy/42/path/to/file"
	out, ok := rewriteViaMigration(in, mig)
	if !ok || out != want {
		t.Fatalf("rewriteViaMigration(%q) = %q, %v; want %q, true", in, out, ok, want)
	}
}

func TestRewriteViaMigration_WithSuffix(t *testing.T) {
	// at v1.0.0, bar’s layout changed from very/ → very/deep/
	var mig PathMigration = &ToolPathSuffixMigration{
		Type:      "suffixChange",
		From:      "bar",
		Ver:       semver.SemVer{Major: 1, Minor: 0, Patch: 0},
		OldSuffix: "very/",
		NewSuffix: "very/deep/",
	}

	in := "tool/bar/7/very/deep/file.txt"
	got, ok := rewriteViaMigration(in, mig)
	if !ok {
		t.Fatal("expected deep‐move rewrite to match")
	}

	want := "tool/bar/7/very/file.txt"
	if got != want {
		t.Fatalf("rewriteViaMigration(%q) = %q; want %q", in, got, want)
	}
}

// Test migratePath which invokes rewriteViaMigration under the hood.

func TestMigratePath_PicksFirstValid(t *testing.T) {
	model := &OnDiskModel{
		basePath: "",
		manifest: &Manifest{Entries: nil},
		migrations: []PathMigration{
			&ToolNameMigration{
				Type: "renameTool",
				From: "old",
				To:   "neo",
				Ver:  semver.SemVer{Major: 1, Minor: 0, Patch: 0},
			},
		},
	}

	out, ok, _ := model.migratePath("tool/neo/1/a")
	if !ok || out != "tool/old/1/a" {
		t.Fatalf("migratePath = (%q, %v), want %q, true", out, ok, "tool/old/1/a")
	}
}

func TestMigratePath_NoneFound(t *testing.T) {
	model := &OnDiskModel{
		basePath: "",
		manifest: &Manifest{Entries: nil},
		migrations: []PathMigration{
			&ToolNameMigration{
				Type: "renameTool",
				From: "x",
				To:   "y",
				Ver:  semver.SemVer{Major: 0, Minor: 0, Patch: 1},
			},
		},
	}

	if out, ok, _ := model.migratePath("tool/w/0/z"); ok {
		t.Fatalf("expected no migration, but got %q", out)
	}
}

func TestMigratePath_SkipsIdentity(t *testing.T) {
	model := &OnDiskModel{
		manifest: &Manifest{Entries: nil},
		migrations: []PathMigration{
			&ToolNameMigration{
				Type: "renameTool",
				From: "legacy",
				To:   "legacy",
				Ver:  semver.SemVer{Major: 1, Minor: 0, Patch: 0},
			},
		},
	}

	out, ok, _ := model.migratePath("tool/legacy/0/file")
	if ok {
		t.Fatalf("expected identity migration to be skipped, but got %q", out)
	}
}

func TestRewriteViaMigration_DeepMove(t *testing.T) {
	// suffix‐only migration: at v1.0.0, output/ → foo/bar/baz/output/
	var mig PathMigration = &ToolPathSuffixMigration{
		Type:      "suffixChange",
		From:      "old",
		Ver:       semver.SemVer{Major: 1, Minor: 0, Patch: 0},
		OldSuffix: "output/",
		NewSuffix: "foo/bar/baz/output/",
	}

	in := "tool/old/0/foo/bar/baz/output/file.dat"
	out, ok := rewriteViaMigration(in, mig)
	if !ok {
		t.Fatal("expected deep-move rewrite to match")
	}

	want := "tool/old/0/output/file.dat"
	if out != want {
		t.Fatalf("got %q; want %q", out, want)
	}
}

func TestInjectLegacyToolOutputs_Basic(t *testing.T) {
	// Given a model whose manifest mentions two tools in different entries,
	// and with an empty ToolsUsed list,
	// when we call InjectLegacyToolOutputs("1.2.3"),
	// then ToolsUsed should contain exactly those two tools,
	// each with Version "1.2.3" and Invocation 0.
	entries := []ManifestEntry{
		{Path: "tool/foo/0/a.txt", ComponentType: ComponentType{Name: "A", SchemaVersion: "1.0"}},
		{Path: "tool/bar/xyz.txt", ComponentType: ComponentType{Name: "B", SchemaVersion: "2.0"}},
		{Path: "other/path.txt", ComponentType: ComponentType{Name: "O", SchemaVersion: "1.0"}},
		{Path: "tool/foo/0/b.txt", ComponentType: ComponentType{Name: "A2", SchemaVersion: "1.0"}}, // duplicate foo
	}
	m := NewOnDiskModel("/runs", &Manifest{Entries: entries, ToolsUsed: nil}, Metadata{})

	m.InjectLegacyToolOutputs("1.2.3")

	// Collect names & versions from ToolsUsed
	seen := map[string]ToolUsed{}
	for _, tu := range m.Manifest().ToolsUsed {
		seen[tu.Tool] = tu
	}

	if len(seen) != 2 {
		t.Fatalf("expected exactly 2 tools injected, got %d: %+v", len(seen), seen)
	}

	foo, ok := seen["foo"]
	if !ok {
		t.Fatal("expected foo in ToolsUsed")
	}
	if foo.Version != "1.2.3" || foo.Invocation != 0 {
		t.Errorf("foo has %+v; want Version=1.2.3, Invocation=0", foo)
	}

	bar, ok := seen["bar"]
	if !ok {
		t.Fatal("expected bar in ToolsUsed")
	}
	if bar.Version != "1.2.3" || bar.Invocation != 0 {
		t.Errorf("bar has %+v; want Version=1.2.3, Invocation=0", bar)
	}
}

func TestInjectLegacyToolOutputs_DoesNotOverrideExisting(t *testing.T) {
	// Given a model whose manifest has one pre-existing ToolsUsed entry,
	// and entries for that tool plus a new one,
	// when we call InjectLegacyToolOutputs,
	// then we should only append the new tool, not overwrite the existing.
	entries := []ManifestEntry{
		{Path: "tool/existing/0/x.txt", ComponentType: ComponentType{Name: "X", SchemaVersion: "1.0"}},
		{Path: "tool/newtool/y.txt", ComponentType: ComponentType{Name: "Y", SchemaVersion: "1.0"}},
	}
	// Pre-populate ToolsUsed with "existing" at version 9.9.9
	pre := []ToolUsed{{Tool: "existing", Version: "9.9.9", Invocation: 0}}
	m := NewOnDiskModel("/runs", &Manifest{Entries: entries, ToolsUsed: pre}, Metadata{})

	m.InjectLegacyToolOutputs("1.0.0")

	// We expect still two entries: the pre-existing one unchanged, plus newtool@1.0.0
	if len(m.Manifest().ToolsUsed) != 2 {
		t.Fatalf("expected 2 ToolsUsed, got %d: %+v", len(m.Manifest().ToolsUsed), m.Manifest().ToolsUsed)
	}

	// Check existing entry was not overridden
	for _, tu := range m.Manifest().ToolsUsed {
		if tu.Tool == "existing" {
			if tu.Version != "9.9.9" || tu.Invocation != 0 {
				t.Errorf("existing was modified: %+v", tu)
			}
		}
		if tu.Tool == "newtool" {
			if tu.Version != "1.0.0" || tu.Invocation != 0 {
				t.Errorf("newtool has %+v; want Version=1.0.0, Invocation=0", tu)
			}
		}
	}
}

func TestMigratePath_CycleError(t *testing.T) {
	// Two migrations that would cycle:
	//   • v1: A → B
	//   • v2: B → A
	v1, _ := semver.ParseSemVer("1.0.0")
	v2, _ := semver.ParseSemVer("2.0.0")
	model := &OnDiskModel{
		migrations: []PathMigration{
			&ToolNameMigration{Type: "renameTool", From: "A", To: "B", Ver: v1},
			&ToolNameMigration{Type: "renameTool", From: "B", To: "A", Ver: v2},
		},
	}

	// Start at "tool/A/0/foo":
	// - v2 matches To=="A" and rewrites to "tool/B/0/foo"
	// - v1 matches To=="B" and would rewrite back to "tool/A/0/foo" ..... cycle, error!
	out, ok, err := model.migratePath("tool/A/0/foo")

	if err == nil {
		t.Fatal("expected cycle error, got nil")
	}
	if ok {
		t.Errorf("expected ok=false when cycle detected, got ok=true and out=%q", out)
	}
	if !strings.Contains(err.Error(), "cycle rewriting") {
		t.Errorf("error did not mention cycle; got %q", err)
	}
}

func TestMigrateToolName(t *testing.T) {
	tests := []struct {
		name             string
		toolName         string
		migrations       []PathMigration
		expectedToolName string
		expectedMigrated bool
		expectedError    string
	}{
		{
			name:     "migrates a matching tool name",
			toolName: "new",
			migrations: []PathMigration{
				&ToolNameMigration{From: "old", To: "new"},
			},
			expectedToolName: "old",
			expectedMigrated: true,
		},
		{
			name:     "chains migrations newest first",
			toolName: "new",
			migrations: []PathMigration{
				&ToolNameMigration{
					From: "old",
					To:   "middle",
					Ver:  semver.SemVer{Major: 1},
				},
				&ToolNameMigration{
					From: "middle",
					To:   "new",
					Ver:  semver.SemVer{Major: 2},
				},
			},
			expectedToolName: "old",
			expectedMigrated: true,
		},
		{
			name:     "leaves an unmatched tool name unchanged",
			toolName: "other",
			migrations: []PathMigration{
				&ToolNameMigration{From: "old", To: "new"},
			},
		},
		{
			name:     "ignores non-name migrations",
			toolName: "a",
			migrations: []PathMigration{
				&ToolInvocationMigration{From: "a"},
				&ToolPathSuffixMigration{From: "a", OldSuffix: "old", NewSuffix: "new"},
			},
		},
		{
			name:     "skips identity migrations",
			toolName: "same",
			migrations: []PathMigration{
				&ToolNameMigration{From: "same", To: "same"},
			},
		},
		{
			name:     "rejects migration cycles",
			toolName: "A",
			migrations: []PathMigration{
				&ToolNameMigration{
					From: "A",
					To:   "B",
					Ver:  semver.SemVer{Major: 1},
				},
				&ToolNameMigration{
					From: "B",
					To:   "A",
					Ver:  semver.SemVer{Major: 2},
				},
			},
			expectedError: `MigrateToolName: detected cycle rewriting "B" to "A"; stopping`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toolName, migrated, err := MigrateToolName(tt.toolName, tt.migrations)

			if tt.expectedError != "" {
				require.EqualError(t, err, tt.expectedError)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tt.expectedToolName, toolName)
			require.Equal(t, tt.expectedMigrated, migrated)
		})
	}
}
