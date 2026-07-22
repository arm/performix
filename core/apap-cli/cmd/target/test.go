// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package target

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/completion"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/grouping"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/serverconfig"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/client"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/clijson"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/target"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/targetlogin"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	engine_target "github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

var TargetTestCmd = newTargetTestCmd(
	client.NewAutostartClient(),
	engine_target.NewDefaultTargetManager(),
	&target.ConcreteTargetTester{},
	targetlogin.NewDefaultTargetLoginService(),
)

type targetTester interface {
	TestTarget(ctx context.Context, client apapproto.ApapClient, target engine_target.Target) (target.TestTargetResponse, error)
}

const TargetTestUse = "test"

func getTargetName(args []string, target string) string {
	if len(args) > 0 {
		return args[0]
	}

	return target
}

func newTargetTestCmd(cc client.ClientConnector, ts target.TargetManagerService, dt targetTester, loginService targetlogin.TargetLoginService) *cobra.Command {
	var target string
	targetTestCmd := &cobra.Command{
		Use:   TargetTestUse,
		Short: "Test that a connection can be established to the target.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return targetTestCommand(cmd, cmd.OutOrStdout(), args, cc, ts, dt, loginService, target)
		},
		Annotations: map[string]string{
			grouping.GroupAnnotation: grouping.GroupTargetSub,
		},
	}
	targetTestCmd.ValidArgsFunction = completion.CompleteTargetNames

	targetTestCmd.Flags().StringVar(&target, "target", "", "Specify the target for the specified action.")
	return targetTestCmd
}

func targetTestCommand(cmd *cobra.Command, out io.Writer, args []string, cc client.ClientConnector, ts target.TargetManagerService, dt targetTester, loginService targetlogin.TargetLoginService, targetFlagName string) error {
	outputAsJSON := viper.GetBool("json")
	targetName := getTargetName(args, targetFlagName)
	if targetName == "" {
		var err error
		targetName, err = ts.GetDefaultTargetName()
		if err != nil {
			return err
		}
	}
	target, err := ts.GetTarget(targetName)
	if err != nil {
		return err
	}
	if _, ok := target.(*engine_target.AndroidTarget); ok && !viper.GetBool(enableAndroidTargetsConfigKey) {
		return message.New(message.CommonUnsupportedTargetType).WithMetadata(map[string]string{"targetType": "android"})
	}

	client, err := cc.ApapClient(serverconfig.FromViperForBackground())
	if err != nil {
		return err
	}

	if err := loginService.LoginToTarget(context.Background(), target, serverconfig.FromViperForBackground()); err != nil {
		return err
	}

	if !viper.GetBool("json") {
		fmt.Fprintf(out, "Testing target: %v\n", targetName)
	}

	testResponse, err := dt.TestTarget(cmd.Context(), client, target)
	if err != nil {
		return err
	}

	if outputAsJSON {
		return clijson.MarshalJSONCLIResponse(cmd.OutOrStdout(), clijson.TestTargetResponseToJSON(testResponse))
	}

	indent := strings.Repeat(" ", 2)
	// CLI output
	fmt.Fprintf(out, "%vTarget Connection - ", indent)
	if testResponse.ConnectionStatus.ConnectionStatus != apapproto.ConnectionStatus_CONNECTION_STATUS_OK {
		fmt.Fprintln(out, "Fail")
	} else {
		fmt.Fprintln(out, "Success")
	}

	var errorSlice []error
	for _, err = range []error{testResponse.ConnectionStatus.Error} {
		if err != nil {
			errorSlice = append(errorSlice, err)
		}
	}

	printTestReport(out, errorSlice)
	return nil
}

func printTestReport(out io.Writer, errors []error) {
	if len(errors) == 0 {
		fmt.Fprintf(out, "\nTest completed with 0 errors.\n")
		return
	}
	indent := 2

	fmt.Fprintf(out, "\nTest completed with %v error(s):\n\n", len(errors))
	for i, err := range errors {
		if err == nil {
			continue
		}
		clijson.HandlePlaintextCLIErrorWithIndent(out, err, indent)

		// Print divider only if there's another error to come below
		if i < len(errors)-1 {
			fmt.Fprintf(out, "\n")
		}
	}
}
