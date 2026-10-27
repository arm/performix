// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package tool

import (
	"context"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/encoding/gzip"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/featureflags"
	"github.com/Arm-Debug/apap-cli/api/targetagentproto"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
)

type catalogueStreamServer struct {
	targetagentproto.UnimplementedTargetAgentServer
	catalog string
}

func (s *catalogueStreamServer) ExecCommand(context.Context, *targetagentproto.ExecCommandRequest) (*targetagentproto.CommandResult, error) {
	return &targetagentproto.CommandResult{Stdout: s.catalog}, nil
}
func (s *catalogueStreamServer) StartProcess(context.Context, *targetagentproto.StartProcessRequest) (*targetagentproto.StartProcessResponse, error) {
	return &targetagentproto.StartProcessResponse{Pid: 1}, nil
}
func (s *catalogueStreamServer) WaitProcess(context.Context, *targetagentproto.WaitProcessRequest) (*targetagentproto.WaitProcessResponse, error) {
	return &targetagentproto.WaitProcessResponse{}, nil
}
func (s *catalogueStreamServer) StreamStdout(_ *targetagentproto.ProcessStreamRequest, stream targetagentproto.TargetAgent_StreamStdoutServer) error {
	for start := 0; start < len(s.catalog); start += 4096 {
		end := min(start+4096, len(s.catalog))
		if err := stream.Send(&targetagentproto.StreamChunk{Data: []byte(s.catalog[start:end])}); err != nil {
			return err
		}
	}
	return nil
}
func (s *catalogueStreamServer) StreamStderr(_ *targetagentproto.ProcessStreamRequest, stream targetagentproto.TargetAgent_StreamStderrServer) error {
	return stream.Send(&targetagentproto.StreamChunk{Data: []byte("catalog diagnostic")})
}
func TestAgentEngineStreamsCatalogueBeyondReceiveLimit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	catalog := "<events><!--" + strings.Repeat("x", 5*1024*1024) + "--></events>"
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	targetagentproto.RegisterTargetAgentServer(server, &catalogueStreamServer{catalog: catalog})
	go func() { _ = server.Serve(listener) }()
	defer server.Stop()
	defer listener.Close()
	conn, err := grpc.NewClient("passthrough:///catalog-test",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }),
		grpc.WithDefaultCallOptions(grpc.UseCompressor(gzip.Name)),
	)
	require.NoError(t, err)
	defer conn.Close()
	engine, _ := NewAgentEngine(ctx, ctx, targetagentproto.NewTargetAgentClient(conn), nil, nil, false, featureflags.Flags{}, conductor.PlatformConfiguration{OS: conductor.Android})
	command := process.LaunchCommand{Command: []string{"sl-record", "--print", "events.xml"}}
	_, err = engine.ExecCommand(&command)
	require.Equal(t, codes.ResourceExhausted, status.Code(err))
	require.ErrorContains(t, err, "after decompression")
	handle, err := engine.StartProcess(&process.StartProcess{
		Ctx: ctx, LaunchCommand: command,
		Stdout: process.StreamRedirect{Mode: process.Stream},
		Stderr: process.StreamRedirect{Mode: process.Stream},
	})
	require.NoError(t, err)
	type readResult struct {
		text string
		err  error
	}
	stderrDone := make(chan readResult, 1)
	go func() { b, err := io.ReadAll(handle.Stderr()); stderrDone <- readResult{string(b), err} }()
	stdout, err := io.ReadAll(handle.Stdout())
	require.NoError(t, err)
	require.Equal(t, catalog, string(stdout))
	stderr := <-stderrDone
	require.NoError(t, stderr.err)
	require.Equal(t, "catalog diagnostic", stderr.text)
	rc, err := handle.Wait()
	require.NoError(t, err)
	require.Zero(t, rc)
}
