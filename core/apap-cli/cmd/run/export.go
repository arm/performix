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
	"github.com/Arm-Debug/apap-cli/apap-cli/service/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

var ExportCmd = NewExportCmd(client.NewAutostartClient(), run.ExportService{})

func NewExportCmd(cc client.ClientConnector, exportService run.Exporter) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export [run_id] [target_directory]",
		Short: "Export a run to a specified directory.",
		Long:  "Export a previous run to the specified directory. The run is exported as a .zip file.",
		Annotations: map[string]string{
			grouping.GroupAnnotation: grouping.GroupRunSub,
		},
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return exportRun(args[0], args[1], exportService, cc, cmd.OutOrStdout())
		},
	}

	return cmd
}

func exportRun(runID string, targetDirectory string, exporter run.Exporter, cc client.ClientConnector, out io.Writer) error {
	id := apapproto.RunId{Value: runID}
	abTargetDir, err := filepath.Abs(targetDirectory)
	if err != nil {
		return message.New(message.CommonUnknownError).WithCause(err)
	}

	client, err := cc.ApapClient(serverconfig.FromViperForBackground())
	if err != nil {
		return err
	}

	request := apapproto.RunExportRequest{RunId: &id, TargetDirectory: abTargetDir}

	response, err := exporter.ExportRun(client, &request)
	if err != nil {
		return err
	}

	if viper.GetBool("json") {
		return clijson.MarshalJSONCLIResponse(out, response)
	}

	fmt.Printf("Successfully exported run %s to %s\n", runID, targetDirectory)
	return nil
}
