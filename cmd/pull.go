package cmd

import (
	"context"
	"fmt"

	"github.com/aep/bolter/pkg/bolter"
	"github.com/spf13/cobra"
)

var pullCmd = &cobra.Command{
	Use:   "pull [repository:tag] [output]",
	Short: "Pull a binary for the current or specified architecture",
	Long: `Pull a binary artifact for the current architecture or a specified platform.
The binary will be saved to the specified output path.`,
	Args: cobra.RangeArgs(1, 2),
	Run:  runPull,
}

var (
	pullUsername string
	pullPassword string
	pullPlatform string
)

func init() {
	rootCmd.AddCommand(pullCmd)
	pullCmd.Flags().StringVarP(&pullUsername, "username", "u", "", "Registry username")
	pullCmd.Flags().StringVarP(&pullPassword, "password", "p", "", "Registry password")
	pullCmd.Flags().StringVar(&pullPlatform, "platform", "", "Platform to pull (e.g., linux/amd64). Defaults to current platform")
}

func runPull(cmd *cobra.Command, args []string) {
	ref := args[0]
	output := "binary"
	if len(args) > 1 {
		output = args[1]
	}

	verbose, _ := cmd.Flags().GetBool("verbose")
	insecure, _ := cmd.Flags().GetBool("insecure")

	ctx := context.Background()

	opts := bolter.PullOptions{
		Output:   output,
		Platform: pullPlatform,
		Username: pullUsername,
		Password: pullPassword,
		Insecure: insecure,
		Verbose:  verbose,
		UseCache: true,
	}

	info, err := bolter.Pull(ctx, ref, opts)
	if err != nil {
		exitWithError("pull failed", err)
	}

	fmt.Printf("Successfully pulled to %s\n", info.Path)
}

