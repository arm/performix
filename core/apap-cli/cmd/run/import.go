// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/grouping"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/serverconfig"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/client"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/clijson"
	run "github.com/Arm-Debug/apap-cli/apap-cli/service/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

var ImportCmd = NewImportCmd(client.NewAutostartClient(), run.ImportService{})

func NewImportCmd(cc client.ClientConnector, importService run.Importer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import [run_path]",
		Short: "Import a run from the specified location.",
		Annotations: map[string]string{
			grouping.GroupAnnotation: grouping.GroupRunSub,
		},
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return importRun(args[0], importService, cc, cmd.OutOrStdout())
		},
	}

	return cmd
}

func importRun(runPath string, importer run.Importer, cc client.ClientConnector, out io.Writer) error {
	absRunPath, err := filepath.Abs(runPath)
	if err != nil {
		return message.New(message.CommonUnknownError).WithCause(err)
	}

	client, err := cc.ApapClient(serverconfig.FromViperForBackground())
	if err != nil {
		return err
	}

	request := apapproto.RunImportRequest{ExternalRunPath: absRunPath}
	response, err := importer.ImportRun(client, &request)
	if err != nil {
		return err
	}

	if viper.GetBool("json") {
		return clijson.MarshalJSONCLIResponse(out, response)
	}

	fmt.Printf("Successfully imported %s as run %s\n", runPath, response.NewId.Value)
	return nil
}
