package cmd

import (
	"context"

	"github.com/aep/bolter/pkg/bolter"
	"github.com/spf13/cobra"
)

var executeCmd = &cobra.Command{
	Use:   "run [repository:tag] [-- args...]",
	Short: "Execute a binary from the registry",
	Long: `Download (if not cached) and execute a binary from the registry for the current platform.
Binaries are cached locally to avoid repeated downloads.`,
	Args: cobra.MinimumNArgs(1),
	Run:  runExecute,
}

var (
	executeUsername string
	executePassword string
	executePlatform string
	executeNoCache  bool
)

func init() {
	rootCmd.AddCommand(executeCmd)
	executeCmd.Flags().StringVarP(&executeUsername, "username", "u", "", "Registry username")
	executeCmd.Flags().StringVarP(&executePassword, "password", "p", "", "Registry password")
	executeCmd.Flags().StringVar(&executePlatform, "platform", "", "Platform to execute (e.g., linux/amd64). Defaults to current platform")
	executeCmd.Flags().BoolVar(&executeNoCache, "no-cache", false, "Don't use cached binaries, always download")
}

func runExecute(cmd *cobra.Command, args []string) {
	ref := args[0]
	execArgs := args[1:]

	verbose, _ := cmd.Flags().GetBool("verbose")
	insecure, _ := cmd.Flags().GetBool("insecure")

	ctx := context.Background()

	opts := bolter.RunOptions{
		Platform: executePlatform,
		Username: executeUsername,
		Password: executePassword,
		Insecure: insecure,
		Verbose:  verbose,
		NoCache:  executeNoCache,
		UseExec:  true, // CLI uses syscall.Exec to replace process
	}

	if err := bolter.Run(ctx, ref, execArgs, opts); err != nil {
		exitWithError("run failed", err)
	}
}

