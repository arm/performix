// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package completion

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	engine_target "github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

func TestCompleteRunIDsReturnsMatchingDirectories(t *testing.T) {
	t.Cleanup(viper.Reset)

	dataDir := t.TempDir()
	viper.Set("data-dir", dataDir)

	runDir := filepath.Join(dataDir, "runs")
	mustMkdir(t, filepath.Join(runDir, "run-1001"))
	mustMkdir(t, filepath.Join(runDir, "run-2002"))
	// Non-directory entries should be ignored
	mustWriteFile(t, filepath.Join(runDir, "not-a-dir"), []byte("irrelevant"))

	suggestions, directive := CompleteRunIDs(nil, nil, "run-2")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("expected directive %v, got %v", cobra.ShellCompDirectiveNoFileComp, directive)
	}

	if len(suggestions) != 1 || suggestions[0] != "run-2002" {
		t.Fatalf("expected only run-2002 suggestion, got %v", suggestions)
	}

	suggestions, directive = CompleteRunIDs(nil, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("expected directive %v, got %v", cobra.ShellCompDirectiveNoFileComp, directive)
	}

	slices.Sort(suggestions)
	expected := []string{"run-1001", "run-2002"}
	if !slices.Equal(suggestions, expected) {
		t.Fatalf("expected suggestions %v, got %v", expected, suggestions)
	}
}

func TestCompleteRunIDsMissingRunsDirectory(t *testing.T) {
	t.Cleanup(viper.Reset)

	dataDir := filepath.Join(t.TempDir(), "does-not-exist")
	viper.Set("data-dir", dataDir)

	suggestions, directive := CompleteRunIDs(nil, nil, "")
	if directive != cobra.ShellCompDirectiveError {
		t.Fatalf("expected directive %v, got %v", cobra.ShellCompDirectiveError, directive)
	}
	if suggestions != nil {
		t.Fatalf("expected nil suggestions, got %v", suggestions)
	}
}

func TestCompleteTargetNamesReturnsMatches(t *testing.T) {
	tempFile := filepath.Join(t.TempDir(), "targets.json")
	originalPath := engine_target.DefaultTargetFilepath
	engine_target.DefaultTargetFilepath = tempFile
	t.Cleanup(func() {
		engine_target.DefaultTargetFilepath = originalPath
	})

	targets := map[string]engine_target.JSONTarget{
		"alpha": {Value: &engine_target.JSONLocalTarget{}},
		"bravo": {
			Value: &engine_target.JSONSSHTarget{Jumps: []engine_target.JSONSSHHostConfig{{
				Host:               "10.0.0.1",
				Port:               22,
				Username:           "user",
				PrivateKeyFilename: "/tmp/key",
			}}},
		},
		"beta": {
			Value: &engine_target.JSONSSHTarget{Jumps: []engine_target.JSONSSHHostConfig{{
				Host:               "10.0.0.2",
				Port:               22,
				Username:           "admin",
				PrivateKeyFilename: "/tmp/key2",
			}}},
		},
	}

	writeTargetConfigFile(t, tempFile, targets, "alpha")

	suggestions, directive := CompleteTargetNames(nil, nil, "b")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("expected directive %v, got %v", cobra.ShellCompDirectiveNoFileComp, directive)
	}

	slices.Sort(suggestions)
	expected := []string{"beta", "bravo"}
	if !slices.Equal(suggestions, expected) {
		t.Fatalf("expected suggestions %v, got %v", expected, suggestions)
	}

	suggestions, directive = CompleteTargetNames(nil, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("expected directive %v, got %v", cobra.ShellCompDirectiveNoFileComp, directive)
	}

	slices.Sort(suggestions)
	expected = []string{"alpha", "beta", "bravo"}
	if util.IsLocalhostSupportedPlatform() {
		expected = append(expected, engine_target.LocalhostName)
	}
	if !slices.Equal(suggestions, expected) {
		t.Fatalf("expected suggestions %v, got %v", expected, suggestions)
	}
}

func TestCompleteTargetNamesInvalidConfig(t *testing.T) {
	tempFile := filepath.Join(t.TempDir(), "targets.json")
	if err := os.WriteFile(tempFile, []byte("not json"), 0o644); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}

	originalPath := engine_target.DefaultTargetFilepath
	engine_target.DefaultTargetFilepath = tempFile
	t.Cleanup(func() {
		engine_target.DefaultTargetFilepath = originalPath
	})

	suggestions, directive := CompleteTargetNames(nil, nil, "")
	if directive != cobra.ShellCompDirectiveError {
		t.Fatalf("expected directive %v, got %v", cobra.ShellCompDirectiveError, directive)
	}
	if suggestions != nil {
		t.Fatalf("expected nil suggestions, got %v", suggestions)
	}
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, perms.LocalDirPerm); err != nil {
		t.Fatalf("failed to create dir %s: %v", dir, err)
	}
}

func mustWriteFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), perms.LocalDirPerm); err != nil {
		t.Fatalf("failed to create parent dir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, data, perms.LocalFilePerm); err != nil {
		t.Fatalf("failed to write file %s: %v", path, err)
	}
}

func writeTargetConfigFile(t *testing.T, path string, targets map[string]engine_target.JSONTarget, defaultName string) {
	t.Helper()
	payload := struct {
		SchemaVersion string                              `json:"schema_version"`
		Default       string                              `json:"default"`
		Targets       map[string]engine_target.JSONTarget `json:"targets"`
	}{
		SchemaVersion: "1.0.0",
		Default:       defaultName,
		Targets:       targets,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal config: %v", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), perms.LocalDirPerm); err != nil {
		t.Fatalf("failed to create parent dir for config: %v", err)
	}

	if err := os.WriteFile(path, data, perms.LocalFilePerm); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}
}
