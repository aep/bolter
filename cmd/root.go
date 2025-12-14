package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "bolter",
	Short: "Manage multi-architecture native binaries as OCI artifacts",
	Long: `Bolter is a CLI tool for uploading, listing, downloading, and executing
native binaries as OCI artifacts with multi-architecture support.`,
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringP("registry", "r", "", "OCI registry URL (e.g., localhost:5000)")
	rootCmd.PersistentFlags().BoolP("insecure", "", false, "Allow insecure registry connections")
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "Enable verbose output")
}

func exitWithError(msg string, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s: %v\n", msg, err)
	} else {
		fmt.Fprintf(os.Stderr, "Error: %s\n", msg)
	}
	os.Exit(1)
}
