// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package cdf

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf/semver"
)

// PathMigration is a marker interface for any migration.
type PathMigration interface {
	// IsPathMigration tags this struct as a PathMigration.
	IsPathMigration()
	// Version returns the SemVer at which this migration was introduced.
	Version() semver.SemVer
}

// ToolNameMigration records that at Ver, the tool directory was renamed
// from From to To.
type ToolNameMigration struct {
	Type string
	From string
	To   string
	Ver  semver.SemVer
}

func (m *ToolNameMigration) IsPathMigration()       {}
func (m *ToolNameMigration) Version() semver.SemVer { return m.Ver }

// ToolPathSuffixMigration records that at Ver, under a fixed tool folder,
// any path starting with OldSuffix should be rewritten under NewSuffix.
type ToolPathSuffixMigration struct {
	Type      string
	From      string
	Ver       semver.SemVer
	OldSuffix string
	NewSuffix string
}

func (m *ToolPathSuffixMigration) IsPathMigration()       {}
func (m *ToolPathSuffixMigration) Version() semver.SemVer { return m.Ver }

type ToolInvocationMigration struct {
	Type string
	From string
	Ver  semver.SemVer
}

func (m *ToolInvocationMigration) IsPathMigration()       {}
func (m *ToolInvocationMigration) Version() semver.SemVer { return m.Ver }

// byVersionDesc sorts a slice of PathMigration newest-first.
type byVersionDesc []PathMigration

func (a byVersionDesc) Len() int           { return len(a) }
func (a byVersionDesc) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a byVersionDesc) Less(i, j int) bool { return semver.Cmp(a[j].Version(), a[i].Version()) < 0 }

type PathParts struct {
	Tool       string // e.g. "streamline-cli" or "instruction_mix"
	Invocation string // e.g. "0" (always present on disk once we migrate)
	Suffix     string // everything after the invocation, like "output/foo.csv"
}

// parsePathParts accepts:
//
//	tool/<name>                         → Invocation="", Suffix=""
//	tool/<name>/<invocation>            → Invocation=<invocation>, Suffix=""
//	tool/<name>/<invocation>/<suffix>  → Invocation=<invocation>, Suffix=<suffix>
//
// It insists that if an invocation segment is present, it must be numeric.
func parsePathParts(p string) (*PathParts, bool) {
	const prefix = "tool/"
	if !strings.HasPrefix(p, prefix) {
		return nil, false
	}
	rest := p[len(prefix):]
	parts := strings.SplitN(rest, "/", 3)
	name := parts[0]

	switch len(parts) {
	case 1:
		// only the tool name
		return &PathParts{name, "", ""}, true
	case 2:
		// could be either “invocation only” or “name+suffix”,
		// but for our modern layout we treat it as invocation if numeric
		if _, err := strconv.Atoi(parts[1]); err == nil {
			return &PathParts{name, parts[1], ""}, true
		}
		// otherwise it’s a suffix with no invocation
		return &PathParts{name, "", parts[1]}, true
	case 3:
		// full modern layout: name, invocation, suffix
		if _, err := strconv.Atoi(parts[1]); err != nil {
			return nil, false
		}
		return &PathParts{name, parts[1], parts[2]}, true
	default:
		return nil, false
	}
}

// parseMissingInvocationPathParts accepts
//
//	tool/<name>         → Invocation="", Suffix=""
//	tool/<name>/<suffix>→ Invocation="", Suffix=<suffix>
func parseMissingInvocationPathParts(p string) (*PathParts, bool) {
	const prefix = "tool/"
	if !strings.HasPrefix(p, prefix) {
		return nil, false
	}
	rest := p[len(prefix):]
	parts := strings.SplitN(rest, "/", 2)
	name := parts[0]
	suffix := ""
	if len(parts) == 2 {
		suffix = parts[1]
	}
	return &PathParts{name, "", suffix}, true
}

func buildPath(pp *PathParts) string {
	base := "tool/" + pp.Tool
	if pp.Invocation != "" {
		base += "/" + pp.Invocation
	}
	if pp.Suffix != "" {
		base += "/" + pp.Suffix
	}
	return base
}

