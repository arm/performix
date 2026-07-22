// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/grouping"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/serverconfig"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/client"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/clijson"
	promptsvc "github.com/Arm-Debug/apap-cli/apap-cli/service/prompt"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

var DeleteCmd = NewDeleteCmd(client.NewAutostartClient(), run.DeleteService{})

type deleteCliParams struct {
	Runs      []string
	DeleteAll bool
}

type runDeletionStatus struct {
	id  string
	err error
}

var deleteAllPrompter = promptsvc.PromptLine

func NewDeleteCmd(cc client.ClientConnector, d run.Deleter) *cobra.Command {
	cliParams := deleteCliParams{}

	cmd := &cobra.Command{
		Use:   "delete [run_id...]",
		Short: `Delete runs by ID, or delete all runs with --all (confirmation required).`,
		Args: func(cmd *cobra.Command, args []string) error {
			cliParams.Runs = args
			if cliParams.DeleteAll {
				return validateDeleteParams(&cliParams)
			}
			return cobra.MinimumNArgs(1)(cmd, args)
		},
		Annotations: map[string]string{
			grouping.GroupAnnotation: grouping.GroupRunSub,
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if cliParams.DeleteAll {
				catalogMsg, err := message.LookupMessage(message.New(message.CliCmdRunDeleteConfirmAllPrompt))
				if err != nil {
					return err
				}
				response, err := deleteAllPrompter(catalogMsg.Message + " ")
				if err != nil {
					return err
				}
				if !strings.EqualFold(strings.TrimSpace(response), "yes") {
					return nil
				}
			}
			return delete(cc, d, &cliParams, cmd.OutOrStdout())
		},
	}

	cmd.Flags().BoolVar(&cliParams.DeleteAll, "all", false, "Delete all runs (prompts for confirmation).")
	return cmd
}

func delete(cc client.ClientConnector, d run.Deleter, cliParams *deleteCliParams, out io.Writer) error {
	jsonOutput := viper.GetBool("json")

	connector, err := cc.ApapClient(serverconfig.FromViperForBackground())
	if err != nil {
		return err
	}

	request := &apapproto.DeleteRunsRequest{Ids: cliParams.Runs, DeleteAll: cliParams.DeleteAll}

	rsp, err := d.DeleteRuns(connector, request)
	if err != nil {
		return err
	}
	jsonRsp := clijson.DeleteRunsResponseToJSON(rsp)

	nonNilErrs := getNonNilErrs(rsp.Statuses)

	numToDelete := len(rsp.Statuses)
	numFailures := len(nonNilErrs)
	numSuccesses := numToDelete - numFailures

	if numFailures == 0 {
		if jsonOutput {
			return clijson.MarshalJSONCLIResponse(out, jsonRsp)
		}
		return nil
	}

	if numToDelete == 1 {
		// Exactly one run was requested to be deleted, and it failed - just display this run's individual message
		failureMsg := nonNilErrs[0].err
		if jsonOutput {
			return clijson.MarshalJSONCLIResponseWithError(out, jsonRsp, failureMsg)
		}
		return failureMsg
	}

	var failureMsg message.Message
	metadata := map[string]string{"numToDelete": fmt.Sprintf("%v", numToDelete)}
	if numSuccesses == 0 {
		failureMsg = message.New(message.CliCmdRunDeleteDeletionFailedNoSuccesses).WithMetadata(metadata)
	} else {
		metadata["numFailures"] = fmt.Sprintf("%v", numFailures)
		failureIDs := util.Map(nonNilErrs, func(status runDeletionStatus) string { return status.id })
		metadata["failureIDs"] = util.DisplayErrorStringSlice(failureIDs)
		if numSuccesses == 1 {
			failureMsg = message.New(message.CliCmdRunDeleteDeletionFailedSingleSuccess).WithMetadata(metadata)
		} else {
			metadata["numSuccesses"] = fmt.Sprintf("%v", numSuccesses)
			failureMsg = message.New(message.CliCmdRunDeleteDeletionFailedPluralSuccesses).WithMetadata(metadata)
		}
	}

	if jsonOutput {
		return clijson.MarshalJSONCLIResponseWithError(out, jsonRsp, failureMsg)
	}

	printFailures(out, nonNilErrs)
	return failureMsg
}

func getNonNilErrs(statuses []*apapproto.RunDeletionStatus) []runDeletionStatus {
	filtered := util.Filter(statuses, func(i int) bool {
		return statuses[i].Error != nil
	})
	return util.Map(filtered, func(status *apapproto.RunDeletionStatus) runDeletionStatus {
		return runDeletionStatus{
			id:  status.Id,
			err: message.ReconstructFromChain(status.Error),
		}
	})
}

func printFailures(out io.Writer, failureStatuses []runDeletionStatus) {
	indent := 2
	for _, status := range failureStatuses {
		fmt.Fprintf(out, "%v:\n", status.id)
		clijson.HandlePlaintextCLIErrorWithIndent(out, status.err, indent)
		fmt.Fprintln(out)
	}
}

func validateDeleteParams(cliParams *deleteCliParams) error {
	if cliParams.DeleteAll && len(cliParams.Runs) > 0 {
		metadata := map[string]string{"flag1": "--all", "flag2": "run IDs"}
		return message.New(message.CliCmdValidationMutuallyExclusiveFlags).WithMetadata(metadata)
	}
	return nil
}
