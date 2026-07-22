// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
)

func TestLoadContent_EmptySelection(t *testing.T) {
	_, _, err := LoadContent(context.Background(), &fakeRunLoader{}, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty content selection")
}

func TestLoadContent_DuplicateRuns(t *testing.T) {
	model := cdf.NewOnDiskModel("/base", &cdf.Manifest{}, cdf.Metadata{})
	loader := &fakeRunLoader{model: model}

	cm, _, err := LoadContent(context.Background(), loader, []run.RunID{{Value: "x"}, {Value: "x"}}, nil)
	require.NoError(t, err)
	assert.Len(t, cm.Entries, 2)
	assert.Equal(t, "x", cm.Entries[1].ID.Value)
}

func TestLoadContent_SetsExternalAccessRootsToBaseRunRoot(t *testing.T) {
	runRoot := t.TempDir()
	model := cdf.NewOnDiskModel(runRoot, &cdf.Manifest{}, cdf.Metadata{})
	loader := &fakeRunLoader{model: model}

	cm, _, err := LoadContent(context.Background(), loader, []run.RunID{{Value: "x"}}, nil)
	require.NoError(t, err)
	require.Len(t, cm.Entries, 1)
	require.Equal(t, []string{runRoot}, cm.Entries[0].ExternalAccessRoots)
}

func TestLoadContent_OverlayContentUsesBaseRunRootForExternalAccess(t *testing.T) {
	runRoot := t.TempDir()
	baseModel := cdf.NewOnDiskModel(runRoot, &cdf.Manifest{}, cdf.Metadata{})
	loader := &fakeRunLoader{model: baseModel}
	cm, _, err := LoadContent(context.Background(), loader, []run.RunID{{Value: "x"}}, nil)
	require.NoError(t, err)
	require.Len(t, cm.Entries, 1)
	require.Equal(t, []string{runRoot}, cm.Entries[0].ExternalAccessRoots)
	require.NotEqual(t, runRoot, cm.Entries[0].Model.BasePath())
	require.True(t, filepath.IsAbs(cm.Entries[0].Model.BasePath()))
	require.True(t, strings.HasPrefix(cm.Entries[0].Model.BasePath(), runRoot))
}

func TestLoadContent_NameChangeOnlyMigrationApplied(t *testing.T) {
	// Migration old to new at v1.1.0, so any runVersion ≥1.1.0 should apply
	mig := tool.Migration{Type: "renameTool", From: "old", To: "new", Version: "1.1.0"}
	factory := &fakeToolFactory{migrations: []tool.Migration{mig}}
	reg := tool.NewToolRegistry()
	reg.RegisterTool(factory)
	pkgMgr := &FakePkgMgr{Registry: reg, Err: nil}

	entries := []cdf.ManifestEntry{
		{Path: "tool/old/0/foo.txt", ComponentType: cdf.ComponentType{Name: "foo", SchemaVersion: "1.0"}},
	}
	toolsUsed := []cdf.ToolUsed{
		{Tool: "old", Version: "1.0.0", Invocation: 0}, // runVersion == migrationVersion
	}
	model := cdf.NewOnDiskModel("/runs", &cdf.Manifest{Entries: entries, ToolsUsed: toolsUsed}, cdf.Metadata{})
	loader := &fakeRunLoader{model: model}

	cm, _, err := LoadContent(context.Background(), loader, []run.RunID{{Value: "r1"}}, pkgMgr)
	require.NoError(t, err)
	mdl := cm.Entries[0].Model

	comp, err := mdl.ResolveComponent("tool/new/0/foo.txt")
	require.NoError(t, err)
	assert.Equal(t, "tool/old/0/foo.txt", comp.RelativePath)
}

func TestLoadContent_NameChangeOnlyMigrationApplied_2(t *testing.T) {
	// Pure rename foo to bar at v2.0.0; should apply for any runVersion
	mig := tool.Migration{Type: "renameTool", From: "foo", To: "bar", Version: "2.0.0"}
	factory := &fakeToolFactory{migrations: []tool.Migration{mig}}
	reg := tool.NewToolRegistry()
	reg.RegisterTool(factory)
	pkgMgr := &FakePkgMgr{Registry: reg, Err: nil}

	entries := []cdf.ManifestEntry{
		{Path: "tool/foo/0/a.txt", ComponentType: cdf.ComponentType{Name: "A", SchemaVersion: "1.0"}},
	}
	model := cdf.NewOnDiskModel("/runs", &cdf.Manifest{
		Entries:   entries,
		ToolsUsed: []cdf.ToolUsed{{Tool: "foo", Version: "1.5.0", Invocation: 0}}, // 1.5.0 < 2.0.0
	}, cdf.Metadata{})
	loader := &fakeRunLoader{model: model}

	cm, _, err := LoadContent(context.Background(), loader, []run.RunID{{Value: "r"}}, pkgMgr)
	require.NoError(t, err)
	m := cm.Entries[0].Model

	comp, err := m.ResolveComponent("tool/bar/0/a.txt")
	require.NoError(t, err)
	assert.Equal(t, "tool/foo/0/a.txt", comp.RelativePath)
}

func TestLoadContent_SkipMigrationsOnRegistryError(t *testing.T) {
	mig := tool.Migration{Type: "renameTool", From: "x", To: "y", Version: "1.1.0"}
	factory := &fakeToolFactory{migrations: []tool.Migration{mig}}
	reg := tool.NewToolRegistry()
	reg.RegisterTool(factory)
	pkgMgr := &FakePkgMgr{Registry: reg, Err: errors.New("no registry")}

	entries := []cdf.ManifestEntry{
		{Path: "tool/x/0/f.txt", ComponentType: cdf.ComponentType{Name: "F", SchemaVersion: "1.0"}},
	}
	model := cdf.NewOnDiskModel("/runs", &cdf.Manifest{
		Entries:   entries,
		ToolsUsed: []cdf.ToolUsed{{Tool: "x", Version: "1.0.0", Invocation: 0}},
	}, cdf.Metadata{})
	loader := &fakeRunLoader{model: model}

	cm, _, err := LoadContent(context.Background(), loader, []run.RunID{{Value: "r"}}, pkgMgr)
	require.NoError(t, err)
	m := cm.Entries[0].Model

	_, err = m.ResolveComponent("tool/y/0/f.txt")
	require.Error(t, err)
	assert.EqualError(t, err, "component not found: \"tool/y/0/f.txt\"")
}

func TestLoadContent_LegacySuffixMigration(t *testing.T) {
	// Legacy layout: data/ to root at v1.1.0
	mig := tool.Migration{Type: "suffixRewrite", From: "bar", Version: "1.1.0", OldSuffix: "data/", NewSuffix: ""}
	factory := &fakeToolFactory{migrations: []tool.Migration{mig}}
	reg := tool.NewToolRegistry()
	reg.RegisterTool(factory)
	pkgMgr := &FakePkgMgr{Registry: reg, Err: nil}

	entries := []cdf.ManifestEntry{
		{Path: "tool/bar/0/data/z.txt", ComponentType: cdf.ComponentType{Name: "Z", SchemaVersion: "1.0"}},
	}
	model := cdf.NewOnDiskModel("/runs", &cdf.Manifest{
		Entries:   entries,
		ToolsUsed: []cdf.ToolUsed{{Tool: "bar", Version: "1.0.0", Invocation: 0}}, // == migration version
	}, cdf.Metadata{})
	loader := &fakeRunLoader{model: model}

	cm, _, err := LoadContent(context.Background(), loader, []run.RunID{{Value: "r"}}, pkgMgr)
	require.NoError(t, err)
	m := cm.Entries[0].Model

	comp, err := m.ResolveComponent("tool/bar/0/z.txt")
	require.NoError(t, err)
	assert.Equal(t, "tool/bar/0/data/z.txt", comp.RelativePath)
}

func TestLoadContent_SuffixMigration(t *testing.T) {
	// Layout moved at v1.1.0: old=output/, new=foo/bar/output/
	mig := tool.Migration{
		Type:      "suffixRewrite",
		From:      "old",
		Version:   "1.1.0",
		OldSuffix: "output/",
		NewSuffix: "foo/bar/output/",
	}
	factory := &fakeToolFactory{migrations: []tool.Migration{mig}}
	reg := tool.NewToolRegistry()
	reg.RegisterTool(factory)
	pkgMgr := &FakePkgMgr{Registry: reg, Err: nil}

	entries := []cdf.ManifestEntry{
		{Path: "tool/old/0/output/file.dat", ComponentType: cdf.ComponentType{Name: "foo", SchemaVersion: "1.0"}},
	}
	model := cdf.NewOnDiskModel("/runs", &cdf.Manifest{
		Entries:   entries,
		ToolsUsed: []cdf.ToolUsed{{Tool: "old", Version: "1.0.0", Invocation: 0}}, // == migration version
	}, cdf.Metadata{})
	loader := &fakeRunLoader{model: model}

	cm, _, err := LoadContent(context.Background(), loader, []run.RunID{{Value: "r"}}, pkgMgr)
	require.NoError(t, err)
	m := cm.Entries[0].Model

	comp, err := m.ResolveComponent("tool/old/0/foo/bar/output/file.dat")
	require.NoError(t, err)
	assert.Equal(t, "tool/old/0/output/file.dat", comp.RelativePath)
}

func TestLoadContent_MultipleToolsUsed(t *testing.T) {
	// Suppose tool "foo" renamed to "fuu" at v2.0.0
	// and tool "bar" renamed to "baz" at v2.0.0
	m1 := tool.Migration{Type: "renameTool", From: "foo", To: "fuu", Version: "2.0.0"}
	m2 := tool.Migration{Type: "renameTool", From: "bar", To: "baz", Version: "2.0.0"}
	factory := &fakeToolFactory{migrations: []tool.Migration{m1, m2}}
	reg := tool.NewToolRegistry()
	reg.RegisterTool(factory)
	pkgMgr := &FakePkgMgr{Registry: reg, Err: nil}

	// Our manifest has two component entries, one under each original tool
	entries := []cdf.ManifestEntry{
		{Path: "tool/foo/0/x.txt", ComponentType: cdf.ComponentType{Name: "X", SchemaVersion: "1.0"}},
		{Path: "tool/bar/0/y.txt", ComponentType: cdf.ComponentType{Name: "Y", SchemaVersion: "1.0"}},
	}
	toolsUsed := []cdf.ToolUsed{
		{Tool: "foo", Version: "1.0.0", Invocation: 0}, // before foo to fuu
		{Tool: "bar", Version: "1.0.0", Invocation: 0}, // before bar to baz
	}

	model := cdf.NewOnDiskModel("/runs", &cdf.Manifest{Entries: entries, ToolsUsed: toolsUsed}, cdf.Metadata{})
	loader := &fakeRunLoader{model: model}

	cm, _, err := LoadContent(context.Background(), loader, []run.RunID{{Value: "r"}}, pkgMgr)
	require.NoError(t, err)
	m := cm.Entries[0].Model

	// Resolving under fuu should map back to foo
	cf, err := m.ResolveComponent("tool/fuu/0/x.txt")
	require.NoError(t, err)
	assert.Equal(t, "tool/foo/0/x.txt", cf.RelativePath)

	// Resolving under baz should map back to bar
	cb, err := m.ResolveComponent("tool/baz/0/y.txt")
	require.NoError(t, err)
	assert.Equal(t, "tool/bar/0/y.txt", cb.RelativePath)
}

func TestLoadContent_LayoutAndCatchAll(t *testing.T) {
	// At v2.0.0 we moved everything under "output/" into "foo/output/".
	// We expect both:
	//   • deep‐moved files to be rewritten via the suffix rule, and
	//   • non‐output files (e.g. state.xml) to still resolve via the catch-all.
	mig := tool.Migration{
		Type:      "suffixRewrite",
		From:      "mytool",
		Version:   "2.0.0",
		OldSuffix: "output/",
		NewSuffix: "foo/output/",
	}
	factory := &fakeToolFactory{migrations: []tool.Migration{mig}}
	reg := tool.NewToolRegistry()
	reg.RegisterTool(factory)
	pkgMgr := &FakePkgMgr{Registry: reg, Err: nil}

	entries := []cdf.ManifestEntry{
		// original run had symbols.json under output/, plus a state.xml at root:
		{Path: "tool/mytool/0/output/symbols.json", ComponentType: cdf.ComponentType{Name: "symbols", SchemaVersion: "1.0"}},
		{Path: "tool/mytool/0/state.xml", ComponentType: cdf.ComponentType{Name: "state", SchemaVersion: "1.0"}},
	}
	model := cdf.NewOnDiskModel("/runs", &cdf.Manifest{
		Entries:   entries,
		ToolsUsed: []cdf.ToolUsed{{Tool: "mytool", Version: "1.0.0", Invocation: 0}},
	}, cdf.Metadata{})
	loader := &fakeRunLoader{model: model}

	cm, _, err := LoadContent(context.Background(), loader, []run.RunID{{Value: "r"}}, pkgMgr)
	require.NoError(t, err)
	m := cm.Entries[0].Model

	// 1) Deep‐moved path should rewrite back to "output/"
	deepPath := "tool/mytool/0/foo/output/symbols.json"
	comp, err := m.ResolveComponent(deepPath)
	require.NoError(t, err)
	assert.Equal(t, "tool/mytool/0/output/symbols.json", comp.RelativePath)

	// 2) A non‐migrated file at the root should still resolve via the catch-all
	rootPath := "tool/mytool/0/state.xml"
	comp2, err := m.ResolveComponent(rootPath)
	require.NoError(t, err)
	assert.Equal(t, "tool/mytool/0/state.xml", comp2.RelativePath)
}

func TestLoadContent_StreamlineCLI_RenameThenLayout(t *testing.T) {
	// (a) at v2.0.0 we renamed streamline-cli to neoprof
	// (b) at v3.0.0 we moved everything under output/ to deep/output/
	rename := tool.Migration{
		Type:    "renameTool",
		From:    "streamline-cli",
		To:      "neoprof",
		Version: "2.0.0",
	}
	layout := tool.Migration{
		Type:      "suffixRewrite",
		From:      "neoprof", // note: suffix migrations always refer to the *current* name
		Version:   "3.0.0",
		OldSuffix: "output/",
		NewSuffix: "deep/output/",
	}
	factory := &fakeToolFactory{migrations: []tool.Migration{rename, layout}}
	reg := tool.NewToolRegistry()
	reg.RegisterTool(factory)
	pkgMgr := &FakePkgMgr{Registry: reg, Err: nil}

	// Our manifest has exactly the *original* layout:
	entries := []cdf.ManifestEntry{
		{Path: "tool/streamline-cli/0/output/foo.txt", ComponentType: cdf.ComponentType{Name: "X", SchemaVersion: "1.0"}},
	}
	// And it records that it was written by streamline-cli@1.0.0:
	model := cdf.NewOnDiskModel("/runs", &cdf.Manifest{
		Entries:   entries,
		ToolsUsed: []cdf.ToolUsed{{Tool: "streamline-cli", Version: "1.0.0", Invocation: 0}},
	}, cdf.Metadata{})
	loader := &fakeRunLoader{model: model}

	cm, _, err := LoadContent(context.Background(), loader, []run.RunID{{Value: "r"}}, pkgMgr)
	require.NoError(t, err)
	m := cm.Entries[0].Model

	// On‐disk today the renderer will look under neoprof/deep/output/foo.txt,
	// so ResolveComponent should chain:
	//   neoprof to streamline-cli  (rename @2.0.0)
	//   deep/output/ to output/     (layout  @3.0.0)
	comp, err := m.ResolveComponent("tool/neoprof/0/deep/output/foo.txt")
	require.NoError(t, err)
	assert.Equal(t, "tool/streamline-cli/0/output/foo.txt", comp.RelativePath)
}

func TestLoadContent_MoveRootFilesToExtras(t *testing.T) {
	// v2.1.0: shift every file at tool-root into extras/
	mig := tool.Migration{
		Type:      "suffixRewrite",
		From:      "streamline-cli", // no rename, just layout change
		Version:   "2.1.0",
		OldSuffix: "",        // files used to live directly under inv/
		NewSuffix: "extras/", // now they live under extras/
	}
	factory := &fakeToolFactory{migrations: []tool.Migration{mig}}
	reg := tool.NewToolRegistry()
	reg.RegisterTool(factory)
	pkgMgr := &FakePkgMgr{Registry: reg, Err: nil}

	// Our old manifest: one moved file, plus one untouched subtree under output/
	entries := []cdf.ManifestEntry{
		{Path: "tool/streamline-cli/0/file1.txt", ComponentType: cdf.ComponentType{Name: "F1", SchemaVersion: "1.0"}},
		{Path: "tool/streamline-cli/0/output/foo.txt", ComponentType: cdf.ComponentType{Name: "OUT", SchemaVersion: "1.0"}},
	}
	model := cdf.NewOnDiskModel("/runs", &cdf.Manifest{
		Entries:   entries,
		ToolsUsed: []cdf.ToolUsed{{Tool: "streamline-cli", Version: "1.0.0", Invocation: 0}},
	}, cdf.Metadata{})
	loader := &fakeRunLoader{model: model}

	// Load, applying our single suffix migration
	cm, _, err := LoadContent(context.Background(), loader, []run.RunID{{Value: "r"}}, pkgMgr)
	require.NoError(t, err)
	m := cm.Entries[0].Model

	// 1) New on-disk path for file1.txt should be under extras/, remapped back:
	c1, err := m.ResolveComponent("tool/streamline-cli/0/extras/file1.txt")
	require.NoError(t, err)
	assert.Equal(t, "tool/streamline-cli/0/file1.txt", c1.RelativePath)

	// 2) Paths under output/ should be unaffected by this migration:
	c2, err := m.ResolveComponent("tool/streamline-cli/0/output/foo.txt")
	require.NoError(t, err)
	assert.Equal(t, "tool/streamline-cli/0/output/foo.txt", c2.RelativePath)
}

func TestLoadContent_RenameThenMoveRootFiles_WithToolsUsed(t *testing.T) {
	// (a) at v2.0.0 we renamed streamline-cli to neoprof
	// (b) at v3.0.0 we moved every root‐file into extras/
	rename := tool.Migration{
		Type:    "renameTool",
		From:    "streamline-cli",
		To:      "neoprof",
		Version: "2.0.0",
	}
	layout := tool.Migration{
		Type:      "suffixRewrite",
		From:      "neoprof", // suffix migrations always refer to the current name
		Version:   "3.0.0",
		OldSuffix: "",        // old files lived at the root of the invocation folder
		NewSuffix: "extras/", // now live under extras/
	}

	factory := &fakeToolFactory{migrations: []tool.Migration{rename, layout}}
	reg := tool.NewToolRegistry()
	reg.RegisterTool(factory)
	pkgMgr := &FakePkgMgr{Registry: reg, Err: nil}

	// Our manifest still contains the *original* paths
	entries := []cdf.ManifestEntry{
		{Path: "tool/streamline-cli/0/file1.txt", ComponentType: cdf.ComponentType{Name: "F1", SchemaVersion: "1.0"}},
	}
	model := cdf.NewOnDiskModel("/runs", &cdf.Manifest{
		Entries:   entries,
		ToolsUsed: []cdf.ToolUsed{{Tool: "streamline-cli", Version: "1.0.0", Invocation: 0}},
	}, cdf.Metadata{})
	loader := &fakeRunLoader{model: model}

	cm, _, err := LoadContent(context.Background(), loader, []run.RunID{{Value: "r"}}, pkgMgr)
	require.NoError(t, err)
	m := cm.Entries[0].Model

	// On disk today the renderer looks under neoprof/extras/file1.txt
	comp, err := m.ResolveComponent("tool/neoprof/0/extras/file1.txt")
	require.NoError(t, err)
	assert.Equal(t,
		"tool/streamline-cli/0/file1.txt",
		comp.RelativePath,
	)
}

func TestLoadContent_RenameThenMoveRootFiles_Legacy(t *testing.T) {
	// Same migrations as above
	rename := tool.Migration{
		Type:    "renameTool",
		From:    "streamline-cli",
		To:      "neoprof",
		Version: "2.0.0",
	}
	layout := tool.Migration{
		Type:      "suffixRewrite",
		From:      "neoprof",
		Version:   "3.0.0",
		OldSuffix: "",
		NewSuffix: "extras/",
	}

	factory := &fakeToolFactory{migrations: []tool.Migration{rename, layout}}
	reg := tool.NewToolRegistry()
	reg.RegisterTool(factory)
	pkgMgr := &FakePkgMgr{Registry: reg, Err: nil}

	// Legacy run: no ToolsUsed at all, but manifest entries still in the old layout
	entries := []cdf.ManifestEntry{
		{Path: "tool/streamline-cli/0/file1.txt", ComponentType: cdf.ComponentType{Name: "F1", SchemaVersion: "1.0"}},
	}
	model := cdf.NewOnDiskModel("/runs", &cdf.Manifest{
		Entries:   entries,
		ToolsUsed: nil,
	}, cdf.Metadata{})
	loader := &fakeRunLoader{model: model}

	cm, _, err := LoadContent(context.Background(), loader, []run.RunID{{Value: "legacy"}}, pkgMgr)
	require.NoError(t, err)
	m := cm.Entries[0].Model

	// Even without ToolsUsed, we inject streamline-cli@1.0.0, then apply
	// rename+layout, so neoprof/extras/file1.txt still maps back correctly:
	comp, err := m.ResolveComponent("tool/neoprof/0/extras/file1.txt")
	require.NoError(t, err)
	assert.Equal(t,
		"tool/streamline-cli/0/file1.txt",
		comp.RelativePath,
	)
}

// 1) Rename then layout, with recorded ToolsUsed
func TestLoadContent_RenameThenLayout_WithToolsUsed(t *testing.T) {
	rename := tool.Migration{Type: "renameTool", From: "streamline-cli", To: "bar", Version: "2.0.0"}
	layout := tool.Migration{
		Type:      "suffixRewrite",
		From:      "bar",
		Version:   "3.0.0",
		OldSuffix: "config/",
		NewSuffix: "cfg/",
	}
	factory := &fakeToolFactory{migrations: []tool.Migration{rename, layout}}
	reg := tool.NewToolRegistry()
	reg.RegisterTool(factory)
	pkgMgr := &FakePkgMgr{Registry: reg, Err: nil}

	entries := []cdf.ManifestEntry{
		{Path: "tool/streamline-cli/0/config/settings.json", ComponentType: cdf.ComponentType{Name: "S", SchemaVersion: "1.0"}},
	}
	model := cdf.NewOnDiskModel("/runs", &cdf.Manifest{
		Entries:   entries,
		ToolsUsed: []cdf.ToolUsed{{Tool: "streamline-cli", Version: "1.0.0", Invocation: 0}},
	}, cdf.Metadata{})
	loader := &fakeRunLoader{model: model}

	cm, _, err := LoadContent(context.Background(), loader, []run.RunID{{Value: "r"}}, pkgMgr)
	require.NoError(t, err)
	m := cm.Entries[0].Model

	// Today we’d look in tool/bar/0/cfg/settings.json
	comp, err := m.ResolveComponent("tool/bar/0/cfg/settings.json")
	require.NoError(t, err)
	assert.Equal(t,
		"tool/streamline-cli/0/config/settings.json",
		comp.RelativePath,
	)
}

// 2) Rename then layout, legacy (no ToolsUsed)
func TestLoadContent_RenameThenLayout_Legacy(t *testing.T) {
	rename := tool.Migration{Type: "renameTool", From: "streamline-cli", To: "bar", Version: "2.0.0"}
	layout := tool.Migration{
		Type:      "suffixRewrite",
		From:      "bar",
		Version:   "3.0.0",
		OldSuffix: "config/",
		NewSuffix: "cfg/",
	}
	factory := &fakeToolFactory{migrations: []tool.Migration{rename, layout}}
	reg := tool.NewToolRegistry()
	reg.RegisterTool(factory)
	pkgMgr := &FakePkgMgr{Registry: reg, Err: nil}

	entries := []cdf.ManifestEntry{
		{Path: "tool/streamline-cli/0/config/settings.json", ComponentType: cdf.ComponentType{Name: "S", SchemaVersion: "1.0"}},
	}
	model := cdf.NewOnDiskModel("/runs", &cdf.Manifest{
		Entries:   entries,
		ToolsUsed: nil,
	}, cdf.Metadata{})
	loader := &fakeRunLoader{model: model}

	cm, _, err := LoadContent(context.Background(), loader, []run.RunID{{Value: "legacy"}}, pkgMgr)
	require.NoError(t, err)
	m := cm.Entries[0].Model

	comp, err := m.ResolveComponent("tool/bar/0/cfg/settings.json")
	require.NoError(t, err)
	assert.Equal(t,
		"tool/streamline-cli/0/config/settings.json",
		comp.RelativePath,
	)
}

// 3) Layout then rename, with recorded ToolsUsed
func TestLoadContent_LayoutThenRename_WithToolsUsed(t *testing.T) {
	layout := tool.Migration{
		Type:      "suffixRewrite",
		From:      "streamline-cli",
		Version:   "2.0.0",
		OldSuffix: "data/",
		NewSuffix: "dt/",
	}
	rename := tool.Migration{Type: "renameTool", From: "streamline-cli", To: "baz", Version: "3.0.0"}
	factory := &fakeToolFactory{migrations: []tool.Migration{layout, rename}}
	reg := tool.NewToolRegistry()
	reg.RegisterTool(factory)
	pkgMgr := &FakePkgMgr{Registry: reg, Err: nil}

	entries := []cdf.ManifestEntry{
		{Path: "tool/streamline-cli/0/data/items.csv", ComponentType: cdf.ComponentType{Name: "I", SchemaVersion: "1.0"}},
	}
	model := cdf.NewOnDiskModel("/runs", &cdf.Manifest{
		Entries:   entries,
		ToolsUsed: []cdf.ToolUsed{{Tool: "streamline-cli", Version: "1.0.0", Invocation: 0}},
	}, cdf.Metadata{})
	loader := &fakeRunLoader{model: model}

	cm, _, err := LoadContent(context.Background(), loader, []run.RunID{{Value: "r2"}}, pkgMgr)
	require.NoError(t, err)
	m := cm.Entries[0].Model

	// Today on disk: tool/baz/0/dt/items.csv
	comp, err := m.ResolveComponent("tool/baz/0/dt/items.csv")
	require.NoError(t, err)
	assert.Equal(t,
		"tool/streamline-cli/0/data/items.csv",
		comp.RelativePath,
	)
}

// 4) Layout then rename, legacy (no ToolsUsed)
func TestLoadContent_LayoutThenRename_Legacy(t *testing.T) {
	layout := tool.Migration{
		Type:      "suffixRewrite",
		From:      "streamline-cli",
		Version:   "2.0.0",
		OldSuffix: "data/",
		NewSuffix: "dt/",
	}
	rename := tool.Migration{Type: "renameTool", From: "streamline-cli", To: "baz", Version: "3.0.0"}
	factory := &fakeToolFactory{migrations: []tool.Migration{layout, rename}}
	reg := tool.NewToolRegistry()
	reg.RegisterTool(factory)
	pkgMgr := &FakePkgMgr{Registry: reg, Err: nil}

	entries := []cdf.ManifestEntry{
		{Path: "tool/streamline-cli/0/data/items.csv", ComponentType: cdf.ComponentType{Name: "I", SchemaVersion: "1.0"}},
	}
	model := cdf.NewOnDiskModel("/runs", &cdf.Manifest{
		Entries:   entries,
		ToolsUsed: nil,
	}, cdf.Metadata{})
	loader := &fakeRunLoader{model: model}

	cm, _, err := LoadContent(context.Background(), loader, []run.RunID{{Value: "legacy2"}}, pkgMgr)
	require.NoError(t, err)
	m := cm.Entries[0].Model

	comp, err := m.ResolveComponent("tool/baz/0/dt/items.csv")
	require.NoError(t, err)
	assert.Equal(t,
		"tool/streamline-cli/0/data/items.csv",
		comp.RelativePath,
	)
}

func TestLoadContent_InterleavedMigrations(t *testing.T) {
	tests := []struct {
		name       string
		migrations []tool.Migration
		entries    []cdf.ManifestEntry
		toolsUsed  []cdf.ToolUsed
		lookup     string
		want       string
	}{
		{
			name: "Layout_Rename_Layout_Rename_With_ToolsUsed",
			migrations: []tool.Migration{
				// 1) move root‐files into rootcfg/
				{Type: "suffixRewrite", From: "foo", Version: "1.1.0", OldSuffix: "", NewSuffix: "rootcfg/"},
				// 2) rename foo to bar
				{Type: "renameTool", From: "foo", To: "bar", Version: "1.5.0"},
				// 3) under bar: rootcfg/ to cfg/
				{Type: "suffixRewrite", From: "bar", Version: "2.0.0", OldSuffix: "rootcfg/", NewSuffix: "cfg/"},
				// 4) rename bar to baz
				{Type: "renameTool", From: "bar", To: "baz", Version: "2.5.0"},
			},
			entries: []cdf.ManifestEntry{
				// original manifest path
				{Path: "tool/foo/0/file.txt", ComponentType: cdf.ComponentType{Name: "X", SchemaVersion: "1.0"}},
			},
			toolsUsed: []cdf.ToolUsed{{Tool: "foo", Version: "1.0.0", Invocation: 0}},
			// today the renderer would look under baz/cfg/file.txt
			lookup: "tool/baz/0/cfg/file.txt",
			want:   "tool/foo/0/file.txt",
		},
		{
			name: "Layout_Rename_Layout_Rename_Legacy",
			migrations: []tool.Migration{
				{Type: "suffixRewrite", From: "streamline-cli", Version: "1.2.0", OldSuffix: "", NewSuffix: "rootcfg/"},
				{Type: "renameTool", From: "streamline-cli", To: "bar", Version: "1.5.0"},
				{Type: "suffixRewrite", From: "bar", Version: "1.6.0", OldSuffix: "rootcfg/", NewSuffix: "cfg/"},
				{Type: "renameTool", From: "bar", To: "baz", Version: "2.5.0"},
			},
			entries: []cdf.ManifestEntry{
				{Path: "tool/streamline-cli/0/file.txt", ComponentType: cdf.ComponentType{Name: "X", SchemaVersion: "1.0"}},
			},
			toolsUsed: nil, // legacy
			lookup:    "tool/baz/0/cfg/file.txt",
			want:      "tool/streamline-cli/0/file.txt",
		},
		{
			name: "Rename_Layout_Rename_With_ToolsUsed",
			migrations: []tool.Migration{
				// 1) rename foo to alpha
				{Type: "renameTool", From: "foo", To: "alpha", Version: "1.1.0"},
				// 2) under alpha: data/ to dt/
				{Type: "suffixRewrite", From: "alpha", Version: "1.2.0", OldSuffix: "data/", NewSuffix: "dt/"},
				// 3) rename alpha to beta
				{Type: "renameTool", From: "alpha", To: "beta", Version: "1.3.0"},
			},
			entries: []cdf.ManifestEntry{
				{Path: "tool/foo/0/data/config.yaml", ComponentType: cdf.ComponentType{Name: "C", SchemaVersion: "1.0"}},
			},
			toolsUsed: []cdf.ToolUsed{{Tool: "foo", Version: "1.0.0", Invocation: 0}},
			lookup:    "tool/beta/0/dt/config.yaml",
			want:      "tool/foo/0/data/config.yaml",
		},
		{
			name: "Rename_Layout_Rename_Legacy",
			migrations: []tool.Migration{
				{Type: "renameTool", From: "streamline-cli", To: "alpha", Version: "1.1.0"},
				{Type: "suffixRewrite", From: "alpha", Version: "1.2.0", OldSuffix: "data/", NewSuffix: "dt/"},
				{Type: "renameTool", From: "alpha", To: "beta", Version: "1.3.0"},
			},
			entries: []cdf.ManifestEntry{
				{Path: "tool/streamline-cli/0/data/config.yaml", ComponentType: cdf.ComponentType{Name: "C", SchemaVersion: "1.0"}},
			},
			toolsUsed: nil,
			lookup:    "tool/beta/0/dt/config.yaml",
			want:      "tool/streamline-cli/0/data/config.yaml",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			factory := &fakeToolFactory{migrations: tc.migrations}
			reg := tool.NewToolRegistry()
			reg.RegisterTool(factory)
			pkgMgr := &FakePkgMgr{Registry: reg, Err: nil}

			model := cdf.NewOnDiskModel("/runs", &cdf.Manifest{
				Entries:   tc.entries,
				ToolsUsed: tc.toolsUsed,
			}, cdf.Metadata{})
			loader := &fakeRunLoader{model: model}

			cm, _, err := LoadContent(context.Background(), loader, []run.RunID{{Value: "r"}}, pkgMgr)
			require.NoError(t, err)
			m := cm.Entries[0].Model

			comp, err := m.ResolveComponent(tc.lookup)
			require.NoError(t, err)
			assert.Equal(t, tc.want, comp.RelativePath)
		})
	}
}

