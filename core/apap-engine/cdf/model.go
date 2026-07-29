// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package cdf

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/afero"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf/semver"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

// ModelView defines the read-only surface area for resolving and listing
// entities/components. Mutating operations (e.g. AddToolOutput) are intentionally
// excluded so overlay semantics can be consistently enforced.
type ModelView interface {
	ResolveComponent(componentPath string) (Component, error)
	ResolveComponentExpectType(componentPath string, expectedComponentType ComponentType) (Component, error)
	ResolveComponentExpectTypes(componentPath string, expectedComponentTypes ...ComponentType) (Component, error)
	ResolveComponentByManifestPattern(componentPath string) (Component, error)
	ResolveComponentExpectTypeV(componentPath string, typeName string, vr semver.VersionRange) (Component, semver.SemVer, error)
	ResolveComponentByPatternExpectTypeV(componentPath string, expectedTypeName string, vr semver.VersionRange) (Component, semver.SemVer, error)
	FindEntities(glob string) ([]Entity, error)
	FindComponents(glob string) ([]Component, error)
	ListEntityComponents(entity Entity) ([]Component, error)
	ListEntityComponentsByTypeName(entity Entity, componentTypeName string) ([]Component, error)
	ListEntityComponentsMatching(entity Entity, pred func(*Component) bool) ([]Component, error)
	Metadata() Metadata
	BasePath() string
}

// OnDiskModel allows access to Entities and Components stored in a run. It is the
// design intention that knowledge of how this is laid out on disk should be encapsulated
// in OnDiskModel and/or Run.
type OnDiskModel struct {
	basePath   string
	manifest   *Manifest
	metadata   Metadata
	migrations []PathMigration
	FS         afero.Fs // if nil, the default system FS will be used
}

// ------------------------
// OnDiskModel
// ------------------------

// ErrComponentNotFound indicates a component couldn't be resolved.
var ErrComponentNotFound = fmt.Errorf("component not found")

// ErrComponentPending indicates a component was resolved successfully, but is pending transfer.
var ErrComponentPending = fmt.Errorf("component pending transfer")

// NewOnDiskModel constructs a new on disk model
func NewOnDiskModel(basePath string, manifest *Manifest, metadata Metadata) *OnDiskModel {
	if manifest == nil {
		manifest = &Manifest{}
	}
	return &OnDiskModel{basePath: basePath, manifest: manifest, metadata: metadata}
}

// Metadata returns the metadata of the run represented by this OnDiskModel
func (m *OnDiskModel) Metadata() Metadata {
	return m.metadata
}

// BasePath returns the base path of the run on disk.
func (m *OnDiskModel) BasePath() string {
	return m.basePath
}

// Manifest returns a copy of the manifest of the run represented by this OnDiskModel.
func (m *OnDiskModel) Manifest() Manifest {
	if m.manifest == nil {
		m.manifest = &Manifest{}
	}
	return *m.manifest
}

// Migrations returns the migrations attached to the manifest
func (m *OnDiskModel) Migrations() []PathMigration { return m.migrations }

var unknownComponentType = ComponentType{"unknown", ""}

