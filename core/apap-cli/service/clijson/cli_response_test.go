// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package clijson

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Arm-Debug/apap-cli/apap-cli/test"
	"github.com/Arm-Debug/apap-cli/apap-cli/utils"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
)

type MarshalFail struct {
	Channel chan int
}

func TestMarshalJSONCLIResponse(t *testing.T) {

	t.Run("marshal produces valid JSON", func(t *testing.T) {
		cmdBuf := &bytes.Buffer{}
		err := MarshalJSONCLIResponse[bool](cmdBuf, false)

		assert.NoError(t, err)
		assert.True(t, utils.IsValidJSON(cmdBuf.String()))
	})
}

func TestHandleCLIError(t *testing.T) {

	t.Run("handle CLI error produces valid JSON when requested", func(t *testing.T) {
		test.SetViperJSON(t, true)
		cmdBuf := &bytes.Buffer{}
		HandleCLIError(cmdBuf, errors.New("rekt"))

		assert.True(t, utils.IsValidJSON(cmdBuf.String()))
		assert.Contains(t, cmdBuf.String(), "rekt")
	})

	t.Run("handle CLI error produces invalid JSON when requested", func(t *testing.T) {
		cmdBuf := &bytes.Buffer{}

		HandleCLIError(cmdBuf, errors.New("rekt"))

		assert.False(t, utils.IsValidJSON(cmdBuf.String()))
		assert.Contains(t, cmdBuf.String(), "rekt")
	})

	t.Run("handle CLI error reflects gRPC code", func(t *testing.T) {
		test.SetViperJSON(t, true)
		cmdBuf := &bytes.Buffer{}

		HandleCLIError(cmdBuf, test.NewTestGRPCError())
		outString := cmdBuf.String()

		assert.True(t, utils.IsValidJSON(outString))
		assert.Contains(t, outString, "Bad things have happened")
	})

	t.Run("handle CLI error with JSON handles nil error", func(t *testing.T) {
		test.SetViperJSON(t, true)
		buf := &bytes.Buffer{}

		HandleCLIError(buf, nil)
		out := buf.String()

		assert.True(t, utils.IsValidJSON(out))
		assert.Contains(t, out, `"code":"0"`)
		assert.Contains(t, out, `"grpc_code":"OK"`)
	})

	t.Run("handle CLI error with text handles nil error", func(t *testing.T) {
		test.SetViperJSON(t, false)
		buf := &bytes.Buffer{}

		HandleCLIError(buf, nil)
		out := buf.String()

		assert.False(t, utils.IsValidJSON(out))
	})

	t.Run("handle CLI error with ErrorAlreadyHandled produces no output (JSON disabled)", func(t *testing.T) {
		test.SetViperJSON(t, false)
		buf := &bytes.Buffer{}

		HandleCLIError(buf, ErrorAlreadyHandled)

		assert.Equal(t, buf.Len(), 0)
	})

	t.Run("handle CLI error with ErrorAlreadyHandled produces no output (JSON enabled)", func(t *testing.T) {
		test.SetViperJSON(t, true)
		buf := &bytes.Buffer{}

		HandleCLIError(buf, ErrorAlreadyHandled)

		assert.Equal(t, buf.Len(), 0)
	})

	t.Run("handle CLI error displays catalog message as text when error is MessageImpl", func(t *testing.T) {
		cmdBuf := &bytes.Buffer{}
		expectedOutput := message.CatalogMessage{
			Code:        "common.UNKNOWN_ERROR",
			Severity:    message.SeverityError,
			Message:     fmt.Sprintf("An unknown error occurred in %v.", terminology.GetProductFullName()),
			Explanation: "This could be due to a bug or an unexpected condition that was not handled.",
			Advice:      "Report this issue to Arm support with the details of the operation you were performing.",
		}

		// Mock the message lookup to return a known catalog message
		orig := LookupMsg
		t.Cleanup(func() { LookupMsg = orig })
		LookupMsg = func(err error) (*message.CatalogMessage, error) {
			return &expectedOutput, nil
		}

		HandleCLIError(cmdBuf, errors.New("anything"))
		outString := cmdBuf.String()

		assert.Contains(t, outString, expectedOutput.Severity)
		assert.Contains(t, outString, expectedOutput.Message)
		assert.Contains(t, outString, expectedOutput.Explanation)
		assert.Contains(t, outString, expectedOutput.Advice)
	})

	t.Run("handle CLI error displays catalog message as JSON when error is MessageImpl", func(t *testing.T) {
		test.SetViperJSON(t, true)
		cmdBuf := &bytes.Buffer{}
		msg := message.New(message.CommonUnknownError)
		expectedOutput := message.CatalogMessage{
			Code:        "common.UNKNOWN_ERROR",
			Severity:    message.SeverityError,
			Message:     fmt.Sprintf("An unknown error occurred in %v.", terminology.GetProductFullName()),
			Explanation: "This could be due to a bug or an unexpected condition that was not handled.",
			Advice:      "Report this issue to Arm support with the details of the operation you were performing.",
		}

		// Mock the message lookup to return a known catalog message
		orig := LookupMsg
		t.Cleanup(func() { LookupMsg = orig })
		LookupMsg = func(err error) (*message.CatalogMessage, error) {
			return &expectedOutput, nil
		}

		HandleCLIError(cmdBuf, msg)
		outString := cmdBuf.String()

		assert.Contains(t, outString, `"code":"-1"`)
		assert.Contains(t, outString, expectedOutput.Code)
		assert.Contains(t, outString, expectedOutput.Severity)
		assert.Contains(t, outString, expectedOutput.Message)
		assert.Contains(t, outString, expectedOutput.Explanation)
		assert.Contains(t, outString, expectedOutput.Advice)
	})

	t.Run("text mode prints catalog message when lookup succeeds", func(t *testing.T) {
		// Ensure we are in text mode
		test.SetViperJSON(t, false)

		// Mock the message lookup to return a known catalog message
		orig := LookupMsg
		t.Cleanup(func() { LookupMsg = orig })
		LookupMsg = func(err error) (*message.CatalogMessage, error) {
			return &message.CatalogMessage{
				Code:        "engine.recipe.run.FAILED",
				Severity:    "Error",
				Message:     "Nicely formatted catalog message",
				Explanation: "Why it happened",
				Advice:      "Try turning it off and on again",
			}, nil
		}

		buf := &bytes.Buffer{}
		HandleCLIError(buf, errors.New("anything"))
		assert.False(t, utils.IsValidJSON(buf.String()))
		assert.Contains(t, buf.String(), "Nicely formatted catalog message")
	})

	t.Run("handle CLI error displays raw error as JSON when error is non-MessageImpl", func(t *testing.T) {
		test.SetViperJSON(t, true)
		cmdBuf := &bytes.Buffer{}
		expectedOutput := "Bad things have happened"

		HandleCLIError(cmdBuf, errors.New(expectedOutput))
		outString := cmdBuf.String()

		assert.True(t, utils.IsValidJSON(outString))
		assert.Contains(t, outString, expectedOutput)
	})

	t.Run("handle CLI error displays raw error as text when lookup fails", func(t *testing.T) {
		test.SetViperJSON(t, false)
		cmdBuf := &bytes.Buffer{}

		// Mock the message lookup to return a plain error
		orig := LookupMsg
		t.Cleanup(func() { LookupMsg = orig })
		LookupMsg = func(err error) (*message.CatalogMessage, error) {
			return nil, errors.New("nope")
		}

		HandleCLIError(cmdBuf, errors.New("rekt"))
		outString := cmdBuf.String()

		assert.False(t, utils.IsValidJSON(outString))
		assert.Equal(t, "rekt\n", outString)
	})

	t.Run("handle CLI error includes catalog fields as JSON when error is MessageImpl", func(t *testing.T) {
		test.SetViperJSON(t, true)

		orig := LookupMsg
		t.Cleanup(func() { LookupMsg = orig })
		LookupMsg = func(err error) (*message.CatalogMessage, error) {
			return &message.CatalogMessage{
				Code:        "engine.recipe.run.FAILED",
				Severity:    "Error",
				Message:     "Run failed",
				Explanation: "X happened",
				Advice:      "Do Y",
			}, nil
		}

		m := message.New(message.EngineRunDoesNotExist)
		buf := &bytes.Buffer{}
		HandleCLIError(buf, m)
		out := buf.String()

		assert.True(t, utils.IsValidJSON(out))
		assert.Contains(t, out, `"code":"-1"`)
		assert.Contains(t, out, `"message":"Run failed"`)
		assert.Contains(t, out, `"severity":"Error"`)
		assert.Contains(t, out, `"grpc_code":"OK"`)
	})

	t.Run("handle CLI error in JSON mode wraps marshal error with non-serializable data", func(t *testing.T) {
		test.SetViperJSON(t, true)

		buf := &bytes.Buffer{}
		err := MarshalJSONCLIResponseWithError[MarshalFail](buf, MarshalFail{}, errors.New("ignored"))

		var msgErr message.Message
		ok := errors.As(err, &msgErr)
		assert.True(t, ok)
		assert.Equal(t, message.CliCmdCommonJsonMarshalFailed, msgErr.Code())
		assert.Contains(t, msgErr.Unwrap().Error(), "unsupported type")
		assert.Contains(t, msgErr.Unwrap().Error(), "chan int")

		// Since marshalling failed, nothing should have been written
		assert.Equal(t, "", buf.String())
	})

	t.Run("json mode captures single wrapped cause", func(t *testing.T) {
		test.SetViperJSON(t, true)
		buf := &bytes.Buffer{}

		inner := errors.New("inner")
		outer := fmt.Errorf("outer: %w", inner)

		HandleCLIError(buf, outer)
		out := buf.String()

		assert.True(t, utils.IsValidJSON(out))
		assert.Contains(t, out, "\"outer: inner\"")
		assert.Contains(t, out, "\"inner\"")
	})

	t.Run("json mode captures multiple joined causes", func(t *testing.T) {
		test.SetViperJSON(t, true)
		buf := &bytes.Buffer{}

		j := errors.Join(errors.New("first"), errors.New("second"))
		HandleCLIError(buf, j)
		out := buf.String()

		assert.True(t, utils.IsValidJSON(out))
		// Children should include both joined causes
		assert.Contains(t, out, "first\",")
		assert.Contains(t, out, "second\",")
	})

	t.Run("json mode falls back to raw message when catalog lookup fails for MessageImpl", func(t *testing.T) {
		test.SetViperJSON(t, true)
		buf := &bytes.Buffer{}

		// Force lookup failure for any MessageImpl
		orig := LookupMsg
		t.Cleanup(func() { LookupMsg = orig })
		LookupMsg = func(err error) (*message.CatalogMessage, error) { return nil, errors.New("lookup failed") }

		m := message.New(message.EngineRunDoesNotExist)
		HandleCLIError(buf, m)
		out := buf.String()

		assert.True(t, utils.IsValidJSON(out))
		assert.Contains(t, out, `"severity":"Error"`)
		assert.Contains(t, out, string(m.Code()))
	})

	t.Run("json mode for MessageImpl defaults locale when empty", func(t *testing.T) {
		test.SetViperJSON(t, true)
		buf := &bytes.Buffer{}

		orig := LookupMsg
		t.Cleanup(func() { LookupMsg = orig })
		LookupMsg = func(err error) (*message.CatalogMessage, error) {
			return &message.CatalogMessage{
				Code:        "engine.recipe.run.FAILED",
				Severity:    "Error",
				Message:     "Run failed",
				Explanation: "X happened",
				Advice:      "Do Y",
			}, nil
		}

		m := message.New(message.EngineRunDoesNotExist)
		HandleCLIError(buf, m)
		out := buf.String()

		assert.True(t, utils.IsValidJSON(out))
		assert.Contains(t, out, `"message":"Run failed"`)
		assert.Contains(t, out, fmt.Sprintf(`"locale":"%s"`, message.LocaleEnglish))
	})

	t.Run("json includes metadata from MessageImpl", func(t *testing.T) {
		test.SetViperJSON(t, true)

		orig := LookupMsg
		t.Cleanup(func() { LookupMsg = orig })
		LookupMsg = func(err error) (*message.CatalogMessage, error) {
			return &message.CatalogMessage{
				Code:        "engine.recipe.run.FAILED",
				Severity:    "Error",
				Message:     "Run failed",
				Explanation: "X happened",
				Advice:      "Do Y",
			}, nil
		}

		// Attach metadata to the top-level MessageImpl
		m := message.New(message.EngineRunDoesNotExist).WithMetadata(map[string]string{
			"request_id": "abc123",
		})

		buf := &bytes.Buffer{}
		HandleCLIError(buf, m)
		out := buf.String()

		assert.True(t, utils.IsValidJSON(out))
		assert.Contains(t, out, `"request_id":"abc123"`)
	})
}

