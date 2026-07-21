// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	log "github.com/sirupsen/logrus"
)

type HTTPQueryServer struct {
	addr       string
	chunkBytes int
	executor   httpQueryExecutor
	listen     func(network string, address string) (net.Listener, error)
	server     *http.Server
}

func NewHTTPQueryServer(
	host string,
	port int,
	chunkBytes int,
	executor httpQueryExecutor,
	listen func(network string, address string) (net.Listener, error),
) *HTTPQueryServer {
	return &HTTPQueryServer{
		addr:       fmt.Sprintf("%s:%d", host, port),
		chunkBytes: chunkBytes,
		executor:   executor,
		listen:     listen,
	}
}

func (s *HTTPQueryServer) Start(ctx context.Context, cancel context.CancelFunc) error {
	httpListener, err := s.listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("http server failed to listen: %w", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/query", newHTTPQueryHandler(s.executor, s.chunkBytes))

	s.server = &http.Server{
		Addr:              s.addr,
		Handler:           mux,
		ReadHeaderTimeout: 30 * time.Second, // prevent slowloris attacks
	}

	httpErrs := make(chan error, 1)
	go func() {
		if err := s.server.Serve(httpListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			httpErrs <- err
		}
	}()

	go func() {
		select {
		case err := <-httpErrs:
			log.WithError(err).Error("HTTP server failed to serve")
			cancel()
		case <-ctx.Done():
		}
	}()

	go func() {
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer shutdownCancel()
		_ = s.server.Shutdown(shutdownCtx)
	}()

	log.WithFields(log.Fields{
		"http_address": s.addr,
	}).Info("HTTP server is listening")

	return nil
}
