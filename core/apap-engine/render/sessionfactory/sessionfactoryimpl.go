// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package sessionfactory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"sync/atomic"

	log "github.com/sirupsen/logrus"

	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/render/reference"
	"github.com/Arm-Debug/apap-cli/apap-engine/targetsession"
)

type sessionImpl struct {
	id             string
	dbKey          string
	content        *render.ContentMap
	renderers      render.RendererList
	database       *render.Database
	manifest       render.Manifest
	referenceHub   render.Hub
	widgetSources  *render.WidgetDataSources
	rerenderFS     render.SessionRenderFS
	targetSessions targetsession.TargetSessionProvider
}

func (s *sessionImpl) ID() string {
	return s.id
}

func (s *sessionImpl) DatabaseKey() string {
	return s.dbKey
}

func (s *sessionImpl) Content() *render.ContentMap {
	return s.content
}

func (s *sessionImpl) Manifest() *render.Manifest {
	return &s.manifest
}

func (s *sessionImpl) Database() *render.Database {
	return s.database
}

func (s *sessionImpl) Reference() render.Hub {
	return s.referenceHub
}

func (s *sessionImpl) TargetSessions() targetsession.TargetSessionProvider {
	return s.targetSessions
}
func (s *sessionImpl) WidgetDataSources() *render.WidgetDataSources {
	return s.widgetSources
}

// Rerender returns the rerender filesystem for this session.
func (s *sessionImpl) Rerender() render.SessionRenderFS {
	return s.rerenderFS
}

func (s *sessionImpl) Close() {
	if s.referenceHub != nil {
		s.referenceHub.Close()
	}

	if s.rerenderFS != nil {
		if err := s.rerenderFS.Cleanup(); err != nil {
			log.WithError(err).Warn("failed to cleanup rerender entities on session close")
		}
	}
	if s.database != nil {
		destroySessionSchema(s.database, s.id)

		s.database.Close()
	}
}

// getSessionSchemaName returns the fully-qualified schema name for a render session inside ATP's compressed catalog.
func getSessionSchemaName(sessionId string) string {
	return render.DuckDBCompressedCatalogName + "." + sessionId
}

func createSessionSchema(db *render.Database, sessionId string) error {
	schemaName := getSessionSchemaName(sessionId)

	_, err := db.Conn.ExecContext(context.Background(), fmt.Sprintf("CREATE SCHEMA %s;", schemaName))
	if err != nil {
		return fmt.Errorf("could not create schema '%s': %w", schemaName, err)
	}
	_, err = db.Conn.ExecContext(context.Background(), fmt.Sprintf("USE %s;", schemaName))
	if err != nil {
		return fmt.Errorf("could not set schema to '%s': %w", schemaName, err)
	}
	return nil
}

func destroySessionSchema(db *render.Database, sessionId string) {
	schemaName := getSessionSchemaName(sessionId)

	// In the future, we'll want more granular cleanup of individual tables, when minimal re-rendering is implemented,
	// as there will be views from later schemas that reference tables from earlier schemas. For now, though, we can
	// just drop the entire schema
	_, err := db.Conn.ExecContext(context.Background(), fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schemaName))
	if err != nil {
		log.Errorf("Failed to drop schema '%s': %s\n", schemaName, err)
	}
}

type Impl struct {
	DBKeyOverride *string
	Flags         *render.SessionFlags
}

var sessionNum = atomic.Int32{}

func contentKeyFromMap(content *render.ContentMap) string {
	if content == nil || len(content.Entries) == 0 {
		return ""
	}

	ids := make([]string, 0, len(content.Entries))
	seen := make(map[string]struct{}, len(content.Entries))
	for _, entry := range content.Entries {
		value := entry.ID.Value
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		ids = append(ids, value)
	}
	sort.Strings(ids)

	hasher := sha256.New()
	for _, id := range ids {
		_, _ = hasher.Write([]byte(id))
		_, _ = hasher.Write([]byte{0})
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

func sessionRunRoots(content *render.ContentMap) []string {
	if content == nil {
		return nil
	}

	seen := make(map[string]struct{}, len(content.Entries))
	runRoots := make([]string, 0, len(content.Entries))
	for _, entry := range content.Entries {
		for _, runRoot := range entry.ExternalAccessRoots {
			if runRoot == "" {
				continue
			}
			if _, ok := seen[runRoot]; ok {
				continue
			}

			seen[runRoot] = struct{}{}
			runRoots = append(runRoots, runRoot)
		}
	}

	sort.Strings(runRoots)
	return runRoots
}

func (s *Impl) sessionFlags() render.SessionFlags {
	if s.Flags == nil {
		return render.DefaultSessionFlags()
	}

	return *s.Flags
}

func (s *Impl) NewSession(content *render.ContentMap, renderers render.RendererList, dbFactory render.DatabaseFactory, rerender render.SessionRenderFS, targetSessions targetsession.TargetSessionProvider) (
	render.Session, error,
) {
	dbKey := ""
	if s.DBKeyOverride != nil {
		dbKey = *s.DBKeyOverride
	} else {
		// Use a hash of the content as the DB key, so that sessions with the same content can share the same in-memory
		// DB. It is possible to share a single DB across all sessions, as sessions are still isolated by schema, but
		// that would mean no isolation between sessions at all, and would make debugging of memory usage more difficult.
		dbKey = contentKeyFromMap(content)
	}

	database, err := dbFactory.Connect(dbKey)
	if err != nil {
		return nil, fmt.Errorf("could not connect to database: %w", err)
	}
	sessionCreated := false
	defer func() {
		if !sessionCreated {
			database.Close()
		}
	}()

	runRoots := sessionRunRoots(content)
	flags := s.sessionFlags()
	if flags.EnableDuckDBSandbox {
		if err := render.ApplyDuckDBSandboxForRunRoots(dbKey, database, runRoots); err != nil {
			return nil, fmt.Errorf("could not apply duckdb sandbox: %w", err)
		}
	}

	// Iterate the session number so that subsequent renders do not collide
	num := sessionNum.Add(1)
	// Prepend "s" so that we don't hit errors creating table names starting with numbers
	sessionID := "s" + strconv.Itoa(int(num))

	err = createSessionSchema(database, sessionID)
	if err != nil {
		return nil, err
	}

	manifest := render.NewManifest()

	referenceHub, err := reference.NewHub(database, &manifest)
	if err != nil {
		return nil, err
	}

	sessionCreated = true
	return &sessionImpl{
		id:             sessionID,
		dbKey:          dbKey,
		content:        content,
		renderers:      renderers,
		database:       database,
		manifest:       manifest,
		referenceHub:   referenceHub,
		widgetSources:  render.NewWidgetDataSources(),
		rerenderFS:     rerender,
		targetSessions: targetSessions,
	}, nil
}
