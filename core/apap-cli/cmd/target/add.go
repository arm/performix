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
	"github.com/Arm-Debug/apap-cli/apap-cli/service/ssh"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/target"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/targetlogin"
	engine_grpcserver "github.com/Arm-Debug/apap-cli/apap-engine/grpcserver"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	engine_ssh "github.com/Arm-Debug/apap-cli/apap-engine/ssh"
	engine_target "github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/userdirs"
)

type AddTargetArgs struct {
	loginString      string
	jumpLoginStrings []string
	name             string
	setDefault       bool
	findPrivateKey   bool
	hostKeyPolicy    string
}

var provisionKey = false
var userPassword string

const enableAndroidTargetsConfigKey = serverconfig.EnableAndroidTargetsConfigKey

var TargetAddCmd = newAddCommand(client.NewAutostartClient(), engine_target.NewDefaultTargetManager(), ssh.NewDefaultSSHKeyProvisioner())

func newAddCommand(cc client.ClientConnector, targetService target.TargetManagerService, sshService ssh.SSHKeyProvisioner) *cobra.Command {
	var parsed AddTargetArgs

	addCmd := &cobra.Command{
		Use:   "add <target>",
		Short: "Add a target configuration.",
		Long:  targetAddLongDescription(),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			parsed.loginString = args[0]

			_, exists := engine_target.ValidHostKeyPolicyValues[parsed.hostKeyPolicy]
			if cmd.Flags().Changed("host-key-policy") && !exists {
				metadata := map[string]string{
					"value": parsed.hostKeyPolicy,
					"flag":  "--host-key-policy",
				}
				return message.New(message.CliCmdValidationInvalidFlagValue).WithMetadata(metadata)
			}

			handledJSON, err := addJSONTargetCommand(cc, parsed, targetService, sshService)
			if err != nil {
				return err
			}
			if !handledJSON {
				protocol, loginString := resolveAddTargetProtocol(args[0])
				parsed.loginString = loginString

				switch protocol {
				case "ssh":
					if err := addSSHTargetCommand(cc, parsed, targetService, sshService); err != nil {
						return err
					}
				case "android":
					if !viper.GetBool(enableAndroidTargetsConfigKey) {
						return message.New(message.CommonUnsupportedTargetType).WithMetadata(map[string]string{"targetType": protocol})
					}
					tgt := parseAndroidLoginString(parsed.loginString)
					if err := addAndroidTargetConfig(parsed, targetService, &tgt); err != nil {
						return err
					}
				default:
					return message.New(message.CommonUnsupportedTargetType).WithMetadata(map[string]string{"targetType": protocol})
				}
			}

			if viper.GetBool("json") {
				return clijson.MarshalJSONCLIResponse(cmd.OutOrStdout(), emptyStruct{})
			}
			return nil
		},
		Annotations: map[string]string{
			grouping.GroupAnnotation: grouping.GroupTargetSub,
		},
	}
	addCmd.Flags().StringArrayVar(&parsed.jumpLoginStrings, "jump", []string{},
		"Connect via one or more jump hosts before reaching the target. A jump flag is required for each jump using the same target string syntax "+
			"as for the final destination. This is a shortcut for jump host setup. Use the JSON interface for advanced configuration")
	addCmd.Flags().StringVar(&parsed.name, "name", "", "Optional target name")
	addCmd.Flags().BoolVar(&parsed.setDefault, "default", false, "Set the target to the default, meaning it will be used by default for all commands unless overridden by the --target flag.")
	addCmd.Flags().BoolVar(&parsed.findPrivateKey, "find-keys", false, "Find the first SSH keys compatible with the target and any jump hosts when adding the target (note this requires connecting to the target and any jump hosts).")
	addCmd.Flags().StringVar(&parsed.hostKeyPolicy, "host-key-policy", "ask", "Control host key checking: 'ask', 'strict', 'accept-new', or 'ignore'")
	addCmd.Flags().BoolVar(&provisionKey, "provision-key", false, "Automatically provision an SSH key by specifying your SSH password when prompted (TTY by default; if stdin is piped, read from stdin). "+
		"A new RSA 2048 key pair will be created, added to authorized_keys on the remote target, and added to the local known_hosts file. The target must be online for this operation to succeed.")

	_ = addCmd.MarkFlagRequired("user")

	return addCmd
}

func RefreshTargetAddHelp() {
	TargetAddCmd.Long = targetAddLongDescription()
}

func targetAddLongDescription() string {
	if viper.GetBool(enableAndroidTargetsConfigKey) {
		return `Add a target configuration.

The target may be an SSH target or an Android target.

Target forms:
  [ssh://][user@]host[:port][:private_ssh_key_path][:auth=key|password]
  android://serial[@host]`
	}

	return `Add a target configuration.
  [ssh://][user@]host[:port][:private_ssh_key_path][:auth=key|password]`
}

