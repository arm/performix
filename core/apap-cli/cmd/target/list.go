// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package target

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/grouping"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/clijson"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	engine_target "github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

const ListUse = "list"

var TargetListCmd = newListCommand(engine_target.NewDefaultTargetManager(), adbAndroidTargetDiscoverer{})

type androidTargetDiscoverer interface {
	DiscoverAndroidTargets() ([]*engine_target.AndroidTarget, error)
}
type adbAndroidTargetDiscoverer struct{}

func (adbAndroidTargetDiscoverer) DiscoverAndroidTargets() ([]*engine_target.AndroidTarget, error) {
	stdout, _, err := conductor.NewExecADBRunner(viper.GetString("adb-path")).Run("devices")
	if err != nil {
		return nil, err
	}

	return parseADBDevices(stdout), nil
}

func parseADBDevices(stdout string) []*engine_target.AndroidTarget {
	var targets []*engine_target.AndroidTarget
	for _, line := range strings.Split(stdout, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == "device" {
			targets = append(targets, &engine_target.AndroidTarget{SerialNumber: fields[0]})
		}
	}
	return targets
}

func newListCommand(mtm target.TargetManagerService, discoverer androidTargetDiscoverer) *cobra.Command {
	var discover bool
	listCmd := &cobra.Command{
		Use:   ListUse,
		Short: "List all target configurations.",
		Long:  "List all target configurations to provide a detailed overview of the saved targets.",
		Args:  cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			return listTargetsCommand(cmd.OutOrStdout(), mtm, discoverer, discover)
		},
		Annotations: map[string]string{
			grouping.GroupAnnotation: grouping.GroupTargetSub,
		},
	}
	listCmd.Flags().BoolVar(&discover, "discover", false, "Discover connected Android targets using adb devices.")
	setDiscoverFlagVisibility(listCmd)
	return listCmd
}

func RefreshTargetListHelp() {
	setDiscoverFlagVisibility(TargetListCmd)
}

func setDiscoverFlagVisibility(cmd *cobra.Command) {
	if flag := cmd.Flags().Lookup("discover"); flag != nil {
		flag.Hidden = !viper.GetBool(enableAndroidTargetsConfigKey)
	}
}

type targetListing struct {
	engine_target.Target
	IsDefault bool
}

func (t targetListing) String() string {
	if t.Target == nil {
		return ""
	}
	return fmt.Sprint(t.Target)
}

func (t targetListing) MarshalJSON() ([]byte, error) {
	// Because we want to add "default" field and JSONTarget has a custom MarshalJSON, we can't just use the regular
	// reflection and JSON tags.
	//
	// We could add a subfield into JSONTarget, or make JSONTarget be a subfield of targetListing, but both of these
	// solutions would make unfriendly JSON containing cruft that exists just to make life easy in this serialization
	// code.

	cfg, err := engine_target.JSONTargetFromEngine(t.Target)
	if err != nil {
		return nil, err
	}

	jt, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}

	// Unmarshal the result into a map so we can add the extra field.
	var m map[string]interface{}
	if err := json.Unmarshal(jt, &m); err != nil {
		return nil, err
	}

	// Remove the name field from the target to avoid duplication
	// since it's already present as the key in the listing map.
	if value, ok := m["value"].(map[string]interface{}); ok {
		delete(value, "name")
	}

	// Add the default field.
	m["default"] = t.IsDefault

	return json.Marshal(m)
}

func listingFromConfig(config *engine_target.TargetConfig) map[string]targetListing {
	result := make(map[string]targetListing)
	for k, tgt := range config.Targets {
		if !viper.GetBool(enableAndroidTargetsConfigKey) {
			if _, ok := tgt.(*engine_target.AndroidTarget); ok {
				continue
			}
		}
		result[k] = targetListing{
			Target:    tgt,
			IsDefault: config.Default == k,
		}
	}

	return result
}

func listDiscoveredAndroidTargets(discoveredTargets []*engine_target.AndroidTarget) map[string]targetListing {
	result := make(map[string]targetListing)
	for _, tgt := range discoveredTargets {

		result[tgt.SerialNumber] = targetListing{
			Target:    tgt,
			IsDefault: false,
		}
	}
	return result
}

