// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
)

func TestRunBuilder(t *testing.T) {
	t.Run("empty initially", func(t *testing.T) {
		builder := RunBuilder{
			runID:      RunID{"aaa"},
			basePath:   "/foo/bar",
			runPath:    "/foo/bar/aaa",
			entities:   []cdf.Entity{},
			components: []builderComponent{},
		}
		assert.Equal(t, false, builder.ContainsEntity(""))
		assert.Equal(t, 0, builder.EntityCount())
	})

	t.Run("empty after adding empty entity", func(t *testing.T) {
		builder := RunBuilder{
			runID:      RunID{"aaa"},
			basePath:   "/foo/bar",
			runPath:    "/foo/bar/aaa",
			entities:   []cdf.Entity{},
			components: []builderComponent{},
		}
		builder.AddEntity("")
		assert.Equal(t, false, builder.ContainsEntity(""))
		assert.Equal(t, 0, builder.EntityCount())
	})

	t.Run("run builder contains entities that are added", func(t *testing.T) {
		builder := RunBuilder{
			runID:      RunID{"aaa"},
			basePath:   "/foo/bar",
			runPath:    "/foo/bar/aaa",
			entities:   []cdf.Entity{},
			components: []builderComponent{},
		}

		builder.AddEntity("/what/does/this/do/")
		builder.AddEntity("foo")
		builder.AddEntity("foo/bar")

		assert.Equal(t, true, builder.ContainsEntity("/what/does/this/do"))
		assert.Equal(t, true, builder.ContainsEntity("what/does/this/do"))
		assert.Equal(t, true, builder.ContainsEntity("what/does/this/do/"))
		assert.Equal(t, true, builder.ContainsEntity("/what/does/this/do/"))
		assert.Equal(t, true, builder.ContainsEntity("what"))
		assert.Equal(t, true, builder.ContainsEntity("what/does"))
		assert.Equal(t, true, builder.ContainsEntity("what/does/"))
		assert.Equal(t, true, builder.ContainsEntity("what/does/this"))
		assert.Equal(t, true, builder.ContainsEntity("what/does/this/do"))

		assert.Equal(t, true, builder.ContainsEntity("foo"))
		assert.Equal(t, true, builder.ContainsEntity("/foo"))
		assert.Equal(t, true, builder.ContainsEntity("/foo/"))
		assert.Equal(t, true, builder.ContainsEntity("foo/"))
		assert.Equal(t, true, builder.ContainsEntity("foo/bar"))
		assert.Equal(t, true, builder.ContainsEntity("/foo/bar"))
	})

	t.Run("run builder doesn't care what type of path separator you use to add entities", func(t *testing.T) {
		builder := RunBuilder{
			runID:      RunID{"aaa"},
			basePath:   "/foo/bar",
			runPath:    "/foo/bar/aaa",
			entities:   []cdf.Entity{},
			components: []builderComponent{},
		}

		builder.AddEntity("\\back\\slashes")

		assert.Equal(t, true, builder.ContainsEntity("back"))
		assert.Equal(t, true, builder.ContainsEntity("back/slashes"))
		assert.Equal(t, true, builder.ContainsEntity("back\\slashes"))
	})

	t.Run("run builder doesn't care what type of path separator you use to add components", func(t *testing.T) {
		builder := RunBuilder{
			runID:      RunID{"aaa"},
			basePath:   "/foo/bar",
			runPath:    "/foo/bar/aaa",
			entities:   []cdf.Entity{},
			components: []builderComponent{},
		}

		assert.Equal(t, filepath.FromSlash("/foo/bar/aaa/a/b/foo.something"), builder.AddComponent(
			cdf.ComponentType{Name: "aaa", SchemaVersion: "bbb"},
			"\\a\\b\\foo.something",
		))

		assert.Equal(t, true, builder.ContainsEntity("a"))
		assert.Equal(t, true, builder.ContainsEntity("a/b"))
		assert.Equal(t, true, builder.ContainsEntity("a\\b"))
	})

	t.Run("consecutive slashes are collapsed", func(t *testing.T) {
		builder := RunBuilder{
			runID:      RunID{"aaa"},
			basePath:   "/foo/bar",
			runPath:    "/foo/bar/aaa",
			entities:   []cdf.Entity{},
			components: []builderComponent{},
		}

		assert.Equal(t, filepath.FromSlash("/foo/bar/aaa/a/b/foo.something"), builder.AddComponent(
			cdf.ComponentType{Name: "aaa", SchemaVersion: "bbb"},
			"\\\\a\\b\\////foo.something",
		))

		assert.Equal(t, true, builder.ContainsEntity("a"))
		assert.Equal(t, true, builder.ContainsEntity("a/b"))
		assert.Equal(t, true, builder.ContainsEntity("a\\b"))
	})

	t.Run("runuilder contains entities for all entities leading up to an added component", func(t *testing.T) {
		builder := RunBuilder{
			runID:      RunID{"aaa"},
			basePath:   "/foo/bar",
			runPath:    "/foo/bar/aaa",
			entities:   []cdf.Entity{},
			components: []builderComponent{},
		}

		assert.Equal(t, filepath.FromSlash("/foo/bar/aaa/foo.something"), builder.AddComponent(
			cdf.ComponentType{Name: "aaa", SchemaVersion: "bbb"},
			"foo.something",
		))

		assert.Equal(t, filepath.FromSlash("/foo/bar/aaa/some/foo.something"), builder.AddComponent(
			cdf.ComponentType{Name: "aaa", SchemaVersion: "bbb"},
			"some/foo.something",
		))

		assert.Equal(t, filepath.FromSlash("/foo/bar/aaa/aaa/foo.something"), builder.AddComponent(
			cdf.ComponentType{Name: "aaa", SchemaVersion: "bbb"},
			"/aaa/foo.something",
		))

		assert.Equal(t, filepath.FromSlash("/foo/bar/aaa/foo/bar/foo.something"), builder.AddComponent(
			cdf.ComponentType{Name: "aaa", SchemaVersion: "bbb"},
			"foo/bar/foo.something",
		))

		// todo not sure what to do with components that are directories
		//      currently we are allowing something to be both a component and an entity, which feels very weird
		assert.Equal(t, filepath.FromSlash("/foo/bar/aaa/foo/bar"), builder.AddComponent(
			cdf.ComponentType{Name: "aaa", SchemaVersion: "bbb"},
			"/foo/bar/",
		))

		assert.Equal(t, true, builder.ContainsEntity("some"))
		assert.Equal(t, true, builder.ContainsEntity("aaa"))
		assert.Equal(t, true, builder.ContainsEntity("foo"))
		assert.Equal(t, true, builder.ContainsEntity("foo/bar"))
	})
	t.Run("new run builders are constructed with correct paths", func(t *testing.T) {
		fakePath := "fake/path"
		runCollection := RunCollection{primaryPath: fakePath}
		builder, err := runCollection.RunBuilder()
		assert.Nil(t, err)
		assert.NotNil(t, builder.runID.Value)
		assert.Equal(t, builder.basePath, fakePath)
		assert.Equal(t, builder.runPath, filepath.Join(fakePath, builder.runID.Value))
	})
}

