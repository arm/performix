// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package ssh

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/clierror"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/grouping"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/serverconfig"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/client"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/clijson"
	"github.com/Arm-Debug/apap-cli/apap-engine/grpcserver"
	"github.com/Arm-Debug/apap-cli/apap-engine/ssh"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

var ListKeysCmd = NewListKeysCmd(client.NewAutostartClient())

func NewListKeysCmd(cc client.ClientConnector) *cobra.Command {

	formattedSearchDirs := util.Map(ssh.GetPrivateKeySearchDirs(), func(s string) string { return fmt.Sprintf("  - %s", s) })
	searchDirsAsString := strings.Join(formattedSearchDirs, "\n")

	cmd := &cobra.Command{
		Use:   "list-keys",
		Short: `Recursively list all SSH private keys found in standard SSH directories.`,
		Long: fmt.Sprintf(`Recursively list all SSH private keys found in the standard directories for the OS.
Output includes whether each key has a passphrase. Directories searched:
%s`, searchDirsAsString),
		Args: cobra.ExactArgs(0),
		Annotations: map[string]string{
			grouping.GroupAnnotation: grouping.GroupSSHSub,
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return listSSHKeys(cc, cmd.OutOrStdout())
		},
	}

	return cmd
}

func listSSHKeys(cc client.ClientConnector, out io.Writer) error {

	connector, err := cc.ApapClient(serverconfig.FromViperForBackground())
	if err != nil {
		return clierror.DecorateError(clierror.Common.ConnectFailed, err)
	}

	protoKeys, err := connector.ListPrivateSSHKeys(context.Background(), &emptypb.Empty{})
	if err != nil {
		return clierror.DecorateError(clierror.SSH.List.ListKeysFailed, err)
	}

	if viper.GetBool("json") {
		return clijson.MarshalJSONCLIResponse(out, protoKeys)
	} else {
		foundKeys := grpcserver.SSHKeyResponseFromProto(protoKeys)
		if len(foundKeys) == 0 {
			fmt.Fprintln(out, "No valid SSH private keys found.")
		} else {
			fmt.Fprintln(out, "SSH private keys found:")
			for _, key := range foundKeys {
				passphraseStatus := "no"
				if key.HasPassphrase {
					passphraseStatus = "yes"
				}
				fmt.Fprintf(out, "  - %s (passphrase: %s)\n", key.Path, passphraseStatus)
			}
		}
	}

	return nil
}
