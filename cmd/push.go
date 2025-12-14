package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/spf13/cobra"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/memory"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"
)

var pushCmd = &cobra.Command{
	Use:   "push [repository:tag]",
	Short: "Push multi-architecture binaries as OCI artifacts",
	Long: `Push native binaries as OCI artifacts with multi-architecture manifest support.

Example:
  bolter push myregistry.io/app:v1.0.0 \
    -b linux/amd64=./bin/app-linux-amd64 \
    -b linux/arm64=./bin/app-linux-arm64 \
    -b darwin/amd64=./bin/app-darwin-amd64`,
	Args: cobra.ExactArgs(1),
	Run:  runPush,
}

var (
	pushUsername  string
	pushPassword  string
	pushPlatforms []string
)

func init() {
	rootCmd.AddCommand(pushCmd)
	pushCmd.Flags().StringVarP(&pushUsername, "username", "u", "", "Registry username")
	pushCmd.Flags().StringVarP(&pushPassword, "password", "p", "", "Registry password")
	pushCmd.Flags().StringArrayVarP(&pushPlatforms, "bin", "b", nil, "Platform mapping in format os/arch=path (e.g., linux/amd64=./bin/myapp)")
	pushCmd.MarkFlagRequired("bin")
}

type platformBinary struct {
	path string
	os   string
	arch string
}

func runPush(cmd *cobra.Command, args []string) {
	ref := args[0]

	verbose, _ := cmd.Flags().GetBool("verbose")
	insecure, _ := cmd.Flags().GetBool("insecure")

	if len(pushPlatforms) == 0 {
		exitWithError("no bin specified, use --bin os/arch=path", nil)
	}

	binaries, err := parsePlatformMappings(pushPlatforms)
	if err != nil {
		exitWithError("failed to parse bin mappings", err)
	}

	if verbose {
		fmt.Printf("Pushing %d binaries...\n", len(binaries))
	}

	ctx := context.Background()

	repo, err := createRepository(ref, insecure)
	if err != nil {
		exitWithError("failed to create repository", err)
	}

	// Try to get credentials
	username := pushUsername
	password := pushPassword

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

	memoryStore := memory.New()

	var manifestDescriptors []ocispec.Descriptor

	for i, binary := range binaries {
		fmt.Printf("[%d/%d] Pushing %s/%s... ", i+1, len(binaries), binary.os, binary.arch)
		descriptor, err := pushBinary(ctx, memoryStore, repo, binary, verbose)
		if err != nil {
			fmt.Printf("FAILED\n")
			exitWithError(fmt.Sprintf("failed to push binary %s/%s", binary.os, binary.arch), err)
		}
		manifestDescriptors = append(manifestDescriptors, descriptor)
		fmt.Printf("✓\n")
	}

	fmt.Printf("Creating manifest index... ")
	indexDescriptor, err := createAndPushIndex(ctx, memoryStore, repo, manifestDescriptors, verbose)
	if err != nil {
		fmt.Printf("FAILED\n")
		exitWithError("failed to create manifest index", err)
	}
	fmt.Printf("✓\n")

	if err := repo.Tag(ctx, indexDescriptor, repo.Reference.Reference); err != nil {
		exitWithError("failed to tag manifest", err)
	}

	fmt.Printf("\nSuccessfully pushed %d binaries to %s\n", len(binaries), ref)
	fmt.Printf("Manifest digest: %s\n", indexDescriptor.Digest)
}

func parsePlatformMappings(platforms []string) ([]platformBinary, error) {
	var binaries []platformBinary

	for _, platform := range platforms {
		parts := strings.SplitN(platform, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid platform format: %s (expected os/arch=path)", platform)
		}

		osParts := strings.Split(parts[0], "/")
		if len(osParts) != 2 {
			return nil, fmt.Errorf("invalid platform format: %s (expected os/arch)", parts[0])
		}

		path := parts[1]
		if _, err := os.Stat(path); err != nil {
			return nil, fmt.Errorf("file not found: %s", path)
		}

		binaries = append(binaries, platformBinary{
			path: path,
			os:   osParts[0],
			arch: osParts[1],
		})
	}

	return binaries, nil
}

