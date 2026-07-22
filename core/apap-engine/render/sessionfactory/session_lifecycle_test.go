// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package sessionfactory

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/ipc"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/query"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

type RenderSessionFixture struct {
	Loader          *render.MockRunLoader
	RendererFactory *render.MockRendererFactory
	Renderers       []*render.MockRenderer
	Session         render.Session
}

// schemaTableExists returns whether the given table exists in the provided catalog and schema.
func schemaTableExists(t *testing.T, db *render.Database, catalogName string, schemaName string, tableName string) bool {
	t.Helper()

	var count int
	err := db.Conn.QueryRowContext(
		context.Background(),
		`SELECT COUNT(*)
		FROM information_schema.tables
		WHERE table_catalog = ? AND table_schema = ? AND table_name = ?`,
		catalogName,
		schemaName,
		tableName,
	).Scan(&count)
	require.NoError(t, err)
	return count > 0
}

// tableExists returns whether the given table name exists in the current session schema.
func tableExists(t *testing.T, session render.Session, tableName string) bool {
	t.Helper()

	var count int
	err := session.Database().Conn.QueryRowContext(
		context.Background(),
		"SELECT COUNT(*) FROM information_schema.tables WHERE table_name = ?",
		tableName,
	).Scan(&count)
	require.NoError(t, err)
	return count > 0
}

// manifestContainsTable returns whether the manifest still tracks the given table name.
func manifestContainsTable(t *testing.T, manifest *render.Manifest, tableName string) bool {
	t.Helper()

	_, err := manifest.GetEntry(tableName)
	return err == nil
}

func createSuccessfulTestRenderSession(t *testing.T, contentSelection []run.RunID, rendererConfigs render.RendererConfigList) (RenderSessionFixture, []error, error) {
	loader := render.MockRunLoader{}
	for _, runID := range contentSelection {
		loader.On("LoadRun", runID).Return(&cdf.OnDiskModel{}, nil)
	}

	rendererFactory := render.MockRendererFactory{}
	var mockRenderers []*render.MockRenderer
	for i, config := range rendererConfigs {
		renderer := render.MockRenderer{}

		rendererFactory.On("NewRenderer", config.Name).Return(&renderer, nil).Times(1)

		renderer.On("Configure", &render.Config{Identity: render.RendererIdentity{Index: i, ID: nil, Name: config.Name}, JSON: config.ConfigJSON}).Return(nil)
		renderer.On("Initialize", mock.Anything, mock.Anything).Return(nil)
		renderer.On("GetInputSpec").Return(render.InputSpec{})
		renderer.On("GetOutputSpec").Return(render.OutputSpec{})

		mockRenderers = append(mockRenderers, &renderer)
	}

	databaseFactory := render.DuckDBFactory{}

	sessionStorage := render.NewSessionStorage()
	defer sessionStorage.CloseAllRenderSessions()

	session, invocationErrors, err := render.StartRenderSession(
		context.Background(),
		&Impl{},
		&sessionStorage,
		&rendererFactory,
		&loader,
		contentSelection,
		rendererConfigs,
		render.WidgetConfigList{{}},
		&databaseFactory,
		nil,
		nil,
	)

	return RenderSessionFixture{
		Loader:          &loader,
		RendererFactory: &rendererFactory,
		Renderers:       mockRenderers,
		Session:         session,
	}, invocationErrors, err
}

