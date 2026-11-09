// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// render_session.go contains the code shared by MCP tools that open and close
// render sessions. Keeping it here ensures that every caller handles render
// failures and cleanup in the same way.

package toolimpl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Arm-Debug/apap-cli/apap-cli/service/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/api/apapproto"
)

const renderSessionCleanupTimeout = 5 * time.Second

const renderTableSchemasDiscoverySQL = `SELECT table_name, column_name, data_type
FROM duckdb_columns()
WHERE schema_name = current_schema()
ORDER BY table_name, column_index
LIMIT 50 OFFSET 0`

// renderSessionResult is the JSON response for an opened render session. It
// matches InvokeRenderResponse and may include a compatibility warning from
// PrepareRender.
type renderSessionResult map[string]any

// renderTableSchema describes one relation available to run_query. The
// visualization data sources retain the logical meaning assigned by the
// recipe while name and columns describe the physical relation to query.
type renderTableSchema struct {
	Name                     string                          `json:"name"`
	Columns                  map[string]string               `json:"columns"`
	VisualizationDataSources []renderVisualizationDataSource `json:"visualization_data_sources"`
}

type renderVisualizationDataSource struct {
	VisualizationID string `json:"visualization_id"`
	DataSource      string `json:"data_source"`
}

// openedRenderSession keeps both daemon responses. The invocation describes
// the opened session, while the preparation may contain a compatibility
// warning that must be returned with it.
type openedRenderSession struct {
	Preparation *apapproto.PrepareRenderResponse
	Invocation  *apapproto.InvokeRenderResponse
}

// openedRenderSessionResult contains the opened daemon session and the data
// needed by an MCP client to query it.
type openedRenderSessionResult struct {
	Session openedRenderSession
	Details renderSessionResult
}

// openRenderSession prepares and invokes the installed recipe for one run. If
// invocation returns an error or a renderer does not complete successfully, it
// closes any returned session before returning the error.
func openRenderSession(
	ctx context.Context,
	engine apapproto.ApapClient,
	runID string,
	renderParameters map[string]*structpb.Value,
) (openedRenderSession, error) {
	var session openedRenderSession
	content := &apapproto.ContentSelection{Runs: []*apapproto.RunId{{Value: runID}}}
	prepared, err := engine.PrepareRender(ctx, &apapproto.PrepareRenderRequest{
		Content:          content,
		RenderParameters: renderParameters,
	})
	if err != nil {
		return session, fmt.Errorf("prepare render: %w", err)
	}
	if prepared == nil {
		return session, errors.New("prepare render returned no configuration")
	}
	session.Preparation = prepared

	invoked, invokeErr := engine.InvokeRender(ctx, &apapproto.InvokeRenderRequest{
		Content:             content,
		RendererConfig:      prepared.GetRenderers(),
		VisualizationConfig: prepared.GetVisualizations(),
	})
	if invoked != nil {
		session.Invocation = invoked
	}
	if invokeErr != nil {
		err = fmt.Errorf("invoke render: %w", invokeErr)
		if sessionID := strings.TrimSpace(invoked.GetSessionId()); sessionID != "" {
			err = joinRenderSessionCleanupError(ctx, engine, sessionID, err)
		}
		return session, err
	}
	if invoked == nil {
		return session, errors.New("invoke render returned no response")
	}

	sessionID := strings.TrimSpace(invoked.GetSessionId())
	if sessionID == "" {
		return session, errors.New("invoke render returned no session ID")
	}

	if err := renderInvocationError(prepared, invoked); err != nil {
		err = joinRenderSessionCleanupError(ctx, engine, sessionID, err)
		return session, err
	}

	return session, nil
}

// openRenderSessionResult opens a session and builds the response returned to
// MCP clients. It closes the session if response conversion fails so callers
// never have to manage a session they did not receive.
func openRenderSessionResult(
	ctx context.Context,
	engine apapproto.ApapClient,
	runID string,
	renderParameters map[string]*structpb.Value,
) (openedRenderSessionResult, error) {
	var opened openedRenderSessionResult
	session, err := openRenderSession(ctx, engine, runID, renderParameters)
	opened.Session = session
	if err != nil {
		return opened, err
	}

	result, err := renderSessionResultFromProto(session.Preparation, session.Invocation)
	if err != nil {
		sessionID := strings.TrimSpace(session.Invocation.GetSessionId())
		err = joinRenderSessionCleanupError(ctx, engine, sessionID, err)
		return opened, err
	}
	opened.Details = result

	tableSchemas, err := queryRenderTableSchemas(ctx, engine, session.Invocation)
	if err != nil {
		sessionID := strings.TrimSpace(session.Invocation.GetSessionId())
		err = joinRenderSessionCleanupError(ctx, engine, sessionID, err)
		return opened, err
	}
	opened.Details["table_schemas"] = tableSchemas
	addRenderSessionGuidance(opened.Details, session.Invocation)
	return opened, nil
}

