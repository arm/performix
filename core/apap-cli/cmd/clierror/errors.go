// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package clierror

import (
	"errors"
	"fmt"

	"github.com/spf13/viper"

	"github.com/Arm-Debug/apap-cli/apap-cli/service"
)

type CommonMessages struct {
	// ConnectFailed is used when the client is unable to connect to the gRPC backend server.
	ConnectFailed string
}

var Common = CommonMessages{
	ConnectFailed: `Could not connect to the gRPC server. Make sure that the gRPC
	server is running and accessible, check your network connection, firewall settings, and
	server address, then try again. If the issue persists, contact support.`,
}

// RecipeInfoMessages holds error messages for the `recipe info` command.
type RecipeInfoMessages struct {
	TargetRetrievalFailed  string
	TargetConversionFailed string
	ParseRecipeFailed      string
}

// RecipeListMessages holds error messages for the `recipe list` command.
type RecipeListMessages struct {
	ReadRecipeFailed      string
	RecipeParseFileFailed func(filename string) string
}

// RecipeRunMessages holds error messages for the `recipe run` command.
type RecipeRunMessages struct {
	MutuallyExclusiveDeployFlags    string
	MutuallyExclusivePIDAndWorkload string
	SystemWideWithPIDOrWorkload     string
	MissingTargetFlags              string
	InvalidPID                      string
}

// RecipeValidateMessages holds error messages for the `recipe validate-parameters` command.
type RecipeValidateMessages struct {
	ValidationErrorsEncountered string
}

// RecipeMessages holds grouped error message templates for all recipe subcommands.
type RecipeMessages struct {
	Info     RecipeInfoMessages
	List     RecipeListMessages
	Run      RecipeRunMessages
	Validate RecipeValidateMessages
}

var Recipe = RecipeMessages{
	Info: RecipeInfoMessages{
		TargetRetrievalFailed: `Could not access the specified target. Check that the target name is
		correct, the target is running and connected, and you have the necessary permissions. For remote
		targets, check your network settings. If the issue persists, contact support.`,
		TargetConversionFailed: `Could not process the specified target. Check the target and try again.
		If the issue continues, contact support.`,
		ParseRecipeFailed: `Could not parse the recipe. Check the file for syntax or formatting issues, and then 
		try again.`,
	},
	List: RecipeListMessages{
		ReadRecipeFailed: `Could not read the recipes. Check their location and readability, or try reinstalling. 
		If the issue continues, contact support.`,
	},
	Run: RecipeRunMessages{
		MutuallyExclusiveDeployFlags: `You cannot use --deploy-tools and --deploy-tools-force at the same time.
		Use --deploy-tools to deploy tools to a target only if they are not already installed. Alternatively,
		use --deploy-tools-force to force deployment even if the tools are already installed.`,
		MutuallyExclusivePIDAndWorkload: `You cannot use --pid and --workload at the same time. Use --pid to
		attach to a currently running process. Alternatively, use --workload to analyze a new workload.`,
		SystemWideWithPIDOrWorkload: `You cannot use --system-wide with --pid or --workload. Use --system-wide
		to profile an entire system. Alternatively, use --pid to attach to a currently running process, or 
		use --workload to analyze a new workload.`,
		MissingTargetFlags: "You must specify either a workload or a process ID, or use the --system-wide flag.",
		InvalidPID:         "Invalid process ID. Specify a process ID of 0 or higher.",
	},
	Validate: RecipeValidateMessages{
		ValidationErrorsEncountered: `Recipe parameter validation failed. Check the input parameters exist on 
		in the recipe and the target is running.`,
	},
}

// RenderQueryMessages holds error messages related to querying and decoding render data.
type RenderQueryMessages struct {
	// QueryFailed is used when the backend query execution fails.
	QueryFailed string

	// ResponseProcessingFailed is used when processing the query response fails.
	ResponseProcessingFailed string

	// JSONUnmarshalFailed is used when a JSON decoding operation fails.
	JSONUnmarshalFailed string

	// DataUnexpectedFormat is used when the response structure is not as expected.
	DataUnexpectedFormat string

	// RowFormatInvalid is used when a returned row is not in expected map format.
	RowFormatInvalid string

	// WrongTypeWrittenToJSON exists in case a bug causes the intermediate JSON to be incorrectly written.
	WrongTypeWrittenToJSON string
}

// RenderCloseMessages hold error message related to closing a session
type RenderCloseMessages struct {
	CloseRenderFailed string
}

// RenderListMessages holds error messages related to listing active sessions
type RenderListMessages struct {
	ListFailed    string
	MarshalFailed string
}

// RenderMessages holds grouped error message templates for the render CLI.
type RenderMessages struct {
	// Query holds all messages related to the `render query` command.
	Query RenderQueryMessages
	Close RenderCloseMessages
	List  RenderListMessages
}

