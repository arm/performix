// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package message

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

// findMessageByCode is a helper function that searches an error chain via
// errors.Unwrap and errors.Join to do a depth-first search for the first
// *MessageImpl with a given code.
func findMessageByCode(err error, code MessageCode) *MessageImpl {
	seen := map[error]struct{}{}
	var dfs func(error) *MessageImpl
	dfs = func(e error) *MessageImpl {
		if e == nil {
			return nil
		}
		if _, ok := seen[e]; ok {
			return nil
		}
		seen[e] = struct{}{}

		if m, ok := e.(*MessageImpl); ok && m.Code() == code {
			return m
		}
		if u, ok := e.(interface{ Unwrap() []error }); ok && u.Unwrap() != nil {
			for _, ch := range u.Unwrap() {
				if r := dfs(ch); r != nil {
					return r
				}
			}
		}
		if u, ok := e.(interface{ Unwrap() error }); ok && u.Unwrap() != nil {
			return dfs(u.Unwrap())
		}
		return nil
	}
	return dfs(err)
}

func TestCancellationError(t *testing.T) {
	t.Run("nil context and nil error returns nil", func(t *testing.T) {
		var ctx context.Context
		require.NoError(t, CancellationError(ctx, nil))
	})

	t.Run("canceled context returns cataloged cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := CancellationError(ctx, nil)
		msg := IsMessage(err)
		require.NotNil(t, msg)
		require.Equal(t, EngineCommonUserCancellationError, msg.Code())
		require.ErrorIs(t, err, context.Canceled)
	})

	t.Run("context canceled error returns cataloged cancellation", func(t *testing.T) {
		var ctx context.Context
		err := CancellationError(ctx, context.Canceled)
		msg := IsMessage(err)
		require.NotNil(t, msg)
		require.Equal(t, EngineCommonUserCancellationError, msg.Code())
		require.ErrorIs(t, err, context.Canceled)
	})

	t.Run("grpc canceled error returns cataloged cancellation", func(t *testing.T) {
		var ctx context.Context
		err := CancellationError(ctx, status.Error(codes.Canceled, "client canceled"))
		msg := IsMessage(err)
		require.NotNil(t, msg)
		require.Equal(t, EngineCommonUserCancellationError, msg.Code())
	})

	t.Run("non-cancellation error returns nil", func(t *testing.T) {
		require.NoError(t, CancellationError(context.Background(), errors.New("boom")))
	})
}