func TestLoadContent_Streamline_InstructionMix_MultiTool_RenameAndLayout(t *testing.T) {
	tests := []struct {
		name       string
		migrations []tool.Migration
		entries    []cdf.ManifestEntry
		toolsUsed  []cdf.ToolUsed
		lookups    []string // componentPaths the renderer will ask for
		wants      []string // expected RelativePath for each lookup
	}{
		{
			name: "Legacy run: rename+layout on both tools",
			migrations: []tool.Migration{
				// streamline-cli to neoprof @2.0.0
				{Type: "renameTool", From: "streamline-cli", To: "neoprof", Version: "2.0.0"},
				// under neoprof, output/ to deep/output/ @3.0.0
				{Type: "suffixRewrite", From: "neoprof", Version: "3.0.0", OldSuffix: "output/", NewSuffix: "deep/output/"},
				// instruction_mix to instr_mix @1.5.0
				{Type: "renameTool", From: "instruction_mix", To: "instr_mix", Version: "1.5.0"},
				// under instr_mix, static_ to data/static_ @2.0.0
				{Type: "suffixRewrite", From: "instr_mix", Version: "2.0.0", OldSuffix: "static_", NewSuffix: "data/static_"},
			},
			// legacy manifest entries (no ToolsUsed)
			entries: []cdf.ManifestEntry{
				// original streamline-cli path (invocation folder present)
				{Path: "tool/streamline-cli/0/output/foo.csv", ComponentType: cdf.ComponentType{Name: "A", SchemaVersion: "1.0"}},
				// original instruction_mix path (no invocation folder)
				{Path: "tool/instruction_mix/0/static_bar.csv", ComponentType: cdf.ComponentType{Name: "B", SchemaVersion: "1.0"}},
			},
			toolsUsed: nil, // triggers legacy injection
			lookups: []string{
				"tool/neoprof/0/deep/output/foo.csv",   // new name+layout for streamline-cli
				"tool/instr_mix/0/data/static_bar.csv", // new name+layout for instruction_mix
			},
			wants: []string{
				"tool/streamline-cli/0/output/foo.csv",
				"tool/instruction_mix/0/static_bar.csv",
			},
		},
		{
			name: "New run: ToolsUsed present, reverse same migrations",
			migrations: []tool.Migration{
				// rename both tools
				{Type: "renameTool", From: "streamline-cli", To: "neoprof", Version: "2.0.0"},
				{Type: "renameTool", From: "instruction_mix", To: "instr_mix", Version: "1.5.0"},
				// layout on both
				{Type: "suffixRewrite", From: "neoprof", Version: "3.0.0", OldSuffix: "output/", NewSuffix: "deep/output/"},
				{Type: "suffixRewrite", From: "instr_mix", Version: "2.0.0", OldSuffix: "static_", NewSuffix: "data/static_"},
			},
			entries: []cdf.ManifestEntry{
				{Path: "tool/streamline-cli/0/output/foo.csv", ComponentType: cdf.ComponentType{Name: "A", SchemaVersion: "1.0"}},
				{Path: "tool/instruction_mix/0/static_bar.csv", ComponentType: cdf.ComponentType{Name: "B", SchemaVersion: "1.0"}},
			},
			toolsUsed: []cdf.ToolUsed{
				{Tool: "streamline-cli", Version: "1.0.0", Invocation: 0},
				{Tool: "instruction_mix", Version: "1.0.0", Invocation: 0},
			},
			lookups: []string{
				"tool/neoprof/0/deep/output/foo.csv",
				"tool/instr_mix/0/data/static_bar.csv",
			},
			wants: []string{
				"tool/streamline-cli/0/output/foo.csv",
				"tool/instruction_mix/0/static_bar.csv",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// register migrations
			fac := &fakeToolFactory{migrations: tc.migrations}
			reg := tool.NewToolRegistry()
			reg.RegisterTool(fac)
			pkgMgr := &FakePkgMgr{Registry: reg, Err: nil}

			model := cdf.NewOnDiskModel("/runs", &cdf.Manifest{
				Entries:   tc.entries,
				ToolsUsed: tc.toolsUsed,
			}, cdf.Metadata{})
			loader := &fakeRunLoader{model: model}

			cm, _, err := LoadContent(context.Background(), loader, []run.RunID{{Value: "r"}}, pkgMgr)
			require.NoError(t, err)
			m := cm.Entries[0].Model

			// for each lookup, we should get back the original manifest path
			for i, lookup := range tc.lookups {
				comp, err := m.ResolveComponent(lookup)
				require.NoError(t, err, "lookup %q should succeed", lookup)
				assert.Equal(t, tc.wants[i], comp.RelativePath)
			}
		})
	}
}

