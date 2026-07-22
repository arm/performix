// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/targetsession"
)

type testSession struct {
	id    string
	db    *render.Database
	dbKey string
}

func (s *testSession) ID() string {
	return s.id
}

func (s *testSession) Close() {
	if s.db != nil {
		s.db.Close()
	}
}

func (s *testSession) Content() *render.ContentMap {
	return nil
}

func (s *testSession) Manifest() *render.Manifest {
	return nil
}

func (s *testSession) Database() *render.Database {
	return s.db
}

func (s *testSession) WidgetDataSources() *render.WidgetDataSources {
	return nil
}

func (s *testSession) Reference() render.Hub {
	return nil
}

func (s *testSession) DatabaseKey() string {
	return s.dbKey
}

func (s *testSession) Rerender() render.SessionRenderFS {
	return nil
}

func (s *testSession) TargetSessions() targetsession.TargetSessionProvider {
	return nil
}

func TestHTTPQueryServerStartAndQueryIPCStream(t *testing.T) {
	dbKey := "test-db"
	db, err := (&render.DuckDBFactory{}).Connect(dbKey)
	require.NoError(t, err)
	defer db.Close()

	session := &testSession{id: "session-1", db: db, dbKey: dbKey}
	apapServer := &ApapServer{sessions: render.NewSessionStorage()}
	require.NoError(t, apapServer.sessions.AddRenderSession(session))

	var listener net.Listener
	listen := func(network string, address string) (net.Listener, error) {
		l, err := net.Listen(network, address)
		if err != nil {
			return nil, err
		}
		listener = l
		return l, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	httpServer := NewHTTPQueryServer("127.0.0.1", 0, 0, apapServer, listen)
	require.NoError(t, httpServer.Start(ctx, cancel))
	require.NotNil(t, listener)

	url := fmt.Sprintf(
		"http://%s/query?session_id=%s&sql=select+1",
		listener.Addr().String(),
		session.ID(),
	)
	resp, err := http.Get(url) // #nosec G107
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Greater(t, len(body), 0)
}
