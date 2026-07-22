// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/completion"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/grouping"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/serverconfig"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/client"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/clijson"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/run"
	"github.com/Arm-Debug/apap-cli/apap-cli/utils"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	engine_run "github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

var UpdateCmd = NewUpdateCmd(client.NewAutostartClient(), run.UpdateService{})

type hostSourceCodePath struct {
	HostSourceCodePaths engine_run.HostSourceCodePath
}

type updateCliParams struct {
	Run                string
	HostSourceCodePath hostSourceCodePath
}

func NewUpdateCmd(cc client.ClientConnector, r run.Updater) *cobra.Command {
	var sourceCodePaths string
	cliParams := updateCliParams{}

	cmd := &cobra.Command{
		Use:   "update [run_id] [--source <path>]",
		Short: "Update a run.",
		Long:  "Update an existing run by its ID.",
		Annotations: map[string]string{
			grouping.GroupAnnotation: grouping.GroupRunSub,
		},
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cliParams.Run = args[0]
			if !cmd.Flags().Changed("source") {
				if viper.GetBool("json") {
					return clijson.MarshalJSONCLIResponse(cmd.OutOrStdout(), &apapproto.UpdateRunsResponse{})
				}
				return nil
			}
			// Split the source code paths by the OS path list separator.
			cliParams.HostSourceCodePath.HostSourceCodePaths.Paths = utils.FilterPaths(strings.Split(sourceCodePaths, string(os.PathListSeparator)))
			return updateRun(cc, r, &cliParams, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&sourceCodePaths, "source", "", "Specifies the host-based source code path(s) that will be used for source code attribution. Specifying \"\" will remove any previously set source code paths")
	cmd.ValidArgsFunction = completion.CompleteRunIDs

	return cmd
}

func updateRun(cc client.ClientConnector, r run.Updater, cliParams *updateCliParams, out io.Writer) error {
	jsonOutput := viper.GetBool("json")

	runID := &apapproto.RunId{Value: cliParams.Run}
	operation := &apapproto.RunUpdateOperation{
		Operation: &apapproto.RunUpdateOperation_SetHostSourceCodePaths{
			SetHostSourceCodePaths: &apapproto.SetHostSourceCodePaths{
				Value: &apapproto.HostSourceCodePaths{
					Paths: cliParams.HostSourceCodePath.HostSourceCodePaths.Paths,
				},
			},
		},
	}
	updateRequest := &apapproto.UpdateRunsRequest{
		RunIds: []*apapproto.RunId{runID},
		Patch:  &apapproto.RunUpdatePatch{Operations: []*apapproto.RunUpdateOperation{operation}},
	}

	connector, err := cc.ApapClient(serverconfig.FromViperForBackground())
	if err != nil {
		return err
	}

	response, err := r.UpdateRuns(connector, updateRequest)
	if err != nil {
		return err
	}
	failureStatuses := getNonNilUpdateErrs(response.GetStatuses())
	if len(failureStatuses) == 1 {
		updateErr := failureStatuses[0].err
		if jsonOutput {
			if err := clijson.MarshalJSONCLIResponseWithError(out, response, updateErr); err != nil {
				return err
			}
			return errors.Join(clijson.ErrorAlreadyHandled, updateErr)
		}
		return updateErr
	}
	if len(failureStatuses) > 1 {
		failureMsg := updateRunsFailureMessage(len(response.GetStatuses()), failureStatuses)
		if jsonOutput {
			if err := clijson.MarshalJSONCLIResponseWithError(out, response, failureMsg); err != nil {
				return err
			}
			return errors.Join(clijson.ErrorAlreadyHandled, failureMsg)
		}
		printUpdateFailures(out, failureStatuses)
		return failureMsg
	}

	if jsonOutput {
		return clijson.MarshalJSONCLIResponse(out, response)
	}
	return nil
}

type runUpdateStatus struct {
	id  string
	err error
}

func getNonNilUpdateErrs(statuses []*apapproto.RunUpdateStatus) []runUpdateStatus {
	filtered := util.Filter(statuses, func(i int) bool {
		return statuses[i].Error != nil
	})
	return util.Map(filtered, func(status *apapproto.RunUpdateStatus) runUpdateStatus {
		return runUpdateStatus{
			id:  status.Id,
			err: message.ReconstructFromChain(status.Error),
		}
	})
}

func updateRunsFailureMessage(numToUpdate int, failureStatuses []runUpdateStatus) message.Message {
	failureIDs := util.Map(failureStatuses, func(status runUpdateStatus) string { return status.id })
	return message.New(message.CliCmdRunUpdateUpdateFailedMultipleRuns).WithMetadata(map[string]string{
		"numToUpdate": fmt.Sprintf("%v", numToUpdate),
		"numFailures": fmt.Sprintf("%v", len(failureStatuses)),
		"failureIDs":  util.DisplayErrorStringSlice(failureIDs),
	})
}

func printUpdateFailures(out io.Writer, failureStatuses []runUpdateStatus) {
	indent := 2
	for _, status := range failureStatuses {
		fmt.Fprintf(out, "%v:\n", status.id)
		clijson.HandlePlaintextCLIErrorWithIndent(out, status.err, indent)
		fmt.Fprintln(out)
	}
}
