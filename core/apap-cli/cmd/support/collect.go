// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package support

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/grouping"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/serverconfig"
	"github.com/Arm-Debug/apap-cli/apap-cli/service"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/client"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/clijson"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
	"github.com/Arm-Debug/apap-cli/atperf-version/versions"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

// By default, support packages will be saved to the configured data directory
var defaultOutputDir = filepath.Join(serverconfig.DefaultDataDir, "support")

// The number of recent log files to include in the support package
const defaultLogCount = 3

var CollectCmd = NewCollectCmd(client.NewAutostartClient())

type collectCLIParams struct {
	runIDs    []string
	OutputDir string
	LogCount  uint32
}

func NewCollectCmd(cc client.ClientConnector) *cobra.Command {
	cliParams := &collectCLIParams{}
	cmd := &cobra.Command{
		Use:   "collect [run_id...]",
		Short: fmt.Sprintf(`Create a support package containing %s logs, host system information, and optional run data.`, terminology.GetProductFullName()),
		Long: fmt.Sprintf(`Collect diagnostic information into a support package that can be uploaded to an Arm support case.
Support packages will be saved to the configured data directory for local storage, or to the specified output directory if provided.

By default, the support package will contain:
- Host system information (operating system details, hostname, IP address)
- %s diagnostic logs and configuration data

Optionally, you can specify one or more run IDs to include in the support package. Each run contains:
- Run information (workload details, recipe parameters, results)
- Logs and data capture files for any tools invoked during the recipe (which may contain workload symbol names)
- Target system information (operating system details, hardware details)
- A copy of the recipe used

Logs can contain sensitive information such as usernames, file paths, or system-specific data.
If you are working in a sensitive environment, review and remove any information you do not wish to share.`, terminology.GetProductFullName()),
		Args: func(cmd *cobra.Command, args []string) error {
			// For now the only expected args are the run IDs
			cliParams.runIDs = args
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, stop := service.ContextWithInterrupt(cmd.Context())
			defer stop()
			return collect(ctx, cmd.OutOrStdout(), cc, cliParams)
		},
		Annotations: map[string]string{
			grouping.GroupAnnotation: grouping.GroupSupportSub,
		},
	}
	cmd.Flags().StringVar(&cliParams.OutputDir, "output-dir", defaultOutputDir, "Directory where the support package should be written to.")
	cmd.Flags().Uint32Var(&cliParams.LogCount, "log-count", defaultLogCount, "Number of recent log files to include.")
	return cmd
}

type collectJSON struct {
	PackagePath        string `json:"package_path"`
	PackageSizeBytes   uint64 `json:"package_size_bytes"`
	PackageSizeDisplay string `json:"package_size_display"`
}

func collect(ctx context.Context, out io.Writer, cc client.ClientConnector, cliParams *collectCLIParams) error {
	request := &apapproto.CreateSupportPackageRequest{
		RunIds:          util.Map(cliParams.runIDs, func(id string) *apapproto.RunId { return &apapproto.RunId{Value: id} }),
		OutputDirectory: cliParams.OutputDir,
		CliVersion:      versions.GetVersion(),
		GuiVersion:      nil, // GUI version is only specified when support packages are collected using the GUI client
		LogCount:        cliParams.LogCount,
	}

	apapClient, err := cc.ApapClient(serverconfig.FromViperForBackground())
	if err != nil {
		// Check here so cancelling during autostart connection returns the user-facing cancellation error.
		if cancelErr := message.CancellationError(ctx, err); cancelErr != nil {
			return cancelErr
		}
		return err
	}
	if err := message.CancellationError(ctx, nil); err != nil {
		return err
	}

	response, err := apapClient.CreateSupportPackage(ctx, request)
	if err != nil {
		if cancelErr := message.CancellationError(ctx, err); cancelErr != nil {
			return cancelErr
		}
		return err
	}

	sizeDisplay := util.FormatBytesIEC(response.GetPackageSizeBytes())

	if viper.GetBool("json") {
		payload := collectJSON{
			PackagePath:        response.GetPackagePath(),
			PackageSizeBytes:   response.GetPackageSizeBytes(),
			PackageSizeDisplay: sizeDisplay,
		}
		return clijson.MarshalJSONCLIResponse(out, payload)
	}

	successMsg := message.New(message.EngineSupportCollectSuccess).WithMetadata(map[string]string{
		"path":        response.GetPackagePath(),
		"sizeBytes":   fmt.Sprintf("%d", response.GetPackageSizeBytes()),
		"sizeDisplay": sizeDisplay,
	})

	return successMsg
}
