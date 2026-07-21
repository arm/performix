// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package tool

import "github.com/Arm-Debug/apap-cli/apap-engine/deploymentsupport"

// Factory defines how to create a new tool integration instance.
type Factory interface {
	NewIntegration(tcd *IntegrationContext) (ToolIntegration, error)
	Name() string
	Version() string
	Deployments() []deploymentsupport.DeploymentDeclaration
	GetMigrations() []Migration
}

// integrationID is a unique identifier for a tool integration, consisting of its name and version.
type integrationID struct {
	Name    string
	Version string
}

// Registry holds all of the known tool integrations
type Registry struct {
	Tools map[integrationID]Factory
}

func NewToolRegistry() *Registry {
	return &Registry{
		Tools: map[integrationID]Factory{},
	}
}

func (tr *Registry) RegisterTool(toolFactory Factory) {
	if tr.Tools == nil {
		tr.Tools = map[integrationID]Factory{}
	}
	tr.Tools[integrationID{Name: toolFactory.Name(), Version: toolFactory.Version()}] = toolFactory
}

func (tr *Registry) FindTool(name, version string) Factory {
	return tr.Tools[integrationID{Name: name, Version: version}]
}

func (r *Registry) Factories() []Factory {
	fs := make([]Factory, 0, len(r.Tools))
	for _, f := range r.Tools {
		fs = append(fs, f)
	}
	return fs
}