func TestLoadContent_LegacyMultiToolInjectionAndMigrations(t *testing.T) {
	// Migrations for two tools:
	//  1) foo to f1 at v2.0.0
	//  2) under f1, “data/” to “new/” at v3.0.0
	//  3) f1 to foo1 at v4.0.0
	//  4) bar to b1 at v1.5.0
	//  5) under b1, “” to “static_/” at v2.0.0
	migs := []tool.Migration{
		{Type: "renameTool", From: "foo", To: "f1", Version: "2.0.0"},
		{Type: "suffixRewrite", From: "f1", Version: "3.0.0", OldSuffix: "data/", NewSuffix: "new/"},
		{Type: "renameTool", From: "f1", To: "foo1", Version: "4.0.0"},
		{Type: "renameTool", From: "bar", To: "b1", Version: "1.5.0"},
		{Type: "suffixRewrite", From: "b1", Version: "2.0.0", OldSuffix: "", NewSuffix: "static_/"},
	}

	factory := &fakeToolFactory{migrations: migs}
	reg := tool.NewToolRegistry()
	reg.RegisterTool(factory)
	pkgMgr := &FakePkgMgr{Registry: reg, Err: nil}

	// Legacy manifest: no ToolsUsed, two tools present in paths
	entries := []cdf.ManifestEntry{
		// foo uses invocation folder
		{Path: "tool/foo/0/data/alpha.csv", ComponentType: cdf.ComponentType{Name: "A", SchemaVersion: "1.0"}},
		{Path: "tool/bar/0/beta.txt", ComponentType: cdf.ComponentType{Name: "B", SchemaVersion: "1.0"}},
	}

	model := cdf.NewOnDiskModel("/runs", &cdf.Manifest{
		Entries:   entries,
		ToolsUsed: nil, // trigger legacy injection
	}, cdf.Metadata{})
	loader := &fakeRunLoader{model: model}

	cm, _, err := LoadContent(context.Background(), loader, []run.RunID{{Value: "r"}}, pkgMgr)
	require.NoError(t, err)
	m := cm.Entries[0].Model

	// 1) We should have injected both foo@1.0.0 and bar@1.0.0
	tu := model.Manifest().ToolsUsed
	require.Len(t, tu, 2)
	got := map[string]string{tu[0].Tool: tu[0].Version, tu[1].Tool: tu[1].Version}
	assert.Equal(t, "1.0.0", got["foo"])
	assert.Equal(t, "1.0.0", got["bar"])

	// 2) Now test resolving each tool through rename→layout→rename:

	// foo path: on-disk is foo1/0/new/alpha.csv
	comp, err := m.ResolveComponent("tool/foo1/0/new/alpha.csv")
	require.NoError(t, err)
	assert.Equal(t,
		"tool/foo/0/data/alpha.csv",
		comp.RelativePath,
	)

	// bar path: on-disk is b1/static_/beta.txt
	comp2, err := m.ResolveComponent("tool/b1/0/static_/beta.txt")
	require.NoError(t, err)
	assert.Equal(t,
		"tool/bar/0/beta.txt",
		comp2.RelativePath,
	)
}

