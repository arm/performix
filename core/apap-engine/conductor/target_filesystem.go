// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package conductor

import (
	"os"
	"path/filepath"
	"time"

	"github.com/pkg/sftp"
	"github.com/spf13/afero"
	"github.com/spf13/afero/sftpfs"

	"github.com/Arm-Debug/apap-cli/apap-engine/aferoutils"
)

type TargetFilesystem interface {
	FileStat(path string) (os.FileInfo, error)
	CreateDirTree(path string, perm os.FileMode) error
	RemoveDirTree(path string) error
	CreateEmptyFile(path string, perm os.FileMode) error
	CopyFromHost(hostPath string, targetPath string, progress []ReportProgressRequest) error
}

type aferoTargetFilesystem struct {
	targetFS afero.Fs
	hostFS   afero.Fs
}

func NewAferoTargetFilesystem(targetFS afero.Fs) TargetFilesystem {
	return NewAferoTargetFilesystemWithHostFS(targetFS, afero.NewOsFs())
}

func NewAferoTargetFilesystemWithHostFS(targetFS afero.Fs, hostFS afero.Fs) TargetFilesystem {
	return &aferoTargetFilesystem{
		targetFS: targetFS,
		hostFS:   hostFS,
	}
}

func (fs *aferoTargetFilesystem) FileStat(path string) (os.FileInfo, error) {
	return fs.targetFS.Stat(path)
}

func (fs *aferoTargetFilesystem) CreateDirTree(path string, perm os.FileMode) error {
	return fs.targetFS.MkdirAll(path, perm)
}

func (fs *aferoTargetFilesystem) RemoveDirTree(path string) error {
	return aferoutils.RemoveAll(fs.targetFS, path)
}

func (fs *aferoTargetFilesystem) CreateEmptyFile(path string, perm os.FileMode) error {
	return afero.WriteFile(fs.targetFS, path, []byte{}, perm)
}

func (fs *aferoTargetFilesystem) CopyFromHost(hostPath string, targetPath string, progress []ReportProgressRequest) error {
	return CopyFile(hostPath, fs.hostFS, targetPath, fs.targetFS, progress)
}

type SFTPTargetFilesystem struct {
	*aferoTargetFilesystem
	sftpClient *sftp.Client
}

func NewSFTPTargetFilesystem(client *sftp.Client) TargetFilesystem {
	return &SFTPTargetFilesystem{
		aferoTargetFilesystem: &aferoTargetFilesystem{
			targetFS: sftpfs.New(client),
			hostFS:   afero.NewOsFs(),
		},
		sftpClient: client,
	}
}

func (fs *SFTPTargetFilesystem) CopyFromHost(hostPath string, targetPath string, progress []ReportProgressRequest) error {
	return CopyFileSFTP(hostPath, fs.hostFS, targetPath, fs.targetFS, fs.sftpClient, progress)
}

type targetFileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
}

func (fi targetFileInfo) Name() string {
	return fi.name
}

func (fi targetFileInfo) Size() int64 {
	return fi.size
}

func (fi targetFileInfo) Mode() os.FileMode {
	return fi.mode
}

func (fi targetFileInfo) ModTime() time.Time {
	return fi.modTime
}

func (fi targetFileInfo) IsDir() bool {
	return fi.mode.IsDir()
}

func (fi targetFileInfo) Sys() any {
	return nil
}

func newTargetFileInfo(path string, size int64, mode os.FileMode) os.FileInfo {
	return targetFileInfo{
		name: filepath.Base(path),
		size: size,
		mode: mode,
	}
}
