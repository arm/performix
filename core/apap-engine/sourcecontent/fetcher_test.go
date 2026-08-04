// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package sourcecontent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/agent"
	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/targetsession"
	targetsessionmocks "github.com/Arm-Debug/apap-cli/apap-engine/targetsession/mocks"
	targetagentproto "github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

func hostSource(path string) SourceFileLocation {
	return SourceFileLocation{Location: SourceLocationHost, Path: path}
}

func targetSource(path string) SourceFileLocation {
	return SourceFileLocation{Location: SourceLocationTarget, Path: path}
}

func TestFetchSourceFilesFetchesHostFiles(t *testing.T) {
	sourceFile := filepath.Join(t.TempDir(), "host.c")
	require.NoError(t, os.WriteFile(sourceFile, []byte("host content"), 0o600))

	results := fetchSourceFiles(context.Background(), []SourceFile{
		{Locations: []SourceFileLocation{hostSource(sourceFile)}},
	}, nil, 2)

	require.Len(t, results, 1)
	assert.Equal(t, "host content", results[0].Content)
	assert.Equal(t, hostSource(sourceFile), results[0].LoadedLocation)
	assert.Empty(t, results[0].Failures)
}

func TestFetchSourceFilesPreservesSourceContent(t *testing.T) {
	sourceFile := filepath.Join(t.TempDir(), "host.c")
	require.NoError(t, os.WriteFile(sourceFile, []byte("line one\r\n\r\nline three\n"), 0o600))

	results := fetchSourceFiles(context.Background(), []SourceFile{
		{Locations: []SourceFileLocation{hostSource(sourceFile)}},
	}, nil, 2)

	require.Len(t, results, 1)
	assert.Equal(t, "line one\r\n\r\nline three\n", results[0].Content)
}

func TestSourceContentLineCount(t *testing.T) {
	tests := map[string]struct {
		content string
		want    int
	}{
		"empty":                  {content: "", want: 1},
		"single line":            {content: "one", want: 1},
		"trailing newline":       {content: "one\n", want: 1},
		"LF lines":               {content: "one\ntwo", want: 2},
		"CRLF lines":             {content: "one\r\ntwo\r\n", want: 2},
		"trailing blank LF line": {content: "one\n\n", want: 2},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, test.want, sourceContentLineCount(test.content))
		})
	}
}

func TestSourceFilesFetcherDoesNotResolveTargetSessionForHostFiles(t *testing.T) {
	sourceFile := filepath.Join(t.TempDir(), "host.c")
	require.NoError(t, os.WriteFile(sourceFile, []byte("host content"), 0o600))

	tgt := &target.LocalTarget{}
	provider := &targetsessionmocks.MockTargetSessionProvider{}
	fetcher := NewSourceFilesFetcher(context.Background(), tgt, provider)

	results := fetcher([]SourceFile{
		{Locations: []SourceFileLocation{hostSource(sourceFile)}},
	})

	require.Len(t, results, 1)
	assert.Equal(t, "host content", results[0].Content)
	assert.Equal(t, hostSource(sourceFile), results[0].LoadedLocation)
	provider.AssertNotCalled(t, "TargetSession", mock.Anything)
}

func TestFetchSourceFilesFallsBackToTarget(t *testing.T) {
	targetFileFetcher := func(_ context.Context, targetPath string) (string, SourceFailureReason, error) {
		if targetPath != "/target/file.c" {
			return "", SourceFailureTargetPathFailed, errors.New("unexpected target path")
		}
		return "target content", sourceFailureNone, nil
	}

	results := fetchSourceFiles(context.Background(), []SourceFile{
		{Locations: []SourceFileLocation{
			hostSource("/path/does/not/exist"),
			targetSource("/target/file.c"),
		}},
	}, targetFileFetcher, 2)

	require.Len(t, results, 1)
	assert.Equal(t, "target content", results[0].Content)
	assert.Equal(t, targetSource("/target/file.c"), results[0].LoadedLocation)
	require.Len(t, results[0].Failures, 1)
	assert.Equal(t, SourceLocationHost, results[0].Failures[0].Location)
	assert.Equal(t, "/path/does/not/exist", results[0].Failures[0].Path)
	assert.Equal(t, SourceFailureHostPathFailed, results[0].Failures[0].Reason)
}

