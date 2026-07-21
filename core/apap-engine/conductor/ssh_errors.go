// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package conductor

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
	"syscall"

	"golang.org/x/crypto/ssh"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

type SSHErrorType string

const (
	KnownHostsErr SSHErrorType = "FailedKnownHosts"
	NetErr        SSHErrorType = "FailedNet"
	DNSErr        SSHErrorType = "FailedDns"
	TimeoutErr    SSHErrorType = "FailedTimeout"
	AuthErr       SSHErrorType = "FailedAuth"
	AlgsErr       SSHErrorType = "FailedAlgs"
	UnknownErr    SSHErrorType = "FailedUnknown"
)

// This is a list of all errors which indicate that automatic key detection will not be possible. If any of these
// errors are encountered while attempting to find an authorized key, the process should immediately fail.
var keyDetectionNotPossibleErrs = []SSHErrorType{NetErr, DNSErr, TimeoutErr, AlgsErr, KnownHostsErr}

const (
	authFailureUnknown  = ""
	authFailurePassword = "password"
	authFailureKey      = "key"
)

func authFailureMethod(err error) string {
	if err == nil {
		return authFailureUnknown
	}

	var msg message.Message
	if errors.As(err, &msg) {
		switch msg.Code() {
		case message.EngineConductorSshPasswordIncorrect,
			message.EngineConductorSshDirectConnFailedPasswordAuth,
			message.EngineConductorSshJumpConnFailedPasswordAuth:
			return authFailurePassword
		case message.EngineConductorSshDirectConnFailedKeyAuth,
			message.EngineConductorSshJumpConnFailedKeyAuth,
			message.EngineConductorSshKeyFileIncorrectPassphrase,
			message.EngineConductorSshKeyFileIncorrectPassphraseForTarget:
			return authFailureKey
		}
	}

	errMsg := err.Error()
	if strings.Contains(errMsg, "attempted methods [password]") || strings.Contains(errMsg, "attempted methods [password,") {
		return authFailurePassword
	}
	if strings.Contains(errMsg, "attempted methods [keyboard-interactive]") || strings.Contains(errMsg, "attempted methods [keyboard-interactive,") {
		return authFailurePassword
	}
	if strings.Contains(errMsg, "attempted methods [publickey]") || strings.Contains(errMsg, "attempted methods [publickey,") {
		return authFailureKey
	}
	if strings.Contains(errMsg, "password") && !strings.Contains(errMsg, "publickey") {
		return authFailurePassword
	}
	if strings.Contains(errMsg, "keyboard-interactive") && !strings.Contains(errMsg, "publickey") {
		return authFailurePassword
	}
	if strings.Contains(errMsg, "publickey") && !strings.Contains(errMsg, "password") {
		return authFailureKey
	}

	return authFailureUnknown
}

