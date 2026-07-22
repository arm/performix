// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipe

// RendererConfig defines the renderer type supported by the recipe, with a unique ID.
type RendererConfig struct {
	Type   string                 // renderer type that is supported
	ID     string                 // unique
	Config map[string]interface{} // optional
}

// WidgetConfig defines a recipe-driven widget type (typically a visualization) produced by a recipe render stage.
type WidgetConfig struct {
	Type        string // widget type that is supported
	ID          string // unique
	RendererID  string
	Placement   string
	Title       string
	Description string
	Config      map[string]interface{} // optional
	// ParameterBindings maps widget parameter IDs to recipe render parameter IDs.
	ParameterBindings map[string]string
	Disabled          *WidgetDisabledState // optional
}

type WidgetDisabledState struct {
	Reason string
}
