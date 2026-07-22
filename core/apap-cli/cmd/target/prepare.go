// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package target

import (
	"context"
	"io"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/grouping"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/serverconfig"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/client"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/clijson"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/target"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/targetlogin"
	"github.com/Arm-Debug/apap-cli/apap-engine/grpcserver"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	engine_target "github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

const PrepareUse = "prepare"

var TargetPrepareCmd = newTargetPrepareCommand(
	client.NewAutostartClient(),
	engine_target.NewDefaultTargetManager(),
	targetlogin.NewDefaultTargetLoginService(),
)
var check bool

func newTargetPrepareCommand(cc client.ClientConnector, targetService target.TargetManagerService, loginService targetlogin.TargetLoginService) *cobra.Command {
	var target string
	cmd := &cobra.Command{
		Use:   PrepareUse,
		Short: "Deploy the mandatory tools to the target.",
		Long: `Deploy the mandatory tools to the target. These are required in order to run recipes,
 to perform recipe support checks, and to query the target system.`,
		Args: cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			err := prepareTarget(cc, targetService, loginService, cmd.OutOrStdout(), target)
			return err
		},
		Annotations: map[string]string{
			grouping.GroupAnnotation: grouping.GroupTargetSub,
		},
	}

	cmd.Flags().BoolVar(&check, "check", false, "Check whether the specified target is already prepared")
	cmd.Flags().StringVar(&target, "target", "", "Specify the target for the specified action.")
	return cmd
}

func prepareTarget(cc client.ClientConnector, targetService target.TargetManagerService, loginService targetlogin.TargetLoginService, out io.Writer, target string) error {
	targetName := target
	targetDefinition, err := targetService.GetTarget(targetName)
	if err != nil {
		return err
	}

	client, err := cc.ApapClient(serverconfig.FromViperForBackground())
	if err != nil {
		return err
	}

	if err := loginService.LoginToTarget(context.Background(), targetDefinition, serverconfig.FromViperForBackground()); err != nil {
		return err
	}

	var deploymentType = apapproto.ToolDeploy_AUTO
	if check {
		deploymentType = apapproto.ToolDeploy_CHECK
	}
	protoTarget := grpcserver.TargetToProto(targetDefinition)
	prepareReq := &apapproto.TargetPrepareRequest{Target: protoTarget, DeploymentType: deploymentType}
	response, err := client.TargetPrepare(context.Background(), prepareReq)
	if err != nil {
		return err
	}

	if viper.GetBool("json") {
		return clijson.MarshalJSONCLIResponse(out, response.Result.String())
	}

	switch response.Result {
	case apapproto.TargetPrepareResult_DEPLOYED:
		return nil
	case apapproto.TargetPrepareResult_DEPLOY:
		return message.New(message.CliCmdTargetPrepareRequired)
	case apapproto.TargetPrepareResult_NO_ACTION:
		return message.New(message.CliCmdTargetPrepareAlreadyPrepared)
	}

	return nil
}
