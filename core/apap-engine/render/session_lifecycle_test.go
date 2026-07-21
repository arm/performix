// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/targetsession"
)

type fakeSession struct {
	manifest Manifest
	content  ContentMap
}

func newFakeSession(runIDs []string) *fakeSession {
	contentEntries := make([]ContentMapEntry, len(runIDs))
	for i, id := range runIDs {
		contentEntries[i] = ContentMapEntry{ID: run.RunID{Value: id}}
	}

	return &fakeSession{
		manifest: NewManifest(),
		content:  ContentMap{Entries: contentEntries},
	}
}

func (s *fakeSession) ID() string           { return "session" }
func (s *fakeSession) DatabaseKey() string  { return "" }
func (s *fakeSession) Close()               {}
func (s *fakeSession) Content() *ContentMap { return &s.content }
func (s *fakeSession) Manifest() *Manifest  { return &s.manifest }
func (s *fakeSession) Database() *Database  { return nil }
func (s *fakeSession) WidgetDataSources() *WidgetDataSources {
	return nil
}
func (s *fakeSession) Reference() Hub { return nil }
func (s *fakeSession) TargetSessions() targetsession.TargetSessionProvider {
	return nil
}

// SessionRenderFS returns nil for the fake session.
func (s *fakeSession) Rerender() SessionRenderFS {
	return nil
}

func (s *fakeSession) SetRerenderFS(SessionRenderFS) {}

type fakeStartRenderSessionFactory struct {
	session Session
	err     error
}

type staticDataSource struct {
	table TableRef
	err   error
}

func (s staticDataSource) Resolve(Session, RendererList) (TableRef, error) {
	return s.table, s.err
}

func (f *fakeStartRenderSessionFactory) NewSession(
	*ContentMap,
	RendererList,
	DatabaseFactory,
	SessionRenderFS,
	targetsession.TargetSessionProvider,
) (Session, error) {
	return f.session, f.err
}

func newLifecycleMockRenderer(t *testing.T, initializeErr error) *MockRenderer {
	t.Helper()

	renderer := &MockRenderer{}
	renderer.On("Configure", mock.Anything).Return(nil)
	renderer.On("GetInputSpec").Return(InputSpec{})
	renderer.On("GetOutputSpec").Return(OutputSpec{})
	renderer.On("Initialize", mock.Anything, mock.Anything).Return(initializeErr)
	t.Cleanup(func() {
		renderer.AssertExpectations(t)
	})

	return renderer
}

func TestInitializeRenderers(t *testing.T) {
	t.Run("emits pending outputs and skips initialize when input data source is pending", func(t *testing.T) {
		session := newFakeSession([]string{"runA"})
		rendererID := "pending-renderer-id"
		inputComponent := cdf.ComponentType{Name: "pending_input", SchemaVersion: "1.0"}
		outputComponent := cdf.ComponentType{Name: "pending_output", SchemaVersion: "1.0"}
		session.Manifest().AddEntry(ManifestEntryInfo{
			componentType:     inputComponent,
			rendererIdentity:  RendererIdentity{Index: 1, Name: "upstream_renderer"},
			associatedContent: []run.RunID{{Value: "runA"}},
			pending:           true,
		})
		inputSpec := InputSpec{
			PortList: PortList{
				Ports: []PortSpec{
					{Name: "input", ComponentType: inputComponent},
				},
			},
		}
		outputSpec := OutputSpec{
			PortList: PortList{
				Ports: []PortSpec{
					{Name: "output", ComponentType: outputComponent},
				},
			},
		}

		renderer := &MockRenderer{}
		renderer.On("GetInputSpec").Return(inputSpec)
		renderer.On("GetOutputSpec").Return(outputSpec)
		defer renderer.AssertExpectations(t)

		rendererSpec := RendererSpec{
			Configs: RendererConfigList{
				{Name: "pending_renderer", ID: &rendererID},
			},
			DataSources: []map[string][]DataSource{
				{
					"input": {
						staticDataSource{
							table: TableRef{Name: "pending_input", Pending: true},
						},
					},
				},
			},
		}

		initializeErrs, err := initializeRenderers(context.Background(), session, rendererSpec, RendererList{renderer}, []error{nil})
		require.NoError(t, err)
		require.Len(t, initializeErrs, 1)
		require.ErrorIs(t, initializeErrs[0], cdf.ErrComponentPending)
		renderer.AssertNotCalled(t, "Initialize", mock.Anything, mock.Anything)

		entries := session.Manifest().Entries()
		require.Len(t, entries, 2)
		var info *ManifestEntryInfo
		for i := range entries {
			if entries[i].Info().ComponentType() == outputComponent {
				info = entries[i].Info()
				break
			}
		}
		require.NotNil(t, info)
		require.True(t, info.Pending())
		require.Equal(t, outputComponent, info.ComponentType())
		require.Equal(t, RendererIdentity{Index: 0, ID: &rendererID, Name: "pending_renderer"}, info.RendererIdentity())
		require.Equal(t, []run.RunID{{Value: "runA"}}, info.AssociatedContent())
	})

	t.Run("emits pending outputs when initialize returns component pending", func(t *testing.T) {
		session := newFakeSession([]string{"runA"})
		rendererID := "pending-renderer-id"
		outputComponent := cdf.ComponentType{Name: "pending_output", SchemaVersion: "1.0"}
		outputSpec := OutputSpec{
			PortList: PortList{
				Ports: []PortSpec{
					{Name: "output", ComponentType: outputComponent},
				},
			},
		}

		renderer := &MockRenderer{}
		renderer.On("GetInputSpec").Return(InputSpec{})
		renderer.On("GetOutputSpec").Return(outputSpec)
		renderer.On("Initialize", session, mock.Anything).Return(cdf.ErrComponentPending)
		defer renderer.AssertExpectations(t)

		rendererSpec := RendererSpec{
			Configs: RendererConfigList{
				{Name: "pending_renderer", ID: &rendererID},
			},
			DataSources: []map[string][]DataSource{nil},
		}

		initializeErrs, err := initializeRenderers(context.Background(), session, rendererSpec, RendererList{renderer}, []error{nil})
		require.NoError(t, err)
		require.Len(t, initializeErrs, 1)
		require.ErrorIs(t, initializeErrs[0], cdf.ErrComponentPending)

		entries := session.Manifest().Entries()
		require.Len(t, entries, 1)
		info := entries[0].Info()
		require.True(t, info.Pending())
		require.Equal(t, outputComponent, info.ComponentType())
		require.Equal(t, RendererIdentity{Index: 0, ID: &rendererID, Name: "pending_renderer"}, info.RendererIdentity())
		require.Equal(t, []run.RunID{{Value: "runA"}}, info.AssociatedContent())
	})
}