func TestLoadContent_UnrelatedToolMigrationsAreIgnored(t *testing.T) {
	// Suppose foo→fuu at v2.0.0, and bar to baz at v2.0.0
	fooRename := tool.Migration{Type: "renameTool", From: "foo", To: "fuu", Version: "2.0.0"}
	barRename := tool.Migration{Type: "renameTool", From: "bar", To: "baz", Version: "2.0.0"}
	factory := &fakeToolFactory{migrations: []tool.Migration{fooRename, barRename}}
	reg := tool.NewToolRegistry()
	reg.RegisterTool(factory)
	pkgMgr := &FakePkgMgr{Registry: reg, Err: nil}

	// Manifest only contains a foo file
	entries := []cdf.ManifestEntry{
		{Path: "tool/foo/0/file.txt", ComponentType: cdf.ComponentType{Name: "X", SchemaVersion: "1.0"}},
	}

	// Case A: run recorded foo@1.0.0
	t.Run("WithToolsUsed", func(t *testing.T) {
		model := cdf.NewOnDiskModel("/runs", &cdf.Manifest{
			Entries:   entries,
			ToolsUsed: []cdf.ToolUsed{{Tool: "foo", Version: "1.0.0", Invocation: 0}},
		}, cdf.Metadata{})
		loader := &fakeRunLoader{model: model}

		cm, _, err := LoadContent(context.Background(), loader, []run.RunID{{Value: "r"}}, pkgMgr)
		require.NoError(t, err)
		m := cm.Entries[0].Model

		// foo to fuu should work:
		comp, err := m.ResolveComponent("tool/fuu/0/file.txt")
		require.NoError(t, err)
		assert.Equal(t, "tool/foo/0/file.txt", comp.RelativePath)

		// but bar to baz should *not* fire on a foo run:
		_, err = m.ResolveComponent("tool/baz/0/file.txt")
		assert.Error(t, err, "bar→baz must not rewrite foo runs")
	})

	// Case B: legacy run (no ToolsUsed)
	t.Run("Legacy", func(t *testing.T) {
		model := cdf.NewOnDiskModel("/runs", &cdf.Manifest{Entries: entries, ToolsUsed: nil}, cdf.Metadata{})
		loader := &fakeRunLoader{model: model}

		cm, _, err := LoadContent(context.Background(), loader, []run.RunID{{Value: "legacy"}}, pkgMgr)
		require.NoError(t, err)
		m := cm.Entries[0].Model

		// we inject foo@1.0.0 by default, so foo to fuu still works:
		comp, err := m.ResolveComponent("tool/fuu/0/file.txt")
		require.NoError(t, err)
		assert.Equal(t, "tool/foo/0/file.txt", comp.RelativePath)

		// and bar baz must still not apply:
		_, err = m.ResolveComponent("tool/baz/0/file.txt")
		assert.Error(t, err)
	})
}

