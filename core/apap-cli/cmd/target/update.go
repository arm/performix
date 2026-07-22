// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package target

import (
	"context"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/grouping"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/serverconfig"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/client"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/clijson"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/target"
	engine_grpcserver "github.com/Arm-Debug/apap-cli/apap-engine/grpcserver"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	engine_target "github.com/Arm-Debug/apap-cli/apap-engine/target"
)

const UpdateUse = "update [name] [flags]"

var TargetUpdateCmd = newUpdateCommand(client.NewAutostartClient(), engine_target.NewDefaultTargetManager())

func newUpdateCommand(cc client.ClientConnector, targetService target.TargetManagerService) *cobra.Command {
	var parsed AddTargetArgs

	cmd := &cobra.Command{
		Use:   UpdateUse,
		Short: "Update an existing target configuration.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().NFlag() == 0 {
				return message.New(message.CliCmdTargetUpdateMissingProperty)
			}

			_, exists := engine_target.ValidHostKeyPolicyValues[parsed.hostKeyPolicy]
			if cmd.Flags().Changed("host-key-policy") && !exists {
				metadata := map[string]string{
					"value": parsed.hostKeyPolicy,
					"flag":  "--host-key-policy",
				}
				return message.New(message.CliCmdValidationInvalidFlagValue).WithMetadata(metadata)
			}

			targetName := args[0]
			err := updateTarget(cc, targetName, parsed, targetService)
			if err == nil && viper.GetBool("json") {
				return clijson.MarshalJSONCLIResponse(cmd.OutOrStdout(), emptyStruct{})
			}
			return err
		},
		Annotations: map[string]string{
			grouping.GroupAnnotation: grouping.GroupTargetSub,
		},
	}
	cmd.Flags().StringVar(&parsed.loginString, "host", "", targetUpdateHostDescription())
	cmd.Flags().StringArrayVar(&parsed.jumpLoginStrings, "jump", []string{},
		"Update the jump hosts for the SSH connection. A jump flag is required for each jump using the same target string syntax "+
			"as for the final destination. This is a shortcut for jump host setup. Use the JSON interface for advanced configuration")
	cmd.Flags().StringVar(&parsed.name, "name", "", "Update the target name")
	cmd.Flags().BoolVar(&parsed.setDefault, "default", false, "Set the target to the default, meaning it will be used by default for all commands unless overridden by the --target flag.")
	cmd.Flags().BoolVar(&parsed.findPrivateKey, "find-keys", false, "Find the first SSH key compatible with the target and any jump hosts when updating the target")
	cmd.Flags().StringVar(&parsed.hostKeyPolicy, "host-key-policy", "", "Control host key checking: 'ask', 'strict', 'accept-new', or 'ignore'")

	return cmd
}

func RefreshTargetUpdateHelp() {
	if flag := TargetUpdateCmd.Flags().Lookup("host"); flag != nil {
		flag.Usage = targetUpdateHostDescription()
	}
}

func targetUpdateHostDescription() string {
	if viper.GetBool(enableAndroidTargetsConfigKey) {
		return "Update the target host. For SSH targets, use [user@]host[:port][:private_ssh_key_path][:auth=key|password]. For Android targets, use android://serial[@host]."
	}

	return "Update the final host for the SSH connection via a [user@]host[:port][:private_ssh_key_path][:auth=key|password] string"
}

func updateTarget(cc client.ClientConnector, name string, args AddTargetArgs, targetService target.TargetManagerService) error {
	originalTarget, err := targetService.GetTarget(name)
	if err != nil {
		return err
	}

	fields := &engine_target.UpdateTargetFields{}
	fields.DefaultFlag = args.setDefault
	fields.Name = args.name

	switch originalTarget.(type) {
	case *engine_target.SSHTarget:
		targetFromJSON, err := engine_target.TryUnmarshalJSONTargetString(args.loginString)
		if err != nil {
			return err
		}
		var updatedTarget *engine_target.SSHTarget
		if targetFromJSON != nil {
			sshTarget, ok := targetFromJSON.(*engine_target.SSHTarget)
			if !ok {
				return message.New(message.CommonUnsupportedTargetType).WithMetadata(map[string]string{"targetType": "unknown"})
			}
			updatedTarget = sshTarget
			for i := range updatedTarget.Jumps {
				updatedTarget.Jumps[i].ApplyDefaults()
			}
		} else {
			updatedTarget, err = updatedSSHTargetFromCLIFlags(originalTarget.(*engine_target.SSHTarget), args)
			if err != nil {
				return err
			}
		}
		fields.UpdatedTarget, err = validateAndResolveSSHKeys(cc, updatedTarget, args)
		if err != nil {
			return err
		}
	case *engine_target.AndroidTarget:
		if !viper.GetBool(enableAndroidTargetsConfigKey) {
			return message.New(message.CommonUnsupportedTargetType).WithMetadata(map[string]string{"targetType": "android"})
		}
		targetFromJSON, err := engine_target.TryUnmarshalJSONTargetString(args.loginString)
		if err != nil {
			return err
		}
		var updatedTarget *engine_target.AndroidTarget
		if targetFromJSON != nil {
			androidTarget, ok := targetFromJSON.(*engine_target.AndroidTarget)
			if !ok {
				return message.New(message.CommonUnsupportedTargetType).WithMetadata(map[string]string{"targetType": "unknown"})
			}
			updatedTarget = androidTarget
		} else {
			updatedTarget, err = updatedAndroidTargetFromCLIFlags(originalTarget.(*engine_target.AndroidTarget), args)
			if err != nil {
				return err
			}
		}
		if err := updatedTarget.Validate(args.name); err != nil {
			return err
		}
		fields.UpdatedTarget = updatedTarget
	case *engine_target.LocalTarget:
		return message.New(message.CommonUpdateLocalhost)
	default:
		return message.New(message.CommonUnsupportedTargetType).WithMetadata(map[string]string{"targetType": "unknown"})
	}

	return targetService.UpdateTarget(name, fields)
}

