// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/query"
)

const (
	httpQueryParamSessionID = "session_id"
	httpQueryParamSQL       = "sql"

	corsAllowMethods  = "GET, POST, OPTIONS"
	corsAllowHeaders  = "Content-Type, Authorization"
	corsExposeHeaders = "Content-Type, Content-Length"

	defaultHTTPChunkBytes = 1024 * 1024
)

var httpQueryRequestID uint64

type httpQueryExecutor interface {
	QueryIPCStream(ctx context.Context, sessionID string, sql string) (io.ReadCloser, error)
}

type tableReadCloser struct {
	reader io.ReadCloser
	table  query.TableAccessorCloser
	done   func()
}

func (t *tableReadCloser) Read(p []byte) (int, error) {
	return t.reader.Read(p)
}

func (t *tableReadCloser) Close() error {
	err := errors.Join(t.reader.Close(), t.table.Close())
	if t.done != nil {
		t.done()
	}
	return err
}

func (s *ApapServer) QueryIPCStream(ctx context.Context, sessionID string, sql string) (io.ReadCloser, error) {
	sessionAccess, err := s.sessions.GetSessionByID(sessionID)
	if err != nil {
		return nil, err
	}

	opts, err := executeOptionsForFormat(query.TableFormatArrowIPC)
	if err != nil {
		sessionAccess.Done()
		return nil, err
	}

	table, err := query.Execute(ctx, sessionAccess.S.Database(), sql, opts)
	if err != nil {
		sessionAccess.Done()
		return nil, err
	}

	streamTable, ok := table.(query.ByteStreamTableAccessor)
	if !ok {
		_ = table.Close()
		sessionAccess.Done()
		return nil, fmt.Errorf("unexpected table format %T", table)
	}

	reader, err := streamTable.OpenReader()
	if err != nil {
		_ = table.Close()
		sessionAccess.Done()
		return nil, err
	}

	return &tableReadCloser{
		reader: reader,
		table:  table,
		done:   sessionAccess.Done,
	}, nil
}

func newHTTPQueryHandler(executor httpQueryExecutor, chunkBytes int) http.Handler {
	if chunkBytes <= 0 {
		chunkBytes = defaultHTTPChunkBytes
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		applyCORSHeaders(w)

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			writeHTTPQueryError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
			return
		}

		queryValues := r.URL.Query()
		sessionID := queryValues.Get(httpQueryParamSessionID)
		if sessionID == "" {
			writeHTTPQueryError(w, http.StatusBadRequest, errors.New("missing session_id"))
			return
		}

		var sql string
		if r.Method == http.MethodPost {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				writeHTTPQueryError(w, http.StatusBadRequest, fmt.Errorf("failed to read sql body: %w", err))
				return
			}
			sql = strings.TrimSpace(string(body))
		} else {
			sql = queryValues.Get(httpQueryParamSQL)
		}

		if sql == "" {
			writeHTTPQueryError(w, http.StatusBadRequest, errors.New("missing sql"))
			return
		}

		start := time.Now()
		requestID := fmt.Sprintf("httpq-%d", atomic.AddUint64(&httpQueryRequestID, 1))
		log.WithFields(log.Fields{
			"method":     r.Method,
			"request_id": requestID,
			"session_id": sessionID,
			"sql_len":    len(sql),
		}).Info("HTTP query request")

		reader, err := executor.QueryIPCStream(r.Context(), sessionID, sql)
		if err != nil {
			log.WithFields(log.Fields{
				"duration_ms": time.Since(start).Milliseconds(),
				"request_id":  requestID,
			}).WithError(err).Error("HTTP query failed")
			writeHTTPQueryError(w, http.StatusInternalServerError, err)
			return
		}
		defer func() {
			_ = reader.Close()
		}()

		w.Header().Set("Content-Type", string(query.TableFormatArrowIPC))
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")

		buf := make([]byte, chunkBytes)
		written, err := io.CopyBuffer(w, reader, buf)
		if err != nil {
			log.WithFields(log.Fields{
				"duration_ms": time.Since(start).Milliseconds(),
				"request_id":  requestID,
			}).WithError(err).Error("HTTP query response stream failed")
			return
		}
		log.WithFields(log.Fields{
			"bytes":       written,
			"duration_ms": time.Since(start).Milliseconds(),
			"request_id":  requestID,
		}).Info("HTTP query completed")
	})
}

func applyCORSHeaders(w http.ResponseWriter) {
	// These are required to allow the electron renderer process to access the HTTP API
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", corsAllowMethods)
	w.Header().Set("Access-Control-Allow-Headers", corsAllowHeaders)
	w.Header().Set("Access-Control-Expose-Headers", corsExposeHeaders)
	w.Header().Set("Access-Control-Max-Age", "600")
}

type httpQueryErrorResponse struct {
	Error *message.ErrorPayload `json:"error,omitempty"`
}

func writeHTTPQueryError(w http.ResponseWriter, status int, err error) {
	resp := httpQueryErrorResponse{
		Error: message.BuildErrorPayload(err, nil),
	}

	data, marshalErr := json.Marshal(resp)
	if marshalErr != nil {
		http.Error(w, "failed to marshal error response", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}