// consolidateDirectErrs takes a slice of errors encountered when trying to connect to the target directly via SSH,
// and some other metadata, and consolidates these errors into a single message.Message (by only looking at the last
// element in the slice). The returned Message is specific to the final failure which occurred.
func consolidateDirectErrs(errs []error, host string, port int32) message.Message {
	if len(errs) == 0 {
		return nil
	}
	lastErr := errs[len(errs)-1]
	metadata := map[string]string{
		"hostName": host,
	}

	switch getErrType(lastErr) {
	case NetErr:
		return message.New(message.EngineConductorSshDirectConnFailedNet).WithCause(lastErr).WithMetadata(metadata)
	case DNSErr:
		return message.New(message.EngineConductorSshDirectConnFailedDns).WithCause(lastErr).WithMetadata(metadata)
	case TimeoutErr:
		metadata["address"] = fmt.Sprintf("%s:%d", host, port)
		metadata["portNum"] = fmt.Sprintf("%d", port)
		return message.New(message.EngineConductorSshDirectConnFailedTimeout).WithCause(lastErr).WithMetadata(metadata)
	case AlgsErr:
		var algErr *ssh.AlgorithmNegotiationError
		if errors.As(lastErr, &algErr) {
			metadata["hostAlgs"] = util.DisplayErrorStringSlice(algErr.RequestedAlgorithms)
			metadata["localAlgs"] = util.DisplayErrorStringSlice(algErr.SupportedAlgorithms)
			return message.New(message.EngineConductorSshDirectConnFailedAlgs).WithCause(lastErr).WithMetadata(metadata)
		}
		// This should never happen as getErrType should only return AlgsErr if lastErr is an AlgorithmNegotiationError,
		// but just for safety, we proceed to returning unknown error
	case AuthErr:
		switch authFailureMethod(lastErr) {
		case authFailurePassword:
			return message.New(message.EngineConductorSshDirectConnFailedPasswordAuth).WithCause(lastErr).WithMetadata(metadata)
		case authFailureKey:
			return message.New(message.EngineConductorSshDirectConnFailedKeyAuth).WithCause(lastErr).WithMetadata(metadata)
		default:
			return message.New(message.CommonUnknownError).WithCause(errors.New("failed to authenticate to target via SSH, auth method unknown"))
		}
	case KnownHostsErr:
		var knownHostsErr message.Message
		if errors.As(lastErr, &knownHostsErr) {
			return knownHostsErr
		}
		// This should never happen as getErrType should only return KnownHostsErr if lastErr is a Message, but just
		// for safety, we proceed to returning unknown error
	}

	return message.New(message.EngineConductorSshDirectConnFailedUnknown).WithCause(lastErr).WithMetadata(metadata)
}

// consolidateJumpErrs takes a slice of errors encountered when trying to connect to a host via a previous jump node,
// and some other metadata, and consolidates these errors into a single message.Message (by only looking at the last
// element in the slice). The returned Message is specific to the final failure which occurred.
func consolidateJumpErrs(errs []error, prevHost string, host string, port int32) message.Message {
	if len(errs) == 0 {
		return nil
	}
	lastErr := errs[len(errs)-1]
	metadata := map[string]string{
		"nextHost": host,
		"prevHost": prevHost,
	}
	metadata["hostName"] = host

	switch getErrType(lastErr) {
	case NetErr:
		return message.New(message.EngineConductorSshJumpConnFailedNet).WithCause(lastErr).WithMetadata(metadata)
	case DNSErr:
		return message.New(message.EngineConductorSshJumpConnFailedDns).WithCause(lastErr).WithMetadata(metadata)
	case TimeoutErr:
		metadata["nextAddress"] = fmt.Sprintf("%s:%d", host, port)
		metadata["nextPort"] = fmt.Sprintf("%d", port)
		return message.New(message.EngineConductorSshJumpConnFailedTimeout).WithCause(lastErr).WithMetadata(metadata)
	case AlgsErr:
		var algErr *ssh.AlgorithmNegotiationError
		if errors.As(lastErr, &algErr) {
			metadata["nextHostAlgs"] = util.DisplayErrorStringSlice(algErr.RequestedAlgorithms)
			metadata["prevHostAlgs"] = util.DisplayErrorStringSlice(algErr.SupportedAlgorithms)
			return message.New(message.EngineConductorSshJumpConnFailedAlgs).WithCause(lastErr).WithMetadata(metadata)
		}
	case AuthErr:
		switch authFailureMethod(lastErr) {
		case authFailurePassword:
			return message.New(message.EngineConductorSshJumpConnFailedPasswordAuth).WithCause(lastErr).WithMetadata(metadata)
		case authFailureKey:
			return message.New(message.EngineConductorSshJumpConnFailedKeyAuth).WithCause(lastErr).WithMetadata(metadata)
		default:
			return message.New(message.CommonUnknownError).WithCause(errors.New("failed to authenticate to jump via SSH, auth method unknown"))
		}
	case KnownHostsErr:
		var knownHostsErr message.Message
		if errors.As(lastErr, &knownHostsErr) {
			return knownHostsErr
		}
	}
	return message.New(message.EngineConductorSshJumpConnFailedUnknown).WithCause(lastErr).WithMetadata(metadata)
}

