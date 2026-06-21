package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version is the RDDA release. Bumped manually only.
const Version = "0.1.0-dev"

func newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "rdda",
		Short:         "RDDA — Russian Doll Double Agent control CLI",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the rdda version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintln(cmd.OutOrStdout(), Version)
		},
	})
	return root
}

// Execute runs the rdda CLI.
func Execute() error {
	return newRoot().Execute()
}
