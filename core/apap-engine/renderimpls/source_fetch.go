// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"container/list"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"sync"

	duckdb "github.com/duckdb/duckdb-go/v2"

	"github.com/Arm-Debug/apap-cli/apap-engine/agent"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/sourcecontent"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/targetsession"
	targetagentproto "github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

const (
	// These failures are detected while resolving the requested source IDs,
	// before files are passed to SourceFilesFetcher.
	sourceContentsFailureInvalidSourceFileID = "invalid_source_file_id"
	sourceContentsFailureSourceFileNotFound  = "source_file_not_found"
)

// lruCache is a simple LRU for source content.
type lruCache struct {
	capacity int
	mu       sync.Mutex
	ll       *list.List
	items    map[string]*list.Element
}

type cacheEntry struct {
	key     string
	content string
}

func newLRU(cap int) *lruCache {
	return &lruCache{
		capacity: cap,
		ll:       list.New(),
		items:    make(map[string]*list.Element),
	}
}

func (c *lruCache) get(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ele, ok := c.items[key]; ok {
		c.ll.MoveToFront(ele)
		return ele.Value.(cacheEntry).content, true
	}
	return "", false
}

func (c *lruCache) add(key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ele, ok := c.items[key]; ok {
		ele.Value = cacheEntry{key: key, content: value}
		c.ll.MoveToFront(ele)
		return
	}
	ele := c.ll.PushFront(cacheEntry{key: key, content: value})
	c.items[key] = ele
	if c.capacity > 0 && c.ll.Len() > c.capacity {
		if tail := c.ll.Back(); tail != nil {
			c.ll.Remove(tail)
			ent := tail.Value.(cacheEntry)
			delete(c.items, ent.key)
		}
	}
}

// sourceFetchContext bundles shared state used by the UDF.
type sourceFetchContext struct {
	targetSessions targetsession.TargetSessionProvider
	runTargets     map[string]target.Target
	cache          *lruCache
	retrieveFile   func(ctx context.Context, client targetagentproto.TargetAgentClient, remotePath string, compress bool, reservedCapacity int, prog agent.ReportProgress) ([]byte, error)
}

