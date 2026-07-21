// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package rpcclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/afero"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

// getRPCHandlers returns a map of RPC method names to handler functions
func getRPCHandlers(fs afero.Fs) map[string]func(ctx context.Context, client targetagentproto.TargetAgentClient, args json.RawMessage) (proto.Message, error) {
	return map[string]func(context.Context, targetagentproto.TargetAgentClient, json.RawMessage) (proto.Message, error){
		"GetVersion": func(ctx context.Context, client targetagentproto.TargetAgentClient, args json.RawMessage) (proto.Message, error) {
			if len(args) >= 0 && string(args) != "{}" {
				return nil, fmt.Errorf("GetVersion does not accept arguments")
			}
			return client.GetVersion(ctx, &emptypb.Empty{})
		},
		"Shutdown": func(ctx context.Context, client targetagentproto.TargetAgentClient, args json.RawMessage) (proto.Message, error) {
			req := &targetagentproto.ShutdownRequest{}
			dec := json.NewDecoder(bytes.NewReader(args))
			dec.DisallowUnknownFields()
			if err := dec.Decode(req); err != nil {
				return nil, fmt.Errorf("failed to decode Shutdown args: %w", err)
			}
			return client.Shutdown(ctx, req)
		},
		"KillProcess": func(ctx context.Context, client targetagentproto.TargetAgentClient, args json.RawMessage) (proto.Message, error) {
			req := &targetagentproto.KillProcessRequest{}
			dec := json.NewDecoder(bytes.NewReader(args))
			dec.DisallowUnknownFields()
			if err := dec.Decode(req); err != nil {
				return nil, fmt.Errorf("failed to decode KillProcess args: %w", err)
			}
			return client.KillProcess(ctx, req)
		},
		"InterruptProcess": func(ctx context.Context, client targetagentproto.TargetAgentClient, args json.RawMessage) (proto.Message, error) {
			req := &targetagentproto.InterruptProcessRequest{}
			dec := json.NewDecoder(bytes.NewReader(args))
			dec.DisallowUnknownFields()
			if err := dec.Decode(req); err != nil {
				return nil, fmt.Errorf("failed to decode InterruptProcess args: %w", err)
			}
			return client.InterruptProcess(ctx, req)
		},
		"WaitProcess": func(ctx context.Context, client targetagentproto.TargetAgentClient, args json.RawMessage) (proto.Message, error) {
			req := &targetagentproto.WaitProcessRequest{}
			dec := json.NewDecoder(bytes.NewReader(args))
			dec.DisallowUnknownFields()
			if err := dec.Decode(req); err != nil {
				return nil, fmt.Errorf("failed to decode WaitProcess args: %w", err)
			}
			return client.WaitProcess(ctx, req)
		},
		"ExecCommand": func(ctx context.Context, client targetagentproto.TargetAgentClient, args json.RawMessage) (proto.Message, error) {
			req := &targetagentproto.ExecCommandRequest{}
			dec := json.NewDecoder(bytes.NewReader(args))
			dec.DisallowUnknownFields()
			if err := dec.Decode(req); err != nil {
				return nil, fmt.Errorf("failed to decode InterruptProcess args: %w", err)
			}
			return client.ExecCommand(ctx, req)
		},
		"ListProcesses": func(ctx context.Context, client targetagentproto.TargetAgentClient, args json.RawMessage) (proto.Message, error) {
			if len(args) >= 0 && string(args) != "{}" {
				return nil, fmt.Errorf("ListProcesses does not accept arguments")
			}
			return client.ListProcesses(ctx, &emptypb.Empty{})
		},
		"StartProcess": func(ctx context.Context, client targetagentproto.TargetAgentClient, args json.RawMessage) (proto.Message, error) {
			req := &targetagentproto.StartProcessRequest{}
			dec := json.NewDecoder(bytes.NewReader(args))
			dec.DisallowUnknownFields()
			if err := dec.Decode(req); err != nil {
				return nil, fmt.Errorf("failed to decode InterruptProcess args: %w", err)
			}
			return client.StartProcess(ctx, req)
		},
		"ReleaseProcessHandles": func(ctx context.Context, client targetagentproto.TargetAgentClient, args json.RawMessage) (proto.Message, error) {
			req := &targetagentproto.ReleaseProcessHandlesRequest{}
			dec := json.NewDecoder(bytes.NewReader(args))
			dec.DisallowUnknownFields()
			if err := dec.Decode(req); err != nil {
				return nil, fmt.Errorf("failed to decode ReleaseProcessHandles args: %w", err)
			}
			return client.ReleaseProcessHandles(ctx, req)
		},
		"StreamStdout": func(ctx context.Context, client targetagentproto.TargetAgentClient, args json.RawMessage) (proto.Message, error) {
			req := &targetagentproto.ProcessStreamRequest{}
			dec := json.NewDecoder(bytes.NewReader(args))
			dec.DisallowUnknownFields()
			if err := dec.Decode(req); err != nil {
				return nil, fmt.Errorf("failed to decode StreamStdout args: %w", err)
			}

			stream, err := client.StreamStdout(ctx, req)
			if err != nil {
				return nil, fmt.Errorf("failed to start StreamStdout stream: %w", err)
			}

			var b []byte
			for {
				chunk, err := stream.Recv()
				if err == io.EOF {
					break
				}
				if err != nil {
					return nil, fmt.Errorf("failed receiving from stream: %w", err)
				}
				b = append(b, chunk.Data...)
			}

			return &wrapperspb.StringValue{Value: string(b)}, nil
		},
		"StreamStderr": func(ctx context.Context, client targetagentproto.TargetAgentClient, args json.RawMessage) (proto.Message, error) {
			req := &targetagentproto.ProcessStreamRequest{}
			dec := json.NewDecoder(bytes.NewReader(args))
			dec.DisallowUnknownFields()
			if err := dec.Decode(req); err != nil {
				return nil, fmt.Errorf("failed to decode StreamStderr args: %w", err)
			}

			stream, err := client.StreamStderr(ctx, req)
			if err != nil {
				return nil, fmt.Errorf("failed to start StreamStderr stream: %w", err)
			}

			var b []byte
			for {
				chunk, err := stream.Recv()
				if err == io.EOF {
					break
				}
				if err != nil {
					return nil, fmt.Errorf("failed receiving from stream: %w", err)
				}
				b = append(b, chunk.Data...)
			}

			return &wrapperspb.StringValue{Value: string(b)}, nil
		},
		"WriteToStdin": func(ctx context.Context, client targetagentproto.TargetAgentClient, args json.RawMessage) (proto.Message, error) {
			req := &targetagentproto.StdinChunk{}
			dec := json.NewDecoder(bytes.NewReader(args))
			dec.DisallowUnknownFields()
			if err := dec.Decode(req); err != nil {
				return nil, fmt.Errorf("failed to decode WriteToStdin args: %w", err)
			}
			return client.WriteToStdin(ctx, req)
		},
		"CreateTempDir": func(ctx context.Context, client targetagentproto.TargetAgentClient, args json.RawMessage) (proto.Message, error) {
			if len(args) >= 0 && string(args) != "{}" {
				return nil, fmt.Errorf("CreateTempDir does not accept arguments")
			}
			return client.CreateTempDir(ctx, &emptypb.Empty{})
		},
		"Mkdir": func(ctx context.Context, client targetagentproto.TargetAgentClient, args json.RawMessage) (proto.Message, error) {
			req := &targetagentproto.MkdirRequest{}
			dec := json.NewDecoder(bytes.NewReader(args))
			dec.DisallowUnknownFields()
			if err := dec.Decode(req); err != nil {
				return nil, fmt.Errorf("failed to decode Mkdir args: %w", err)
			}
			return client.Mkdir(ctx, req)
		},
		"Rm": func(ctx context.Context, client targetagentproto.TargetAgentClient, args json.RawMessage) (proto.Message, error) {
			req := &targetagentproto.RmRequest{}
			dec := json.NewDecoder(bytes.NewReader(args))
			dec.DisallowUnknownFields()
			if err := dec.Decode(req); err != nil {
				return nil, fmt.Errorf("failed to decode Rm args: %w", err)
			}
			return client.Rm(ctx, req)
		},
		"MakeWritable": func(ctx context.Context, client targetagentproto.TargetAgentClient, args json.RawMessage) (proto.Message, error) {
			req := &targetagentproto.MakeWritableRequest{}
			dec := json.NewDecoder(bytes.NewReader(args))
			dec.DisallowUnknownFields()
			if err := dec.Decode(req); err != nil {
				return nil, fmt.Errorf("failed to decode MakeWritable args: %w", err)
			}
			return client.MakeWritable(ctx, req)
		},
		"Chown": func(ctx context.Context, client targetagentproto.TargetAgentClient, args json.RawMessage) (proto.Message, error) {
			req := &targetagentproto.ChownRequest{}
			dec := json.NewDecoder(bytes.NewReader(args))
			dec.DisallowUnknownFields()
			if err := dec.Decode(req); err != nil {
				return nil, fmt.Errorf("failed to decode Chown args: %w", err)
			}
			return client.Chown(ctx, req)
		},
		"ListFiles": func(ctx context.Context, client targetagentproto.TargetAgentClient, args json.RawMessage) (proto.Message, error) {
			req := &targetagentproto.ListFilesRequest{}
			dec := json.NewDecoder(bytes.NewReader(args))
			dec.DisallowUnknownFields()
			if err := dec.Decode(req); err != nil {
				return nil, fmt.Errorf("failed to decode ListFiles args: %w", err)
			}
			return client.ListFiles(ctx, req)
		},
		"StoreFile": func(ctx context.Context, client targetagentproto.TargetAgentClient, args json.RawMessage) (proto.Message, error) {
			type storeFileArgs struct {
				LocalPath  string `json:"local_path"`
				RemotePath string `json:"remote_path"`
				Append     bool   `json:"append"`
			}
			req := &storeFileArgs{}
			dec := json.NewDecoder(bytes.NewReader(args))
			dec.DisallowUnknownFields()
			if err := dec.Decode(req); err != nil {
				return nil, fmt.Errorf("failed to decode StoreFile args: %w", err)
			}

			f, err := fs.Open(req.LocalPath)
			if err != nil {
				return nil, fmt.Errorf("failed to open local file: %w", err)
			}
			defer f.Close()

			stream, err := client.StoreFile(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to open StoreFile stream: %w", err)
			}

			if err := stream.Send(&targetagentproto.StoreRequest{
				Item: &targetagentproto.StoreRequest_Open{
					Open: &targetagentproto.OpenFileRequest{
						Path:   req.RemotePath,
						Append: req.Append,
					},
				},
			}); err != nil {
				return nil, fmt.Errorf("failed to send OpenFileRequest: %w", err)
			}

			buf := make([]byte, 64*1024)
			for {
				n, readErr := f.Read(buf)
				if n > 0 {
					err = stream.Send(&targetagentproto.StoreRequest{
						Item: &targetagentproto.StoreRequest_Content{Content: buf[:n]},
					})
					if err != nil {
						return nil, fmt.Errorf("failed to send content: %w", err)
					}
				}
				if readErr == io.EOF {
					break
				}
				if readErr != nil {
					return nil, fmt.Errorf("file read error: %w", readErr)
				}
			}

			_, err = stream.CloseAndRecv()
			if err != nil {
				return nil, fmt.Errorf("failed to close stream: %w", err)
			}
			return &emptypb.Empty{}, nil
		},
		"RetrieveFile": func(ctx context.Context, client targetagentproto.TargetAgentClient, args json.RawMessage) (proto.Message, error) {
			type retrieveFileArgs struct {
				LocalPath  string `json:"local_path"`
				RemotePath string `json:"remote_path"`
			}
			req := &retrieveFileArgs{}
			dec := json.NewDecoder(bytes.NewReader(args))
			dec.DisallowUnknownFields()
			if err := dec.Decode(req); err != nil {
				return nil, fmt.Errorf("failed to decode RetrieveFile args: %w", err)
			}

			exists, err := afero.Exists(fs, req.LocalPath)
			if err != nil {
				return nil, fmt.Errorf("failed to check local path: %w", err)
			}
			if exists {
				return nil, fmt.Errorf("local file already exists: %s", req.LocalPath)
			}

			file, err := fs.OpenFile(req.LocalPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, perms.LocalFilePerm)
			if err != nil {
				return nil, fmt.Errorf("failed to create local file: %w", err)
			}
			defer file.Close()

			stream, err := client.RetrieveFile(ctx, &targetagentproto.FileRequest{Path: req.RemotePath})
			if err != nil {
				return nil, fmt.Errorf("failed to start RetrieveFile stream: %w", err)
			}

			for {
				chunk, err := stream.Recv()
				if err == io.EOF {
					break
				}
				if err != nil {
					return nil, fmt.Errorf("failed receiving from stream: %w", err)
				}
				if _, err := file.Write(chunk.Content); err != nil {
					return nil, fmt.Errorf("failed writing chunk to local file: %w", err)
				}
			}

			return &emptypb.Empty{}, nil
		},
		"GetTargetInfo": func(ctx context.Context, client targetagentproto.TargetAgentClient, args json.RawMessage) (proto.Message, error) {
			if len(args) >= 0 && string(args) != "{}" {
				return nil, fmt.Errorf("GetTargetInfo does not accept arguments")
			}
			return client.GetTargetInfo(ctx, &emptypb.Empty{})
		},
		"ElevatePrivileges": func(ctx context.Context, client targetagentproto.TargetAgentClient, args json.RawMessage) (proto.Message, error) {
			req := &targetagentproto.ElevatePrivilegesRequest{}
			raw := string(args)
			if len(args) == 0 || raw == "" || raw == "null" || raw == "{}" {
				// Default to passwordless sudo
				req.Proof = &targetagentproto.PrivilegeProof{
					Mech: &targetagentproto.PrivilegeProof_NoPasswdSudo{},
				}
			} else {
				if err := protojson.Unmarshal(args, req); err != nil {
					return nil, fmt.Errorf("failed to decode ElevatePrivileges args: %w", err)
				}
				if req.GetProof() == nil {
					return nil, fmt.Errorf("privilege proof is required")
				}
			}
			return client.ElevatePrivileges(ctx, req)
		},
	}
}