func TestStartRenderSession(t *testing.T) {
	t.Run("fails if session factory fails", func(t *testing.T) {
		expectedError := errors.New("rekt")

		sessionFactory := render.MockSessionFactory{}
		sessionFactory.On("NewSession", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return(&render.MockSession{}, expectedError)

		loader := render.MockRunLoader{}
		loader.On("LoadRun", mock.Anything).Return(&cdf.OnDiskModel{}, nil)

		databaseFactory := render.DuckDBFactory{}

		sessionStorage := render.NewSessionStorage()

		session, invocationErrors, err := render.StartRenderSession(
			context.Background(),
			&sessionFactory,
			&sessionStorage,
			&render.MockRendererFactory{},
			&loader,
			[]run.RunID{{Value: "a"}},
			render.RendererConfigList{},
			render.WidgetConfigList{{}},
			&databaseFactory,
			nil,
			nil,
		)

		assert.ErrorContains(t, err, expectedError.Error())
		assert.Nil(t, session)
		assert.Equal(t, 0, sessionStorage.SessionCount())
		assert.Nil(t, invocationErrors)
	})

	t.Run("persists previous session even if error constructing new one", func(t *testing.T) {
		previousSession := render.MockSession{}
		previousSession.On("ID").Return("previousId")
		previousSession.On("Close").Return()

		sessionFactory := render.MockSessionFactory{}
		sessionFactory.On("NewSession", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return(&render.MockSession{}, errors.New("rekt"))

		databaseFactory := render.DuckDBFactory{}

		sessionStorage := render.NewSessionStorage()
		err := sessionStorage.AddRenderSession(&previousSession)
		assert.NoError(t, err)

		session, _, _ := render.StartRenderSession(
			context.Background(),
			&sessionFactory,
			&sessionStorage,
			&render.MockRendererFactory{},
			&render.MockRunLoader{},
			[]run.RunID{},
			render.RendererConfigList{},
			render.WidgetConfigList{{}},
			&databaseFactory,
			nil,
			nil,
		)

		assert.Nil(t, session)
		assert.Equal(t, 1, sessionStorage.SessionCount())
	})

	t.Run("fails if content selection empty", func(t *testing.T) {
		sessionFactory := render.MockSessionFactory{}
		sessionFactory.On("NewSession", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return(&render.MockSession{}, nil)

		databaseFactory := render.DuckDBFactory{}

		sessionStorage := render.NewSessionStorage()

		session, invocationErrors, err := render.StartRenderSession(
			context.Background(),
			&sessionFactory,
			&sessionStorage,
			&render.MockRendererFactory{},
			&render.MockRunLoader{},
			[]run.RunID{},
			render.RendererConfigList{},
			render.WidgetConfigList{{}},
			&databaseFactory,
			nil,
			nil,
		)

		assert.ErrorContains(t, err, "empty content selection")
		assert.Equal(t, 0, sessionStorage.SessionCount())
		assert.Nil(t, session)
		assert.Nil(t, invocationErrors)
	})

	t.Run("content selection is loaded", func(t *testing.T) {
		runID0 := run.RunID{Value: "foo"}
		runID1 := run.RunID{Value: "bar"}

		fixture, invocationErrors, err := createSuccessfulTestRenderSession(
			t,
			[]run.RunID{runID0, runID1},
			render.RendererConfigList{render.RendererConfig{}},
		)

		assert.NoError(t, err)
		assert.NotNil(t, fixture.Session)
		assert.True(t, fixture.Session.Content().Contains(runID0))
		assert.True(t, fixture.Session.Content().Contains(runID1))
		assert.EqualValues(t, []error{nil}, invocationErrors)

		fixture.Loader.AssertExpectations(t)
	})

	t.Run("duplicate content produces warning", func(t *testing.T) {
		runID := run.RunID{Value: "foo"}

		var buf bytes.Buffer
		log.SetOutput(&buf)
		defer func() {
			log.SetOutput(os.Stderr)
		}()

		_, invocationErrors, err := createSuccessfulTestRenderSession(
			t,
			[]run.RunID{runID, runID},
			render.RendererConfigList{render.RendererConfig{}},
		)

		assert.NoError(t, err)
		assert.Len(t, invocationErrors, 1)
		assert.Nil(t, invocationErrors[0])
		assert.Contains(t, buf.String(), "Content selection contains duplicate run")
	})

	t.Run("fails if database factory fails", func(t *testing.T) {
		runID := run.RunID{Value: "foo"}

		expectedError := errors.New("rekt")

		loader := render.MockRunLoader{}
		loader.On("LoadRun", runID).Return(&cdf.OnDiskModel{}, nil)

		databaseFactory := render.MockDatabaseFactory{}
		databaseFactory.On("Connect", mock.Anything).Return(&render.Database{}, expectedError)
		sessionStorage := render.NewSessionStorage()

		session, invocationErrors, err := render.StartRenderSession(
			context.Background(),
			&Impl{},
			&sessionStorage,
			&render.MockRendererFactory{},
			&loader,
			[]run.RunID{runID},
			render.RendererConfigList{},
			render.WidgetConfigList{{}},
			&databaseFactory,
			nil,
			nil,
		)

		assert.ErrorContains(t, err, expectedError.Error())
		assert.Nil(t, session)
		assert.Equal(t, 0, sessionStorage.SessionCount())
		assert.Nil(t, invocationErrors)

		loader.AssertExpectations(t)
	})

	t.Run("fails if content loader fails", func(t *testing.T) {
		runID := run.RunID{Value: "foo"}

		expectedError := errors.New("rekt")

		loader := render.MockRunLoader{}
		loader.On("LoadRun", runID).Return(&cdf.OnDiskModel{}, expectedError)

		databaseFactory := render.DuckDBFactory{}
		sessionStorage := render.NewSessionStorage()

		session, invocationErrors, err := render.StartRenderSession(
			context.Background(),
			&Impl{},
			&sessionStorage,
			&render.MockRendererFactory{},
			&loader,
			[]run.RunID{runID},
			render.RendererConfigList{},
			render.WidgetConfigList{{}},
			&databaseFactory,
			nil,
			nil,
		)

		assert.ErrorContains(t, err, expectedError.Error())
		assert.Nil(t, session)
		assert.Equal(t, 0, sessionStorage.SessionCount())
		assert.Nil(t, invocationErrors)

		loader.AssertExpectations(t)
	})

	t.Run("fails if renderer list is empty", func(t *testing.T) {
		runID := run.RunID{Value: "foo"}

		loader := render.MockRunLoader{}
		loader.On("LoadRun", runID).Return(&cdf.OnDiskModel{}, nil)

		databaseFactory := render.DuckDBFactory{}
		sessionStorage := render.NewSessionStorage()

		session, invocationErrors, err := render.StartRenderSession(
			context.Background(),
			&Impl{},
			&sessionStorage,
			&render.MockRendererFactory{},
			&loader,
			[]run.RunID{runID},
			render.RendererConfigList{},
			render.WidgetConfigList{{}},
			&databaseFactory,
			nil,
			nil,
		)

		assert.ErrorContains(t, err, "no renderer specified")
		assert.Nil(t, session)
		assert.Nil(t, invocationErrors)
	})

	t.Run("session is closed if initialize fails", func(t *testing.T) {
		runID := run.RunID{Value: "foo"}

		sessionMocked := render.MockSession{}
		sessionMocked.On("Close").Return()
		sessionMocked.On("ID").Return("aa")
		manifest := render.NewManifest()
		sessionMocked.On("Manifest").Return(&manifest)
		mockDB, err := (&render.DuckDBFactory{}).Connect(t.Name())
		require.NoError(t, err)
		defer mockDB.Close()
		sessionMocked.On("Database").Return(mockDB)

		sessionFactory := render.MockSessionFactory{}
		sessionFactory.On("NewSession", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return(&sessionMocked, nil)

		loader := render.MockRunLoader{}
		loader.On("LoadRun", runID).Return(&cdf.OnDiskModel{}, nil)

		expectedError := errors.New("rekt")

		renderer := render.MockRenderer{}
		renderer.On("Configure", mock.Anything).Return(nil)
		renderer.On("Initialize", mock.Anything, mock.Anything).Return(expectedError)
		renderer.On("GetInputSpec").Return(render.InputSpec{})
		renderer.On("GetOutputSpec").Return(render.OutputSpec{})

		rendererFactory := render.MockRendererFactory{}
		rendererFactory.On("NewRenderer", mock.Anything).Return(&renderer, nil)

		rendererConfigs := render.RendererConfigList{render.RendererConfig{}}

		databaseFactory := render.DuckDBFactory{}
		sessionStorage := render.NewSessionStorage()

		session, invocationErrors, err := render.StartRenderSession(
			context.Background(),
			&sessionFactory,
			&sessionStorage,
			&rendererFactory,
			&loader,
			[]run.RunID{runID},
			rendererConfigs,
			render.WidgetConfigList{{}},
			&databaseFactory,
			nil,
			nil,
		)

		assert.Nil(t, err)
		assert.Len(t, invocationErrors, 1)
		assert.ErrorContains(t, invocationErrors[0], expectedError.Error())
		assert.Nil(t, session)

		renderer.AssertExpectations(t)
		rendererFactory.AssertExpectations(t)
		sessionMocked.AssertExpectations(t)
	})

	t.Run("renderers are constructed, configured, and initialized", func(t *testing.T) {
		runID := run.RunID{Value: "foo"}

		rendererConfigs := render.RendererConfigList{
			render.RendererConfig{Name: "some_renderer", ConfigJSON: `{"some_json": {"name": "foo"}}`},
			render.RendererConfig{Name: "some_renderer", ConfigJSON: `{"some_other_json": {"name": "foo"}}`},
			render.RendererConfig{Name: "some_other_renderer", ConfigJSON: `{"some_other_other_json": {"name": "foo"}}`},
		}

		fixture, invocationErrors, err := createSuccessfulTestRenderSession(
			t,
			[]run.RunID{runID},
			rendererConfigs,
		)

		assert.NoError(t, err)
		assert.NotNil(t, fixture.Session)
		assert.Len(t, invocationErrors, 3)
		assert.Nil(t, invocationErrors[0])
		assert.Nil(t, invocationErrors[1])
		assert.Nil(t, invocationErrors[2])

		fixture.RendererFactory.AssertExpectations(t)
		for _, renderer := range fixture.Renderers {
			renderer.AssertExpectations(t)
		}
	})

	t.Run("renderer ID appears in manifest entry", func(t *testing.T) {
		runID := run.RunID{Value: "foo"}
		rendererID := "custom-id"

		componentType := cdf.ComponentType{Name: "mock_component", SchemaVersion: "1.0"}

		var identity render.RendererIdentity
		// Setup a mock renderer that adds to the manifest
		renderer := render.MockRenderer{}
		renderer.On("Configure", mock.MatchedBy(func(cfg *render.Config) bool {
			identity = cfg.Identity
			return cfg.Identity.ID != nil && *cfg.Identity.ID == rendererID
		})).Return(nil)
		renderer.On("Initialize", mock.Anything, mock.Anything).Return(nil).Run(func(args mock.Arguments) {
			ctx := args.Get(0).(render.Session)
			ctx.Manifest().AddEntry(render.NewManifestEntryInfo(
				componentType,
				identity,
				[]run.RunID{runID},
			))
		})
		renderer.On("GetInputSpec").Return(render.InputSpec{})
		renderer.On("GetOutputSpec").Return(render.OutputSpec{})

		// Setup factory to return the mock renderer
		rendererFactory := render.MockRendererFactory{}
		rendererFactory.On("NewRenderer", "my_renderer").Return(&renderer, nil)

		loader := render.MockRunLoader{}
		loader.On("LoadRun", runID).Return(&cdf.OnDiskModel{}, nil)

		databaseFactory := render.DuckDBFactory{}

		sessionStorage := render.NewSessionStorage()
		rendererConfigs := render.RendererConfigList{
			render.RendererConfig{
				Name:       "my_renderer",
				ID:         &rendererID,
				ConfigJSON: "{}", // dummy config
			},
		}

		session, invocationErrors, err := render.StartRenderSession(
			context.Background(),
			&Impl{},
			&sessionStorage,
			&rendererFactory,
			&loader,
			[]run.RunID{runID},
			rendererConfigs,
			render.WidgetConfigList{{}},
			&databaseFactory,
			nil,
			nil,
		)

		assert.NoError(t, err)
		assert.NotNil(t, session)
		assert.Len(t, invocationErrors, 1)
		assert.Nil(t, invocationErrors[0])

		manifest := session.Manifest()
		found := false
		for _, entry := range manifest.Entries() {
			if entry.Info().ComponentType() == componentType {
				if entry.Info().RendererIdentity().ID != nil && *entry.Info().RendererIdentity().ID == rendererID {
					found = true
					break
				}
			}
		}
		assert.True(t, found, "expected renderer ID to appear in manifest entry")

		renderer.AssertExpectations(t)
		rendererFactory.AssertExpectations(t)
	})

	t.Run("fails if duplicate renderer IDs are provided", func(t *testing.T) {
		runID := run.RunID{Value: "foo"}

		rendererConfigs := render.RendererConfigList{
			{Name: "renderer_one", ID: proto.String("duplicateID"), ConfigJSON: "{}"},
			{Name: "renderer_two", ID: proto.String("duplicateID"), ConfigJSON: "{}"},
		}

		loader := render.MockRunLoader{}
		loader.On("LoadRun", runID).Return(&cdf.OnDiskModel{}, nil)

		databaseFactory := render.DuckDBFactory{}

		sessionStorage := render.NewSessionStorage()

		session, invocationErrors, err := render.StartRenderSession(
			context.Background(),
			&Impl{},
			&sessionStorage,
			&render.MockRendererFactory{},
			&loader,
			[]run.RunID{runID},
			rendererConfigs,
			render.WidgetConfigList{{}},
			&databaseFactory,
			nil,
			nil,
		)

		assert.ErrorContains(t, err, "duplicate renderer ID")
		assert.Nil(t, session)
		assert.Nil(t, invocationErrors)
	})

	t.Run("fails if renderer ID is invalid", func(t *testing.T) {
		runID := run.RunID{Value: "foo"}

		invalidIDs := []*string{
			proto.String("  leadingSpace"),
			proto.String("trailingSpace "),
			proto.String("has space"),
			proto.String("1startsWithNumber"),
			proto.String("_underscoreFirst"),
			proto.String(""), // explicitly empty
		}

		for _, id := range invalidIDs {
			rendererConfigs := render.RendererConfigList{
				{Name: "invalid_renderer", ID: id, ConfigJSON: "{}"},
			}

			loader := render.MockRunLoader{}
			loader.On("LoadRun", runID).Return(&cdf.OnDiskModel{}, nil)

			databaseFactory := render.DuckDBFactory{}

			sessionStorage := render.NewSessionStorage()

			session, invocationErrors, err := render.StartRenderSession(
				context.Background(),
				&Impl{},
				&sessionStorage,
				&render.MockRendererFactory{},
				&loader,
				[]run.RunID{runID},
				rendererConfigs,
				render.WidgetConfigList{{}},
				&databaseFactory,
				nil,
				nil,
			)

			assert.ErrorContains(t, err, "renderer ID")
			assert.Nil(t, session)
			assert.Nil(t, invocationErrors)
		}
	})

	t.Run("succeeds with error in invocation errors if one renderer fails during configuration", func(t *testing.T) {
		runID := run.RunID{Value: "foo"}

		successfulRenderer := render.MockRenderer{}
		successfulRenderer.On("Configure", mock.Anything).Return(nil)
		successfulRenderer.On("Initialize", mock.Anything, mock.Anything).Return(nil)
		successfulRenderer.On("GetInputSpec").Return(render.InputSpec{})
		successfulRenderer.On("GetOutputSpec").Return(render.OutputSpec{})

		failingRenderer := render.MockRenderer{}
		expectedErr := errors.New("renderer configuration failed")
		failingRenderer.On("Configure", mock.Anything).Return(expectedErr)

		// Initialize is not expected to be called for the failing renderer
		failingRenderer.On("Initialize", mock.Anything, mock.Anything).Maybe()

		rendererFactory := render.MockRendererFactory{}
		rendererFactory.On("NewRenderer", "working_renderer").Return(&successfulRenderer, nil)
		rendererFactory.On("NewRenderer", "failing_renderer").Return(&failingRenderer, nil)

		rendererConfigs := render.RendererConfigList{
			{Name: "working_renderer", ID: proto.String("r1"), ConfigJSON: "{}"},
			{Name: "failing_renderer", ID: proto.String("r2"), ConfigJSON: "{}"},
		}

		loader := render.MockRunLoader{}
		loader.On("LoadRun", runID).Return(&cdf.OnDiskModel{}, nil)

		databaseFactory := render.DuckDBFactory{}

		sessionStorage := render.NewSessionStorage()

		session, invocationErrors, err := render.StartRenderSession(
			context.Background(),
			&Impl{},
			&sessionStorage,
			&rendererFactory,
			&loader,
			[]run.RunID{runID},
			rendererConfigs,
			render.WidgetConfigList{{}},
			&databaseFactory,
			nil,
			nil,
		)

		assert.NoError(t, err)
		assert.NotNil(t, session)
		assert.Equal(t, 2, len(invocationErrors))
		assert.Nil(t, invocationErrors[0])                                // first renderer succeeded
		assert.ErrorContains(t, invocationErrors[1], expectedErr.Error()) // second renderer failed during Configure

		successfulRenderer.AssertExpectations(t)
		failingRenderer.AssertExpectations(t)
		rendererFactory.AssertExpectations(t)
	})

	t.Run("succeeds with error in invocation errors if one renderer fails during initialization", func(t *testing.T) {
		runID := run.RunID{Value: "foo"}

		successfulRenderer := render.MockRenderer{}
		successfulRenderer.On("Configure", mock.Anything).Return(nil)
		successfulRenderer.On("Initialize", mock.Anything, mock.Anything).Return(nil)
		successfulRenderer.On("GetInputSpec").Return(render.InputSpec{})
		successfulRenderer.On("GetOutputSpec").Return(render.OutputSpec{})

		failingRenderer := render.MockRenderer{}
		failingRenderer.On("Configure", mock.Anything).Return(nil)
		expectedErr := errors.New("renderer initialization failed")
		failingRenderer.On("Initialize", mock.Anything, mock.Anything).Return(expectedErr)
		failingRenderer.On("GetInputSpec").Return(render.InputSpec{})
		failingRenderer.On("GetOutputSpec").Return(render.OutputSpec{})

		rendererFactory := render.MockRendererFactory{}
		rendererFactory.On("NewRenderer", "working_renderer").Return(&successfulRenderer, nil)
		rendererFactory.On("NewRenderer", "failing_renderer").Return(&failingRenderer, nil)

		rendererConfigs := render.RendererConfigList{
			{Name: "working_renderer", ID: proto.String("r1"), ConfigJSON: "{}"},
			{Name: "failing_renderer", ID: proto.String("r2"), ConfigJSON: "{}"},
		}

		loader := render.MockRunLoader{}
		loader.On("LoadRun", runID).Return(&cdf.OnDiskModel{}, nil)

		databaseFactory := render.DuckDBFactory{}

		sessionStorage := render.NewSessionStorage()

		visID := "vis1"
		// Create a valid JSON config for the visualization, but referencing tables that won't exist.
		// Expect no error
		visConfigList := render.WidgetConfigList{
			{ID: &visID, ConfigJSON: `{"data_source": {"tables": {"delta": [{"renderer_id": "random", "output": "output1"}]}}}`},
		}
		session, invocationErrors, err := render.StartRenderSession(
			context.Background(),
			&Impl{},
			&sessionStorage,
			&rendererFactory,
			&loader,
			[]run.RunID{runID},
			rendererConfigs,
			visConfigList,
			&databaseFactory,
			nil,
			nil,
		)

		assert.NoError(t, err)
		assert.NotNil(t, session)
		assert.Equal(t, 2, len(invocationErrors))
		assert.Nil(t, invocationErrors[0])                                // first renderer succeeded
		assert.ErrorContains(t, invocationErrors[1], expectedErr.Error()) // second renderer failed
		assert.Equal(t, session.WidgetDataSources().Get(), map[string]render.TableRefMap{"vis1": nil})

		successfulRenderer.AssertExpectations(t)
		failingRenderer.AssertExpectations(t)
		rendererFactory.AssertExpectations(t)
	})

	t.Run("drops temp tables after successful renderer initialization", func(t *testing.T) {
		runID := run.RunID{Value: "foo"}
		visibleTableName := ""
		tempTableName := ""

		renderer := render.MockRenderer{}
		renderer.On("Configure", mock.Anything).Return(nil)
		renderer.On("Initialize", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
			session := args.Get(0).(render.Session)
			identity := render.RendererIdentity{Index: 0, Name: "hidden_renderer"}

			tempTableName = session.Manifest().AddTempTable()
			_, err := session.Database().Conn.ExecContext(context.Background(), "CREATE TABLE \""+tempTableName+"\" (val INT)")
			require.NoError(t, err)

			visibleTableName = session.Manifest().AddEntry(render.NewManifestEntryInfo(
				cdf.ComponentType{Name: "visible_artifact", SchemaVersion: "1.0.0"},
				identity,
				[]run.RunID{runID},
			))
			_, err = session.Database().Conn.ExecContext(context.Background(), "CREATE TABLE \""+visibleTableName+"\" (val INT)")
			require.NoError(t, err)
		}).Return(nil)
		renderer.On("GetInputSpec").Return(render.InputSpec{})
		renderer.On("GetOutputSpec").Return(render.OutputSpec{})

		rendererFactory := render.MockRendererFactory{}
		rendererFactory.On("NewRenderer", "hidden_renderer").Return(&renderer, nil)

		loader := render.MockRunLoader{}
		loader.On("LoadRun", runID).Return(&cdf.OnDiskModel{}, nil)

		databaseFactory := render.DuckDBFactory{}
		sessionStorage := render.NewSessionStorage()
		defer sessionStorage.CloseAllRenderSessions()

		session, invocationErrors, err := render.StartRenderSession(
			context.Background(),
			&Impl{},
			&sessionStorage,
			&rendererFactory,
			&loader,
			[]run.RunID{runID},
			render.RendererConfigList{{Name: "hidden_renderer", ConfigJSON: "{}"}},
			render.WidgetConfigList{{}},
			&databaseFactory,
			nil,
			nil,
		)

		require.NoError(t, err)
		require.NotNil(t, session)
		require.Equal(t, []error{nil}, invocationErrors)
		assert.False(t, tableExists(t, session, tempTableName))
		assert.False(t, manifestContainsTable(t, session.Manifest(), tempTableName))
		assert.True(t, tableExists(t, session, visibleTableName))
		assert.True(t, manifestContainsTable(t, session.Manifest(), visibleTableName))

		renderer.AssertExpectations(t)
		rendererFactory.AssertExpectations(t)
	})

	t.Run("later renderers cannot see temp tables from previous renderers", func(t *testing.T) {
		runID := run.RunID{Value: "foo"}
		tempTableName := ""
		tempTableSeenBySecondRenderer := false

		firstRenderer := render.MockRenderer{}
		firstRenderer.On("Configure", mock.Anything).Return(nil)
		firstRenderer.On("Initialize", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
			session := args.Get(0).(render.Session)
			tempTableName = session.Manifest().AddTempTable()
			_, err := session.Database().Conn.ExecContext(context.Background(), "CREATE TABLE \""+tempTableName+"\" (val INT)")
			require.NoError(t, err)
		}).Return(nil)
		firstRenderer.On("GetInputSpec").Return(render.InputSpec{})
		firstRenderer.On("GetOutputSpec").Return(render.OutputSpec{})

		secondRenderer := render.MockRenderer{}
		secondRenderer.On("Configure", mock.Anything).Return(nil)
		secondRenderer.On("Initialize", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
			session := args.Get(0).(render.Session)
			tempTableSeenBySecondRenderer = tableExists(t, session, tempTableName)
		}).Return(nil)
		secondRenderer.On("GetInputSpec").Return(render.InputSpec{})
		secondRenderer.On("GetOutputSpec").Return(render.OutputSpec{})

		rendererFactory := render.MockRendererFactory{}
		rendererFactory.On("NewRenderer", "first_renderer").Return(&firstRenderer, nil)
		rendererFactory.On("NewRenderer", "second_renderer").Return(&secondRenderer, nil)

		loader := render.MockRunLoader{}
		loader.On("LoadRun", runID).Return(&cdf.OnDiskModel{}, nil)

		databaseFactory := render.DuckDBFactory{}
		sessionStorage := render.NewSessionStorage()
		defer sessionStorage.CloseAllRenderSessions()

		session, invocationErrors, err := render.StartRenderSession(
			context.Background(),
			&Impl{},
			&sessionStorage,
			&rendererFactory,
			&loader,
			[]run.RunID{runID},
			render.RendererConfigList{{Name: "first_renderer", ConfigJSON: "{}"}, {Name: "second_renderer", ConfigJSON: "{}"}},
			render.WidgetConfigList{{}},
			&databaseFactory,
			nil,
			nil,
		)

		require.NoError(t, err)
		require.NotNil(t, session)
		require.Equal(t, []error{nil, nil}, invocationErrors)
		assert.False(t, tempTableSeenBySecondRenderer)

		firstRenderer.AssertExpectations(t)
		secondRenderer.AssertExpectations(t)
		rendererFactory.AssertExpectations(t)
	})

	t.Run("cleans up temp tables when renderer initialization fails", func(t *testing.T) {
		runID := run.RunID{Value: "foo"}
		tempTableName := ""

		successfulRenderer := render.MockRenderer{}
		successfulRenderer.On("Configure", mock.Anything).Return(nil)
		successfulRenderer.On("Initialize", mock.Anything, mock.Anything).Return(nil)
		successfulRenderer.On("GetInputSpec").Return(render.InputSpec{})
		successfulRenderer.On("GetOutputSpec").Return(render.OutputSpec{})

		failingRenderer := render.MockRenderer{}
		failingRenderer.On("Configure", mock.Anything).Return(nil)
		failingRenderer.On("Initialize", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
			session := args.Get(0).(render.Session)
			tempTableName = session.Manifest().AddTempTable()
			_, err := session.Database().Conn.ExecContext(context.Background(), "CREATE TABLE \""+tempTableName+"\" (val INT)")
			require.NoError(t, err)
		}).Return(errors.New("renderer initialization failed"))
		failingRenderer.On("GetInputSpec").Return(render.InputSpec{})
		failingRenderer.On("GetOutputSpec").Return(render.OutputSpec{})

		rendererFactory := render.MockRendererFactory{}
		rendererFactory.On("NewRenderer", "working_renderer").Return(&successfulRenderer, nil)
		rendererFactory.On("NewRenderer", "failing_renderer").Return(&failingRenderer, nil)

		loader := render.MockRunLoader{}
		loader.On("LoadRun", runID).Return(&cdf.OnDiskModel{}, nil)

		databaseFactory := render.DuckDBFactory{}
		sessionStorage := render.NewSessionStorage()
		defer sessionStorage.CloseAllRenderSessions()

		session, invocationErrors, err := render.StartRenderSession(
			context.Background(),
			&Impl{},
			&sessionStorage,
			&rendererFactory,
			&loader,
			[]run.RunID{runID},
			render.RendererConfigList{{Name: "working_renderer", ConfigJSON: "{}"}, {Name: "failing_renderer", ConfigJSON: "{}"}},
			render.WidgetConfigList{{}},
			&databaseFactory,
			nil,
			nil,
		)

		require.NoError(t, err)
		require.NotNil(t, session)
		require.Len(t, invocationErrors, 2)
		assert.Nil(t, invocationErrors[0])
		assert.ErrorContains(t, invocationErrors[1], "renderer initialization failed")
		assert.False(t, tableExists(t, session, tempTableName))
		assert.False(t, manifestContainsTable(t, session.Manifest(), tempTableName))

		successfulRenderer.AssertExpectations(t)
		failingRenderer.AssertExpectations(t)
		rendererFactory.AssertExpectations(t)
	})
}