func TestFetchSourceFilesRecordsEmptyHostFileAsMissing(t *testing.T) {
	sourceFile := filepath.Join(t.TempDir(), "host.c")
	require.NoError(t, os.WriteFile(sourceFile, nil, 0o600))

	results := fetchSourceFiles(context.Background(), []SourceFile{
		{Locations: []SourceFileLocation{hostSource(sourceFile)}},
	}, nil, 2)

	require.Len(t, results, 1)
	assert.Empty(t, results[0].Content)
	assert.Empty(t, results[0].LoadedLocation)
	require.Len(t, results[0].Failures, 1)
	assert.EqualError(t, results[0].Failures[0].Err, "empty source content")
	assert.Equal(t, SourceLocationHost, results[0].Failures[0].Location)
	assert.Equal(t, sourceFile, results[0].Failures[0].Path)
	assert.Equal(t, SourceFailureHostPathFailed, results[0].Failures[0].Reason)
}

func TestFetchSourceFilesRecordsMissingHostMapping(t *testing.T) {
	results := fetchSourceFiles(context.Background(), []SourceFile{
		{Locations: []SourceFileLocation{hostSource("")}},
	}, nil, 2)

	require.Len(t, results, 1)
	assert.Empty(t, results[0].Content)
	assert.Empty(t, results[0].LoadedLocation)
	require.Len(t, results[0].Failures, 1)
	assert.EqualError(t, results[0].Failures[0].Err, "missing host source mapping")
	assert.Equal(t, SourceLocationHost, results[0].Failures[0].Location)
	assert.Empty(t, results[0].Failures[0].Path)
	assert.Equal(t, SourceFailureMissingHostMapping, results[0].Failures[0].Reason)
}

func TestFetchSourceFilesFallsBackToTargetWhenHostSourceContentMismatched(t *testing.T) {
	sourceFile := filepath.Join(t.TempDir(), "host.c")
	require.NoError(t, os.WriteFile(sourceFile, []byte("line one\nline two"), 0o600))

	targetFileFetcher := func(_ context.Context, targetPath string) (string, SourceFailureReason, error) {
		if targetPath != "/target/file.c" {
			return "", SourceFailureTargetPathFailed, errors.New("unexpected target path")
		}
		return "target line one\ntarget line two\ntarget line three", sourceFailureNone, nil
	}

	results := fetchSourceFiles(context.Background(), []SourceFile{
		{
			Locations: []SourceFileLocation{
				hostSource(sourceFile),
				targetSource("/target/file.c"),
			},
			MinimumLineCount: 3,
		},
	}, targetFileFetcher, 2)

	require.Len(t, results, 1)
	assert.Equal(t, "target line one\ntarget line two\ntarget line three", results[0].Content)
	assert.Equal(t, targetSource("/target/file.c"), results[0].LoadedLocation)
	require.Len(t, results[0].Failures, 1)
	assert.Equal(t, SourceLocationHost, results[0].Failures[0].Location)
	assert.Equal(t, sourceFile, results[0].Failures[0].Path)
	assert.Equal(t, SourceFailureHostPathMismatched, results[0].Failures[0].Reason)
}

func TestFetchSourceFilesRecordsTargetMismatch(t *testing.T) {
	targetFileFetcher := func(_ context.Context, targetPath string) (string, SourceFailureReason, error) {
		assert.Equal(t, "/target/file.c", targetPath)
		return "line one\nline two", sourceFailureNone, nil
	}

	results := fetchSourceFiles(context.Background(), []SourceFile{
		{
			Locations:        []SourceFileLocation{targetSource("/target/file.c")},
			MinimumLineCount: 3,
		},
	}, targetFileFetcher, 2)

	require.Len(t, results, 1)
	assert.Empty(t, results[0].Content)
	assert.Empty(t, results[0].LoadedLocation)
	require.Len(t, results[0].Failures, 1)
	assert.Error(t, results[0].Failures[0].Err)
	assert.Equal(t, SourceLocationTarget, results[0].Failures[0].Location)
	assert.Equal(t, "/target/file.c", results[0].Failures[0].Path)
	assert.Equal(t, SourceFailureTargetPathMismatched, results[0].Failures[0].Reason)
}