// common metadata helper to reduce repetition.
func (s *sourceFetchContext) meta(runID string, sfID int64, extra map[string]string) map[string]string {
	m := map[string]string{
		"run_id":         runID,
		"source_file_id": fmt.Sprintf("%d", sfID),
	}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func (s *sourceFetchContext) hostFetch(runID string, sfID int64, hostVal any) (string, error) {
	hostPath, ok := hostVal.(string)
	if !ok {
		return "", s.invalidSourceValue(runID, sfID, map[string]string{
			"field": "host_location",
			"type":  fmt.Sprintf("%T", hostVal),
		})
	}
	text, err := sourcecontent.FetchHostFile(hostPath)
	if err != nil {
		return "", s.hostReadError(runID, sfID, map[string]string{
			"path": hostPath,
		}, err)
	}
	return text, nil
}

func (s *sourceFetchContext) targetFetch(runID string, sfID int64, targetVal any) (string, error) {
	if s.targetSessions == nil || s.runTargets == nil {
		return "", s.targetSessionRequired(runID, sfID, targetVal)
	}
	tgt := s.runTargets[runID]
	if tgt == nil {
		return "", s.targetSessionRequired(runID, sfID, targetVal)
	}
	ctx := context.Background()
	ts, err := s.targetSessions.TargetSession(tgt)
	if err != nil {
		return "", s.targetSessionRequired(runID, sfID, targetVal, err)
	}
	if _, err := ts.Connect(ctx); err != nil {
		return "", s.targetSessionRequired(runID, sfID, targetVal, err)
	}
	agentConn, err := ts.TargetAgent(ctx)
	if err != nil {
		var msg message.Message
		if errors.As(err, &msg) && msg.Code() == message.EngineAgentConnectionCreatorAgentNotDeployed {
			return "", err
		}
		return "", s.targetSessionRequired(runID, sfID, targetVal, err)
	}

	// At this point, the TargetPlaform should be valid, just return err in this defensive case
	platform, err := ts.TargetPlatform()
	if err != nil {
		return "", err
	}

	pathStr, ok := targetVal.(string)
	if !ok {
		return "", s.invalidSourceValue(runID, sfID, map[string]string{
			"field": "target_location",
			"type":  fmt.Sprintf("%T", targetVal),
		})
	}
	text, err := sourcecontent.FetchTargetFile(ctx, agentConn.Client, platform, pathStr, s.retrieveFile)
	if err != nil {
		var msg message.Message
		if errors.As(err, &msg) && msg.Code() == message.EngineAgentConnectionCreatorAgentNotDeployed {
			return "", err
		}
		return "", s.targetReadError(runID, sfID, map[string]string{
			"path": pathStr,
		}, err)
	}
	return text, nil
}

func (s *sourceFetchContext) fetch(runID string, sfID int64, hostVal any, targetVal any) (string, error) {
	cacheKey := fmt.Sprintf("%s:%d", runID, sfID)
	if s.cache != nil {
		if cached, ok := s.cache.get(cacheKey); ok {
			return cached, nil
		}
	}

	// Host path takes precedence.
	if hostVal != nil && hostVal != "" {
		text, err := s.hostFetch(runID, sfID, hostVal)
		if err == nil && s.cache != nil {
			s.cache.add(cacheKey, text)
		}
		return text, err
	}

	if targetVal != nil {
		text, err := s.targetFetch(runID, sfID, targetVal)
		if err == nil && s.cache != nil {
			s.cache.add(cacheKey, text)
		}
		return text, err
	}

	return "", s.invalidSourceValue(runID, sfID, map[string]string{
		"field": "source_location",
	})
}

func (s *sourceFetchContext) invalidSourceValue(runID string, sfID int64, extra map[string]string) message.Message {
	return message.New(message.EngineRenderSourceContentInvalidSourceValue).
		WithMetadata(s.meta(runID, sfID, extra))
}

func (s *sourceFetchContext) targetSessionRequired(runID string, sfID int64, targetVal any, causes ...error) message.Message {
	return message.New(message.EngineRenderSourceContentTargetSessionRequired).
		WithMetadata(s.meta(runID, sfID, map[string]string{
			"path": fmt.Sprint(targetVal),
		})).
		WithCause(errors.Join(causes...))
}

func (s *sourceFetchContext) targetReadError(runID string, sfID int64, extra map[string]string, causes ...error) message.Message {
	return message.New(message.EngineRenderSourceContentTargetReadError).
		WithMetadata(s.meta(runID, sfID, extra)).
		WithCause(errors.Join(causes...))
}

func (s *sourceFetchContext) hostReadError(runID string, sfID int64, extra map[string]string, causes ...error) message.Message {
	return message.New(message.EngineRenderSourceContentHostReadError).
		WithMetadata(s.meta(runID, sfID, extra)).
		WithCause(errors.Join(causes...))
}

func mustTypeInfo(t duckdb.Type) duckdb.TypeInfo {
	ti, err := duckdb.NewTypeInfo(t)
	if err != nil {
		panic(err)
	}
	return ti
}

func mustListTypeInfo(child duckdb.TypeInfo) duckdb.TypeInfo {
	typeInfo, err := duckdb.NewListInfo(child)
	if err != nil {
		panic(err)
	}
	return typeInfo
}

func mustStructEntry(typeInfo duckdb.TypeInfo, name string) duckdb.StructEntry {
	entry, err := duckdb.NewStructEntry(typeInfo, name)
	if err != nil {
		panic(err)
	}
	return entry
}

func mustStructTypeInfo(first duckdb.StructEntry, others ...duckdb.StructEntry) duckdb.TypeInfo {
	typeInfo, err := duckdb.NewStructInfo(first, others...)
	if err != nil {
		panic(err)
	}
	return typeInfo
}

// createSourceFilesUnionView builds a union view over per-run source_files tables tagged with run_id.
func createSourceFilesUnionView(db *sql.Conn, viewName string, runTables map[string]string) error {
	var parts []string
	for runID, table := range runTables {
		parts = append(parts, fmt.Sprintf("SELECT '%s' AS run_id, source_file_id, host_location, target_location FROM %s", runID, table))
	}
	stmt := fmt.Sprintf("CREATE OR REPLACE VIEW %s AS %s;", viewName, strings.Join(parts, " UNION ALL "))
	_, err := db.ExecContext(context.Background(), stmt)
	return err
}

// loadSourceContentUDF implements the on-demand source fetch.
// Inputs: source_file_id (INT), host_location (VARCHAR), target_location (VARCHAR), run_id (VARCHAR).
type loadSourceContentUDF struct {
	ctx *sourceFetchContext
}

// loadSourceContentsUDF loads multiple source files in one request. It returns
// the content or failure reasons for each source ID requested through the
// public load_source_contents table macro.
type loadSourceContentsUDF struct {
	targetSessions targetsession.TargetSessionProvider
	runTargets     map[string]target.Target
}

func (f *loadSourceContentsUDF) Config() duckdb.ScalarFuncConfig {
	bigintType := mustTypeInfo(duckdb.TYPE_BIGINT)
	booleanType := mustTypeInfo(duckdb.TYPE_BOOLEAN)
	varcharType := mustTypeInfo(duckdb.TYPE_VARCHAR)
	varcharListType := mustListTypeInfo(varcharType)
	descriptorType := mustStructTypeInfo(
		mustStructEntry(bigintType, "source_file_id"),
		mustStructEntry(booleanType, "source_file_found"),
		mustStructEntry(varcharType, "host_location"),
		mustStructEntry(varcharType, "target_location"),
	)
	resultType := mustStructTypeInfo(
		mustStructEntry(bigintType, "source_file_id"),
		mustStructEntry(varcharType, "content"),
		mustStructEntry(varcharListType, "failure_reasons"),
	)

	return duckdb.ScalarFuncConfig{
		InputTypeInfos: []duckdb.TypeInfo{
			varcharType,
			mustListTypeInfo(descriptorType),
		},
		ResultTypeInfo:      mustListTypeInfo(resultType),
		Volatile:            true,
		SpecialNullHandling: true,
	}
}

func (f *loadSourceContentsUDF) Executor() duckdb.ScalarFuncExecutor {
	return duckdb.ScalarFuncExecutor{
		RowContextExecutor: func(ctx context.Context, values []driver.Value) (any, error) {
			return f.execute(ctx, values)
		},
	}
}

func (f *loadSourceContentsUDF) execute(ctx context.Context, values []driver.Value) ([]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(values) != 2 {
		return nil, fmt.Errorf("load source contents: expected 2 arguments, got %d", len(values))
	}
	runID, ok := values[0].(string)
	if !ok || runID == "" {
		return nil, fmt.Errorf("load source contents: invalid run ID")
	}

	descriptors, ok := values[1].([]any)
	if values[1] != nil && !ok {
		return nil, fmt.Errorf("load source contents: invalid source_file_descriptors value of type %T", values[1])
	}

	results := make([]any, len(descriptors))
	files := make([]sourcecontent.SourceFile, 0, len(descriptors))
	fetchedSourceFileIDs := make([]int64, 0, len(descriptors))
	resultIndexesBySourceFileID := make(map[int64][]int, len(descriptors))
	// Invalid and unknown IDs become independent result rows. Valid duplicate
	// IDs share one fetch while retaining a result in each original slot.
	for i, value := range descriptors {
		descriptor, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("load source contents: invalid source file descriptor at index %d", i)
		}

		sourceFileID, validID := descriptor["source_file_id"].(int64)
		if !validID {
			results[i] = sourceContentsResult(nil, nil, []string{sourceContentsFailureInvalidSourceFileID})
			continue
		}

		sourceFileFound, validFound := descriptor["source_file_found"].(bool)
		if !validFound {
			return nil, fmt.Errorf("load source contents: invalid source_file_found value at index %d", i)
		}
		if !sourceFileFound {
			results[i] = sourceContentsResult(sourceFileID, nil, []string{sourceContentsFailureSourceFileNotFound})
			continue
		}

		hostLocation, err := sourceContentsString(descriptor["host_location"], "host_location", i)
		if err != nil {
			return nil, err
		}
		targetLocation, err := sourceContentsString(descriptor["target_location"], "target_location", i)
		if err != nil {
			return nil, err
		}
		// Prefer the mapped host file while retaining the target path as a
		// fallback when the host mapping is missing, unreadable, or mismatched.
		locations := []sourcecontent.SourceFileLocation{{
			Location: sourcecontent.SourceLocationHost,
			Path:     hostLocation,
		}}
		if targetLocation != "" {
			locations = append(locations, sourcecontent.SourceFileLocation{
				Location: sourcecontent.SourceLocationTarget,
				Path:     targetLocation,
			})
		}
		if resultIndexes, found := resultIndexesBySourceFileID[sourceFileID]; found {
			resultIndexesBySourceFileID[sourceFileID] = append(resultIndexes, i)
			continue
		}
		resultIndexesBySourceFileID[sourceFileID] = []int{i}
		files = append(files, sourcecontent.SourceFile{Locations: locations})
		fetchedSourceFileIDs = append(fetchedSourceFileIDs, sourceFileID)
	}

	if len(files) == 0 {
		return results, nil
	}
	fetcher := sourcecontent.NewSourceFilesFetcher(
		ctx,
		f.runTargets[runID],
		f.targetSessions,
	)
	contents := fetcher(files)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(contents) != len(files) {
		return nil, fmt.Errorf("load source contents: fetched %d results for %d files", len(contents), len(files))
	}
	for i, content := range contents {
		sourceFileID := fetchedSourceFileIDs[i]
		var result any
		if content.LoadedLocation.Location != "" {
			// Intermediate location failures are not useful once a fallback
			// location has supplied the requested content.
			result = sourceContentsResult(sourceFileID, content.Content, []string{})
		} else {
			failureReasons := make([]string, 0, len(content.Failures))
			for _, failure := range content.Failures {
				if failure.Reason != "" {
					failureReasons = append(failureReasons, string(failure.Reason))
				}
			}
			if len(failureReasons) == 0 {
				return nil, fmt.Errorf("load source contents: source file %d failed without a reason", sourceFileID)
			}
			result = sourceContentsResult(sourceFileID, nil, failureReasons)
		}
		for _, resultIndex := range resultIndexesBySourceFileID[sourceFileID] {
			results[resultIndex] = result
		}
	}
	return results, nil
}