func schemaExists(t *testing.T, db *render.Database, schemaName string) bool {
	t.Helper()

	var count int
	err := db.Conn.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name = ? OR catalog_name || '.' || schema_name = ?",
		schemaName,
		schemaName,
	).Scan(&count)
	require.NoError(t, err)
	return count > 0
}

func arrowRowsFromQuery(t *testing.T, db *render.Database, sql string) []map[string]any {
	t.Helper()

	table, err := query.Execute(context.Background(), db, sql, query.ExecuteOptions{
		Format:   query.TableFormatArrowIPC,
		Settings: &query.ArrowIPCSettings{},
	})
	require.NoError(t, err)
	defer table.Close()

	byteStream, ok := table.(query.ByteStreamTableAccessor)
	require.True(t, ok)

	stream, err := byteStream.OpenReader()
	require.NoError(t, err)
	defer stream.Close()

	ipcReader, err := ipc.NewReader(stream)
	require.NoError(t, err)
	defer ipcReader.Release()

	var rows []map[string]any
	for ipcReader.Next() {
		rec := ipcReader.RecordBatch()
		require.NotNil(t, rec)

		data, err := json.Marshal(rec)
		require.NoError(t, err)

		var batch []map[string]any
		require.NoError(t, json.Unmarshal(data, &batch))
		rows = append(rows, batch...)

		rec.Release()
	}
	require.NoError(t, ipcReader.Err())
	return rows
}