func TestMessage(t *testing.T) {
	t.Run("message implementations are constructed correctly", func(t *testing.T) {
		m := &MessageImpl{
			domain: "engine.recipe.run",
			reason: "TEST_ERROR",
			metadata: map[string]string{
				"ba da bah bah bah": "i'm loving it",
			},
			grpcInfo: GRPCInfo{},
		}

		assert.Equal(t, "engine.recipe.run.TEST_ERROR", m.Error())
		assert.Equal(t, "engine.recipe.run", m.Domain())
		assert.Equal(t, "TEST_ERROR", m.Reason())
		assert.Equal(t, MessageCode("engine.recipe.run.TEST_ERROR"), m.Code())
		assert.Equal(t, "i'm loving it", m.Metadata()["ba da bah bah bah"])
		assert.Empty(t, m.grpcInfo)
	})
	t.Run("metadata can be added successfully", func(t *testing.T) {

		originalMetadata := map[string]string{
			"key1": "value1",
			"key2": "value2",
		}

		newMetadata := map[string]string{
			"key2": "new_value2",
			"key3": "value3",
		}

		msg := &MessageImpl{
			domain:   "my.fake.domain",
			reason:   "REALLY_BAD_ERROR",
			metadata: originalMetadata,
			locale:   "en-US",
		}

		updated := msg.WithMetadata(newMetadata)

		expectedMetadata := map[string]string{
			"key1": "value1",
			"key2": "new_value2",
			"key3": "value3",
		}

		if !reflect.DeepEqual(updated.Metadata(), expectedMetadata) {
			t.Errorf("Expected metadata %+v, but got %+v", expectedMetadata, updated.Metadata())
		}

		if !reflect.DeepEqual(msg.Metadata(), originalMetadata) {
			t.Errorf("Original metadata was modified, expected %+v but got %+v", originalMetadata, msg.Metadata())
		}

		if updated == msg {
			t.Error("WithMetadata should return a new MessageImpl, not modify the original")
		}

	})
	t.Run("new message interface works successfully", func(t *testing.T) {
		msg := New("engine.recipe.run.SOME_REASON")
		assert.NotNil(t, msg)
		assert.Contains(t, msg.Error(), "engine.recipe.run.SOME_REASON")
		assert.Empty(t, msg.Metadata())

		// Should return nil on invalid message code
		invalidMsg := New("invalid")
		assert.Nil(t, invalidMsg)
	})
	t.Run("messages can be converted to and from gRPC status", func(t *testing.T) {
		msgTo := &MessageImpl{
			domain:   "engine.recipe.run",
			reason:   "WORKLOAD_INVALID",
			metadata: map[string]string{"leroy": "jenkins"},
			locale:   LocaleEnglish,
			grpcInfo: GRPCInfo{},
		}
		err := msgTo.ToGRPCStatus()
		assert.NotNil(t, err)

		st, _ := status.FromError(err)
		withErrInfo, _ := st.WithDetails(&errdetails.ErrorInfo{
			Domain:   msgTo.Domain(),
			Reason:   msgTo.Reason(),
			Metadata: msgTo.Metadata(),
		}, &errdetails.LocalizedMessage{
			Locale: LocaleEnglish,
		})

		msgFrom := FromGRPCStatus(withErrInfo.Err())
		catMsg, err := LookupMessage(msgFrom)
		assert.NoError(t, err)
		expectedCatMsg, err := LookupMessage(msgTo)
		assert.NoError(t, err)
		assert.Equal(t, catMsg, expectedCatMsg)
	})
	t.Run("FromGRPCStatus returns plain string errors as-is", func(t *testing.T) {
		rawErr := errors.New("this is a raw error!")
		grpcErr, _ := status.FromError(rawErr)

		msg := FromGRPCStatus(grpcErr.Err())
		assert.Equal(t, rawErr, msg)
	})
	t.Run("FromGRPCStatus preserves joined string errors by concatenating them", func(t *testing.T) {
		rawErr := errors.Join(errors.New("error A"), errors.New("error B"))
		grpcErr, _ := status.FromError(rawErr)

		msg := FromGRPCStatus(grpcErr.Err())
		expectedErr := errors.New("error A\nerror B")
		assert.Equal(t, expectedErr, msg)
	})
	t.Run("WithMetadata preserves cause", func(t *testing.T) {
		base := New(CommonUnknownError)
		cause := errors.New("root cause")
		withCause := base.WithCause(cause)

		updated := withCause.WithMetadata(map[string]string{"x": "y"})

		// Cause preserved
		assert.True(t, errors.Is(updated, cause))

		// Metadata merged (and original untouched)
		assert.Equal(t, "y", updated.Metadata()["x"])
		assert.Empty(t, base.Metadata())
	})
	t.Run("Empty cause is not attached on empty error string", func(t *testing.T) {
		base := New(CommonUnknownError)
		cause := errors.New("")
		withCause := base.WithCause(cause)

		// Cause not attached
		assert.False(t, errors.Is(withCause, cause))
		assert.Nil(t, withCause.Unwrap())
	})
	t.Run("gRPC round-trip preserves error chain via Any", func(t *testing.T) {
		// Build: top(MessageImpl) -> join(child1(MessageImpl), child2(plain-wrapped))
		child1 := New(CommonUnknownError).WithMetadata(map[string]string{"a": "1"})
		child2 := fmt.Errorf("blah: %w", errors.New("scary error"))
		top := New(EngineRunDoesNotExist).WithCause(errors.Join(child1, child2))

		// Send over gRPC
		err := top.ToGRPCStatus()
		assert.NotNil(t, err)

		// Receive and rebuild
		rebuilt := FromGRPCStatus(err)
		assert.NotNil(t, rebuilt)

		// Top should be a *MessageImpl with same code
		var topMsg *MessageImpl
		assert.True(t, errors.As(rebuilt, &topMsg))
		assert.Equal(t, top.Code(), topMsg.Code())

		// Child1 survives and retains metadata
		child1Msg := findMessageByCode(rebuilt, child1.Code())
		if assert.NotNil(t, child1Msg, "expected to find child1 in the chain") {
			assert.Equal(t, child1.Code(), child1Msg.Code())
			assert.Equal(t, "1", child1Msg.Metadata()["a"])
		}

		// Plain child survives (string present)
		assert.Contains(t, rebuilt.Error(), "scary error")
	})
	t.Run("toNode does not mis-tag non-Message nodes", func(t *testing.T) {
		inner := New(CommonUnknownError)
		outer := fmt.Errorf("wrapper: %w", inner)

		n := toNode(outer, make(map[error]struct{}))

		// Outer is not a MessageImpl, so it shouldn't have any MessageDetails.
		assert.Nil(t, n.Message)

		// Child should carry the MessageDetails.
		if assert.Len(t, n.Children, 1) {
			assert.NotNil(t, n.Children[0].Message)
			assert.Equal(t, string(inner.Code()), n.Children[0].Message.Code)
		}
	})
}

