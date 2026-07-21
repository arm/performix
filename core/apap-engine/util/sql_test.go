// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package util

import "testing"

func TestSQLQuoteStringLiteral(t *testing.T) {
	if got := SQLQuoteStringLiteral("plain"); got != "'plain'" {
		t.Fatalf("SQLQuoteStringLiteral(plain) = %q", got)
	}

	if got := SQLQuoteStringLiteral("O'Reilly"); got != "'O''Reilly'" {
		t.Fatalf("SQLQuoteStringLiteral with embedded quote = %q", got)
	}
}

func TestSQLStringListLiteral(t *testing.T) {
	if got := SQLStringListLiteral([]string{"alpha", "beta"}); got != "['alpha', 'beta']" {
		t.Fatalf("SQLStringListLiteral(simple) = %q", got)
	}

	if got := SQLStringListLiteral([]string{"O'Reilly", "plain"}); got != "['O''Reilly', 'plain']" {
		t.Fatalf("SQLStringListLiteral(with embedded quote) = %q", got)
	}
}