func currentSettingValue(t *testing.T, db *render.Database, name string) any {
	t.Helper()

	var value any
	err := db.Conn.QueryRowContext(
		context.Background(),
		"SELECT current_setting(?)",
		name,
	).Scan(&value)
	require.NoError(t, err)
	return value
}

func currentSettingStringList(t *testing.T, db *render.Database, name string) []string {
	t.Helper()

	raw := currentSettingValue(t, db, name)
	items, ok := raw.([]interface{})
	require.True(t, ok, "expected current_setting(%q) to be a list, got %T", name, raw)

	result := make([]string, 0, len(items))
	for _, item := range items {
		s, ok := item.(string)
		require.True(t, ok, "expected current_setting(%q) item to be a string, got %T", name, item)
		result = append(result, s)
	}
	return result
}

func requireSettingOmitsPath(t *testing.T, db *render.Database, name string, unexpectedPath string) {
	t.Helper()

	rawPaths := currentSettingStringList(t, db, name)
	for _, rawPath := range rawPaths {
		require.NotEqual(t, filepath.Clean(unexpectedPath), filepath.Clean(rawPath))
	}
}

func requireSettingContainsPaths(t *testing.T, db *render.Database, name string, expectedPaths []string) {
	t.Helper()

	rawPaths := currentSettingStringList(t, db, name)
	actual := make(map[string]struct{}, len(rawPaths))
	for _, rawPath := range rawPaths {
		actual[filepath.Clean(rawPath)] = struct{}{}
	}

	for _, expectedPath := range expectedPaths {
		_, ok := actual[filepath.Clean(expectedPath)]
		require.True(t, ok, "expected current_setting(%q) to contain %q, got %v", name, expectedPath, rawPaths)
	}
}