var Render = RenderMessages{
	Query: RenderQueryMessages{
		QueryFailed: `Could not query the render data. Check that the data is in the right location,
		that file paths and settings are correct, and that your network and permissions are sufficient for remote 
		files. If the issue continues, contact support.`,
		ResponseProcessingFailed: `Could not process the JSON response. Check your input values and network 
		connection, and then try again. If the issue continues, contact support.`,
		JSONUnmarshalFailed: `Could not process the JSON data. Make sure that your data is correctly formatted, 
		valid, and complete. If the issue continues, double-check your configuration or contact support.`,
		DataUnexpectedFormat: `The data provided does not match the required format. Make sure that your file 
		is correctly structured and formatted. If the problem continues, contact support.`,
		RowFormatInvalid: `One or more rows in your file do not follow the expected format. Make sure that
		each row contains all required fields in the correct order, and then try again. If the issue 
		continues, contact support.`,
		WrongTypeWrittenToJSON: `wrong type written to JSON`,
	},
	Close: RenderCloseMessages{
		CloseRenderFailed: `Could not close the render.`,
	},
	List: RenderListMessages{
		ListFailed: `Could not list the renders.`,
		MarshalFailed: `Could not process the render list. Check for empty entries or formatting 
		issues and then try again. If the issue continues, contact support.`,
	},
}

// RunDeleteMessages holds error message templates used by the run delete command.
type RunDeleteMessages struct {
	// MarshalFailed returns a formatted message when marshalling a run response fails.
	MarshalFailed func(runID string) string
}

// RunExportMessages holds error message templates used by the run export command.
type RunExportMessages struct {
	// InvalidPath is used when the export target directory path can't be resolved.
	InvalidPath string

	// ExportFailed is used when the export operation fails.
	ExportFailed string
}

// RunImportMessages holds error message templates used by the run import command.
type RunImportMessages struct {
	// InvalidPath is used when the import path is invalid or inaccessible.
	InvalidPath string

	// ImportFailed is used when importing a run fails.
	ImportFailed string
}

// RunInfoMessages holds error message templates used by the run info command.
type RunInfoMessages struct {
	// ListFailed is used when the run cannot be listed.
	ListFailed string

	// MarshalFailed returns a formatted message when marshalling the run fails.
	MarshalFailed func(runID string) string
}

// RunListMessages holds error message templates used by the run list command.
type RunListMessages struct {
	// ListFailed is used when retrieving the list of runs fails.
	ListFailed string

	// MarshalFailed is used when JSON marshaling of the run list fails.
	MarshalFailed string
}

// RunRenameMessages holds error message templates used by the run rename command.
type RunRenameMessages struct {
	// EmptyNewName is used when an empty new name is given.
	EmptyNewName string
	// RenameFailed is used when renaming the run fails.
	RenameFailed string
}

// RunRenderMessages holds error message templates used by the run render command.
type RunRenderMessages struct {
	// InvalidPreparationParams is used when the CLI detects an invalid combination of parameters in render preparation.
	InvalidPreparationParams string

	// PrepareRenderFailed is used when the PrepareRender API call fails.
	PrepareRenderFailed string

	// InvalidRendererConfig is used when renderer config strings are malformed.
	InvalidRendererConfig string

	// InvokeRenderFailed is used when the rendering process fails.
	InvokeRenderFailed string

	// FilterRenderFailed is used when filtering the render preparation by visualization ID fails.
	FilterRenderFailed string
}

// RunUpdateMessages holds error message templates used by the run update command.
type RunUpdateMessages struct {
	// UpdateFailed is used when updating the run fails.
	UpdateFailed string
}

// RunMessages holds grouped error message templates for all run subcommands.
type RunMessages struct {
	Delete RunDeleteMessages
	Export RunExportMessages
	Import RunImportMessages
	Info   RunInfoMessages
	List   RunListMessages
	Rename RunRenameMessages
	Render RunRenderMessages
	Update RunUpdateMessages
}

