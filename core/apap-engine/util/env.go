// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cast"

	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
)

func GetEnvBool(key string) bool {
	value, present := os.LookupEnv(key)

	if !present {
		return false
	}

	return cast.ToBool(value)
}

func ApplyEnvPrefix(suffix string) string {
	return fmt.Sprintf("%v_%v", terminology.GetEnvVarPrefix(), suffix)
}

var EnvVarReplacer = strings.NewReplacer("-", "_")