// NewRegistry initializes an RPCRegistry with default handlers and OS Fs.
func NewRegistry() *RPCRegistry {
	return NewRegistryWithFs(afero.NewOsFs())
}

// NewRegistryWithFs initializes an RPCRegistry with default handlers.
func NewRegistryWithFs(fs afero.Fs) *RPCRegistry {
	return &RPCRegistry{handlers: getRPCHandlers(fs)}
}

// RPCRegistry holds mapping of RPC method names to handler functions.
type RPCRegistry struct {
	handlers map[string]func(ctx context.Context, client targetagentproto.TargetAgentClient, args json.RawMessage) (proto.Message, error)
}

// NewRPCHandler parses a request string and returns a configured RPCHandler.
func (r *RPCRegistry) NewRPCHandler(method string) (*RPCHandler, error) {
	h, ok := r.handlers[method]
	if !ok {
		return nil, fmt.Errorf("unsupported request: %s", method)
	}
	return &RPCHandler{Name: method, handler: h}, nil
}

// RPCHandler encapsulates a specific RPC invocation request.
type RPCHandler struct {
	Name    string
	handler func(ctx context.Context, client targetagentproto.TargetAgentClient, args json.RawMessage) (proto.Message, error)
}

// Invoke executes the RPC using the provided context and client.
func (h *RPCHandler) Invoke(ctx context.Context, client targetagentproto.TargetAgentClient, args json.RawMessage) (proto.Message, error) {
	return h.handler(ctx, client, args)
}
