// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package sourcecontent

import (
	"context"
	"os"
	"path"
	"strings"

	"github.com/Arm-Debug/apap-cli/apap-engine/agent"
	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
	targetagentproto "github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

// RetrieveFileFunc retrieves a file from a target agent.
type RetrieveFileFunc func(ctx context.Context, client targetagentproto.TargetAgentClient, remotePath string, compress bool, reservedCapacity int, prog agent.ReportProgress) ([]byte, error)

// FetchHostFile reads source content from the host filesystem.
func FetchHostFile(hostPath string) (string, error) {
	data, err := os.ReadFile(hostPath)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// FetchTargetFile reads source content from a target using the supplied agent client and platform.
func FetchTargetFile(ctx context.Context, client targetagentproto.TargetAgentClient, platform *conductor.TargetPlatform, targetPath string, retrieve RetrieveFileFunc) (string, error) {
	if retrieve == nil {
		retrieve = agent.RetrieveFileToMemory
	}
	normalizedPath := platform.Path.ToOSPath(cleanTargetPath(targetPath, platform.OS))
	data, err := retrieve(ctx, client, normalizedPath, false, 0, nil)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func cleanTargetPath(p string, targetOS conductor.OS) string {
	forced := util.ForceToSlash(p)
	isUNC := targetOS == conductor.Win && strings.HasPrefix(forced, "//")
	cleaned := path.Clean(forced)
	if isUNC {
		// path.Clean collapses leading double slashes; restore for UNC.
		cleaned = "//" + strings.TrimPrefix(cleaned, "/")
	}
	return cleaned
}
