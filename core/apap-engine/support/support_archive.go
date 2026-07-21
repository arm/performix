// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package support

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

// createArchive creates an archive of the given directory. If an archive with the same name already exists in the
// output directory, it appends a number to the archive name to avoid overwriting.
func createArchive(ctx context.Context, sourceDir, outputDir, archiveBase string) (string, error) {
	if outputDir == "" {
		outputDir = "."
	}
	dest := filepath.Join(outputDir, fmt.Sprintf("%s.zip", archiveBase))

	for i := 1; ; i++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if _, err := os.Stat(dest); errors.Is(err, fs.ErrNotExist) {
			break
		} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("failed to check if archive %s already exists: %w", dest, err)
		}
		dest = filepath.Join(outputDir, fmt.Sprintf("%s_%d.zip", archiveBase, i))
	}

	tmpFile, err := os.CreateTemp(outputDir, archiveBase+"-*.zip.tmp")
	if err != nil {
		return "", fmt.Errorf("failed to create temporary archive in %s: %w", outputDir, err)
	}
	tmpPath := tmpFile.Name()
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("failed to close temporary archive %s: %w", tmpPath, err)
	}
	defer func() { _ = os.Remove(tmpPath) }()

	if err := zipDir(ctx, sourceDir, tmpPath); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := os.Rename(tmpPath, dest); err != nil {
		return "", fmt.Errorf("failed to move support package archive to %s: %w", dest, err)
	}
	return dest, nil
}

// copyFile copies a file from src to dst. If the destination file exists, it will be overwritten.
func copyFile(ctx context.Context, src, dst string) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(dst), perms.LocalDirPerm); err != nil {
		return
	}

	srcFile, err := os.OpenFile(src, os.O_RDONLY, 0)
	if err != nil {
		return
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return
	}

	if err := dstFile.Chmod(perms.LocalFilePerm); err != nil {
		_ = dstFile.Close()
		return err
	}

	if _, err := util.CopyWithContext(ctx, dstFile, srcFile); err != nil {
		_ = dstFile.Close()
		_ = os.Remove(dst)
		return err
	}

	return dstFile.Close()
}

// zipDir creates a zip archive of the source directory at the destination path.
func zipDir(ctx context.Context, src, dest string) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), perms.LocalDirPerm); err != nil {
		return fmt.Errorf("failed to create directory for archive %s: %w", dest, err)
	}

	zipFile, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("failed to create file for archive %s: %w", dest, err)
	}
	defer func() {
		_ = zipFile.Close()
		if err != nil {
			_ = os.Remove(dest)
		}
	}()

	zipWriter := zip.NewWriter(zipFile)

	base := filepath.Base(src)
	err = filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		zipPath := filepath.ToSlash(filepath.Join(base, rel))
		if d.IsDir() {
			_, err := zipWriter.Create(strings.TrimSuffix(zipPath, "/") + "/")
			return err
		}

		info, err := os.Stat(path)
		if err != nil {
			return err
		}

		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = zipPath
		header.Method = zip.Deflate

		writer, err := zipWriter.CreateHeader(header)
		if err != nil {
			return err
		}

		f, err := os.Open(path) //nolint:gosec // path comes from filepath.WalkDir over the support package directory.
		if err != nil {
			return err
		}

		if _, err := util.CopyWithContext(ctx, writer, f); err != nil {
			_ = f.Close()
			return err
		}

		return f.Close()
	})
	if err != nil {
		return fmt.Errorf("failed to create zip archive %s: %w", dest, err)
	}
	if err = zipWriter.Close(); err != nil {
		return fmt.Errorf("failed to close zip archive %s: %w", dest, err)
	}
	if err = zipFile.Close(); err != nil {
		return fmt.Errorf("failed to close archive file %s: %w", dest, err)
	}
	return nil
}
