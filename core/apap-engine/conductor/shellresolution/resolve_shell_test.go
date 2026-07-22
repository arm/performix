// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package shellresolution

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	conductormocks_test "github.com/Arm-Debug/apap-cli/apap-engine/conductor/conductormocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
)

func TestShellifyWorkload(t *testing.T) {
	t.Run("errors on unknown OS", func(t *testing.T) {
		_, _, err := ShellifyWorkload("something", nil, nil)
		expectedErr := message.New(message.EngineCommonUnsupportedTargetOs).WithMetadata(map[string]string{"os": "something"})
		assert.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})
	t.Run("uses $SHELL if set for Linux target", func(t *testing.T) {
		wl := &tool.WorkloadLaunch{
			RawCommand: `echo "a" && echo "b says \"c\"" && echo 'D'`,
			Command:    []string{"echo", "a", "&&", "echo", `b says "c"`, "&&", "echo", "D"},
			UseShell:   true,
		}
		cmdRunner := conductormocks_test.MockCommandRunner{}
		cmdRunner.On("RunCommand", "echo $SHELL").Return("/bin/something\n", "", nil)
		cmdRunner.On("RunCommand", "/bin/something -c true").Return("", "", nil)
		updatedRaw, updatedSlice, err := ShellifyWorkload(conductor.Linux, &cmdRunner, wl)
		assert.NoError(t, err)
		assert.Equal(t, `/bin/something -c "echo \"a\" && echo \"b says \\\"c\\\"\" && echo 'D'"`, updatedRaw)
		assert.Equal(t, []string{"/bin/something", "-c", wl.RawCommand}, updatedSlice)
		cmdRunner.AssertExpectations(t)
	})
	t.Run("falls back to /bin/sh on Linux target when $SHELL not set", func(t *testing.T) {
		wl := &tool.WorkloadLaunch{
			RawCommand: `echo "a" && echo "b says \"c\"" && echo 'D'`,
			Command:    []string{"echo", "a", "&&", "echo", `b says "c"`, "&&", "echo", "D"},
			UseShell:   true,
		}
		cmdRunner := conductormocks_test.MockCommandRunner{}
		cmdRunner.On("RunCommand", "echo $SHELL").Return("", "", nil)
		cmdRunner.On("RunCommand", "/bin/sh -c true").Return("", "", nil)
		updatedRaw, updatedSlice, err := ShellifyWorkload(conductor.Linux, &cmdRunner, wl)
		assert.NoError(t, err)
		assert.Equal(t, `/bin/sh -c "echo \"a\" && echo \"b says \\\"c\\\"\" && echo 'D'"`, updatedRaw)
		assert.Equal(t, []string{"/bin/sh", "-c", wl.RawCommand}, updatedSlice)
		cmdRunner.AssertExpectations(t)
	})
	t.Run("uses %SHELL% if available for Windows target", func(t *testing.T) {
		wl := &tool.WorkloadLaunch{
			RawCommand: "echo \"a\"; echo \"b says \\`\"c\\`\"\"; echo 'D'",
			Command:    []string{"echo", "a;", "echo", "b says \\`\"c\"\\`;", "echo", "D"},
			UseShell:   true,
		}
		// path contains spaces - testing that this is quoted correctly
		path := "C:\\Windows\\System32\\WindowsPowerShell\\v1.0\\my dir\\powershell.exe"
		quotedPath := fmt.Sprintf("\"%v\"", strings.ReplaceAll(path, `\`, `\\`))
		cmdRunner := conductormocks_test.MockCommandRunner{}
		cmdRunner.On("RunCommand", "echo %SHELL%").Return(fmt.Sprintf("%v\r\n", path), "", nil)
		cmdRunner.On("RunCommand", fmt.Sprintf("%v -NoLogo -WindowStyle Hidden -Command exit", quotedPath)).Return("", "", nil)
		updatedRaw, updatedSlice, err := ShellifyWorkload(conductor.Win, &cmdRunner, wl)
		assert.NoError(t, err)
		assert.Equal(t, fmt.Sprintf("%v -NoLogo -WindowStyle Hidden -Command \"echo \\\"a\\\"; echo \\\"b says \\\\`\\\"c\\\\`\\\"\\\"; echo 'D'\"", quotedPath), updatedRaw)
		assert.Equal(t, []string{path, "-NoLogo", "-WindowStyle", "Hidden", "-Command", wl.RawCommand}, updatedSlice)
	})
	t.Run("falls back to cmd.exe on Windows target if %SHELL% not set", func(t *testing.T) {
		wl := &tool.WorkloadLaunch{
			RawCommand: "echo \"a\"; echo \"b says \\`\"c\\`\"\"; echo 'D'",
			Command:    []string{"echo", "a;", "echo", "b says \\`\"c\"\\`;", "echo", "D"},
			UseShell:   true,
		}
		cmdRunner := conductormocks_test.MockCommandRunner{}
		cmdRunner.On("RunCommand", "echo %SHELL%").Return("", "", nil)
		cmdRunner.On("RunCommand", "cmd.exe /c exit").Return("", "", nil)
		updatedRaw, updatedSlice, err := ShellifyWorkload(conductor.Win, &cmdRunner, wl)
		assert.NoError(t, err)
		assert.Equal(t, "cmd.exe /c \"echo \\\"a\\\"; echo \\\"b says \\\\`\\\"c\\\\`\\\"\\\"; echo 'D'\"", updatedRaw)
		assert.Equal(t, []string{"cmd.exe", "/c", wl.RawCommand}, updatedSlice)
	})
	t.Run("falls back to cmd.exe on Windows target if %SHELL% returned as literal", func(t *testing.T) {
		wl := &tool.WorkloadLaunch{
			RawCommand: "echo \"a\"; echo \"b says \\`\"c\\`\"\"; echo 'D'",
			Command:    []string{"echo", "a;", "echo", "b says \\`\"c\"\\`;", "echo", "D"},
			UseShell:   true,
		}
		cmdRunner := conductormocks_test.MockCommandRunner{}
		cmdRunner.On("RunCommand", "echo %SHELL%").Return("%SHELL%\r\n", "", nil)
		cmdRunner.On("RunCommand", "cmd.exe /c exit").Return("", "", nil)
		updatedRaw, updatedSlice, err := ShellifyWorkload(conductor.Win, &cmdRunner, wl)
		assert.NoError(t, err)
		assert.Equal(t, "cmd.exe /c \"echo \\\"a\\\"; echo \\\"b says \\\\`\\\"c\\\\`\\\"\\\"; echo 'D'\"", updatedRaw)
		assert.Equal(t, []string{"cmd.exe", "/c", wl.RawCommand}, updatedSlice)
	})
	t.Run("returns appropriate error if no viable shell was found on Linux", func(t *testing.T) {
		wl := &tool.WorkloadLaunch{}
		cmdRunner := conductormocks_test.MockCommandRunner{}
		cmdRunner.On("RunCommand", mock.Anything).Return("", "", errors.New("does't exist!"))
		_, _, err := ShellifyWorkload(conductor.Linux, &cmdRunner, wl)

		expectedMetadata := map[string]string{"envVar": "$SHELL", "fallback": "/bin/sh"}
		expectedErr := message.New(message.EngineConductorShellresolutionNoShellFound).WithMetadata(expectedMetadata)
		assert.Equal(t, expectedErr, err)
		cmdRunner.AssertExpectations(t)
	})
	t.Run("returns appropriate error if no viable shell was found on Windows", func(t *testing.T) {
		wl := &tool.WorkloadLaunch{}
		cmdRunner := conductormocks_test.MockCommandRunner{}
		cmdRunner.On("RunCommand", mock.Anything).Return("", "", errors.New("does't exist!"))
		_, _, err := ShellifyWorkload(conductor.Win, &cmdRunner, wl)

		expectedMetadata := map[string]string{"envVar": "%SHELL%", "fallback": "cmd.exe"}
		expectedErr := message.New(message.EngineConductorShellresolutionNoShellFound).WithMetadata(expectedMetadata)
		assert.Equal(t, expectedErr, err)
		cmdRunner.AssertExpectations(t)
	})
}

func TestQuoteIfNeeded(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "returns input unchanged when it has no whitespace",
			input:    "/bin/bash",
			expected: "/bin/bash",
		},
		{
			name:     "returns empty string unchanged",
			input:    "",
			expected: "",
		},
		{
			name:     "quotes strings with spaces",
			input:    "/Applications/Arm Tools/bin/bash",
			expected: `"/Applications/Arm Tools/bin/bash"`,
		},
		{
			name:     "quotes windows paths with spaces and escapes backslashes",
			input:    `C:\Program Files\Arm Tools\shell.exe`,
			expected: `"C:\\Program Files\\Arm Tools\\shell.exe"`,
		},
		{
			name:     "quotes strings with tabs",
			input:    "arm\ttools",
			expected: `"arm\ttools"`,
		},
		{
			name:     "quotes strings with newlines",
			input:    "arm\ntools",
			expected: `"arm\ntools"`,
		},
		{
			name:     "quotes strings with whitespace and escapes embedded quotes",
			input:    `echo "arm tools"`,
			expected: `"echo \"arm tools\""`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, quoteIfNeeded(tc.input))
		})
	}
}
