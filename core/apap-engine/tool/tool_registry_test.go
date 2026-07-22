// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package tool

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Arm-Debug/apap-cli/apap-engine/deploymentsupport"
)

type basicTestFactory struct {
	name    string
	version string
}

func (f *basicTestFactory) NewIntegration(*IntegrationContext) (ToolIntegration, error) {
	return nil, nil
}
func (f *basicTestFactory) Name() string { return f.name }
func (f *basicTestFactory) Version() string {
	return f.version
}
func (f *basicTestFactory) Deployments() []deploymentsupport.DeploymentDeclaration {
	return nil
}
func (f *basicTestFactory) GetMigrations() []Migration { return nil }

func TestRegistryFindToolRequiresExactVersion(t *testing.T) {
	registry := NewToolRegistry()
	factory := &basicTestFactory{name: "sysutil-timeline", version: "1.0.0"}

	registry.RegisterTool(factory)

	assert.Same(t, factory, registry.FindTool("sysutil-timeline", "1.0.0"))
	assert.Nil(t, registry.FindTool("sysutil-timeline", "2.0.0"))
	assert.Nil(t, registry.FindTool("other-tool", "1.0.0"))
	assert.Equal(t, []Factory{factory}, registry.Factories())
}

func TestRegistryRegisterToolInitializesZeroValueRegistry(t *testing.T) {
	registry := &Registry{}
	factory := &basicTestFactory{name: "sysutil-timeline", version: "2.0.0"}

	registry.RegisterTool(factory)

	assert.Same(t, factory, registry.FindTool("sysutil-timeline", "2.0.0"))
	assert.Nil(t, registry.FindTool("sysutil-timeline", "1.0.0"))
	assert.Nil(t, registry.FindTool("sysutil-timeline", ""))
}

func TestRegistryRegisterToolUsesExactVersion(t *testing.T) {
	registry := NewToolRegistry()
	factory := &basicTestFactory{name: "plain-tool", version: "1.0.0"}

	registry.RegisterTool(factory)

	assert.Same(t, factory, registry.FindTool("plain-tool", "1.0.0"))
	assert.Nil(t, registry.FindTool("plain-tool", "0.9.0"))
	assert.Equal(t, []Factory{factory}, registry.Factories())
}
