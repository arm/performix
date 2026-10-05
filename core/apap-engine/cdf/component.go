// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package cdf

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/klauspost/compress/zstd"
)

type ComponentResolver interface {
	ResolveComponent(relativePath string) (Component, error)
}

// ComponentType represents the type of a Component, in terms of name and version.
type ComponentType struct {
	Name          string `json:"name"`
	SchemaVersion string `json:"schema_version"`
}

// Component represents a Component within a OnDiskModel.
type Component struct {
	Type         ComponentType
	RelativePath string
	AbsolutePath string
	Compressed   bool
}

// GlobPath appends a suffix to the component path while preserving the compression suffix as the final extension used for format auto-detection.
// Example: Component{AbsolutePath: "output/data.csv.zst", Compressed: true}.GlobPath("-*.csv") returns "output/data.csv-*.csv.zst".
func (c Component) GlobPath(suffix string) string {
	path := c.AbsolutePath
	if c.Compressed {
		path = strings.TrimSuffix(path, ZstdSuffix)
		return path + suffix + ZstdSuffix
	}
	return path + suffix
}

// Open opens the literal AbsolutePath, transparently decompressing compressed components.
// It does not expand glob patterns; callers must resolve individual files first.
func (c Component) Open() (io.ReadCloser, error) {
	file, err := os.Open(c.AbsolutePath)
	if err != nil {
		return nil, err
	}
	if !c.Compressed {
		return file, nil
	}

	decoder, err := zstd.NewReader(file)
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("open compressed component %q: %w", c.RelativePath, err)
	}
	return &compressedComponentReader{Decoder: decoder, file: file}, nil
}

// ReadAll reads and closes a component, transparently decompressing it when required.
func (c Component) ReadAll() ([]byte, error) {
	reader, err := c.Open()
	if err != nil {
		return nil, err
	}
	contents, err := io.ReadAll(reader)
	closeErr := reader.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return contents, nil
}

type compressedComponentReader struct {
	*zstd.Decoder
	file *os.File
}

func (r *compressedComponentReader) Close() error {
	r.Decoder.Close()
	return r.file.Close()
}
