// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package cdf

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf/semver"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
)

type failOnOpenFS struct {
	afero.Fs
	blockedPath string
}

func (f failOnOpenFS) Open(name string) (afero.File, error) {
	if NormalizePath(name) == NormalizePath(f.blockedPath) {
		return nil, fmt.Errorf("blocked path opened: %s", name)
	}
	return f.Fs.Open(name)
}

func TestModel(t *testing.T) {
	t.Run("resolvecomponent finds item in manifest and makes it absolute", func(t *testing.T) {
		cType := ComponentType{
			Name:          "foo",
			SchemaVersion: "bar",
		}
		model := NewOnDiskModel("/foo/bar", &Manifest{Entries: []ManifestEntry{
			{Path: "aaa/bbb", ComponentType: cType},
		}}, Metadata{})

		component, err := model.ResolveComponent("aaa/bbb")
		assert.NoError(t, err)

		assert.Equal(t, "aaa/bbb", component.RelativePath)
		assert.Equal(t, cType, component.Type)
		assert.Equal(t, filepath.FromSlash("/foo/bar/aaa/bbb"), component.AbsolutePath)
	})

	t.Run("resolvecomponent finds unknown item type and errors if not in manifest", func(t *testing.T) {
		model := NewOnDiskModel("/foo/bar", &Manifest{Entries: []ManifestEntry{}}, Metadata{})

		component, err := model.ResolveComponent("aaa/bbb")
		assert.Error(t, err)

		assert.Equal(t, "aaa/bbb", component.RelativePath)
		assert.Equal(t, "unknown", component.Type.Name)
		assert.Equal(t, "", component.AbsolutePath)
	})

	t.Run("resolvecomponent finds unknown item type but well defined absolute path if not in manifest but existent on disk", func(t *testing.T) {
		model := NewOnDiskModel("/foo/bar", &Manifest{Entries: []ManifestEntry{}}, Metadata{})

		model.FS = afero.NewMemMapFs()
		err := model.FS.MkdirAll("/foo/bar/aaa/bbb", perms.LocalDirPerm)
		assert.NoError(t, err)
		if err != nil {
			return
		}
		file, err := model.FS.Create("/foo/bar/aaa/bbb")
		assert.NoError(t, err)
		if err != nil {
			return
		}
		file.Close()

		component, err := model.ResolveComponent("aaa/bbb")
		assert.NoError(t, err)

		assert.Equal(t, "aaa/bbb", component.RelativePath)
		assert.Equal(t, "unknown", component.Type.Name)
		assert.Equal(t, filepath.FromSlash("/foo/bar/aaa/bbb"), component.AbsolutePath)
	})

	t.Run("resolvecomponent rejects paths that escape the model base", func(t *testing.T) {
		model := NewOnDiskModel("/foo/bar", &Manifest{Entries: []ManifestEntry{}}, Metadata{})
		model.FS = afero.NewMemMapFs()

		err := model.FS.MkdirAll("/foo", perms.LocalDirPerm)
		require.NoError(t, err)

		file, err := model.FS.Create("/foo/outside.txt")
		require.NoError(t, err)
		require.NoError(t, file.Close())

		component, err := model.ResolveComponent("../outside.txt")

		require.Error(t, err)
		require.ErrorContains(t, err, "escapes model base")
		require.Equal(t, Component{}, component)
	})
}

func TestResolveComponent(t *testing.T) {
	cType := ComponentType{Name: "type", SchemaVersion: "1"}

	t.Run("returns pending error for requested component", func(t *testing.T) {
		model := NewOnDiskModel("/base", &Manifest{Entries: []ManifestEntry{
			{Path: "a/component", ComponentType: cType, Pending: true},
		}}, Metadata{})

		component, err := model.ResolveComponent("a/component")

		require.ErrorIs(t, err, ErrComponentPending)
		require.Equal(t, Component{}, component)
	})

	t.Run("returns pending error when matching child is pending", func(t *testing.T) {
		model := NewOnDiskModel("/base", &Manifest{Entries: []ManifestEntry{
			{Path: "a/*.csv", ComponentType: cType},
			{Path: "a/pending.csv", ComponentType: cType, Pending: true},
		}}, Metadata{})

		component, err := model.ResolveComponent("a/*.csv")

		require.ErrorIs(t, err, ErrComponentPending)
		require.Equal(t, Component{}, component)
	})

	t.Run("ignores unrelated pending components", func(t *testing.T) {
		model := NewOnDiskModel("/base", &Manifest{Entries: []ManifestEntry{
			{Path: "a/*.csv", ComponentType: cType},
			{Path: "a/ready.csv", ComponentType: cType},
			{Path: "b/pending.csv", ComponentType: cType, Pending: true},
		}}, Metadata{})

		component, err := model.ResolveComponent("a/*.csv")

		require.NoError(t, err)
		require.Equal(t, Component{
			Type:         cType,
			RelativePath: "a/*.csv",
			AbsolutePath: filepath.FromSlash("/base/a/*.csv"),
		}, component)
	})
}

