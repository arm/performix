// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// This file selects and renders the structured error details used by MCP
// commands. The shared CLI error tree remains the source of the localised
// messages; this file only chooses how MCP commands present them.
package mcp

import (
	"fmt"
	"io"
	"strings"

	"github.com/Arm-Debug/apap-cli/apap-cli/service/clijson"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

// mcpCatalogMessagesFromError selects the localised messages from the
// existing CLI error tree. Plain errors are retained in that tree for JSON and
// logging, but are not shown as supporting details in human-readable output.
func mcpCatalogMessagesFromError(
	err error,
) (*message.ErrorPayload, []*message.ErrorPayload) {
	root := clijson.BuildErrorTree(err)
	if root == nil {
		return nil, nil
	}
	var catalogRoot *message.ErrorPayload
	if root.Code != "" {
		rootWithoutChildren := *root
		rootWithoutChildren.Children = nil
		catalogRoot = &rootWithoutChildren
	}
	var details []*message.ErrorPayload
	appendMCPCatalogDetails(&details, root.Children)
	return catalogRoot, details
}

func appendMCPCatalogDetails(details *[]*message.ErrorPayload, nodes []*message.ErrorPayload) {
	for _, node := range nodes {
		if node.Code != "" {
			*details = append(*details, node)
		}
		appendMCPCatalogDetails(details, node.Children)
	}
}

func catalogMessagesFromChain(
	chain *apapproto.ErrorChain,
) (*message.ErrorPayload, []*message.ErrorPayload) {
	return mcpCatalogMessagesFromError(message.ReconstructFromChain(chain))
}

func catalogErrorAdvice(catalogError *message.ErrorPayload) []string {
	if catalogError == nil || catalogError.Advice == "" {
		return nil
	}
	return []string{catalogError.Advice}
}

func writeMCPErrorWithDetailsAndIndent(out io.Writer, err error, numChars int) {
	clijson.HandlePlaintextCLIErrorWithIndent(out, err, numChars)
	_, details := mcpCatalogMessagesFromError(err)
	if len(details) == 0 {
		return
	}
	heading, lookupErr := clijson.LookupMsg(message.New(message.CliCmdCommonDetailsHeading))
	if lookupErr == nil {
		fmt.Fprintf(out, "%s%s\n", strings.Repeat(" ", numChars), heading.Message)
	}
	for _, detail := range details {
		fmt.Fprintf(out, "%s%s\n", strings.Repeat(" ", numChars+2), detail.Message)
	}
}
