// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sort"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/apap-cli/service/mcpserver/toolimpl"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
	apapprotomocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
)

const mcpProtocolVersion20260728 = "2026-07-28"

func connectTestServer(t *testing.T, ctx context.Context, engine apapproto.ApapClient) (*mcp.ClientSession, *mcp.ServerSession) {
	t.Helper()

	server := newServer(io.Discard, toolimpl.ToolDependencies{Engine: engine}, DefaultToolRegistry())
	clientTransport, serverTransport := mcp.NewInMemoryTransports()

	serverSession, err := server.Connect(ctx, serverTransport, nil)
	require.NoError(t, err)

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	require.NoError(t, err)

	return clientSession, serverSession
}

// Codex code mode renders anyOf and oneOf branches independently of fields
// defined alongside the union. For example, it loses common when processing:
//
//	{
//	  "required": ["common"],
//	  "properties": {"common": {}, "a": {}, "b": {}},
//	  "oneOf": [
//	    {"required": ["a"]},
//	    {"required": ["b"]}
//	  ]
//	}
//
// Make each variant self-contained by repeating the sibling properties and
// required fields, while leaving the root constraints in place:
//
//	"oneOf": [
//	  {
//	    "required": ["common", "a"],
//	    "properties": {"common": {}, "a": {}, "b": {}}
//	  },
//	  {
//	    "required": ["common", "b"],
//	    "properties": {"common": {}, "a": {}, "b": {}}
//	  }
//	]
//
// This is equivalent under JSON Schema because the root and selected variant
// are both applied; the repeated constraints do not admit or reject any new
// inputs. See https://github.com/openai/codex/issues/42283.
func checkSelfContainedUnionVariants(t *testing.T, schema any, path string) {
	t.Helper()

	switch schema := schema.(type) {
	case map[string]any:
		outerProperties, _ := schema["properties"].(map[string]any)
		outerRequired, _ := schema["required"].([]any)
		for _, keyword := range []string{"anyOf", "oneOf"} {
			variants, ok := schema[keyword].([]any)
			if !ok {
				continue
			}

			for index, rawVariant := range variants {
				variant, ok := rawVariant.(map[string]any)
				if !ok {
					t.Errorf("%s.%s[%d] is not an object schema", path, keyword, index)
					continue
				}

				variantProperties, _ := variant["properties"].(map[string]any)
				missingProperties := make([]string, 0)
				for name := range outerProperties {
					if _, found := variantProperties[name]; !found {
						missingProperties = append(missingProperties, name)
					}
				}
				sort.Strings(missingProperties)

				variantRequired, _ := variant["required"].([]any)
				variantRequiredSet := make(map[string]struct{}, len(variantRequired))
				for _, value := range variantRequired {
					if name, ok := value.(string); ok {
						variantRequiredSet[name] = struct{}{}
					}
				}
				missingRequired := make([]string, 0)
				for _, value := range outerRequired {
					name, ok := value.(string)
					if !ok {
						continue
					}
					if _, found := variantRequiredSet[name]; !found {
						missingRequired = append(missingRequired, name)
					}
				}
				sort.Strings(missingRequired)

				if len(missingProperties) > 0 || len(missingRequired) > 0 {
					t.Errorf(
						"%s.%s[%d] is not self-contained: missing properties %v, missing required fields %v",
						path,
						keyword,
						index,
						missingProperties,
						missingRequired,
					)
				}
			}
		}

		for name, child := range schema {
			checkSelfContainedUnionVariants(t, child, path+"."+name)
		}
	case []any:
		for index, child := range schema {
			checkSelfContainedUnionVariants(t, child, fmt.Sprintf("%s[%d]", path, index))
		}
	}
}

func TestMCPServerRun(t *testing.T) {
	t.Run("serves until context is cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		reader, writer := io.Pipe()
		defer writer.Close()

		errCh := make(chan error, 1)
		server := NewMCPServer(
			apapprotomocks.NewApapClient(t),
			nil,
		)
		go func() {
			errCh <- server.Run(ctx, reader, &bytes.Buffer{}, &bytes.Buffer{})
		}()

		cancel()
		require.ErrorIs(t, <-errCh, context.Canceled)
	})
}