func sourceContentsString(value any, field string, index int) (string, error) {
	if value == nil {
		return "", nil
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("load source contents: invalid %s value at index %d", field, index)
	}
	return text, nil
}

func sourceContentsResult(sourceFileID any, content any, failureReasons []string) map[string]any {
	return map[string]any{
		"source_file_id":  sourceFileID,
		"content":         content,
		"failure_reasons": failureReasons,
	}
}

func (f *loadSourceContentUDF) Config() duckdb.ScalarFuncConfig {
	return duckdb.ScalarFuncConfig{
		InputTypeInfos: []duckdb.TypeInfo{
			mustTypeInfo(duckdb.TYPE_INTEGER),
			mustTypeInfo(duckdb.TYPE_VARCHAR),
			mustTypeInfo(duckdb.TYPE_VARCHAR),
			mustTypeInfo(duckdb.TYPE_VARCHAR),
		},
		ResultTypeInfo:      mustTypeInfo(duckdb.TYPE_VARCHAR),
		SpecialNullHandling: true,
	}
}

func (f *loadSourceContentUDF) Executor() duckdb.ScalarFuncExecutor {
	return duckdb.ScalarFuncExecutor{
		RowExecutor: func(values []driver.Value) (any, error) {
			if values[0] == nil {
				return nil, message.New(message.EngineRenderSourceContentInvalidSourceValue).
					WithMetadata(map[string]string{
						"field":          "source_file_id",
						"source_file_id": "",
						"run_id":         "",
					})
			}
			var sfID int64
			switch v := values[0].(type) {
			case int64:
				sfID = v
			case int32:
				sfID = int64(v)
			case int:
				sfID = int64(v)
			case int16:
				sfID = int64(v)
			case int8:
				sfID = int64(v)
			default:
				return nil, message.New(message.EngineRenderSourceContentInvalidSourceValue).
					WithMetadata(map[string]string{
						"field":          "source_file_id",
						"source_file_id": fmt.Sprint(values[0]),
						"run_id":         "",
					})
			}
			hostVal := values[1]
			targetVal := values[2]
			runIDVal := values[3]
			runID := ""
			if s, ok := runIDVal.(string); ok {
				runID = s
			} else if runIDVal != nil {
				runID = fmt.Sprint(runIDVal)
			}
			return f.ctx.fetch(runID, sfID, hostVal, targetVal)
		},
	}
}

