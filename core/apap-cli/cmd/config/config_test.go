// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"bytes"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

func TestViperBindPFlag(t *testing.T) {
	var pFlagVar, flagVar string
	NewCmd := &cobra.Command{
		Use: "command",
	}
	NewCmd.PersistentFlags().StringVar(&pFlagVar, "persistent-flag", "default", "A persistent flag")
	NewCmd.Flags().StringVar(&flagVar, "non-persistent-flag", "default", "A non-persistent flag")

	t.Run("an error is logged if an unknown flag is provided", func(t *testing.T) {
		var buf bytes.Buffer
		log.SetOutput(&buf)

		ViperBindPFlag(NewCmd, "unknown-flag", false)
		assert.Contains(t, buf.String(), "Unable to bind configuration for `unknown-flag`, values set using files or env vars might be incorrect.")
	})

	t.Run("an error is logged if an persistent flag is provided as a normal flag", func(t *testing.T) {
		var buf bytes.Buffer
		log.SetOutput(&buf)

		ViperBindPFlag(NewCmd, "persistent-flag", false)
		assert.Contains(t, buf.String(), "Unable to bind configuration for `persistent-flag`, values set using files or env vars might be incorrect.")
	})

	t.Run("an error is logged if an unknown flag is provided", func(t *testing.T) {
		var buf bytes.Buffer
		log.SetOutput(&buf)

		ViperBindPFlag(NewCmd, "non-persistent-flag", true)
		assert.Contains(t, buf.String(), "Unable to bind configuration for `non-persistent-flag`, values set using files or env vars might be incorrect.")
	})

	t.Run("no error when a known persistent flag is provided", func(t *testing.T) {
		var buf bytes.Buffer
		log.SetOutput(&buf)

		ViperBindPFlag(NewCmd, "persistent-flag", true)
		assert.Empty(t, buf.String())
	})

	t.Run("no error when a known non-persistent flag is provided", func(t *testing.T) {
		var buf bytes.Buffer
		log.SetOutput(&buf)

		ViperBindPFlag(NewCmd, "non-persistent-flag", false)
		assert.Empty(t, buf.String())
	})
}
