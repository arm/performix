// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package conductor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
)

type ADBClient struct {
	serialNumber    string
	deviceIPAddress *string
	runner          ADBRunner
	mu              sync.Mutex
	connected       bool
	adbAddress      string
	forwards        map[*adbForwardConn]struct{}
}

func NewADBClient(serialNumber string, deviceIPAddress *string, runner ADBRunner) *ADBClient {
	return &ADBClient{
		serialNumber:    serialNumber,
		deviceIPAddress: deviceIPAddress,
		runner:          runner,
	}
}

func (c *ADBClient) CheckHealth() error {
	return c.CheckHealthContext(context.Background())
}

// CheckHealthContext also reports the identity resolved during this check.
func (c *ADBClient) CheckHealthContext(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	device, err := ResolveADBDeviceBySerial(c.serialNumber, c.deviceIPAddress, c.runner)
	if err != nil {
		c.connected = false
		return err
	}
	reportADBDevice(ctx, device)
	if c.adbAddress != "" && c.adbAddress != device.Address {
		c.connected = false
		// The session must discard cached agents and their forwarded connections.
		return fmt.Errorf("android ADB transport changed from %q to %q", c.adbAddress, device.Address)
	}
	c.adbAddress = device.Address
	_, _, err = c.runner.Run("-s", c.adbAddress, "get-state")
	c.connected = err == nil
	if err != nil {
		return message.New(message.EngineConductorAdbDeviceNotFound).WithMetadata(map[string]string{"serial": c.serialNumber}).WithCause(err)
	}
	return nil
}

func (c *ADBClient) Close() error {
	c.mu.Lock()
	forwards := make([]*adbForwardConn, 0, len(c.forwards))
	for forward := range c.forwards {
		forwards = append(forwards, forward)
	}
	c.connected = false
	c.mu.Unlock()
	var errs []error
	for _, forward := range forwards {
		errs = append(errs, forward.Close())
	}
	return errors.Join(errs...)
}

func (c *ADBClient) CommandRunner() CommandRunner {
	return &ADBCommandRunner{client: c}
}

func (c *ADBClient) Filesystem() TargetFilesystem {
	return &ADBTargetFilesystem{client: c}
}

func (c *ADBClient) ensureConnected() error {
	if c.connected {
		return nil
	}
	device, err := ResolveADBDeviceBySerial(c.serialNumber, c.deviceIPAddress, c.runner)
	if err != nil {
		return err
	}
	c.adbAddress = device.Address
	c.connected = true
	return nil
}

func (c *ADBClient) adbDevice(args ...string) (string, string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.adbDeviceLocked(args...)
}

func (c *ADBClient) adbDeviceLocked(args ...string) (string, string, error) {
	if err := c.ensureConnected(); err != nil {
		return "", "", err
	}
	deviceArgs := append([]string{"-s", c.adbAddress}, args...)
	stdout, stderr, err := c.runner.Run(deviceArgs...)
	if err != nil {
		c.connected = false
	}
	return stdout, stderr, err
}

// COMMAND RUNNER

type ADBCommandRunner struct {
	client *ADBClient
}

func (r *ADBCommandRunner) RunCommand(cmd string) (string, string, error) {
	return r.client.adbShell(cmd)
}

func (c *ADBClient) adbShell(command string) (string, string, error) {
	return c.adbDevice("shell", command)
}

type ADBRunner interface {
	Run(args ...string) (string, string, error)
}

type ExecADBRunner struct {
	mu         sync.RWMutex
	executable string
}

func NewExecADBRunner(executable string) *ExecADBRunner {
	return &ExecADBRunner{executable: executable}
}

func (r *ExecADBRunner) Run(args ...string) (string, string, error) {
	var stdout, stderr bytes.Buffer
	r.mu.RLock()
	adb := r.executable
	r.mu.RUnlock()
	if adb == "" {
		return "", "", message.Wrap(message.EngineConductorAdbAdbNotFound, exec.ErrNotFound)
	}
	cmd := newLocalCommand(adb, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist) {
		err = message.Wrap(message.EngineConductorAdbAdbNotFound, err)
	}
	return stdout.String(), stderr.String(), err
}

func (r *ExecADBRunner) SetExecutable(executable string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.executable = executable
}

// TCP DIALER VIA ADB PORT FORWARDING

type adbForwardConn struct {
	net.Conn
	client    *ADBClient
	localSpec string
	address   string
	once      sync.Once
	closeErr  error
}