func TestRendererConfigGetDisplayID(t *testing.T) {
	t.Run("returns configured ID", func(t *testing.T) {
		id := "renderer-a"
		config := RendererConfig{Name: "test-renderer", ID: &id}

		require.Equal(t, "renderer-a", config.GetDisplayID())
	})

	t.Run("returns undefined when ID is missing", func(t *testing.T) {
		config := RendererConfig{Name: "test-renderer"}

		require.Equal(t, "undefined", config.GetDisplayID())
	})
}

func TestStartRenderSession(t *testing.T) {
	stringPtr := func(value string) *string {
		return &value
	}
	newLoader := func(t *testing.T) *fakeRunLoader {
		t.Helper()
		model := cdf.NewOnDiskModel(t.TempDir(), &cdf.Manifest{}, cdf.Metadata{})
		return &fakeRunLoader{model: model}
	}
	start := func(
		t *testing.T,
		rendererFactory RendererFactory,
		rendererConfigs RendererConfigList,
	) (Session, []error, error) {
		t.Helper()

		sessionStorage := NewSessionStorage()
		session := newFakeSession([]string{"run-a"})
		return StartRenderSession(
			context.Background(),
			&fakeStartRenderSessionFactory{session: session},
			&sessionStorage,
			rendererFactory,
			newLoader(t),
			[]run.RunID{{Value: "run-a"}},
			rendererConfigs,
			WidgetConfigList{},
			nil,
			nil,
			nil,
		)
	}

	t.Run("returns wrapped error when create data sources fails", func(t *testing.T) {
		dupeID := stringPtr("duplicate-renderer")
		session, invocationErrors, err := start(
			t,
			&MockRendererFactory{},
			RendererConfigList{
				{Name: "renderer_a", ID: dupeID, ConfigJSON: "{}"},
				{Name: "renderer_b", ID: dupeID, ConfigJSON: "{}"},
			},
		)

		require.Error(t, err)
		require.ErrorContains(t, err, "creating data sources failed")
		require.ErrorContains(t, err, "duplicate renderer ID")
		require.Nil(t, session)
		require.Nil(t, invocationErrors)
	})

	t.Run("returns renderer spec errors as invocation errors", func(t *testing.T) {
		session, invocationErrors, err := start(
			t,
			&MockRendererFactory{},
			RendererConfigList{
				// Renderers depend on each other: cycle => renderer spec creation error
				{
					Name:       "renderer_a",
					ID:         stringPtr("renderer-a"),
					ConfigJSON: `{"data_source":{"tables":{"in":[{"renderer_id":"renderer-b","output":"out"}]}}}`,
				},
				{
					Name:       "renderer_b",
					ID:         stringPtr("renderer-b"),
					ConfigJSON: `{"data_source":{"tables":{"in":[{"renderer_id":"renderer-a","output":"out"}]}}}`,
				},
			},
		)

		require.NoError(t, err)
		require.Nil(t, session)
		require.Len(t, invocationErrors, 2)
		for i := range 2 {
			require.ErrorIs(t, invocationErrors[i], message.New(message.EngineRenderRendererspecRendererDependencyCycle))
			require.NoError(t, message.ValidateMetadataPlaceholders(invocationErrors[i]))
		}
	})

	t.Run("returns renderer creation and initialization errors in invocation errors", func(t *testing.T) {
		creationErr := errors.New("renderer construction failed")
		initializationErr := errors.New("renderer initialization failed")

		workingRenderer := newLifecycleMockRenderer(t, nil)

		creationFailingRenderer := &MockRenderer{}
		defer creationFailingRenderer.AssertNotCalled(t, "Configure", mock.Anything)
		defer creationFailingRenderer.AssertNotCalled(t, "Initialize", mock.Anything, mock.Anything)

		initializationFailingRenderer := newLifecycleMockRenderer(t, initializationErr)

		rendererFactory := &MockRendererFactory{}
		rendererFactory.On("NewRenderer", "working_renderer").Return(workingRenderer, nil)
		rendererFactory.On("NewRenderer", "creation_failing_renderer").Return(creationFailingRenderer, creationErr)
		rendererFactory.On("NewRenderer", "initialization_failing_renderer").Return(initializationFailingRenderer, nil)
		defer rendererFactory.AssertExpectations(t)

		session, invocationErrors, err := start(
			t,
			rendererFactory,
			RendererConfigList{
				{Name: "working_renderer", ID: stringPtr("working-renderer"), ConfigJSON: "{}"},
				{Name: "creation_failing_renderer", ID: stringPtr("creation-failing-renderer"), ConfigJSON: "{}"},
				{Name: "initialization_failing_renderer", ID: stringPtr("initialization-failing-renderer"), ConfigJSON: "{}"},
			},
		)

		require.NoError(t, err)
		require.NotNil(t, session)
		require.Len(t, invocationErrors, 3)

		require.NoError(t, invocationErrors[0])

		require.Error(t, invocationErrors[1])
		require.ErrorContains(t, invocationErrors[1], "failed to construct renderer with name 'creation_failing_renderer'")
		require.ErrorIs(t, invocationErrors[1], creationErr)

		require.Error(t, invocationErrors[2])
		require.ErrorContains(t, invocationErrors[2], "failed to initialize renderer 'initialization_failing_renderer'")
		require.ErrorIs(t, invocationErrors[2], initializationErr)
	})

	t.Run("returns wrapped error when renderer initialization cannot start", func(t *testing.T) {
		session, invocationErrors, err := start(t, &MockRendererFactory{}, RendererConfigList{})

		require.Error(t, err)
		require.ErrorContains(t, err, "starting session failed")
		require.ErrorContains(t, err, "no renderer specified")
		require.Nil(t, session)
		require.Nil(t, invocationErrors)
	})
}