func pushBinary(ctx context.Context, memoryStore *memory.Store, repo *remote.Repository, binary platformBinary, verbose bool) (ocispec.Descriptor, error) {
	data, err := os.ReadFile(binary.path)
	if err != nil {
		return ocispec.Descriptor{}, err
	}

	binaryDesc := ocispec.Descriptor{
		MediaType: getMediaTypeForPlatform(binary.os, binary.arch),
		Digest:    digest.FromBytes(data),
		Size:      int64(len(data)),
		Annotations: map[string]string{
			"org.opencontainers.image.title": fmt.Sprintf("%s-%s", binary.os, binary.arch),
		},
	}

	if err := memoryStore.Push(ctx, binaryDesc, strings.NewReader(string(data))); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "already exists") {
			return ocispec.Descriptor{}, err
		}
	}

	configDesc := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageConfig,
		Digest:    digest.FromBytes([]byte("{}")),
		Size:      2,
	}

	if err := memoryStore.Push(ctx, configDesc, strings.NewReader("{}")); err != nil {
		// Ignore "already exists" - same config is used for all binaries
		if !strings.Contains(strings.ToLower(err.Error()), "already exists") {
			return ocispec.Descriptor{}, err
		}
	}

	manifestBytes := []byte(fmt.Sprintf(`{"schemaVersion":2,"mediaType":"%s","config":{"mediaType":"%s","digest":"%s","size":%d},"layers":[{"mediaType":"%s","digest":"%s","size":%d,"annotations":{%s}}]}`,
		ocispec.MediaTypeImageManifest,
		configDesc.MediaType, configDesc.Digest, configDesc.Size,
		binaryDesc.MediaType, binaryDesc.Digest, binaryDesc.Size,
		fmt.Sprintf(`"org.opencontainers.image.title":"%s"`, fmt.Sprintf("%s-%s", binary.os, binary.arch)),
	))

	manifestDesc := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageManifest,
		Digest:    digest.FromBytes(manifestBytes),
		Size:      int64(len(manifestBytes)),
		Platform: &ocispec.Platform{
			OS:           binary.os,
			Architecture: binary.arch,
		},
	}

	if err := memoryStore.Push(ctx, manifestDesc, strings.NewReader(string(manifestBytes))); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "already exists") {
			return ocispec.Descriptor{}, err
		}
	}

	if err := oras.CopyGraph(ctx, memoryStore, repo, manifestDesc, oras.DefaultCopyGraphOptions); err != nil {
		// Ignore "already exists" errors - this just means the blob is already in the registry
		if !strings.Contains(strings.ToLower(err.Error()), "already exists") {
			return ocispec.Descriptor{}, err
		}
	}

	return manifestDesc, nil
}

func createAndPushIndex(ctx context.Context, memoryStore *memory.Store, repo *remote.Repository, manifests []ocispec.Descriptor, verbose bool) (ocispec.Descriptor, error) {
	var manifestsJSON strings.Builder
	manifestsJSON.WriteString("[")
	for i, m := range manifests {
		if i > 0 {
			manifestsJSON.WriteString(",")
		}
		manifestsJSON.WriteString(fmt.Sprintf(`{"mediaType":"%s","digest":"%s","size":%d,"platform":{"os":"%s","architecture":"%s"}}`,
			m.MediaType, m.Digest, m.Size, m.Platform.OS, m.Platform.Architecture))
	}
	manifestsJSON.WriteString("]")

	indexBytes := []byte(fmt.Sprintf(`{"schemaVersion":2,"mediaType":"%s","manifests":%s}`,
		ocispec.MediaTypeImageIndex, manifestsJSON.String()))

	indexDesc := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageIndex,
		Digest:    digest.FromBytes(indexBytes),
		Size:      int64(len(indexBytes)),
	}

	if err := memoryStore.Push(ctx, indexDesc, strings.NewReader(string(indexBytes))); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "already exists") {
			return ocispec.Descriptor{}, err
		}
	}

	if err := oras.CopyGraph(ctx, memoryStore, repo, indexDesc, oras.DefaultCopyGraphOptions); err != nil {
		// Ignore "already exists" errors - this just means the blob is already in the registry
		if !strings.Contains(strings.ToLower(err.Error()), "already exists") {
			return ocispec.Descriptor{}, err
		}
	}

	return indexDesc, nil
}

func createRepository(ref string, insecure bool) (*remote.Repository, error) {
	// If ref doesn't contain a registry (no . or : in first component),
	// default to Docker Hub
	if !strings.Contains(strings.Split(ref, "/")[0], ".") &&
	   !strings.Contains(strings.Split(ref, "/")[0], ":") {
		ref = "docker.io/" + ref
	}

	repo, err := remote.NewRepository(ref)
	if err != nil {
		return nil, err
	}

	repo.PlainHTTP = insecure

	return repo, nil
}

func getMediaTypeForPlatform(goos, goarch string) string {
	// WASM has special handling
	if goos == "js" || goos == "wasip1" || goarch == "wasm" {
		return "application/vnd.bolter.wasm.v1"
	}

	// Platform-specific binary formats
	switch goos {
	case "windows":
		return "application/vnd.bolter.windows.exe.v1"
	case "darwin":
		return "application/vnd.bolter.macho.v1"
	case "linux", "freebsd", "openbsd", "netbsd", "dragonfly", "solaris", "illumos":
		return "application/vnd.bolter.elf.v1"
	case "plan9":
		return "application/vnd.bolter.plan9.v1"
	case "aix":
		return "application/vnd.bolter.xcoff.v1"
	default:
		return "application/vnd.bolter.binary.v1"
	}
}
