// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"encoding/json"
	"time"
)

type UTCRFC3339Time time.Time

// Return the current time in a uniform way.
func CurrentTime() UTCRFC3339Time {
	return UTCRFC3339Time(time.Now().UTC())
}

// Return the zero value time, representing an invalid time
func InvalidTime() UTCRFC3339Time {
	return UTCRFC3339Time(time.Time{})
}

func (t UTCRFC3339Time) MarshalJSON() ([]byte, error) {
	str := t.ToFormattedString()
	return json.Marshal(str)
}

func (t *UTCRFC3339Time) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	value, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return err
	}
	*t = UTCRFC3339Time(value)
	return nil
}

func (t UTCRFC3339Time) ToFormattedString() string {
	return time.Time(t).UTC().Format(time.RFC3339)
}