func resolveAddTargetProtocol(target string) (string, string) {
	protocol, loginString, ok := strings.Cut(target, "://")
	if !ok {
		return "ssh", target
	}

	return protocol, loginString
}

func addJSONTargetCommand(cc client.ClientConnector, args AddTargetArgs, targetService target.TargetManagerService, sshService ssh.SSHKeyProvisioner) (bool, error) {
	tgtFromJSON, err := engine_target.TryUnmarshalJSONTargetString(args.loginString)
	if err != nil {
		return false, err
	}
	if tgtFromJSON == nil {
		return false, nil
	}

	switch t := tgtFromJSON.(type) {
	case *engine_target.SSHTarget:
		return true, addSSHTargetConfig(cc, args, targetService, sshService, t)
	case *engine_target.AndroidTarget:
		if !viper.GetBool(enableAndroidTargetsConfigKey) {
			return true, message.New(message.CommonUnsupportedTargetType).WithMetadata(map[string]string{"targetType": "android"})
		}
		return true, addAndroidTargetConfig(args, targetService, t)
	case *engine_target.LocalTarget:
		return true, message.New(message.CommonUnsupportedTargetType).WithMetadata(map[string]string{"targetType": "local"})
	default:
		return true, message.New(message.CommonUnsupportedTargetType).WithMetadata(map[string]string{"targetType": "unknown"})
	}
}

type emptyStruct struct{}

func parseAndroidLoginString(s string) engine_target.AndroidTarget {
	serialNumber, host, ok := strings.Cut(s, "@")
	config := engine_target.AndroidTarget{SerialNumber: serialNumber}

	if ok {
		config.DeviceIPAddress = &host
	}

	return config
}

func parseLoginStringWithDefaults(s string, hostKeyPolicy string) (engine_target.SSHHostConfig, error) {
	loginString, err := engine_ssh.ParseLoginString(s)
	if err != nil {
		return engine_target.SSHHostConfig{}, err
	}

	authMethod := engine_target.SSHAuthMethodKey
	if loginString.AuthMethod != "" {
		method, ok := engine_target.ValidSSHAuthMethods[loginString.AuthMethod]
		if !ok {
			metadata := map[string]string{"value": loginString.AuthMethod}
			return engine_target.SSHHostConfig{}, message.New(message.CliCmdValidationInvalidAuthMethod).WithMetadata(metadata)
		}
		authMethod = method
	}

	config := engine_target.SSHHostConfig{
		Host:               loginString.Host,
		Port:               int32(loginString.Port),
		Username:           loginString.User,
		PrivateKeyFilename: loginString.PrivateKeyFilename,
		HostKeyPolicy:      engine_target.ValidHostKeyPolicyValues[hostKeyPolicy],
		AuthMethod:         authMethod,
	}
	config.ApplyDefaults()

	return config, nil
}

func addAndroidTargetConfig(args AddTargetArgs, targetService target.TargetManagerService, tgt *engine_target.AndroidTarget) error {
	if args.name == "" {
		generatedTargetName, err := target.GenerateUniqueTargetName(targetService)
		if err != nil {
			return err
		}
		args.name = generatedTargetName
	}

	if err := targetService.AddTarget(args.name, tgt); err != nil {
		return err
	}

	if args.setDefault {
		err := targetService.SetDefaultTarget(args.name)
		if err != nil {
			return err
		}
	}

	return nil
}

func addSSHTargetCommand(cc client.ClientConnector, args AddTargetArgs, targetService target.TargetManagerService, sshService ssh.SSHKeyProvisioner) error {
	tgt := &engine_target.SSHTarget{}

	for _, hostLogin := range args.jumpLoginStrings {
		protocol, loginString := resolveAddTargetProtocol(hostLogin)
		if protocol != "ssh" {
			return message.New(message.CommonUnsupportedTargetType).WithMetadata(map[string]string{"targetType": protocol})
		}

		jumpHostConfig, err := parseLoginStringWithDefaults(loginString, args.hostKeyPolicy)
		if err != nil {
			return err
		}

		tgt.Jumps = append(tgt.Jumps, jumpHostConfig)
	}

	hostConfig, err := parseLoginStringWithDefaults(args.loginString, args.hostKeyPolicy)
	if err != nil {
		return err
	}
	tgt.Jumps = append(tgt.Jumps, hostConfig)

	return addSSHTargetConfig(cc, args, targetService, sshService, tgt)
}