func TestMessageIs(t *testing.T) {
	t.Run("same code returns true (direct call)", func(t *testing.T) {
		m1 := New(EngineRunDoesNotExist)
		m2 := New(EngineRunDoesNotExist)
		is := m1.Is(m2)
		assert.True(t, is)
	})

	t.Run("different code returns false", func(t *testing.T) {
		m1 := New(EngineRunDoesNotExist)
		m2 := New(EngineRecipeDoesNotExist)
		is := m1.Is(m2)
		assert.False(t, is)
	})

	t.Run("target not a MessageImpl returns false", func(t *testing.T) {
		m1 := New(EngineRunDoesNotExist)
		plain := errors.New("plain error")
		is := m1.Is(plain)
		assert.False(t, is)
	})

	t.Run("errors.Is matches by code", func(t *testing.T) {
		m1 := New(EngineRunDoesNotExist)
		target := New(EngineRunDoesNotExist)
		is := errors.Is(m1, target)
		assert.True(t, is)
	})

	t.Run("errors.Is with wrap finds inner MessageImpl by code", func(t *testing.T) {
		inner := New(EngineRunDoesNotExist)
		wrapped := fmt.Errorf("context: %w", inner)
		target := New(EngineRunDoesNotExist)
		is := errors.Is(wrapped, target)
		assert.True(t, is)
	})

	t.Run("errors.Is with join matches any branch by code", func(t *testing.T) {
		matching := New(EngineRunDoesNotExist)
		other := errors.New("something else")
		joined := errors.Join(other, matching)
		target := New(EngineRunDoesNotExist)
		is := errors.Is(joined, target)
		assert.True(t, is)
	})

	t.Run("errors.Is returns false when codes differ through wrapping", func(t *testing.T) {
		inner := New(EngineRunDoesNotExist)
		wrapped := fmt.Errorf("context: %w", inner)
		target := New(EngineRecipeFailedToRead)
		is := errors.Is(wrapped, target)
		assert.False(t, is)
	})

	t.Run("errors.Is with nested MessageImpl (parent != target) still finds child", func(t *testing.T) {
		child := New(EngineRunDoesNotExist)
		// Create a parent MessageImpl that wraps the child
		parent := Wrap(EngineRunDoesNotExist, child)
		target := New(EngineRunDoesNotExist)
		is := errors.Is(parent, target)
		assert.True(t, is)
	})
}

func TestIsMessage(t *testing.T) {
	t.Run("returns nil for plain go errors", func(t *testing.T) {
		msg := errors.New("plain error")
		isMsg := IsMessage(msg)
		assert.Nil(t, isMsg)
	})

	t.Run("returns MessageImpl for MessageImpl ", func(t *testing.T) {
		msg := New(EngineRunDoesNotExist)
		isMsg := IsMessage(msg)
		assert.NotNil(t, isMsg)
	})
}

func TestIsInfoOrWarning(t *testing.T) {
	t.Run("returns true for info messages", func(t *testing.T) {
		msg := New(CliCmdTargetPrepareAlreadyPrepared)
		isInfoOrWarning := msg.IsInfoOrWarning()
		assert.True(t, isInfoOrWarning)
	})

	// Disabled for now until we have some Warning message examples in the catalog
	//t.Run("returns true for warning messages", func(t *testing.T) {
	//	msg := New(FillMeInWithAWarningMessageCode)
	//	isInfoOrWarning := msg.IsInfoOrWarning()
	//	assert.True(t, isInfoOrWarning)
	//})

	t.Run("returns false for error messages", func(t *testing.T) {
		msg := New(EngineRunDoesNotExist)
		isInfoOrWarning := msg.IsInfoOrWarning()
		assert.False(t, isInfoOrWarning)
	})
}

