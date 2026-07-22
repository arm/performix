// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"io"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/clierror"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/grouping"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/serverconfig"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/client"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/clijson"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/render"
)

var CloseRenderCmd = newCloseCmd(client.NewAutostartClient(), &render.CloseService{})

func newCloseCmd(cc client.ClientConnector, cs render.Closer) *cobra.Command {
	closeCmd := &cobra.Command{
		Use:   "close [session_id]",
		Short: "Close an active render session.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionID := args[0]
			return closeRender(cc, cs, sessionID, cmd.OutOrStdout())
		},
		Annotations: map[string]string{
			grouping.GroupAnnotation: grouping.GroupRenderSub,
		},
	}

	return closeCmd
}

func closeRender(cc client.ClientConnector, cs render.Closer, sessionID string, out io.Writer) error {
	jsonOut := viper.GetBool("json")

	connector, err := cc.ApapClient(serverconfig.FromViperForBackground())
	if err != nil {
		return clierror.DecorateError(clierror.Common.ConnectFailed, err)
	}

	err = cs.CloseRender(connector, sessionID)
	if err != nil {
		return clierror.DecorateError(clierror.Render.Close.CloseRenderFailed, err)
	}

	if jsonOut {
		err := clijson.MarshalJSONCLIResponse(out, struct{}{})
		if err != nil {
			return err
		}
	}

	return nil
}
