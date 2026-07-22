// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package insights

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strconv"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	run "github.com/Arm-Debug/apap-cli/apap-engine/run"
)

const callTreeCoverageThresholdPercent = 99.0
const callTreeSummaryName = "call_tree"
const callTreeVisualizationID = "call_stack"
const callTreeStructuralRootID int64 = 0
const callTreeSelfSamplesIdentifier = "unknown.measurement.periodic.samples.self"
const callTreeTotalSamplesIdentifier = "unknown.measurement.periodic.samples.total"

var callTreePromptFragment = fmt.Sprintf("Call tree hot paths emitted in depth-first order, with siblings sorted by descending total sample count. The payload contains ts=total self samples, n=total call tree nodes, and roots=root node ids. The node list omits nodes at or below %g%% of ts by total sample count, and may be truncated after ordering to fit the summary byte limit. Node entry fields: id=node id, pid=parent node id or -1 for root nodes, d=depth, fn=function name, img=image name, s=self sample count, t=total sample count, spct=self sample percentage from 0.0 to 100.0, tpct=total sample percentage from 0.0 to 100.0, c=retained child node ids omitted for leaves, file=optional source file. Percentages are relative to ts.", 100.0-callTreeCoverageThresholdPercent)

var CallTreeSummarizer = BudgetedRunSummarizer{
	Name:      callTreeSummaryName,
	Summarize: SummarizeCallTree,
}

type callTreePayload struct {
	TotalSelfSamples uint64                `json:"ts"`
	TotalNodes       uint64                `json:"n"`
	RootIDs          []int64               `json:"roots"`
	Nodes            []callTreePayloadNode `json:"nodes"`
}

type callTreePayloadNode struct {
	ID           int64   `json:"id"`
	ParentID     int64   `json:"pid"`
	Depth        uint32  `json:"d"`
	FunctionName string  `json:"fn"`
	ImageName    string  `json:"img"`
	SelfSamples  uint64  `json:"s"`
	TotalSamples uint64  `json:"t"`
	SelfPercent  float32 `json:"spct"`
	TotalPercent float32 `json:"tpct"`
	ChildIDs     []int64 `json:"c,omitempty"`
	SourceFile   *string `json:"file,omitempty"`
}

type callTreeNode struct {
	callTreeID       int64
	callTreeParentID int64
	depth            uint32
	functionName     string
	imageName        string
	sourceFile       *string
	selfSamples      uint64
	totalSamples     uint64
}

type callTreeRow struct {
	callTreeID       int64
	callTreeParentID int64
	functionName     string
	imageName        string
	sourceFile       sql.NullString
	selfSamples      float64
	totalSamples     float64
}

type callTreeTables struct {
	drilldown   string
	symbols     string
	images      string
	sourceFiles string
}

// SummarizeCallTree creates a summary of the hottest call-tree paths from the provided render session.
func SummarizeCallTree(ctx context.Context, _ *run.RunDescription, session render.Session, byteLimit int) (RunSummary, error) {
	tables, err := resolveCallTreeTables(session)
	if err != nil {
		return RunSummary{}, err
	}

	rows, err := session.Database().Conn.QueryContext(ctx, buildCallTreeSQL(tables))
	if err != nil {
		return RunSummary{}, message.New(message.EngineInsightsRenderQueryFailed).
			WithMetadata(map[string]string{"summaryName": callTreeSummaryName}).
			WithCause(err)
	}
	defer rows.Close()

	payload, err := collectCallTree(rows, byteLimit)
	if err != nil {
		return RunSummary{}, err
	}

	return NewRunSummary(callTreeSummaryName, callTreePromptFragment, payload)
}

// collectCallTree builds the call-tree payload from rows, applying the coverage threshold
// and byte limit.
func collectCallTree(rows *sql.Rows, byteLimit int) (callTreePayload, error) {
	nodes, err := collectCallTreeNodes(rows)
	if err != nil {
		return callTreePayload{}, err
	}
	totalSelfSamples := sumCallTreeSelfSamples(nodes)
	numNodes := uint64(len(nodes))
	payload := callTreePayload{
		TotalSelfSamples: totalSelfSamples,
		TotalNodes:       numNodes,
		RootIDs:          []int64{},
		Nodes:            []callTreePayloadNode{},
	}

	summary, err := NewRunSummary(callTreeSummaryName, callTreePromptFragment, payload)
	if err != nil {
		return callTreePayload{}, err
	}
	if runSummarySizeBytes(summary) > byteLimit {
		return callTreePayload{}, message.New(message.EngineInsightsInsufficientByteLimit).
			WithMetadata(map[string]string{
				"summaryName": callTreeSummaryName,
				"byteLimit":   strconv.Itoa(byteLimit),
			})
	}

	callTree := buildCallTree(nodes, totalSelfSamples)
	if len(callTree) == 0 {
		return payload, nil
	}

	return buildPayloadFromCallTree(callTree, byteLimit, totalSelfSamples, numNodes)
}

