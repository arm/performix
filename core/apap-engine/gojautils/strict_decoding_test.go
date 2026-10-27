// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package gojautils

import (
	"bytes"
	"errors"
	"testing"

	"github.com/dop251/goja"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStrictDecodingRejectsCycles(t *testing.T) {
	for name, source := range map[string]string{
		"option object":  `(() => { const opts = {}; opts.self = opts; return opts; })()`,
		"array":          `(() => { const args = []; args.push(args); return args; })()`,
		"indirect cycle": `(() => { const opts = {}; opts.child = [opts]; return opts; })()`,
	} {
		t.Run(name, func(t *testing.T) {
			value, err := goja.New().RunString(source)
			require.NoError(t, err)
			var dest any
			err = ParseObjectFromJSStrictWithRegex(value, &dest, nil, nil)
			require.EqualError(t, err, "type mismatch: cyclic values are not allowed")
		})
	}
}

func TestStrictDecodingErrorsOmitPrivateValues(t *testing.T) {
	for name, tc := range map[string]struct{ source, diagnostic string }{
		"command argument":          {`({command: ["program", {secret: "private-argument"}]})`, "command element expects string"},
		"environment value":         {`({environment: {TOKEN: {secret: "private-environment"}}})`, "environment value expects string"},
		"numeric environment value": {`({environment: {TOKEN: 987654321}})`, "environment value expects string"},
		"working directory":         {`({workingDirectory: ["private-directory"]})`, "workingDirectory expects string"},
		"redirect field":            {`({stdout: {redirect: ["private-redirect"]}})`, "stdout.redirect expects string"},
	} {
		t.Run(name, func(t *testing.T) {
			logger := log.StandardLogger()
			output, level := logger.Out, logger.GetLevel()
			t.Cleanup(func() {
				logger.SetOutput(output)
				logger.SetLevel(level)
			})
			var logs bytes.Buffer
			logger.SetOutput(&logs)
			logger.SetLevel(log.WarnLevel)

			value, err := goja.New().RunString(tc.source)
			require.NoError(t, err)
			var dest struct {
				Command          []string          `json:"command"`
				Environment      map[string]string `json:"environment"`
				WorkingDirectory string            `json:"workingDirectory"`
				Stdout           struct {
					Redirect string `json:"redirect"`
				} `json:"stdout"`
			}
			err = ParseObjectFromJSStrictWithRegex(value, &dest, nil, nil)
			require.EqualError(t, err, "type mismatch: "+tc.diagnostic)
			assert.NotContains(t, logs.String(), "private-")
			assert.NotContains(t, logs.String(), "987654321")
			assert.Contains(t, logs.String(), "type mismatch: "+tc.diagnostic)
			require.Nil(t, errors.Unwrap(err), "the private decoder error must not survive as a cause")
		})
	}
}

func TestStrictDecodingAcceptsSharedValues(t *testing.T) {
	value, err := goja.New().RunString(`(() => {
		const shared = {value: "kept"};
		return {first: shared, second: shared};
	})()`)
	require.NoError(t, err)
	var dest map[string]map[string]string
	require.NoError(t, ParseObjectFromJSStrictWithRegex(value, &dest, nil, nil))
	require.Equal(t, map[string]map[string]string{
		"first": {"value": "kept"}, "second": {"value": "kept"},
	}, dest)
}

func TestStrictNullDiagnostics(t *testing.T) {
	for name, tc := range map[string]struct{ source, diagnostic string }{
		"environment value": {`({environment: {TOKEN: null}})`, "environment value expects string"},
		"redirect field":    {`({stdout: {redirect: null}})`, "stdout.redirect expects string"},
	} {
		t.Run(name, func(t *testing.T) {
			value, err := goja.New().RunString(tc.source)
			require.NoError(t, err)
			var dest struct {
				Environment map[string]string `json:"environment"`
				Stdout      struct {
					Redirect string `json:"redirect"`
				} `json:"stdout"`
			}
			err = ParseObjectFromJSStrictWithRegex(value, &dest, nil, nil)
			require.EqualError(t, err, "type mismatch: "+tc.diagnostic)
		})
	}
}