func TestRunBuilderManifest(t *testing.T) {
	t.Run("run builder manifest lookup succeeds on things that have been added", func(t *testing.T) {
		builder := RunBuilder{
			runID:      RunID{"aaa"},
			basePath:   "/foo/bar",
			runPath:    "/foo/bar/aaa",
			entities:   []cdf.Entity{},
			components: []builderComponent{},
		}

		test1ComponentType := cdf.ComponentType{Name: "test1", SchemaVersion: "test1_ver"}
		assert.Equal(t, filepath.FromSlash("/foo/bar/aaa/foo.something"), builder.AddComponent(
			test1ComponentType,
			"foo.something",
		))

		test2ComponentType := cdf.ComponentType{Name: "test2", SchemaVersion: "test2_ver"}
		assert.Equal(t, filepath.FromSlash("/foo/bar/aaa/some/foo.something"), builder.AddComponent(
			test2ComponentType,
			"some/foo.something",
		))

		test3ComponentType := cdf.ComponentType{Name: "test3", SchemaVersion: "test3_ver"}
		assert.Equal(t, filepath.FromSlash("/foo/bar/aaa/aaa/foo.something"), builder.AddComponent(
			test3ComponentType,
			"/aaa/foo.something",
		))

		test4ComponentType := cdf.ComponentType{Name: "test4", SchemaVersion: "test4_ver"}
		assert.Equal(t, filepath.FromSlash("/foo/bar/aaa/foo/bar/foo.something"), builder.AddComponent(
			test4ComponentType,
			"foo/bar/foo.something",
		))

		// todo not sure what to do with components that are directories
		//      currently we are allowing something to be both a component and an entity, which feels very weird
		test5ComponentType := cdf.ComponentType{Name: "test5", SchemaVersion: "test5_ver"}
		assert.Equal(t, filepath.FromSlash("/foo/bar/aaa/foo/bar"), builder.AddComponent(
			test5ComponentType,
			"/foo/bar/",
		))

		manifestPtr := builder.buildManifest()
		assert.NotNil(t, manifestPtr)
		manifest := *manifestPtr

		assert.Equal(t, &cdf.ManifestEntry{Path: "foo.something", ComponentType: test1ComponentType}, manifest.Lookup("foo.something"))

		assert.Equal(t, &cdf.ManifestEntry{Path: "some/foo.something", ComponentType: test2ComponentType}, manifest.Lookup("some/foo.something"))
		assert.Equal(t, &cdf.ManifestEntry{Path: "some/foo.something", ComponentType: test2ComponentType}, manifest.Lookup("some///////foo.something"))
		assert.Equal(t, &cdf.ManifestEntry{Path: "some/foo.something", ComponentType: test2ComponentType}, manifest.Lookup("\\some/foo.something"))
		assert.Equal(t, &cdf.ManifestEntry{Path: "some/foo.something", ComponentType: test2ComponentType}, manifest.Lookup("some/foo.something/"))
		assert.Nil(t, manifest.Lookup("/"))
		assert.Nil(t, manifest.Lookup(""))

		assert.Equal(t, &cdf.ManifestEntry{Path: "aaa/foo.something", ComponentType: test3ComponentType}, manifest.Lookup("aaa/foo.something"))

		assert.Equal(t, &cdf.ManifestEntry{Path: "foo/bar", ComponentType: test5ComponentType}, manifest.Lookup("foo/bar"))
	})

	t.Run("manifest includes pending component state", func(t *testing.T) {
		builder := RunBuilder{
			runID:      RunID{"aaa"},
			basePath:   "/foo/bar",
			runPath:    "/foo/bar/aaa",
			entities:   []cdf.Entity{},
			components: []builderComponent{},
		}

		componentType := cdf.ComponentType{Name: "pending", SchemaVersion: "v1"}
		builder.AddPendingComponent(componentType, "some/foo.something")

		assert.Equal(t, &cdf.ManifestEntry{
			Path:          "some/foo.something",
			ComponentType: componentType,
			Pending:       true,
		}, builder.buildManifest().Lookup("some/foo.something"))
	})
}

