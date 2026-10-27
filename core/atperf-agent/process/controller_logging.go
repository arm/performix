// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package process

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	log "github.com/sirupsen/logrus"
)

type controllerLogPipe struct {
	reader, writer *os.File
	done           chan struct{}
}

func newControllerLogPipe() (*controllerLogPipe, error) {
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("create controller log pipe: %w", err)
	}
	return &controllerLogPipe{reader: reader, writer: writer}, nil
}

func (p *controllerLogPipe) start(pid int) {
	_ = p.writer.Close() // Only the controller should keep the write end open.
	p.done = make(chan struct{})
	go func() {
		defer close(p.done)
		defer p.reader.Close()
		decoder := json.NewDecoder(p.reader)
		for {
			var record log.Fields
			if err := decoder.Decode(&record); err != nil {
				if err != io.EOF && !errors.Is(err, os.ErrClosed) {
					log.WithError(err).WithField("controller_pid", pid).Warn("failed to read controller diagnostics")
				}
				return
			}
			levelName, _ := record["level"].(string)
			level, err := log.ParseLevel(levelName)
			if err != nil {
				level = log.WarnLevel
			}
			if level < log.ErrorLevel {
				level = log.ErrorLevel // A child diagnostic must never panic or exit the agent.
			}
			message, _ := record["msg"].(string)
			delete(record, "level")
			delete(record, "msg")
			record["controller_time"] = record["time"]
			delete(record, "time")
			record["controller_pid"] = pid
			log.WithFields(record).Log(level, message)
		}
	}()
}

func (p *controllerLogPipe) close() {
	_ = p.writer.Close()
	_ = p.reader.Close()
	if p.done != nil {
		<-p.done
	}
}