// handleFindKeysDirectErr takes an error encountered when trying to connect to a host directly via SSH, and some
// other metadata, produces a Message specific to the failure which occurred, and either:
// - wraps this in a FIND_KEYS_FAILED_DIRECT message if the underlying failure indicates automatic key detection will
// not work (e.g. the host is not online)
// - returns nil if the underlying failure is specific to the key that was attempted (AuthErr or UnknownErr)
func handleFindKeysDirectErr(err error, host string, port int32) message.Message {
	cause := errors.Unwrap(err)
	if cause == nil {
		return nil
	}
	if slices.Contains(keyDetectionNotPossibleErrs, getErrType(cause)) {
		metadata := map[string]string{
			"address":  fmt.Sprintf("%s:%d", host, port),
			"hostName": host,
			"portNum":  fmt.Sprintf("%d", port),
		}
		return message.New(message.EngineConductorSshFindKeysFailedDirect).WithCause(err).WithMetadata(metadata)
	}
	return nil
}

// handleFindKeysJumpErr takes an error encountered when trying to connect to a host via a previous jump node, and
// some other metadata, produces a Message specific to the failure which occurred, and either:
// - wraps this in a FIND_KEYS_FAILED_JUMP message if the underlying failure indicates automatic key detection will
// not work (e.g. the next host is not online)
// - returns nil if the underlying failure is specific to the key that was attempted (AuthErr or UnknownErr)
func handleFindKeysJumpErr(err error, prevHost string, nextHost string, nextPort int32) message.Message {
	cause := errors.Unwrap(err)
	if cause == nil {
		return nil
	}
	if slices.Contains(keyDetectionNotPossibleErrs, getErrType(cause)) {
		metadata := map[string]string{
			"nextAddress": fmt.Sprintf("%s:%d", nextHost, nextPort),
			"nextHost":    nextHost,
			"nextPort":    fmt.Sprintf("%d", nextPort),
			"prevHost":    prevHost,
		}
		return message.New(message.EngineConductorSshFindKeysFailedJump).WithCause(err).WithMetadata(metadata)
	}
	return nil
}

// getErrType takes an error and categorises it into an SSHErrorType
func getErrType(err error) SSHErrorType {
	// If returned error contains a Message this indicates it came from the known_hosts callback func
	var knownHostsErr message.Message
	if errors.As(err, &knownHostsErr) {
		return KnownHostsErr
	}
	// Couldn't determine route to the target, likely no internet
	if errors.Is(err, syscall.ENETUNREACH) || strings.Contains(err.Error(), "connect: network is unreachable") {
		return NetErr
	}
	// Failed to resolve hostname (host doesn't exist or couldn't connect to DNS server - this could also indicate no internet)
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) || strings.Contains(err.Error(), "ssh: rejected: connect failed (Name or service not known)") {
		return DNSErr
	}

	// Context deadline exceeded (timeout)
	if errors.Is(err, context.DeadlineExceeded) {
		return TimeoutErr
	}

	// No response from target: target offline, nothing on port, not using necessary VPN etc
	var netErr *net.OpError
	if errors.As(err, &netErr) {
		// No response in time
		if netErr.Timeout() {
			return TimeoutErr
		}

		// Other unexpected network connection error
		return UnknownErr
	}

	// Failed to agree on cryptographic algorithms to use
	var algErr *ssh.AlgorithmNegotiationError
	if errors.As(err, &algErr) {
		return AlgsErr
	}
	// Credentials were rejected
	if strings.Contains(err.Error(), "ssh: unable to authenticate") {
		return AuthErr
	}
	// Otherwise cause is unknown (Server banner error, firewall blocked request etc)
	return UnknownErr
}
