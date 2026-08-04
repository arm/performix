// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package target

import (
	"bytes"
	"encoding/json"
	"errors"
	"os/exec"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-cli/service/clijson"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/target"
	"github.com/Arm-Debug/apap-cli/apap-cli/utils"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	engine_target "github.com/Arm-Debug/apap-cli/apap-engine/target"
)

type fakeAndroidTargetDiscoverer struct {
	targets []*engine_target.AndroidTarget
	err     error
	calls   int
}

func (d *fakeAndroidTargetDiscoverer) DiscoverAndroidTargets() ([]*engine_target.AndroidTarget, error) {
	d.calls++
	return d.targets, d.err
}

func TestListTarget(t *testing.T) {

	mtm := target.MockTargetManager{}
	mtm.On("ReadTargetConfig").Return(&engine_target.TargetConfig{Default: "default", Targets: make(map[string]engine_target.Target)}, nil)

	t.Run("invalid arguments fail", func(t *testing.T) {
		cmd := newListCommand(&mtm, &fakeAndroidTargetDiscoverer{})
		cmd.SetArgs([]string{"InvalidArgumentHere"})

		_, err := cmd.ExecuteC()

		require.Error(t, err)

		expectedErr := "accepts 0 arg(s), received 1"
		assert.Equal(t, expectedErr, err.Error())
	})

	t.Run("valid arguments succeed", func(t *testing.T) {
		cmd := newListCommand(&mtm, &fakeAndroidTargetDiscoverer{})
		cmd.SetArgs([]string{})

		_, err := cmd.ExecuteC()

		assert.NoError(t, err)
	})

	t.Run("default discoverer uses configured adb path", func(t *testing.T) {
		originalEnableAndroidTargets := viper.GetBool(enableAndroidTargetsConfigKey)
		originalADBPath := viper.GetString("adb-path")
		viper.Set(enableAndroidTargetsConfigKey, true)
		viper.Set("adb-path", "configured-adb-that-does-not-exist")
		t.Cleanup(func() {
			viper.Set(enableAndroidTargetsConfigKey, originalEnableAndroidTargets)
			viper.Set("adb-path", originalADBPath)
		})

		cmd := newListCommand(&mtm, adbAndroidTargetDiscoverer{})
		cmd.SetArgs([]string{"--discover"})

		_, err := cmd.ExecuteC()

		var execErr *exec.Error
		require.ErrorAs(t, err, &execErr)
		assert.Equal(t, "configured-adb-that-does-not-exist", execErr.Name)
	})

	t.Run("discover flag is hidden when android targets are disabled", func(t *testing.T) {
		originalEnableAndroidTargets := viper.GetBool(enableAndroidTargetsConfigKey)
		viper.Set(enableAndroidTargetsConfigKey, false)
		t.Cleanup(func() {
			viper.Set(enableAndroidTargetsConfigKey, originalEnableAndroidTargets)
		})

		cmd := newListCommand(&mtm, &fakeAndroidTargetDiscoverer{})

		assert.True(t, cmd.Flags().Lookup("discover").Hidden)
	})

	t.Run("discover flag is visible when android targets are enabled", func(t *testing.T) {
		originalEnableAndroidTargets := viper.GetBool(enableAndroidTargetsConfigKey)
		viper.Set(enableAndroidTargetsConfigKey, true)
		t.Cleanup(func() {
			viper.Set(enableAndroidTargetsConfigKey, originalEnableAndroidTargets)
		})

		cmd := newListCommand(&mtm, &fakeAndroidTargetDiscoverer{})

		assert.False(t, cmd.Flags().Lookup("discover").Hidden)
	})

	testConfig := &engine_target.TargetConfig{
		Default: "aaa",
		Targets: map[string]engine_target.Target{
			"bbbeeee": &engine_target.LocalTarget{},
			"aaa": &engine_target.SSHTarget{Jumps: []engine_target.SSHHostConfig{
				{
					Host:               "q",
					Port:               222,
					Username:           "whoever",
					PrivateKeyFilename: "c",
				},
				{
					Host:               "a",
					Port:               123,
					Username:           "b",
					PrivateKeyFilename: "c",
					HostKeyPolicy:      engine_target.ValidHostKeyPolicyValues["accept-new"],
				},
			}},
		},
	}

	t.Run("items are listed in a table", func(t *testing.T) {
		originalEnableAndroidTargets := viper.GetBool(enableAndroidTargetsConfigKey)
		viper.Set(enableAndroidTargetsConfigKey, false)
		t.Cleanup(func() {
			viper.Set(enableAndroidTargetsConfigKey, originalEnableAndroidTargets)
		})

		buf := bytes.Buffer{}
		printTargetListing(listingFromConfig(testConfig), &buf)

		expected := `name     default  host_key_policy  value
aaa      yes      accept-new       b@a:123 [key=c] (via whoever@q:222 [key=c])
bbbeeee  no                        localhost

2 targets found
`

		assert.Equal(t, expected, buf.String())
	})

	t.Run("type column is listed in a table when android targets are enabled", func(t *testing.T) {
		originalEnableAndroidTargets := viper.GetBool(enableAndroidTargetsConfigKey)
		viper.Set(enableAndroidTargetsConfigKey, true)
		t.Cleanup(func() {
			viper.Set(enableAndroidTargetsConfigKey, originalEnableAndroidTargets)
		})

		buf := bytes.Buffer{}
		printTargetListing(listingFromConfig(testConfig), &buf)

		expected := `name     default  type   host_key_policy  value
aaa      yes      ssh    accept-new       b@a:123 [key=c] (via whoever@q:222 [key=c])
bbbeeee  no       local                   localhost

2 targets found
`

		assert.Equal(t, expected, buf.String())
	})

	t.Run("android item is listed in a table", func(t *testing.T) {
		originalEnableAndroidTargets := viper.GetBool(enableAndroidTargetsConfigKey)
		viper.Set(enableAndroidTargetsConfigKey, true)
		t.Cleanup(func() {
			viper.Set(enableAndroidTargetsConfigKey, originalEnableAndroidTargets)
		})

		deviceIP := "10.0.0.1:5555"
		config := &engine_target.TargetConfig{
			Default: "android",
			Targets: map[string]engine_target.Target{
				"android": &engine_target.AndroidTarget{SerialNumber: "device-123", DeviceIPAddress: &deviceIP},
			},
		}

		buf := bytes.Buffer{}
		printTargetListing(listingFromConfig(config), &buf)

		expected := `name     default  type     host_key_policy  value
android  yes      android  n/a              device-123@10.0.0.1:5555

1 target found
`

		assert.Equal(t, expected, buf.String())
	})

	t.Run("android item is hidden in a table when disabled", func(t *testing.T) {
		originalEnableAndroidTargets := viper.GetBool(enableAndroidTargetsConfigKey)
		viper.Set(enableAndroidTargetsConfigKey, false)
		t.Cleanup(func() {
			viper.Set(enableAndroidTargetsConfigKey, originalEnableAndroidTargets)
		})

		deviceIP := "10.0.0.1:5555"
		config := &engine_target.TargetConfig{
			Default: "ssh",
			Targets: map[string]engine_target.Target{
				"android": &engine_target.AndroidTarget{SerialNumber: "device-123", DeviceIPAddress: &deviceIP},
				"ssh": &engine_target.SSHTarget{Jumps: []engine_target.SSHHostConfig{
					{
						Host:          "host",
						Port:          22,
						Username:      "user",
						HostKeyPolicy: engine_target.ValidHostKeyPolicyValues["strict"],
					},
				}},
			},
		}

		buf := bytes.Buffer{}
		printTargetListing(listingFromConfig(config), &buf)

		expected := `name  default  host_key_policy  value
ssh   yes      strict           user@host [key]

1 target found
`

		assert.Equal(t, expected, buf.String())
	})

	t.Run("android item is hidden as json when disabled", func(t *testing.T) {
		originalEnableAndroidTargets := viper.GetBool(enableAndroidTargetsConfigKey)
		viper.Set(enableAndroidTargetsConfigKey, false)
		t.Cleanup(func() {
			viper.Set(enableAndroidTargetsConfigKey, originalEnableAndroidTargets)
		})

		deviceIP := "10.0.0.1:5555"
		config := &engine_target.TargetConfig{
			Default: "android",
			Targets: map[string]engine_target.Target{
				"android": &engine_target.AndroidTarget{SerialNumber: "device-123", DeviceIPAddress: &deviceIP},
			},
		}

		buf := bytes.Buffer{}
		err := clijson.MarshalJSONCLIResponse(&buf, listingFromConfig(config))
		assert.NoError(t, err)
		if err != nil {
			return
		}

		var response map[string]interface{}
		err = json.Unmarshal(buf.Bytes(), &response)
		assert.NoError(t, err)
		assert.Empty(t, response["data"])
	})

	t.Run("items are listed as json", func(t *testing.T) {
		buf := bytes.Buffer{}
		err := clijson.MarshalJSONCLIResponse(&buf, listingFromConfig(testConfig))
		assert.NoError(t, err)
		if err != nil {
			return
		}

		var expectedMap = map[string]interface{}{
			"code": "0",
			"error": map[string]interface{}{
				"message_code": "",
				"severity":     "",
				"message":      "",
				"explanation":  "",
				"advice":       "",
				"locale":       "",
				"metadata":     nil,
			},
			"grpc_info": map[string]interface{}{
				"grpc_code":    "OK",
				"grpc_message": "",
			},
			"data": map[string]interface{}{
				"aaa": map[string]interface{}{
					"type": "ssh",
					"value": map[string]interface{}{
						"jumps": []interface{}{
							map[string]interface{}{
								"host":                  "q",
								"port":                  222,
								"username":              "whoever",
								"private_key_filename":  "c",
								"host_key_policy":       "ignore",
								"authentication_method": "key",
							},
							map[string]interface{}{
								"host":                  "a",
								"port":                  123,
								"username":              "b",
								"private_key_filename":  "c",
								"host_key_policy":       "accept-new",
								"authentication_method": "key",
							},
						},
					},
					"default": true,
				},
				"bbbeeee": map[string]interface{}{
					"type":    "local",
					"value":   map[string]interface{}{},
					"default": false,
				},
			},
		}
		expected, err := json.Marshal(expectedMap)
		assert.NoError(t, err)

		assert.JSONEq(t, string(expected), buf.String())
	})

	t.Run("formatTargetWithAuth includes auth per hop", func(t *testing.T) {
		tgt := &engine_target.SSHTarget{Jumps: []engine_target.SSHHostConfig{
			{
				Host:               "jump",
				Port:               222,
				Username:           "jumpuser",
				PrivateKeyFilename: "/keys/jump",
				AuthMethod:         engine_target.SSHAuthMethodKey,
			},
			{
				Host:     "jump2",
				Port:     220,
				Username: "jumper",
			},
			{
				Host:       "dest",
				Port:       22,
				Username:   "user",
				AuthMethod: engine_target.SSHAuthMethodPassword,
			},
		}}

		got := formatTargetWithAuth(tgt)
		expected := "user@dest [password] (via jumper@jump2:220 [key], jumpuser@jump:222 [key=/keys/jump])"
		assert.Equal(t, expected, got)
	})

	t.Run("adb discoverer parses connected devices", func(t *testing.T) {
		stdout := `List of devices attached
emulator-5556	device
offline-device	offline
unauthorized-device	unauthorized
192.0.2.1:5555	device product:sdk model:Pixel device:generic

`

		targets := parseADBDevices(stdout)

		assert.Equal(t, []*engine_target.AndroidTarget{
			{SerialNumber: "emulator-5556"},
			{SerialNumber: "192.0.2.1:5555"},
		}, targets)
	})

	t.Run("discovers android targets in a table", func(t *testing.T) {
		originalEnableAndroidTargets := viper.GetBool(enableAndroidTargetsConfigKey)
		viper.Set(enableAndroidTargetsConfigKey, true)
		t.Cleanup(func() {
			viper.Set(enableAndroidTargetsConfigKey, originalEnableAndroidTargets)
		})

		discoverer := &fakeAndroidTargetDiscoverer{
			targets: []*engine_target.AndroidTarget{
				{SerialNumber: "emulator-5556"},
				{SerialNumber: "device-123"},
			},
		}
		mtm := target.MockTargetManager{}
		mtm.On("ReadTargetConfig").Return(&engine_target.TargetConfig{
			Default: "ssh",
			Targets: map[string]engine_target.Target{
				"phone": &engine_target.AndroidTarget{SerialNumber: "device-123"},
				"ssh": &engine_target.SSHTarget{Jumps: []engine_target.SSHHostConfig{
					{
						Host:          "host",
						Port:          22,
						Username:      "user",
						HostKeyPolicy: engine_target.ValidHostKeyPolicyValues["strict"],
					},
				}},
			},
		}, nil)
		cmd := newListCommand(&mtm, discoverer)
		buf := bytes.Buffer{}
		cmd.SetOut(&buf)
		cmd.SetArgs([]string{"--discover"})

		_, err := cmd.ExecuteC()

		require.NoError(t, err)
		assert.Equal(t, 1, discoverer.calls)
		expected := `name           default  type     host_key_policy  value
device-123     no       android  n/a              device-123
emulator-5556  no       android  n/a              emulator-5556

2 targets found
`
		assert.Equal(t, expected, buf.String())
	})

	t.Run("discovers android targets as json", func(t *testing.T) {
		originalEnableAndroidTargets := viper.GetBool(enableAndroidTargetsConfigKey)
		viper.Set(enableAndroidTargetsConfigKey, true)
		t.Cleanup(func() {
			viper.Set(enableAndroidTargetsConfigKey, originalEnableAndroidTargets)
		})

		discoverer := &fakeAndroidTargetDiscoverer{
			targets: []*engine_target.AndroidTarget{
				{SerialNumber: "emulator-5556"},
				{SerialNumber: "device-123"},
			},
		}
		mtm := target.MockTargetManager{}
		mtm.On("ReadTargetConfig").Return(&engine_target.TargetConfig{
			Targets: map[string]engine_target.Target{
				"phone": &engine_target.AndroidTarget{SerialNumber: "device-123"},
			},
		}, nil)
		buf := bytes.Buffer{}
		cmd := newListCommand(&mtm, discoverer)
		cmd.SetOut(&buf)
		utils.SetPersistentFlags(cmd)
		cmd.SetArgs([]string{"--discover", "--json"})

		_, err := cmd.ExecuteC()

		require.NoError(t, err)
		assert.Equal(t, 1, discoverer.calls)
		expected := `{
			"code": "0",
			"error": {
				"message_code": "",
				"severity": "",
				"message": "",
				"explanation": "",
				"advice": "",
				"locale": "",
				"metadata": null
			},
			"grpc_info": {
				"grpc_code": "OK",
				"grpc_message": ""
			},
			"data": {
				"emulator-5556": {
					"type": "android",
					"value": {
						"serial_number": "emulator-5556"
					},
					"default": false
				},
				"device-123": {
					"type": "android",
					"value": {
						"serial_number": "device-123"
					},
					"default": false
				}
			}
		}`
		assert.JSONEq(t, expected, buf.String())
	})

	t.Run("discover fails when android targets are disabled", func(t *testing.T) {
		originalEnableAndroidTargets := viper.GetBool(enableAndroidTargetsConfigKey)
		viper.Set(enableAndroidTargetsConfigKey, false)
		t.Cleanup(func() {
			viper.Set(enableAndroidTargetsConfigKey, originalEnableAndroidTargets)
		})

		discoverer := &fakeAndroidTargetDiscoverer{}
		mtm := target.MockTargetManager{}
		mtm.On("ReadTargetConfig").Return(&engine_target.TargetConfig{Targets: map[string]engine_target.Target{}}, nil)
		cmd := newListCommand(&mtm, discoverer)
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetArgs([]string{"--discover"})

		_, err := cmd.ExecuteC()

		require.Error(t, err)
		assert.Equal(t, 0, discoverer.calls)
		assert.Equal(t, message.CommonUnsupportedTargetType, err.(*message.MessageImpl).Code())
	})

	t.Run("discover returns adb error", func(t *testing.T) {
		originalEnableAndroidTargets := viper.GetBool(enableAndroidTargetsConfigKey)
		viper.Set(enableAndroidTargetsConfigKey, true)
		t.Cleanup(func() {
			viper.Set(enableAndroidTargetsConfigKey, originalEnableAndroidTargets)
		})

		adbErr := errors.New("adb unavailable")
		discoverer := &fakeAndroidTargetDiscoverer{err: adbErr}
		mtm := target.MockTargetManager{}
		mtm.On("ReadTargetConfig").Return(&engine_target.TargetConfig{Targets: map[string]engine_target.Target{}}, nil)
		err := listTargetsCommand(&bytes.Buffer{}, &mtm, discoverer, true)

		require.ErrorIs(t, err, adbErr)
		assert.Equal(t, 1, discoverer.calls)
	})
}