func TestLoadContent_DoesNotRegisterUnrelatedToolMigrations(t *testing.T) {
	// Declare two renames:
	//  • foo to foo2 @2.0.0
	//  • bar to bar2 @2.0.0
	renameFoo := tool.Migration{Type: "renameTool", From: "foo", To: "foo2", Version: "2.0.0"}
	renameBar := tool.Migration{Type: "renameTool", From: "bar", To: "bar2", Version: "2.0.0"}
	factory := &fakeToolFactory{migrations: []tool.Migration{renameFoo, renameBar}}
	reg := tool.NewToolRegistry()
	reg.RegisterTool(factory)
	pkgMgr := &FakePkgMgr{Registry: reg, Err: nil}

	// Our manifest only contains foo:
	entries := []cdf.ManifestEntry{
		{Path: "tool/foo/0/a.txt", ComponentType: cdf.ComponentType{Name: "A", SchemaVersion: "1.0"}},
	}
	model := cdf.NewOnDiskModel("/runs", &cdf.Manifest{
		Entries:   entries,
		ToolsUsed: []cdf.ToolUsed{{Tool: "foo", Version: "1.0.0", Invocation: 0}},
	}, cdf.Metadata{})
	loader := &fakeRunLoader{model: model}

	cm, _, err := LoadContent(context.Background(), loader, []run.RunID{{Value: "r"}}, pkgMgr)
	require.NoError(t, err)
	m := cm.Entries[0].Model

	// Foo migration should apply.
	comp, err := m.ResolveComponent("tool/foo2/0/a.txt")
	require.NoError(t, err)
	assert.Equal(t, "tool/foo/0/a.txt", comp.RelativePath)

	// Bar migration must not apply.
	_, err = m.ResolveComponent("tool/bar2/0/a.txt")
	assert.Error(t, err)
}