func TestModelResolveExpectedComponentType(t *testing.T) {
	cType := ComponentType{
		Name:          "foo",
		SchemaVersion: "bar",
	}
	model := NewOnDiskModel("/foo/bar", &Manifest{Entries: []ManifestEntry{
		{Path: "aaa/bbb", ComponentType: cType},
	}}, Metadata{})

	t.Run("resolvecomponentexpecttype fails if component type mismatches name", func(t *testing.T) {
		mismatchCType := ComponentType{Name: "a", SchemaVersion: cType.SchemaVersion}
		_, err := model.ResolveComponentExpectType("aaa/bbb", mismatchCType)
		assert.Error(t, err)
		assert.ErrorContains(t, err, "/foo/bar")
		assert.ErrorContains(t, err, "aaa/bbb")
		assert.ErrorContains(t, err, fmt.Sprintf("%v", cType))
		assert.ErrorContains(t, err, fmt.Sprintf("%v", mismatchCType))
	})

	t.Run("resolvecomponentexpecttype fails if component type mismatches version", func(t *testing.T) {
		mismatchCType := ComponentType{Name: cType.Name, SchemaVersion: "qqqq"}
		_, err := model.ResolveComponentExpectType("aaa/bbb", mismatchCType)
		assert.Error(t, err)
		assert.ErrorContains(t, err, "/foo/bar")
		assert.ErrorContains(t, err, "aaa/bbb")
		assert.ErrorContains(t, err, fmt.Sprintf("%v", cType))
		assert.ErrorContains(t, err, fmt.Sprintf("%v", mismatchCType))
	})

	t.Run("resolvecomponentexpecttype returns absolute path for match", func(t *testing.T) {
		component, err := model.ResolveComponentExpectType("aaa/bbb", cType)
		assert.NoError(t, err)
		assert.Equal(t, filepath.FromSlash("/foo/bar/aaa/bbb"), component.AbsolutePath)
	})
}

func TestModelFindEntities(t *testing.T) {
	fs := afero.NewMemMapFs()
	model := NewOnDiskModel("/a/model", &Manifest{}, Metadata{})
	model.FS = fs

	err := fs.MkdirAll(`/a/model`, perms.LocalDirPerm)
	assert.NoError(t, err)

	err = fs.MkdirAll(`/a/model/entity1/sub`, perms.LocalDirPerm)
	assert.NoError(t, err)

	err = fs.MkdirAll(`/a/model/entity1/sub/sub2`, perms.LocalDirPerm)
	assert.NoError(t, err)

	err = fs.MkdirAll(`/a/model/entity1/sub1/sub2`, perms.LocalDirPerm)
	assert.NoError(t, err)

	file, err := fs.OpenFile(`/a/model/entity1/sub/sub2/component`, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perms.LocalDirPerm)
	assert.NoError(t, err)
	file.Close()

	err = fs.MkdirAll(`/a/model/entity2`, perms.LocalDirPerm)
	assert.NoError(t, err)

	err = fs.MkdirAll(`/not/a/model`, perms.LocalDirPerm)
	assert.NoError(t, err)

	t.Run("only finds entities not components", func(t *testing.T) {
		entities, err := model.FindEntities("**")
		assert.NoError(t, err)
		assert.Equal(t, []Entity{
			{RelativePath: ""}, // root entity
			{RelativePath: "entity1"},
			{RelativePath: "entity1/sub"},
			{RelativePath: "entity1/sub/sub2"},
			{RelativePath: "entity1/sub1"},
			{RelativePath: "entity1/sub1/sub2"},
			{RelativePath: "entity2"},
		}, entities)
	})

	t.Run("star works", func(t *testing.T) {
		entities, err := model.FindEntities("entity1/*")
		assert.NoError(t, err)
		assert.Equal(t, []Entity{
			{RelativePath: "entity1/sub"},
			{RelativePath: "entity1/sub1"},
		}, entities)
	})

	t.Run("double star works", func(t *testing.T) {
		entities, err := model.FindEntities("**/sub2")
		assert.NoError(t, err)
		assert.Equal(t, []Entity{
			{RelativePath: "entity1/sub/sub2"},
			{RelativePath: "entity1/sub1/sub2"},
		}, entities)
	})

	t.Run("finds root entity", func(t *testing.T) {
		entities, err := model.FindEntities("")
		assert.NoError(t, err)
		assert.Equal(t, []Entity{
			{RelativePath: ""},
		}, entities)
	})

	t.Run("finds root entity from /", func(t *testing.T) {
		entities, err := model.FindEntities("/")
		assert.NoError(t, err)
		assert.Equal(t, []Entity{
			{RelativePath: ""},
		}, entities)
	})

	t.Run("backslashes in glob normalized", func(t *testing.T) {
		entities, err := model.FindEntities("\\entity1\\**")
		assert.NoError(t, err)
		assert.Equal(t, []Entity{
			{RelativePath: "entity1/sub"},
			{RelativePath: "entity1/sub/sub2"},
			{RelativePath: "entity1/sub1"},
			{RelativePath: "entity1/sub1/sub2"},
		}, entities)
	})
}

