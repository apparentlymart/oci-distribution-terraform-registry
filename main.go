package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/apparentlymart/go-userdirs/userdirs"
	"github.com/apparentlymart/oci-distribution-terraform-registry/internal/config"
	"github.com/apparentlymart/oci-distribution-terraform-registry/internal/server"
	"github.com/hashicorp/hcl/v2"
	"github.com/spf13/cobra"
)

func main() {
	err := rootCommand().Execute()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to execute: %s\n", err)
		os.Exit(1)
	}
}

func rootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "oci-distribution-terraform-registry",
		Short: "A proxy for providing Terraform registry services using an OCI Distribution server.",
	}
	root.SetUsageTemplate(usageTemplate)
	cmdLineConfigFile := root.PersistentFlags().String("config", "", "Configuration file to use")
	var globalConfig *config.Config

	root.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		var configFile string
		if *cmdLineConfigFile != "" {
			configFile = *cmdLineConfigFile
		} else {
			candidates := dirs.FindConfigFiles("config.hcl")
			if len(candidates) == 0 {
				fmt.Fprintf(
					cmd.ErrOrStderr(),
					"Error: No configuration file found.\n\nEither specify a config file using the --config option, or place config.hcl\nin one of the following directories:\n",
				)
				for _, dir := range dirs.ConfigDirs {
					fmt.Fprintf(cmd.ErrOrStderr(), " - %s\n", dir)
				}
				os.Exit(1)
			}
			if len(candidates) != 1 {
				fmt.Fprintf(
					cmd.ErrOrStderr(),
					"Error: Multiple configuration files found.\n\nUse the --config option to specify which configuration file to use.\nFound the following configuration files:\n",
				)
				for _, filename := range candidates {
					fmt.Fprintf(cmd.ErrOrStderr(), " - %s\n", filename)
				}
				os.Exit(1)
			}
			configFile = candidates[0]
		}

		gotConfig, diags := config.LoadConfigFile(configFile)
		for _, diag := range diags {
			severity := "Problem"
			switch diag.Severity {
			case hcl.DiagError:
				severity = "Error"
			case hcl.DiagWarning:
				severity = "Warning"
			}
			prefix := severity
			if diag.Subject != nil {
				prefix = fmt.Sprintf("%s at %s", severity, *diag.Subject)
			}
			detail := ""
			if diag.Detail != "" {
				detail = "\n\n" + diag.Detail + "\n"
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "%s: %s%s\n", prefix, diag.Summary, detail)
		}
		if diags.HasErrors() {
			fmt.Fprintf(cmd.ErrOrStderr(), "\nConfiguration is invalid.\n")
			os.Exit(1)
		}
		globalConfig = gotConfig
	}

	root.AddCommand(
		&cobra.Command{
			Use:   "server",
			Short: "Run a server providing all of the services described in the configuration",
			Run: func(cmd *cobra.Command, args []string) {
				ctx, cancel := context.WithCancel(context.Background())
				go func() {
					defer cancel()

					signalCh := make(chan os.Signal, 1)
					signal.Notify(signalCh, os.Interrupt)

					select {
					case <-signalCh:
					case <-ctx.Done():
					}
				}()
				server.Run(ctx, globalConfig)
			},
		},
	)

	return root
}

var dirs = userdirs.ForApp(
	"OCI Distribution Terraform Registry",
	"apparentlymart",
	"io.terraform.oci-distribution-terraform-registry",
)

const usageTemplate = `Usage:{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{if gt (len .Aliases) 0}}

Aliases:
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

Examples:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

Available subcommands:{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{.Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

Additional subcommands:{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

Options:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

Global options:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasHelpSubCommands}}

Additional help topics:{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

Use "{{.CommandPath}} [command] --help" for more information about a command.{{end}}
`
