// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package completion

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/Arm-Debug/apap-cli/apap-cli/resolvepath"
	engine_target "github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
)

var RootCompletionCmd = &cobra.Command{
	Use:    "completion [bash|zsh|powershell]",
	Short:  "Generate shell completion script for simplified CLI usage.",
	Hidden: true,
	Long: strings.NewReplacer("__PRODUCT_BINARY_NAME__", terminology.GetProductBinaryName()).Replace(`Generate a shell completion script for your chosen shell, so that commands automatically complete by pressing
the tab key.

Completions are currently supported for Bash, Zsh, and PowerShell. Command names, run IDs, and target names can be completed.

To load completions, run the following command and follow the instructions for your shell:

Bash:

$ source <(__PRODUCT_BINARY_NAME__ completion bash)
$ __PRODUCT_BINARY_NAME__ completion bash > /etc/bash_completion.d/__PRODUCT_BINARY_NAME__
$ __PRODUCT_BINARY_NAME__ completion bash > /usr/local/etc/bash_completion.d/__PRODUCT_BINARY_NAME__

Zsh:

$ echo "autoload -Uz compinit; compinit" >> ~/.zshrc
$ __PRODUCT_BINARY_NAME__ completion zsh > "${fpath[1]}/___PRODUCT_BINARY_NAME__"
$ source ~/.zshrc

PowerShell:

PS> __PRODUCT_BINARY_NAME__ completion powershell | Out-String | Invoke-Expression
PS> __PRODUCT_BINARY_NAME__ completion powershell > __PRODUCT_BINARY_NAME__.ps1
`),
	ValidArgs: []string{"bash", "zsh", "powershell"},
	Args:      cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		switch args[0] {
		case "bash":
			_ = cmd.Root().GenBashCompletion(os.Stdout)
		case "zsh":
			_ = cmd.Root().GenZshCompletion(os.Stdout)
		case "powershell":
			_ = cmd.Root().GenPowerShellCompletionWithDesc(os.Stdout)
		}
	},
}

// CompleteRunIDs can be used by CLIs that expect a single positional argument that's a run id
// It's currently used by `run info <run id>` and `run render <run id>`
// This will need updating if it's to be used within a CLI that takes a run id and other positional
// arguments, for example `run rename <run id> <new name of run>`
func CompleteRunIDs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	runDir := filepath.Join(resolvepath.ResolvePath(viper.GetString("data-dir")), "runs")
	entries, err := os.ReadDir(runDir)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	var suggestions []string
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), toComplete) {
			suggestions = append(suggestions, entry.Name())
		}
	}

	return suggestions, cobra.ShellCompDirectiveNoFileComp
}

func CompleteTargetNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	targetManager := engine_target.NewDefaultTargetManager()
	config, err := targetManager.ReadTargetConfig()
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	var suggestions []string
	for name := range config.Targets {
		if strings.HasPrefix(name, toComplete) {
			suggestions = append(suggestions, name)
		}
	}

	return suggestions, cobra.ShellCompDirectiveNoFileComp
}
