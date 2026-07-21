// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package conductor

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/pkg/sftp"
	"github.com/spf13/afero"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
)

type ReportProgress func(received int64, max int64, when time.Time)

type ReportProgressRequest struct {
	Callback ReportProgress
	Interval time.Duration
}

type ProgressReadIntercept struct {
	src         io.Reader
	transferred int64
	size        int64
	lastUpdates []time.Time
	progUpdates []ReportProgressRequest
}

func (s *ProgressReadIntercept) Read(p []byte) (n int, err error) {
	n, err = s.src.Read(p)
	now := time.Now()

	s.transferred += int64(n)
	for i, update := range s.progUpdates {
		if s.transferred >= s.size || now.Sub(s.lastUpdates[i]) > s.progUpdates[i].Interval {
			update.Callback(s.transferred, s.size, now)
			s.lastUpdates[i] = now
		}
	}

	return n, err
}

func newProgressReadIntercept(src io.Reader, size int64, progUpdates []ReportProgressRequest) ProgressReadIntercept {
	return ProgressReadIntercept{
		src:         src,
		size:        size,
		lastUpdates: make([]time.Time, len(progUpdates)),
		progUpdates: progUpdates,
	}
}

// This is the permission level neccessary to copy files
// Given we might be transfering binary executables, we use 744 to allow the owner to execute
const copiedFilePerm os.FileMode = 0o744

func CopyFile(srcFilePath string, srcFS afero.Fs, dstFilePath string, destFS afero.Fs, progUpdates []ReportProgressRequest) error {
	normalisedDstFilePath := filepath.ToSlash(dstFilePath)
	normalisedSrcFilePath := filepath.ToSlash(srcFilePath)
	metadata := map[string]string{
		"srcFilePath": normalisedSrcFilePath,
		"dstFilePath": normalisedDstFilePath,
	}

	dstFile, err := destFS.OpenFile(normalisedDstFilePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, copiedFilePerm)
	if err != nil {
		exists, _ := afero.Exists(destFS, normalisedDstFilePath)
		if exists {
			return fmt.Errorf("destination file %s already exists. Will not move file", normalisedDstFilePath)
		} else {
			metadata["parentDir"] = filepath.Dir(normalisedDstFilePath)
			return message.New(message.EngineConductorFileTransferCreateDestFile).WithMetadata(metadata).WithCause(err)
		}
	}
	defer dstFile.Close()

	srcFile, err := srcFS.Open(normalisedSrcFilePath)
	if err != nil {
		return message.New(message.EngineConductorFileTransferOpenSrcFile).WithMetadata(metadata).WithCause(err)
	}
	defer srcFile.Close()

	fi, err := srcFile.Stat()
	if err != nil {
		return message.New(message.CommonUnknownError).WithMetadata(metadata).WithCause(err)
	}

	srcReader := &ProgressReadIntercept{
		src:         srcFile,
		size:        fi.Size(),
		lastUpdates: make([]time.Time, len(progUpdates)),
		progUpdates: progUpdates,
	}

	if _, err = io.Copy(dstFile, srcReader); err != nil {
		return message.New(message.EngineConductorFileTransferCopyContents).
			WithMetadata(metadata).
			WithCause(err)
	}

	return nil
}

func CopyFileSFTP(srcFilePath string, srcFS afero.Fs, dstFilePath string, destFS afero.Fs, sftpClient *sftp.Client, progUpdates []ReportProgressRequest) error {
	normalisedDstFilePath := filepath.ToSlash(dstFilePath)
	normalisedSrcFilePath := filepath.ToSlash(srcFilePath)
	metadata := map[string]string{
		"srcFilePath": normalisedSrcFilePath,
		"dstFilePath": normalisedDstFilePath,
	}

	srcFile, err := srcFS.Open(normalisedSrcFilePath)
	if err != nil {
		return message.New(message.EngineConductorFileTransferOpenSrcFile).WithMetadata(metadata).WithCause(err)
	}
	defer srcFile.Close()

	dstFile, err := sftpClient.OpenFile(normalisedDstFilePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY)
	if err != nil {
		exists, _ := afero.Exists(destFS, normalisedDstFilePath)
		if exists {
			return fmt.Errorf("destination file %s already exists. Will not move file", normalisedDstFilePath)
		} else {
			metadata["parentDir"] = filepath.Dir(normalisedDstFilePath)
			return message.New(message.EngineConductorFileTransferCreateDestFile).WithMetadata(metadata).WithCause(err)
		}
	}
	defer dstFile.Close()

	if err = dstFile.Chmod(copiedFilePerm); err != nil {
		return message.New(message.EngineConductorFileTransferCreateDestFile).
			WithMetadata(metadata).
			WithCause(err)
	}

	fi, err := srcFile.Stat()
	if err != nil {
		return message.New(message.CommonUnknownError).WithMetadata(metadata).WithCause(err)
	}

	srcReader := newProgressReadIntercept(srcFile, int64(fi.Size()), progUpdates)

	if _, err = dstFile.ReadFromWithConcurrency(&srcReader, 0); err != nil {
		return message.New(message.EngineConductorFileTransferCopyContents).
			WithMetadata(metadata).
			WithCause(err)
	}

	return nil
}
