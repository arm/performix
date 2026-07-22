// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package render

// Renderer is an interface for components that are capable of rendering performance data.
type Renderer interface {
	// Configure sets up the renderer according to configuration provided by the user.
	Configure(config *Config) error
	// Initialize must add items to the Session's Manifest. It must guarantee that, if an item exists in the Manifest,
	// this item is queryable from the Session's Database.
	Initialize(session Session, resolvedDataSources map[string][]TableRef) error
	GetInputSpec() InputSpec
	GetOutputSpec() OutputSpec
	Name() string
	Version() string
}

// InitializeCompletionListener allows renderers to perform cleanup once all Initialize calls succeed.
type InitializeCompletionListener interface {
	OnInitializeComplete(Session)
}

// RendererFactory is an interface for components that create instances of Renderer.
type RendererFactory interface {
	NewRenderer(rendererName string) (Renderer, error)
}
