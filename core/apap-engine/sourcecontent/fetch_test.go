// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package sourcecontent

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/agent"
	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	targetagentmocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
	targetagentproto "github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

func TestFetchHostFile(t *testing.T) {
	tmp, err := os.CreateTemp("", "source-fetch-*")
	require.NoError(t, err)
	defer os.Remove(tmp.Name())

	require.NoError(t, os.WriteFile(tmp.Name(), []byte("hello"), 0o600))

	got, err := FetchHostFile(tmp.Name())
	require.NoError(t, err)
	require.Equal(t, "hello", got)
}

func TestFetchHostFileNotFound(t *testing.T) {
	_, err := FetchHostFile("/nonexistent/path/that/does/not/exist")
	require.Error(t, err)
}

func TestFetchTargetFileDefaultRetrieve(t *testing.T) {
	client := &targetagentmocks.TargetAgentClient{}
	client.
		On("RetrieveFile", mock.Anything, mock.Anything, mock.Anything).
		Return((targetagentproto.TargetAgent_RetrieveFileClient)(nil), errors.New("stub")).
		Once()

	platform := &conductor.TargetPlatform{
		Path:                  &conductor.LinuxPathUtils{},
		PlatformConfiguration: conductor.PlatformConfiguration{OS: conductor.Linux},
	}

	_, err := FetchTargetFile(context.Background(), client, platform, "/usr/src/foo.c", nil)
	require.Error(t, err)
	client.AssertExpectations(t)
}

func TestFetchTargetFile(t *testing.T) {
	client := &targetagentmocks.TargetAgentClient{}
	platform := &conductor.TargetPlatform{
		Path:                  &conductor.WindowsPathUtils{},
		PlatformConfiguration: conductor.PlatformConfiguration{OS: conductor.Win},
	}
	var retrievedPath string

	got, err := FetchTargetFile(context.Background(), client, platform, `\\server\share\dir\file.c`, func(_ context.Context, gotClient targetagentproto.TargetAgentClient, remotePath string, _ bool, _ int, _ agent.ReportProgress) ([]byte, error) {
		require.Same(t, client, gotClient)
		retrievedPath = remotePath
		return []byte("target content"), nil
	})

	require.NoError(t, err)
	require.Equal(t, "target content", got)
	require.Equal(t, `\\server\share\dir\file.c`, retrievedPath)
}

func TestFetchTargetFilePathNormalization(t *testing.T) {
	tests := []struct {
		name         string
		targetOS     conductor.OS
		pathUtils    conductor.PathUtilities
		inputPath    string
		expectedPath string
	}{
		{
			name:         "linux target from windows host",
			targetOS:     conductor.Linux,
			pathUtils:    &conductor.LinuxPathUtils{},
			inputPath:    `\\etc\\hosts`,
			expectedPath: "/etc/hosts",
		},
		{
			name:         "windows target posix input",
			targetOS:     conductor.Win,
			pathUtils:    &conductor.WindowsPathUtils{},
			inputPath:    "/etc/hosts",
			expectedPath: `\etc\hosts`,
		},
		{
			name:         "windows target unc path preserved",
			targetOS:     conductor.Win,
			pathUtils:    &conductor.WindowsPathUtils{},
			inputPath:    `\\server\share\dir\file.c`,
			expectedPath: `\\server\share\dir\file.c`,
		},
		{
			name:         "windows target drive path unaffected",
			targetOS:     conductor.Win,
			pathUtils:    &conductor.WindowsPathUtils{},
			inputPath:    `C:\dir\file.c`,
			expectedPath: `C:\dir\file.c`,
		},
		{
			name:         "windows target forward slash drive path",
			targetOS:     conductor.Win,
			pathUtils:    &conductor.WindowsPathUtils{},
			inputPath:    "C:/dir/file.c",
			expectedPath: `C:\dir\file.c`,
		},
		{
			name:         "linux target clean posix path",
			targetOS:     conductor.Linux,
			pathUtils:    &conductor.LinuxPathUtils{},
			inputPath:    "/usr/src/foo.c",
			expectedPath: "/usr/src/foo.c",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			platform := &conductor.TargetPlatform{
				Path:                  tt.pathUtils,
				PlatformConfiguration: conductor.PlatformConfiguration{OS: tt.targetOS},
			}
			var retrievedPath string

			got, err := FetchTargetFile(context.Background(), &targetagentmocks.TargetAgentClient{}, platform, tt.inputPath, func(_ context.Context, _ targetagentproto.TargetAgentClient, remotePath string, _ bool, _ int, _ agent.ReportProgress) ([]byte, error) {
				retrievedPath = remotePath
				return []byte("ok"), nil
			})

			require.NoError(t, err)
			require.Equal(t, "ok", got)
			require.Equal(t, tt.expectedPath, retrievedPath)
		})
	}
}