func updatedAndroidTargetFromCLIFlags(originalTgt *engine_target.AndroidTarget, args AddTargetArgs) (*engine_target.AndroidTarget, error) {
	if args.loginString != "" {
		protocol := "android"
		loginString := args.loginString
		if parsedProtocol, parsedLoginString, ok := strings.Cut(args.loginString, "://"); ok {
			protocol = parsedProtocol
			loginString = parsedLoginString
		}
		if protocol != "android" {
			return nil, message.New(message.CommonUnsupportedTargetType).WithMetadata(map[string]string{"targetType": protocol})
		}

		updatedTgt := parseAndroidLoginString(loginString)
		return &updatedTgt, nil
	}

	if originalTgt == nil {
		return &engine_target.AndroidTarget{}, nil
	}
	updatedTgt := *originalTgt
	return &updatedTgt, nil
}

func updatedSSHTargetFromCLIFlags(originalTgt *engine_target.SSHTarget, args AddTargetArgs) (*engine_target.SSHTarget, error) {
	updatedTgt := &engine_target.SSHTarget{}
	hostKeyPolicy := args.hostKeyPolicy
	if hostKeyPolicy == "" {
		hostKeyPolicy = engine_target.HostKeyPolicyToString[originalTgt.ReadKnownHostsPolicy()]
	}

	if len(args.jumpLoginStrings) > 0 {
		// Parse the jump host login strings
		for _, hostLogin := range args.jumpLoginStrings {
			protocol, loginString := resolveAddTargetProtocol(hostLogin)
			if protocol != "ssh" {
				return nil, message.New(message.CommonUnsupportedTargetType).WithMetadata(map[string]string{"targetType": protocol})
			}

			jumpHostConfig, err := parseLoginStringWithDefaults(loginString, hostKeyPolicy)
			if err != nil {
				return nil, err
			}

			updatedTgt.Jumps = append(updatedTgt.Jumps, jumpHostConfig)
		}
	} else if len(originalTgt.Jumps) > 1 {
		updatedJumps := originalTgt.Jumps[:len(originalTgt.Jumps)-1]
		for i, jump := range updatedJumps {
			jump.HostKeyPolicy = engine_target.ValidHostKeyPolicyValues[hostKeyPolicy]
			updatedJumps[i] = jump
		}
		updatedTgt.Jumps = updatedJumps
	}

	if len(args.loginString) > 0 {
		// Parse the final target login string
		protocol, loginString := resolveAddTargetProtocol(args.loginString)
		if protocol != "ssh" {
			return nil, message.New(message.CommonUnsupportedTargetType).WithMetadata(map[string]string{"targetType": protocol})
		}

		hostConfig, err := parseLoginStringWithDefaults(loginString, hostKeyPolicy)
		if err != nil {
			return nil, err
		}
		updatedTgt.Jumps = append(updatedTgt.Jumps, hostConfig)
	} else if len(originalTgt.Jumps) > 0 {
		updatedJump := originalTgt.Jumps[len(originalTgt.Jumps)-1]
		updatedJump.HostKeyPolicy = engine_target.ValidHostKeyPolicyValues[hostKeyPolicy]
		updatedTgt.Jumps = append(updatedTgt.Jumps, updatedJump)
	}

	return updatedTgt, nil
}

func validateAndResolveSSHKeys(cc client.ClientConnector, updatedTgt *engine_target.SSHTarget, args AddTargetArgs) (*engine_target.SSHTarget, error) {
	usesPasswordAuth := false
	for _, jump := range updatedTgt.Jumps {
		if jump.AuthMethod == engine_target.SSHAuthMethodPassword {
			usesPasswordAuth = true
			if jump.PrivateKeyFilename != "" {
				metadata := map[string]string{"host": jump.Host, "key": jump.PrivateKeyFilename}
				return nil, message.New(message.CliCmdValidationPasswordAuthWithKey).WithMetadata(metadata)
			}
		}
	}

	if usesPasswordAuth && args.findPrivateKey {
		metadata := map[string]string{
			"flag": "--find-keys",
		}
		return nil, message.New(message.CliCmdValidationPasswordAuthUnsupportedFlag).WithMetadata(metadata)
	}

	if args.findPrivateKey {
		client, err := cc.ApapClient(serverconfig.FromViperForBackground())
		if err != nil {
			return nil, err
		}
		resp, err := client.FindSSHKeysForTarget(context.Background(), engine_grpcserver.TargetToProto(updatedTgt))
		if err != nil {
			return nil, err
		}
		if resp.GetError() != nil {
			return nil, message.ReconstructFromChain(resp.GetError())
		}
		privateKeyPaths := resp.GetPrivateKeyPaths().GetValues()
		for i := range updatedTgt.Jumps {
			updatedTgt.Jumps[i].PrivateKeyFilename = privateKeyPaths[i]
		}
	}

	return updatedTgt, nil
}