// collectCallTreeNodes iterates through query rows to build the call-tree nodes.
func collectCallTreeNodes(rows *sql.Rows) ([]callTreeNode, error) {
	nodes := []callTreeNode{}
	for rows.Next() {
		var row callTreeRow
		if err := rows.Scan(
			&row.callTreeID,
			&row.callTreeParentID,
			&row.functionName,
			&row.imageName,
			&row.sourceFile,
			&row.selfSamples,
			&row.totalSamples,
		); err != nil {
			return nil, message.New(message.EngineInsightsRenderQueryFailed).
				WithMetadata(map[string]string{"summaryName": callTreeSummaryName}).
				WithCause(err)
		}

		node := callTreeNode{
			callTreeID:       row.callTreeID,
			callTreeParentID: row.callTreeParentID,
			functionName:     row.functionName,
			imageName:        row.imageName,
			selfSamples:      uint64(row.selfSamples),
			totalSamples:     uint64(row.totalSamples),
		}
		if row.sourceFile.Valid && row.sourceFile.String != "" {
			path := row.sourceFile.String
			node.sourceFile = &path
		}
		nodes = append(nodes, node)
	}

	if err := rows.Err(); err != nil {
		return nil, message.New(message.EngineInsightsRenderQueryFailed).
			WithMetadata(map[string]string{"summaryName": callTreeSummaryName}).
			WithCause(err)
	}

	return nodes, nil
}

func sumCallTreeSelfSamples(nodes []callTreeNode) uint64 {
	var total uint64
	for i := range nodes {
		total += nodes[i].selfSamples
	}
	return total
}

// buildCallTree returns the call-tree nodes in depth-first order.
// Siblings with the highest total sample count are ordered first, with the
// node's descendants being ordered before moving to the next sibling.
// Nodes below the sample cutoff are excluded.
// For example, if A has higher total samples than B, and A2 has higher total
// samples than A1, this tree:
//
//	A
//	  A1
//	  A2
//	B
//	  B1
//
// is returned in the order [A, A2, A1, B, B1].
func buildCallTree(nodes []callTreeNode, totalSelfSamples uint64) []callTreeNode {
	nodeByID := make(map[int64]*callTreeNode, len(nodes))
	childrenByParent := make(map[int64][]int64, len(nodes))
	for i := range nodes {
		node := &nodes[i]
		nodeByID[node.callTreeID] = node
		childrenByParent[node.callTreeParentID] = append(childrenByParent[node.callTreeParentID], node.callTreeID)
	}
	for parentID := range childrenByParent {
		sortCallTreeIDsByHotness(childrenByParent[parentID], nodeByID)
	}

	rootIDs := childrenByParent[callTreeStructuralRootID]

	cutoff := float64(totalSelfSamples) * (100.0 - callTreeCoverageThresholdPercent) / 100.0
	callTree := make([]callTreeNode, 0, len(nodes))
	var appendDepthFirst func(int64, uint32)
	appendDepthFirst = func(id int64, depth uint32) {
		node := nodeByID[id]
		if float64(node.totalSamples) <= cutoff {
			return
		}
		node.depth = depth
		callTree = append(callTree, *node)
		for _, childID := range childrenByParent[id] {
			appendDepthFirst(childID, depth+1)
		}
	}
	for _, rootID := range rootIDs {
		appendDepthFirst(rootID, 0)
	}

	return callTree
}

// sortCallTreeIDsByHotness orders node IDs by total samples, self samples, function name, then node ID.
func sortCallTreeIDsByHotness(ids []int64, nodeByID map[int64]*callTreeNode) {
	sort.Slice(ids, func(i, j int) bool {
		left := nodeByID[ids[i]]
		right := nodeByID[ids[j]]
		if left.totalSamples != right.totalSamples {
			return left.totalSamples > right.totalSamples
		}
		if left.selfSamples != right.selfSamples {
			return left.selfSamples > right.selfSamples
		}
		if left.functionName != right.functionName {
			return left.functionName < right.functionName
		}
		return ids[i] < ids[j]
	})
}

