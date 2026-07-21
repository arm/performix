// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package aferoutils

import "github.com/spf13/afero"

// RemoveAll delete files recursively in the directory and Recursively delete subdirectories.
// An error will be returned if no file or directory with the specified path exists
//
// The existing afero.Fs.RemoveAll implementation for sftp is incomplete, there's an open PR to resolve this:
// https://github.com/spf13/afero/pull/348
//
// Current code implementation does not work with deleting symlinks if they don't point to a valid file.

func RemoveAll(fs afero.Fs, path string) error {

	// Get the file/directory information
	fi, err := fs.Stat(path)
	if err != nil {
		return err
	}

	if fi.IsDir() {
		// Delete files recursively in the directory
		files, err := afero.ReadDir(fs, path)
		if err != nil {
			return err
		}

		for _, file := range files {
			if file.IsDir() {
				// Recursively delete subdirectories
				err = RemoveAll(fs, path+"/"+file.Name())
				if err != nil {
					return err
				}
			} else {
				// Delete individual files
				err = RemoveAll(fs, path+"/"+file.Name())
				if err != nil {
					return err
				}
			}
		}

	}

	return fs.Remove(path)

}