func TestSessionSchemaLifecycle(t *testing.T) {
	dbFactory := render.DuckDBFactory{}
	sessionFactory := Impl{}

	content := &render.ContentMap{
		Entries: []render.ContentMapEntry{
			{ID: run.RunID{Value: "run-a"}, ExternalAccessRoots: []string{t.TempDir()}},
		},
	}

	session, err := sessionFactory.NewSession(content, nil, &dbFactory, nil, nil)
	require.NoError(t, err)

	holdDb, err := dbFactory.Connect(session.DatabaseKey())
	require.NoError(t, err)
	defer holdDb.Close()

	schemaName := getSessionSchemaName(session.ID())
	require.True(t, schemaExists(t, holdDb, schemaName))

	// Verify CREATE TABLE uses the session schema in the compressed catalog.
	_, err = session.Database().Conn.ExecContext(context.Background(), "CREATE TABLE session_schema_lifecycle (val INT)")
	require.NoError(t, err)
	require.True(t, tableExists(t, session, "session_schema_lifecycle"))
	require.True(t, schemaTableExists(t, holdDb, render.DuckDBCompressedCatalogName, session.ID(), "session_schema_lifecycle"))

	session.Close()
	require.False(t, schemaExists(t, holdDb, schemaName))
	require.False(t, schemaTableExists(t, holdDb, render.DuckDBCompressedCatalogName, session.ID(), "session_schema_lifecycle"))
}

