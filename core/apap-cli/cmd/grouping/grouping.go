// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grouping

import "fmt"

// For each command group we have 2 annotations, the root (e.g. recipe) and sub (e.g. recipeSub)
// In our noun verb command format, the root correlates to the noun and the sub to the verb.
// Noun annotation is required to extract the Usage string for the group heading.
// Verb annotation is required to display each verb command along with its usage
//
// When adding a new noun command use the GroupXX annotation
// When adding a new verb command use the GroupXXSub annotation

const (
	GroupAnnotation = "GroupAnnotation"
	GroupRecipe     = "recipe"
	GroupRecipeSub  = "recipeSub"
	GroupRun        = "run"
	GroupRunSub     = "runSub"
	GroupSupport    = "support"
	GroupSupportSub = "supportSub"
	GroupTarget     = "target"
	GroupTargetSub  = "targetSub"
	GroupRender     = "render"
	GroupRenderSub  = "renderSub"
	GroupDaemon     = "daemon"
	GroupDaemonSub  = "daemonSub"
	GroupConfig     = "config"
	GroupConfigSub  = "configSub"
	GroupSSH        = "ssh"
	GroupSSHSub     = "sshSub"
	GroupMCP        = "mcp"
	GroupMCPSub     = "mcpSub"
	GroupVersion    = "version"
	GroupHelp       = "help"
)

func UsageMessage() string {
	usage := `Usage:{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command] [operation]{{end}}{{if gt (len .Aliases) 0}}
  {{ $rootCommands := .Commands }}

Aliases:
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

Examples:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}
{{if (eq .Name .Root.Name)}}
Recipe: {{range $c := .Commands}}{{if (eq (index .Annotations "%[1]s") "%[2]s")}}{{.Short}} {{end}}{{range $c.Commands}}{{if (eq (index .Annotations "%[1]s") "%[3]s")}}
  {{rpad $c.Name .NamePadding }}{{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}

Run: {{range $c := .Commands}}{{if (eq (index .Annotations "%[1]s") "%[4]s")}}{{.Short}} {{end}}{{range $c.Commands}}{{if (eq (index .Annotations "%[1]s") "%[5]s")}}
  {{rpad $c.Name .NamePadding }}{{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}

Support: {{range $c := .Commands}}{{if (eq (index .Annotations "%[1]s") "%[6]s")}}{{.Short}} {{end}}{{range $c.Commands}}{{if (eq (index .Annotations "%[1]s") "%[7]s")}}
  {{rpad $c.Name .NamePadding }}{{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}

Target: {{range $c := .Commands}}{{if (eq (index .Annotations "%[1]s") "%[8]s")}}{{.Short}} {{end}}{{range $c.Commands}}{{if (eq (index .Annotations "%[1]s") "%[9]s")}}
  {{rpad $c.Name .NamePadding }}{{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}

Render: {{range $c := .Commands}}{{if (eq (index .Annotations "%[1]s") "%[10]s")}}{{.Short}} {{end}}{{range $c.Commands}}{{if (eq (index .Annotations "%[1]s") "%[11]s")}}
  {{rpad $c.Name .NamePadding }}{{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}

SSH: {{range $c := .Commands}}{{if (eq (index .Annotations "%[1]s") "%[12]s")}}{{.Short}} {{end}}{{range $c.Commands}}{{if (eq (index .Annotations "%[1]s") "%[13]s")}}
  {{rpad $c.Name .NamePadding }}{{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}

Daemon: {{range $c := .Commands}}{{if (eq (index .Annotations "%[1]s") "%[14]s")}}{{.Short}} {{end}}{{range $c.Commands}}{{if (eq (index .Annotations "%[1]s") "%[15]s")}}
  {{rpad $c.Name .NamePadding }}{{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}

Config: {{range $c := .Commands}}{{if (eq (index .Annotations "%[1]s") "%[16]s")}}{{.Short}} {{end}}{{range $c.Commands}}{{if (eq (index .Annotations "%[1]s") "%[17]s")}}
  {{rpad $c.Name .NamePadding }}{{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}

{{range $c := .Commands}}{{if (and $c.IsAvailableCommand (eq (index $c.Annotations "%[1]s") "%[18]s"))}}MCP: {{$c.Short}} {{range $c.Commands}}{{if (and .IsAvailableCommand (eq (index .Annotations "%[1]s") "%[19]s"))}}
  {{rpad $c.Name .NamePadding }}{{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}

{{end}}{{end}}Version: {{range .Commands}}{{if (or (eq (index .Annotations "%[1]s") "%[20]s") (eq .Name "version"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}

Help: {{range .Commands}}{{if (or (eq (index .Annotations "%[1]s") "%[21]s") (eq .Name "help") (eq .Name "completion"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}
{{else}}
Available Commands:{{range .Commands}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}
{{end}}{{end}}{{if .HasAvailableLocalFlags}}
Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

Global Flags:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasHelpSubCommands}}

Additional help topics:{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

Use "{{.CommandPath}} [command] --help" for more information about a command.{{end}}
`
	return fmt.Sprintf(usage,
		GroupAnnotation,
		GroupRecipe, GroupRecipeSub,
		GroupRun, GroupRunSub,
		GroupSupport, GroupSupportSub,
		GroupTarget, GroupTargetSub,
		GroupRender, GroupRenderSub,
		GroupSSH, GroupSSHSub,
		GroupDaemon, GroupDaemonSub,
		GroupConfig, GroupConfigSub,
		GroupMCP, GroupMCPSub,
		GroupVersion,
		GroupHelp)
}
