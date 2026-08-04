// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package privilege

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"

	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	targetagentmocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

func TestPrivilegeProofForOS(t *testing.T) {
	t.Run("Linux uses passwordless sudo", func(t *testing.T) {
		proof := privilegeProofForOS(conductor.Linux)
		assert.True(t, proof.GetNoPasswdSudo())
	})

	t.Run("Android uses su", func(t *testing.T) {
		proof := privilegeProofForOS(conductor.Android)
		assert.True(t, proof.GetAndroidSu())
	})
}

func TestPrivilegeSession_withElevatedPrivileges(t *testing.T) {
	t.Run("successfully attempts to elevate privileges", func(t *testing.T) {
		ctx := t.Context()

		mockClient := new(targetagentmocks.TargetAgentClient)

		// Mock ElevatePrivileges
		req := &targetagentproto.ElevatePrivilegesRequest{
			Proof: &targetagentproto.PrivilegeProof{
				Mech: &targetagentproto.PrivilegeProof_NoPasswdSudo{
					NoPasswdSudo: true,
				},
			},
		}
		resp := &targetagentproto.ElevatePrivilegesResponse{
			Token: &targetagentproto.PrivilegeProofToken{
				Value: "test-token",
			},
		}
		mockClient.On("ElevatePrivileges", ctx, req).
			Return(resp, nil)

		session := NewPrivilegeSession(mockClient, conductor.Linux)
		_, err := session.withElevatedPrivileges(ctx, "TestRequest")
		require.NoError(t, err)

		mockClient.AssertExpectations(t)
	})

	t.Run("successfully appennds privilege metadata to context", func(t *testing.T) {
		ctx := t.Context()

		mockClient := new(targetagentmocks.TargetAgentClient)

		// Mock ElevatePrivileges
		req := &targetagentproto.ElevatePrivilegesRequest{
			Proof: &targetagentproto.PrivilegeProof{
				Mech: &targetagentproto.PrivilegeProof_NoPasswdSudo{
					NoPasswdSudo: true,
				},
			},
		}
		resp := &targetagentproto.ElevatePrivilegesResponse{
			Token: &targetagentproto.PrivilegeProofToken{
				Value: "test-token",
			},
		}
		mockClient.On("ElevatePrivileges", mock.Anything, req).
			Return(resp, nil)

		session := NewPrivilegeSession(mockClient, conductor.Linux)
		elevatedCtx, err := session.withElevatedPrivileges(ctx, "TestRequest")
		require.NoError(t, err)

		md, ok := metadata.FromOutgoingContext(elevatedCtx)
		require.True(t, ok)

		assert.Equal(t, []string{"true"}, md.Get(metadataAsPrivileged))
		assert.Equal(t, []string{"test-token"}, md.Get(metadataPrivilegeProofToken))

		mockClient.AssertExpectations(t)
	})

	t.Run("fails to elevate privileges when target agent returns error", func(t *testing.T) {
		ctx := t.Context()

		mockClient := new(targetagentmocks.TargetAgentClient)

		// Mock ElevatePrivileges
		req := &targetagentproto.ElevatePrivilegesRequest{
			Proof: &targetagentproto.PrivilegeProof{
				Mech: &targetagentproto.PrivilegeProof_NoPasswdSudo{
					NoPasswdSudo: true,
				},
			},
		}
		mockClient.On("ElevatePrivileges", mock.Anything, req).
			Return(nil, assert.AnError)

		session := NewPrivilegeSession(mockClient, conductor.Linux)
		_, err := session.withElevatedPrivileges(ctx, "TestRequest")

		assert.Error(t, err)

		mockClient.AssertExpectations(t)
	})
}

func TestPrivilegeSession_Invoke(t *testing.T) {
	t.Run("successfully invokes with privilege metadata", func(t *testing.T) {
		ctx := t.Context()

		mockClient := new(targetagentmocks.TargetAgentClient)

		// Mock ElevatePrivileges
		elevateReq := &targetagentproto.ElevatePrivilegesRequest{
			Proof: &targetagentproto.PrivilegeProof{
				Mech: &targetagentproto.PrivilegeProof_NoPasswdSudo{
					NoPasswdSudo: true,
				},
			},
		}
		elevateResp := &targetagentproto.ElevatePrivilegesResponse{
			Token: &targetagentproto.PrivilegeProofToken{
				Value: "test-token",
			},
		}
		mockClient.On("ElevatePrivileges", mock.Anything, elevateReq).
			Return(elevateResp, nil)

		session := NewPrivilegeSession(mockClient, conductor.Linux)

		// privCtx should contain privilege metadata
		err := session.Invoke(ctx, "TestRequest", func(privCtx context.Context) error {
			md, ok := metadata.FromOutgoingContext(privCtx)
			require.True(t, ok)

			assert.Equal(t, []string{"true"}, md.Get(metadataAsPrivileged))
			assert.Equal(t, []string{"test-token"}, md.Get(metadataPrivilegeProofToken))

			return nil
		})
		require.NoError(t, err)

		mockClient.AssertExpectations(t)
	})

	t.Run("successfully retries to elevate privileges", func(t *testing.T) {
		ctx := t.Context()

		mockClient := new(targetagentmocks.TargetAgentClient)

		invalidTokenErr := message.New(message.AgentElevatePrivilegesInvalidToken)

		// Mock ElevatePrivileges
		// Return invalid tokens
		mockClient.On("ElevatePrivileges", mock.Anything, mock.Anything).
			Return(&targetagentproto.ElevatePrivilegesResponse{
				Token: &targetagentproto.PrivilegeProofToken{Value: "invalid-token"},
			}, nil).Times(maxInvokeRetryCount)

		session := NewPrivilegeSession(mockClient, conductor.Linux)
		_ = session.Invoke(ctx, "TestRequest", func(privCtx context.Context) error {
			return invalidTokenErr
		})

		mockClient.AssertNumberOfCalls(t, "ElevatePrivileges", maxInvokeRetryCount)
	})

	t.Run("fails to invoke when ElevatePrivileges fails", func(t *testing.T) {
		ctx := t.Context()

		mockClient := new(targetagentmocks.TargetAgentClient)

		// Mock ElevatePrivileges to return error
		mockClient.On("ElevatePrivileges", mock.Anything, mock.Anything).
			Return(nil, assert.AnError)

		session := NewPrivilegeSession(mockClient, conductor.Linux)
		err := session.Invoke(ctx, "TestRequest", func(privCtx context.Context) error {
			return nil
		})

		assert.Error(t, err)

		mockClient.AssertExpectations(t)
	})
}