func TestNewSessionRestrictsDuckDBExternalAccessToRunRoots(t *testing.T) {
	dbFactory := render.DuckDBFactory{}
	sessionFactory := Impl{}

	runRoot := t.TempDir()
	content := &render.ContentMap{
		Entries: []render.ContentMapEntry{
			{
				ID:                  run.RunID{Value: "run-a"},
				Model:               cdf.NewOnDiskModel(runRoot, &cdf.Manifest{}, cdf.Metadata{}),
				ExternalAccessRoots: []string{runRoot},
			},
		},
	}

	session, err := sessionFactory.NewSession(content, nil, &dbFactory, nil, nil)
	require.NoError(t, err)
	defer session.Close()

	requireSettingContainsPaths(t, session.Database(), "allowed_directories", []string{runRoot})
	require.Equal(t, false, currentSettingValue(t, session.Database(), "enable_external_access"))
	require.Equal(t, true, currentSettingValue(t, session.Database(), "lock_configuration"))
}

func TestNewSessionUsesBaseRunRootForOverlayContentAllowlist(t *testing.T) {
	dbFactory := render.DuckDBFactory{}
	sessionFactory := Impl{}

	runRoot := t.TempDir()
	overlayRoot := filepath.Join(runRoot, "render", "overlay-id")
	require.NoError(t, os.MkdirAll(overlayRoot, 0o755))

	baseModel := cdf.NewOnDiskModel(runRoot, &cdf.Manifest{}, cdf.Metadata{})
	overlayModel := cdf.NewOnDiskModel(overlayRoot, &cdf.Manifest{}, cdf.Metadata{})
	model, err := cdf.NewOverlayModel(baseModel, overlayModel)
	require.NoError(t, err)

	content := &render.ContentMap{
		Entries: []render.ContentMapEntry{
			{
				ID:                  run.RunID{Value: "run-a"},
				Model:               model,
				ExternalAccessRoots: []string{runRoot},
			},
		},
	}

	session, err := sessionFactory.NewSession(content, nil, &dbFactory, nil, nil)
	require.NoError(t, err)
	defer session.Close()

	requireSettingContainsPaths(t, session.Database(), "allowed_directories", []string{runRoot})
}

