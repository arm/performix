// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package insights

import (
	"encoding/json"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
)

type RunSummary struct {
	Name           string
	PromptFragment string
	Payload        string
}

// NewRunSummary creates a RunSummary with the given name, prompt fragment, and payload, marshaling the payload to JSON.
func NewRunSummary(name, promptFragment string, payload any) (RunSummary, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return RunSummary{}, message.New(message.EngineInsightsRunSummaryMarshalFailed).WithCause(err)
	}

	return RunSummary{
		Name:           name,
		PromptFragment: promptFragment,
		Payload:        string(payloadBytes),
	}, nil
}
