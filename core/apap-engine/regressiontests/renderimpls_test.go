// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package regressiontests

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Arm-Debug/apap-cli/apap-engine/deploymentsupport"
	"github.com/Arm-Debug/apap-cli/apap-engine/grpcserver"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/query"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/render/sessionfactory"
	"github.com/Arm-Debug/apap-cli/apap-engine/renderimpls"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

type QueryConfig struct {
	Type           string `json:"type"`
	OverrideTarget string `json:"overrideTarget"`
	SQL            string `json:"sql"`
}

type RendererRegressionTestConfigurationList struct {
	Renderers        []RendererRegressionTestConfiguration        `json:"renderers"`
	Queries          []QueryConfig                                `json:"queries"`
	Visualizations   []VisualisationRegressionTestConfiguration   `json:"visualizations"`
	ToolIntegrations []ToolIntegrationRegressionTestConfiguration `json:"toolIntegrations"`
}

type VisualisationRegressionTestConfiguration struct {
	VisID  *string     `json:"visualizationID"`
	Config interface{} `json:"visualizationConfig"`
}

type RendererRegressionTestConfiguration struct {
	RendererName   string         `json:"rendererName"`
	RendererID     *string        `json:"rendererID"`
	RendererConfig interface{}    `json:"rendererConfig"`
	Content        []run.RunID    `json:"content"`
	ExpectedError  *ExpectedError `json:"expectedError"`
}

type ExpectedError struct {
	Contains string `json:"contains"`
}

func (e *ExpectedError) assertExpectations(t *testing.T, err error) {
	if e == nil {
		assert.NoError(t, err)
		return
	}
	assert.ErrorContains(t, err, e.Contains)
}

type QuerySpec struct {
	SQL             string
	OutputTableName string
}

type ToolIntegrationRegressionTestConfiguration struct {
	Name       string           `json:"name"`
	Version    string           `json:"version"`
	Migrations []tool.Migration `json:"migrations"`
}

const (
	captureAPCComponentName = "capture_apc"
	testConfigDir           = "test-data/tests"
)

type rendererTestResources struct {
	config        *RendererRegressionTestConfigurationList
	runCollection *run.RunCollection
	truthDir      string
}

func loadRendererTestResources(path string) (*rendererTestResources, error) {
	dirTestConfigsRoot, _ := filepath.Split(path)
	dirTestDataRoot, _ := filepath.Split(filepath.Clean(dirTestConfigsRoot))

	config, err := util.ReadJSONFile[RendererRegressionTestConfigurationList](path)
	if err != nil {
		return nil, err
	}
	if config == nil {
		return nil, fmt.Errorf("renderer regression test config %q was empty", path)
	}

	runCollection, err := run.NewRunCollection(filepath.Join(dirTestDataRoot, "runs"))
	if err != nil {
		return nil, err
	}

	testName := basenameWithoutExt(path)
	truthDir := filepath.Join(dirTestDataRoot, "truth", testName)

	return &rendererTestResources{
		config:        config,
		runCollection: runCollection,
		truthDir:      truthDir,
	}, nil
}

type regressionTestToolFactory struct {
	name       string
	version    string
	migrations []tool.Migration
}

func (f *regressionTestToolFactory) NewIntegration(*tool.IntegrationContext) (tool.ToolIntegration, error) {
	return nil, errors.New("regression test tool factory does not create integrations")
}

func (f *regressionTestToolFactory) Name() string {
	return f.name
}

func (f *regressionTestToolFactory) Version() string {
	return f.version
}

func (f *regressionTestToolFactory) Deployments() []deploymentsupport.DeploymentDeclaration {
	return nil
}

func (f *regressionTestToolFactory) GetMigrations() []tool.Migration {
	return f.migrations
}

func newTestPackageProvider(integrations []ToolIntegrationRegressionTestConfiguration) *render.FakePkgMgr {
	if len(integrations) == 0 {
		return nil
	}

	registry := tool.NewToolRegistry()
	for _, integration := range integrations {
		registry.RegisterTool(&regressionTestToolFactory{
			name:       integration.Name,
			version:    integration.Version,
			migrations: integration.Migrations,
		})
	}

	return &render.FakePkgMgr{Registry: registry}
}

func getQueryConfigsPertainingToTarget(queries []QueryConfig, targetManifestEntryName string) []QueryConfig {
	var result []QueryConfig
	for i := range queries {
		if queries[i].Type == "override" && queries[i].OverrideTarget == targetManifestEntryName {
			result = append(result, queries[i])
		}
	}
	return result
}

