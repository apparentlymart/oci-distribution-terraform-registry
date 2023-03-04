package main

import (
	"fmt"
	"os"

	"github.com/apparentlymart/oci-distribution-terraform-registry/internal/server"

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

	root.AddCommand(
		&cobra.Command{
			Use:   "server",
			Short: "Run a server providing all of the services described in the configuration",
			Run: func(cmd *cobra.Command, args []string) {
				server.Run()
			},
		},
	)

	return root
}