// ResolveComponent is used to perform a lookup of a Component by path.
// It is used by the renderer to request access to required Components from disk.
func (m *OnDiskModel) ResolveComponent(componentPath string) (Component, error) {
	normalized := NormalizePath(componentPath)

	var absolute string
	var rewrote string
	var ok bool
	var err error
	var entry *ManifestEntry

	// 1) try direct lookup in the manifest
	entry = m.manifest.Lookup(normalized)
	if entry != nil {
		goto FOUND
	}

	// 2) try our migration table
	rewrote, ok, err = m.migratePath(normalized)
	if ok {
		normalized = rewrote
		entry = m.manifest.Lookup(normalized)
		if entry != nil {
			log.Debugf("path-migration: rewrote %q to %q", componentPath, normalized)
			goto FOUND
		}
	}
	if err != nil {
		return Component{}, fmt.Errorf("failed to migrate path %q: %w", componentPath, err)
	}

	// fell through—no component in manifest
	absolute, err = m.resolveModelPath(normalized)
	if err != nil {
		return Component{}, err
	}
	if info, err := m.fs().Stat(absolute); err == nil && !info.IsDir() {
		log.Warnf("Component %q missing manifest entry but exists on disk", absolute)
		return Component{unknownComponentType, normalized, absolute}, nil
	}

	return Component{unknownComponentType, normalized, ""}, fmt.Errorf("%w: %q", ErrComponentNotFound, normalized)

FOUND:
	if entry.Pending {
		return Component{}, ErrComponentPending
	}
	// If the requested path is a glob, check if other child components are pending too
	for _, e := range m.manifest.Entries {
		ok, err = doublestar.Match(entry.Path, e.Path)
		if err != nil {
			continue
		}
		if ok {
			if e.Pending {
				return Component{}, ErrComponentPending
			}
		}
	}
	absolute, err = m.resolveModelPath(entry.Path)
	if err != nil {
		return Component{}, err
	}
	return Component{entry.ComponentType, entry.Path, absolute}, nil
}

// ResolveComponentExpectType looks up a component using ResolveComponent, but fails if the component type doesn't
// match the supplied expected value
func (m *OnDiskModel) ResolveComponentExpectType(
	componentPath string,
	expectedComponentType ComponentType,
) (Component, error) {
	component, err := m.ResolveComponent(componentPath)
	if err != nil {
		return Component{}, err
	}

	if component.Type != expectedComponentType {
		return Component{}, fmt.Errorf(
			"wrong component type for component '%s' in run '%s': %v; expected %v",
			componentPath,
			m.basePath,
			component.Type,
			expectedComponentType,
		)
	}

	return component, nil
}

// ResolveComponentExpectTypes looks up a component using ResolveComponent, but fails if the component type doesn't
// match any of the supplied expected values
func (m *OnDiskModel) ResolveComponentExpectTypes(
	componentPath string,
	expectedComponentTypes ...ComponentType,
) (Component, error) {
	component, err := m.ResolveComponent(componentPath)
	if err != nil {
		return Component{}, err
	}

	for _, expectedComponentType := range expectedComponentTypes {
		if component.Type == expectedComponentType {
			return component, nil
		}
	}
	return Component{}, fmt.Errorf(
		"wrong component type for component '%s' in run '%s': %v; expected one of: %+v",
		componentPath,
		m.basePath,
		component.Type,
		expectedComponentTypes,
	)
}

// ResolveComponentByManifestPattern attempts to resolve a component by matching the given componentPath
// against glob/wildcard patterns defined in the manifest. The componentPath itself is not a pattern.
// If a matching manifest entry is found, it returns the corresponding Component. If not, it returns an error.
func (m *OnDiskModel) ResolveComponentByManifestPattern(componentPath string) (Component, error) {
	want := NormalizePath(componentPath)

	// 1) first try a glob match against the manifest entries
	for _, e := range m.manifest.Entries {
		ok, err := doublestar.Match(e.Path, want)
		if err != nil {
			return Component{}, fmt.Errorf("pattern match error: %w", err)
		}
		if ok {
			if e.Pending {
				return Component{}, ErrComponentPending
			}
			// entry.Path may contain wildcards; we return that as RelativePath,
			// but the actual file lives at `want`.
			abs, err := m.resolveModelPath(want)
			if err != nil {
				return Component{}, err
			}
			return Component{
				e.ComponentType,
				e.Path,
				abs,
			}, nil
		}
	}

	var rewrote string
	var ok bool
	var err error

	// 2) if nothing matched, try path‐migrations first (newest first)
	rewrote, ok, err = m.migratePath(want)
	if ok {
		for _, e := range m.manifest.Entries {
			ok, err := doublestar.Match(e.Path, rewrote)
			if err != nil {
				return Component{}, fmt.Errorf("pattern match error: %w", err)
			}
			if ok {
				log.Debugf("pattern-migration: rewrote %q → %q", componentPath, rewrote)
				if e.Pending {
					return Component{}, ErrComponentPending
				}
				abs, err := m.resolveModelPath(rewrote)
				if err != nil {
					return Component{}, err
				}
				return Component{
					e.ComponentType,
					e.Path,
					abs,
				}, nil
			}
		}
	}
	if err != nil {
		return Component{}, fmt.Errorf("failed to migrate path %q: %w", componentPath, err)
	}

	// 3) still nothing?
	return Component{
		unknownComponentType,
		want,
		"",
	}, fmt.Errorf("%w in manifest (pattern match): '%s'", ErrComponentNotFound, componentPath)
}