func listTargetsCommand(out io.Writer, targetService target.TargetManagerService, discoverer androidTargetDiscoverer, discover bool) error {
	jsonOutput := viper.GetBool("json")
	config, err := targetService.ReadTargetConfig()
	if err != nil {
		return err
	}

	if discover {
		if !viper.GetBool(enableAndroidTargetsConfigKey) {
			return message.New(message.CommonUnsupportedTargetType).WithMetadata(map[string]string{"targetType": "android"})
		}

		discoveredTargets, err := discoverer.DiscoverAndroidTargets()
		if err != nil {
			// TODO: Wrap ADB discovery failures in a user-facing message.Message and preserve the raw error as cause context.
			return err
		}

		listing := listDiscoveredAndroidTargets(discoveredTargets)
		if jsonOutput {
			return clijson.MarshalJSONCLIResponse(out, listing)
		}
		printTargetListing(listing, out)
		return nil
	}

	listing := listingFromConfig(config)

	if jsonOutput {
		return clijson.MarshalJSONCLIResponse(out, listing)
	} else {
		printTargetListing(listing, out)
	}

	return nil
}

func printTargetListing(listing map[string]targetListing, out io.Writer) {
	itemCount := len(listing)
	var isDefault string
	writer := tabwriter.NewWriter(out, 1, 1, 2, ' ', tabwriter.StripEscape)
	if itemCount > 0 {
		showTypeColumn := viper.GetBool(enableAndroidTargetsConfigKey)
		if showTypeColumn {
			fmt.Fprintln(writer, "name\tdefault\ttype\thost_key_policy\tvalue")
		} else {
			fmt.Fprintln(writer, "name\tdefault\thost_key_policy\tvalue")
		}
		keys := util.CopyKeysSlice(listing)
		slices.Sort(keys)
		for _, name := range keys {
			item := listing[name]
			if item.IsDefault {
				isDefault = "yes"
			} else {
				isDefault = "no"
			}

			targetStr := item.String()
			targetType := ""
			var hostKeyPolicy string
			switch v := item.Target.(type) {
			case *engine_target.SSHTarget:
				targetType = "ssh"
				hostKeyPolicy = engine_target.HostKeyPolicyToString[v.ReadKnownHostsPolicy()]
				targetStr = formatTargetWithAuth(v)
			case *engine_target.AndroidTarget:
				targetType = "android"
				hostKeyPolicy = "n/a"
				targetStr = formatAndroidTarget(v)
			case *engine_target.LocalTarget:
				targetType = "local"
			default:
				hostKeyPolicy = ""
			}

			if showTypeColumn {
				fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\n", name, isDefault, targetType, hostKeyPolicy, targetStr)
			} else {
				fmt.Fprintf(writer, "%s\t%s\t%s\t%s\n", name, isDefault, hostKeyPolicy, targetStr)
			}
		}
		fmt.Fprintf(writer, "\n")
	}
	writer.Flush()
	s := "targets"
	if itemCount == 1 {
		s = "target"
	}
	fmt.Fprintf(out, "%d %s found\n", itemCount, s)
}

// formatAndroidTarget returns a string representation of an Android target.
func formatAndroidTarget(tgt *engine_target.AndroidTarget) string {
	if tgt == nil {
		return ""
	}
	if tgt.DeviceIPAddress == nil || *tgt.DeviceIPAddress == "" {
		return tgt.SerialNumber
	}
	return fmt.Sprintf("%s@%s", tgt.SerialNumber, *tgt.DeviceIPAddress)
}

// formatTargetWithAuth returns a string representation of the target including authentication information.
func formatTargetWithAuth(tgt *engine_target.SSHTarget) string {
	if tgt == nil || len(tgt.Jumps) == 0 {
		return ""
	}
	lastJump := tgt.LastJump()
	var viaStr string
	if len(tgt.Jumps) > 1 {
		parts := util.Map(
			tgt.Jumps[:len(tgt.Jumps)-1],
			func(config engine_target.SSHHostConfig) string { return formatHostWithAuth(config) },
		)
		slices.Reverse(parts)
		viaParts := strings.Join(parts, ", ")
		viaStr = fmt.Sprintf(" (via %s)", viaParts)
	}
	return fmt.Sprintf("%s%s", formatHostWithAuth(lastJump), viaStr)
}

// formatHostWithAuth returns a string representation of the host including authentication information.
func formatHostWithAuth(host engine_target.SSHHostConfig) string {
	display := host.DisplayString()
	switch host.AuthMethod {
	case engine_target.SSHAuthMethodPassword:
		return fmt.Sprintf("%s [password]", display)
	case engine_target.SSHAuthMethodKey:
		fallthrough
	default:
		if host.PrivateKeyFilename != "" {
			return fmt.Sprintf("%s [key=%s]", display, host.PrivateKeyFilename)
		}
		return fmt.Sprintf("%s [key]", display)
	}
}