func TestModelFindComponents(t *testing.T) {
	fs := afero.NewMemMapFs()
	logType := ComponentType{
		Name:          TypeLogText,
		SchemaVersion: "0.1",
	}
	jsonType := ComponentType{
		Name:          TypeLogJSON,
		SchemaVersion: "0.1",
	}
	metricsType := ComponentType{
		Name:          "metrics",
		SchemaVersion: "0.1",
	}

	model := NewOnDiskModel("/a/model", &Manifest{Entries: []ManifestEntry{
		{Path: "tool/logs/0/messages.txt", ComponentType: logType},
		{Path: "tool/logs/1/events.jsonl", ComponentType: jsonType},
		{Path: "tool/other/0/metrics.csv", ComponentType: metricsType},
	}}, Metadata{})
	model.FS = fs

	require.NoError(t, fs.MkdirAll(`/a/model/tool/logs/0/nested`, perms.LocalDirPerm))
	require.NoError(t, fs.MkdirAll(`/a/model/tool/logs/1`, perms.LocalDirPerm))
	require.NoError(t, fs.MkdirAll(`/a/model/tool/other/0`, perms.LocalDirPerm))

	file, err := fs.Create(`/a/model/tool/logs/0/messages.txt`)
	require.NoError(t, err)
	require.NoError(t, file.Close())

	file, err = fs.Create(`/a/model/tool/logs/0/nested/ignored.txt`)
	require.NoError(t, err)
	require.NoError(t, file.Close())

	file, err = fs.Create(`/a/model/tool/logs/1/events.jsonl`)
	require.NoError(t, err)
	require.NoError(t, file.Close())

	file, err = fs.Create(`/a/model/tool/other/0/metrics.csv`)
	require.NoError(t, err)
	require.NoError(t, file.Close())

	t.Run("finds components recursively by glob", func(t *testing.T) {
		components, err := model.FindComponents("tool/logs/**")
		require.NoError(t, err)
		require.Equal(t, []Component{
			{
				Type:         logType,
				RelativePath: "tool/logs/0/messages.txt",
				AbsolutePath: filepath.FromSlash("/a/model/tool/logs/0/messages.txt"),
			},
			{
				Type:         unknownComponentType,
				RelativePath: "tool/logs/0/nested/ignored.txt",
				AbsolutePath: filepath.FromSlash("/a/model/tool/logs/0/nested/ignored.txt"),
			},
			{
				Type:         jsonType,
				RelativePath: "tool/logs/1/events.jsonl",
				AbsolutePath: filepath.FromSlash("/a/model/tool/logs/1/events.jsonl"),
			},
		}, components)
	})

	t.Run("backslashes in glob normalized", func(t *testing.T) {
		components, err := model.FindComponents(`\tool\logs\**`)
		require.NoError(t, err)
		require.Len(t, components, 3)
		require.Equal(t, "tool/logs/0/messages.txt", components[0].RelativePath)
		require.Equal(t, "tool/logs/0/nested/ignored.txt", components[1].RelativePath)
		require.Equal(t, "tool/logs/1/events.jsonl", components[2].RelativePath)
	})

	t.Run("leading slash is ignored", func(t *testing.T) {
		components, err := model.FindComponents("/tool/other/**")
		require.NoError(t, err)
		require.Equal(t, []Component{
			{
				Type:         metricsType,
				RelativePath: "tool/other/0/metrics.csv",
				AbsolutePath: filepath.FromSlash("/a/model/tool/other/0/metrics.csv"),
			},
		}, components)
	})

	t.Run("accepts an exact component glob without widening it", func(t *testing.T) {
		components, err := model.FindComponents("tool/other/0/metrics.csv")
		require.NoError(t, err)
		require.Equal(t, []Component{
			{
				Type:         metricsType,
				RelativePath: "tool/other/0/metrics.csv",
				AbsolutePath: filepath.FromSlash("/a/model/tool/other/0/metrics.csv"),
			},
		}, components)
	})

	t.Run("returns no matches for an exact component path that does not exist", func(t *testing.T) {
		components, err := model.FindComponents("tool/other/0/missing.csv")
		require.NoError(t, err)
		require.Empty(t, components)
	})

	t.Run("returns unknown concrete components for files transferred from a former globbed request", func(t *testing.T) {
		globbedManifestModel := NewOnDiskModel("/a/globbed-model", &Manifest{Entries: []ManifestEntry{
			{
				Path:          "tool/example/0/output/*.parquet",
				ComponentType: ComponentType{Name: "example-data", SchemaVersion: "1.0"},
			},
		}}, Metadata{})
		globbedManifestModel.FS = afero.NewMemMapFs()

		require.NoError(t, globbedManifestModel.FS.MkdirAll(`/a/globbed-model/tool/example/0/output`, perms.LocalDirPerm))

		file, err := globbedManifestModel.FS.Create(`/a/globbed-model/tool/example/0/output/one.parquet`)
		require.NoError(t, err)
		require.NoError(t, file.Close())

		file, err = globbedManifestModel.FS.Create(`/a/globbed-model/tool/example/0/output/two.parquet`)
		require.NoError(t, err)
		require.NoError(t, file.Close())

		components, err := globbedManifestModel.FindComponents("tool/example/0/output/**")
		require.NoError(t, err)
		require.Equal(t, []Component{
			{
				Type:         unknownComponentType,
				RelativePath: "tool/example/0/output/one.parquet",
				AbsolutePath: filepath.FromSlash("/a/globbed-model/tool/example/0/output/one.parquet"),
			},
			{
				Type:         unknownComponentType,
				RelativePath: "tool/example/0/output/two.parquet",
				AbsolutePath: filepath.FromSlash("/a/globbed-model/tool/example/0/output/two.parquet"),
			},
		}, components)
	})

	t.Run("invalid glob returns an error", func(t *testing.T) {
		_, err := model.FindComponents("[")
		require.Error(t, err)
		require.ErrorContains(t, err, "failure during pattern match")
	})

	t.Run("invalid glob returns an error even when the tree is empty", func(t *testing.T) {
		emptyModel := NewOnDiskModel("/a/empty-model", &Manifest{}, Metadata{})
		emptyModel.FS = afero.NewMemMapFs()

		require.NoError(t, emptyModel.FS.MkdirAll(`/a/empty-model`, perms.LocalDirPerm))

		_, err := emptyModel.FindComponents("[")
		require.Error(t, err)
		require.ErrorContains(t, err, "failure during pattern match")
	})

	t.Run("limits walking to the literal prefix before glob metacharacters", func(t *testing.T) {
		baseFS := afero.NewMemMapFs()
		scopedModel := NewOnDiskModel("/a/scoped-model", &Manifest{Entries: []ManifestEntry{
			{Path: "tool/logs/0/messages.txt", ComponentType: logType},
		}}, Metadata{})

		require.NoError(t, baseFS.MkdirAll(`/a/scoped-model/tool/logs/0`, perms.LocalDirPerm))
		require.NoError(t, baseFS.MkdirAll(`/a/scoped-model/unrelated/deep`, perms.LocalDirPerm))

		file, err := baseFS.Create(`/a/scoped-model/tool/logs/0/messages.txt`)
		require.NoError(t, err)
		require.NoError(t, file.Close())

		file, err = baseFS.Create(`/a/scoped-model/unrelated/deep/ignored.txt`)
		require.NoError(t, err)
		require.NoError(t, file.Close())

		scopedModel.FS = failOnOpenFS{
			Fs:          baseFS,
			blockedPath: `/a/scoped-model/unrelated`,
		}

		components, err := scopedModel.FindComponents("tool/logs/**")
		require.NoError(t, err)
		require.Equal(t, []Component{
			{
				Type:         logType,
				RelativePath: "tool/logs/0/messages.txt",
				AbsolutePath: filepath.FromSlash("/a/scoped-model/tool/logs/0/messages.txt"),
			},
		}, components)
	})

	t.Run("returns no matches when the scoped glob root does not exist", func(t *testing.T) {
		components, err := model.FindComponents("tool/missing/**")
		require.NoError(t, err)
		require.Empty(t, components)
	})

	t.Run("returns an error when the glob prefix escapes the model base", func(t *testing.T) {
		components, err := model.FindComponents("../**/*.csv")
		require.Nil(t, components)
		require.Error(t, err)
		require.ErrorContains(t, err, "escapes model base")
	})

	t.Run("scopes walking to the last literal path segment before an in-segment wildcard", func(t *testing.T) {
		baseFS := afero.NewMemMapFs()
		scopedModel := NewOnDiskModel("/a/mid-segment-model", &Manifest{Entries: []ManifestEntry{
			{Path: "tool/logs-app/0/messages.txt", ComponentType: logType},
		}}, Metadata{})

		require.NoError(t, baseFS.MkdirAll(`/a/mid-segment-model/tool/logs-app/0`, perms.LocalDirPerm))
		require.NoError(t, baseFS.MkdirAll(`/a/mid-segment-model/elsewhere/deep`, perms.LocalDirPerm))

		file, err := baseFS.Create(`/a/mid-segment-model/tool/logs-app/0/messages.txt`)
		require.NoError(t, err)
		require.NoError(t, file.Close())

		file, err = baseFS.Create(`/a/mid-segment-model/elsewhere/deep/ignored.txt`)
		require.NoError(t, err)
		require.NoError(t, file.Close())

		scopedModel.FS = failOnOpenFS{
			Fs:          baseFS,
			blockedPath: `/a/mid-segment-model/elsewhere`,
		}

		components, err := scopedModel.FindComponents("tool/logs-*/**")
		require.NoError(t, err)
		require.Equal(t, []Component{
			{
				Type:         logType,
				RelativePath: "tool/logs-app/0/messages.txt",
				AbsolutePath: filepath.FromSlash("/a/mid-segment-model/tool/logs-app/0/messages.txt"),
			},
		}, components)
	})
}

