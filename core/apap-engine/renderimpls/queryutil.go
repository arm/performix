// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import "strings"

// QuoteColumnName quotes a column name for use in SQL queries, escaping any double quotes within the identifier.
func QuoteColumnName(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