// generateQuerySpecsForManifestEntry iterates over the query specs in the configuration; any query specs in the
// configuration that are marked as overriding the default query for the specified manifest entry are collected and
// returned. If none are present, the default query specs are generated. If the manifest entry is hidden, the default
// query spec list is empty.
func generateQuerySpecsForManifestEntry(db *render.Database, config *RendererRegressionTestConfigurationList, entry *render.ManifestEntry) []QuerySpec {
	relevantConfigs := getQueryConfigsPertainingToTarget(config.Queries, entry.TableName())

	if len(relevantConfigs) == 0 {
		// Generate default query
		if entry.IsHidden() || entry.Info().Pending() {
			return []QuerySpec{}
		} else {
			// List all the columns in the table, ordered by rowid, so we can generate an ORDER BY clause
			// that is deterministic. This is important for the regression tests.
			// Note that this is not necessarily the order the rows were inserted in, but it
			// is deterministic and stable.

			colsQuery := fmt.Sprint("SELECT column_name FROM (DESCRIBE ", entry.TableName(), ")")
			colRows, err := db.Conn.QueryContext(context.Background(), colsQuery)
			if err != nil {
				panic(fmt.Sprintf("failed to describe table %q: %v", entry.TableName(), err))
			}
			defer colRows.Close()

			var cols []string
			for colRows.Next() {
				var colName string
				if err := colRows.Scan(&colName); err != nil {
					panic(fmt.Sprintf("failed to scan column info for table %q: %v", entry.TableName(), err))
				}
				cols = append(cols, colName)
			}

			if len(cols) == 0 {
				// No columns, so no output
				return []QuerySpec{}
			}

			// Escape column names that might be reserved words or contain special characters
			for i := range cols {
				cols[i] = `"` + strings.ReplaceAll(cols[i], `"`, `""`) + `"`
			}
			orderByClause := " ORDER BY " + strings.Join(cols, ", ")

			// If only one entry, the output table name is just the manifest entry table name; this affects the naming
			// of the file
			return []QuerySpec{
				{
					"SELECT * FROM " + entry.TableName() + orderByClause,
					entry.TableName(),
				},
			}
		}
	}

	var queries []QuerySpec
	for i := range relevantConfigs {
		// If there are multiple entries, we append _qryI to the filename
		qry := QuerySpec{
			relevantConfigs[i].SQL,
			entry.TableName() + "_qry" + strconv.Itoa(i),
		}
		queries = append(queries, qry)
	}

	return queries
}

func basenameWithoutExt(path string) string {
	_, filename := filepath.Split(path)
	return strings.TrimSuffix(filename, filepath.Ext(filename))
}

func regenTruth(truthFilePath string, table query.ProtoStructTableAccessor, t *testing.T) {
	f, err := os.OpenFile(truthFilePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perms.LocalFilePerm)
	assert.NoError(t, err)
	defer f.Close()

	for {
		rows, err := table.NextChunk()
		if err == io.EOF {
			break
		}
		assert.NoError(t, err)

		for _, row := range rows {
			b, err := json.Marshal(row.AsMap())
			assert.NoError(t, err)
			_, err = f.Write(append(b, '\n'))
			assert.NoError(t, err)
		}
	}
}

func checkTruth(truthFilePath string, table query.ProtoStructTableAccessor, t *testing.T) {
	file, err := os.Open(truthFilePath)
	assert.NoError(t, err)
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Split(bufio.ScanLines)

	var i int
	var currentChunk []*structpb.Struct
	for scanner.Scan() {
		expectedLine := scanner.Bytes()
		assert.NoError(t, scanner.Err())

		// Dequeue rows from current chunk or get a new chunk
		for len(currentChunk) == 0 {
			chunk, err := table.NextChunk()
			if err == io.EOF {
				assert.Failf(t, "not enough rows in result", "expected at least %d rows in file %s", i+1, truthFilePath)
				return
			}
			assert.NoError(t, err)
			currentChunk = chunk
		}

		row := currentChunk[0]
		currentChunk = currentChunk[1:]

		actualJSON, err := json.Marshal(row.AsMap())
		assert.NoError(t, err)
		assert.JSONEqf(t, string(expectedLine), string(actualJSON), "mismatch at line %d", i+1)
		i++
	}
	// Check for extra rows in the render table
	for {
		if len(currentChunk) > 0 {
			assert.Failf(t, "too many rows in result", "expected %d rows in file %s, but got more", i, truthFilePath)
			break
		}
		chunk, err := table.NextChunk()
		if err == io.EOF {
			break
		}
		assert.NoError(t, err)
		if len(chunk) > 0 {
			assert.Failf(t, "too many rows in result", "expected %d rows in file %s, but got more", i, truthFilePath)
			break

		}
	}
}

