package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/spf13/cobra"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"
)

var listCmd = &cobra.Command{
	Use:   "list [repository:tag]",
	Short: "List available architectures for an artifact",
	Long:  `List all available architectures for a multi-architecture binary artifact.`,
	Args:  cobra.ExactArgs(1),
	Run:   runList,
}

var (
	listUsername string
	listPassword string
)

func init() {
	rootCmd.AddCommand(listCmd)
	listCmd.Flags().StringVarP(&listUsername, "username", "u", "", "Registry username")
	listCmd.Flags().StringVarP(&listPassword, "password", "p", "", "Registry password")
}

func runList(cmd *cobra.Command, args []string) {
	ref := args[0]

	verbose, _ := cmd.Flags().GetBool("verbose")
	insecure, _ := cmd.Flags().GetBool("insecure")

	ctx := context.Background()

	repo, err := createRepository(ref, insecure)
	if err != nil {
		exitWithError("failed to create repository", err)
	}

	// Try to get credentials
	username := listUsername
	password := listPassword

	// If credentials not provided via flags, try Docker config
	if username == "" || password == "" {
		if dockerUser, dockerPass, found := getDockerCredentials(repo.Reference.Registry); found {
			username = dockerUser
			password = dockerPass
			if verbose {
				fmt.Printf("Using credentials from ~/.docker/config.json\n")
			}
		}
	}

	if username != "" && password != "" {
		repo.Client = &auth.Client{
			Client:     retry.DefaultClient,
			Cache:      auth.NewCache(),
			Credential: auth.StaticCredential(repo.Reference.Registry, auth.Credential{
				Username: username,
				Password: password,
			}),
		}
	}

	descriptor, err := repo.Resolve(ctx, repo.Reference.Reference)
	if err != nil {
		exitWithError("failed to resolve reference", err)
	}

	if verbose {
		fmt.Printf("Resolved: %s\n", descriptor.Digest)
		fmt.Printf("Media Type: %s\n", descriptor.MediaType)
	}

	if descriptor.MediaType == ocispec.MediaTypeImageIndex {
		if err := listIndex(ctx, repo, descriptor); err != nil {
			exitWithError("failed to list index", err)
		}
	} else if descriptor.MediaType == ocispec.MediaTypeImageManifest {
		if err := listManifest(ctx, repo, descriptor); err != nil {
			exitWithError("failed to list manifest", err)
		}
	} else {
		fmt.Printf("Unknown media type: %s\n", descriptor.MediaType)
	}
}

func listIndex(ctx context.Context, repo *remote.Repository, descriptor ocispec.Descriptor) error {
	rc, err := repo.Fetch(ctx, descriptor)
	if err != nil {
		return err
	}
	defer rc.Close()

	indexBytes, err := io.ReadAll(rc)
	if err != nil {
		return err
	}

	var index ocispec.Index
	if err := json.Unmarshal(indexBytes, &index); err != nil {
		return err
	}

	fmt.Printf("Available platforms (%d):\n", len(index.Manifests))
	for _, manifest := range index.Manifests {
		if manifest.Platform != nil {
			fmt.Printf("  %s/%s (digest: %s, size: %d bytes)\n",
				manifest.Platform.OS,
				manifest.Platform.Architecture,
				manifest.Digest,
				manifest.Size,
			)
		}
	}

	return nil
}

func listManifest(ctx context.Context, repo *remote.Repository, descriptor ocispec.Descriptor) error {
	rc, err := repo.Fetch(ctx, descriptor)
	if err != nil {
		return err
	}
	defer rc.Close()

	manifestBytes, err := io.ReadAll(rc)
	if err != nil {
		return err
	}

	var manifest ocispec.Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return err
	}

	fmt.Printf("Single platform manifest:\n")
	if descriptor.Platform != nil {
		fmt.Printf("  Platform: %s/%s\n", descriptor.Platform.OS, descriptor.Platform.Architecture)
	}
	fmt.Printf("  Layers: %d\n", len(manifest.Layers))
	for i, layer := range manifest.Layers {
		fmt.Printf("    [%d] %s (size: %d bytes)\n", i, layer.Digest, layer.Size)
	}

	return nil
}
