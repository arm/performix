// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package cdf

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf/semver"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

// OverlayModel composes two ModelViews (base + overlay) and applies consistent
// overlay semantics: overlay wins on conflicts, pattern resolution checks overlay
// first, and entity/component listings are a de-duplicated union.
type OverlayModel struct {
	base    ModelView
	overlay ModelView
}

// mergeByRelativePath de-duplicates overlay and base slices by relative path, with overlay winning.
func mergeByRelativePath[T any](overlayItems []T, baseItems []T, key func(T) string) []T {
	seen := make(map[string]struct{}, len(overlayItems))
	result := make([]T, 0, len(overlayItems)+len(baseItems))

	// Overlay wins on duplicate relative paths, so track seen keys and skip them in base.
	for _, item := range overlayItems {
		seen[key(item)] = struct{}{}
		result = append(result, item)
	}
	for _, item := range baseItems {
		if _, ok := seen[key(item)]; ok {
			continue
		}
		result = append(result, item)
	}
	return result
}

// overlayRootIsNested reports whether the overlay root lives within the base root.
func overlayRootIsNested(baseRoot string, overlayRoot string) bool {
	if baseRoot == "" || overlayRoot == "" {
		return false
	}
	if overlayRoot == baseRoot {
		return false
	}
	return strings.HasPrefix(overlayRoot, baseRoot+string(filepath.Separator))
}

