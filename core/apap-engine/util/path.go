// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/bmatcuk/doublestar"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
)

var metachar = '*'
var unsupportedMetachars = "?[]{}"

// PathExists returns a bool indicating whether the specified path exists
// or not. If we fail to check existence of the file, we return the error.
func PathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// IsChildPath reports whether childPath resolves to a path strictly below basePath.
// It returns false for basePath itself, parent paths, and paths that cannot be expressed as a relative child.
func IsChildPath(basePath string, childPath string) bool {
	rel, err := filepath.Rel(basePath, childPath)
	if err != nil {
		return false
	}
	if rel == "." || rel == ".." {
		return false
	}
	// If path escapes the base path
	if strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	// Relative path must not be absolute
	if filepath.IsAbs(rel) {
		return false
	}
	return true
}

func splitAtFirstWildcardSegment(path string) (prefix string, suffix string, ok bool) {
	segments := strings.Split(path, "/")
	for i, segment := range segments {
		if strings.ContainsRune(segment, rune(metachar)) {
			if i == 0 {
				return "", strings.Join(segments[i:], "/"), true
			}
			if i == 1 && segments[0] == "" {
				return "/", strings.Join(segments[i:], "/"), true
			}
			return strings.Join(segments[:i], "/"), strings.Join(segments[i:], "/"), true
		}
	}

	return path, "", false
}

func joinPrefixAndDelta(prefix string, delta string) string {
	// can't use path.Join() because that cleans the path and we need to
	// preserve ../ for validation
	switch {
	case prefix == "":
		return delta
	case delta == "":
		return prefix
	default:
		return prefix + "/" + delta
	}
}

// RemapGlobbedPath composes a local concrete path by matching the concrete remote
// path against the remote glob template and replacing the concrete remote prefix
// with the local prefix when both templates share the same wildcard-containing suffix.
// local - contains the local destination path
// remote - contains the expanded glob path
// remoteBase - contains the original remote path before expansion
//
// If remoteBase contains any glob, local MUST contain the same wildcard-containing
// suffix, starting at the first path segment that contains a "*".
func RemapGlobbedPath(local, remote, remoteBase string) (string, error) {
	// Clean and convert to slashes internally - will reconvert back to appropriate local path
	// separator before returning
	normalisedLocal := ForceToSlash(local)
	normalisedRemote := ForceToSlash(remote)
	normalisedRemoteBase := ForceToSlash(remoteBase)

	// Check that no disallowed metachars are present in any path, and that no paths are directories
	for _, p := range []string{normalisedLocal, normalisedRemote, normalisedRemoteBase} {
		if strings.ContainsAny(p, unsupportedMetachars) {
			return "", fmt.Errorf("'%v' contains unsupported meta character(s) ('%v')", p, unsupportedMetachars)
		}
		if strings.HasSuffix(p, "/") {
			return "", fmt.Errorf("'%v' is a directory", p)
		}
	}

	// Clean paths
	cleanLocal := ForceToSlash(filepath.Clean(normalisedLocal))
	cleanRemote := ForceToSlash(filepath.Clean(normalisedRemote))
	cleanRemoteBase := ForceToSlash(filepath.Clean(normalisedRemoteBase))

	if isAbsolutePath(cleanRemote) != isAbsolutePath(cleanRemoteBase) {
		return "", message.New(message.EnginePathRemapCoordinateSpaceMismatch).WithMetadata(map[string]string{
			"remotePath": remote,
			"remoteBase": remoteBase,
		})
	}

	// Check that remote is a concrete path (doesn't contain any metachars)
	if containsMetachar(cleanRemote) {
		return "", fmt.Errorf("remote path '%v' must be concrete (cannot contain '%v')", remote, string(metachar))
	}

	// Handle case where remoteBase is non-globbed
	if !containsMetachar(cleanRemoteBase) {
		if cleanRemote != cleanRemoteBase {
			return "", fmt.Errorf("remote '%v' and remote base '%v' are both concrete, but not the same", remote, remoteBase)
		}
		if containsMetachar(cleanLocal) {
			return "", fmt.Errorf("remote base is concrete, but local path '%v' is globbed", local)
		}
		return filepath.FromSlash(cleanLocal), nil
	}

	localPrefix, localSuffix, localHasWildcardSegment := splitAtFirstWildcardSegment(cleanLocal)
	remotePrefix, remoteSuffix, remoteHasWildcardSegment := splitAtFirstWildcardSegment(cleanRemoteBase)
	if !localHasWildcardSegment || !remoteHasWildcardSegment || localSuffix != remoteSuffix {
		return "", message.New(message.EnginePathRemapWildcardSuffixMismatch).WithMetadata(map[string]string{
			"localPath":  local,
			"remoteBase": remoteBase,
		})
	}

	// Validate remote under base
	match, err := doublestar.Match(cleanRemoteBase, cleanRemote)
	if err != nil {
		return "", err
	}
	if !match {
		return "", message.New(message.EnginePathRemapRemotePathPatternMismatch).WithMetadata(map[string]string{
			"remotePath": remote,
			"remoteBase": remoteBase,
		})
	}

	if remotePrefix == "" {
		// When there is no concrete prefix to trim, the entire remote path is the delta.
		remappedLocal := joinPrefixAndDelta(localPrefix, cleanRemote)
		if err := validateRemappedLocalPath(cleanLocal, remappedLocal, []string{cleanRemote}); err != nil {
			return "", err
		}
		return filepath.FromSlash(filepath.Clean(remappedLocal)), nil
	}

	trimmed := strings.TrimPrefix(cleanRemote, remotePrefix)
	trimmed = strings.TrimPrefix(trimmed, "/")
	remappedLocal := joinPrefixAndDelta(localPrefix, trimmed)
	if err := validateRemappedLocalPath(cleanLocal, remappedLocal, []string{trimmed}); err != nil {
		return "", err
	}
	return filepath.FromSlash(filepath.Clean(remappedLocal)), nil
}