// addRenderSessionGuidance links guides relevant to the published renderers.
func addRenderSessionGuidance(result renderSessionResult, invoked *apapproto.InvokeRenderResponse) {
	renderers := map[string]bool{}
	for _, entry := range invoked.GetManifest().GetEntry() {
		renderers[entry.GetRendererId().GetValue()] = true
	}

	var guidance []string
	if renderers["jfr_recordings"] {
		guidance = append(guidance, "This run contains JVM data. Read the MCP resource apx://instructions/jvm-guidance before interpreting it.")
	}
	if len(guidance) > 0 {
		result["guidance"] = strings.Join(guidance, "\n")
	}
}

// fitRenderTableSchemas lets schemas use all remaining response bytes. If they
// do not fit, the caller can discover them in the still-open render session.
func fitRenderTableSchemas(response any, session renderSessionResult) error {
	encoded, err := json.Marshal(response)
	if err != nil {
		return err
	}
	if len(encoded) <= aiInsightsMaxResponseBytes {
		return nil
	}
	tables := session["table_schemas"].([]renderTableSchema)
	metadata := make([]renderTableSchema, len(tables))
	for i, table := range tables {
		metadata[i] = table
		metadata[i].Columns = map[string]string{}
	}
	session["table_schemas"] = metadata
	session["table_schemas_query"] = renderTableSchemasDiscoverySQL
	if err := ensureJSONSizeAtMost(response, aiInsightsMaxResponseBytes); err == nil {
		return nil
	}
	session["table_schemas"] = []renderTableSchema{}
	return ensureJSONSizeAtMost(response, aiInsightsMaxResponseBytes)
}

// renderInvocationError checks that InvokeRender returned one successful
// status for every prepared renderer. This prevents callers from using a
// session whose data may be incomplete.
func renderInvocationError(
	prepared *apapproto.PrepareRenderResponse,
	invoked *apapproto.InvokeRenderResponse,
) error {
	renderers := prepared.GetRenderers()
	statuses := invoked.GetInvocationStatuses()
	if len(statuses) != len(renderers) {
		return fmt.Errorf(
			"render returned %d invocation statuses for %d renderers",
			len(statuses),
			len(renderers),
		)
	}

	params := &run.RenderInvocationParams{
		RendererConfig:      renderers,
		VisualizationConfig: prepared.GetVisualizations(),
	}
	if run.AnyRenderError(invoked) {
		return fmt.Errorf("render failed: %s", strings.Join(run.ListFailedRenderersForDisplay(params, invoked), "; "))
	}
	if run.AnyRendererPending(invoked) {
		return fmt.Errorf("render remained pending: %s", strings.Join(run.ListPendingRenderersForDisplay(params, invoked), "; "))
	}
	for index, status := range statuses {
		if status.GetSuccess() == nil {
			return fmt.Errorf("render returned an unknown status for renderer %d", index)
		}
	}

	return nil
}

// renderSessionResultFromProto converts the daemon responses to the JSON
// returned to MCP clients. Renderer-oriented metadata is omitted because the
// separately queried table schemas contain the physical schema and logical
// visualization mappings needed by MCP clients.
func renderSessionResultFromProto(
	prepared *apapproto.PrepareRenderResponse,
	invoked *apapproto.InvokeRenderResponse,
) (renderSessionResult, error) {
	encoded, err := (protojson.MarshalOptions{
		EmitUnpopulated: true,
		UseProtoNames:   true,
	}).Marshal(invoked)
	if err != nil {
		return nil, fmt.Errorf("convert invoke render response: %w", err)
	}

	result := renderSessionResult{}
	if err := json.Unmarshal(encoded, &result); err != nil {
		return nil, fmt.Errorf("convert invoke render response: %w", err)
	}
	delete(result, "manifest")
	delete(result, "visualization_resolved_tables")
	if warning := compatibilityWarningFromProto(prepared); warning != nil {
		result["compatibility_warning"] = warning
	}
	return result, nil
}

