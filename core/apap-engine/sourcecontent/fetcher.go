// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package sourcecontent

// fetcher.go contains SourceFilesFetcher, used to fetch source content
// concurrently from host and target machines. Source content is returned
// verbatim as a single string. Consumers should perform any required
// post-processing, such as splitting it into lines. Failures are returned as
// SourceFailureReason values.

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/Arm-Debug/apap-cli/apap-engine/agent"
	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/targetsession"
)

// A small fixed worker pool overlaps per-file latency without allowing large
// batches to place unbounded load on the host or target.
const defaultSourceFilesFetchConcurrency = 4

// SourceFile identifies one source file and the ordered locations to try.
type SourceFile struct {
	Locations []SourceFileLocation
	// MinimumLineCount rejects source mappings that cannot contain a required
	// source line, allowing the next configured location to be tried.
	MinimumLineCount uint32
}

// SourceFileLocation identifies one source file location.
type SourceFileLocation struct {
	Location SourceLocation
	Path     string
}

// SourceFileContent contains fetched source content and failure metadata.
// Line-oriented consumers should split Content themselves rather than making
// the fetcher discard information needed by complete-file consumers.
type SourceFileContent struct {
	// Content is the complete source file exactly as read from the selected
	// location, including its original line endings and trailing newline.
	Content        string
	Failures       []SourceFileFailure
	LoadedLocation SourceFileLocation
}

// SourceFileFailure describes a failed source-content fetch attempt.
type SourceFileFailure struct {
	Location SourceLocation
	Path     string
	Err      error
	Reason   SourceFailureReason
}

type SourceLocation string

const (
	SourceLocationHost   SourceLocation = "host"
	SourceLocationTarget SourceLocation = "target"
)

// SourceFilesFetcher fetches the content of the given source files.
// One result is returned for each input file, in order.
type SourceFilesFetcher func(files []SourceFile) []SourceFileContent

type SourceFailureReason string

const (
	SourceFailureMissingHostMapping     SourceFailureReason = "missing_host_mapping"
	SourceFailureHostPathFailed         SourceFailureReason = "host_source_path_failed"
	SourceFailureHostPathMismatched     SourceFailureReason = "host_source_path_mismatched"
	SourceFailureTargetNotReachable     SourceFailureReason = "target_not_reachable"
	SourceFailureTargetAgentUnavailable SourceFailureReason = "target_agent_unavailable"
	SourceFailureTargetPathFailed       SourceFailureReason = "target_source_path_failed"
	SourceFailureTargetPathMismatched   SourceFailureReason = "target_source_path_mismatched"

	sourceFailureNone SourceFailureReason = ""
)

type targetFileFetcher func(ctx context.Context, targetPath string) (content string, failure SourceFailureReason, err error)

// NewSourceFilesFetcher returns a source file fetcher for the given target.
func NewSourceFilesFetcher(ctx context.Context, tgt target.Target, targetSessions targetsession.TargetSessionProvider) SourceFilesFetcher {
	return func(files []SourceFile) []SourceFileContent {
		tgtFileFetcher := newTargetFileFetcher(tgt, targetSessions, nil)
		return fetchSourceFiles(ctx, files, tgtFileFetcher, defaultSourceFilesFetchConcurrency)
	}
}

