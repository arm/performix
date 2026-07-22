// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package transport

import (
	"fmt"
	"io"

	"github.com/sirupsen/logrus"
)

// loggingTransport wraps a Transport and logs all read and write operations.
type loggingTransport struct {
	Transport
	logger *logrus.Entry
}

// NewLoggingTransport creates a new loggingTransport. If the provided logger is nil, it defaults to the standard logger.
func NewLoggingTransport(base Transport, logger *logrus.Logger) Transport {
	if logger == nil {
		logger = logrus.StandardLogger()
	}

	xport := &loggingTransport{Transport: base}

	xport.logger = logger.
		WithField("component", "LoggingTransport").
		WithField("ptr", fmt.Sprintf("%p", xport))

	return xport
}

// Read logs the read operation and delegates to the underlying Transport.
func (lt *loggingTransport) Read(p []byte) (n int, err error) {
	n, err = lt.Transport.Read(p)
	if err != nil && err != io.EOF {
		lt.logger.WithError(err).Error("Read error")
	} else {
		lt.logger.Infof("Read %d bytes", n)
	}
	return
}

// Write logs the write operation and delegates to the underlying Transport.
func (lt *loggingTransport) Write(p []byte) (n int, err error) {
	n, err = lt.Transport.Write(p)
	if err != nil {
		lt.logger.WithError(err).Error("Write error")
	} else {
		lt.logger.Infof("Wrote %d bytes", n)
	}
	return
}

// Close logs the close operation and delegates to the underlying Transport.
func (lt *loggingTransport) Close() error {
	err := lt.Transport.Close()
	if err != nil {
		lt.logger.WithError(err).Error("Close error")
	} else {
		lt.logger.Info("Transport closed")
	}
	return err
}