var Run = RunMessages{
	Delete: RunDeleteMessages{
		MarshalFailed: func(runID string) string {
			return fmt.Sprintf(`Could not process the data for run “%q”. Check that the run data is valid 
			and compatible with this version of the tool, and then try again. If the issue continues, 
			contact support.`, runID)
		},
	},
	Export: RunExportMessages{
		InvalidPath: `Could not find the specified path. Check the path details and try again. 
		If the issue continues, contact support.`,
		ExportFailed: `Could not export the run. Check that the run data is valid and accessible,
		and then try again. If the issue continues, contact support.`,
	},
	Import: RunImportMessages{
		InvalidPath: `Could not find the specified path. Check the path details and try again.
		If the issue continues, contact support.`,
		ImportFailed: `Could not import the run. Check that the run data is valid and accessible,
		and then try again. If the issue continues, contact support.`,
	},
	Info: RunInfoMessages{
		ListFailed: `Could not list the runs. Check that you have permission to write to the runs 
		directory and then try again. If the issue continues, contact support.`,
		MarshalFailed: func(runID string) string {
			return fmt.Sprintf(`Could not process the data for run “%q”. Check that the run data is 
			valid and compatible with this version of the tool, and then try again. If the issue 
			continues, contact support.`, runID)
		},
	},
	List: RunListMessages{
		ListFailed: `Could not list the run. Check that you have permission to write to the runs 
		directory and then try again. If the issue continues, contact support.`,
		MarshalFailed: `Could not process the run list. Check for empty entries or formatting 
		issues and then try again. If the issue continues, contact support.`,
	},
	Rename: RunRenameMessages{
		EmptyNewName: "New run name cannot be empty.",
		RenameFailed: "Unable to rename run.",
	},
	Render: RunRenderMessages{
		InvalidPreparationParams: `Invalid parameters for render preparation. Check your configuration and try again.`,
		PrepareRenderFailed: `Could not determine run rendering configuration. Check the data and settings, make sure 
		that your system has sufficient resources, and then try again. If the issue continues, contact support.`,
		InvalidRendererConfig: `The renderer configuration is invalid. Review the settings to make sure 
		that all parameters are correctly specified and then try again. If the issue continues, contact support.`,
		InvokeRenderFailed: `Could not render the run. Check the data and settings, make sure that your system has
		sufficient resources, and then try again. If the issue continues, contact support.`,
		FilterRenderFailed: `Filtering step failed; please check your arguments and try again. If the issue continues, 
		contact support`,
	},
	Update: RunUpdateMessages{
		UpdateFailed: "Unable to update run.",
	},
}

// SSHListMessages holds error message templates used by the `ssh list-keys` command.
type SSHListMessages struct {
	// ListKeysFailed is used when listing SSH private keys fails.
	ListKeysFailed string
}

// SSHMessages holds grouped error message templates for all ssh subcommands.
type SSHMessages struct {
	List SSHListMessages
}

var SSH = SSHMessages{
	List: SSHListMessages{
		ListKeysFailed: "Failed to list private ssh keys.",
	},
}

// TargetAddMessages holds error message templates used by the `target add` command.
type TargetAddMessages struct {
	GenerateNameFailed                string
	ConnectFailed                     string
	ParseLoginStringFailed            string
	FindSSHKeysFailed                 string
	AddTargetFailed                   string
	UnmarshalJSONTargetFailed         string
	SetDefaultTargetFailed            string
	NameCollision                     string
	InvalidHostKeyPolicyValue         string
	KeyProvisionFailed                string
	ReadPublicKeyFailed               string
	FailedToReadPassword              string
	FailedToDetermineUsersPassword    string
	MutuallyExclusiveKeyPasswordFlags string
}

// TargetInfoMessages holds error message templates for the `target info` command.
type TargetInfoMessages struct {
	LoadFailed       string
	UnknownTarget    string
	TargetTestFailed string
}

// TargetListMessages holds error message templates for the `target list` command.
type TargetListMessages struct {
	ReadConfigFailed string
}

// TargetDefaultMessages holds error message templates used by the `target default` command.
type TargetDefaultMessages struct {
	SetDefaultTargetFailed string
}

// TargetPrepareMessages holds error message templates for the `target prepare` command.
type TargetPrepareMessages struct {
	GetTargetFailed string
	PrepareFailed   string
}

// TargetRemoveMessages holds error message templates for the `target remove` command.
type TargetRemoveMessages struct {
	RemoveTargetFailed     string
	RemoveAllTargetsFailed string
}

// TargetTestMessages holds error message templates for the `target test` command.
type TargetTestMessages struct {
	UnknownTarget string
	TestFailed    string
}

// TargetUpdateMessages holds error message templates used by the `target update` command.
type TargetUpdateMessages struct {
	UpdateFailed    string
	NoFlagSpecified string
}

// Extend TargetMessages struct
type TargetMessages struct {
	Add     TargetAddMessages
	Info    TargetInfoMessages
	List    TargetListMessages
	Prefer  TargetDefaultMessages
	Prepare TargetPrepareMessages
	Remove  TargetRemoveMessages
	Test    TargetTestMessages
	Update  TargetUpdateMessages
}