func (m *OnDiskModel) fs() afero.Fs {
	if m.FS == nil {
		return afero.NewOsFs()
	}
	return m.FS
}

func (m *OnDiskModel) resolveModelPath(path string) (string, error) {
	basePath := filepath.Clean(m.basePath)
	resolvedPath := filepath.Join(basePath, filepath.FromSlash(path))

	rel, err := filepath.Rel(basePath, resolvedPath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve path %q relative to model %q: %w", path, m.basePath, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("path %q escapes model base %q", path, m.basePath)
	}

	return resolvedPath, nil
}

func normalizeGlob(glob string) string {
	glob = strings.ReplaceAll(glob, "\\", "/")
	if len(glob) > 0 && glob[0] == '/' {
		glob = glob[1:]
	}
	return glob
}

func hasGlobMeta(path string) bool {
	return strings.ContainsAny(path, "*?[{")
}

func literalGlobPrefix(glob string) string {
	segments := strings.Split(glob, "/")
	prefixEnd := 0
	for i, segment := range segments {
		if hasGlobMeta(segment) {
			break
		}
		prefixEnd = i + 1
	}
	return strings.Join(segments[:prefixEnd], "/")
}

// FindEntities lists all entities in the model matching the supplied glob pattern; glob pattern supports wildcard
// elements '*' and '**'
func (m *OnDiskModel) FindEntities(glob string) ([]Entity, error) {
	glob = strings.ReplaceAll(glob, "\\", "/")
	if len(glob) > 0 && glob[0] == '/' {
		glob = glob[1:]
	}

	var matches []Entity
	walkFunc := func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return nil
		}

		subpath := path[min(len(path), len(m.basePath)+1):]
		subpath = strings.ReplaceAll(subpath, "\\", "/")
		isMatch, err := doublestar.Match(glob, subpath)
		if err != nil {
			return fmt.Errorf("failure during pattern match: %w", err)
		}

		if isMatch {
			matches = append(matches, Entity{RelativePath: subpath})
		}

		return nil
	}

	if err := afero.Walk(m.fs(), m.basePath, walkFunc); err != nil {
		return nil, fmt.Errorf("failed to list entities for pattern '%s' in model '%s': %w", glob, m.basePath, err)
	}

	return matches, nil
}

// FindComponents lists all components in the model matching the supplied glob pattern.
func (m *OnDiskModel) FindComponents(glob string) ([]Component, error) {
	glob = normalizeGlob(glob)

	// Check the pattern is valid before we start walking the filesystem. This will catch errors in the pattern even if
	// no files are matched.
	if _, err := doublestar.Match(glob, glob); err != nil {
		return nil, fmt.Errorf("failure during pattern match: %w", err)
	}

	if !hasGlobMeta(glob) {
		component, err := m.ResolveComponent(glob)
		if err != nil {
			if errors.Is(err, ErrComponentNotFound) {
				return nil, nil
			}
			return nil, fmt.Errorf("failed to resolve component %q: %w", glob, err)
		}
		return []Component{component}, nil
	}

	searchRoot, err := m.resolveModelPath(literalGlobPrefix(glob))
	if err != nil {
		return nil, fmt.Errorf("failed to list components for pattern '%s' in model '%s': %w", glob, m.basePath, err)
	}
	rootInfo, err := m.fs().Stat(searchRoot)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to list components for pattern '%s' in model '%s': %w", glob, m.basePath, err)
	}
	if !rootInfo.IsDir() {
		return nil, nil
	}

	var matches []Component
	walkFunc := func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		subpath := path[min(len(path), len(m.basePath)+1):]
		subpath = strings.ReplaceAll(subpath, "\\", "/")
		isMatch, err := doublestar.Match(glob, subpath)
		if err != nil {
			return fmt.Errorf("failure during pattern match: %w", err)
		}
		if !isMatch {
			return nil
		}

		component, err := m.ResolveComponent(subpath)
		if err != nil {
			return fmt.Errorf("failed to resolve component %q: %w", subpath, err)
		}
		matches = append(matches, component)
		return nil
	}

	if err := afero.Walk(m.fs(), searchRoot, walkFunc); err != nil {
		return nil, fmt.Errorf("failed to list components for pattern '%s' in model '%s': %w", glob, m.basePath, err)
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].RelativePath < matches[j].RelativePath
	})

	return matches, nil
}

