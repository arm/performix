// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package regressiontests

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

type ImportedRunRegressionTestConfiguration struct {
	Alias          string                                     `json:"alias"`
	PrerecordedRun *PrerecordedRunRegressionTestConfiguration `json:"prerecordedRun"`
}

type PrerecordedRunRegressionTestConfiguration struct {
	ArtifactDir        string `json:"artifactDir"`
	ArtifactoryRunBase string `json:"artifactoryRunBase"`
	ArchiveName        string `json:"archiveName"`
}

type regressionTestSkipError struct {
	reason string
}

func (e *regressionTestSkipError) Error() string {
	return e.reason
}

func newRunCollectionForRegressionConfig(config *RendererRegressionTestConfigurationList, localRunsDir string, t *testing.T) (*run.RunCollection, map[string]run.RunID, error) {
	if len(config.ImportedRuns) == 0 {
		runCollection, err := run.NewRunCollection(localRunsDir)
		return runCollection, nil, err
	}

	importedRunsDir := filepath.Join(t.TempDir(), "imported-runs")
	if err := os.MkdirAll(importedRunsDir, perms.LocalDirPerm); err != nil {
		return nil, nil, fmt.Errorf("failed to create imported-runs directory %q: %w", importedRunsDir, err)
	}

	runCollection, err := run.NewRunCollectionWithSecondaryPaths(importedRunsDir, []string{localRunsDir})
	if err != nil {
		return nil, nil, err
	}

	importedRunAliases := make(map[string]run.RunID, len(config.ImportedRuns))
	for _, importedRun := range config.ImportedRuns {
		if importedRun.PrerecordedRun == nil {
			return nil, nil, fmt.Errorf("imported run alias %q does not specify a prerecordedRun source", importedRun.Alias)
		}

		archivePath, err := resolvePrerecordedRunArchive(importedRun.PrerecordedRun)
		if err != nil {
			return nil, nil, err
		}

		importedID, err := runCollection.ImportRun(archivePath)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to import prerecorded run archive %q for alias %q: %w", archivePath, importedRun.Alias, err)
		}
		importedRunAliases[importedRun.Alias] = importedID
	}

	applyImportedRunAliases(config, importedRunAliases)
	return runCollection, importedRunAliases, nil
}

func applyImportedRunAliases(config *RendererRegressionTestConfigurationList, importedRunAliases map[string]run.RunID) {
	if len(importedRunAliases) == 0 {
		return
	}

	for i := range config.Renderers {
		for j := range config.Renderers[i].Content {
			if importedID, ok := importedRunAliases[config.Renderers[i].Content[j].Value]; ok {
				config.Renderers[i].Content[j] = importedID
			}
		}
	}
}

func reverseImportedRunAliases(importedRunAliases map[string]run.RunID) map[string]string {
	if len(importedRunAliases) == 0 {
		return nil
	}

	reversed := make(map[string]string, len(importedRunAliases))
	for alias, importedID := range importedRunAliases {
		reversed[importedID.Value] = alias
	}
	return reversed
}

func ensurePathWithinRoot(root string, pathSegments ...string) (string, error) {
	cleanRoot := filepath.Clean(root)
	candidate := filepath.Join(append([]string{cleanRoot}, pathSegments...)...)

	relativePath, err := filepath.Rel(cleanRoot, candidate)
	if err != nil {
		return "", fmt.Errorf("failed to resolve prerecorded-run cache path %q relative to %q: %w", candidate, cleanRoot, err)
	}
	if relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("prerecorded-run cache path %q escapes cache root %q", candidate, cleanRoot)
	}
	return candidate, nil
}