func addSSHTargetConfig(cc client.ClientConnector, args AddTargetArgs, targetService target.TargetManagerService, sshService ssh.SSHKeyProvisioner, tgt *engine_target.SSHTarget) error {
	for i := range tgt.Jumps {
		tgt.Jumps[i].ApplyDefaults()
	}

	if args.name == "" {
		generatedTargetName, err := target.GenerateUniqueTargetName(targetService)
		if err != nil {
			return err
		}
		args.name = generatedTargetName
	}

	if provisionKey && tgt.LastJump().PrivateKeyFilename != "" {
		metadata := map[string]string{
			"flag1": "--provision-key",
			"flag2": "embedded-private-key",
		}
		return message.New(message.CliCmdValidationMutuallyExclusiveFlags).WithMetadata(metadata)
	}

	if args.findPrivateKey && provisionKey {
		metadata := map[string]string{
			"flag1": "--find-keys",
			"flag2": "--provision-key",
		}
		return message.New(message.CliCmdValidationMutuallyExclusiveFlags).WithMetadata(metadata)
	}

	if args.findPrivateKey && tgt.LastJump().PrivateKeyFilename != "" {
		metadata := map[string]string{
			"flag1": "--find-keys",
			"flag2": "embedded-private-key",
		}
		return message.New(message.CliCmdValidationMutuallyExclusiveFlags).WithMetadata(metadata)
	}

	usesPasswordAuth := false
	for _, jump := range tgt.Jumps {
		if jump.AuthMethod == engine_target.SSHAuthMethodPassword {
			usesPasswordAuth = true
			if jump.PrivateKeyFilename != "" {
				metadata := map[string]string{"host": jump.Host, "key": jump.PrivateKeyFilename}
				return message.New(message.CliCmdValidationPasswordAuthWithKey).WithMetadata(metadata)
			}
		}
	}

	if usesPasswordAuth && args.findPrivateKey {
		metadata := map[string]string{"flag": "--find-keys"}
		return message.New(message.CliCmdValidationPasswordAuthUnsupportedFlag).WithMetadata(metadata)
	}

	if usesPasswordAuth && provisionKey {
		metadata := map[string]string{"flag": "--provision-key"}
		return message.New(message.CliCmdValidationPasswordAuthUnsupportedFlag).WithMetadata(metadata)
	}

	if args.findPrivateKey {
		client, err := cc.ApapClient(serverconfig.FromViperForBackground())
		if err != nil {
			return err
		}
		resp, err := client.FindSSHKeysForTarget(context.Background(), engine_grpcserver.TargetToProto(tgt))
		if err != nil {
			return err
		}
		if resp.GetError() != nil {
			return message.ReconstructFromChain(resp.GetError())
		}
		privateKeyPaths := resp.GetPrivateKeyPaths().GetValues()
		for i := range tgt.Jumps {
			tgt.Jumps[i].PrivateKeyFilename = privateKeyPaths[i]
		}
	} else if provisionKey {
		pw, err := targetlogin.PromptSecret("Enter password: ")
		if err != nil {
			return message.New(message.CliServiceTargetloginPromptFailed).WithCause(err)
		}
		userPassword = string(pw)
		privateKey, err := provisionAndDeployKeys(sshService, tgt.LastJump())
		if err != nil {
			return err
		}
		tgt.Jumps[0].PrivateKeyFilename = privateKey
	}

	if err := targetService.AddTarget(args.name, tgt); err != nil {
		return err
	}

	if args.setDefault {
		err := targetService.SetDefaultTarget(args.name)
		if err != nil {
			return err
		}
	}

	return nil
}

// provisionAndDeployKeys will create the ssh key pair and attempt to copy to the specified
// target.
// TODO APAP-1289 - Implement a roll-back mechanism
func provisionAndDeployKeys(sshService ssh.SSHKeyProvisioner, hostConfig engine_target.SSHHostConfig) (string, error) {
	configDir, err := userdirs.ConfigDir()
	if err != nil {
		return "", message.New(message.CliCmdTargetAddCouldNotGetConfigDir).WithCause(err)
	}

	// Step 1: Create keypair
	filename, err := sshService.CreateSSHKeyPair(configDir)
	if err != nil {
		return "", err
	}

	// Step 2: Read public key
	pubFileName := filename + ".pub"
	pubKeyBytes, err := sshService.ReadPublicKey(pubFileName)
	if err != nil {
		return "", message.New(message.CliCmdTargetAddCouldNotReadPublicKey).WithCause(err).WithMetadata(map[string]string{"path": pubFileName})
	}

	// Step 3: Provision public key using password
	err = sshService.ProvisionPublicKeyWithPassword(
		configDir,
		hostConfig.Host,
		int(hostConfig.Port),
		hostConfig.Username,
		userPassword,
		string(pubKeyBytes),
	)
	if err != nil {
		return "", err
	}
	return filename, nil
}