// existingConnConnector lets us reuse an existing duckdb.Conn with sql.OpenDB, avoiding sqldblogger wrappers.
type existingConnConnector struct {
	conn driver.Conn
}

func (c existingConnConnector) Connect(ctx context.Context) (driver.Conn, error) { return c.conn, nil }
func (c existingConnConnector) Driver() driver.Driver                            { return duckdb.Driver{} }

// registerScalarUDFUnwrapped registers a UDF against the underlying duckdb.Conn, even if the sql.Conn is wrapped.
func registerScalarUDFUnwrapped(db *sql.Conn, name string, udf duckdb.ScalarFunc) error {
	return db.Raw(func(dc any) error {
		driverConn, ok := dc.(driver.Conn)
		if !ok {
			return fmt.Errorf("unexpected conn type %T", dc)
		}
		rawConn, err := render.GetRawDuckDBConn(driverConn)
		if err != nil {
			return err
		}

		// Use a temporary *sql.Conn that exposes the raw *duckdb.Conn to RegisterScalarUDF.
		tempDB := sql.OpenDB(existingConnConnector{conn: rawConn})
		tempConn, err := tempDB.Conn(context.Background())
		if err != nil {
			return err
		}
		// Do not close tempConn/tempDB here to avoid closing the shared raw connection.
		return duckdb.RegisterScalarUDF(tempConn, name, udf)
	})
}