func TestModelListEntityComponents(t *testing.T) {
	fs := afero.NewMemMapFs()
	cType1 := ComponentType{
		Name:          "foo",
		SchemaVersion: "bar",
	}
	cType2 := ComponentType{
		Name:          "baz",
		SchemaVersion: "boz",
	}
	cType3 := ComponentType{
		Name:          "aaa",
		SchemaVersion: "bbb",
	}
	model := NewOnDiskModel("/a/model", &Manifest{Entries: []ManifestEntry{
		{Path: "entity1/component1", ComponentType: cType1},
		{Path: "entity1/sub/sub_component1", ComponentType: cType2},
		{Path: "entity1/sub/sub_component2", ComponentType: cType3},
	}}, Metadata{})
	model.FS = fs

	err := fs.MkdirAll(`/a/model`, perms.LocalDirPerm)
	assert.NoError(t, err)

	err = fs.MkdirAll(`/a/model/entity1/sub`, perms.LocalDirPerm)
	assert.NoError(t, err)

	err = fs.MkdirAll(`/a/model/entity1/sub/sub2`, perms.LocalDirPerm)
	assert.NoError(t, err)

	file, err := fs.Create(`/a/model/component_in_root`)
	assert.NoError(t, err)
	file.Close()

	file, err = fs.Create(`/a/model/entity1/component1`)
	assert.NoError(t, err)
	file.Close()

	file, err = fs.Create(`/a/model/entity1/sub/sub_component1`)
	assert.NoError(t, err)
	file.Close()

	file, err = fs.Create(`/a/model/entity1/sub/sub_component2`)
	assert.NoError(t, err)
	file.Close()

	t.Run("lists components in root", func(t *testing.T) {
		components, err := model.ListEntityComponents(Entity{"/"})
		assert.NoError(t, err)
		assert.Equal(t, []Component{
			{
				unknownComponentType,
				"component_in_root",
				filepath.FromSlash("/a/model/component_in_root"),
			},
		}, components)
	})

	t.Run("lists components in entity1", func(t *testing.T) {
		components, err := model.ListEntityComponents(Entity{"entity1/"})
		assert.NoError(t, err)
		assert.Equal(t, []Component{
			{
				cType1,
				"entity1/component1",
				filepath.FromSlash("/a/model/entity1/component1"),
			},
		}, components)
	})

	t.Run("lists components in entity1_sub", func(t *testing.T) {
		components, err := model.ListEntityComponents(Entity{"entity1/sub"})
		assert.NoError(t, err)
		assert.Equal(t, []Component{
			{
				cType2,
				"entity1/sub/sub_component1",
				filepath.FromSlash("/a/model/entity1/sub/sub_component1"),
			},
			{
				cType3,
				"entity1/sub/sub_component2",
				filepath.FromSlash("/a/model/entity1/sub/sub_component2"),
			},
		}, components)
	})

	t.Run("lists components in entity1_sub_sub2", func(t *testing.T) {
		components, err := model.ListEntityComponents(Entity{"entity1/sub/sub2"})
		assert.NoError(t, err)
		assert.Equal(t, []Component{}, components)
	})
}

