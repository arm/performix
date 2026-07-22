// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	log "github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/render"
)

type stubQueryExecutor struct {
	reader    io.ReadCloser
	err       error
	sessionID string
	sql       string
}

func (s *stubQueryExecutor) QueryIPCStream(_ context.Context, sessionID string, sql string) (io.ReadCloser, error) {
	s.sessionID = sessionID
	s.sql = sql
	if s.err != nil {
		return nil, s.err
	}
	return s.reader, nil
}

type trackingReadCloser struct {
	*bytes.Reader
	closed bool
}

func (t *trackingReadCloser) Close() error {
	t.closed = true
	return nil
}

func TestHTTPQueryHandlerOptions(t *testing.T) {
	handler := newHTTPQueryHandler(&stubQueryExecutor{}, 0)

	req := httptest.NewRequest(http.MethodOptions, "/query", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, corsAllowMethods, rec.Header().Get("Access-Control-Allow-Methods"))
	assert.Equal(t, corsAllowHeaders, rec.Header().Get("Access-Control-Allow-Headers"))
	assert.Equal(t, corsExposeHeaders, rec.Header().Get("Access-Control-Expose-Headers"))
}

func TestHTTPQueryHandlerMissingParams(t *testing.T) {
	handler := newHTTPQueryHandler(&stubQueryExecutor{}, 0)

	req := httptest.NewRequest(http.MethodGet, "/query?sql=select+1", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))
}

func TestHTTPQueryHandlerMethodNotAllowed(t *testing.T) {
	handler := newHTTPQueryHandler(&stubQueryExecutor{}, 0)

	req := httptest.NewRequest(http.MethodPut, "/query", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestHTTPQueryHandlerMissingSQLQuery(t *testing.T) {
	handler := newHTTPQueryHandler(&stubQueryExecutor{}, 0)

	req := httptest.NewRequest(http.MethodGet, "/query?session_id=session-1", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHTTPQueryHandlerMissingSQLBody(t *testing.T) {
	handler := newHTTPQueryHandler(&stubQueryExecutor{}, 0)

	req := httptest.NewRequest(http.MethodPost, "/query?session_id=session-1", bytes.NewBufferString("   "))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHTTPQueryHandlerExecError(t *testing.T) {
	executor := &stubQueryExecutor{err: errors.New("boom")}
	handler := newHTTPQueryHandler(executor, 0)

	req := httptest.NewRequest(http.MethodGet, "/query?session_id=s1&sql=select+1", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestHTTPQueryHandlerExecErrorLogging(t *testing.T) {
	executor := &stubQueryExecutor{err: errors.New("boom")}
	handler := newHTTPQueryHandler(executor, 0)

	logger := log.StandardLogger()
	oldHooks := logger.ReplaceHooks(make(log.LevelHooks))
	oldOut := logger.Out
	logger.SetOutput(io.Discard)
	t.Cleanup(func() {
		logger.ReplaceHooks(oldHooks)
		logger.SetOutput(oldOut)
	})
	hook := logtest.NewLocal(logger)

	req := httptest.NewRequest(http.MethodGet, "/query?session_id=session-1&sql=select+1", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code)

	var requestID string
	var foundError bool
	for _, entry := range hook.AllEntries() {
		if entry.Message == "HTTP query request" {
			if value, ok := entry.Data["request_id"].(string); ok {
				requestID = value
			}
			continue
		}
		if entry.Message == "HTTP query failed" && entry.Level == log.ErrorLevel {
			foundError = true
			assert.Equal(t, requestID, entry.Data["request_id"])
			_, hasDuration := entry.Data["duration_ms"]
			assert.True(t, hasDuration)
		}
	}

	require.NotEmpty(t, requestID)
	assert.True(t, foundError)
}

func TestHTTPQueryHandlerSuccess(t *testing.T) {
	payload := []byte{0x01, 0x02, 0x03}
	reader := &trackingReadCloser{Reader: bytes.NewReader(payload)}
	executor := &stubQueryExecutor{reader: reader}
	handler := newHTTPQueryHandler(executor, 0)

	req := httptest.NewRequest(http.MethodGet, "/query?session_id=session-1&sql=select+1", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "application/vnd.apache.arrow.stream", resp.Header.Get("Content-Type"))
	assert.Equal(t, payload, body)
	assert.True(t, reader.closed)
	assert.Equal(t, "session-1", executor.sessionID)
	assert.Equal(t, "select 1", executor.sql)
}

func TestHTTPQueryHandlerLogging(t *testing.T) {
	payload := []byte{0x01, 0x02, 0x03}
	reader := &trackingReadCloser{Reader: bytes.NewReader(payload)}
	executor := &stubQueryExecutor{reader: reader}
	handler := newHTTPQueryHandler(executor, 0)

	logger := log.StandardLogger()
	oldHooks := logger.ReplaceHooks(make(log.LevelHooks))
	oldOut := logger.Out
	logger.SetOutput(io.Discard)
	t.Cleanup(func() {
		logger.ReplaceHooks(oldHooks)
		logger.SetOutput(oldOut)
	})
	hook := logtest.NewLocal(logger)

	req := httptest.NewRequest(http.MethodGet, "/query?session_id=session-1&sql=select+1", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var startID string
	var completeID string
	for _, entry := range hook.AllEntries() {
		switch entry.Message {
		case "HTTP query request":
			if value, ok := entry.Data["request_id"].(string); ok {
				startID = value
			}
		case "HTTP query completed":
			if value, ok := entry.Data["request_id"].(string); ok {
				completeID = value
			}
		}
	}

	require.NotEmpty(t, startID)
	assert.Equal(t, startID, completeID)
}

func TestHTTPQueryHandlerSuccessPostBody(t *testing.T) {
	payload := []byte{0x04, 0x05}
	reader := &trackingReadCloser{Reader: bytes.NewReader(payload)}
	executor := &stubQueryExecutor{reader: reader}
	handler := newHTTPQueryHandler(executor, 0)

	req := httptest.NewRequest(http.MethodPost, "/query?session_id=session-2", bytes.NewBufferString("select 2"))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "application/vnd.apache.arrow.stream", resp.Header.Get("Content-Type"))
	assert.Equal(t, payload, body)
	assert.True(t, reader.closed)
	assert.Equal(t, "session-2", executor.sessionID)
	assert.Equal(t, "select 2", executor.sql)
}

func TestQueryIPCStreamMissingSession(t *testing.T) {
	server := &ApapServer{sessions: render.NewSessionStorage()}

	_, err := server.QueryIPCStream(context.Background(), "missing", "select 1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")
}

func TestQueryIPCStreamQueryError(t *testing.T) {
	dbKey := "test-db"
	db, err := (&render.DuckDBFactory{}).Connect(dbKey)
	require.NoError(t, err)
	defer db.Close()

	session := &testSession{id: "session-1", db: db, dbKey: dbKey}
	server := &ApapServer{sessions: render.NewSessionStorage()}
	require.NoError(t, server.sessions.AddRenderSession(session))

	_, err = server.QueryIPCStream(context.Background(), session.ID(), "select_missing_column")
	require.Error(t, err)
}