// Regenerate the truth file for resolved visualizations
func regenTruthResolvedVisualizations(path string, session render.Session, t *testing.T) {
	bytes := grpcserver.MarshalResolvedVisualizationsToJSON(session, true)
	err := os.WriteFile(path, bytes, perms.LocalFilePerm)
	assert.NoError(t, err)
}

// Check the truth file for resolved visualizations
func checkTruthResolvedVisualizations(path string, session render.Session, t *testing.T) {
	expectedJSON, err := os.ReadFile(path)
	assert.NoError(t, err)

	diff, err := grpcserver.JSONToResolvedVisualizationsDiff(expectedJSON, session)
	assert.NoError(t, err)
	if diff != "" {
		t.Errorf("Resolved visualizations mismatch (-want +got):\n%s", diff)
	}
}

func regenTruthManifest(truthManifestFilePath string, session render.Session, t *testing.T) {
	bytes := grpcserver.MarshalRenderManifestToJSON(session, true)
	err := os.WriteFile(truthManifestFilePath, bytes, perms.LocalFilePerm)
	assert.NoError(t, err)
}

func checkTruthManifest(truthManifestFilePath string, session render.Session, t *testing.T) {
	expectedJSON, err := os.ReadFile(truthManifestFilePath)
	assert.NoError(t, err)

	diff, err := grpcserver.JSONToManifestDiff(expectedJSON, session)
	assert.NoError(t, err)
	if diff != "" {
		t.Errorf("mismatch (-want +got):\n%s", diff)
	}
}

func clearPreviousOutput(dirCurrentTruth string) error {
	exists, err := util.PathExists(dirCurrentTruth)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}

	return os.RemoveAll(dirCurrentTruth)
}

func RunRendererTest(resources *rendererTestResources, regen bool, t *testing.T) {
	// Expect:
	//
	//  test-data/                      Root of test data dir
	//  test-data/tests/xyz.json        The test data config files; one of these is passed in path
	//  test-data/runs               	The run collection for the regression test data
	//  test-data/truth/xyz/some.json   Previous output of running xyz render regression test with a regen
	//  (optionally capture_apc components inside runs to exercise the Streamline C API path)

	config := resources.config
	cat := resources.runCollection
	dirCurrentTruth := resources.truthDir

	rendererConfigs := render.RendererConfigList{}
	// hmm this is a bit silly; maybe we should unmarshal at the API layer
	for _, c := range config.Renderers {
		configJSONBytes, err := json.Marshal(c.RendererConfig)
		assert.NoError(t, err)
		configJSON := string(configJSONBytes)
		rendererConfigs = append(rendererConfigs, render.RendererConfig{Name: c.RendererName, ID: c.RendererID, ConfigJSON: configJSON})
	}

	visConfigList := render.WidgetConfigList{}
	for _, c := range config.Visualizations {
		configJSONBytes, err := json.Marshal(c.Config)
		assert.NoError(t, err)
		configJSON := string(configJSONBytes)
		visConfigList = append(visConfigList, render.WidgetConfig{ID: c.VisID, ConfigJSON: configJSON})
	}

	rendererFactory := renderimpls.NewRegistry()
	sessionStorage := render.NewSessionStorage()
	var pkgProvider render.ToolRegistryProvider
	if len(config.ToolIntegrations) > 0 {
		pkgProvider = newTestPackageProvider(config.ToolIntegrations)
	}
	// Set a shared DB key so that all render sessions share the same underlying database;
	// while this is not the main aim of this test, it adds confidence that that multiple render
	// sessions can coexist in the same DB
	sharedDbKey := "renderimpls-shared"
	session, invocationErrors, err := render.StartRenderSession(
		context.Background(),
		&sessionfactory.Impl{DBKeyOverride: &sharedDbKey},
		&sessionStorage,
		rendererFactory,
		cat,
		config.Renderers[0].Content,
		rendererConfigs,
		visConfigList,
		&render.DuckDBFactory{},
		pkgProvider,
		nil,
	)
	assert.NoError(t, err)
	assert.Equal(t, len(invocationErrors), len(config.Renderers))
	for i, rendererConfig := range config.Renderers {
		rendererConfig.ExpectedError.assertExpectations(t, invocationErrors[i])
	}
	assert.NotNil(t, session)
	defer sessionStorage.CloseAllRenderSessions()

	if regen {
		err := clearPreviousOutput(dirCurrentTruth)
		assert.NoError(t, err)

		err = os.MkdirAll(dirCurrentTruth, perms.LocalDirPerm)
		assert.NoError(t, err)
	}

	truthManifestFile := filepath.Join(dirCurrentTruth, "manifest.json")
	truthResolvedVisualizationsFile := filepath.Join(dirCurrentTruth, "resolved_visualizations.json")
	if regen {
		regenTruthManifest(truthManifestFile, session, t)
		regenTruthResolvedVisualizations(truthResolvedVisualizationsFile, session, t)
	} else {
		checkTruthManifest(truthManifestFile, session, t)
		checkTruthResolvedVisualizations(truthResolvedVisualizationsFile, session, t)
	}

	for i := range session.Manifest().Entries() {
		querySpecs := generateQuerySpecsForManifestEntry(session.Database(), config, &session.Manifest().Entries()[i])

		for _, spec := range querySpecs {
			currentTruthFile := filepath.Join(dirCurrentTruth, spec.OutputTableName+".json")

			table, err := query.NewProtoStructTableAccessor(session.Database(), spec.SQL, query.ProtobufStructSettings{})
			assert.NoError(t, err)
			assert.NotNil(t, table)
			if err != nil {
				continue
			}
			defer table.Close()

			if regen {
				regenTruth(currentTruthFile, table, t)
			} else {
				checkTruth(currentTruthFile, table, t)
			}
		}
	}
}

