// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

// componentAdder is the minimal thing WriteSourceCode actually needs - done to aid unit testing
type componentAdder interface {
	AddComponent(ct cdf.ComponentType, filename string) string
}

type HostSourceCodePath struct {
	Paths []string `json:"paths"`
}

const SourceCodeFilename = "source-code.json"
const componentNamePrefix = "source-code"
const componentSchemaVersion = "1.0"

func SourceCodeCT() cdf.ComponentType {
	return cdf.ComponentType{Name: componentNamePrefix, SchemaVersion: componentSchemaVersion}
}

func CreateInitialHostSourceCodePath(builder componentAdder, sc *HostSourceCodePath) error {
	componentFile := builder.AddComponent(SourceCodeCT(), SourceCodeFilename)
	err := WriteHostSourceCodePath(componentFile, sc)
	if err != nil {
		return fmt.Errorf("failed to write initial host source code path: %w", err)
	}
	return nil
}

func WriteHostSourceCodePath(path string, sc *HostSourceCodePath) error {
	err := util.WriteJSONFile(path, sc, perms.LocalFilePerm)
	if err != nil {
		return fmt.Errorf("failed to write host source code path: %w", err)
	}
	return nil
}

func ReadHostSourceCodePath(path string) (*HostSourceCodePath, error) {
	sc, err := util.ReadJSONFile[HostSourceCodePath](path)
	if err != nil {
		return &HostSourceCodePath{}, fmt.Errorf("failed to read host source code path: %w", err)
	}
	return sc, nil
}

func (c *RunCollection) getSourceCodePath(entry RunID) string {
	basePath, _ := c.runBasePath(entry)
	return filepath.Join(basePath, entry.Value, SourceCodeFilename)
}

// searchInPath tries to find sourceFile (as pre-split suffixes) under a single base directory.
// It returns the first match (resolving symlinks) or "" if none was found.
func searchInPath(src string, suffixes []string) (string, error) {
	for _, suffix := range suffixes {
		candidate := filepath.Join(src, suffix)
		info, err := os.Lstat(candidate)
		if err != nil {
			continue
		}

		if info.Mode()&os.ModeSymlink != 0 {
			dest, err := filepath.EvalSymlinks(candidate)
			if err != nil {
				return "", fmt.Errorf("failed to eval symlink: %v", err)
			}
			return dest, nil
		}

		return candidate, nil
	}

	return "", nil
}

// normalizeToSuffixes takes a sourceFile path and normalizes it to a list of suffixes.
func normalizeToSuffixes(sourceFile string) []string {
	norm := filepath.Clean(filepath.FromSlash(sourceFile))

	// Detach Windows volume (e.g., "C:") -- does nothing on Unix-like systems
	vol := filepath.VolumeName(norm)
	rest := strings.TrimPrefix(norm, vol)

	// Remove leading path separators (both Unix and Windows style)
	rest = strings.TrimLeft(rest, "/\\")
	if rest == "" {
		return nil
	}

	// Pre compute suffixes
	parts := strings.FieldsFunc(rest, func(r rune) bool {
		// Handle both Unix and Windows path separators
		return r == '/' || r == '\\'
	})

	suffixes := make([]string, len(parts))
	for i := range parts {
		suffixes[i] = filepath.Join(parts[i:]...)
	}
	return suffixes
}

// SearchSourceFile locates sourceFile within sourcePaths by progressively trimming
// leading directories, joining the remainder with each path in sourcePaths, and checking for existence.
// It returns the full path of the first find, resolving symbolic links if necessary,
// or the original path if no match is found. It also returns a bool to indicate whether it matched or not.
func SearchSourceFile(sourcePaths HostSourceCodePath, sourceFile string) (string, bool) {
	suffixes := normalizeToSuffixes(sourceFile)

	found := false
	hostFile := sourceFile

	for _, dir := range sourcePaths.Paths {
		if dir == "" {
			continue
		}

		dir = filepath.Clean(filepath.FromSlash(dir))

		result, err := searchInPath(dir, suffixes)
		if err != nil {
			log.WithFields(log.Fields{
				"source": sourceFile,
				"err":    err,
			}).Warn("Failed to search in path")
		}

		if result != "" {
			hostFile = result
			found = true
			break
		}
	}

	log.WithFields(log.Fields{
		"source": sourceFile,
		"host":   hostFile,
		"found":  found,
	}).Info("Found source file")
	return hostFile, found
}