func TestModelListEntityComponentsByTypeName(t *testing.T) {
	fs := afero.NewMemMapFs()
	cType1 := ComponentType{
		Name:          "foo",
		SchemaVersion: "bar",
	}
	cType2 := ComponentType{
		Name:          "baz",
		SchemaVersion: "boz",
	}
	model := NewOnDiskModel("/a/model", &Manifest{Entries: []ManifestEntry{
		{Path: "entity1/component1", ComponentType: cType1},
		{Path: "entity1/component2", ComponentType: cType2},
	}}, Metadata{})
	model.FS = fs

	err := fs.MkdirAll(`/a/model`, perms.LocalDirPerm)
	assert.NoError(t, err)

	err = fs.MkdirAll(`/a/model/entity1`, perms.LocalDirPerm)
	assert.NoError(t, err)

	file, err := fs.Create(`/a/model/entity1/component1`)
	assert.NoError(t, err)
	file.Close()

	file, err = fs.Create(`/a/model/entity1/component2`)
	assert.NoError(t, err)
	file.Close()

	t.Run("lists components in entity1", func(t *testing.T) {
		components, err := model.ListEntityComponentsByTypeName(Entity{"entity1/"}, "foo")
		assert.NoError(t, err)
		assert.Equal(t, []Component{
			{
				cType1,
				"entity1/component1",
				filepath.FromSlash("/a/model/entity1/component1"),
			},
		}, components)
	})
}

