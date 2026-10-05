// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
)

func coreTypeUnavailableError(coreType string, cause error) error {
	return message.New(message.EngineRenderCoreTypeFilterCoreTypeUnavailable).
		WithMetadata(map[string]string{"coreType": coreType}).
		WithCause(cause)
}

// resolveCoreNumbers maps a collected CPU name to its OS core numbers.
func resolveCoreNumbers(
	ctx context.Context,
	conn *sql.Conn,
	targetInfoTable string,
	coreType string,
) ([]int, error) {
	rows, err := conn.QueryContext(
		ctx,
		fmt.Sprintf(
			"SELECT DISTINCT core_number FROM %s WHERE name = ? ORDER BY core_number",
			targetInfoTable,
		),
		coreType,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query target CPU information from table '%s': %w", targetInfoTable, err)
	}
	defer rows.Close()

	coreNumbers := make([]int, 0)
	for rows.Next() {
		var coreNumber int
		if err := rows.Scan(&coreNumber); err != nil {
			return nil, fmt.Errorf("failed to read target CPU information for core type '%s': %w", coreType, err)
		}
		coreNumbers = append(coreNumbers, coreNumber)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate target CPU information for core type '%s': %w", coreType, err)
	}
	if len(coreNumbers) == 0 {
		return nil, fmt.Errorf("core type '%s' not found", coreType)
	}

	return coreNumbers, nil
}

// resolveTelemetryCPUName preserves the existing first-CPU fallback when no
// explicit type is configured, while allowing filtered renders to select the
// matching telemetry methodology.
func resolveTelemetryCPUName(
	ctx context.Context,
	conn *sql.Conn,
	targetInfoTable string,
	configuredCPUName string,
) (string, error) {
	if configuredCPUName != "" {
		return configuredCPUName, nil
	}

	// targetInfoTable is an engine-generated render-manifest table name, not user input.
	query := fmt.Sprintf("SELECT name FROM %s LIMIT 1", targetInfoTable) //nolint:gosec

	var cpuName string
	if err := conn.QueryRowContext(ctx, query).Scan(&cpuName); err != nil {
		return "", fmt.Errorf("failed to query target CPU name from table '%s': %w", targetInfoTable, err)
	}
	return cpuName, nil
}
