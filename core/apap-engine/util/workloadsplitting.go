// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package util

func WorkloadStringToSlice(w string) []string {
	args := []string{}
	var buf []rune
	var tokenStarted bool
	inQuote := false
	var quoteChar rune
	quoteStartLen := 0

	runes := []rune(w)
	for i := 0; i < len(runes); i++ {
		r := runes[i]

		if inQuote {
			if r == quoteChar {
				inQuote = false
				tokenStarted = true
				continue
			}
			if r == '\\' && quoteChar == '"' {
				if i+1 < len(runes) {
					next := runes[i+1]
					if next == '\\' || next == '"' || next == '$' || next == '`' || next == '\n' {
						i++
						if next != '\n' {
							buf = append(buf, next)
						}
					} else {
						buf = append(buf, r, next)
						i++
					}
					tokenStarted = true
					continue
				}
				buf = append(buf, r)
				tokenStarted = true
				continue
			}
			buf = append(buf, r)
			tokenStarted = true
			continue
		}

		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if tokenStarted {
				args = append(args, string(buf))
				buf = buf[:0]
				tokenStarted = false
			}
			continue
		}

		if r == '\'' || r == '"' {
			inQuote = true
			quoteChar = r
			quoteStartLen = len(buf)
			tokenStarted = true
			continue
		}

		if r == '\\' {
			if i+1 < len(runes) {
				next := runes[i+1]
				i++
				if next != '\n' {
					buf = append(buf, next)
				}
				tokenStarted = true
				continue
			}
			buf = append(buf, r)
			tokenStarted = true
			continue
		}

		buf = append(buf, r)
		tokenStarted = true
	}

	if inQuote {
		if quoteStartLen < 0 || quoteStartLen > len(buf) {
			quoteStartLen = len(buf)
		}
		buf = append(buf, 0)
		copy(buf[quoteStartLen+1:], buf[quoteStartLen:])
		buf[quoteStartLen] = quoteChar
	}

	if tokenStarted {
		args = append(args, string(buf))
	}

	return args
}