func TestInstructionMix_InvocationOnly(t *testing.T) {
	// At v1.1.0, instruction_mix began writing under …/0/… instead of root.
	mig := tool.Migration{
		Type:    "missingInvocation",
		From:    "instruction_mix",
		Version: "1.1.0",
	}
	factory := &fakeToolFactory{migrations: []tool.Migration{mig}}
	reg := tool.NewToolRegistry()
	reg.RegisterTool(factory)
	pkgMgr := &FakePkgMgr{Registry: reg, Err: nil}

	// Old manifest: root‐level
	entries := []cdf.ManifestEntry{
		{Path: "tool/instruction_mix/fileA.txt", ComponentType: cdf.ComponentType{Name: "A", SchemaVersion: "1.0"}},
	}
	// Run recorded as instruction_mix@1.0.0
	model := cdf.NewOnDiskModel("/runs", &cdf.Manifest{
		Entries:   entries,
		ToolsUsed: []cdf.ToolUsed{{Tool: "instruction_mix", Version: "1.0.0", Invocation: 0}},
	}, cdf.Metadata{})
	loader := &fakeRunLoader{model: model}

	cm, _, err := LoadContent(context.Background(), loader, []run.RunID{{Value: "r"}}, pkgMgr)
	require.NoError(t, err)
	m := cm.Entries[0].Model

	// On-disk today, code will ask for tool/instruction_mix/0/fileA.txt
	comp, err := m.ResolveComponent("tool/instruction_mix/0/fileA.txt")
	require.NoError(t, err)
	assert.Equal(t, "tool/instruction_mix/fileA.txt", comp.RelativePath)
}