func TestFetchSourceFilesRecordsSourceFailureReason(t *testing.T) {
	targetErr := errors.New("target failed")
	targetFileFetcher := func(_ context.Context, targetPath string) (string, SourceFailureReason, error) {
		assert.Equal(t, "/target/file.c", targetPath)
		return "", SourceFailureTargetAgentUnavailable, targetErr
	}

	results := fetchSourceFiles(context.Background(), []SourceFile{
		{Locations: []SourceFileLocation{targetSource("/target/file.c")}},
	}, targetFileFetcher, 2)

	require.Len(t, results, 1)
	assert.Empty(t, results[0].Content)
	assert.Empty(t, results[0].LoadedLocation)
	require.Len(t, results[0].Failures, 1)
	assert.ErrorIs(t, results[0].Failures[0].Err, targetErr)
	assert.Equal(t, SourceFileFailure{
		Location: SourceLocationTarget,
		Path:     "/target/file.c",
		Err:      targetErr,
		Reason:   SourceFailureTargetAgentUnavailable,
	}, results[0].Failures[0])
}

func TestFetchSourceFilesRecordsMissingTargetFetcher(t *testing.T) {
	results := fetchSourceFiles(context.Background(), []SourceFile{
		{Locations: []SourceFileLocation{targetSource("/target/file.c")}},
	}, nil, 2)

	require.Len(t, results, 1)
	assert.Empty(t, results[0].Content)
	assert.Empty(t, results[0].LoadedLocation)
	require.Len(t, results[0].Failures, 1)
	assert.Error(t, results[0].Failures[0].Err)
	assert.Equal(t, SourceLocationTarget, results[0].Failures[0].Location)
	assert.Equal(t, "/target/file.c", results[0].Failures[0].Path)
	assert.Equal(t, SourceFailureTargetNotReachable, results[0].Failures[0].Reason)
}

func TestTargetFileFetcherConnectsOnce(t *testing.T) {
	tgt := &target.LocalTarget{}
	provider := &targetsessionmocks.MockTargetSessionProvider{}
	session := &targetsessionmocks.MockTargetSession{}
	provider.On("TargetSession", tgt).Return(session, nil).Once()
	session.On("Connect", mock.Anything).Return(targetsession.TargetConnection(nil), nil).Once()
	session.On("TargetAgent", mock.Anything).Return(&agent.AgentConn{}, nil).Once()
	session.On("TargetPlatform").Return(&conductor.TargetPlatform{
		Path:                  &conductor.LinuxPathUtils{},
		PlatformConfiguration: conductor.PlatformConfiguration{OS: conductor.Linux},
	}, nil).Once()

	targetContent := map[string]string{
		"/target/one.c": "one",
		"/target/two.c": "two",
	}
	fetcher := newTargetFileFetcher(
		tgt,
		provider,
		func(_ context.Context, _ targetagentproto.TargetAgentClient, remotePath string, _ bool, _ int, _ agent.ReportProgress) ([]byte, error) {
			content, ok := targetContent[remotePath]
			if !ok {
				return nil, errors.New("unexpected target path")
			}
			return []byte(content), nil
		},
	)
	require.NotNil(t, fetcher)

	first, failure, err := fetcher(context.Background(), "/target/one.c")
	require.NoError(t, err)
	assert.Equal(t, sourceFailureNone, failure)
	second, failure, err := fetcher(context.Background(), "/target/two.c")
	require.NoError(t, err)
	assert.Equal(t, sourceFailureNone, failure)

	assert.Equal(t, "one", first)
	assert.Equal(t, "two", second)
	provider.AssertExpectations(t)
	session.AssertExpectations(t)
}

