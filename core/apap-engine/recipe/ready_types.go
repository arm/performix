// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipe

import "github.com/Arm-Debug/apap-cli/apap-engine/message"

// RenderOutput defines the output of a render stage, which includes a list of renderers and widgets
// (notably visualizations)
type RenderOutput struct {
	Renderers []RendererConfig
	Widgets   []WidgetConfig
}

const ReadyStatusReady = "ready"
const ReadyStatusError = "error"
const ReadyStatusUnknown = "unknown"
const ReadyStatusWarning = "warning"

const AdviceSeverityError = "error"
const AdviceSeverityUnknown = "unknown"
const AdviceSeverityWarning = "warning"
const AdviceSeverityMessage = "message"

// ReadyOutput contains the output of recipe ready.
type ReadyOutput struct {
	Status string
	Advice []ReadyAdvice
}

type ReadyAdvice struct {
	ToolName       string
	AdviceSeverity string
	AdviceMessage  message.Message
}

// ConvertMessageToReadyOutput converts a structured error message to a ReadyOutput with an error status and advice.
func ConvertMessageToReadyOutput(errorMsg *message.MessageImpl) ReadyOutput {
	readyOutput := ReadyOutput{
		Status: ReadyStatusError,
		Advice: []ReadyAdvice{
			{
				AdviceSeverity: AdviceSeverityError,
				AdviceMessage:  errorMsg,
			},
		},
	}
	return readyOutput
}