func TestNewSessionWithDuckDBSandboxDisabledLeavesDuckDBUnlocked(t *testing.T) {
	dbFactory := render.DuckDBFactory{}
	sessionFactory := Impl{
		Flags: &render.SessionFlags{EnableDuckDBSandbox: false},
	}

	runRoot := t.TempDir()
	content := &render.ContentMap{
		Entries: []render.ContentMapEntry{
			{
				ID:                  run.RunID{Value: "run-a"},
				Model:               cdf.NewOnDiskModel(runRoot, &cdf.Manifest{}, cdf.Metadata{}),
				ExternalAccessRoots: []string{runRoot},
			},
		},
	}

	session, err := sessionFactory.NewSession(content, nil, &dbFactory, nil, nil)
	require.NoError(t, err)
	defer session.Close()

	require.Equal(t, true, currentSettingValue(t, session.Database(), "enable_external_access"))
	require.Equal(t, false, currentSettingValue(t, session.Database(), "lock_configuration"))
	requireSettingOmitsPath(t, session.Database(), "allowed_directories", runRoot)
}

func TestSessionCloseOnlyDropsClosedSessionSchemaForSharedDB(t *testing.T) {
	dbFactory := render.DuckDBFactory{}
	sharedKey := "shared-db-schema-cleanup"
	sessionFactory := Impl{DBKeyOverride: &sharedKey}
	sharedRoot := t.TempDir()

	contentA := &render.ContentMap{
		Entries: []render.ContentMapEntry{
			{ID: run.RunID{Value: "run-a"}, ExternalAccessRoots: []string{sharedRoot}},
		},
	}
	contentB := &render.ContentMap{
		Entries: []render.ContentMapEntry{
			{ID: run.RunID{Value: "run-b"}, ExternalAccessRoots: []string{sharedRoot}},
		},
	}

	sessionA, err := sessionFactory.NewSession(contentA, nil, &dbFactory, nil, nil)
	require.NoError(t, err)
	defer sessionA.Close()
	sessionAID := sessionA.ID()

	sessionB, err := sessionFactory.NewSession(contentB, nil, &dbFactory, nil, nil)
	require.NoError(t, err)
	defer sessionB.Close()
	sessionBID := sessionB.ID()

	holdDb, err := dbFactory.Connect(sharedKey)
	require.NoError(t, err)
	defer holdDb.Close()

	// Create a same-named table in both sessions to prove the sessions are isolated by schema even
	// when they share the same underlying DuckDB instance.
	_, err = sessionA.Database().Conn.ExecContext(context.Background(), "CREATE TABLE session_scoped_table (val INT)")
	require.NoError(t, err)
	_, err = sessionA.Database().Conn.ExecContext(context.Background(), "INSERT INTO session_scoped_table VALUES (1)")
	require.NoError(t, err)

	_, err = sessionB.Database().Conn.ExecContext(context.Background(), "CREATE TABLE session_scoped_table (val INT)")
	require.NoError(t, err)
	_, err = sessionB.Database().Conn.ExecContext(context.Background(), "INSERT INTO session_scoped_table VALUES (2)")
	require.NoError(t, err)

	require.True(t, schemaTableExists(t, holdDb, render.DuckDBCompressedCatalogName, sessionAID, "session_scoped_table"))
	require.True(t, schemaTableExists(t, holdDb, render.DuckDBCompressedCatalogName, sessionBID, "session_scoped_table"))

	sessionA.Close()
	sessionA = nil

	require.False(t, schemaExists(t, holdDb, getSessionSchemaName(sessionAID)))
	require.False(t, schemaTableExists(t, holdDb, render.DuckDBCompressedCatalogName, sessionAID, "session_scoped_table"))
	require.True(t, schemaExists(t, holdDb, getSessionSchemaName(sessionBID)))
	require.True(t, schemaTableExists(t, holdDb, render.DuckDBCompressedCatalogName, sessionBID, "session_scoped_table"))

	var sessionBValue int
	require.NoError(t, sessionB.Database().Conn.QueryRowContext(
		context.Background(),
		"SELECT val FROM session_scoped_table",
	).Scan(&sessionBValue))
	require.Equal(t, 2, sessionBValue)

	sessionB.Close()
	sessionB = nil

	require.False(t, schemaExists(t, holdDb, getSessionSchemaName(sessionBID)))
	require.False(t, schemaTableExists(t, holdDb, render.DuckDBCompressedCatalogName, sessionBID, "session_scoped_table"))
}

