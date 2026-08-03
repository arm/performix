// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package cdf

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf/semver"
)

// mockModelView provides a testify-based mock for ModelView.
type mockModelView struct {
	mock.Mock
}

// ResolveComponent returns the mocked component and error for a path.
func (m *mockModelView) ResolveComponent(componentPath string) (Component, error) {
	args := m.Called(componentPath)
	return args.Get(0).(Component), args.Error(1)
}

// ResolveComponentExpectType returns the mocked component and error for a path.
func (m *mockModelView) ResolveComponentExpectType(
	componentPath string,
	expectedComponentType ComponentType,
) (Component, error) {
	args := m.Called(componentPath, expectedComponentType)
	return args.Get(0).(Component), args.Error(1)
}

// ResolveComponentExpectTypes returns the mocked component and error for a path.
func (m *mockModelView) ResolveComponentExpectTypes(
	componentPath string,
	expectedComponentTypes ...ComponentType,
) (Component, error) {
	args := m.Called(componentPath, expectedComponentTypes)
	return args.Get(0).(Component), args.Error(1)
}

// ResolveComponentByManifestPattern returns the mocked component and error for a path.
func (m *mockModelView) ResolveComponentByManifestPattern(componentPath string) (Component, error) {
	args := m.Called(componentPath)
	return args.Get(0).(Component), args.Error(1)
}

// ResolveComponentExpectTypeV returns the mocked component, version, and error.
func (m *mockModelView) ResolveComponentExpectTypeV(
	componentPath string,
	typeName string,
	vr semver.VersionRange,
) (Component, semver.SemVer, error) {
	args := m.Called(componentPath, typeName, vr)
	return args.Get(0).(Component), args.Get(1).(semver.SemVer), args.Error(2)
}

// ResolveComponentByPatternExpectTypeV returns the mocked component, version, and error.
func (m *mockModelView) ResolveComponentByPatternExpectTypeV(
	componentPath string,
	expectedTypeName string,
	vr semver.VersionRange,
) (Component, semver.SemVer, error) {
	args := m.Called(componentPath, expectedTypeName, vr)
	return args.Get(0).(Component), args.Get(1).(semver.SemVer), args.Error(2)
}

// FindEntities returns the mocked entities and error for a glob.
func (m *mockModelView) FindEntities(glob string) ([]Entity, error) {
	args := m.Called(glob)
	return args.Get(0).([]Entity), args.Error(1)
}

// FindComponents returns the mocked components and error for a glob.
func (m *mockModelView) FindComponents(glob string) ([]Component, error) {
	args := m.Called(glob)
	return args.Get(0).([]Component), args.Error(1)
}

// ListEntityComponents returns the mocked components and error for an entity.
func (m *mockModelView) ListEntityComponents(entity Entity) ([]Component, error) {
	args := m.Called(entity)
	return args.Get(0).([]Component), args.Error(1)
}

// ListEntityComponentsByTypeName returns the mocked components and error for an entity.
func (m *mockModelView) ListEntityComponentsByTypeName(entity Entity, componentTypeName string) ([]Component, error) {
	args := m.Called(entity, componentTypeName)
	return args.Get(0).([]Component), args.Error(1)
}

// ListEntityComponentsMatching returns the mocked components and error for an entity.
func (m *mockModelView) ListEntityComponentsMatching(
	entity Entity,
	pred func(*Component) bool,
) ([]Component, error) {
	args := m.Called(entity, pred)
	return args.Get(0).([]Component), args.Error(1)
}

// Metadata returns the mocked metadata for this view.
func (m *mockModelView) Metadata() Metadata {
	args := m.Called()
	return args.Get(0).(Metadata)
}

// BasePath returns the mocked base path for this view.
func (m *mockModelView) BasePath() string {
	args := m.Called()
	return args.String(0)
}

