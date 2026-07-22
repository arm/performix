// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package conductor

import (
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
)

type progressUpdate struct {
	received int64
	max      int64
	when     time.Time
}

func recordProgress(updates *[]progressUpdate) ReportProgress {
	return func(received int64, max int64, when time.Time) {
		*updates = append(*updates, progressUpdate{
			received: received,
			max:      max,
			when:     when,
		})
	}
}

func lastProgressUpdate(t *testing.T, updates []progressUpdate) progressUpdate {
	t.Helper()
	require.NotEmpty(t, updates)
	return updates[len(updates)-1]
}

func TestCopyFile(t *testing.T) {
	srcFS := afero.NewMemMapFs()
	dstFS := afero.NewMemMapFs()
	require.NoError(t, srcFS.MkdirAll("/src", 0o755))
	require.NoError(t, dstFS.MkdirAll("/dst", 0o755))
	require.NoError(t, afero.WriteFile(srcFS, "/src/report.txt", []byte("payload"), 0o644))

	var updates []progressUpdate
	err := CopyFile("/src/report.txt", srcFS, "/dst/report.txt", dstFS, []ReportProgressRequest{{
		Callback: recordProgress(&updates),
		Interval: time.Millisecond * 10,
	}})

	require.NoError(t, err)
	got, err := afero.ReadFile(dstFS, "/dst/report.txt")
	require.NoError(t, err)
	assert.Equal(t, []byte("payload"), got)

	last := lastProgressUpdate(t, updates)
	assert.Equal(t, int64(len("payload")), last.received)
	assert.Equal(t, int64(len("payload")), last.max)
	assert.False(t, last.when.IsZero())
}

func TestCopyFileReturnsErrorMessage(t *testing.T) {
	srcFS := afero.NewMemMapFs()
	dstFS := afero.NewMemMapFs()
	require.NoError(t, dstFS.MkdirAll("/dst", 0o755))

	err := CopyFile("/src/missing.txt", srcFS, "/dst/report.txt", dstFS, nil)

	require.Error(t, err)
	msg := message.IsMessage(err)
	require.NotNil(t, msg)
	assert.Equal(t, message.EngineConductorFileTransferOpenSrcFile, msg.Code())
	assert.Equal(t, "/src/missing.txt", msg.Metadata()["srcFilePath"])
	assert.Equal(t, "/dst/report.txt", msg.Metadata()["dstFilePath"])
}

func newTestSFTPClient(t *testing.T, root string) *sftp.Client {
	t.Helper()

	serverConn, clientConn := net.Pipe()

	server, err := sftp.NewServer(
		serverConn,
		sftp.WithServerWorkingDirectory(root),
	)
	require.NoError(t, err)

	go func() {
		err := server.Serve()
		if err != nil && err != io.EOF {
			t.Logf("sftp server stopped: %v", err)
		}
	}()

	client, err := sftp.NewClientPipe(clientConn, clientConn)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = client.Close()
		_ = server.Close()
		_ = serverConn.Close()
		_ = clientConn.Close()
	})

	return client
}

func TestCopyFileSFTP(t *testing.T) {
	srcFS := afero.NewMemMapFs()
	require.NoError(t, srcFS.MkdirAll("/src", 0o755))
	require.NoError(t, afero.WriteFile(srcFS, "/src/tool", []byte("payload"), 0o644))

	remoteRoot := t.TempDir()
	destFS := afero.NewBasePathFs(afero.NewOsFs(), remoteRoot)
	client := newTestSFTPClient(t, remoteRoot)

	var progress []int64
	err := CopyFileSFTP("/src/tool", srcFS, "tool", destFS, client, []ReportProgressRequest{{
		Callback: func(received, max int64, _ time.Time) {
			progress = append(progress, received, max)
		},
		Interval: time.Hour,
	}})

	require.NoError(t, err)

	got, err := os.ReadFile(filepath.Join(remoteRoot, "tool"))
	require.NoError(t, err)
	assert.Equal(t, []byte("payload"), got)

	assert.Contains(t, progress, int64(len("payload")))

}
