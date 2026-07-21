// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package terminology

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed terminology.json
var rawTerms []byte
var terms terminology

type terminology struct {
	ProductFullName   string `json:"PRODUCT_FULL_NAME"`
	ProductBinaryName string `json:"PRODUCT_BINARY_NAME"`
	AgentBinaryName   string `json:"AGENT_BINARY_NAME"`
	DaemonDirName     string `json:"DAEMON_DIR_NAME"`
	EnvVarPrefix      string `json:"ENV_VAR_PREFIX"`
}

func init() {
	setTerms()
}

func setTerms() {
	if err := json.Unmarshal(rawTerms, &terms); err != nil {
		panic(fmt.Sprintf("failed to parse terminology.json file: %v", err))
	}
}

// GetProductFullName returns the full name of the product
func GetProductFullName() string {
	return terms.ProductFullName
}

// GetProductBinaryName returns the name of the product binary
func GetProductBinaryName() string {
	return terms.ProductBinaryName
}

// GetAgentBinaryName returns the name of the agent binary
func GetAgentBinaryName() string {
	return terms.AgentBinaryName
}

// GetDaemonDirName returns the name of the daemon directory
func GetDaemonDirName() string {
	return terms.DaemonDirName
}

// GetEnvVarPrefix returns the prefix to each environment variable name
func GetEnvVarPrefix() string {
	return terms.EnvVarPrefix
}
