// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package message

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateMetadataPlaceholders(t *testing.T) {
	t.Run("returns nil when provided error is not a Message", func(t *testing.T) {
		msg := errors.New("some text")
		err := ValidateMetadataPlaceholders(msg)
		assert.NoError(t, err)
	})

	t.Run("returns nil when metadata covers all placeholders", func(t *testing.T) {
		msg := New(CliCmdValidationInvalidFlagValue).WithMetadata(map[string]string{
			"flag":  "--target",
			"value": "abc",
		})
		err := ValidateMetadataPlaceholders(msg)
		assert.NoError(t, err)
	})

	t.Run("reports missing placeholders", func(t *testing.T) {
		msg := New(CliCmdValidationInvalidFlagValue).WithMetadata(map[string]string{
			"flag": "--target",
		})
		err := ValidateMetadataPlaceholders(msg)
		assert.ErrorContains(t, err, CliCmdValidationInvalidFlagValue)
		assert.ErrorContains(t, err, "value")
	})

	t.Run("ignores extra metadata entries", func(t *testing.T) {
		msg := New(CliCmdValidationInvalidFlagValue).WithMetadata(map[string]string{
			"flag":   "--target",
			"value":  "abc",
			"extra":  "ignored",
			"extra2": "",
		})
		err := ValidateMetadataPlaceholders(msg)
		assert.NoError(t, err)
	})

	t.Run("ignores duplicate missing placeholders", func(t *testing.T) {
		msg := New(CliCmdValidationInvalidFlagValue).WithMetadata(map[string]string{
			"value": "abc",
		})
		err := ValidateMetadataPlaceholders(msg)
		assert.ErrorContains(t, err, CliCmdValidationInvalidFlagValue)
		assert.ErrorContains(t, err, "flag")
		assert.NotContains(t, err.Error(), "flag, flag")
	})
}
