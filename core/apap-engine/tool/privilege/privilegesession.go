// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package privilege

import (
	"context"
	"errors"
	"sync"

	"google.golang.org/grpc/metadata"

	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/logging/logx"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

// gRPC metadata headers to pass privilege information
const (
	metadataAsPrivileged        = "apx-as-privileged"
	metadataPrivilegeProofToken = "apx-privilege-proof-token" //nolint:gosec // Metadata header key, not a credential
)

// maxInvokeRetryCount defines the maximum number of attempts to
// make when elevating privileges.
const maxInvokeRetryCount = 3

// PrivilegeSession is the interface for the engine to execute privileged
// gRPC calls. It handles privilege elevation and token management.
type PrivilegeSession interface {
	// Invoke injects the privilege gRPC metadata into the context and
	// executes the given gRPC call with elevated privileges.
	Invoke(ctx context.Context, request string, call func(context.Context) error) error
}

type PrivilegeSessionImpl struct {
	client   targetagentproto.TargetAgentClient
	targetOS conductor.OS

	token   string
	tokenMu sync.RWMutex
}

func NewPrivilegeSession(client targetagentproto.TargetAgentClient, targetOS conductor.OS) *PrivilegeSessionImpl {
	return &PrivilegeSessionImpl{
		client:   client,
		targetOS: targetOS,
	}
}

// withElevatedPrivileges injects the privilege metadata into the give gRPC
// contex. It triggers privilege elevation if no token is present.
func (p *PrivilegeSessionImpl) withElevatedPrivileges(ctx context.Context, request string) (context.Context, error) {
	currToken := p.getToken()

	if currToken == "" {
		newToken, err := p.elevatePrivileges(ctx, request)
		if err != nil {
			return nil, err
		}
		currToken = newToken
	}

	ctx = metadata.AppendToOutgoingContext(ctx,
		metadataAsPrivileged, "true",
		metadataPrivilegeProofToken, currToken,
	)
	return ctx, nil
}

// Invoke executes the given gRPC call with elevated privileges.
// The ctx will be injected with the privilege metadata.
// The new ctx is then passed to the call function to make gRPC calls.
// The agent will then read the metadata and execute the call with elevated privileges.
// If the gRPC call returns an invalid token error, we re-attempt to elevate privileges
// and re-execute the call, up to maxInvokeRetryCount times.
func (p *PrivilegeSessionImpl) Invoke(ctx context.Context, request string, call func(context.Context) error) error {
	var err error

	// Re-try mechanism if the token is invalid or expired
	for i := 0; i < maxInvokeRetryCount; i++ {
		elevatedCtx, err := p.withElevatedPrivileges(ctx, request)
		if err != nil {
			return err
		}

		err = call(elevatedCtx)

		// Call succeeded
		if err == nil {
			return nil
		}

		// Error: any
		if !isInvalidTokenError(err) {
			return err
		}

		// Error: invalid token, try again
		logx.FromContext(elevatedCtx).
			Warnf("privilege proof token is invalid, re-elevating privileges (attempt %d/%d)", i+1, maxInvokeRetryCount)
		p.setToken("")
	}

	return err
}

// elevatePrivileges requests the passwordless mechanism for the target OS.
func (p *PrivilegeSessionImpl) elevatePrivileges(ctx context.Context, request string) (string, error) {
	req := &targetagentproto.ElevatePrivilegesRequest{
		Proof: privilegeProofForOS(p.targetOS),
	}

	// We only try once to elevate privileges as the passwordless mechanism either works or not.
	// We might want to try more than once depending on the mechanism in the future.
	// For example; sudo password mechanism might fail due to user entering wrong password (try 3 times).
	resp, err := p.client.ElevatePrivileges(ctx, req)
	if err != nil {
		return "", message.New(message.EngineToolServiceElevatePrivilegesFailed).
			WithMetadata(map[string]string{"request": request}).
			WithCause(err)
	}

	token := resp.GetToken().GetValue()
	p.setToken(token)

	return token, nil
}

func privilegeProofForOS(targetOS conductor.OS) *targetagentproto.PrivilegeProof {
	if targetOS == conductor.Android {
		return &targetagentproto.PrivilegeProof{
			Mech: &targetagentproto.PrivilegeProof_AndroidSu{AndroidSu: true},
		}
	}
	return &targetagentproto.PrivilegeProof{
		Mech: &targetagentproto.PrivilegeProof_NoPasswdSudo{NoPasswdSudo: true},
	}
}

func (p *PrivilegeSessionImpl) getToken() string {
	p.tokenMu.RLock()
	defer p.tokenMu.RUnlock()
	return p.token
}

func (p *PrivilegeSessionImpl) setToken(token string) {
	p.tokenMu.Lock()
	defer p.tokenMu.Unlock()
	p.token = token
}

func isInvalidTokenError(err error) bool {
	invalidTokenErr := message.New(message.AgentElevatePrivilegesInvalidToken)
	return errors.Is(message.FromGRPCStatus(err), invalidTokenErr)
}