func TestModel_ResolveComponentByManifestPattern(t *testing.T) {
	fs := afero.NewMemMapFs()
	cType := ComponentType{Name: "foo", SchemaVersion: "1"}
	model := NewOnDiskModel("/base", &Manifest{Entries: []ManifestEntry{
		{Path: "entity/*/component", ComponentType: cType},
	}}, Metadata{})
	model.FS = fs

	// Create a file that matches the manifest pattern
	_ = fs.MkdirAll("/base/entity/a", perms.LocalDirPerm)
	file, err := fs.Create("/base/entity/a/component")
	assert.NoError(t, err)
	file.Close()

	// Should match manifest entry
	comp, err := model.ResolveComponentByManifestPattern("entity/a/component")
	assert.NoError(t, err)
	assert.Equal(t, cType, comp.Type)
	assert.Equal(t, "entity/*/component", comp.RelativePath)
	assert.Equal(t, filepath.FromSlash("/base/entity/a/component"), comp.AbsolutePath)

	// File exists but not in manifest
	_ = fs.MkdirAll("/base/other", perms.LocalDirPerm)
	file, err = fs.Create("/base/other/file")
	assert.NoError(t, err)
	file.Close()

	comp, err = model.ResolveComponentByManifestPattern("other/file")
	assert.Error(t, err)
	assert.Equal(t, unknownComponentType, comp.Type)
	assert.Equal(t, "other/file", comp.RelativePath)
	assert.Equal(t, "", comp.AbsolutePath)

	// File does not exist
	comp, err = model.ResolveComponentByManifestPattern("missing/file")
	assert.Error(t, err)
	assert.Equal(t, unknownComponentType, comp.Type)
	assert.Equal(t, "missing/file", comp.RelativePath)
	assert.Equal(t, "", comp.AbsolutePath)
}

func TestResolveComponentByManifestPattern(t *testing.T) {
	cType := ComponentType{Name: "type", SchemaVersion: "1"}

	t.Run("returns pending error for matching pattern", func(t *testing.T) {
		model := NewOnDiskModel("/base", &Manifest{Entries: []ManifestEntry{
			{Path: "a/*.csv", ComponentType: cType, Pending: true},
		}}, Metadata{})

		component, err := model.ResolveComponentByManifestPattern("a/pending.csv")

		require.ErrorIs(t, err, ErrComponentPending)
		require.Equal(t, Component{}, component)
	})

	t.Run("returns pending error after path migration", func(t *testing.T) {
		model := NewOnDiskModel("/base", &Manifest{Entries: []ManifestEntry{
			{Path: "tool/old/0/output/*.csv", ComponentType: cType, Pending: true},
		}}, Metadata{})
		model.AddPathMigrations([]PathMigration{
			&ToolNameMigration{
				From: "old",
				To:   "new",
				Ver:  semver.SemVer{Major: 1},
			},
		})

		component, err := model.ResolveComponentByManifestPattern("tool/new/0/output/pending.csv")

		require.ErrorIs(t, err, ErrComponentPending)
		require.Equal(t, Component{}, component)
	})
}

func TestModel_ResolveComponentByManifestPattern_NameMigrationWithGlob(t *testing.T) {
	fs := afero.NewMemMapFs()
	model := NewOnDiskModel("/base", &Manifest{Entries: []ManifestEntry{
		{
			Path: "tool/streamline-cli/0/output/sources-capture-periodic_sampling*",
			ComponentType: ComponentType{
				Name:          "sl-collect-source-line-attribution",
				SchemaVersion: "1.0",
			},
		},
	}}, Metadata{})
	model.FS = fs

	model.AddPathMigrations([]PathMigration{
		&ToolNameMigration{
			From: "streamline-cli",
			To:   "neoprof",
			Ver:  semver.SemVer{Major: 1, Minor: 1, Patch: 0},
		},
	})

	comp, err := model.ResolveComponentByManifestPattern("tool/neoprof/0/output/sources-capture-periodic_sampling-foo-bar.csv")
	assert.NoError(t, err)
	assert.Equal(t, model.manifest.Entries[0].ComponentType, comp.Type)
	assert.Equal(t, model.manifest.Entries[0].Path, comp.RelativePath)
	assert.Equal(t, filepath.FromSlash("/base/tool/streamline-cli/0/output/sources-capture-periodic_sampling-foo-bar.csv"), comp.AbsolutePath)
}

