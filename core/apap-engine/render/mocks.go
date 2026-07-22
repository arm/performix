// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"os"
	"path/filepath"

	"github.com/stretchr/testify/mock"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/deploymentsupport"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/targetsession"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
)

type MockSessionFactory struct {
	mock.Mock
}

func (m *MockSessionFactory) NewSession(
	content *ContentMap,
	renderers RendererList,
	dbFactory DatabaseFactory,
	rerender SessionRenderFS,
	targetSessions targetsession.TargetSessionProvider,
) (Session, error) {
	mockArgs := m.Called(content, renderers, dbFactory, rerender, targetSessions)
	return mockArgs.Get(0).(Session), mockArgs.Error(1)
}

type MockSession struct {
	mock.Mock
}

func (m *MockSession) ID() string {
	mockArgs := m.Called()
	return mockArgs.String(0)
}

func (m *MockSession) DatabaseKey() string {
	mockArgs := m.Called()
	return mockArgs.String(0)
}

func (m *MockSession) Close() {
	m.Called()
}

func (m *MockSession) Content() *ContentMap {
	mockArgs := m.Called()
	return mockArgs.Get(0).(*ContentMap)
}

func (m *MockSession) Manifest() *Manifest {
	mockArgs := m.Called()
	return mockArgs.Get(0).(*Manifest)
}

func (m *MockSession) Database() *Database {
	mockArgs := m.Called()
	return mockArgs.Get(0).(*Database)
}

func (m *MockSession) WidgetDataSources() *WidgetDataSources {
	mockArgs := m.Called()
	return mockArgs.Get(0).(*WidgetDataSources)
}

func (m *MockSession) Reference() Hub {
	mockArgs := m.Called()
	return mockArgs.Get(0).(Hub)
}

func (m *MockSession) TargetSessions() targetsession.TargetSessionProvider {
	mockArgs := m.Called()
	if mockArgs.Get(0) == nil {
		return nil
	}
	return mockArgs.Get(0).(targetsession.TargetSessionProvider)
}

// SessionRenderFS returns the rerender filesystem for a mock session.
func (m *MockSession) Rerender() SessionRenderFS {
	mockArgs := m.Called()
	if mockArgs.Get(0) == nil {
		return nil
	}
	return mockArgs.Get(0).(SessionRenderFS)
}

type MockRendererFactory struct {
	mock.Mock
}

func (m *MockRendererFactory) NewRenderer(rendererName string) (Renderer, error) {
	mockArgs := m.Called(rendererName)
	return mockArgs.Get(0).(Renderer), mockArgs.Error(1)
}

type MockRunLoader struct {
	mock.Mock
	rerenderRoot       string
	rerenderCollection *run.RunCollection
}

func (m *MockRunLoader) LoadRun(id run.RunID) (*cdf.OnDiskModel, error) {
	mockArgs := m.Called(id)
	return mockArgs.Get(0).(*cdf.OnDiskModel), mockArgs.Error(1)
}

func (m *MockRunLoader) NewRunRenderFS(id run.RunID) (*run.RunRenderFS, error) {
	if m.rerenderCollection == nil {
		root := m.rerenderRoot
		if root == "" {
			tempRoot, err := os.MkdirTemp("", "rerender-test-*")
			if err != nil {
				return nil, err
			}
			root = tempRoot
			m.rerenderRoot = root
		}
		rc, err := run.NewRunCollection(root)
		if err != nil {
			return nil, err
		}
		m.rerenderCollection = rc
	}

	if err := os.MkdirAll(filepath.Join(m.rerenderRoot, id.Value), perms.LocalDirPerm); err != nil {
		return nil, err
	}

	return m.rerenderCollection.NewRunRenderFS(id)
}

type MockRenderer struct {
	mock.Mock
}

func (m *MockRenderer) Configure(config *Config) error {
	mockArgs := m.Called(config)
	return mockArgs.Error(0)
}

func (m *MockRenderer) Initialize(session Session, resolvedDataSources map[string][]TableRef) error {
	mockArgs := m.Called(session, resolvedDataSources)
	return mockArgs.Error(0)
}

func (m *MockRenderer) Name() string {
	mockArgs := m.Called()
	return mockArgs.String(0)
}

func (m *MockRenderer) Version() string {
	mockArgs := m.Called()
	return mockArgs.String(0)
}

func (m *MockRenderer) GetInputSpec() InputSpec {
	mockArgs := m.Called()
	return mockArgs.Get(0).(InputSpec)
}

func (m *MockRenderer) GetOutputSpec() OutputSpec {
	mockArgs := m.Called()
	return mockArgs.Get(0).(OutputSpec)
}

type MockDatabaseFactory struct {
	mock.Mock
}

func (m *MockDatabaseFactory) Connect(key string) (*Database, error) {
	mockArgs := m.Called(key)
	return mockArgs.Get(0).(*Database), mockArgs.Error(1)
}

// fakeRunLoader satisfies run.RunLoader
type fakeRunLoader struct {
	model *cdf.OnDiskModel
}

func (f *fakeRunLoader) LoadRun(id run.RunID) (*cdf.OnDiskModel, error) {
	return f.model, nil
}

func (f *fakeRunLoader) NewRunRenderFS(id run.RunID) (*run.RunRenderFS, error) {
	root, err := os.MkdirTemp("", "rerender-test-*")
	if err != nil {
		return nil, err
	}
	rc, err := run.NewRunCollection(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(root, id.Value), perms.LocalDirPerm); err != nil {
		return nil, err
	}
	return rc.NewRunRenderFS(id)
}

// fakeToolFactory satisfies tool.Factory
type fakeToolFactory struct {
	migrations []tool.Migration
}

func (f *fakeToolFactory) NewIntegration(_ *tool.IntegrationContext) (tool.ToolIntegration, error) {
	panic("not used")
}
func (f *fakeToolFactory) Name() string {
	return "fake"
}
func (f *fakeToolFactory) Version() string {
	return "1.1.0"
}
func (f *fakeToolFactory) Deployments() []deploymentsupport.DeploymentDeclaration {
	return nil
}
func (f *fakeToolFactory) GetMigrations() []tool.Migration {
	return f.migrations
}

// fakePkgMgr satisfies packages.PackageManager
type FakePkgMgr struct {
	Registry *tool.Registry
	Err      error
}

func (f *FakePkgMgr) FindToolIntegrations() (*tool.Registry, error) {
	return f.Registry, f.Err
}
