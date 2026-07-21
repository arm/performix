// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package daemon

import (
	"io"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
)

func assertExecutedInLessThan(t testing.TB, f func(), timeout time.Duration) {
	t.Helper() // Makes stack trace more accurate
	done := make(chan bool, 1)
	go func() {
		f()
		done <- true
	}()
	select {
	case <-done:
		return
	case <-time.After(timeout):
		t.Fatalf("Function did not complete executing within %v timeout", timeout)
	}
}

func setLogOutputAndLevel(output io.Writer, level log.Level) (restoreLog func()) {
	oldLevel := log.GetLevel()
	oldOutput := log.StandardLogger().Out
	log.SetOutput(output)
	log.SetLevel(level)
	return func() {
		log.SetLevel(oldLevel)
		log.SetOutput(oldOutput)
	}
}