func TestTargetFileFetcherReturnsTargetSessionProviderError(t *testing.T) {
	tgt := &target.LocalTarget{}
	provider := &targetsessionmocks.MockTargetSessionProvider{}
	session := &targetsessionmocks.MockTargetSession{}
	providerErr := errors.New("target session failed")
	provider.On("TargetSession", tgt).Return(session, providerErr).Once()

	fetcher := newTargetFileFetcher(tgt, provider, nil)
	require.NotNil(t, fetcher)

	content, failure, err := fetcher(context.Background(), "/target/file.c")

	assert.Empty(t, content)
	assert.ErrorIs(t, err, providerErr)
	assert.Equal(t, SourceFailureTargetNotReachable, failure)
	provider.AssertExpectations(t)
	session.AssertNotCalled(t, "Connect", mock.Anything)
}

func TestTargetFileFetcherReturnsConnectFailure(t *testing.T) {
	tgt := &target.LocalTarget{}
	provider := &targetsessionmocks.MockTargetSessionProvider{}
	session := &targetsessionmocks.MockTargetSession{}
	connectErr := errors.New("connect failed")
	provider.On("TargetSession", tgt).Return(session, nil).Once()
	session.On("Connect", mock.Anything).Return(targetsession.TargetConnection(nil), connectErr).Once()

	fetcher := newTargetFileFetcher(tgt, provider, nil)
	require.NotNil(t, fetcher)

	content, failure, err := fetcher(context.Background(), "/target/file.c")

	assert.Empty(t, content)
	assert.ErrorIs(t, err, connectErr)
	assert.Equal(t, SourceFailureTargetNotReachable, failure)
	provider.AssertExpectations(t)
	session.AssertExpectations(t)
	session.AssertNotCalled(t, "TargetAgent", mock.Anything)
}

func TestTargetFileFetcherReturnsAgentFailure(t *testing.T) {
	tgt := &target.LocalTarget{}
	provider := &targetsessionmocks.MockTargetSessionProvider{}
	session := &targetsessionmocks.MockTargetSession{}
	agentErr := errors.New("agent failed")
	provider.On("TargetSession", tgt).Return(session, nil).Once()
	session.On("Connect", mock.Anything).Return(targetsession.TargetConnection(nil), nil).Once()
	session.On("TargetAgent", mock.Anything).Return((*agent.AgentConn)(nil), agentErr).Once()

	fetcher := newTargetFileFetcher(tgt, provider, nil)
	require.NotNil(t, fetcher)

	content, failure, err := fetcher(context.Background(), "/target/file.c")

	assert.Empty(t, content)
	assert.ErrorIs(t, err, agentErr)
	assert.Equal(t, SourceFailureTargetAgentUnavailable, failure)
	provider.AssertExpectations(t)
	session.AssertExpectations(t)
	session.AssertNotCalled(t, "TargetPlatform")
}

func TestTargetFileFetcherReturnsSourcePathFailure(t *testing.T) {
	tgt := &target.LocalTarget{}
	provider := &targetsessionmocks.MockTargetSessionProvider{}
	session := &targetsessionmocks.MockTargetSession{}
	retrieveErr := errors.New("retrieve failed")
	provider.On("TargetSession", tgt).Return(session, nil).Once()
	session.On("Connect", mock.Anything).Return(targetsession.TargetConnection(nil), nil).Once()
	session.On("TargetAgent", mock.Anything).Return(&agent.AgentConn{}, nil).Once()
	session.On("TargetPlatform").Return(&conductor.TargetPlatform{
		Path:                  &conductor.LinuxPathUtils{},
		PlatformConfiguration: conductor.PlatformConfiguration{OS: conductor.Linux},
	}, nil).Once()

	fetcher := newTargetFileFetcher(
		tgt,
		provider,
		func(_ context.Context, _ targetagentproto.TargetAgentClient, remotePath string, _ bool, _ int, _ agent.ReportProgress) ([]byte, error) {
			assert.Equal(t, "/target/file.c", remotePath)
			return nil, retrieveErr
		},
	)
	require.NotNil(t, fetcher)

	content, failure, err := fetcher(context.Background(), "/target/file.c")

	assert.Empty(t, content)
	assert.ErrorIs(t, err, retrieveErr)
	assert.Equal(t, SourceFailureTargetPathFailed, failure)
	provider.AssertExpectations(t)
	session.AssertExpectations(t)
}