// fetchSourceFiles fetches source file content concurrently and returns results
// in the same order as files.
func fetchSourceFiles(ctx context.Context, files []SourceFile, tgtFileFetcher targetFileFetcher, concurrency int) []SourceFileContent {
	if len(files) == 0 {
		return nil
	}
	if concurrency <= 0 {
		concurrency = 1
	}

	type sourceFileResult struct {
		index   int
		content SourceFileContent
	}

	results := make(chan sourceFileResult, len(files))
	jobs := make(chan int, len(files))
	for i := range files {
		jobs <- i
	}
	close(jobs)

	workerCount := min(concurrency, len(files))
	var wg sync.WaitGroup
	wg.Add(workerCount)
	for range workerCount {
		go func() {
			defer wg.Done()
			for sourceFileIndex := range jobs {
				results <- sourceFileResult{
					index:   sourceFileIndex,
					content: fetchSourceFile(ctx, files[sourceFileIndex], tgtFileFetcher),
				}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	contents := make([]SourceFileContent, len(files))
	for result := range results {
		contents[result.index] = result.content
	}
	return contents
}

// fetchSourceFile tries each configured source location in order until success or all locations fail.
func fetchSourceFile(ctx context.Context, sourceFile SourceFile, tgtFileFetcher targetFileFetcher) SourceFileContent {
	result := SourceFileContent{}
	for _, location := range sourceFile.Locations {
		var content string
		var failure SourceFileFailure
		switch location.Location {
		case SourceLocationHost:
			content, failure = fetchHostSourceLocation(location)
		case SourceLocationTarget:
			content, failure = fetchTargetSourceLocation(ctx, location, tgtFileFetcher)
		default:
			failure = sourceLocationFailure(location, sourceFailureNone, fmt.Errorf("unsupported source location %q", location.Location))
		}

		if failure.Err != nil {
			result.Failures = append(result.Failures, failure)
			continue
		}

		if sourceFile.MinimumLineCount == 0 || sourceContentLineCount(content) >= int(sourceFile.MinimumLineCount) {
			result.Content = content
			result.LoadedLocation = location
			return result
		}

		reason := SourceFailureTargetPathMismatched
		if location.Location == SourceLocationHost {
			reason = SourceFailureHostPathMismatched
		}
		result.Failures = append(result.Failures, SourceFileFailure{
			Location: location.Location,
			Path:     location.Path,
			Err:      fmt.Errorf("source content shorter than minimum line count %d", sourceFile.MinimumLineCount),
			Reason:   reason,
		})
	}
	if len(result.Failures) == 0 {
		result.Failures = append(result.Failures, SourceFileFailure{Err: fmt.Errorf("missing source location")})
	}
	return result
}

// sourceContentLineCount returns the number of lines separated by '\n'.
func sourceContentLineCount(content string) int {
	lineCount := strings.Count(content, "\n")
	if !strings.HasSuffix(content, "\n") {
		lineCount++
	}
	return lineCount
}

// fetchHostSourceLocation reads source content from the host.
func fetchHostSourceLocation(location SourceFileLocation) (string, SourceFileFailure) {
	if location.Path == "" {
		return "", sourceLocationFailure(location, SourceFailureMissingHostMapping, fmt.Errorf("missing host source mapping"))
	}
	content, err := FetchHostFile(location.Path)
	if err == nil && content != "" {
		return content, SourceFileFailure{}
	}
	if err == nil {
		err = fmt.Errorf("empty source content")
	}
	return "", sourceLocationFailure(location, SourceFailureHostPathFailed, err)
}

// fetchTargetSourceLocation reads source content from the target.
func fetchTargetSourceLocation(ctx context.Context, location SourceFileLocation, tgtFileFetcher targetFileFetcher) (string, SourceFileFailure) {
	if location.Path == "" {
		return "", sourceLocationFailure(location, SourceFailureTargetPathFailed, fmt.Errorf("missing target source path"))
	}
	if tgtFileFetcher == nil {
		return "", sourceLocationFailure(location, SourceFailureTargetNotReachable, fmt.Errorf("target source fetcher required"))
	}
	content, failure, err := tgtFileFetcher(ctx, location.Path)
	if err == nil && content != "" {
		return content, SourceFileFailure{}
	}
	if err == nil {
		err = fmt.Errorf("empty source content")
		failure = SourceFailureTargetPathFailed
	}
	return "", sourceLocationFailure(location, failure, err)
}

func sourceLocationFailure(location SourceFileLocation, reason SourceFailureReason, err error) SourceFileFailure {
	return SourceFileFailure{
		Location: location.Location,
		Path:     location.Path,
		Err:      err,
		Reason:   reason,
	}
}

// newTargetFileFetcher returns a target file fetcher that initializes the
// target session on the first target fetch.
func newTargetFileFetcher(tgt target.Target, targetSessions targetsession.TargetSessionProvider, retrieveFile RetrieveFileFunc) targetFileFetcher {
	if tgt == nil || targetSessions == nil {
		return nil
	}

	var once sync.Once
	var agentConn *agent.AgentConn
	var platform *conductor.TargetPlatform
	var setupErr error
	var setupFailure SourceFailureReason

	return func(ctx context.Context, targetPath string) (string, SourceFailureReason, error) {
		once.Do(func() {
			targetSession, err := targetSessions.TargetSession(tgt)
			if err != nil {
				setupErr = err
				setupFailure = SourceFailureTargetNotReachable
				return
			}
			if targetSession == nil {
				setupErr = fmt.Errorf("target source fetcher required")
				setupFailure = SourceFailureTargetNotReachable
				return
			}
			if _, err := targetSession.Connect(ctx); err != nil {
				setupErr = err
				setupFailure = SourceFailureTargetNotReachable
				return
			}
			agentConn, setupErr = targetSession.TargetAgent(ctx)
			if setupErr != nil {
				setupFailure = SourceFailureTargetAgentUnavailable
				return
			}
			platform, setupErr = targetSession.TargetPlatform()
			if setupErr != nil {
				setupFailure = SourceFailureTargetAgentUnavailable
			}
		})
		if setupErr != nil {
			return "", setupFailure, setupErr
		}
		content, err := FetchTargetFile(ctx, agentConn.Client, platform, targetPath, retrieveFile)
		if err != nil {
			return "", SourceFailureTargetPathFailed, err
		}
		return content, sourceFailureNone, nil
	}
}
