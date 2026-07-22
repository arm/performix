// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWorkloadStringToSlice(t *testing.T) {
	testCases := []struct {
		name     string
		wl       string
		expected []string
	}{
		{
			name:     "Empty string yields empty slice",
			wl:       "",
			expected: []string{},
		},
		{
			name:     "Whitespace-only string yields empty slice",
			wl:       " \t  \n  ",
			expected: []string{},
		},
		{
			name:     "Empty quotes yield empty elem",
			wl:       `a "" b '' c`,
			expected: []string{"a", ``, "b", ``, "c"},
		},
		{
			name:     "Splits spaces",
			wl:       `a b c`,
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "Splits tabs",
			wl:       "a\tb\tc",
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "Splits newlines",
			wl:       "a\nb\nc",
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "Splits carriage returns",
			wl:       "a\rb",
			expected: []string{"a", "b"},
		},
		{
			name:     "Splits on single quotes",
			wl:       `workload 'a b c "de' f g'"'h`,
			expected: []string{"workload", `a b c "de`, "f", `g"h`},
		},
		{
			name: "Splits on double quotes",
			wl:   `workload "a b c " de' f g'"'h`,
			// Unterminated quoting should not be edited
			expected: []string{"workload", `a b c `, `de f g"'h`},
		},
		{
			name:     "Backslash escapes any char outside of quotes",
			wl:       `workload \"a"bc" \\d\'e'\\f\'\d`,
			expected: []string{"workload", `"abc`, `\d'e\\f\d`},
		},
		{
			name: "Backslash escapes chars inside double quotes, otherwise is kept literally",
			wl: `workload "ab\"c\
\\a\$, \e\r\.\'' avc" 'b"e"`,
			expected: []string{`workload`, `ab"c\a$, \e\r\.\'' avc`, `'b"e"`},
		},
		{
			name:     "Backslash is always literal in single quotes",
			wl:       `a 'b \" \\ \e \$ \' sdfs'`,
			expected: []string{"a", `b \" \\ \e \$ \`, `sdfs'`},
		},
		{
			name:     "Escapes single quotes correctly",
			wl:       `workload \'a b c 'd e\' f g' h`,
			expected: []string{"workload", `'a`, "b", "c", `d e\`, "f", "g' h"},
		},
		{
			name:     "Ignores escaped double quotes",
			wl:       `workload \"a "\"bc d\\" e\""\" f" '" g "'`,
			expected: []string{"workload", `"a`, `"bc d\`, `e"" f`, `" g "`},
		},
		{
			name:     "Double quotes within single quotes are not treated as special",
			wl:       `workload '"a ' b c" d e\" "`,
			expected: []string{"workload", `"a `, "b", `c d e" `},
		},
		{
			name:     "multiple consecutive whitespace is ignored",
			wl:       "workload   \t  \t a  \t\n\t\t\n  b",
			expected: []string{"workload", "a", "b"},
		},
		{
			name:     "leading / trailing whitespace is ignored",
			wl:       "\t\t\nworkload   \ta  \t\nb  \t\n",
			expected: []string{"workload", "a", "b"},
		},
		{
			name:     "unclosed quotes are not replaced",
			wl:       `workload ' a c \"' "b d`,
			expected: []string{"workload", ` a c \"`, `"b d`},
		},
		{
			name:     "backslash at end of input is fine",
			wl:       `workload a b c\`,
			expected: []string{"workload", "a", "b", `c\`},
		},
		{
			name: "whitespace can be escaped",
			wl: `workload a\ b c\	d\
e`,
			expected: []string{"workload", `a b`, `c	de`},
		},
		{
			name:     "handles non-standard characters",
			wl:       `workload åßd'jd i "s©' '   	jd \' ®' π`,
			expected: []string{"workload", `åßdjd i "s©`, `   	jd \`, `®' π`},
		},
		{
			name: "Example case",
			wl: `bash -c "abc def" 'abc "def hij"	\"
k lm\' n' \"o p`,
			expected: []string{"bash", "-c", `abc def`, `abc "def hij"	\"
k lm\`, `n' \"o p`},
		},
		{
			name:     "Example case 2",
			wl:       `bash -c 'echo START && exit 42'`,
			expected: []string{"bash", "-c", "echo START && exit 42"},
		},
		{
			name:     "Example case 3",
			wl:       `bash -c 'echo MARKER && sleep 1 && echo DONE'`,
			expected: []string{"bash", "-c", "echo MARKER && sleep 1 && echo DONE"},
		},
		{
			name:     "Example case 4",
			wl:       `bash -c "echo 'inner single' && echo \"inner double\""`,
			expected: []string{"bash", "-c", `echo 'inner single' && echo "inner double"`},
		},
		{
			name:     "Example case 5",
			wl:       `bash -c 'echo "path with spaces" && ls /usr/bin/bash'`,
			expected: []string{"bash", "-c", `echo "path with spaces" && ls /usr/bin/bash`},
		},
		{
			name:     "Example case 6",
			wl:       `bash -c 'echo C:\\Temp && exit 0'`,
			expected: []string{"bash", "-c", `echo C:\\Temp && exit 0`},
		},
		{
			name:     "Example case 7",
			wl:       `sh -c 'echo $HOME && echo \$NOT_EXPANDED'`,
			expected: []string{"sh", "-c", `echo $HOME && echo \$NOT_EXPANDED`},
		},
		{
			name:     "Example case 8",
			wl:       `bash -c "sleep 1 & sleep 1; echo BOTH_DONE"`,
			expected: []string{"bash", "-c", `sleep 1 & sleep 1; echo BOTH_DONE`},
		},
		{
			name:     "Example case 9",
			wl:       `bash -c 'yes MARK | head -n 3'`,
			expected: []string{"bash", "-c", `yes MARK | head -n 3`},
		},
		{
			name:     "Example case 10",
			wl:       `sh -c "echo 'a b' && echo \"c d\""`,
			expected: []string{"sh", "-c", `echo 'a b' && echo "c d"`},
		},
		{
			name:     "Example case 11",
			wl:       `cmd path\\with\\slashes`,
			expected: []string{"cmd", `path\with\slashes`},
		},
		{
			name:     "Example case 12",
			wl:       `"C:\Program Files\My App\foo.exe" --flag`,
			expected: []string{`C:\Program Files\My App\foo.exe`, "--flag"},
		},
		{
			name:     "Example case 13",
			wl:       `/tmp/atperf\tempFile`,
			expected: []string{`/tmp/atperftempFile`},
		},
		{
			name:     "Example case 14",
			wl:       `cmd /c echo ^&`,
			expected: []string{"cmd", "/c", "echo", "^&"},
		},
		{
			name:     "Double-quote escapes dollar sign",
			wl:       `"\$"`,
			expected: []string{`$`},
		},
		{
			name:     "Double-quote preserves backslash for non-escapable",
			wl:       `"\\q"`,
			expected: []string{`\q`},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := WorkloadStringToSlice(tc.wl)
			assert.Equal(t, tc.expected, got)
		})
	}
}