// filterOutsideOverlayRoot removes items whose absolute paths live under the overlay root.
func filterOutsideOverlayRoot[T any](
	baseRoot string,
	overlayRoot string,
	items []T,
	relPath func(T) string,
) []T {
	if !overlayRootIsNested(filepath.Clean(baseRoot), filepath.Clean(overlayRoot)) {
		// If the overlay root isn't nested under the base root, skip filtering.
		return items
	}
	filtered := make([]T, 0, len(items))
	for _, item := range items {
		absPath := filepath.Clean(filepath.Join(baseRoot, filepath.FromSlash(relPath(item))))
		if absPath == overlayRoot || strings.HasPrefix(absPath, overlayRoot+string(filepath.Separator)) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

// NewOverlayModel constructs an OverlayModel from base and overlay views.
func NewOverlayModel(base ModelView, overlay ModelView) (*OverlayModel, error) {
	if base == nil || overlay == nil {
		return nil, errors.New("base and overlay model views cannot be nil")
	}
	return &OverlayModel{base: base, overlay: overlay}, nil
}

// ResolveComponent resolves a component path with overlay-first precedence.
func (m *OverlayModel) ResolveComponent(componentPath string) (Component, error) {
	// Only fall back to base on a not-found error; otherwise surface overlay errors.
	component, err := m.overlay.ResolveComponent(componentPath)
	if err == nil {
		return component, nil
	}
	if !errors.Is(err, ErrComponentNotFound) {
		return Component{}, err
	}
	return m.base.ResolveComponent(componentPath)
}

// ResolveComponentExpectType enforces a specific component type after resolution.
func (m *OverlayModel) ResolveComponentExpectType(
	componentPath string,
	expectedComponentType ComponentType,
) (Component, error) {
	// Only fall back to base on a not-found error; otherwise surface overlay errors.
	component, err := m.overlay.ResolveComponentExpectType(componentPath, expectedComponentType)
	if err == nil {
		return component, nil
	}
	if !errors.Is(err, ErrComponentNotFound) {
		return Component{}, err
	}
	return m.base.ResolveComponentExpectType(componentPath, expectedComponentType)

}

// ResolveComponentExpectTypes enforces that the resolved component matches one of the expected types.
func (m *OverlayModel) ResolveComponentExpectTypes(
	componentPath string,
	expectedComponentTypes ...ComponentType,
) (Component, error) {
	// Only fall back to base on a not-found error; otherwise surface overlay errors.
	component, err := m.overlay.ResolveComponentExpectTypes(componentPath, expectedComponentTypes...)
	if err == nil {
		return component, nil
	}
	if !errors.Is(err, ErrComponentNotFound) {
		return Component{}, err
	}
	return m.base.ResolveComponentExpectTypes(componentPath, expectedComponentTypes...)
}

// ResolveComponentByManifestPattern resolves a component by manifest glob, preferring overlay entries.
func (m *OverlayModel) ResolveComponentByManifestPattern(componentPath string) (Component, error) {
	// Only fall back to base on a not-found error; otherwise surface overlay errors.
	component, err := m.overlay.ResolveComponentByManifestPattern(componentPath)
	if err == nil {
		return component, nil
	}
	if !errors.Is(err, ErrComponentNotFound) {
		return Component{}, err
	}
	return m.base.ResolveComponentByManifestPattern(componentPath)
}

// ResolveComponentExpectTypeV resolves a component and enforces type + version range constraints.
func (m *OverlayModel) ResolveComponentExpectTypeV(
	componentPath string,
	typeName string,
	vr semver.VersionRange,
) (Component, semver.SemVer, error) {
	// Only fall back to base on a not-found error; otherwise surface overlay errors.
	component, ver, err := m.overlay.ResolveComponentExpectTypeV(componentPath, typeName, vr)
	if err == nil {
		return component, ver, nil
	}
	if !errors.Is(err, ErrComponentNotFound) {
		return Component{}, semver.SemVer{}, err
	}
	return m.base.ResolveComponentExpectTypeV(componentPath, typeName, vr)
}

// ResolveComponentByPatternExpectTypeV resolves by manifest pattern then enforces type + version range.
func (m *OverlayModel) ResolveComponentByPatternExpectTypeV(
	componentPath string,
	expectedTypeName string,
	vr semver.VersionRange,
) (Component, semver.SemVer, error) {
	// Only fall back to base on a not-found error; otherwise surface overlay errors.
	component, ver, err := m.overlay.ResolveComponentByPatternExpectTypeV(componentPath, expectedTypeName, vr)
	if err == nil {
		return component, ver, nil
	}
	if !errors.Is(err, ErrComponentNotFound) {
		return Component{}, semver.SemVer{}, err
	}
	return m.base.ResolveComponentByPatternExpectTypeV(componentPath, expectedTypeName, vr)
}

// FindEntities returns the de-duplicated union of overlay and base entities.
func (m *OverlayModel) FindEntities(glob string) ([]Entity, error) {

	overlayEntities, overlayErr := m.overlay.FindEntities(glob)
	if overlayErr != nil && !isNotExist(overlayErr) {
		return nil, overlayErr
	}

	baseEntities, baseErr := m.base.FindEntities(glob)
	if baseErr != nil && !isNotExist(baseErr) {
		return nil, baseErr
	}
	if isNotExist(overlayErr) && isNotExist(baseErr) {
		// If both overlay and base report not-exist, return not-exist error.
		return nil, fs.ErrNotExist
	}
	// If the overlay root is nested under the base root, exclude base entities that live under the overlay root to avoid duplicates.
	baseEntities = filterOutsideOverlayRoot(m.base.BasePath(), m.overlay.BasePath(), baseEntities, func(entity Entity) string {
		return entity.RelativePath
	})
	return mergeByRelativePath(
		overlayEntities,
		baseEntities,
		func(entity Entity) string { return entity.RelativePath },
	), nil
}

// FindComponents returns the de-duplicated union of overlay and base components.
func (m *OverlayModel) FindComponents(glob string) ([]Component, error) {
	overlayComponents, overlayErr := m.overlay.FindComponents(glob)
	if overlayErr != nil && !isNotExist(overlayErr) {
		return nil, overlayErr
	}

	baseComponents, baseErr := m.base.FindComponents(glob)
	if baseErr != nil && !isNotExist(baseErr) {
		return nil, baseErr
	}
	if isNotExist(overlayErr) && isNotExist(baseErr) {
		return nil, fs.ErrNotExist
	}

	baseComponents = filterOutsideOverlayRoot(m.base.BasePath(), m.overlay.BasePath(), baseComponents, func(component Component) string {
		return component.RelativePath
	})
	return mergeByRelativePath(
		overlayComponents,
		baseComponents,
		func(component Component) string { return component.RelativePath },
	), nil
}

// ListEntityComponents returns the de-duplicated union of overlay and base components for an entity.
func (m *OverlayModel) ListEntityComponents(entity Entity) ([]Component, error) {
	overlayComponents, overlayErr := m.overlay.ListEntityComponents(entity)
	if overlayErr != nil && !isNotExist(overlayErr) {
		return nil, overlayErr
	}

	baseComponents, baseErr := m.base.ListEntityComponents(entity)
	if baseErr != nil && !isNotExist(baseErr) {
		return nil, baseErr
	}
	if isNotExist(overlayErr) && isNotExist(baseErr) {
		// If both overlay and base report not-exist, return not-exist error.
		return nil, fs.ErrNotExist
	}

	// If the overlay root is nested under the base root, exclude base components that live under the overlay root to avoid duplicates.
	baseComponents = filterOutsideOverlayRoot(m.base.BasePath(), m.overlay.BasePath(), baseComponents, func(entity Component) string {
		return entity.RelativePath
	})
	// Merge overlay and base components, with overlay winning on duplicate relative paths.
	return mergeByRelativePath(
		overlayComponents,
		baseComponents,
		func(component Component) string { return component.RelativePath },
	), nil
}

// ListEntityComponentsByTypeName filters merged components by type name.
func (m *OverlayModel) ListEntityComponentsByTypeName(entity Entity, componentTypeName string) ([]Component, error) {
	components, err := m.ListEntityComponents(entity)
	if err != nil {
		return nil, err
	}
	return util.Filter(components, func(i int) bool {
		return components[i].Type.Name == componentTypeName
	}), nil
}

// ListEntityComponentsMatching filters merged components by a predicate.
func (m *OverlayModel) ListEntityComponentsMatching(entity Entity, pred func(*Component) bool) ([]Component, error) {
	components, err := m.ListEntityComponents(entity)
	if err != nil {
		return nil, err
	}
	return util.Filter(components, func(i int) bool {
		return pred(&components[i])
	}), nil
}

// Metadata returns overlay metadata if available, otherwise base metadata.
func (m *OverlayModel) Metadata() Metadata {
	return m.overlay.Metadata()
}

// Migrations returns overlay migrations if available, otherwise base migrations.
func (m *OverlayModel) Migrations() []PathMigration {
	if overlayMigrations := m.overlay.Migrations(); overlayMigrations != nil {
		return m.overlay.Migrations()
	}
	return m.base.Migrations()
}

// BasePath returns the base path of the overlay if available, otherwise the base path of the base model.
func (m *OverlayModel) BasePath() string {
	return m.overlay.BasePath()
}

// isNotExist normalizes filesystem not-found errors for overlay fallbacks.
func isNotExist(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || os.IsNotExist(err)
}