func resolvePrerecordedRunArchive(config *PrerecordedRunRegressionTestConfiguration) (string, error) {
	artifactDir := strings.TrimSpace(config.ArtifactDir)
	if artifactDir == "" {
		return "", fmt.Errorf("prerecorded run config must set artifactDir")
	}

	archiveName := strings.TrimSpace(config.ArchiveName)
	if archiveName == "" {
		archiveName = "latest.zip"
	}

	cacheDir := strings.TrimSpace(os.Getenv("PERFORMIX_REGRESSIONTEST_PRERECORDED_RUN_CACHE_DIR"))
	if cacheDir == "" {
		userCacheDir, err := os.UserCacheDir()
		if err != nil {
			return "", fmt.Errorf("failed to resolve user cache directory for prerecorded regression runs: %w", err)
		}
		cacheDir = filepath.Join(userCacheDir, "performix", "apap-engine-regressiontests", "prerecorded-runs")
	}
	cacheDir = filepath.Clean(cacheDir)

	artifactoryRunBase := resolvePrerecordedRunBase(config.ArtifactoryRunBase)
	archivePath, err := ensurePathWithinRoot(
		cacheDir,
		filepath.FromSlash(strings.Trim(artifactoryRunBase, "/")),
		filepath.FromSlash(strings.Trim(artifactDir, "/")),
		archiveName,
	)
	if err != nil {
		return "", err
	}
	exists, err := util.PathExists(archivePath)
	if err != nil {
		return "", err
	}
	if exists {
		return archivePath, nil
	}

	token := strings.TrimSpace(os.Getenv("ARTIFACTORY_API_TOKEN"))
	if token == "" {
		if util.GetEnvBool("PERFORMIX_REGRESSIONTEST_REQUIRE_IMPORTED_RUNS") {
			return "", fmt.Errorf(
				"prerecorded run %q/%s is not cached locally and ARTIFACTORY_API_TOKEN is not set",
				artifactDir,
				archiveName,
			)
		}
		return "", &regressionTestSkipError{
			reason: fmt.Sprintf(
				"prerecorded run %q/%s is not cached locally and ARTIFACTORY_API_TOKEN is not set; set PERFORMIX_REGRESSIONTEST_PRERECORDED_RUN_CACHE_DIR or expose ARTIFACTORY_API_TOKEN to exercise imported-run regressions",
				artifactDir,
				archiveName,
			),
		}
	}

	archiveDir, err := ensurePathWithinRoot(
		cacheDir,
		filepath.FromSlash(strings.Trim(artifactoryRunBase, "/")),
		filepath.FromSlash(strings.Trim(artifactDir, "/")),
	)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(archiveDir, perms.LocalDirPerm); err != nil {
		return "", fmt.Errorf("failed to create prerecorded-run cache directory %q: %w", archiveDir, err)
	}

	sourcePath := strings.Trim(artifactoryRunBase, "/") + "/" + strings.Trim(artifactDir, "/") + "/" + archiveName
	archiveURL := strings.TrimRight(resolveArtifactoryBaseURL(), "/") + "/" + sourcePath

	req, err := http.NewRequest(http.MethodGet, archiveURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to construct prerecorded run request %q: %w", archiveURL, err)
	}
	req.Header.Set("X-JFrog-Art-Api", token)

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to download prerecorded run %q: %w", archiveURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("failed to download prerecorded run %q: HTTP %s: %s", archiveURL, resp.Status, strings.TrimSpace(string(body)))
	}

	tempPath, err := ensurePathWithinRoot(
		cacheDir,
		filepath.FromSlash(strings.Trim(artifactoryRunBase, "/")),
		filepath.FromSlash(strings.Trim(artifactDir, "/")),
		archiveName+".tmp",
	)
	if err != nil {
		return "", err
	}
	out, err := os.OpenFile(tempPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perms.LocalFilePerm)
	if err != nil {
		return "", fmt.Errorf("failed to create prerecorded-run cache file %q: %w", tempPath, err)
	}
	if _, err := io.Copy(out, resp.Body); err != nil {
		out.Close()
		return "", fmt.Errorf("failed to write prerecorded-run cache file %q: %w", tempPath, err)
	}
	if err := out.Close(); err != nil {
		return "", fmt.Errorf("failed to close prerecorded-run cache file %q: %w", tempPath, err)
	}
	if err := os.Rename(tempPath, archivePath); err != nil {
		return "", fmt.Errorf("failed to move cached prerecorded run into place at %q: %w", archivePath, err)
	}

	return archivePath, nil
}

func resolvePrerecordedRunBase(configuredBase string) string {
	if value := strings.TrimSpace(configuredBase); value != "" {
		return value
	}
	if value := strings.TrimSpace(os.Getenv("PERFORMIX_REGRESSIONTEST_ARTIFACTORY_RUN_BASE")); value != "" {
		return value
	}
	return "its.apx-prerecorded-runs/manual"
}

func resolveArtifactoryBaseURL() string {
	if value := strings.TrimSpace(os.Getenv("PERFORMIX_REGRESSIONTEST_ARTIFACTORY_BASE_URL")); value != "" {
		return value
	}
	if value := strings.TrimSpace(os.Getenv("ARTIFACTORY_BASE_URL")); value != "" {
		return value
	}
	return "https://artifactory.arm.com/artifactory"
}