// registerLoadSourceContentFunction registers the singular scalar macro and the
// plural table macro used to fetch source content. load_source_content retains
// its existing error behaviour for GUI callers. load_source_contents fetches a
// batch concurrently and returns failures per source file without aborting
// successful rows.
//
// load_source_contents accepts a run ID and a list of source file IDs. It
// returns one row per requested ID with the source content and any failure
// reasons. Successful rows contain the complete source file and an empty
// failure_reasons list; failed rows contain NULL content.
//
// Example:
//
//	SELECT source_file_id, content, failure_reasons
//	FROM load_source_contents('0123456789ab', [12, 34]);
func registerLoadSourceContentFunction(db *sql.Conn, sourceFilesViewName string, ts targetsession.TargetSessionProvider, runTargets map[string]target.Target, sessionID string) error {
	singularUDFName := fmt.Sprintf("load_source_content_udf_%s", sessionID)
	udf := &loadSourceContentUDF{
		ctx: &sourceFetchContext{
			targetSessions: ts,
			runTargets:     runTargets,
			cache:          newLRU(128),
		},
	}
	if err := registerScalarUDFUnwrapped(db, singularUDFName, udf); err != nil {
		return err
	}
	batchUDFName := fmt.Sprintf("load_source_contents_udf_%s", sessionID)
	batchUDF := &loadSourceContentsUDF{
		targetSessions: ts,
		runTargets:     runTargets,
	}
	if err := registerScalarUDFUnwrapped(db, batchUDFName, batchUDF); err != nil {
		return err
	}

	// The macro resolves each requested source ID into one descriptor. The UDF
	// returns an ordered LIST<STRUCT>, which the macro expands into primitive
	// columns that run_query can serialise.
	//nolint:gosec // view name is internal and not user-controlled.
	stmt := fmt.Sprintf(`
CREATE OR REPLACE MACRO load_source_content(run_id_param, src_id) AS (
  SELECT %s(source_file_id, host_location, target_location, run_id_param)
  FROM %s WHERE run_id = run_id_param AND source_file_id = src_id
);

CREATE OR REPLACE MACRO load_source_contents(run_id_param, source_ids) AS TABLE (
  WITH requested AS (
    SELECT source_file_id, request_index
    FROM UNNEST(source_ids::BIGINT[]) WITH ORDINALITY
      AS requested(source_file_id, request_index)
  ), descriptors AS (
    SELECT
      requested.request_index,
      requested.source_file_id,
      source_files.source_file_id IS NOT NULL AS source_file_found,
      source_files.host_location,
      source_files.target_location
    FROM requested
    LEFT JOIN %s AS source_files
      ON source_files.run_id = run_id_param
      AND source_files.source_file_id = requested.source_file_id
  ), batch AS MATERIALIZED (
    SELECT %s(
      run_id_param,
      list(
        struct_pack(
          source_file_id := source_file_id,
          source_file_found := source_file_found,
          host_location := host_location,
          target_location := target_location
        )
        ORDER BY request_index
      )
    ) AS results
    FROM descriptors
    GROUP BY run_id_param
  )
  SELECT
    result.source_file_id,
    result.content,
    result.failure_reasons
  FROM batch, UNNEST(results) AS fetched(result)
);`, singularUDFName, sourceFilesViewName, sourceFilesViewName, batchUDFName)
	_, err := db.ExecContext(context.Background(), stmt)
	return err
}
