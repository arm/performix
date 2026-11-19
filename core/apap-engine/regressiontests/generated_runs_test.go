// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package regressiontests

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/regressiontests/fixtures/jfr"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

type generatedFixture interface {
	Name() string
	GenerateRun(rootDir string) error
}

var generatedRunFixtures = map[string]string{}

func TestMain(m *testing.M) {
	os.Exit(runRegressionTests(m))
}

func runRegressionTests(m *testing.M) int {
	retain := os.Getenv("APX_KEEP_GENERATED_TEST_DATA") == "1"
	root := filepath.Join("test-data", "runs", "generated")
	if retain {
		if err := os.RemoveAll(root); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	} else {
		var err error
		root, err = os.MkdirTemp("", "performix-regression-runs-")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		defer os.RemoveAll(root)
	}

	fixtures := []generatedFixture{
		jfr.ExecutionSamples{},
	}
	// Generate each registered fixture once for this package.
	for _, fixture := range fixtures {
		name := fixture.Name()
		if _, exists := generatedRunFixtures[name]; name == "" || exists {
			fmt.Fprintf(os.Stderr, "empty or duplicate fixture name: %q\n", name)
			return 1
		}
		path := filepath.Join(root, name)
		if err := fixture.GenerateRun(path); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		generatedRunFixtures[name] = path
	}
	if retain {
		fmt.Printf("Retained generated Runs: %s\n", root)
	}
	return m.Run()
}

func TestGeneratedRunAliasesShouldResolveToIndependentRuns(t *testing.T) {
	// Given: two instances must get independent IDs and resolve through their aliases.
	root := t.TempDir()
	path := filepath.Join(root, "tests", "generated.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	require.NoError(t, os.WriteFile(path, []byte(`{
  "importedRuns": [
    {"alias": "before", "generatedRun": "jfr_execution_samples"},
    {"alias": "after", "generatedRun": "jfr_execution_samples"}
  ],
  "renderers": [{"content": [{"value":"before"}, {"value":"after"}, {"value":"local_run"}]}]
}`), 0600))

	// When
	resources, err := loadRendererTestResources(path, t)

	// Then
	require.NoError(t, err)
	content := resources.config.Renderers[0].Content
	require.NotEqual(t, content[0], content[1])
	require.Equal(t, "local_run", content[2].Value)
	for index, alias := range []string{"before", "after"} {
		require.Equal(t, alias, resources.importedRunAliases[content[index].Value])
		model, err := resources.runCollection.LoadRun(content[index])
		require.NoError(t, err)
		files, err := model.FindComponents(jfr.ParquetRoot + "/" + jfr.ObservationPattern)
		require.NoError(t, err)
		require.Len(t, files, 2)
	}

	// When: remove one partition from the first copy.
	before, err := resources.runCollection.LoadRun(content[0])
	require.NoError(t, err)
	files, err := before.FindComponents(jfr.ParquetRoot + "/" + jfr.ObservationPattern)
	require.NoError(t, err)
	require.NoError(t, os.Remove(files[0].AbsolutePath))
	// Then
	after, err := resources.runCollection.LoadRun(content[1])
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(after.BasePath(), files[0].RelativePath))
	require.FileExists(t, filepath.Join(generatedRunFixtures["jfr_execution_samples"], files[0].RelativePath))
}

func TestInvalidRunSourcesShouldBeRejected(t *testing.T) {
	for _, test := range []struct {
		name   string
		source string
		error  string
	}{
		{"a run without a source should be rejected", `[{"alias":"run"}]`, "exactly one"},
		{"a run with two sources should be rejected", `[{"alias":"run","generatedRun":"jfr_execution_samples","prerecordedRun":{}}]`, "exactly one"},
		{"an unknown generator should be rejected", `[{"alias":"run","generatedRun":"unknown"}]`, "unknown generated run fixture"},
		{"a run without an alias should be rejected", `[{"generatedRun":"jfr_execution_samples"}]`, "must specify an alias"},
		{"duplicate run aliases should be rejected", `[{"alias":"run","generatedRun":"jfr_execution_samples"},{"alias":"run","generatedRun":"jfr_execution_samples"}]`, "duplicate imported run alias"},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Given
			var config RendererRegressionTestConfigurationList
			require.NoError(t, json.Unmarshal([]byte(`{"importedRuns":`+test.source+`}`), &config))
			// When
			_, _, err := newRunCollectionForRegressionConfig(&config, t.TempDir(), t)
			// Then
			require.ErrorContains(t, err, test.error)
		})
	}
}

func TestLocalRunsShouldRemainAvailableAlongsideGeneratedRuns(t *testing.T) {
	// Given: the shared source is already an existing local Run.
	source := generatedRunFixtures["jfr_execution_samples"]
	root := filepath.Dir(source)
	config := &RendererRegressionTestConfigurationList{
		ImportedRuns: []ImportedRunRegressionTestConfiguration{{Alias: "generated", GeneratedRun: "jfr_execution_samples"}},
	}
	// When
	collection, _, err := newRunCollectionForRegressionConfig(config, root, t)
	// Then
	require.NoError(t, err)
	_, err = collection.LoadRun(run.RunID{Value: filepath.Base(source)})
	require.NoError(t, err)
}
