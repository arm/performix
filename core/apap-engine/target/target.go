// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package target

import (
	"errors"
	"fmt"
	"os/user"
	"slices"
	"strings"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

const LocalhostName = "localhost"

type HostKeyPolicy int

const (
	IgnoreHostKey HostKeyPolicy = iota
	RejectHostKeyIfMissing
	AcceptNewHost
	AskNewHost
)

var ValidHostKeyPolicyValues = map[string]HostKeyPolicy{
	"ignore":     IgnoreHostKey,
	"strict":     RejectHostKeyIfMissing,
	"accept-new": AcceptNewHost,
	"ask":        AskNewHost,
}

var HostKeyPolicyToString = map[HostKeyPolicy]string{
	IgnoreHostKey:          "ignore",
	RejectHostKeyIfMissing: "strict",
	AcceptNewHost:          "accept-new",
	AskNewHost:             "ask",
}

func (p *HostKeyPolicy) UnmarshalJSON(data []byte) error {
	s, err := util.DecodeJSON[string](data)
	if err != nil {
		return err
	}
	switch *s {
	case "ignore":
		*p = IgnoreHostKey
	case "accept-new":
		*p = AcceptNewHost
	case "ask":
		*p = AskNewHost
	case "strict":
		fallthrough
	default:
		*p = RejectHostKeyIfMissing
	}
	return nil
}

func (p HostKeyPolicy) MarshalJSON() ([]byte, error) {
	var s string
	switch p {
	case IgnoreHostKey:
		s = "ignore"
	case RejectHostKeyIfMissing:
		s = "strict"
	case AcceptNewHost:
		s = "accept-new"
	case AskNewHost:
		s = "ask"
	default:
		s = ""
	}
	return util.EncodeJSON(&s)
}

// SSHAuthMethod defines the authentication method for SSH connections.
type SSHAuthMethod int

const (
	SSHAuthMethodKey SSHAuthMethod = iota
	SSHAuthMethodPassword
)

var ValidSSHAuthMethods = map[string]SSHAuthMethod{
	"key":      SSHAuthMethodKey,
	"password": SSHAuthMethodPassword,
}

var SSHAuthMethodToString = map[SSHAuthMethod]string{
	SSHAuthMethodKey:      "key",
	SSHAuthMethodPassword: "password",
}

func (m *SSHAuthMethod) UnmarshalJSON(data []byte) error {
	s, err := util.DecodeJSON[string](data)
	if err != nil {
		return err
	}
	switch *s {
	case "password":
		*m = SSHAuthMethodPassword
	case "key":
		fallthrough
	default:
		*m = SSHAuthMethodKey
	}
	return nil
}

func (m SSHAuthMethod) MarshalJSON() ([]byte, error) {
	var s string
	switch m {
	case SSHAuthMethodKey:
		s = "key"
	case SSHAuthMethodPassword:
		s = "password"
	default:
		s = ""
	}
	return util.EncodeJSON(&s)
}

// SSHHostConfig is the configuration for a single jump in an SSHTarget.
type SSHHostConfig struct {
	Host               string
	Port               int32
	Username           string
	PrivateKeyFilename string
	HostKeyPolicy      HostKeyPolicy
	AuthMethod         SSHAuthMethod
}

const defaultSSHPort = 22

// DisplayString returns a string that can be used for user-facing display of this host
func (c SSHHostConfig) DisplayString() string {
	var portStr string
	if c.Port != defaultSSHPort {
		portStr = fmt.Sprintf(":%d", c.Port)
	}

	var userStr = c.Username
	if len(userStr) == 0 {
		userStr = "<unknown>"
	}

	var hostStr = c.Host
	if len(hostStr) == 0 {
		hostStr = "<unknown>"
	}

	return fmt.Sprintf("%s@%s%s", userStr, hostStr, portStr)
}

// Apply default values to SSHostconfig.
// Port default -> 22
// Username default -> current system user (if determinable)
func (c *SSHHostConfig) ApplyDefaults() {
	if c.Port == 0 {
		c.Port = defaultSSHPort
	}
	if c.Username == "" {
		current, err := user.Current()
		if err == nil { // leave empty if current user can't be determined
			c.Username = current.Username
		}
	}
}

// SSHTarget is a target configuration for an SSH connection. Jumps are the hosts on the path from the user's machine
// to the target. The last item on the list is the target. If the list is empty, LastJump will return a default
// initialized host config.
type SSHTarget struct {
	Jumps []SSHHostConfig
}

// LastJump returns the final jump, or a default initialized host config if the list is empty.
func (t *SSHTarget) LastJump() SSHHostConfig {
	if len(t.Jumps) == 0 {
		return SSHHostConfig{}
	}
	return t.Jumps[len(t.Jumps)-1]
}

// DisplayHost returns the LastJump's DisplayString, for SSHTarget.
func (t *SSHTarget) DisplayHost() string {
	return t.LastJump().DisplayString()
}

// GetUserDataDirectoryName returns the username of the LastJump.
func (t *SSHTarget) GetUserDataDirectoryName() (string, error) {
	if len(t.Jumps) == 0 {
		return "", errors.New("cannot generate user data directory name: missing host configuration")
	}

	name := t.LastJump().Username

	if len(name) == 0 {
		return "", errors.New("cannot generate user data directory name: missing user name")
	}

	return name, nil
}

// String converts SSHTarget to string. Prints all jump hosts in the form "lastjump (via secondLastJump, thirdLastJump, ...)
func (t *SSHTarget) String() string {
	lastJump := t.LastJump()

	var viaStr string
	if len(t.Jumps) > 1 {
		parts := util.Map(
			t.Jumps[:len(t.Jumps)-1],
			func(config SSHHostConfig) string { return config.DisplayString() },
		)
		slices.Reverse(parts)
		viaParts := strings.Join(parts, ", ")
		viaStr = fmt.Sprintf(" (via %s)", viaParts)
	}

	return fmt.Sprintf("%s%s", lastJump.DisplayString(), viaStr)
}

func (t *SSHTarget) ReadKnownHostsPolicy() HostKeyPolicy {
	return t.LastJump().HostKeyPolicy
}

func (t *SSHTarget) Validate(name string) error {
	if isNameReserved(name) {
		return message.New(message.EngineTargetConfigNameReserved).WithMetadata(map[string]string{"name": name})
	}

	for _, jump := range t.Jumps {
		if err := validateHost(jump); err != nil {
			return err
		}
		if err := validatePort(jump); err != nil {
			return err
		}
		if err := validateKey(jump); err != nil {
			return err
		}
	}
	return nil
}

// LocalTarget is the configuration struct for local deployment.
type LocalTarget struct {
}

// DisplayHost returns LocalhostName for LocalTarget.
func (t *LocalTarget) DisplayHost() string {
	return LocalhostName
}

// GetUserDataDirectoryName returns the current user's name, for LocalTarget.
func (t *LocalTarget) GetUserDataDirectoryName() (string, error) {
	currentUser, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("cannot get user data directory name: %w", err)
	}

	return currentUser.Username, nil
}

