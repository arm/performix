// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package conductor

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

type ADBClient struct {
	serialNumber    string
	deviceIPAddress *string
	runner          ADBRunner
	mu              sync.Mutex
	connected       bool
}

func NewADBClient(serialNumber string, deviceIPAddress *string) *ADBClient {
	return &ADBClient{
		serialNumber:    serialNumber,
		deviceIPAddress: deviceIPAddress,
		runner:          &ExecADBRunner{},
	}
}

func newADBClientWithRunner(serialNumber string, deviceIPAddress *string, runner ADBRunner) *ADBClient {
	return &ADBClient{
		serialNumber:    serialNumber,
		deviceIPAddress: deviceIPAddress,
		runner:          runner,
	}
}

func (c *ADBClient) CheckHealth() error {
	if _, _, err := c.adbDevice("get-state"); err != nil {
		return fmt.Errorf("health check for Android target failed: %w", err)
	}
	return nil
}

func (c *ADBClient) Close() error {
	return nil
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
	devices, err := c.adbDevices()
	if err != nil {
		return err
	}
	if adbDevicesHasDevice(devices, c.serialNumber) {
		c.connected = true
		return nil
	}
	if c.deviceIPAddress == nil || *c.deviceIPAddress == "" {
		return fmt.Errorf("android device %q is not visible in adb devices and no device IP address is configured", c.serialNumber)
	}
	if err := c.adbConnect(*c.deviceIPAddress); err != nil {
		return err
	}
	c.connected = true
	return nil
}

func (c *ADBClient) adbDevices() (string, error) {
	stdout, _, err := c.runner.Run("devices")
	return stdout, err
}

func (c *ADBClient) adbConnect(deviceIPAddress string) error {
	_, _, err := c.runner.Run("connect", deviceIPAddress)
	return err
}

func adbDevicesHasDevice(output string, serialNumber string) bool {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == serialNumber && fields[1] == "device" {
			return true
		}
	}
	return false
}

func (c *ADBClient) adb(args ...string) (string, string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ensureConnected(); err != nil {
		return "", "", err
	}
	stdout, stderr, err := c.runner.Run(args...)
	if err != nil {
		c.connected = false
	}
	return stdout, stderr, err
}

func (c *ADBClient) adbDevice(args ...string) (string, string, error) {
	deviceArgs := append([]string{"-s", c.serialNumber}, args...)
	return c.adb(deviceArgs...)
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

type ExecADBRunner struct{}

func (r *ExecADBRunner) Run(args ...string) (string, string, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.Command("adb", args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// TCP DIALER VIA ADB PORT FORWARDING

type adbForwardConn struct {
	net.Conn
	client    *ADBClient
	localSpec string
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

	localSpec := fmt.Sprintf("tcp:%d", localPort)
	remoteSpec := fmt.Sprintf("tcp:%d", remotePort)
	if _, _, err := c.adbDevice("forward", localSpec, remoteSpec); err != nil {
		return nil, err
	}

	conn, err := net.Dial(network, net.JoinHostPort("127.0.0.1", strconv.Itoa(localPort)))
	if err != nil {
		_, _, _ = c.adbDevice("forward", "--remove", localSpec)
		return nil, err
	}

	return &adbForwardConn{Conn: conn, client: c, localSpec: localSpec}, nil
}

func (c *adbForwardConn) Close() error {
	c.once.Do(func() {
		connErr := c.Conn.Close()
		_, _, forwardErr := c.client.adbDevice("forward", "--remove", c.localSpec)
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