// buildPayloadFromCallTree builds the payload from callTree.
// Byte-limit truncation keeps the largest prefix of callTree that fits. For
// example, if only three nodes fit, [A, A2, A1, B, B1] is truncated to
// [A, A2, A1].
func buildPayloadFromCallTree(callTree []callTreeNode, byteLimit int, totalSelfSamples uint64, numNodes uint64) (callTreePayload, error) {
	var searchErr error
	firstTooLarge := sort.Search(len(callTree)+1, func(nodeLimit int) bool {
		payload := buildPayloadForNodeLimit(callTree, nodeLimit, totalSelfSamples, numNodes)
		summary, err := NewRunSummary(callTreeSummaryName, callTreePromptFragment, payload)
		if err != nil {
			searchErr = err
			return true
		}
		return runSummarySizeBytes(summary) > byteLimit
	})
	if searchErr != nil {
		return callTreePayload{}, searchErr
	}

	return buildPayloadForNodeLimit(callTree, firstTooLarge-1, totalSelfSamples, numNodes), nil
}

// buildPayloadForNodeLimit builds a payload from the first nodeLimit call-tree nodes.
func buildPayloadForNodeLimit(callTree []callTreeNode, nodeLimit int, totalSelfSamples uint64, numNodes uint64) callTreePayload {
	payload := callTreePayload{
		TotalSelfSamples: totalSelfSamples,
		TotalNodes:       numNodes,
		RootIDs:          []int64{},
		Nodes:            make([]callTreePayloadNode, 0, nodeLimit),
	}

	// Build childrenByParent for the given node limit to ensure truncated children aren't referenced.
	childrenByParent := make(map[int64][]int64, nodeLimit)
	for i := 0; i < nodeLimit; i++ {
		node := callTree[i]
		if node.depth > 0 {
			childrenByParent[node.callTreeParentID] = append(childrenByParent[node.callTreeParentID], node.callTreeID)
		}
	}

	for i := 0; i < nodeLimit; i++ {
		node := callTree[i]
		parentID := node.callTreeParentID
		if node.depth == 0 {
			payload.RootIDs = append(payload.RootIDs, node.callTreeID)
			parentID = -1
		}
		var selfPercent, totalPercent float32
		if totalSelfSamples != 0 {
			selfPercent = 100.0 * float32(node.selfSamples) / float32(totalSelfSamples)
			totalPercent = 100.0 * float32(node.totalSamples) / float32(totalSelfSamples)
		}
		payload.Nodes = append(payload.Nodes, callTreePayloadNode{
			ID:           node.callTreeID,
			ParentID:     parentID,
			ChildIDs:     childrenByParent[node.callTreeID],
			Depth:        node.depth,
			FunctionName: node.functionName,
			ImageName:    node.imageName,
			SourceFile:   node.sourceFile,
			SelfSamples:  node.selfSamples,
			TotalSamples: node.totalSamples,
			SelfPercent:  selfPercent,
			TotalPercent: totalPercent,
		})
	}

	return payload
}

// resolveCallTreeTables returns the tables backing the call-stack visualization.
func resolveCallTreeTables(session render.Session) (callTreeTables, error) {
	tables := callTreeTables{}

	err := resolveSummaryTables(session, callTreeVisualizationID, callTreeSummaryName, []summaryTableRequirement{
		{field: &tables.drilldown, sourceName: "drilldown"},
		{field: &tables.symbols, sourceName: "symbols"},
		{field: &tables.images, sourceName: "images"},
		{field: &tables.sourceFiles, sourceName: "source_files"},
	})
	if err != nil {
		return callTreeTables{}, err
	}

	return tables, nil
}

// buildCallTreeSQL selects self and total sample rows, omitting the structural root node,
// and joins function, image, and source metadata.
func buildCallTreeSQL(t callTreeTables) string {
	return fmt.Sprintf(`
SELECT
  d.call_tree_id,
  d.call_tree_parent_id,
  COALESCE(sym.name, '') AS function_name,
  COALESCE(img.image_name, '') AS image_name,
  sf.target_location AS source_file,
  SUM(CASE WHEN rm.identifier = '%s' THEN d.measurement_value ELSE 0 END) AS self_samples,
  SUM(CASE WHEN rm.identifier = '%s' THEN d.measurement_value ELSE 0 END) AS total_samples
FROM %s d
JOIN ref_measurements rm ON d.measurement_id = rm.measurement_id
  AND rm.identifier IN ('%s', '%s')
LEFT JOIN %s sym ON sym.symbol_id = d.symbol_id
LEFT JOIN %s img ON img.image_id = sym.image_id
LEFT JOIN %s sf ON sf.source_file_id = sym.source_file_id
WHERE d.node_type = 'function'
  AND d.call_tree_parent_id >= 0
GROUP BY d.call_tree_id, d.call_tree_parent_id, sym.name, img.image_name, sf.target_location`,
		callTreeSelfSamplesIdentifier,
		callTreeTotalSamplesIdentifier,
		t.drilldown,
		callTreeSelfSamplesIdentifier,
		callTreeTotalSamplesIdentifier,
		t.symbols,
		t.images,
		t.sourceFiles,
	)
}