// queryRenderTableSchemas describes the tables published in the render
// manifest, which excludes hidden renderer inputs. Advertising their often
// wide schemas can exceed the AI Insights response limit. Tables remain
// queryable whether or not their schemas are advertised here.
func queryRenderTableSchemas(
	ctx context.Context,
	engine apapproto.ApapClient,
	invoked *apapproto.InvokeRenderResponse,
) ([]renderTableSchema, error) {
	result, err := queryRenderSession(ctx, engine, invoked.GetSessionId(), `SELECT table_name, column_name, data_type
FROM duckdb_columns()
WHERE schema_name = current_schema()
ORDER BY table_name, column_index`)
	if err != nil {
		return nil, fmt.Errorf("query rendered table schemas: %w", err)
	}
	if result.ReturnedRowCount == 0 {
		return nil, errors.New("rendered table schema query returned no columns")
	}

	byName := make(map[string]*renderTableSchema)
	publicTables := make(map[string]bool)
	for _, entry := range invoked.GetManifest().GetEntry() {
		publicTables[entry.GetTableName()] = true
	}
	for _, row := range result.Rows {
		if len(row) != 3 {
			return nil, fmt.Errorf("rendered table schema query returned a row with %d columns", len(row))
		}
		tableName, tableOK := row[0].(string)
		columnName, columnOK := row[1].(string)
		dataType, typeOK := row[2].(string)
		if !tableOK || !columnOK || !typeOK {
			return nil, errors.New("rendered table schema query returned a non-string value")
		}
		if !publicTables[tableName] {
			continue
		}

		table := byName[tableName]
		if table == nil {
			table = &renderTableSchema{
				Name:                     tableName,
				Columns:                  map[string]string{},
				VisualizationDataSources: []renderVisualizationDataSource{},
			}
			byName[tableName] = table
		}
		table.Columns[columnName] = dataType
	}
	if len(byName) == 0 {
		return nil, errors.New("rendered table schema query returned no published tables")
	}

	for _, entry := range invoked.GetVisualizationResolvedTables().GetEntries() {
		visualizationID := entry.GetId().GetValue()
		for dataSource, tables := range entry.GetTables() {
			for _, tableName := range tables.GetValues() {
				if tableName == "" {
					continue
				}
				table := byName[tableName]
				if table == nil {
					return nil, fmt.Errorf(
						"visualization %q data source %q resolved to table %q, which is absent from the active schema",
						visualizationID,
						dataSource,
						tableName,
					)
				}
				table.VisualizationDataSources = append(
					table.VisualizationDataSources,
					renderVisualizationDataSource{
						VisualizationID: visualizationID,
						DataSource:      dataSource,
					},
				)
			}
		}
	}

	tableNames := make([]string, 0, len(byName))
	for tableName := range byName {
		tableNames = append(tableNames, tableName)
	}
	sort.Strings(tableNames)

	tables := make([]renderTableSchema, 0, len(tableNames))
	for _, tableName := range tableNames {
		table := byName[tableName]
		sort.Slice(table.VisualizationDataSources, func(i, j int) bool {
			left := table.VisualizationDataSources[i]
			right := table.VisualizationDataSources[j]
			if left.VisualizationID != right.VisualizationID {
				return left.VisualizationID < right.VisualizationID
			}
			return left.DataSource < right.DataSource
		})
		tables = append(tables, *table)
	}
	return tables, nil
}

// compatibilityWarningFromProto converts the daemon warning to the structured
// error format used in MCP responses so callers retain its cause and advice.
func compatibilityWarningFromProto(prepared *apapproto.PrepareRenderResponse) *toolError {
	warning := message.ReconstructFromChain(prepared.GetCompatibilityWarning())
	if warning == nil {
		return nil
	}
	return newToolError(warning)
}

// cleanupRenderSession closes a session even if the request was cancelled. Its
// own timeout gives cleanup a chance to finish without blocking indefinitely.
func cleanupRenderSession(ctx context.Context, engine apapproto.ApapClient, sessionID string) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), renderSessionCleanupTimeout)
	defer cancel()
	return closeRenderSession(cleanupCtx, engine, sessionID)
}

// joinRenderSessionCleanupError closes a rejected session and reports both
// errors if closing also fails. When closing succeeds, it returns the original
// error without wrapping it.
func joinRenderSessionCleanupError(
	ctx context.Context,
	engine apapproto.ApapClient,
	sessionID string,
	err error,
) error {
	if cleanupErr := cleanupRenderSession(ctx, engine, sessionID); cleanupErr != nil {
		return errors.Join(err, cleanupErr)
	}
	return err
}

func closeRenderSession(ctx context.Context, engine apapproto.ApapClient, sessionID string) error {
	_, err := engine.CloseRender(ctx, &apapproto.CloseRenderRequest{SessionId: sessionID})
	if err != nil {
		return fmt.Errorf("close render %q: %w", sessionID, err)
	}
	return nil
}