func TestInstructionMix_InvocationThenSuffix(t *testing.T) {
	// (a) invocation introduced @1.1.0
	// (b) static_ → data/static_ @2.0.0
	mig1 := tool.Migration{
		Type:    "missingInvocation",
		From:    "instruction_mix",
		Version: "1.1.0",
	}
	mig2 := tool.Migration{
		Type:      "suffixRewrite",
		From:      "instruction_mix",
		Version:   "2.0.0",
		OldSuffix: "static_",
		NewSuffix: "data/static_",
	}
	factory := &fakeToolFactory{migrations: []tool.Migration{mig1, mig2}}
	reg := tool.NewToolRegistry()
	reg.RegisterTool(factory)
	pkgMgr := &FakePkgMgr{Registry: reg, Err: nil}

	// Original manifest: root-level static_
	entries := []cdf.ManifestEntry{
		{Path: "tool/instruction_mix/static_B.csv", ComponentType: cdf.ComponentType{Name: "B", SchemaVersion: "1.0"}},
	}
	model := cdf.NewOnDiskModel("/runs", &cdf.Manifest{
		Entries:   entries,
		ToolsUsed: []cdf.ToolUsed{{Tool: "instruction_mix", Version: "1.0.0", Invocation: 0}},
	}, cdf.Metadata{})
	loader := &fakeRunLoader{model: model}

	cm, _, err := LoadContent(context.Background(), loader, []run.RunID{{Value: "r"}}, pkgMgr)
	require.NoError(t, err)
	m := cm.Entries[0].Model

	// On-disk lookup includes invocation and new suffix
	lookup := "tool/instruction_mix/0/data/static_B.csv"
	comp, err := m.ResolveComponent(lookup)
	require.NoError(t, err)
	assert.Equal(t, "tool/instruction_mix/static_B.csv", comp.RelativePath)
}

