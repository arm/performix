// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"errors"
	"os/user"
	"runtime"
	"testing"
)

func TestCurrentUsernamePrefersLookup(t *testing.T) {
	origLookup := lookupCurrentUser
	t.Cleanup(func() { lookupCurrentUser = origLookup })

	expected := "lookup-user"
	lookupCurrentUser = func() (*user.User, error) {
		return &user.User{Username: expected}, nil
	}

	got, err := CurrentUsername()
	if err != nil {
		t.Fatalf("CurrentUsername() returned error: %v", err)
	}
	if got != expected {
		t.Fatalf("CurrentUsername() = %q, want %q", got, expected)
	}
}

func TestCurrentUsernameEnvFallback(t *testing.T) {
	origLookup := lookupCurrentUser
	t.Cleanup(func() { lookupCurrentUser = origLookup })

	lookupCurrentUser = func() (*user.User, error) {
		return nil, errors.New("forced failure")
	}

	expected := "env-user"
	t.Setenv("USER", expected)
	t.Setenv("LOGNAME", "")
	t.Setenv("USERNAME", "")

	got, err := CurrentUsername()
	if err != nil {
		t.Fatalf("CurrentUsername() returned error: %v", err)
	}
	if got != expected {
		t.Fatalf("CurrentUsername() = %q, want %q", got, expected)
	}
}

func TestCurrentUsernameSudoFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sudo environment variables are not used on windows")
	}

	origLookup := lookupCurrentUser
	t.Cleanup(func() { lookupCurrentUser = origLookup })

	lookupCurrentUser = func() (*user.User, error) {
		return nil, errors.New("forced failure")
	}

	// clear standard envs to force sudo fallback
	t.Setenv("USER", "")
	t.Setenv("LOGNAME", "")
	t.Setenv("USERNAME", "")
	expected := "sudo-user"
	t.Setenv("SUDO_USER", expected)
	t.Setenv("SUDO_UID", "")
	t.Setenv("SUDO_USERNAME", "")

	got, err := CurrentUsername()
	if err != nil {
		t.Fatalf("CurrentUsername() returned error: %v", err)
	}
	if got != expected {
		t.Fatalf("CurrentUsername() = %q, want %q", got, expected)
	}
}

func TestCurrentUsernameNoMatch(t *testing.T) {
	origLookup := lookupCurrentUser
	t.Cleanup(func() { lookupCurrentUser = origLookup })

	lookupCurrentUser = func() (*user.User, error) {
		return nil, errors.New("forced failure")
	}

	t.Setenv("USER", "")
	t.Setenv("LOGNAME", "")
	t.Setenv("USERNAME", "")
	t.Setenv("SUDO_USER", "")
	t.Setenv("SUDO_UID", "")
	t.Setenv("SUDO_USERNAME", "")

	got, err := CurrentUsername()
	if !errors.Is(err, errNoUsername) {
		t.Fatalf("CurrentUsername() error = %v, want errNoUsername", err)
	}
	if got != "" {
		t.Fatalf("CurrentUsername() = %q, want empty string", got)
	}
}