func TestMCPServerProtocol(t *testing.T) {
	t.Run("initializes and lists default tools", func(t *testing.T) {
		ctx := context.Background()
		clientSession, serverSession := connectTestServer(t, ctx, nil)
		defer clientSession.Close()
		defer serverSession.Close()

		tools, err := clientSession.ListTools(ctx, nil)

		require.NoError(t, err)
		require.NotEmpty(t, tools.Tools)

		names := make([]string, 0, len(tools.Tools))
		for _, tool := range tools.Tools {
			names = append(names, tool.Name)
		}

		assert.Contains(t, names, "list_recipes")
		assert.Contains(t, names, "recipe_info")
		assert.Contains(t, names, "list_runs")
		assert.Contains(t, names, "open_render_session")
		assert.Contains(t, names, "list_render_sessions")
		assert.Contains(t, names, "close_render_session")
		assert.Contains(t, names, "run_query")
	})

	t.Run("negotiates latest supported protocol with SDK client", func(t *testing.T) {
		ctx := context.Background()
		clientSession, serverSession := connectTestServer(t, ctx, nil)
		defer clientSession.Close()
		defer serverSession.Close()

		initResult := clientSession.InitializeResult()
		require.NotNil(t, initResult)
		assert.Equal(t, mcpProtocolVersion20260728, initResult.ProtocolVersion)
		require.NotNil(t, initResult.ServerInfo)
		assert.Equal(t, terminology.GetMCPServerName()+"-mcp-server", initResult.ServerInfo.Name)

		tools, err := clientSession.ListTools(ctx, nil)
		require.NoError(t, err)
		assert.NotEmpty(t, tools.Tools)
	})

	t.Run("advertises self-contained union variants", func(t *testing.T) {
		ctx := context.Background()
		clientSession, serverSession := connectTestServer(t, ctx, nil)
		defer clientSession.Close()
		defer serverSession.Close()

		tools, err := clientSession.ListTools(ctx, nil)
		require.NoError(t, err)
		require.NotEmpty(t, tools.Tools)

		for _, tool := range tools.Tools {
			tool := tool
			t.Run(tool.Name, func(t *testing.T) {
				require.NotNil(t, tool.InputSchema)

				schemaJSON, err := json.Marshal(tool.InputSchema)
				require.NoError(t, err)
				var inputSchema any
				require.NoError(t, json.Unmarshal(schemaJSON, &inputSchema))
				checkSelfContainedUnionVariants(t, inputSchema, "$")
			})
		}
	})

	t.Run("advertises server instructions on initialize", func(t *testing.T) {
		ctx := context.Background()
		clientSession, serverSession := connectTestServer(t, ctx, nil)
		defer clientSession.Close()
		defer serverSession.Close()

		initResult := clientSession.InitializeResult()
		require.NotNil(t, initResult)
		assert.NotEmpty(t, initResult.Instructions)
		assert.NotContains(t, initResult.Instructions, "SPDX-")
		assert.Equal(t, instructions, initResult.Instructions)
	})

	t.Run("exposes instructions as readable resources", func(t *testing.T) {
		ctx := context.Background()
		clientSession, serverSession := connectTestServer(t, ctx, nil)
		defer clientSession.Close()
		defer serverSession.Close()

		resources, err := clientSession.ListResources(ctx, nil)
		require.NoError(t, err)
		require.NotEmpty(t, resources.Resources)

		byURI := make(map[string]*mcp.Resource, len(resources.Resources))
		for _, resource := range resources.Resources {
			byURI[resource.URI] = resource
		}

		scheme := terminology.GetProductBinaryName()

		sections := parseInstructionSections(instructions)
		require.NotEmpty(t, sections, "embedded instructions should contain at least one section")
		require.Len(t, resources.Resources, len(sections))

		// Every parsed section should be advertised as its own resource.
		for _, section := range sections {
			uri := scheme + "://instructions/" + section.slug
			require.Contains(t, byURI, uri)
		}

		// Reading a section resource returns exactly that section's body.
		section := sections[0]
		read, err := clientSession.ReadResource(ctx, &mcp.ReadResourceParams{
			URI: scheme + "://instructions/" + section.slug,
		})
		require.NoError(t, err)
		require.Len(t, read.Contents, 1)
		assert.Equal(t, instructionsResourceMIMEType, read.Contents[0].MIMEType)
		assert.Equal(t, section.body, read.Contents[0].Text)
	})

	t.Run("unknown tool returns error", func(t *testing.T) {
		ctx := context.Background()
		clientSession, serverSession := connectTestServer(t, ctx, nil)
		defer clientSession.Close()
		defer serverSession.Close()

		_, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name: "missing_tool",
		})

		require.Error(t, err)
	})

	t.Run("surfaces catalog detail from gRPC client interceptors", func(t *testing.T) {
		ctx := context.Background()
		engine, cleanup := connectGRPCTestEngine(t, grpcCatalogErrorServer{})
		defer cleanup()

		clientSession, serverSession := connectTestServer(t, ctx, engine)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "list_recipes"})

		require.NoError(t, err)
		require.True(t, result.IsError)
		var content struct {
			Error *message.ErrorPayload `json:"error"`
		}
		require.Len(t, result.Content, 1)
		text, ok := result.Content[0].(*mcp.TextContent)
		require.True(t, ok)
		require.NoError(t, json.Unmarshal([]byte(text.Text), &content))
		require.NotNil(t, content.Error)
		assert.Equal(t, message.EngineRunDoesNotExist, content.Error.Code)
	})

	t.Run("server logs go to error stream", func(t *testing.T) {
		var errOut bytes.Buffer

		server := newServer(&errOut, toolimpl.ToolDependencies{}, DefaultToolRegistry())
		clientTransport, serverTransport := mcp.NewInMemoryTransports()
		serverSession, err := server.Connect(context.Background(), serverTransport, nil)
		require.NoError(t, err)
		defer serverSession.Close()

		client := mcp.NewClient(&mcp.Implementation{Name: "test-client"}, nil)
		clientSession, err := client.Connect(context.Background(), clientTransport, nil)
		require.NoError(t, err)
		defer clientSession.Close()

		_, err = clientSession.ListTools(context.Background(), nil)
		require.NoError(t, err)
		assert.Contains(t, errOut.String(), "server session connected")
	})

	t.Run("tools receive engine client", func(t *testing.T) {
		expectedClient := apapprotomocks.NewApapClient(t)
		tool := &capturingTool{}

		_ = newServer(io.Discard, toolimpl.ToolDependencies{Engine: expectedClient}, NewToolRegistry(tool))

		assert.Same(t, expectedClient, tool.deps.Engine)
	})
}