func TestExtractGRPCMessage(t *testing.T) {
	t.Run("non-gRPC error returns raw message", func(t *testing.T) {
		err := errors.New("plain error")
		got := ExtractGRPCMessage(err)
		assert.Equal(t, "plain error", got)
	})

	t.Run("gRPC error returns status message", func(t *testing.T) {
		err := test.NewTestGRPCError()
		got := ExtractGRPCMessage(err)
		assert.Contains(t, got, "Bad things have happened")
	})
}

// helper types to exercise unwrapOne and unwrapMany

type one struct{ cause error }

func (e one) Error() string { return "one" }
func (e one) Unwrap() error { return e.cause }

type many struct{ causes []error }

func (e many) Error() string   { return "many" }
func (e many) Unwrap() []error { return e.causes }

func TestDirectChildren(t *testing.T) {
	t.Run("nil error returns nil", func(t *testing.T) {
		assert.Nil(t, message.DirectChildren(nil))
	})

	t.Run("single unwrap returns one child", func(t *testing.T) {
		child := errors.New("child-one")
		dc := message.DirectChildren(one{cause: child})
		assert.Len(t, dc, 1)
		assert.EqualError(t, dc[0], "child-one")
	})

	t.Run("multi unwrap returns both children", func(t *testing.T) {
		a := errors.New("alpha")
		b := errors.New("beta")
		dc := message.DirectChildren(many{causes: []error{a, b}})
		assert.Len(t, dc, 2)
		// order is preserved
		assert.EqualError(t, dc[0], "alpha")
		assert.EqualError(t, dc[1], "beta")
	})
}

