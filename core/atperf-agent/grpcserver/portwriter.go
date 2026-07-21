// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	log "github.com/sirupsen/logrus"

	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
)

type PortWriter interface {
	Write(port int) error
	Remove() error
}

// CompositePortWriter fans out to multiple writers
type CompositePortWriter struct {
	Writers []PortWriter
}

// Write calls each PortWriter's Write function, joining and returning any errors
func (c *CompositePortWriter) Write(port int) error {
	var errs error
	for _, w := range c.Writers {
		if w == nil {
			continue
		}
		if err := w.Write(port); err != nil {
			errs = errors.Join(errs, err)
		}
	}
	return errs
}

// Remove calls each PortWriter's Remove function, joining and returning any errors
func (c *CompositePortWriter) Remove() error {
	var errs error
	for _, w := range c.Writers {
		if w == nil {
			continue
		}
		if err := w.Remove(); err != nil {
			errs = errors.Join(errs, err)
		}
	}
	return errs
}

// FilePortWriter writes the port number to the given file
type FilePortWriter struct {
	filename string
	rename   func(oldpath, newpath string) error // Inject rename for testing
}

// NewFilePortWriter builds a FilePortWriter with the current pid.
func NewFilePortWriter(portFileDir string) PortWriter {
	if portFileDir == "" {
		portFileDir = os.TempDir()
	}
	pid := os.Getpid()
	return &FilePortWriter{
		filename: filepath.Join(portFileDir, fmt.Sprintf("%v_%d.port", terminology.GetAgentBinaryName(), pid)),
		rename:   os.Rename,
	}
}

// Write atomically writes the port number to filename
func (w *FilePortWriter) Write(port int) error {
	dir := filepath.Dir(w.filename)
	base := filepath.Base(w.filename)

	// Create a unique temp file in the same dir
	tf, err := os.CreateTemp(dir, base+".tmp-*")
	if err != nil {
		return err
	}
	tmpname := tf.Name()

	cleanup := func(e error) error {
		_ = tf.Close()
		_ = os.Remove(tmpname)
		return e
	}

	// Set file permissions
	if err := tf.Chmod(perms.TargetAgentSocketPerm); err != nil {
		return cleanup(err)
	}

	// Write port number
	if _, err := tf.WriteString(strconv.Itoa(port) + "\n"); err != nil {
		return cleanup(err)
	}

	// Ensure data is on disk
	if err := tf.Sync(); err != nil {
		return cleanup(err)
	}
	if err := tf.Close(); err != nil {
		_ = os.Remove(tmpname)
		return err
	}

	// Atomic rename into place
	if err := w.rename(tmpname, w.filename); err != nil {
		_ = os.Remove(tmpname)
		return err
	}

	return nil
}

// Remove removes the filename file
func (w *FilePortWriter) Remove() error {
	// ignore missing‐file errors
	if err := os.Remove(w.filename); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// LoggingPortWriter logs the port number
type LoggingPortWriter struct {
}

// NewLoggingPortWriter returns a logging port writer
func NewLoggingPortWriter() PortWriter { return &LoggingPortWriter{} }

func (l *LoggingPortWriter) Write(port int) error {
	log.WithField("port", port).Info("Server port chosen")
	return nil
}

func (l *LoggingPortWriter) Remove() error { return nil }

// TerminalPortWriter writes the port to the specified io writer (defaults to stdout)
type TerminalPortWriter struct {
	Out io.Writer
}

// NewTerminalPortWriter returns a terminal port writer
func NewTerminalPortWriter() PortWriter { return &TerminalPortWriter{Out: os.Stdout} }

func (t *TerminalPortWriter) Write(port int) error {
	w := t.Out
	if w == nil {
		w = os.Stdout
	}
	_, err := fmt.Fprintf(w, "%d\n", port)
	return err
}

func (t *TerminalPortWriter) Remove() error { return nil }

// NullPortWriter does nothing
type NullPortWriter struct{}

// NewNullPortWriter returns a null port writer
func NewNullPortWriter() PortWriter { return &NullPortWriter{} }

func (n *NullPortWriter) Write(port int) error { return nil }
func (n *NullPortWriter) Remove() error        { return nil }

// WrapPortWriter returns a null port writer if the input is nil, otherwise returns the input
func WrapPortWriter(pw PortWriter) PortWriter {
	if pw != nil {
		return pw
	}
	return NewNullPortWriter()
}
