// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"errors"
	"os"
	"os/user"
	"runtime"
	"strings"
)

var errNoUsername = errors.New("unable to determine current username")

// lookupCurrentUser points at os/user.Current and can be overridden in tests.
var lookupCurrentUser = user.Current

// CurrentUsername attempts to find the username that launched the current process. It checks user.Current() first,
// then standard login environment variables, and finally sudo-specific environment variables. The user.Current()
// method doesn't always work; for example, in minimal container environments, if not built with cgo, or if missing
// win32 APIs. Windows doesn't have sudo-style elevation, so the sudo checks are skipped there.
func CurrentUsername() (string, error) {
	if u, err := lookupCurrentUser(); err == nil && strings.TrimSpace(u.Username) != "" {
		return strings.TrimSpace(u.Username), nil
	}
	for _, key := range []string{"USER", "LOGNAME", "USERNAME"} {
		if val := strings.TrimSpace(os.Getenv(key)); val != "" {
			return val, nil
		}
	}
	if runtime.GOOS != "windows" {
		for _, key := range []string{"SUDO_USER", "SUDO_UID", "SUDO_USERNAME"} {
			if val := strings.TrimSpace(os.Getenv(key)); val != "" {
				return val, nil
			}
		}
	}
	return "", errNoUsername
}
