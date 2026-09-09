// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// This file holds the shared CLI model, engine connection, and status helpers
// used by the MCP client subcommands.
package mcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/serverconfig"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/client"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/clijson"
	installerservice "github.com/Arm-Debug/apap-cli/apap-cli/service/mcpclientinstaller"
	"github.com/Arm-Debug/apap-cli/apap-engine/mcpclientinstaller/clientids"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

// mcpClientDependencies keeps Cobra commands independent from the production
// gRPC connection, allowing command behaviour to be tested without a daemon.
type mcpClientDependencies struct {
	connect func() (apapproto.ApapClient, func() error, error)
	service installerservice.Service
}

type mcpClientOutput struct {
	ID                string                `json:"id"`
	Name              string                `json:"name"`
	Detected          bool                  `json:"detected"`
	RegistrationState string                `json:"registration_state"`
	RegistrationError *clijson.ErrorPayload `json:"registration_error,omitempty"`
	ConfigurationPath string                `json:"configuration_path,omitempty"`
	ExecutablePath    string                `json:"executable_path,omitempty"`
	Error             string                `json:"error,omitempty"`
}

type mcpClientDoctorOutput struct {
	ClientID   string                 `json:"client_id"`
	ClientName string                 `json:"client_name"`
	Ready      bool                   `json:"ready"`
	Checks     []mcpClientDoctorCheck `json:"checks"`
}

type mcpClientDoctorCheck struct {
	ID          string                `json:"id"`
	Status      string                `json:"status"`
	Message     string                `json:"message"`
	Diagnostic  *message.ErrorPayload `json:"diagnostic,omitempty"`
	Details     []string              `json:"details,omitempty"`
	Remediation []string              `json:"remediation,omitempty"`
}

type mcpClientStatusOutput struct {
	Server  *apapproto.MCPServerLaunchConfiguration `json:"server"`
	Clients []mcpClientOutput                       `json:"clients"`
}

type mcpClientOperationOutput struct {
	Client  mcpClientOutput `json:"client"`
	Outcome string          `json:"outcome,omitempty"`
}

// addMCPClientCommands exposes each stable client ID as a Cobra subcommand so
// Cobra can provide its normal help and spelling suggestions before gRPC.
func addMCPClientCommands(
	command *cobra.Command,
	description func(string) string,
	run func(*cobra.Command, string) error,
) {
	for _, clientID := range clientids.All() {
		clientID := clientID
		command.AddCommand(&cobra.Command{
			Use:   clientID,
			Short: description(clientID),
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return run(cmd, clientID)
			},
		})
	}
}

// rejectUnexpectedMCPArguments handles unknown client commands for parents
// which also run an operation themselves. Cobra otherwise treats the unknown
// command as a positional argument and omits its usual command suggestions.
func rejectUnexpectedMCPArguments(cmd *cobra.Command, args []string) error {
	if err := cobra.NoArgs(cmd, args); err != nil {
		return fmt.Errorf("%w%s", err, formatMCPClientSuggestions(cmd, args[0]))
	}
	return nil
}

func formatMCPClientSuggestions(cmd *cobra.Command, arg string) string {
	suggestions := cmd.SuggestionsFor(arg)
	if len(suggestions) == 0 {
		return ""
	}
	return "\n\nDid you mean this?\n\t" + strings.Join(suggestions, "\n\t") + "\n"
}

// defaultMCPClientDependencies creates the shared-daemon connection used by
// short-lived client management commands. MCP start remains separate because
// it hosts the protocol server for the lifetime of an AI client session.
func defaultMCPClientDependencies() mcpClientDependencies {
	connector := client.NewAutostartClientWithOutput(io.Discard)
	return mcpClientDependencies{
		connect: func() (apapproto.ApapClient, func() error, error) {
			config := serverconfig.FromViperForBackground()
			engine, err := connector.ApapClient(config)
			if err != nil {
				return nil, nil, err
			}
			return engine, func() error { return nil }, nil
		},
		service: installerservice.GRPCService{},
	}
}

func explicitMCPClient(args []string) string {
	if len(args) == 1 {
		return args[0]
	}
	return ""
}

func validateMCPCommandTarget(args []string, all bool) error {
	if all && len(args) != 0 {
		return message.New(message.CliCmdMcpCommonClientAndAll)
	}
	return nil
}