var Target = TargetMessages{
	Add: TargetAddMessages{
		GenerateNameFailed: `Could not generate a unique target name. Make sure that your specified 
		name is not already in use and try again. If the issue continues, contact support.`,
		ParseLoginStringFailed: "Failed to parse login string.",
		FindSSHKeysFailed: `Could not find compatible SSH keys. Check your SSH key setup and make 
		sure that the target and any jump hosts have valid authorized keys.`,
		AddTargetFailed: `Could not add the target. Check the target settings and try again. 
		If the issue continues, contact support.`,
		UnmarshalJSONTargetFailed: `Could not unmarshal target JSON string. Make sure that your data is correctly formatted, 
		valid, and complete. If the issue continues, contact support.`,
		SetDefaultTargetFailed: `Could not set the default target. Check the target details 
		and try again. If the issue continues, contact support.`,
		NameCollision: `All generated target names already exist. Make sure that your 
		target names are unique and do not overlap with existing target names.`,
		InvalidHostKeyPolicyValue:         "Invalid value for --host-key-policy: must be one of 'ask (default)', 'strict', 'accept-new', or 'ignore'",
		KeyProvisionFailed:                "Could not automatically provision SSH keys for this target. Check your settings, or set the keys up manually",
		ReadPublicKeyFailed:               "Failed to read the public key",
		FailedToReadPassword:              "Failed to read password",
		FailedToDetermineUsersPassword:    "Failed to read the user's password",
		MutuallyExclusiveKeyPasswordFlags: `You cannot use --find-keys and --provision-key at the same time. Use --provision-key to provision an SSH key via password prompt. Alternatively, use --find-keys to search for available keys.`,
	},
	Info: TargetInfoMessages{
		TargetTestFailed: `Could not test the target. Make sure the target is correctly configured 
		and connected, and then try again.`,
	},
	List: TargetListMessages{
		ReadConfigFailed: `Could not read the target configuration. Check that the configuration is available 
		and that you have the required permissions to access it, and then try again.`,
	},
	Prefer: TargetDefaultMessages{
		SetDefaultTargetFailed: `Could not set the specified target as the preferred target. Make sure that 
		the target exists and you have the necessary permissions to modify preferences, and then try again. 
		If the issue continues, contact support.`,
	},
	Prepare: TargetPrepareMessages{
		GetTargetFailed: `An error occurred while getting the specified target. Check the target details 
		and try again. If the issue continues, contact support.`,
		PrepareFailed: `Could not prepare the target. Check the configuration settings and make sure the 
		target is ready and all necessary services are running.`,
	},
	Remove: TargetRemoveMessages{
		RemoveTargetFailed: `Could not remove the target. Check that the target exists and that you 
		have the necessary permissions, and then try again.`,
		RemoveAllTargetsFailed: `Could not remove the targets. Check that the targets exist and that 
		you have the necessary permissions, and then try again.`,
	},
	Test: TargetTestMessages{
		UnknownTarget: `The target to test is unknown. Check the target details or create a 
		new target if necessary, and then try again.`,
		TestFailed: `Could not test the target. Make sure the target is correctly configured and 
		connected, and then try again.`,
	},
	Update: TargetUpdateMessages{
		UpdateFailed: `Could not update target. Check your target settings and access permissions, 
		and then try again. If the issue continues, contact support.`,
		NoFlagSpecified: "No update fields specified. Please provide at least one flag to update.",
	},
}

type errorService interface {
	ExtractGRPCError(err error) (string, string, string, bool)
}

// DecorateError returns a new error, incorporating a summary message.
// There is special handling for gRPC errors, in which case fields are extracted from gRPC Error.
// This is safe to call with non gRPC errors and will format them consistently with gRPC errors.
func DecorateError(summaryMessage string, err error) error {
	return decorateError(service.GRPCErrors{}, summaryMessage, err)
}

func decorateError(errorService errorService, summaryMessage string, err error) error {
	var errorMessage string

	// gRPC errors are extracted when constructed the json response
	if viper.GetBool("json") {
		return fmt.Errorf("%s\n- %s", summaryMessage, err)
	}

	if err == nil {
		return nil
	}

	// For now we'll ignore grpcCode
	_, grpcMessage, grpcDetails, gRPCError := errorService.ExtractGRPCError(err)

	if gRPCError {
		errorMessage = fmt.Sprintf("%s\n%s", summaryMessage, grpcDetails)
		errorMessage += fmt.Sprintf("\n- gRPC Message: %s", grpcMessage)
	} else {
		errorMessage = fmt.Sprintf("%s\n- %s", summaryMessage, err)
	}
	return errors.New(errorMessage)
}

// GRPCErrorDetails Return the details of a gRPC message or fallback to the message string if not a gRPC error
func GRPCErrorDetails(err error) string {
	return gRPCErrorDetails(service.GRPCErrors{}, err)
}

func gRPCErrorDetails(errorService errorService, err error) string {
	_, _, details, gRPCError := errorService.ExtractGRPCError(err)

	if gRPCError {
		return details
	} else {
		return err.Error()
	}
}