// ListEntityComponents lists components that are direct descendents of the supplied entity; it does not descend into
// children
func (m *OnDiskModel) ListEntityComponents(entity Entity) ([]Component, error) {
	entries, err := afero.ReadDir(m.fs(), filepath.Join(m.basePath, entity.RelativePath))
	if err != nil {
		return nil, fmt.Errorf("failed to list components in entity '%s': %w", entity.RelativePath, err)
	}

	result := make([]Component, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		componentPath := filepath.Join(entity.RelativePath, entry.Name())
		component, err := m.ResolveComponent(componentPath)
		if err != nil {
			return nil, fmt.Errorf("failed to list components in entity '%s': %w", entity.RelativePath, err)
		}

		result = append(result, component)
	}

	return result, nil
}

// ListEntityComponentsByTypeName lists components that are direct descendents of the supplied entity, filtered to
// only those whose component type name matches the supplied value
func (m *OnDiskModel) ListEntityComponentsByTypeName(entity Entity, componentTypeName string) ([]Component, error) {
	components, err := m.ListEntityComponents(entity)
	if err != nil {
		return nil, err
	}
	return util.Filter(components, func(i int) bool {
		return components[i].Type.Name == componentTypeName
	}), nil
}

// ListEntityComponentsMatching lists components that are direct descendents of the supplied entity, filtered to
// only those whose component type matches the supplied predicate
func (m *OnDiskModel) ListEntityComponentsMatching(entity Entity, pred func(*Component) bool) ([]Component, error) {
	components, err := m.ListEntityComponents(entity)
	if err != nil {
		return nil, err
	}
	return util.Filter(components, func(i int) bool {
		return pred(&components[i])
	}), nil
}

// ------------------------
// New helpers for version constraint resolution
// ------------------------

// ResolveComponentExpectTypeV resolves componentPath and enforces:
//   - component type name matches typeName
//   - component schema_version satisfies vr
//
// Returns the Component and its parsed schema SemVer.
func (m *OnDiskModel) ResolveComponentExpectTypeV(
	componentPath string,
	typeName string,
	vr semver.VersionRange,
) (Component, semver.SemVer, error) {
	c, err := m.ResolveComponent(componentPath)
	if err != nil {
		return Component{}, semver.SemVer{}, err
	}

	if c.Type.Name != typeName {
		return Component{}, semver.SemVer{}, fmt.Errorf(
			"wrong component type for '%s' in run '%s': got {%s %s}, expected {%s <range>}",
			componentPath, m.basePath, c.Type.Name, c.Type.SchemaVersion, typeName,
		)
	}

	sv, err := semver.ParseSemVer(c.Type.SchemaVersion)
	if err != nil {
		return Component{}, semver.SemVer{}, fmt.Errorf(
			"invalid schema_version %q for component '%s': %v",
			c.Type.SchemaVersion, componentPath, err,
		)
	}

	if !semver.InRange(sv, vr) {
		return Component{}, semver.SemVer{}, fmt.Errorf(
			"schema_version %q for '%s' (type %s) does not satisfy constraint (%s)",
			c.Type.SchemaVersion, componentPath, typeName, vr.String(),
		)
	}

	return c, sv, nil
}

