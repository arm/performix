// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package ssh

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
)

const authPrefix = "auth="

// LoginString represents a parsed SSH login string.
type LoginString struct {
	User               string
	Host               string
	Port               int32
	PrivateKeyFilename string
	AuthMethod         string
}

// ParseLoginString parses an SSH login string of the form "[user@]host[:port][:key_path][:auth=key|password]"
// and returns user (or ""), host, port (or "0" if not specified), key_path (or "" if not specified), auth (or "" if not specified).
func ParseLoginString(s string) (LoginString, error) {
	origString := s
	var err error
	var result LoginString

	// Split user and host[:port][:key_path][:auth=key|password]
	atSplit := strings.SplitN(s, "@", 2)
	if len(atSplit) == 2 {
		result.User = atSplit[0]
		s = atSplit[1]
	}

	// Split host, [:port], and [:key_path][:auth=key|password]
	colonSplit := strings.SplitN(s, ":", 3)

	result.Host = colonSplit[0]
	if result.Host == "" {
		return LoginString{}, message.New(message.EngineSshMissingHost).WithMetadata(map[string]string{"loginString": origString})
	}

	switch l := len(colonSplit); l {
	case 2:
		// Assume the solitary optional string is a port number if it can be converted to an int
		if port, err := strconv.ParseInt(colonSplit[1], 10, 32); err == nil { // authMethodFromProto converts apapproto.SSHAuthMethod protobuf to target.SSHAuthMethod
			result.Port = int32(port)
		} else {
			result.PrivateKeyFilename, result.AuthMethod = splitKeyPathAndAuth(colonSplit[1])
		}
	case 3:
		// Assume the first optional string is a port number if it can be converted to an int, otherwise assume it's part of the key path
		if port, err := strconv.ParseInt(colonSplit[1], 10, 32); err == nil {
			result.Port = int32(port)
			result.PrivateKeyFilename, result.AuthMethod = splitKeyPathAndAuth(colonSplit[2])
		} else {
			keyPathAndAuth := colonSplit[1] + ":" + colonSplit[2]
			result.PrivateKeyFilename, result.AuthMethod = splitKeyPathAndAuth(keyPathAndAuth)
		}
	}

	if result.PrivateKeyFilename, err = expandPath(result.PrivateKeyFilename); err != nil {
		return LoginString{}, message.New(message.EngineSshMissingHomeDir).WithMetadata(map[string]string{"privateKeyPath": result.PrivateKeyFilename}).WithCause(err)
	}

	return result, nil
}

// splitKeyPathAndAuth splits a string of the form "key_path:auth=method" into key_path and method.
// If the auth suffix is not present, it returns the original string as key_path and an empty string as method.
// If the key path is not present, it returns an empty string as key_path and the original string as method.
func splitKeyPathAndAuth(s string) (string, string) {
	if strings.HasPrefix(s, authPrefix) {
		return "", strings.TrimPrefix(s, authPrefix)
	}
	authDelim := ":" + authPrefix
	idx := strings.LastIndex(s, authDelim)
	if idx != -1 {
		return s[:idx], s[idx+len(authDelim):]
	}
	return s, ""
}

// expandPath expands paths with shell-like syntax into a standard format
func expandPath(path string) (string, error) {
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path, err
		}
		return filepath.Join(home, path[1:]), nil
	}
	return path, nil
}