func rewriteViaMigration(path string, mig PathMigration) (string, bool) {
	// first, try the all-in-one modern parser
	parts, ok := parsePathParts(path)
	if !ok {
		// only name- or suffix- migrations may handle legacy layouts:
		switch mig.(type) {
		case *ToolNameMigration, *ToolPathSuffixMigration:
			parts, ok = parseMissingInvocationPathParts(path)
			if !ok {
				return "", false
			}
		default:
			return "", false
		}
	}

	switch m := mig.(type) {
	case *ToolInvocationMigration:
		// modern layout guaranteed: only strip the “0” invocation off
		if parts.Tool != m.From || parts.Invocation != "0" {
			return "", false
		}
		parts.Invocation = ""
		return buildPath(parts), true

	case *ToolNameMigration:
		if parts.Tool != m.To {
			return "", false
		}
		parts.Tool = m.From
		return buildPath(parts), true

	case *ToolPathSuffixMigration:
		if parts.Tool != m.From {
			return "", false
		}
		if !strings.HasPrefix(parts.Suffix, m.NewSuffix) {
			return "", false
		}
		tail := parts.Suffix[len(m.NewSuffix):]
		parts.Suffix = m.OldSuffix + tail
		return buildPath(parts), true

	default:
		return "", false
	}
}

// migratePath attempts to migrate the given “today” path backwards through all
// registered PathMigrations for this model. It does not consult the manifest itself.
//
// Returns (rewritten, true) if any migration was applied; otherwise ("", false).
func (m *OnDiskModel) migratePath(original string) (string, bool, error) {
	return MigratePath(original, m.migrations)
}

// MigratePath walks the given “today” path backwards through all provided
// PathMigrations (newest-first), chaining each successful rewrite. If it ever
// produces the same intermediate path twice, it stops to avoid an infinite
// loop.
//
// Returns (rewritten, true) if any migration was applied; otherwise ("", false).
func MigratePath(original string, ms []PathMigration) (string, bool, error) {
	norm := NormalizePath(original)

	// Sort migrations newest-first
	migs := append([]PathMigration(nil), ms...)
	sort.Sort(byVersionDesc(migs))

	current := norm
	seen := map[string]struct{}{
		norm: {},
	}

	for _, pm := range migs {
		cand, ok := rewriteViaMigration(current, pm)
		if !ok || cand == current {
			continue
		}
		// Cycle detection: if we’ve already seen this candidate, break out
		if _, exists := seen[cand]; exists {
			return "", false, fmt.Errorf("MigratePath: detected cycle rewriting %q to %q; stopping", current, cand)
		}
		seen[cand] = struct{}{}
		current = cand
	}

	if current != norm {
		return current, true, nil
	}
	return "", false, nil
}

func MigrateToolName(toolName string, ms []PathMigration) (string, bool, error) {
	// Sort migrations newest-first
	migs := append([]PathMigration(nil), ms...)
	sort.Sort(byVersionDesc(migs))

	current := toolName
	seen := map[string]struct{}{
		toolName: {},
	}

	for _, pm := range migs {
		switch m := pm.(type) {
		case *ToolNameMigration:
			if current != m.To || current == m.From {
				continue
			}
			// Cycle detection: if we’ve already seen this candidate, break out
			if _, exists := seen[m.From]; exists {
				return "", false, fmt.Errorf("MigrateToolName: detected cycle rewriting %q to %q; stopping", current, m.From)
			}
			seen[m.From] = struct{}{}
			current = m.From
		case *ToolInvocationMigration, *ToolPathSuffixMigration:
		default:
			continue
		}
	}

	if current != toolName {
		return current, true, nil
	}
	return "", false, nil
}

// InjectLegacyToolOutputs scans m.manifest.Entries for any paths under
// tool/<name>/… and injects a default ToolUsed{Version:defaultVersion,Invocation:0}
// for each <name> *not already* in m.manifest.ToolsUsed.
func (m *OnDiskModel) InjectLegacyToolOutputs(defaultVersion string) {
	// 1) Record existing toolsUsed
	seen := make(map[string]struct{}, len(m.manifest.ToolsUsed))
	for _, tu := range m.manifest.ToolsUsed {
		seen[tu.Tool] = struct{}{}
	}

	// 2) Scan manifest entries for any new tool folders
	for _, e := range m.manifest.Entries {
		parts := strings.Split(e.Path, "/")
		if len(parts) < 2 || parts[0] != "tool" {
			continue
		}
		name := parts[1]
		if _, ok := seen[name]; ok {
			continue // already recorded
		}
		seen[name] = struct{}{}
		// inject with Invocation=0
		m.AddToolOutput(ToolUsed{
			Tool:       name,
			Version:    defaultVersion,
			Invocation: 0,
		})
	}
}

// AddPathMigrations registers the given migrations on the OnDiskModel, then
// appends for each one a “catch-all” name-swap fallback so that any path
// not covered by a suffix rule still at least gets its tool directory renamed.
func (m *OnDiskModel) AddPathMigrations(ms []PathMigration) {
	m.migrations = append(m.migrations, ms...)
}