func TestModel_ResolveComponentByManifestPattern_SuffixMigrationWithGlob(t *testing.T) {
	fs := afero.NewMemMapFs()
	model := NewOnDiskModel("/base", &Manifest{Entries: []ManifestEntry{
		{
			Path: "tool/streamline-cli/0/output/sources-capture-periodic_sampling*",
			ComponentType: ComponentType{
				Name:          "sl-collect-source-line-attribution",
				SchemaVersion: "1.0",
			},
		},
	}}, Metadata{})
	model.FS = fs

	model.AddPathMigrations([]PathMigration{
		&ToolPathSuffixMigration{
			Type:      "suffixRewrite",
			From:      "streamline-cli",
			Ver:       semver.SemVer{Major: 1, Minor: 1, Patch: 0},
			OldSuffix: "output/",
			NewSuffix: "output/deep/path/",
		},
	})

	comp, err := model.ResolveComponentByManifestPattern("tool/streamline-cli/0/output/deep/path/sources-capture-periodic_sampling-foo-bar.csv")
	assert.NoError(t, err)
	assert.Equal(t, model.manifest.Entries[0].ComponentType, comp.Type)
	assert.Equal(t, model.manifest.Entries[0].Path, comp.RelativePath)
	assert.Equal(t, filepath.FromSlash("/base/tool/streamline-cli/0/output/sources-capture-periodic_sampling-foo-bar.csv"), comp.AbsolutePath)
}

func TestModel_ResolveComponentByManifestPattern_ErrorPaths(t *testing.T) {
	fs := afero.NewMemMapFs()
	cType := ComponentType{Name: "foo", SchemaVersion: "1"}
	// Bad pattern to force doublestar.Match error
	model := NewOnDiskModel("/base", &Manifest{Entries: []ManifestEntry{
		{Path: "[invalid[", ComponentType: cType},
	}}, Metadata{})
	model.FS = fs

	_, err := model.ResolveComponentByManifestPattern("entity/a/component")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "pattern match error")

	// File exists but is a directory, not in manifest
	_ = fs.MkdirAll("/base/dirfile", perms.LocalDirPerm)
	model = NewOnDiskModel("/base", &Manifest{Entries: []ManifestEntry{}}, Metadata{})
	model.FS = fs

	comp, err := model.ResolveComponentByManifestPattern("dirfile")
	assert.Error(t, err)
	assert.Equal(t, unknownComponentType, comp.Type)
	assert.Equal(t, "dirfile", comp.RelativePath)
	assert.Equal(t, "", comp.AbsolutePath)
}

func TestResolveComponentByPatternExpectTypeV_Success(t *testing.T) {
	// In-memory FS with one CSV that should match the manifest pattern.
	mem := afero.NewMemMapFs()
	base := "/runs/abc"
	outDir := filepath.Join(base, "tool/neoprof/0/output")
	if err := mem.MkdirAll(outDir, perms.LocalDirPerm); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Concrete file on disk:
	fileRel := "tool/neoprof/0/output/sources-capture-periodic_sampling-foo.csv"
	if err := afero.WriteFile(mem, filepath.Join(base, fileRel), []byte("dummy"), perms.LocalFilePerm); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Manifest entry with a wildcard pattern; note schema "1.0".
	pattern := "tool/neoprof/0/output/sources-capture-periodic_sampling*"
	manifest := Manifest{
		Entries: []ManifestEntry{
			{
				Path: pattern,
				ComponentType: ComponentType{
					Name:          "sl-collect-source-line-attribution",
					SchemaVersion: "1.0",
				},
			},
		},
	}

	m := NewOnDiskModel(base, &manifest, Metadata{})
	m.FS = mem

	// Expect type + version in [1.0.0, 2.0.0)
	c, sv, err := m.ResolveComponentByPatternExpectTypeV(
		fileRel,
		"sl-collect-source-line-attribution",
		semver.VersionRange{
			Min: &semver.SemVer{Major: 1, Minor: 0, Patch: 0},
			Max: &semver.SemVer{Major: 2, Minor: 0, Patch: 0},
		},
	)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	// IMPORTANT: When resolving by pattern, RelativePath should be the manifest ENTRY PATH (the pattern),
	// not the concrete file path.
	if c.RelativePath != pattern {
		t.Fatalf("wrong component relative path: got %q want %q", c.RelativePath, pattern)
	}

	// Absolute path should still point to the concrete file on disk.
	wantAbs := filepath.Join(base, fileRel)
	if c.AbsolutePath != wantAbs {
		t.Fatalf("wrong absolute path: got %q want %q", c.AbsolutePath, wantAbs)
	}

	// Parsed schema version from "1.0" -> 1.0.0
	if sv.Major != 1 || sv.Minor != 0 || sv.Patch != 0 {
		t.Fatalf("parsed schema wrong: got %v want 1.0.0", sv)
	}
}