func (c *ADBClient) Dial(network, addr string) (net.Conn, error) {
	remotePort, err := portFromAddr(addr)
	if err != nil {
		return nil, err
	}

	localPort, err := freeLocalPort()
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	localSpec := fmt.Sprintf("tcp:%d", localPort)
	remoteSpec := fmt.Sprintf("tcp:%d", remotePort)
	if _, _, err := c.adbDeviceLocked("forward", localSpec, remoteSpec); err != nil {
		return nil, err
	}

	conn, err := net.Dial(network, net.JoinHostPort("127.0.0.1", strconv.Itoa(localPort)))
	if err != nil {
		_, _, _ = c.runner.Run("-s", c.adbAddress, "forward", "--remove", localSpec)
		return nil, err
	}

	forward := &adbForwardConn{Conn: conn, client: c, localSpec: localSpec, address: c.adbAddress}
	if c.forwards == nil {
		c.forwards = make(map[*adbForwardConn]struct{})
	}
	c.forwards[forward] = struct{}{}
	return forward, nil
}

func (c *adbForwardConn) Close() error {
	c.once.Do(func() {
		connErr := c.Conn.Close()
		// Cleanup belongs to the transport that created the forward, even after a
		// device reconnects at a different address.
		c.client.mu.Lock()
		defer c.client.mu.Unlock()
		_, _, forwardErr := c.client.runner.Run("-s", c.address, "forward", "--remove", c.localSpec)
		delete(c.client.forwards, c)
		c.closeErr = errors.Join(connErr, forwardErr)
	})
	return c.closeErr
}

func portFromAddr(addr string) (int, error) {
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return 0, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return 0, err
	}
	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("port out of range: %d", port)
	}
	return port, nil
}

func freeLocalPort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

// FILESYSTEM

type ADBTargetFilesystem struct {
	client *ADBClient
}

// Produces size in bytes and permissions like "1234 -rw-r--r--" for example
func (fs *ADBTargetFilesystem) FileStat(name string) (os.FileInfo, error) {
	command := fmt.Sprintf("stat -c '%%s %%A' %s", adbShellQuote(name))
	stdout, _, err := fs.client.adbShell(command)
	if err != nil {
		return nil, err
	}

	fields := strings.Fields(stdout)
	if len(fields) < 2 {
		return nil, fmt.Errorf("could not parse adb stat output for %q: %q", name, stdout)
	}

	size, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("could not parse adb stat size for %q: %w", name, err)
	}

	if fields[1] == "" {
		return nil, fmt.Errorf("could not parse adb stat permission mode for %q: %q", name, stdout)
	}

	mode := os.FileMode(0)
	if fields[1][0] == 'd' {
		mode = os.ModeDir
	}
	return newTargetFileInfo(name, size, mode), nil
}

func (fs *ADBTargetFilesystem) CreateDirTree(path string, perm os.FileMode) error {
	quotedPath := adbShellQuote(path)
	if _, _, err := fs.client.adbShell(fmt.Sprintf("mkdir -p %s", quotedPath)); err != nil {
		return err
	}
	_, _, err := fs.client.adbShell(fmt.Sprintf("chmod %s %s", adbChmodMode(perm), quotedPath))
	return err
}

func (fs *ADBTargetFilesystem) RemoveDirTree(path string) error {
	_, _, err := fs.client.adbShell(fmt.Sprintf("rm -rf %s", adbShellQuote(path)))
	return err
}

func (fs *ADBTargetFilesystem) CreateEmptyFile(path string, perm os.FileMode) error {
	quotedPath := adbShellQuote(path)
	if _, _, err := fs.client.adbShell(fmt.Sprintf("touch %s", quotedPath)); err != nil {
		return err
	}
	_, _, err := fs.client.adbShell(fmt.Sprintf("chmod %s %s", adbChmodMode(perm), quotedPath))
	return err
}

func (fs *ADBTargetFilesystem) CopyFromHost(hostPath string, targetPath string, progress []ReportProgressRequest) error {
	size, err := hostFileSize(hostPath)
	if err != nil {
		return err
	}
	reportTransferProgress(0, size, progress)
	_, _, err = fs.client.adbDevice("push", hostPath, targetPath)
	if err != nil {
		return err
	}
	reportTransferProgress(size, size, progress)
	return nil
}

func adbShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func adbChmodMode(perm os.FileMode) string {
	permString := perm.Perm().String()
	return fmt.Sprintf(
		"u=%s,g=%s,o=%s",
		removePermissionDash(permString[1:4]),
		removePermissionDash(permString[4:7]),
		removePermissionDash(permString[7:10]),
	)
}

func removePermissionDash(permission string) string {
	return strings.ReplaceAll(permission, "-", "")
}

func hostFileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

func reportTransferProgress(transferred int64, size int64, progress []ReportProgressRequest) {
	when := time.Now()
	for _, update := range progress {
		if update.Callback != nil {
			update.Callback(transferred, size, when)
		}
	}
}
