// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type somethingWithTime struct {
	Timestamp UTCRFC3339Time `json:"timestamp"`
}

func TestUTCRFC3339Time(t *testing.T) {
	t.Run("marshal and unmarshal work", func(t *testing.T) {
		now := time.Now().UTC().Truncate(time.Second)
		obj := somethingWithTime{UTCRFC3339Time(now)}

		marshalled, err := json.Marshal(obj)
		assert.NoError(t, err)
		if err != nil {
			return
		}

		var unmarshalled somethingWithTime
		err = json.Unmarshal(marshalled, &unmarshalled)
		assert.NoError(t, err)
		if err != nil {
			return
		}

		assert.Equal(t, obj, unmarshalled)
	})
}