func TestResolveComponentByPatternExpectTypeV_WrongType(t *testing.T) {
	mem := afero.NewMemMapFs()
	base := "/runs/abc"
	_ = mem.MkdirAll(filepath.Join(base, "tool/x/0/output"), perms.LocalDirPerm)
	fileRel := "tool/x/0/output/a.csv"
	_ = afero.WriteFile(mem, filepath.Join(base, fileRel), []byte{0}, perms.LocalFilePerm)

	manifest := Manifest{
		Entries: []ManifestEntry{
			{
				Path: "tool/x/0/output/*.csv",
				ComponentType: ComponentType{
					Name:          "type-A",
					SchemaVersion: "1.0",
				},
			},
		},
	}
	m := NewOnDiskModel(base, &manifest, Metadata{})
	m.FS = mem

	_, _, err := m.ResolveComponentByPatternExpectTypeV(fileRel, "type-B",
		semver.VersionRange{Min: &semver.SemVer{Major: 1, Minor: 0, Patch: 0}, Max: &semver.SemVer{Major: 2, Minor: 0, Patch: 0}},
	)

	wantErr := fmt.Sprintf(
		"wrong component type for %q: got %s@%s; expected %s",
		fileRel,
		manifest.Entries[0].ComponentType.Name,
		manifest.Entries[0].ComponentType.SchemaVersion,
		"type-B",
	)

	// this asserts both that err != nil and err.Error() == wantErr
	require.EqualError(
		t,
		err,
		wantErr,
		"expected that resolving %s with type-B would error with the right message",
		manifest.Entries[0].Path,
	)
}

func TestResolveComponentByPatternExpectTypeV_VersionOutOfRange(t *testing.T) {
	mem := afero.NewMemMapFs()
	base := "/runs/abc"
	_ = mem.MkdirAll(filepath.Join(base, "tool/y/0/output"), perms.LocalDirPerm)
	fileRel := "tool/y/0/output/a.csv"
	_ = afero.WriteFile(mem, filepath.Join(base, fileRel), []byte{0}, perms.LocalFilePerm)

	// Entry says schema 1.1, but we will require >=2.0
	manifest := Manifest{
		Entries: []ManifestEntry{
			{
				Path: "tool/y/0/output/*.csv",
				ComponentType: ComponentType{
					Name:          "type-Y",
					SchemaVersion: "1.1",
				},
			},
		},
	}
	m := NewOnDiskModel(base, &manifest, Metadata{})
	m.FS = mem

	_, _, err := m.ResolveComponentByPatternExpectTypeV(fileRel, "type-Y",
		semver.VersionRange{Min: &semver.SemVer{Major: 2, Minor: 0, Patch: 0}}, // >= 2.0.0
	)

	wantErr := fmt.Sprintf(
		"version constraint not satisfied for %q: have %s@%s, require >= %s",
		fileRel,
		manifest.Entries[0].ComponentType.Name,
		"1.1.0",
		"2.0.0",
	)

	// Assert both that err != nil and that err.Error() equals our expected string
	require.EqualError(
		t,
		err,
		wantErr,
		"expected a version‐constraint error for %s with >=2.0.0", fileRel,
	)
}

func TestResolveComponentByPatternExpectTypeV_OverlappingPatternsChoosesExpectedType(t *testing.T) {
	base := "/runs/abc"
	fileRel := "tool/neoprof/0/output/disassembly-capture-periodic_sampling"
	pattern := "tool/neoprof/0/output/*"
	manifest := Manifest{
		Entries: []ManifestEntry{
			{
				Path: pattern,
				ComponentType: ComponentType{
					Name:          "sl-collect-source-line-attribution",
					SchemaVersion: "1.0",
				},
			},
			{
				Path: pattern,
				ComponentType: ComponentType{
					Name:          "disassembly_capture_samples",
					SchemaVersion: "1.1",
				},
			},
			{
				Path: pattern,
				ComponentType: ComponentType{
					Name:          "disassembly_capture_metrics",
					SchemaVersion: "1.0",
				},
			},
		},
	}
	m := NewOnDiskModel(base, &manifest, Metadata{})

	c, sv, err := m.ResolveComponentByPatternExpectTypeV(
		fileRel,
		"disassembly_capture_samples",
		semver.VersionRange{
			Min: &semver.SemVer{Major: 1, Minor: 0, Patch: 0},
			Max: &semver.SemVer{Major: 1, Minor: 2, Patch: 0},
		},
	)
	require.NoError(t, err)

	require.Equal(t, "disassembly_capture_samples", c.Type.Name)
	require.Equal(t, "1.1", c.Type.SchemaVersion)
	require.Equal(t, pattern, c.RelativePath)
	require.Equal(t, filepath.Join(base, fileRel), c.AbsolutePath)
	require.Equal(t, semver.SemVer{Major: 1, Minor: 1, Patch: 0}, sv)
}
