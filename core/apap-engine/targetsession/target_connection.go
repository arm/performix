// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package targetsession

import (
	"errors"
	"fmt"

	"github.com/pkg/sftp"
	"github.com/spf13/afero"

	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/grpcconnection"
)

// TargeConnection defines the interface for a connection to a target.
type TargetConnection interface {
	// CheckHealth checks if the connection is healthy.
	CheckHealth() error
	// Close closes the connection.
	Close() error
	// CommandRunner returns a command runner for the target.
	CommandRunner() conductor.CommandRunner
	// Filesystem returns a filesystem for the target.
	Filesystem() conductor.TargetFilesystem
	// Dialer returns a TCP dialer for the target.
	Dialer() grpcconnection.TCPDialer
}

// androidTargetConnection implements TargetConnection for Android targets.
type androidTargetConnection struct {
	client *conductor.ADBClient
}

func (s *androidTargetConnection) CheckHealth() error {
	return s.client.CheckHealth()
}

func (s *androidTargetConnection) Close() error {
	return s.client.Close()
}

func (s *androidTargetConnection) CommandRunner() conductor.CommandRunner {
	return s.client.CommandRunner()
}

func (s *androidTargetConnection) Filesystem() conductor.TargetFilesystem {
	return s.client.Filesystem()
}

func (s *androidTargetConnection) Dialer() grpcconnection.TCPDialer {
	return s.client
}

// localTargetConnection implements targetConnection for localhost.
type localTargetConnection struct {
}

func (s *localTargetConnection) CheckHealth() error {
	return nil
}

func (s *localTargetConnection) Close() error {
	return nil
}

func (s *localTargetConnection) CommandRunner() conductor.CommandRunner {
	return &conductor.LocalCommandRunner{}
}

func (s *localTargetConnection) Filesystem() conductor.TargetFilesystem {
	return conductor.NewAferoTargetFilesystem(afero.NewOsFs())
}

func (s *localTargetConnection) Dialer() grpcconnection.TCPDialer {
	return nil
}

// sshTargetConnection implements targetConnection for SSH targets.
type sshTargetConnection struct {
	sshConn   conductor.SecureClient
	sftpCache sftpCache
}

func (s *sshTargetConnection) CheckHealth() error {
	if err := s.sftpCache.CheckHealth(); err != nil {
		return fmt.Errorf("health check for target SSH connection failed: %w", err)
	}
	return nil
}

func (s *sshTargetConnection) Close() error {
	var errs []error
	if err := s.sftpCache.Close(); err != nil {
		errs = append(errs, err)
	}
	if err := s.sshConn.Close(); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return fmt.Errorf("error closing target SSH connection: %w", errors.Join(errs...))
	}
	return nil
}

func (s *sshTargetConnection) CommandRunner() conductor.CommandRunner {
	return s.sshConn.CommandRunner()
}

func (s *sshTargetConnection) Filesystem() conductor.TargetFilesystem {
	return conductor.NewSFTPTargetFilesystem(s.sftpCache.Client())
}

func (s *sshTargetConnection) Dialer() grpcconnection.TCPDialer {
	return s.sshConn
}

// sftpCache holds a cached SFTP client.
type sftpCache interface {
	Client() *sftp.Client
	CheckHealth() error
	Close() error
}

// concreteSftpCache implements SFTPCache.
type concreteSftpCache struct {
	sftpClient *sftp.Client
}

// newSftpCache creates a new sftpCache with the provided SFTP client.
func newSftpCache(sftpClient *sftp.Client) sftpCache {
	return &concreteSftpCache{sftpClient: sftpClient}
}

func (s *concreteSftpCache) Client() *sftp.Client {
	return s.sftpClient
}

func (s *concreteSftpCache) CheckHealth() error {
	if s.sftpClient == nil {
		return errors.New("no SFTP client")
	}
	_, err := s.sftpClient.RealPath(".")
	return err
}

func (s *concreteSftpCache) Close() error {
	if s.sftpClient == nil {
		return nil
	}
	return s.sftpClient.Close()
}