type capturingTool struct {
	deps toolimpl.ToolDependencies
}

func (t *capturingTool) Register(_ *mcp.Server, deps toolimpl.ToolDependencies) {
	t.deps = deps
}

// connectGRPCTestEngine connects the MCP server to a real in-memory gRPC engine client instead
// of a direct mock. This is useful for tests that need production-like gRPC error handling:
// the test server serialises catalog errors into gRPC status details, and the test client
// interceptors reconstruct them before the MCP tool builds its structured error payload.
func connectGRPCTestEngine(t *testing.T, engine apapproto.ApapServer) (apapproto.ApapClient, func()) {
	t.Helper()

	listener := bufconn.Listen(1 << 20)
	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(message.ErrorHandlingServerInterceptor()),
		grpc.ChainStreamInterceptor(message.ErrorHandlingServerStreamInterceptor()),
	)
	apapproto.RegisterApapServer(grpcServer, engine)
	go func() { _ = grpcServer.Serve(listener) }()

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithChainUnaryInterceptor(message.ErrorHandlingClientInterceptor()),
		grpc.WithChainStreamInterceptor(message.ErrorHandlingClientStreamInterceptor()),
	)
	require.NoError(t, err)

	cleanup := func() {
		_ = conn.Close()
		grpcServer.Stop()
		_ = listener.Close()
	}

	return apapproto.NewApapClient(conn), cleanup
}

type grpcCatalogErrorServer struct {
	apapproto.UnimplementedApapServer
}

func (grpcCatalogErrorServer) ListRecipes(context.Context, *emptypb.Empty) (*apapproto.RecipeNameListing, error) {
	return nil, message.New(message.EngineRunDoesNotExist).WithMetadata(map[string]string{"runID": "missing-run"})
}
