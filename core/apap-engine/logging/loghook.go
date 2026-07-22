// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package logging

import (
	"bytes"
	"fmt"
	"os"
	"sync"

	log "github.com/sirupsen/logrus"

	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
)

// DeferredFileOpenLogHook is a log hook that is intended to write to file in situations where you need to create the
// hook and begin capturing logs before the file can be opened (because, for instance, the file open / creation is part
// of the process that is being logged). Once the file is ready to be opened, SwapToFile can be called to open the file
// and begin logging to it.
type DeferredFileOpenLogHook struct {
	file           *os.File
	filename       string
	formatter      log.Formatter
	pool           *sync.Pool
	initialEntries []*log.Entry
	isOpened       bool
	mu             sync.Mutex
}

// NewDeferredFileOpenLogHook creates a new instance of DeferredFileOpenLogHook with the specified log.Formatter,
// which will be used to format entries written to file. Note: Caller must call Close() on the returned log hook
// to close the underlying file.
func NewDeferredFileOpenLogHook(formatter log.Formatter) *DeferredFileOpenLogHook {
	return &DeferredFileOpenLogHook{formatter: formatter}
}

// SwapToFile changes the output destination to the specified file.
// Any log entries that were written to the hook before this function was called will be written to the file on
// successful open. On failure, an error is returned. This function is thread safe.
func (h *DeferredFileOpenLogHook) SwapToFile(filename string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.isOpened {
		if h.filename != filename {
			return fmt.Errorf("cannot open log file '%s': log already opened with a different filename '%s'", filename, h.filename)
		}
		return nil
	}

	var err error
	h.file, err = os.OpenFile(filename, os.O_CREATE|os.O_WRONLY, perms.LocalFilePerm)
	if err != nil {
		return fmt.Errorf("failed to swap to log file '%s': %w", filename, err)
	}

	h.pool = &sync.Pool{
		New: func() interface{} {
			return new(bytes.Buffer)
		},
	}

	// Drain initials
	for _, e := range h.initialEntries {
		if err := h.writeUnprotected(e); err != nil {
			return err
		}
	}

	h.initialEntries = []*log.Entry{}
	h.filename = filename
	h.isOpened = true

	return nil
}

func (h *DeferredFileOpenLogHook) Levels() []log.Level {
	// Hook all levels
	return []log.Level{
		log.PanicLevel,
		log.FatalLevel,
		log.ErrorLevel,
		log.WarnLevel,
		log.InfoLevel,
		log.DebugLevel,
		log.TraceLevel,
	}
}

func (h *DeferredFileOpenLogHook) writeUnprotected(entry *log.Entry) error {
	if h.file == nil {
		return nil
	}

	// This code follows the pattern from log.Entry.log: grab a new buffer from the pool,
	// set it on the Entry, invoke the Formatter, then reset the Entry's buffer to whatever
	// it was before. Finally, we can write the bytes to the output and release the buffer
	// back to the pool
	buf := h.pool.Get().(*bytes.Buffer)
	oldBuf := entry.Buffer
	entry.Buffer = buf
	defer func() {
		buf.Reset()
		entry.Buffer = oldBuf
		h.pool.Put(buf)
	}()

	byteArray, err := h.formatter.Format(entry)
	if err != nil {
		return err
	}

	_, err = h.file.Write(byteArray)
	if err != nil {
		return err
	}

	return nil
}

func (h *DeferredFileOpenLogHook) Fire(entry *log.Entry) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.isOpened {
		return h.writeUnprotected(entry)
	} else {
		h.initialEntries = append(h.initialEntries, entry)
		return nil
	}
}

func (h *DeferredFileOpenLogHook) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.file != nil {
		return h.file.Close()
	}
	return nil
}