func TestValidateRendererOutputSuccess(t *testing.T) {
	session := newFakeSession([]string{"runA", "runB"})
	rendererIndex := 2

	renderer := &MockRenderer{}
	perRunComponent := cdf.ComponentType{Name: "per_run_component", SchemaVersion: "1.0"}
	aggregateComponent := cdf.ComponentType{Name: "aggregate_component", SchemaVersion: "2.0"}

	runIDs := []run.RunID{{Value: "runA"}, {Value: "runB"}}

	// Entry from another renderer should be ignored.
	otherIdentity := RendererIdentity{Index: rendererIndex + 1, Name: "OtherRenderer"}
	otherInfo := NewManifestEntryInfo(perRunComponent, otherIdentity, []run.RunID{{Value: "runA"}})
	session.Manifest().AddEntry(otherInfo)

	for _, id := range runIDs {
		info := NewManifestEntryInfo(perRunComponent, RendererIdentity{Index: rendererIndex, Name: "TestRenderer"}, []run.RunID{id})
		session.Manifest().AddEntry(info)
	}

	info := NewManifestEntryInfo(aggregateComponent, RendererIdentity{Index: rendererIndex, Name: "TestRenderer"}, runIDs)
	session.Manifest().AddEntry(info)

	spec := OutputSpec{
		PortList: PortList{
			Ports: []PortSpec{
				{Name: "per_run", Cardinality: CardinalityPerRun, ComponentType: perRunComponent},
				{Name: "aggregate", Cardinality: CardinalityOne, ComponentType: aggregateComponent},
			},
		},
	}

	renderer.On("GetOutputSpec").Return(spec)
	defer renderer.AssertExpectations(t)

	identity := RendererIdentity{Index: rendererIndex, Name: "TestRenderer"}
	err := ValidateRendererOutput(session, renderer, identity)
	require.NoError(t, err)
}