// ResolveComponentByPatternExpectTypeV first resolves by manifest wildcard/pattern,
// then enforces component type and schema_version range. Returns Component + parsed SemVer.
func (m *OnDiskModel) ResolveComponentByPatternExpectTypeV(
	componentPath string,
	expectedTypeName string,
	vr semver.VersionRange,
) (Component, semver.SemVer, error) {
	// 1) Find the manifest entry by glob/pattern and expected type.
	normalizedPath := NormalizePath(componentPath)
	c, err := m.resolveComponentByManifestPatternAndType(normalizedPath, expectedTypeName)
	if err != nil {
		return Component{}, semver.SemVer{}, err
	}

	// 2) Parse schema_version as semver.
	sv, err := semver.ParseSemVer(c.Type.SchemaVersion)
	if err != nil {
		return Component{}, semver.SemVer{}, fmt.Errorf(
			"bad schema_version for %q (%s@%s): %v",
			componentPath, c.Type.Name, c.Type.SchemaVersion, err,
		)
	}

	// 3) Check version range.
	if !semver.InRange(sv, vr) {
		return Component{}, semver.SemVer{}, fmt.Errorf(
			"version constraint not satisfied for %q: have %s@%s, require %s",
			componentPath, c.Type.Name, sv.String(), vr.String(),
		)
	}

	return c, sv, nil
}

func (m *OnDiskModel) resolveComponentByManifestPatternAndType(componentPath string, expectedTypeName string) (Component, error) {
	component, err := m.findManifestPatternComponentByType(componentPath, componentPath, expectedTypeName)

	// If not found retry with migrated path
	if err == nil && component == nil {
		migratedPath, ok, migrationErr := m.migratePath(componentPath)
		if migrationErr != nil {
			return Component{}, fmt.Errorf("failed to migrate path %q: %w", componentPath, migrationErr)
		}
		if ok {
			log.Debugf("pattern-migration: rewrote %q → %q", componentPath, migratedPath)
			component, err = m.findManifestPatternComponentByType(componentPath, migratedPath, expectedTypeName)
		}
	}

	if err != nil {
		return Component{}, err
	}
	if component == nil {
		return Component{}, fmt.Errorf("%w in manifest (pattern match): '%s'", ErrComponentNotFound, componentPath)
	}
	return *component, nil
}

func (m *OnDiskModel) findManifestPatternComponentByType(originalPath string, matchPath string, expectedTypeName string) (*Component, error) {
	var firstMatch *ComponentType

	for _, e := range m.manifest.Entries {
		ok, err := doublestar.Match(e.Path, matchPath)
		if err != nil {
			return nil, fmt.Errorf("pattern match error: %w", err)
		}
		if ok {
			if e.ComponentType.Name == expectedTypeName {
				if e.Pending {
					return &Component{}, ErrComponentPending
				}
				absolutePath, err := m.resolveModelPath(matchPath)
				if err != nil {
					return nil, err
				}
				return &Component{
					Type:         e.ComponentType,
					RelativePath: e.Path,
					AbsolutePath: absolutePath,
				}, nil
			}
			if firstMatch == nil {
				firstMatch = &e.ComponentType
			}
		}
	}

	if firstMatch != nil {
		return nil, fmt.Errorf(
			"wrong component type for %q: got %s@%s; expected %s",
			originalPath, firstMatch.Name, firstMatch.SchemaVersion, expectedTypeName,
		)
	}

	return nil, nil
}

func (m *OnDiskModel) AddToolOutput(tu ToolUsed) {
	m.manifest.ToolsUsed = append(m.manifest.ToolsUsed, tu)
}

// AddEntry adds a ManifestEntry to the manifest. Note that this does not write to disk; callers must call WriteManifest to persist changes.
func (m *OnDiskModel) AddEntry(e ManifestEntry) {
	m.manifest.Entries = append(m.manifest.Entries, e)
}