func TestLookupMessage(t *testing.T) {
	t.Run("messages lookups in catalog en-US are successful for valid message codes", func(t *testing.T) {
		msg := New(CommonUnknownError)
		catalogMsg, err := LookupMessage(msg)
		assert.NoError(t, err)
		expectedMsg := &CatalogMessage{
			Code:        "common.UNKNOWN_ERROR",
			Severity:    SeverityError,
			Message:     fmt.Sprintf("An unknown error occurred in %v.", terminology.GetProductFullName()),
			Explanation: "This error might be caused by a bug or an unhandled condition.",
			Advice:      "Report this issue to Arm support with the details of the operation you were performing.",
		}
		assert.Equal(t, catalogMsg, expectedMsg)
	})
	t.Run("message lookups in catalog en-US fail gracefully for invalid message codes", func(t *testing.T) {
		msg := &MessageImpl{domain: "invalid", reason: "code"}
		catalogMsg, err := LookupMessage(msg)
		assert.NoError(t, err)
		expectedMsg := unknownCatalogMessage.Interpolate(map[string]string{"code": "invalid.code"})
		assert.Equal(t, expectedMsg, catalogMsg)
	})
	t.Run("lookup finds MessageImpl in chain when wrapped with %%w", func(t *testing.T) {
		inner := New(CommonUnknownError).WithMetadata(map[string]string{"k": "v"})
		wrapped := fmt.Errorf("wrapping: %w", inner)

		catalogMsg, err := LookupMessage(wrapped)
		assert.NoError(t, err)

		expected, err := LookupMessage(inner)
		assert.NoError(t, err)
		assert.Equal(t, expected, catalogMsg)
	})
	t.Run("lookup finds MessageImpl inside errors.Join", func(t *testing.T) {
		a := errors.New("some plain error that isn't a MessageImpl lol")
		b := New(CommonUnknownError)
		joined := errors.Join(a, b)

		catalogMsg, err := LookupMessage(joined)
		assert.NoError(t, err)

		expected, err := LookupMessage(b)
		assert.NoError(t, err)
		assert.Equal(t, expected, catalogMsg)
	})
	t.Run("lookup always performs string interoplation", func(t *testing.T) {
		msg := New(EngineSshKeyFileNotFound).WithMetadata(map[string]string{"path": "path/to/keyfile"})
		catalogMsg, err := LookupMessage(msg)
		assert.NoError(t, err)
		expectedMsg := &CatalogMessage{
			Code:        "engine.ssh.KEY_FILE_NOT_FOUND",
			Severity:    SeverityError,
			Message:     fmt.Sprintf("%v cannot find the SSH key file.", terminology.GetProductFullName()),
			Explanation: "The SSH key file at `path/to/keyfile` cannot be found on the system.",
			Advice:      fmt.Sprintf("Check that the SSH key file exists. If %v still cannot find the file, contact Arm support.", terminology.GetProductFullName()),
		}
		assert.Equal(t, catalogMsg, expectedMsg)
	})
}