func (t *LocalTarget) String() string {
	return t.DisplayHost()
}

func (t *LocalTarget) Validate(name string) error {
	if isNameReserved(name) {
		return message.New(message.EngineTargetConfigNameReserved).WithMetadata(map[string]string{"name": name})
	}

	return nil
}

// AndroidTarget is the configuration struct for Android targets reached through ADB.
type AndroidTarget struct {
	SerialNumber    string
	DeviceIPAddress *string
}

// DisplayHost returns the serial number for AndroidTarget.
func (t *AndroidTarget) DisplayHost() string {
	if t.SerialNumber == "" {
		return "<unknown>"
	}
	return t.SerialNumber
}

// GetUserDataDirectoryName returns the serial number, for AndroidTarget.
func (t *AndroidTarget) GetUserDataDirectoryName() (string, error) {
	if t.SerialNumber == "" {
		return "", errors.New("cannot generate user data directory name: missing Android serial number")
	}
	return t.SerialNumber, nil
}

// String converts AndroidTarget to string. If DeviceIPAddress is set, it is included after the serial number.
func (t *AndroidTarget) String() string {
	if t.DeviceIPAddress == nil || *t.DeviceIPAddress == "" {
		return t.DisplayHost()
	}

	return fmt.Sprintf("%s (%s)", t.DisplayHost(), *t.DeviceIPAddress)
}

func (t *AndroidTarget) Validate(name string) error {
	if isNameReserved(name) {
		return message.New(message.EngineTargetConfigNameReserved).WithMetadata(map[string]string{"name": name})
	}

	if t.SerialNumber == "" {
		return message.New(message.EngineTargetConfigInvalidHostFormat).WithMetadata(map[string]string{"hostAddress": "", "jumpNode": "Android target"})
	}
	if t.SerialNumber[0] == '-' {
		return message.New(message.EngineTargetConfigInvalidHostFormat).WithMetadata(map[string]string{"hostAddress": t.SerialNumber, "jumpNode": "Android target"})
	}
	if strings.ContainsAny(t.SerialNumber, "\x00\t\n\r ") || strings.Count(t.SerialNumber, ":") > 1 {
		return message.New(message.EngineTargetConfigInvalidHostFormat).WithMetadata(map[string]string{"hostAddress": t.SerialNumber, "jumpNode": "Android target"})
	}
	if t.DeviceIPAddress == nil || *t.DeviceIPAddress == "" {
		return nil
	}
	if (*t.DeviceIPAddress)[0] == '-' || strings.ContainsAny(*t.DeviceIPAddress, "\x00\t\n\r ") || strings.Count(*t.DeviceIPAddress, ":") > 1 {
		return message.New(message.EngineTargetConfigInvalidHostFormat).WithMetadata(map[string]string{"hostAddress": t.SerialNumber + "@" + *t.DeviceIPAddress, "jumpNode": "Android target"})
	}
	return nil
}

type Target interface {
	// DisplayHost returns a string that can be used to display this host to the user.
	DisplayHost() string

	// GetUserDataDirectoryName returns the name of the subdirectory to be written into the temp data store on the target.
	GetUserDataDirectoryName() (string, error)

	// String returns a string representation of the target.
	String() string

	Validate(name string) error
}

type localHostVerifier interface {
	IsLocalhostSupported() bool
}

type concreteLocalhostVerifier struct {
}

func (t *concreteLocalhostVerifier) IsLocalhostSupported() bool {
	return util.IsLocalhostSupportedPlatform()
}
