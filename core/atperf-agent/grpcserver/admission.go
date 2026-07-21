// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"context"
	"strings"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type AdmissionManager interface {
	Restrict()
	Wait()
}

// AdmissionInterceptor controls which RPCs are admitted once restriction is enabled.
// It tracks non-exempt in-flight RPCs so callers can wait for them to finish after
// restriction begins.
type AdmissionInterceptor struct {
	isExemptDuringRestriction func(string) bool
	wg                        sync.WaitGroup
	restrict                  bool
	gateMu                    sync.RWMutex
}

func NewAdmissionInterceptor(isExempt func(string) bool) *AdmissionInterceptor {
	return &AdmissionInterceptor{isExemptDuringRestriction: isExempt}
}

// Restrict enables restriction mode. Any non-exempt RPCs will be rejected while in
// restriction mode.
func (a *AdmissionInterceptor) Restrict() {
	a.gateMu.Lock()
	a.restrict = true
	a.gateMu.Unlock()
}

// Wait waits for non-exempt in-flight RPCs to finish
func (a *AdmissionInterceptor) Wait() { a.wg.Wait() }

// Unary rejects non-exempt unary RPCs while restricted. When not restricted, it adds
// non-exempt RPCs to a wait group so callers can wait for them to finish after
// restriction begins.
func (a *AdmissionInterceptor) Unary() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if a.isExemptDuringRestriction(info.FullMethod) {
			return handler(ctx, req)
		}
		a.gateMu.RLock()
		if a.restrict {
			a.gateMu.RUnlock()
			return nil, status.Error(codes.Unavailable, "server is restricting new RPCs")
		}
		a.wg.Add(1)
		a.gateMu.RUnlock()
		defer a.wg.Done()
		return handler(ctx, req)
	}
}

// Stream rejects non-exempt stream RPCs while restricted. When not restricted, it adds
// non-exempt RPCs to a wait group so callers can wait for them to finish after
// restriction begins.
func (a *AdmissionInterceptor) Stream() grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if a.isExemptDuringRestriction(info.FullMethod) {
			return handler(srv, ss)
		}
		a.gateMu.RLock()
		if a.restrict {
			a.gateMu.RUnlock()
			return status.Error(codes.Unavailable, "server is restricting new RPCs")
		}
		a.wg.Add(1)
		a.gateMu.RUnlock()
		defer a.wg.Done()
		return handler(srv, ss)
	}
}

// ExemptOnlyShutdown exempts only Shutdown methods while restricted.
func ExemptOnlyShutdown(fullMethod string) bool {
	return strings.HasSuffix(fullMethod, "/Shutdown")
}