func TestBuildErrorChain(t *testing.T) {
	t.Run("builds complete error chain", func(t *testing.T) {
		// Structure
		//     msg2
		//       |
		//     msg1
		//       |
		//     err3
		//      /\
		//  err1  err2

		// Leaf nodes
		err1 := errors.New("this is an error 1")
		err2 := errors.New("this is an error 2")

		// Join of leaves
		err3 := errors.Join(err1, err2)

		// Parent of join
		msg1 := New(EngineTargetConfigDoesNotExist).WithCause(err3).WithMetadata(map[string]string{"meta1": "thing", "meta2": "thing again"})
		// Parent of msg1
		msg2 := New(EngineSshKeyFileNotFound).WithMetadata(map[string]string{"meta3": "another thing"}).WithCause(msg1)

		resp := BuildErrorChain(msg2)
		// Top-level error (msg2)
		assert.Equal(t, EngineSshKeyFileNotFound, resp.Root.Message.Code)
		assert.Equal(t, "*message.MessageImpl", resp.Root.Type)
		assert.Equal(t, "another thing", resp.Root.Message.Metadata["meta3"])
		assert.Equal(t, 1, len(resp.Root.Children))

		// msg2 child -> msg1
		child1 := resp.Root.Children[0]
		assert.Equal(t, EngineTargetConfigDoesNotExist, child1.Message.Code)
		assert.Equal(t, "*message.MessageImpl", resp.Root.Type)
		assert.Equal(t, "thing", child1.Message.Metadata["meta1"])
		assert.Equal(t, "thing again", child1.Message.Metadata["meta2"])
		assert.Equal(t, 1, len(child1.Children))

		// msg1 child -> join node (err3)
		joinChild := child1.Children[0]
		assert.Equal(t, "this is an error 1\nthis is an error 2", joinChild.Error)
		assert.Equal(t, "*errors.joinError", joinChild.Type)
		assert.Equal(t, 2, len(joinChild.Children))

		// join node children -> err1 and err2
		child2 := joinChild.Children[0]
		child3 := joinChild.Children[1]
		assert.Equal(t, "this is an error 1", child2.Error)
		assert.Nil(t, child2.Message)
		assert.Nil(t, child2.Children)
		assert.Equal(t, "*errors.errorString", child2.Type)

		assert.Equal(t, "this is an error 2", child3.Error)
		assert.Nil(t, child3.Message)
		assert.Nil(t, child3.Children)
		assert.Equal(t, "*errors.errorString", child3.Type)
	})
	t.Run("handles nil error", func(t *testing.T) {
		resp := BuildErrorChain(nil)
		assert.Nil(t, resp)
	})
}

func TestReconstructFromChain(t *testing.T) {
	t.Run("reconstructs complete error chain", func(t *testing.T) {
		// Structure
		//     msgNode2
		//         |
		//     msgNode1
		//         |
		//       node3
		//        /\
		//   node1  node2

		// Leaf nodes
		node1 := &apapproto.ErrorNode{
			Error:    "this is an error 1",
			Type:     "*errors.errorString",
			Message:  nil,
			Children: nil,
		}
		node2 := &apapproto.ErrorNode{
			Error:    "this is an error 2",
			Type:     "*errors.errorString",
			Message:  nil,
			Children: nil,
		}

		// Join of leaves
		node3 := &apapproto.ErrorNode{
			Error:    "this is an error 1\nthis is an error 2",
			Type:     "*errors.joinError",
			Message:  nil,
			Children: []*apapproto.ErrorNode{node1, node2},
		}

		// Parent of join
		msgDet1 := &apapproto.MessageDetails{
			Code:     EngineTargetConfigDoesNotExist,
			Metadata: map[string]string{"meta1": "thing", "meta2": "thing again"},
			Locale:   "",
		}
		msgNode1 := &apapproto.ErrorNode{
			Error:    "",
			Type:     "*message.MessageImpl",
			Message:  msgDet1,
			Children: []*apapproto.ErrorNode{node3},
		}

		// Parent of msgNode1
		msgDet2 := &apapproto.MessageDetails{
			Code:     EngineSshKeyFileNotFound,
			Metadata: map[string]string{"meta3": "another thing"},
			Locale:   "",
		}
		msgNode2 := &apapproto.ErrorNode{
			Error:    "",
			Type:     "*message.MessageImpl",
			Message:  msgDet2,
			Children: []*apapproto.ErrorNode{msgNode1},
		}
		resp := ReconstructFromChain(&apapproto.ErrorChain{Root: msgNode2})

		// Rebuilding expected error
		err1 := errors.New("this is an error 1")
		err2 := errors.New("this is an error 2")
		err3 := errors.Join(err1, err2)

		err4 := fmt.Errorf("%s: %w", err3, err3)

		msg1 := New(EngineTargetConfigDoesNotExist).WithCause(err4).WithMetadata(map[string]string{"meta1": "thing", "meta2": "thing again"})
		msg2 := New(EngineSshKeyFileNotFound).WithMetadata(map[string]string{"meta3": "another thing"}).WithCause(msg1)
		assert.Equal(t, msg2, resp)
	})
	t.Run("handles nil error", func(t *testing.T) {
		resp := ReconstructFromChain(nil)
		assert.Nil(t, resp)
	})
}