func TestRunBuilderComponentUpdates(t *testing.T) {
	t.Run("clear pending and remove component normalize relative paths", func(t *testing.T) {
		builder := RunBuilder{
			runID:      RunID{"aaa"},
			basePath:   "/foo/bar",
			runPath:    "/foo/bar/aaa",
			entities:   []cdf.Entity{},
			components: []builderComponent{},
		}

		componentType := cdf.ComponentType{Name: "test", SchemaVersion: "v1"}
		builder.AddPendingComponent(componentType, "a/b/foo.something")
		builder.ClearPending("\\a\\b\\foo.something")
		assert.False(t, builder.buildManifest().Lookup("a/b/foo.something").Pending)

		builder.RemoveComponent("/a/b/foo.something/")
		assert.Equal(t, 0, builder.ComponentCount())
		assert.Nil(t, builder.buildManifest().Lookup("a/b/foo.something"))
		builder.ClearPending("a/b/foo.something")
		builder.RemoveComponent("a/b/foo.something")
		assert.Equal(t, 0, builder.ComponentCount())
	})

	t.Run("component pending check normalizes paths", func(t *testing.T) {
		builder := RunBuilder{
			runID:      RunID{"aaa"},
			basePath:   "/foo/bar",
			runPath:    "/foo/bar/aaa",
			entities:   []cdf.Entity{},
			components: []builderComponent{},
		}

		componentType := cdf.ComponentType{Name: "test", SchemaVersion: "v1"}
		builder.AddPendingComponent(componentType, "pending/foo.something")
		builder.AddComponent(componentType, "complete/foo.something")

		assert.True(t, builder.IsComponentPending("\\pending\\foo.something"))
		assert.False(t, builder.IsComponentPending("/complete/foo.something/"))
		assert.False(t, builder.IsComponentPending("missing/foo.something"))
	})
}

func TestGenerateRunName(t *testing.T) {
	tcs := []struct {
		name       string
		recipeName string
		runID      string
		want       string
	}{
		{
			name:       "normal recipe and id",
			recipeName: "instruction_mix",
			runID:      "9873298472937",
			want:       "instruction_mix_937",
		},
		{
			name:       "long recipe",
			recipeName: "my_really_long_custom_recipe_name",
			runID:      "89439872497924",
			want:       "my_really_long_c_924",
		},
		{
			name:       "empty inputs",
			recipeName: "",
			runID:      "",
			want:       "_",
		},
		{
			name:       "short id",
			recipeName: "cpu_microarchitecture",
			runID:      "7",
			want:       "cpu_microarchite_7",
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			b := &RunBuilder{
				runID: RunID{Value: tc.runID},
			}
			got := b.GenerateRunName(tc.recipeName)
			if got != tc.want {
				t.Fatalf("GenerateRunName(%q, %q) = %q; want %q",
					tc.recipeName, tc.runID, got, tc.want)
			}
		})
	}
}