func clientIDs(statuses []*apapproto.MCPClientStatus) []string {
	statuses = sortedMCPClients(statuses)
	ids := make([]string, 0, len(statuses))
	for _, status := range statuses {
		ids = append(ids, status.ClientId)
	}
	return ids
}

// connectAndList asks the engine to inspect either all clients or one named
// client. The engine performs detection at request time; this CLI keeps no
// discovery cache.
func connectAndList(
	ctx context.Context,
	deps mcpClientDependencies,
	clientID string,
) (apapproto.ApapClient, *apapproto.MCPClientListing, func() error, error) {
	engineClient, shutdown, err := deps.connect()
	if err != nil {
		return nil, nil, nil, err
	}
	var listing *apapproto.MCPClientListing
	if clientID == "" {
		listing, err = deps.service.List(ctx, engineClient)
	} else {
		listing, err = deps.service.Get(ctx, engineClient, clientID)
	}
	if err != nil {
		return nil, nil, nil, errors.Join(err, shutdown())
	}
	return engineClient, listing, shutdown, nil
}

// connectAndListWithProgress shows an indeterminate tracker only for an
// all-client scan. Named commands intentionally skip it because they normally
// complete fast enough that a transient tracker would be distracting.
func connectAndListWithProgress(
	cmd *cobra.Command,
	deps mcpClientDependencies,
	clientID string,
	progressMessageCode message.MessageCode,
	keepDetectedHeading bool,
) (apapproto.ApapClient, *apapproto.MCPClientListing, func() error, error) {
	if clientID != "" {
		return connectAndList(cmd.Context(), deps, clientID)
	}

	progressWriter, progressTrackers, progressDone, err := startMCPStatusProgress(
		cmd,
		progressMessageCode,
	)
	if err != nil {
		return nil, nil, nil, err
	}
	engineClient, listing, shutdown, listErr := connectAndList(cmd.Context(), deps, clientID)
	if progressWriter != nil {
		if listErr == nil && keepDetectedHeading {
			progressTrackers[0].UpdateMessage(
				mcpMessageText(message.CliCmdMcpStatusDetectedHeading),
			)
		}
		markMCPProgressComplete(progressTrackers, 0, listErr != nil)
		<-progressDone
		if listErr == nil && !keepDetectedHeading {
			fmt.Fprint(cmd.ErrOrStderr(), text.CursorUp.Sprint(), text.EraseLine.Sprint())
		}
	}
	return engineClient, listing, shutdown, listErr
}

func clientOutput(status *apapproto.MCPClientStatus) mcpClientOutput {
	return clientOutputWithDiagnostic(status, mcpClientDiagnostic(status))
}

func clientOutputWithDiagnostic(
	status *apapproto.MCPClientStatus,
	diagnostic error,
) mcpClientOutput {
	return mcpClientOutput{
		ID:                status.ClientId,
		Name:              status.DisplayName,
		Detected:          status.Detected,
		RegistrationState: registrationState(status.RegistrationState),
		RegistrationError: clijson.BuildErrorTree(diagnostic),
		ConfigurationPath: status.GetConfigurationPath(),
		ExecutablePath:    status.GetExecutablePath(),
	}
}

func mcpClientDiagnostic(status *apapproto.MCPClientStatus) error {
	if status.GetError() != nil {
		return message.ReconstructFromChain(status.GetError())
	}
	return message.ReconstructFromChain(status.GetRegistrationDetail())
}

func errorChainDetails(chain *apapproto.ErrorChain) []string {
	if chain == nil || chain.Root == nil {
		return nil
	}
	details := make([]string, 0, 2)
	if code := errorCode(chain.Root); code != "" {
		details = append(details, mcpMessageTextWithMetadata(
			message.CliCmdMcpCommonErrorCodeDetail,
			map[string]string{"code": code},
		))
	}
	if detail := errorDetail(chain.Root); detail != "" {
		details = append(details, mcpMessageTextWithMetadata(
			message.CliCmdMcpCommonTechnicalDetail,
			map[string]string{"detail": detail},
		))
	}
	return details
}

func mcpMessageText(code message.MessageCode) string {
	return mcpMessageTextWithMetadata(code, nil)
}

func mcpMessageTextWithMetadata(code message.MessageCode, metadata map[string]string) string {
	value, err := message.LookupMessage(message.New(code).WithMetadata(metadata))
	if err != nil {
		return string(code)
	}
	return value.Message
}

