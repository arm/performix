// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package filetransfer

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/afero"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

// FileTransferManager contains methoeds for transferring files between the client and target.
type FileTransferManager interface {
	StoreFile(stream targetagentproto.TargetAgent_StoreFileServer) error
	RetrieveFile(req *targetagentproto.FileRequest, stream targetagentproto.TargetAgent_RetrieveFileServer) error
}

// CommonFileTransferManager is a cross-platform implementation of FileTransferManager.
type CommonFileTransferManager struct {
	fs afero.Fs
}

func NewFileTransferManager() *CommonFileTransferManager {
	return NewFileTransferManagerWithFs(afero.NewOsFs())
}

func NewFileTransferManagerWithFs(fs afero.Fs) *CommonFileTransferManager {
	return &CommonFileTransferManager{fs: fs}
}

// StoreFile handles a stream of StoreRequest messages to create and write to a file on the target.
// The stream must first send an Open message to specify the target file path and write mode.
// Subsequent Content messages will be written to the file.
func (m *CommonFileTransferManager) StoreFile(stream targetagentproto.TargetAgent_StoreFileServer) error {
	var file afero.File
	defer func() {
		if file != nil {
			file.Close()
		}
	}()

	req, err := stream.Recv()
	if err != nil {
		return fmt.Errorf("failed to receive OpenFileRequest: %w", err)
	}

	openReq, ok := req.Item.(*targetagentproto.StoreRequest_Open)
	if !ok {
		return fmt.Errorf("first message must be OpenFileRequest")
	}

	flags := os.O_CREATE | os.O_WRONLY
	if openReq.Open.Append {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}

	path := filepath.Clean(openReq.Open.Path)
	file, err = m.fs.OpenFile(path, flags, perms.TargetFilePerm)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}

	for {
		req, err := stream.Recv()
		if err == io.EOF {
			return stream.SendAndClose(&emptypb.Empty{})
		}
		if err != nil {
			return err
		}

		switch x := req.Item.(type) {
		case *targetagentproto.StoreRequest_Content:
			if _, err := file.Write(x.Content); err != nil {
				return fmt.Errorf("failed to write content: %w", err)
			}

		default:
			return fmt.Errorf("unexpected StoreRequest item, expected Content")
		}
	}
}

// RetrieveFile reads the contents of the file specified in the request from the target and sends it in chunks over a stream.
func (m *CommonFileTransferManager) RetrieveFile(req *targetagentproto.FileRequest, stream targetagentproto.TargetAgent_RetrieveFileServer) error {
	path := filepath.Clean(req.Path)
	file, err := m.fs.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	ctx := stream.Context()
	buf := make([]byte, 64*1024)
	for {
		// Check for client cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		n, readErr := file.Read(buf)
		if n > 0 {
			if sendErr := stream.Send(&targetagentproto.FileContent{
				Content: buf[:n],
			}); sendErr != nil {
				return fmt.Errorf("failed to send chunk: %w", sendErr)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fmt.Errorf("file read error: %w", readErr)
		}
	}
	return nil
}