func isRegenEnabled() bool {
	// If the environment variable REGEN is passed, the tests will regenerate their output instead of checking
	return util.GetEnvBool("REGEN")
}

func TestRendererRegression(t *testing.T) {
	regen := isRegenEnabled()
	if regen {
		fmt.Println("REGEN is enabled; will regenerate test data output")
	}

	// Expect json files in test-data/tests describing the regression tests to run
	dirTestConfigs, err := filepath.Abs(testConfigDir)
	assert.NoError(t, err)
	entries, err := os.ReadDir(dirTestConfigs)
	assert.NoError(t, err)

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		fullPath := filepath.Join(dirTestConfigs, entry.Name())
		t.Run(fmt.Sprintf("Regression test '%s' should pass", entry.Name()), func(t *testing.T) {
			resources, err := loadRendererTestResources(fullPath)
			if !assert.NoError(t, err) {
				return
			}

			t.Run(fmt.Sprintf("Regression test '%s' should pass for json/csv code path", entry.Name()), func(t *testing.T) {
				RunRendererTest(resources, regen, t)
			})
		})
	}
}

func TestAllRenderersAreInvokedByRegressionTests(t *testing.T) {
	if isRegenEnabled() {
		// Don't fail on the thing we just told the user to do!
		return
	}

	registry := renderimpls.NewRegistry()

	found := make(map[string]bool)
	entries, err := os.ReadDir(testConfigDir)
	assert.NoError(t, err)

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		path := filepath.Join(testConfigDir, entry.Name())

		config, err := util.ReadJSONFile[RendererRegressionTestConfigurationList](path)
		assert.NoError(t, err)
		assert.NotNil(t, config)

		for _, r := range config.Renderers {
			found[r.RendererName] = true
		}

	}

	for _, name := range registry.AvailableRendererNames() {
		assert.True(
			t,
			found[name],
			"no regression test found for renderer registered with name '%s'; please add a config to '%s' and run tests with env var REGEN=1",
			name,
			testConfigDir,
		)
	}
}

// These are some exception renderers that are allowd to have an empty output spec.
var ExceptionRenderersNoOutputSpec = []string{"SlAnalyzeRenderer", "SQL"}

func isExceptionRenderer(name string) bool {
	for _, exception := range ExceptionRenderersNoOutputSpec {
		if name == exception {
			return true
		}
	}
	return false
}

func TestAllRenderersHaveOutputSpec(t *testing.T) {
	registry := renderimpls.NewRegistry()

	for _, name := range registry.AvailableRendererNames() {
		if isExceptionRenderer(name) {
			continue
		}
		renderer, err := registry.NewRenderer(name)
		assert.NoError(t, err)
		assert.NotNil(t, renderer)

		outputSpec := renderer.GetOutputSpec()
		assert.NotNil(t, outputSpec)
		assert.NotEmpty(t, outputSpec.PortList, "renderer '%s' has empty output spec", name)
	}
}

var comparisonRendererNames = []string{
	"CompareFlatTable",
	"CompareDrilldownFlat",
	"CompareDrilldownCallStacks",
}

func TestAllRenderersHaveValidInputSpec(t *testing.T) {
	registry := renderimpls.NewRegistry()
	t.Run("Comparison renderers must have input specs", func(t *testing.T) {
		for _, name := range comparisonRendererNames {
			renderer, err := registry.NewRenderer(name)
			assert.NoError(t, err)
			inputSpec := renderer.GetInputSpec()
			assert.NotNil(t, inputSpec)
			assert.NotEmpty(t, inputSpec.PortList, "renderer '%s' is a comparison renderer but has empty input spec", name)
		}
	})
}