// errorDetail returns the underlying cause from a wrapped engine error. The
// root often contains an internal message code, while the leaf says what was
// actually wrong with the client's configuration.
func errorDetail(node *apapproto.ErrorNode) string {
	for len(node.Children) == 1 && node.Children[0] != nil {
		node = node.Children[0]
	}
	return node.Error
}

func errorCode(node *apapproto.ErrorNode) string {
	if node.Message != nil && node.Message.Code != "" {
		return node.Message.Code
	}
	for _, child := range node.Children {
		if child != nil {
			if code := errorCode(child); code != "" {
				return code
			}
		}
	}
	return ""
}

func registrationState(state apapproto.MCPClientRegistrationState) string {
	return strings.ToLower(strings.TrimPrefix(state.String(), "MCP_CLIENT_REGISTRATION_STATE_"))
}

// registrationMarker makes the overview status easy to scan without relying
// on terminal colours. A tick is configured, a cross needs attention, and a
// dash is not configured.
func registrationMarker(state string) string {
	switch state {
	case "configured":
		return "✓"
	case "conflict", "unreadable":
		return "✗"
	default:
		return "–"
	}
}

func displayRegistrationState(state string) string {
	switch state {
	case "configured":
		return mcpMessageText(message.CliCmdMcpStatusStateConfigured)
	case "not_configured":
		return mcpMessageText(message.CliCmdMcpStatusStateNotConfigured)
	case "conflict":
		return mcpMessageText(message.CliCmdMcpStatusStateConflict)
	case "unreadable":
		return mcpMessageText(message.CliCmdMcpStatusStateUnreadable)
	default:
		return mcpMessageText(message.CliCmdMcpStatusStateUnknown)
	}
}

func summaryRegistrationState(output mcpClientOutput) string {
	if output.RegistrationState != "conflict" && output.RegistrationState != "unreadable" {
		return displayRegistrationState(output.RegistrationState)
	}
	return mcpMessageTextWithMetadata(message.CliCmdMcpStatusDiagnose, map[string]string{
		"client": output.ID,
		"state":  displayRegistrationState(output.RegistrationState),
	})
}

func installOperationOutcome(outcome apapproto.MCPClientInstallOutcome) string {
	return operationOutcome(outcome.String(), "MCP_CLIENT_INSTALL_OUTCOME_")
}

func uninstallOperationOutcome(outcome apapproto.MCPClientUninstallOutcome) string {
	return operationOutcome(outcome.String(), "MCP_CLIENT_UNINSTALL_OUTCOME_")
}

func operationOutcome(outcome, prefix string) string {
	return strings.ToLower(strings.TrimPrefix(outcome, prefix))
}

func isAvailableForInstall(status *apapproto.MCPClientStatus) bool {
	return status.Detected &&
		status.RegistrationState ==
			apapproto.MCPClientRegistrationState_MCP_CLIENT_REGISTRATION_STATE_NOT_CONFIGURED
}

func isAvailableForUninstall(status *apapproto.MCPClientStatus) bool {
	return status.Detected &&
		status.RegistrationState ==
			apapproto.MCPClientRegistrationState_MCP_CLIENT_REGISTRATION_STATE_CONFIGURED
}

func filterClients(
	statuses []*apapproto.MCPClientStatus,
	include func(*apapproto.MCPClientStatus) bool,
) []*apapproto.MCPClientStatus {
	filtered := make([]*apapproto.MCPClientStatus, 0, len(statuses))
	for _, status := range statuses {
		if include(status) {
			filtered = append(filtered, status)
		}
	}
	return filtered
}

func findClient(statuses []*apapproto.MCPClientStatus, id string) *apapproto.MCPClientStatus {
	for _, status := range statuses {
		if status.ClientId == id {
			return status
		}
	}
	return nil
}

func writeMCPInfo(cmd *cobra.Command, code message.MessageCode) error {
	if viper.GetBool("json") {
		return clijson.MarshalJSONCLIResponse(cmd.OutOrStdout(), []mcpClientOperationOutput{})
	}
	catalogMessage, err := message.LookupMessage(message.New(code))
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(cmd.OutOrStdout(), catalogMessage.Message)
	return err
}

func sortedMCPClients(statuses []*apapproto.MCPClientStatus) []*apapproto.MCPClientStatus {
	sorted := append([]*apapproto.MCPClientStatus(nil), statuses...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ClientId < sorted[j].ClientId })
	return sorted
}
