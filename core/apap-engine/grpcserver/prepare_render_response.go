// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"github.com/Arm-Debug/apap-cli/apap-engine/parameters"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

// buildRenderParameterDetails converts bound render parameters into proto-ready details.
func buildRenderParameterDetails(bound parameters.BoundRenderParameters) (map[string]*apapproto.RenderParameterDetails, error) {
	out := make(map[string]*apapproto.RenderParameterDetails, len(bound.Parameters))

	for _, param := range bound.Parameters {
		value := bound.Values[param.ID]

		protoValue, err := AnyToProto(value)
		if err != nil {
			return nil, err
		}

		protoDefault, err := AnyToProto(nil)
		if err != nil {
			return nil, err
		}

		out[param.ID] = &apapproto.RenderParameterDetails{
			Value:        protoValue,
			DefaultValue: protoDefault,
		}
	}

	return out, nil
}

// buildVisualizationParameterBindings flattens per-visualization bindings into "vizId.paramId" keys.
func buildVisualizationParameterBindings(visualizations []recipe.WidgetConfig) map[string]string {
	out := map[string]string{}
	for _, visualization := range visualizations {
		for paramID, renderParamID := range visualization.ParameterBindings {
			key := util.MakeVisualizationParameterKey(visualization.ID, paramID)
			out[key] = renderParamID
		}
	}
	return out
}
