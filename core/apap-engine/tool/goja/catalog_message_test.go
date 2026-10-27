// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package tool_goja

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	tool_mocks "github.com/Arm-Debug/apap-cli/apap-engine/tool/mocks"
)

func TestWriteCatalogMessage(t *testing.T) {
	tests := []struct {
		name      string
		arguments string
		level     string
		text      string
		errorText string
	}{
		{name: "info_interpolates_metadata", arguments: `'cli.cmd.mcp.common.TECHNICAL_DETAIL', {detail: 'test detail'}`, level: "info", text: "Detail: test detail"},
		{name: "warning", arguments: `'tool_integrations.sysutil_timeline.NUMASTAT_NOT_FOUND', {}`, level: "warn", text: "The target does not have `numastat`."},
		{name: "error_interpolates_constants", arguments: `'cli.cmd.common.JSON_MARSHAL_FAILED', {}`, level: "error", text: "Arm Performix cannot create the JSON output."},
		{name: "metadata_can_be_omitted_without_placeholders", arguments: `'cli.cmd.common.DETAILS_HEADING'`, level: "info", text: "Details:"},
		{name: "unknown_code", arguments: `'test.UNKNOWN', {}`, errorText: `unknown message code "test.UNKNOWN"`},
		{name: "missing_metadata", arguments: `'cli.cmd.mcp.common.TECHNICAL_DETAIL'`, errorText: "unset placeholders: detail"},
		{name: "null_metadata", arguments: `'cli.cmd.mcp.common.TECHNICAL_DETAIL', null`, errorText: "unset placeholders: detail"},
		{name: "empty_string_is_valid_metadata", arguments: `'cli.cmd.mcp.common.TECHNICAL_DETAIL', {detail: ''}`, level: "info", text: "Detail: "},
		{name: "missing_placeholder", arguments: `'cli.cmd.mcp.common.TECHNICAL_DETAIL', {other: 'value'}`, errorText: "unset placeholders: detail"},
		{name: "invalid_metadata_type", arguments: `'cli.cmd.mcp.common.TECHNICAL_DETAIL', 42`, errorText: "could not convert 42 to map[string]string"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := &tool_mocks.MockEngineContext{}
			if tt.errorText == "" {
				engine.On("WriteUserMessage", tt.level, mock.MatchedBy(func(text string) bool {
					return strings.Contains(text, tt.text) && !strings.Contains(text, "{")
				})).Once()
			}
			source := ToolSourceBegin + fmt.Sprintf("run: (engine) => { engine.writeCatalogMessage(%s); },", tt.arguments) + ToolSourceEnd
			factory, err := LoadFromSource(source, "catalog-message.js")
			require.NoError(t, err)
			integration, err := factory.NewIntegration(integrationContext(engine, nil))
			require.NoError(t, err)
			cleanup, err := integration.StartRuntime()
			require.NoError(t, err)
			defer cleanup()
			err = integration.Run()
			if tt.errorText != "" {
				require.ErrorContains(t, err, tt.errorText)
				engine.AssertNotCalled(t, "WriteUserMessage", mock.Anything, mock.Anything)
			} else {
				require.NoError(t, err)
			}
			engine.AssertExpectations(t)
		})
	}
}
