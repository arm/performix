// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/parameters"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

// MapVisualizationParamsToRenderParams converts visualization parameter values into render parameter values,
// ignoring entries that do not map cleanly to a known render parameter.
// Returns an error if multiple visualization parameters map to the same render parameter.
func MapVisualizationParamsToRenderParams(visualizationParams map[string]any, visualizations []recipe.WidgetConfig, renderParams parameters.RenderParameters) (map[string]any, error) {
	mapped := map[string]any{}
	seenKeys := map[string][]string{}
	seenValues := map[string]any{}
	conflicts := map[string]struct{}{}
	if len(visualizationParams) == 0 {
		return mapped, nil
	}

	validRenderParams := renderParamIDSet(renderParams)
	for key, value := range visualizationParams {
		renderParamID, ok := resolveVisualizationBinding(key, visualizations)
		if !ok {
			// Ignore unknown visualization parameters or invalid bindings.
			continue
		}
		if _, ok := validRenderParams[renderParamID]; !ok {
			// Ignore bindings that target non-existent render parameters.
			continue
		}
		if existingValue, exists := seenValues[renderParamID]; exists {
			seenKeys[renderParamID] = append(seenKeys[renderParamID], key)
			if !reflect.DeepEqual(existingValue, value) {
				// Reject conflicting visualization inputs for the same render parameter.
				conflicts[renderParamID] = struct{}{}
			}
			continue
		}
		mapped[renderParamID] = value
		seenValues[renderParamID] = value
		seenKeys[renderParamID] = []string{key}
	}

	for renderParamID := range conflicts {
		keys := seenKeys[renderParamID]
		return nil, message.New(message.EngineGrpcserverApiApapRenderParamConflict).WithMetadata(map[string]string{
			"renderParam": renderParamID,
			"keys":        strings.Join(keys, ","),
		})
	}

	return mapped, nil
}

// MergeRenderParams combines explicit render params with mapped visualization params,
// keeping explicit values when keys overlap.
func MergeRenderParams(explicitParams map[string]any, mappedParams map[string]any) map[string]any {
	merged := make(map[string]any, len(explicitParams)+len(mappedParams))
	for key, value := range explicitParams {
		merged[key] = value
	}
	for key, value := range mappedParams {
		if _, exists := merged[key]; exists {
			continue
		}
		merged[key] = value
	}
	return merged
}

// ValidateRenderOutputStability enforces MVP restrictions that topology and bindings do not change.
func ValidateRenderOutputStability(before recipe.RenderOutput, after recipe.RenderOutput) error {
	if err := validateRendererTopology(before, after); err != nil {
		return err
	}
	if err := validateWidgetTopology(before, after); err != nil {
		return err
	}
	if err := validateVisualizationBindings(before, after); err != nil {
		return err
	}
	return nil
}

// validateRendererTopology ensures renderer IDs and types are unchanged.
func validateRendererTopology(before recipe.RenderOutput, after recipe.RenderOutput) error {
	if len(before.Renderers) != len(after.Renderers) {
		return newRenderTopologyError(fmt.Sprintf("renderer count changed from %d to %d", len(before.Renderers), len(after.Renderers)))
	}
	afterByID := map[string]recipe.RendererConfig{}
	for _, renderer := range after.Renderers {
		afterByID[renderer.ID] = renderer
	}
	for _, renderer := range before.Renderers {
		afterRenderer, ok := afterByID[renderer.ID]
		if !ok {
			return newRenderTopologyError(fmt.Sprintf("renderer %q missing", renderer.ID))
		}
		if renderer.Type != afterRenderer.Type {
			return newRenderTopologyError(fmt.Sprintf("renderer %q type changed from %q to %q", renderer.ID, renderer.Type, afterRenderer.Type))
		}
	}
	return nil
}

// validateWidgetTopology ensures widget IDs, types, placements, and renderer IDs are unchanged.
func validateWidgetTopology(before recipe.RenderOutput, after recipe.RenderOutput) error {
	if len(before.Widgets) != len(after.Widgets) {
		return newRenderTopologyError(fmt.Sprintf("visualization count changed from %d to %d", len(before.Widgets), len(after.Widgets)))
	}
	afterByID := map[string]recipe.WidgetConfig{}
	for _, visualization := range after.Widgets {
		afterByID[visualization.ID] = visualization
	}
	for _, visualization := range before.Widgets {
		afterVisualization, ok := afterByID[visualization.ID]
		if !ok {
			return newRenderTopologyError(fmt.Sprintf("visualization %q missing", visualization.ID))
		}
		if visualization.Type != afterVisualization.Type {
			return newRenderTopologyError(fmt.Sprintf("visualization %q type changed from %q to %q", visualization.ID, visualization.Type, afterVisualization.Type))
		}
		if visualization.Placement != afterVisualization.Placement {
			return newRenderTopologyError(fmt.Sprintf("visualization %q placement changed from %q to %q", visualization.ID, visualization.Placement, afterVisualization.Placement))
		}
		if visualization.RendererID != afterVisualization.RendererID {
			return newRenderTopologyError(fmt.Sprintf("visualization %q renderer changed from %q to %q", visualization.ID, visualization.RendererID, afterVisualization.RendererID))
		}
	}
	return nil
}

// validateVisualizationBindings ensures visualization parameter bindings are unchanged.
func validateVisualizationBindings(before recipe.RenderOutput, after recipe.RenderOutput) error {
	afterByID := map[string]recipe.WidgetConfig{}
	for _, visualization := range after.Widgets {
		afterByID[visualization.ID] = visualization
	}
	for _, visualization := range before.Widgets {
		afterVisualization, ok := afterByID[visualization.ID]
		if !ok {
			continue
		}
		if !bindingsEqual(visualization.ParameterBindings, afterVisualization.ParameterBindings) {
			return newRenderBindingsError(fmt.Sprintf("bindings changed for visualization %q", visualization.ID))
		}
	}
	return nil
}

func newRenderTopologyError(detail string) error {
	return message.New(message.EngineGrpcserverApiApapRenderTopologyChanged).WithMetadata(map[string]string{"detail": detail})
}

func newRenderBindingsError(detail string) error {
	return message.New(message.EngineGrpcserverApiApapRenderBindingsChanged).WithMetadata(map[string]string{"detail": detail})
}

// resolveVisualizationBinding finds the render parameter ID for a visualization parameter key.
// Keys must be in "<visualizationId>.<paramId>" form.
func resolveVisualizationBinding(key string, visualizations []recipe.WidgetConfig) (string, bool) {
	visID, paramID, ok := util.SplitVisualizationParameterKey(key)
	if !ok {
		return "", false
	}
	for _, vis := range visualizations {
		if vis.ID != visID {
			continue
		}
		if renderParamID, ok := vis.ParameterBindings[paramID]; ok {
			return renderParamID, true
		}
		return "", false
	}
	return "", false
}

func renderParamIDSet(params parameters.RenderParameters) map[string]struct{} {
	set := make(map[string]struct{}, len(params))
	for _, param := range params {
		set[param.ID] = struct{}{}
	}
	return set
}

func bindingsEqual(left map[string]string, right map[string]string) bool {
	if len(left) == 0 && len(right) == 0 {
		return true
	}
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if rightValue, ok := right[key]; !ok || rightValue != value {
			return false
		}
	}
	return true
}