func TestOverlayModel(t *testing.T) {
	t.Run("resolve prefers overlay", func(t *testing.T) {
		baseDir := t.TempDir()
		overlayDir := t.TempDir()

		relPath := filepath.ToSlash(filepath.Join("tool", "foo", "0", "file.txt"))
		require.NoError(t, os.MkdirAll(filepath.Join(baseDir, filepath.Dir(relPath)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(baseDir, relPath), []byte("base"), 0o644))
		require.NoError(t, os.MkdirAll(filepath.Join(overlayDir, filepath.Dir(relPath)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(overlayDir, relPath), []byte("overlay"), 0o644))

		baseManifest := Manifest{
			Entries: []ManifestEntry{
				{Path: relPath, ComponentType: ComponentType{Name: "base", SchemaVersion: "1.0"}},
			},
		}
		overlayManifest := Manifest{
			Entries: []ManifestEntry{
				{Path: relPath, ComponentType: ComponentType{Name: "overlay", SchemaVersion: "1.0"}},
			},
		}

		base := NewOnDiskModel(baseDir, &baseManifest, Metadata{})
		overlay := NewOnDiskModel(overlayDir, &overlayManifest, Metadata{})
		model, err := NewOverlayModel(base, overlay)
		require.NoError(t, err)

		component, err := model.ResolveComponent(relPath)
		require.NoError(t, err)
		assert.Equal(t, "overlay", component.Type.Name)
		assert.Equal(t, filepath.Join(overlayDir, relPath), component.AbsolutePath)
	})

	t.Run("resolve falls back to base when overlay missing", func(t *testing.T) {
		baseDir := t.TempDir()
		overlayDir := t.TempDir()

		relPath := filepath.ToSlash(filepath.Join("tool", "foo", "0", "file.txt"))
		require.NoError(t, os.MkdirAll(filepath.Join(baseDir, filepath.Dir(relPath)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(baseDir, relPath), []byte("base"), 0o644))

		baseManifest := Manifest{
			Entries: []ManifestEntry{
				{Path: relPath, ComponentType: ComponentType{Name: "base", SchemaVersion: "1.0"}},
			},
		}

		base := NewOnDiskModel(baseDir, &baseManifest, Metadata{})
		overlay := NewOnDiskModel(overlayDir, &Manifest{}, Metadata{})
		model, err := NewOverlayModel(base, overlay)
		require.NoError(t, err)

		component, err := model.ResolveComponent(relPath)
		require.NoError(t, err)
		assert.Equal(t, "base", component.Type.Name)
		assert.Equal(t, filepath.Join(baseDir, relPath), component.AbsolutePath)
	})

	t.Run("pattern resolves prefer overlay", func(t *testing.T) {
		baseDir := t.TempDir()
		overlayDir := t.TempDir()

		pattern := filepath.ToSlash(filepath.Join("tool", "foo", "*", "file.txt"))
		requestPath := filepath.ToSlash(filepath.Join("tool", "foo", "0", "file.txt"))

		baseManifest := Manifest{
			Entries: []ManifestEntry{
				{Path: pattern, ComponentType: ComponentType{Name: "base", SchemaVersion: "1.0"}},
			},
		}
		overlayManifest := Manifest{
			Entries: []ManifestEntry{
				{Path: pattern, ComponentType: ComponentType{Name: "overlay", SchemaVersion: "1.0"}},
			},
		}

		base := NewOnDiskModel(baseDir, &baseManifest, Metadata{})
		overlay := NewOnDiskModel(overlayDir, &overlayManifest, Metadata{})
		model, err := NewOverlayModel(base, overlay)
		require.NoError(t, err)

		component, err := model.ResolveComponentByManifestPattern(requestPath)
		require.NoError(t, err)
		assert.Equal(t, "overlay", component.Type.Name)
	})

	t.Run("list entity components is de-duplicated with overlay precedence", func(t *testing.T) {
		baseDir := t.TempDir()
		overlayDir := t.TempDir()
		entity := filepath.ToSlash(filepath.Join("tool", "foo", "0"))

		baseFile := filepath.Join(baseDir, entity, "a.txt")
		overlayFile := filepath.Join(overlayDir, entity, "a.txt")
		overlayExtra := filepath.Join(overlayDir, entity, "b.txt")

		require.NoError(t, os.MkdirAll(filepath.Dir(baseFile), 0o755))
		require.NoError(t, os.MkdirAll(filepath.Dir(overlayFile), 0o755))
		require.NoError(t, os.WriteFile(baseFile, []byte("base"), 0o644))
		require.NoError(t, os.WriteFile(overlayFile, []byte("overlay"), 0o644))
		require.NoError(t, os.WriteFile(overlayExtra, []byte("overlay"), 0o644))

		baseManifest := Manifest{
			Entries: []ManifestEntry{
				{Path: filepath.ToSlash(filepath.Join(entity, "a.txt")), ComponentType: ComponentType{Name: "base", SchemaVersion: "1.0"}},
			},
		}
		overlayManifest := Manifest{
			Entries: []ManifestEntry{
				{Path: filepath.ToSlash(filepath.Join(entity, "a.txt")), ComponentType: ComponentType{Name: "overlay", SchemaVersion: "1.0"}},
				{Path: filepath.ToSlash(filepath.Join(entity, "b.txt")), ComponentType: ComponentType{Name: "overlay", SchemaVersion: "1.0"}},
			},
		}

		base := NewOnDiskModel(baseDir, &baseManifest, Metadata{})
		overlay := NewOnDiskModel(overlayDir, &overlayManifest, Metadata{})
		model, err := NewOverlayModel(base, overlay)
		require.NoError(t, err)

		components, err := model.ListEntityComponents(Entity{RelativePath: entity})
		require.NoError(t, err)
		require.Len(t, components, 2)

		componentByPath := map[string]Component{}
		for _, component := range components {
			componentByPath[component.RelativePath] = component
		}

		overlayComponent, ok := componentByPath[filepath.ToSlash(filepath.Join(entity, "a.txt"))]
		require.True(t, ok)
		assert.Equal(t, "overlay", overlayComponent.Type.Name)
		assert.Equal(t, overlayFile, overlayComponent.AbsolutePath)
	})

	t.Run("find entities returns union of base and overlay", func(t *testing.T) {
		baseDir := t.TempDir()
		overlayDir := t.TempDir()

		baseEntity := filepath.Join(baseDir, "tool", "foo", "0")
		overlayEntity := filepath.Join(overlayDir, "tool", "foo", "0")
		overlayOnly := filepath.Join(overlayDir, "tool", "bar", "0")

		require.NoError(t, os.MkdirAll(baseEntity, 0o755))
		require.NoError(t, os.MkdirAll(overlayEntity, 0o755))
		require.NoError(t, os.MkdirAll(overlayOnly, 0o755))

		base := NewOnDiskModel(baseDir, &Manifest{}, Metadata{})
		overlay := NewOnDiskModel(overlayDir, &Manifest{}, Metadata{})
		model, err := NewOverlayModel(base, overlay)
		require.NoError(t, err)

		entities, err := model.FindEntities("tool/*/0")
		require.NoError(t, err)

		seen := map[string]struct{}{}
		for _, entity := range entities {
			seen[entity.RelativePath] = struct{}{}
		}

		assert.Contains(t, seen, filepath.ToSlash(filepath.Join("tool", "foo", "0")))
		assert.Contains(t, seen, filepath.ToSlash(filepath.Join("tool", "bar", "0")))
	})

	t.Run("resolve returns overlay error when not not-found", func(t *testing.T) {
		base := &mockModelView{}
		overlay := &mockModelView{}
		overlayErr := errors.New("boom")

		overlay.On("ResolveComponent", "x").Return(Component{}, overlayErr)

		model, err := NewOverlayModel(base, overlay)
		require.NoError(t, err)

		_, err = model.ResolveComponent("x")
		require.Error(t, err)
		assert.EqualError(t, err, "boom")

		base.AssertNotCalled(t, "ResolveComponent", mock.Anything)
		overlay.AssertExpectations(t)
	})

	t.Run("resolve falls back when overlay not-found", func(t *testing.T) {
		base := &mockModelView{}
		overlay := &mockModelView{}
		baseComponent := Component{
			Type:         ComponentType{Name: "base", SchemaVersion: "1.0"},
			RelativePath: "tool/base/0/a.txt",
			AbsolutePath: "/base/tool/base/0/a.txt",
		}

		overlay.On("ResolveComponent", "x").Return(Component{}, ErrComponentNotFound)
		base.On("ResolveComponent", "x").Return(baseComponent, nil)

		model, err := NewOverlayModel(base, overlay)
		require.NoError(t, err)

		component, err := model.ResolveComponent("x")
		require.NoError(t, err)
		assert.Equal(t, baseComponent, component)

		base.AssertExpectations(t)
		overlay.AssertExpectations(t)
	})

	t.Run("pattern resolve returns overlay error when not not-found", func(t *testing.T) {
		base := &mockModelView{}
		overlay := &mockModelView{}
		overlayErr := errors.New("boom")

		overlay.On("ResolveComponentByManifestPattern", "x").Return(Component{}, overlayErr)

		model, err := NewOverlayModel(base, overlay)
		require.NoError(t, err)

		_, err = model.ResolveComponentByManifestPattern("x")
		require.Error(t, err)
		assert.EqualError(t, err, "boom")

		base.AssertNotCalled(t, "ResolveComponentByManifestPattern", mock.Anything)
		overlay.AssertExpectations(t)
	})

	t.Run("pattern resolve falls back when overlay not-found", func(t *testing.T) {
		base := &mockModelView{}
		overlay := &mockModelView{}
		baseComponent := Component{
			Type:         ComponentType{Name: "base", SchemaVersion: "1.0"},
			RelativePath: "tool/base/0/a.txt",
			AbsolutePath: "/base/tool/base/0/a.txt",
		}

		overlay.On("ResolveComponentByManifestPattern", "x").Return(Component{}, ErrComponentNotFound)
		base.On("ResolveComponentByManifestPattern", "x").Return(baseComponent, nil)

		model, err := NewOverlayModel(base, overlay)
		require.NoError(t, err)

		component, err := model.ResolveComponentByManifestPattern("x")
		require.NoError(t, err)
		assert.Equal(t, baseComponent, component)

		base.AssertExpectations(t)
		overlay.AssertExpectations(t)
	})

	t.Run("find entities ignores overlay not-exist", func(t *testing.T) {
		base := &mockModelView{}
		overlay := &mockModelView{}
		expected := []Entity{{RelativePath: "tool/base/0"}}

		base.On("BasePath").Return("")
		overlay.On("BasePath").Return("")
		overlay.On("FindEntities", "g").Return([]Entity(nil), fs.ErrNotExist)
		base.On("FindEntities", "g").Return(expected, nil)

		model, err := NewOverlayModel(base, overlay)
		require.NoError(t, err)

		entities, err := model.FindEntities("g")
		require.NoError(t, err)
		assert.Equal(t, expected, entities)

		base.AssertExpectations(t)
		overlay.AssertExpectations(t)
	})

	t.Run("find components ignores overlay not-exist", func(t *testing.T) {
		base := &mockModelView{}
		overlay := &mockModelView{}
		expected := []Component{{RelativePath: "tool/base/0/a.txt"}}

		base.On("BasePath").Return("")
		overlay.On("BasePath").Return("")
		overlay.On("FindComponents", "g").Return([]Component(nil), fs.ErrNotExist)
		base.On("FindComponents", "g").Return(expected, nil)

		model, err := NewOverlayModel(base, overlay)
		require.NoError(t, err)

		components, err := model.FindComponents("g")
		require.NoError(t, err)
		assert.Equal(t, expected, components)

		base.AssertExpectations(t)
		overlay.AssertExpectations(t)
	})

	t.Run("find components returns overlay error for non-not-exist", func(t *testing.T) {
		base := &mockModelView{}
		overlay := &mockModelView{}
		overlayErr := errors.New("boom")

		overlay.On("FindComponents", "g").Return([]Component(nil), overlayErr)

		model, err := NewOverlayModel(base, overlay)
		require.NoError(t, err)

		_, err = model.FindComponents("g")
		require.Error(t, err)
		assert.EqualError(t, err, "boom")

		base.AssertNotCalled(t, "FindComponents", mock.Anything)
		overlay.AssertExpectations(t)
	})

	t.Run("find entities returns overlay error for non-not-exist", func(t *testing.T) {
		base := &mockModelView{}
		overlay := &mockModelView{}
		overlayErr := errors.New("boom")

		overlay.On("FindEntities", "g").Return([]Entity(nil), overlayErr)

		model, err := NewOverlayModel(base, overlay)
		require.NoError(t, err)

		_, err = model.FindEntities("g")
		require.Error(t, err)
		assert.EqualError(t, err, "boom")

		base.AssertNotCalled(t, "FindEntities", mock.Anything)
		overlay.AssertExpectations(t)
	})

	t.Run("list components ignores overlay not-exist", func(t *testing.T) {
		base := &mockModelView{}
		overlay := &mockModelView{}
		entity := Entity{RelativePath: "tool/base/0"}
		expected := []Component{{RelativePath: "tool/base/0/a.txt"}}

		base.On("BasePath").Return("")
		overlay.On("BasePath").Return("")
		overlay.On("ListEntityComponents", entity).Return([]Component(nil), fs.ErrNotExist)
		base.On("ListEntityComponents", entity).Return(expected, nil)

		model, err := NewOverlayModel(base, overlay)
		require.NoError(t, err)

		components, err := model.ListEntityComponents(entity)
		require.NoError(t, err)
		assert.Equal(t, expected, components)

		base.AssertExpectations(t)
		overlay.AssertExpectations(t)
	})

	t.Run("list components returns overlay error for non-not-exist", func(t *testing.T) {
		base := &mockModelView{}
		overlay := &mockModelView{}
		entity := Entity{RelativePath: "tool/base/0"}
		overlayErr := errors.New("boom")

		overlay.On("ListEntityComponents", entity).Return([]Component(nil), overlayErr)

		model, err := NewOverlayModel(base, overlay)
		require.NoError(t, err)

		_, err = model.ListEntityComponents(entity)
		require.Error(t, err)
		assert.EqualError(t, err, "boom")

		base.AssertNotCalled(t, "ListEntityComponents", mock.Anything)
		overlay.AssertExpectations(t)
	})

	t.Run("metadata returns overlay metadata", func(t *testing.T) {
		base := &mockModelView{}
		overlay := &mockModelView{}
		expected := Metadata{RunResult: "base"}

		overlay.On("Metadata").Return(expected)

		model, err := NewOverlayModel(base, overlay)
		require.NoError(t, err)

		metadata := model.Metadata()
		assert.Equal(t, expected, metadata)

		base.AssertNotCalled(t, "Metadata")
		overlay.AssertExpectations(t)
	})
	t.Run("find entities returns not-exist when both base and overlay missing", func(t *testing.T) {
		base := &mockModelView{}
		overlay := &mockModelView{}

		overlay.On("FindEntities", "g").Return([]Entity(nil), fs.ErrNotExist)
		base.On("FindEntities", "g").Return([]Entity(nil), fs.ErrNotExist)

		model, err := NewOverlayModel(base, overlay)
		require.NoError(t, err)

		_, err = model.FindEntities("g")
		require.Error(t, err)
		assert.ErrorIs(t, err, fs.ErrNotExist)

		base.AssertExpectations(t)
		overlay.AssertExpectations(t)
	})
	t.Run("find components returns not-exist when both base and overlay missing", func(t *testing.T) {
		base := &mockModelView{}
		overlay := &mockModelView{}

		overlay.On("FindComponents", "g").Return([]Component(nil), fs.ErrNotExist)
		base.On("FindComponents", "g").Return([]Component(nil), fs.ErrNotExist)

		model, err := NewOverlayModel(base, overlay)
		require.NoError(t, err)

		_, err = model.FindComponents("g")
		require.Error(t, err)
		assert.ErrorIs(t, err, fs.ErrNotExist)

		base.AssertExpectations(t)
		overlay.AssertExpectations(t)
	})
	t.Run("list components returns not-exist when both base and overlay missing", func(t *testing.T) {
		base := &mockModelView{}
		overlay := &mockModelView{}
		entity := Entity{RelativePath: "tool/base/0"}

		overlay.On("ListEntityComponents", entity).Return([]Component(nil), fs.ErrNotExist)
		base.On("ListEntityComponents", entity).Return([]Component(nil), fs.ErrNotExist)

		model, err := NewOverlayModel(base, overlay)
		require.NoError(t, err)

		_, err = model.ListEntityComponents(entity)
		require.Error(t, err)
		assert.ErrorIs(t, err, fs.ErrNotExist)

		base.AssertExpectations(t)
		overlay.AssertExpectations(t)
	})
	t.Run("returns error when base or manifest is nil", func(t *testing.T) {
		base := &mockModelView{}
		overlay := &mockModelView{}
		expectedError := "base and overlay model views cannot be nil"

		_, err := NewOverlayModel(base, nil)
		assert.EqualError(t, err, expectedError)

		_, err = NewOverlayModel(nil, overlay)
		assert.EqualError(t, err, expectedError)
	})
	t.Run("find entities does not return overlay entities relative to the base when overlay is inside base", func(t *testing.T) {
		baseDir := t.TempDir()
		overlayDir := filepath.Join(baseDir, "overlay")

		baseEntity := filepath.Join(baseDir, "tool", "foo", "0")
		overlayEntity := filepath.Join(overlayDir, "tool", "foo", "0")

		require.NoError(t, os.MkdirAll(baseEntity, 0o755))
		require.NoError(t, os.MkdirAll(overlayEntity, 0o755))

		base := NewOnDiskModel(baseDir, &Manifest{}, Metadata{})
		overlay := NewOnDiskModel(overlayDir, &Manifest{}, Metadata{})
		model, err := NewOverlayModel(base, overlay)
		require.NoError(t, err)

		entities, err := model.FindEntities("**")
		require.NoError(t, err)

		seen := map[string]struct{}{}
		for _, entity := range entities {
			seen[entity.RelativePath] = struct{}{}
		}

		assert.Contains(t, seen, filepath.ToSlash(filepath.Join("tool", "foo", "0")))
		assert.NotContains(t, seen, filepath.ToSlash(filepath.Join("overlay", "tool", "foo", "0")))
	})

	t.Run("list components does not return overlay components relative to the base when overlay is inside base", func(t *testing.T) {
		baseDir := t.TempDir()
		overlayDir := filepath.Join(baseDir, "overlay")

		entity := filepath.ToSlash(filepath.Join("overlay", "tool", "foo", "0"))
		componentPath := filepath.Join(baseDir, entity, "a.txt")

		require.NoError(t, os.MkdirAll(filepath.Dir(componentPath), 0o755))
		require.NoError(t, os.WriteFile(componentPath, []byte("overlay"), 0o644))

		base := NewOnDiskModel(baseDir, &Manifest{}, Metadata{})
		overlay := NewOnDiskModel(overlayDir, &Manifest{}, Metadata{})
		model, err := NewOverlayModel(base, overlay)
		require.NoError(t, err)

		components, err := model.ListEntityComponents(Entity{RelativePath: entity})
		require.NoError(t, err)
		assert.Empty(t, components)
	})

	t.Run("find components de-duplicates with overlay precedence", func(t *testing.T) {
		base := &mockModelView{}
		overlay := &mockModelView{}

		overlayComponent := Component{
			Type:         ComponentType{Name: TypeLogText, SchemaVersion: "0.1"},
			RelativePath: "tool/logs/0/messages.txt",
			AbsolutePath: "/overlay/tool/logs/0/messages.txt",
		}
		overlayExtra := Component{
			Type:         ComponentType{Name: TypeLogJSON, SchemaVersion: "0.1"},
			RelativePath: "tool/logs/1/events.jsonl",
			AbsolutePath: "/overlay/tool/logs/1/events.jsonl",
		}
		baseDuplicate := Component{
			Type:         ComponentType{Name: "base", SchemaVersion: "0.1"},
			RelativePath: "tool/logs/0/messages.txt",
			AbsolutePath: "/base/tool/logs/0/messages.txt",
		}
		baseExtra := Component{
			Type:         ComponentType{Name: "metrics", SchemaVersion: "0.1"},
			RelativePath: "tool/other/0/metrics.csv",
			AbsolutePath: "/base/tool/other/0/metrics.csv",
		}

		base.On("BasePath").Return("/base")
		overlay.On("BasePath").Return("/overlay")
		overlay.On("FindComponents", "tool/**").Return([]Component{overlayComponent, overlayExtra}, nil)
		base.On("FindComponents", "tool/**").Return([]Component{baseDuplicate, baseExtra}, nil)

		model, err := NewOverlayModel(base, overlay)
		require.NoError(t, err)

		components, err := model.FindComponents("tool/**")
		require.NoError(t, err)
		assert.Equal(t, []Component{overlayComponent, overlayExtra, baseExtra}, components)

		base.AssertExpectations(t)
		overlay.AssertExpectations(t)
	})
}
