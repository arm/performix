// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/clierror"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/grouping"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/serverconfig"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/client"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/clijson"
	"github.com/Arm-Debug/apap-cli/api/apapproto"
)

func NewConfigGetCmd(cc client.ClientConnector) *cobra.Command {
	return newConfigTelemetryCmd(cc, false)
}

func NewConfigSetCmd(cc client.ClientConnector) *cobra.Command {
	return newConfigTelemetryCmd(cc, true)
}

func newConfigTelemetryCmd(cc client.ClientConnector, set bool) *cobra.Command {
	var enabled bool
	cmd := &cobra.Command{
		Use:     "get",
		Short:   "Read engine configuration settings.",
		Example: "  apx config get --telemetry",
		Args:    cobra.NoArgs,
		Annotations: map[string]string{
			grouping.GroupAnnotation: grouping.GroupConfigSub,
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			engine, err := cc.ApapClient(serverconfig.FromViperForBackground())
			if err != nil {
				return clierror.DecorateError(clierror.Common.ConnectFailed, err)
			}
			if !set {
				preference, err := engine.GetProductTelemetryPreference(cmd.Context(), &emptypb.Empty{})
				if err != nil {
					return err
				}
				enabled = preference.Enabled
			} else {
				if _, err := engine.SetProductTelemetryPreference(cmd.Context(), &apapproto.ProductTelemetryPreference{Enabled: enabled}); err != nil {
					return err
				}
			}
			if viper.GetBool("json") {
				return clijson.MarshalJSONCLIResponse(cmd.OutOrStdout(), enabled)
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), enabled)
			return err
		},
	}
	flagHelp := "Read the telemetry preference"
	if set {
		cmd.Use = "set"
		cmd.Short = "Save engine configuration settings."
		cmd.Example = "  apx config set --telemetry=true\n  apx config set --telemetry=false"
		flagHelp = "Enable or disable telemetry and save the preference"
	}
	cmd.Flags().BoolVar(&enabled, "telemetry", false, flagHelp)
	_ = cmd.MarkFlagRequired("telemetry")
	return cmd
}
