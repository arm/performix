// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package main

import (
	log "github.com/sirupsen/logrus"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd"
)

func init() {
	// Use full timestamp in log messages
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp: true,
	})
	// Log infos and above.
	log.SetLevel(log.InfoLevel)
}

func main() {
	rootCmd := cmd.NewRootCmd()
	cmd.Execute(rootCmd)
}