func TestValidateRendererOutputUnexpectedComponent(t *testing.T) {
	session := newFakeSession([]string{"runA"})
	rendererIndex := 0

	renderer := &MockRenderer{}
	componentSpec := cdf.ComponentType{Name: "expected_component", SchemaVersion: "1.0"}
	unexpectedComponent := cdf.ComponentType{Name: "unexpected_component", SchemaVersion: "0.1"}

	expectedInfo := NewManifestEntryInfo(componentSpec, RendererIdentity{Index: rendererIndex, Name: "TestRenderer"}, []run.RunID{{Value: "runA"}})
	session.Manifest().AddEntry(expectedInfo)

	unexpectedInfo := NewManifestEntryInfo(unexpectedComponent, RendererIdentity{Index: rendererIndex, Name: "TestRenderer"}, []run.RunID{{Value: "runA"}})
	unexpectedTable := session.Manifest().AddEntry(unexpectedInfo)

	spec := OutputSpec{
		PortList: PortList{
			Ports: []PortSpec{
				{Name: "expected", Cardinality: CardinalityOne, ComponentType: componentSpec},
			},
		},
	}

	renderer.On("GetOutputSpec").Return(spec)
	defer renderer.AssertExpectations(t)

	identity := RendererIdentity{Index: rendererIndex, Name: "TestRenderer"}
	err := ValidateRendererOutput(session, renderer, identity)
	require.Error(t, err)
	require.ErrorContains(t, err, fmt.Sprintf("table '%s' not declared in output spec", unexpectedTable))
}

func TestValidateRendererOutputFailsWhenMissingDeclaredOutput(t *testing.T) {
	session := newFakeSession([]string{"runA"})
	rendererIndex := 1

	renderer := &MockRenderer{}
	perRunComponent := cdf.ComponentType{Name: "per_run_component", SchemaVersion: "1.0"}

	spec := OutputSpec{
		PortList: PortList{
			Ports: []PortSpec{
				{Name: "per_run", Cardinality: CardinalityPerRun, ComponentType: perRunComponent},
			},
		},
	}

	renderer.On("GetOutputSpec").Return(spec)
	defer renderer.AssertExpectations(t)

	identity := RendererIdentity{Index: rendererIndex, Name: "TestRenderer"}
	err := ValidateRendererOutput(session, renderer, identity)
	require.Error(t, err)
	require.ErrorContains(t, err, "no table produced for declared output 'per_run'")
}

func TestValidateRendererOutputDoesNotCountEntriesFromOtherRenderer(t *testing.T) {
	session := newFakeSession([]string{"runA"})
	rendererIndex := 4

	renderer := &MockRenderer{}
	component := cdf.ComponentType{Name: "expected_component", SchemaVersion: "1.0"}

	// Entry belonging to another renderer (same component) should not satisfy our spec.
	otherIdentity := RendererIdentity{Index: rendererIndex + 1, Name: "OtherRenderer"}
	otherInfo := NewManifestEntryInfo(component, otherIdentity, []run.RunID{{Value: "runA"}})
	session.Manifest().AddEntry(otherInfo)

	spec := OutputSpec{
		PortList: PortList{
			Ports: []PortSpec{
				{Name: "expected", ComponentType: component},
			},
		},
	}

	renderer.On("GetOutputSpec").Return(spec)
	defer renderer.AssertExpectations(t)

	targetIdentity := RendererIdentity{Index: rendererIndex, Name: "TargetRenderer"}
	err := ValidateRendererOutput(session, renderer, targetIdentity)
	require.Error(t, err)
	require.ErrorContains(t, err, "no table produced for declared output 'expected'")
}

func TestValidateRendererOutputIgnoresHiddenEntries(t *testing.T) {
	session := newFakeSession([]string{"runA"})
	rendererIndex := 2

	renderer := &MockRenderer{}
	component := cdf.ComponentType{Name: "expected_component", SchemaVersion: "1.0"}

	hiddenInfo := NewManifestEntryInfo(component, RendererIdentity{Index: rendererIndex, Name: "HiddenRenderer"}, []run.RunID{{Value: "runA"}})
	session.Manifest().AddEntryHidden(hiddenInfo)

	spec := OutputSpec{
		PortList: PortList{
			Ports: []PortSpec{
				{Name: "expected", ComponentType: component},
			},
		},
	}

	renderer.On("GetOutputSpec").Return(spec)
	defer renderer.AssertExpectations(t)

	targetIdentity := RendererIdentity{Index: rendererIndex, Name: "HiddenRenderer"}
	err := ValidateRendererOutput(session, renderer, targetIdentity)
	require.Error(t, err)
	require.ErrorContains(t, err, "no table produced for declared output 'expected'")
}
