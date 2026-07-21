// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package agent

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"google.golang.org/grpc"
	"google.golang.org/grpc/encoding/gzip"

	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

// ReportProgress is a callback function to report progress of file transfer.
type ReportProgress func(received int64)

// ReportProgressRequest is an io.Writer that reports progress via a callback.
type ReportProgressRequest struct {
	Callback ReportProgress
}

func (s *ReportProgressRequest) Write(p []byte) (int, error) {
	s.Callback(int64(len(p)))
	return len(p), nil
}

// sendStreamToWriter reads all chunks from the stream and writes them to the provided writer.
// If opts.Progress != nil, the writer is wrapped to report progress.
func sendStreamToWriter(ctx context.Context, stream targetagentproto.TargetAgent_RetrieveFileClient, writer io.Writer, opts ReportProgress) error {
	w := writer

	if opts != nil {
		w = io.MultiWriter(writer, &ReportProgressRequest{Callback: opts})
	}

	chunk := targetagentproto.FileContent{}
	for {
		chunk.Reset()
		err := stream.RecvMsg(&chunk)
		if err == io.EOF {
			break
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if err != nil {
			return fmt.Errorf("recv chunk: %w", err)
		}
		if len(chunk.Content) == 0 {
			continue
		}
		if _, err := w.Write(chunk.Content); err != nil {
			return fmt.Errorf("write chunk: %w", err)
		}
	}
	return nil
}

// ReceiveFile retrieves a single file from the agent and writes it to local disk, requesting a
// compressed transfer. The received file is first written to a temp file, then atomically
// renamed to the final location.
//
// If prog is non-nil, progress updates are emitted.
func ReceiveFile(
	ctx context.Context,
	localPath string,
	remotePath string,
	agent targetagentproto.TargetAgentClient,
	prog ReportProgress,
) (err error) {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	dir := filepath.Dir(localPath)
	if err := os.MkdirAll(dir, perms.LocalDirPerm); err != nil {
		return fmt.Errorf("mkdir %q: %w", filepath.Dir(localPath), err)
	}

	out, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary file in %q: %w", dir, err)
	}
	tmpFileName := out.Name()

	// Ensure close + capture on failure
	closeTempFile := func() {
		if cerr := out.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}
	removeTempFile := func() {
		if removeErr := os.Remove(tmpFileName); removeErr != nil && err == nil {
			err = removeErr
		}
	}

	scClose := util.ScopeCleaner{}
	scRemove := util.ScopeCleaner{}
	defer func() {
		scClose.MaybeCleanup(closeTempFile)
		scRemove.MaybeCleanup(removeTempFile)
	}()

	bw := bufio.NewWriterSize(out, 256*1024)

	stream, err := agent.RetrieveFile(
		ctx,
		&targetagentproto.FileRequest{Path: remotePath},
		grpc.UseCompressor(gzip.Name),
	)
	if err != nil {
		return fmt.Errorf("start retrieve %q: %w", dir, err)
	}

	if err = sendStreamToWriter(ctx, stream, bw, prog); err != nil {
		return err
	}

	if ferr := bw.Flush(); ferr != nil {
		return fmt.Errorf("flush %q: %w", tmpFileName, ferr)
	}

	if serr := out.Sync(); serr != nil { // Ensure all data is on disk ready for rename
		return fmt.Errorf("fsync %q: %w", tmpFileName, serr)
	}

	scClose.CancelCleanup() // Prevent double close

	if cerr := out.Close(); cerr != nil {
		return fmt.Errorf("close %q: %w", tmpFileName, cerr)
	}

	if rerr := os.Rename(tmpFileName, localPath); rerr != nil {
		return fmt.Errorf("rename %q -> %q: %w", tmpFileName, localPath, rerr)
	}

	scRemove.CancelCleanup()

	return err
}

// RetrieveFileToMemory retrieves a single file from the agent and returns it as a byte slice.
// If compress is true, a compressed transfer is requested.
// reservedCapacity can be used to preallocate buffer space to minimise reallocation.
// If prog is non-nil, progress updates are emitted.
func RetrieveFileToMemory(
	ctx context.Context,
	client targetagentproto.TargetAgentClient,
	remotePath string,
	compress bool,
	reservedCapacity int,
	prog ReportProgress,
) ([]byte, error) {
	var stream targetagentproto.TargetAgent_RetrieveFileClient
	var err error
	if compress {
		stream, err = client.RetrieveFile(ctx, &targetagentproto.FileRequest{Path: remotePath}, grpc.UseCompressor(gzip.Name))
	} else {
		stream, err = client.RetrieveFile(ctx, &targetagentproto.FileRequest{Path: remotePath})
	}
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if reservedCapacity > 0 {
		buf.Grow(reservedCapacity)
	}

	if err = sendStreamToWriter(ctx, stream, &buf, prog); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