func TestBuildErrorTree(t *testing.T) {
	t.Run("build error tree returns nil payload when error is nil", func(t *testing.T) {
		assert.Nil(t, BuildErrorTree(nil))
	})

	t.Run("build error tree supports mix of MessageImpl and plain errors", func(t *testing.T) {
		// Make catalog lookups succeed for any MessageImpl to exercise the catalog path
		orig := LookupMsg
		t.Cleanup(func() { LookupMsg = orig })
		LookupMsg = func(err error) (*message.CatalogMessage, error) {
			return &message.CatalogMessage{
				Code:        "engine.recipe.run.FAILED",
				Severity:    "Error",
				Message:     "FromCatalog",
				Explanation: "X happened",
				Advice:      "Do Y",
			}, nil
		}

		// Build: parent(MessageImpl) -> child(joined) -> grandchildren{ MessageImpl, plain }
		childMsg := message.New(message.EngineRunDoesNotExist)
		childPlain := errors.New("plain-child")
		parent := message.New(message.CommonUnknownError).WithCause(errors.Join(childMsg, childPlain))

		tree := BuildErrorTree(parent)
		if tree == nil {
			t.Fatal("expected non-nil tree")
			return
		}

		// Root came from catalog
		assert.Equal(t, "FromCatalog", tree.Message)
		assert.Equal(t, "Error", tree.Severity)

		// Root has a single child: the joined error node
		if assert.Len(t, tree.Children, 1) {
			joinedNode := tree.Children[0]

			// The joined node (a plain error payload) should have two children
			if assert.Len(t, joinedNode.Children, 2) {
				var seenCatalog, seenPlain bool
				for _, ch := range joinedNode.Children {
					if ch.Message == "FromCatalog" {
						seenCatalog = true
						assert.Equal(t, "Error", ch.Severity)
					}
					if ch.Message == "plain-child" {
						seenPlain = true
						assert.Equal(t, "Error", ch.Severity)
					}
				}
				assert.True(t, seenCatalog, "expected a catalog-derived grandchild")
				assert.True(t, seenPlain, "expected a plain grandchild")
			}
		}
	})
}