func TestSessionQueriesUseCorrectSchemaForSharedDb(t *testing.T) {
	dbFactory := render.DuckDBFactory{}
	sharedKey := "shared-db"
	sessionFactory := Impl{DBKeyOverride: &sharedKey}
	sharedRoot := t.TempDir()

	contentA := &render.ContentMap{
		Entries: []render.ContentMapEntry{
			{ID: run.RunID{Value: "run-a"}, ExternalAccessRoots: []string{sharedRoot}},
		},
	}
	contentB := &render.ContentMap{
		Entries: []render.ContentMapEntry{
			{ID: run.RunID{Value: "run-b"}, ExternalAccessRoots: []string{sharedRoot}},
		},
	}

	sessionA, err := sessionFactory.NewSession(contentA, nil, &dbFactory, nil, nil)
	require.NoError(t, err)
	defer sessionA.Close()

	sessionB, err := sessionFactory.NewSession(contentB, nil, &dbFactory, nil, nil)
	require.NoError(t, err)
	defer sessionB.Close()

	_, err = sessionA.Database().Conn.ExecContext(context.Background(), "CREATE TABLE test (val INT)")
	require.NoError(t, err)
	_, err = sessionA.Database().Conn.ExecContext(context.Background(), "INSERT INTO test VALUES (1)")
	require.NoError(t, err)

	_, err = sessionB.Database().Conn.ExecContext(context.Background(), "CREATE TABLE test (val INT)")
	require.NoError(t, err)
	_, err = sessionB.Database().Conn.ExecContext(context.Background(), "INSERT INTO test VALUES (2)")
	require.NoError(t, err)

	accessorA, err := query.NewNativeRowTableAccessor(sessionA.Database(), "SELECT val FROM test", query.NativeRowSettings{
		RowsPerBatch: 1,
	})
	require.NoError(t, err)
	defer accessorA.Close()

	chunk, err := accessorA.NextChunk()
	require.NoError(t, err)
	require.Len(t, chunk, 1)
	require.Equal(t, int64(1), chunk[0]["val"])

	accessorB, err := query.NewNativeRowTableAccessor(sessionB.Database(), "SELECT val FROM test", query.NativeRowSettings{
		RowsPerBatch: 1,
	})
	require.NoError(t, err)
	defer accessorB.Close()

	chunk, err = accessorB.NextChunk()
	require.NoError(t, err)
	require.Len(t, chunk, 1)
	require.Equal(t, int64(2), chunk[0]["val"])

	rowsA := arrowRowsFromQuery(t, sessionA.Database(), "SELECT val FROM test")
	require.Len(t, rowsA, 1)
	require.Equal(t, float64(1), rowsA[0]["val"])

	rowsB := arrowRowsFromQuery(t, sessionB.Database(), "SELECT val FROM test")
	require.Len(t, rowsB, 1)
	require.Equal(t, float64(2), rowsB[0]["val"])
}
