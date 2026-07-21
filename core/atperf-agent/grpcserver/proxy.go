// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"errors"

	log "github.com/sirupsen/logrus"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

// unaryProxyOptions describes the configuration for proxying a unary RPC to the root worker.
// T is the response type of the RPC.
type unaryProxyOptions[T any] struct {
	// RPCName is the name of the RPC being proxied, used for logging.
	RPCName string

	// Token is used to acquire/release elevated privileges.
	Token string

	// Invoke is the function that performs the actual RPC call.
	Invoke func(targetagentproto.TargetAgentClient) (T, error)
}

// proxyUnary proxies a unary RPC to the root worker, handling token lifetime and client retrieval.
func proxyUnary[T any](s *AgentServerAPI, opts unaryProxyOptions[T]) (T, error) {
	var emptyResp T

	if opts.Invoke == nil {
		return emptyResp, errors.New("proxy invoke callback is required")
	}

	// Hold the privilege proof token for the duration of the RPC
	if err := s.TokenStorage.Acquire(opts.Token); err != nil {
		return emptyResp, err
	}
	defer func() {
		if err := s.TokenStorage.Release(opts.Token, true); err != nil {
			log.WithError(err).
				Warnf("Unable to release privilege proof token after %s", opts.RPCName)
		}
	}()

	client, ok := s.RootWorkerClient()
	if !ok {
		log.Errorf("No root worker available to execute %s with elevated privileges", opts.RPCName)
		return emptyResp, message.New(message.AgentElevatePrivilegesNoRootWorkerFound)
	}
	return opts.Invoke(client)
}

// serverStreamProxyOptions describes the configuration for proxying a server streaming RPC to the root worker.
// T is the server stream type (e.g., TargetAgent_StreamStdoutServer, TargetAgent_StreamStderrServer).
type serverStreamProxyOptions[T any] struct {
	// RPCName is the name of the RPC being proxied, used for logging.
	RPCName string

	// Token is used to acquire/release elevated privileges.
	Token string

	// Invoke is the function that performs the actual RPC call and handles streaming.
	Invoke func(targetagentproto.TargetAgentClient, T) error

	// Stream is the server stream to send responses to the client.
	Stream T
}

// proxyServerStream proxies a server streaming RPC to the root worker, handling token lifetime and client retrieval.
func proxyServerStream[T any](s *AgentServerAPI, opts serverStreamProxyOptions[T]) error {
	if opts.Invoke == nil {
		return errors.New("proxy invoke callback is required")
	}

	// Hold the privilege proof token for the duration of the RPC
	if err := s.TokenStorage.Acquire(opts.Token); err != nil {
		return err
	}
	defer func() {
		if err := s.TokenStorage.Release(opts.Token, true); err != nil {
			log.WithError(err).
				Warnf("Unable to release privilege proof token after %s", opts.RPCName)
		}
	}()

	client, ok := s.RootWorkerClient()
	if !ok {
		log.Errorf("No root worker available to execute %s with elevated privileges", opts.RPCName)
		return message.New(message.AgentElevatePrivilegesNoRootWorkerFound)
	}

	return opts.Invoke(client, opts.Stream)
}