func TestInstructionMix_ChainedNameLayoutInvocation(t *testing.T) {
	// (a) rename instruction_mix→instr_mix @1.2.0
	// (b) invocation introduced @1.3.0
	// (c) layout data/out→out @1.4.0
	rename := tool.Migration{Type: "renameTool", From: "instruction_mix", To: "instr_mix", Version: "1.2.0"}
	invoc := tool.Migration{Type: "missingInvocation", From: "instr_mix", Version: "1.3.0"}
	layout := tool.Migration{Type: "suffixRewrite", From: "instr_mix", Version: "1.4.0", OldSuffix: "data/out/", NewSuffix: "out/"}
	factory := &fakeToolFactory{migrations: []tool.Migration{rename, invoc, layout}}
	reg := tool.NewToolRegistry()
	reg.RegisterTool(factory)
	pkgMgr := &FakePkgMgr{Registry: reg, Err: nil}

	// Original manifest written by 1.1.0, so still instruction_mix and root data/out
	entries := []cdf.ManifestEntry{
		{Path: "tool/instruction_mix/data/out/chain.csv", ComponentType: cdf.ComponentType{Name: "C", SchemaVersion: "1.0"}},
	}
	model := cdf.NewOnDiskModel("/runs", &cdf.Manifest{
		Entries:   entries,
		ToolsUsed: []cdf.ToolUsed{{Tool: "instruction_mix", Version: "1.1.0", Invocation: 0}},
	}, cdf.Metadata{})
	loader := &fakeRunLoader{model: model}

	cm, _, err := LoadContent(context.Background(), loader, []run.RunID{{Value: "r"}}, pkgMgr)
	require.NoError(t, err)
	m := cm.Entries[0].Model

	// On-disk lookup uses all three migrations forward chained
	lookup := "tool/instr_mix/0/out/chain.csv"
	comp, err := m.ResolveComponent(lookup)
	require.NoError(t, err)
	assert.Equal(t, "tool/instruction_mix/data/out/chain.csv", comp.RelativePath)
}

func TestInterleavedTools_InvocationAndRename(t *testing.T) {
	scRename := tool.Migration{Type: "renameTool", From: "streamline-cli", To: "scli", Version: "1.2.0"}
	scInvoc := tool.Migration{Type: "missingInvocation", From: "scli", Version: "1.3.0"}
	imInvoc := tool.Migration{Type: "missingInvocation", From: "instruction_mix", Version: "1.1.0"}
	imRename := tool.Migration{Type: "renameTool", From: "instruction_mix", To: "foo", Version: "1.3.0"}

	factory := &fakeToolFactory{migrations: []tool.Migration{scRename, scInvoc, imInvoc, imRename}}
	reg := tool.NewToolRegistry()
	reg.RegisterTool(factory)
	pkgMgr := &FakePkgMgr{Registry: reg, Err: nil}

	entries := []cdf.ManifestEntry{
		{Path: "tool/streamline-cli/static_/foo.csv", ComponentType: cdf.ComponentType{Name: "F", SchemaVersion: "1.0"}},
		{Path: "tool/instruction_mix/bar.txt", ComponentType: cdf.ComponentType{Name: "B", SchemaVersion: "1.0"}},
	}
	model := cdf.NewOnDiskModel("/runs", &cdf.Manifest{
		Entries: entries,
		ToolsUsed: []cdf.ToolUsed{
			{Tool: "streamline-cli", Version: "1.0.0", Invocation: 0},
			{Tool: "instruction_mix", Version: "1.0.0", Invocation: 0},
		},
	}, cdf.Metadata{})
	loader := &fakeRunLoader{model: model}

	cm, _, err := LoadContent(context.Background(), loader, []run.RunID{{Value: "r"}}, pkgMgr)
	require.NoError(t, err)
	m := cm.Entries[0].Model

	// streamline-cli: on-disk now under tool/scli/0/static/foo.csv
	comp1, err := m.ResolveComponent("tool/scli/0/static_/foo.csv")
	require.NoError(t, err)
	assert.Equal(t, "tool/streamline-cli/static_/foo.csv", comp1.RelativePath)

	// instruction_mix: on-disk now under tool/foo/0/bar.txt
	comp2, err := m.ResolveComponent("tool/foo/0/bar.txt")
	require.NoError(t, err)
	assert.Equal(t, "tool/instruction_mix/bar.txt", comp2.RelativePath)
}