func containsMetachar(p string) bool {
	return strings.Contains(p, string(metachar))
}

func isAbsolutePath(p string) bool {
	// filepath.IsAbs has platform dependent behavior, so we implement our own check that works for both Windows and Unix-style paths.
	// This doesn't cover UNC paths, but that will be fine for now
	normalised := ForceToSlash(p)
	if strings.HasPrefix(normalised, "/") {
		return true
	}
	if len(normalised) >= 3 && unicode.IsLetter(rune(normalised[0])) && normalised[1] == ':' && normalised[2] == '/' {
		return true
	}
	return false
}

func validateRemappedLocalPath(localTemplate string, remappedLocal string, pathParts []string) error {
	for _, pathPart := range pathParts {
		if containsParentPathSegment(pathPart) {
			return message.New(message.EnginePathRemapPathTraversal).WithMetadata(map[string]string{
				"remappedPath":  remappedLocal,
				"pathComponent": pathPart,
			})
		}
	}

	if isAbsolutePath(localTemplate) {
		return nil
	}

	cleanedRemappedLocal := ForceToSlash(filepath.Clean(remappedLocal))
	if isAbsolutePath(cleanedRemappedLocal) {
		return message.New(message.EnginePathRemapRelativeTemplateAbsolutePath).WithMetadata(map[string]string{
			"remappedPath":  remappedLocal,
			"localTemplate": localTemplate,
		})
	}
	if cleanedRemappedLocal == ".." || strings.HasPrefix(cleanedRemappedLocal, "../") {
		return message.New(message.EnginePathRemapRelativeTemplateBaseEscape).WithMetadata(map[string]string{
			"remappedPath":  remappedLocal,
			"localTemplate": localTemplate,
		})
	}

	return nil
}

func containsParentPathSegment(p string) bool {
	for _, segment := range strings.Split(p, "/") {
		if segment == ".." {
			return true
		}
	}
	return false
}

func ForceToSlash(p string) string {
	return strings.ReplaceAll(p, `\`, "/")
}

// CanonicalPath returns an absolute path and resolves symlinks when possible.
// If symlink evaluation fails, the absolute path is returned as a best effort.
func CanonicalPath(path string) string {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return path
	}

	resolvedPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return absPath
	}
	return resolvedPath
}

// MatchesAny checks whether the file path matches any of the specified globbed patterns.
// Note that escaping of metacharacters in the globbed patterns is not supported.
func MatchesAny(filePath string, patterns []string) (bool, error) {
	cleanedPath := ForceToSlash(filepath.Clean(ForceToSlash(filePath)))
	for _, pattern := range patterns {
		cleanedPattern := ForceToSlash(filepath.Clean(ForceToSlash(pattern)))
		shouldExclude, err := doublestar.Match(cleanedPattern, cleanedPath)
		if err != nil {
			return false, err
		}
		if shouldExclude {
			return true, nil
		}
	}
	return false, nil
}
