// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package serverconfig

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
)

func TestValidatePortAllocsMetadataOnFailure(t *testing.T) {
	const portName = "test-port"
	invalidPort := minPort - 1

	err := ValidatePort(portName, invalidPort)
	require.Error(t, err)

	var msg message.Message
	require.True(t, errors.As(err, &msg), "error should be a message.Message")
	require.Equal(t, map[string]string{
		"name":     portName,
		"min":      fmt.Sprint(minPort),
		"max":      fmt.Sprint(maxPort),
		"provided": fmt.Sprint(invalidPort),
	}, msg.Metadata())
}

func TestValidatePortAcceptsValuesWithinRange(t *testing.T) {
	require.NoError(t, ValidatePort("server", minPort))
	require.NoError(t, ValidatePort("server", maxPort))
}
