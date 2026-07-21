// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package android

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

func TestParsePackageList(t *testing.T) {
	output := `
package:com.example.beta
package:/data/app/~~abc/com.example.alpha/base.apk=com.example.alpha
ignored
package:com.example.beta
package:
`

	assert.Equal(t, []string{"com.example.alpha", "com.example.beta"}, parsePackageList(output))
}

func TestParseMainActivityComponentsByPackage(t *testing.T) {
	output := `
com.example.app/.MainActivity
com.example.app/com.example.app.SettingsActivity
com.example.app/com.example.app.SettingsActivity
com.example.other/.LauncherActivity
`

	assert.Equal(t, map[string][]string{
		"com.example.app": {
			"com.example.app.MainActivity",
			"com.example.app.SettingsActivity",
		},
		"com.example.other": {"com.example.other.LauncherActivity"},
	}, parseMainActivityComponentsByPackage(output))
}

func TestParseComponentName(t *testing.T) {
	tests := map[string]struct {
		component   string
		packageName string
		activity    string
		ok          bool
	}{
		"relative activity": {
			component:   "com.example.app/.MainActivity",
			packageName: "com.example.app",
			activity:    "com.example.app.MainActivity",
			ok:          true,
		},
		"fully qualified activity": {
			component:   "com.example.app/com.example.app.SettingsActivity",
			packageName: "com.example.app",
			activity:    "com.example.app.SettingsActivity",
			ok:          true,
		},
		"component with surrounding whitespace": {
			component:   "  com.example.app/.MainActivity  ",
			packageName: "com.example.app",
			activity:    "com.example.app.MainActivity",
			ok:          true,
		},
		"line without component": {
			component: "Action: \"android.intent.action.MAIN\"",
		},
		"empty activity": {
			component: "com.example.app/",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			packageName, activity, ok := parseComponentName(tt.component)

			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.packageName, packageName)
			assert.Equal(t, tt.activity, activity)
		})
	}
}

func TestListPackages(t *testing.T) {
	pm := &process.MockProcessManager{}
	pm.On("ExecCommand", &process.LaunchCommand{Command: []string{"pm", "list", "packages", "-3"}}).
		Return(&process.CommandResult{Rc: 0, Stdout: "package:com.example.beta\npackage:com.example.alpha\npackage:com.example.noentry\n"}, nil).Once()
	pm.On("ExecCommand", &process.LaunchCommand{Command: []string{"sh", "-c", mainActivitiesCommand}}).
		Return(&process.CommandResult{Rc: 0, Stdout: `
com.example.alpha/.MainActivity
com.example.beta/.LauncherActivity
com.example.beta/.SettingsActivity
com.android.settings/.Settings
`}, nil).Once()

	resp, err := ListPackages(pm)

	require.NoError(t, err)
	assert.Equal(t, &targetagentproto.AndroidPackageList{
		Packages: []*targetagentproto.AndroidPackage{
			{
				Name:       "com.example.alpha",
				Activities: []*targetagentproto.AndroidActivity{{Name: "com.example.alpha.MainActivity"}},
			},
			{
				Name: "com.example.beta",
				Activities: []*targetagentproto.AndroidActivity{
					{Name: "com.example.beta.LauncherActivity"},
					{Name: "com.example.beta.SettingsActivity"},
				},
			},
		},
	}, resp)
	pm.AssertExpectations(t)
}

func TestListPackagesReturnsCommandError(t *testing.T) {
	expectedErr := errors.New("pm unavailable")
	pm := &process.MockProcessManager{}
	pm.On("ExecCommand", &process.LaunchCommand{Command: []string{"pm", "list", "packages", "-3"}}).
		Return(&process.CommandResult{}, expectedErr).Once()

	resp, err := ListPackages(pm)

	require.Nil(t, resp)
	require.ErrorIs(t, err, expectedErr)
	pm.AssertExpectations(t)
}

func TestListPackagesReturnsResolverCommandError(t *testing.T) {
	expectedErr := errors.New("dumpsys unavailable")
	pm := &process.MockProcessManager{}
	pm.On("ExecCommand", &process.LaunchCommand{Command: []string{"pm", "list", "packages", "-3"}}).
		Return(&process.CommandResult{Rc: 0, Stdout: "package:com.example.app\n"}, nil).Once()
	pm.On("ExecCommand", &process.LaunchCommand{Command: []string{"sh", "-c", mainActivitiesCommand}}).
		Return(&process.CommandResult{}, expectedErr).Once()

	resp, err := ListPackages(pm)

	require.Nil(t, resp)
	require.ErrorIs(t, err, expectedErr)
	pm.AssertExpectations(t)
}
