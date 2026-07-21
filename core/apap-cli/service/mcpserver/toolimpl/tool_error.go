// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package toolimpl

import (
	"github.com/google/jsonschema-go/jsonschema"

	"github.com/Arm-Debug/apap-cli/apap-cli/service/clijson"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
)

type toolError = clijson.ErrorPayload

func newToolError(err error) *toolError {
	return clijson.BuildErrorTree(err)
}

// toolErrorSchemaID gives recursive error payload schemas a stable base URI for
// children.items $ref values. Without an ID, the same schema fragment can be
// advertised but jsonschema-go cannot resolve nested children during validation.
const toolErrorSchemaID = "urn:arm:performix:mcp:tool-error"

// toolErrorSchema describes the toolError shape for tool output schemas.
func toolErrorSchema() *jsonschema.Schema {
	return toolErrorSchemaWithID(toolErrorSchemaID)
}

func toolErrorSchemaWithID(schemaID string) *jsonschema.Schema {
	return &jsonschema.Schema{
		ID:          schemaID,
		Type:        "object",
		Description: "Structured error detail from the Performix message catalog.",
		Required:    []string{"severity", "message"},
		Properties:  toolErrorSchemaProperties(schemaID),
		Defs: map[string]*jsonschema.Schema{
			"errorPayload": {
				Type:        "object",
				Description: "Structured error detail from the Performix message catalog.",
				Required:    []string{"severity", "message"},
				Properties:  toolErrorSchemaProperties(schemaID),
			},
		},
	}
}

func toolErrorSchemaProperties(schemaID string) map[string]*jsonschema.Schema {
	return map[string]*jsonschema.Schema{
		"message_code": {Type: "string", Description: "Catalog message code, when the failure maps to a known catalog message."},
		"severity":     {Type: "string", Enum: []any{message.SeverityInfo, message.SeverityWarning, message.SeverityError}, Description: "Catalog severity."},
		"message":      {Type: "string", Description: "Human-readable summary of the failure."},
		"explanation":  {Type: "string", Description: "Detailed explanation of the cause, when available from the catalog."},
		"advice":       {Type: "string", Description: "Suggested next steps to resolve the problem, when available from the catalog."},
		"locale":       {Type: "string", Description: "Message locale."},
		"metadata": {
			Description: "Message metadata used to render catalog placeholders.",
			AnyOf: []*jsonschema.Schema{
				{Type: "object", AdditionalProperties: &jsonschema.Schema{Type: "string"}},
				{Type: "null"},
			},
		},
		"children": {
			Type:        "array",
			Description: "Wrapped or joined child errors that provide supporting detail.",
			Items:       &jsonschema.Schema{Ref: schemaID + "#/$defs/errorPayload"},
		},
	}
}
