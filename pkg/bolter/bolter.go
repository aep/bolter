package bolter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"
)

// PullOptions configures the Pull operation
type PullOptions struct {
	// Output path for the binary. If empty, binary is only cached.
	Output string
	// Platform in format "os/arch" (e.g., "linux/amd64"). Defaults to current platform.
	Platform string
	// Username for registry authentication
	Username string
	// Password for registry authentication
	Password string
	// Insecure allows insecure registry connections
	Insecure bool
	// Verbose enables verbose output
	Verbose bool
	// UseCache enables caching of downloaded binaries
	UseCache bool
	// CacheDir overrides the default cache directory (~/.cache/bolter)
	CacheDir string
}

// RunOptions configures the Run operation
type RunOptions struct {
	// Platform in format "os/arch" (e.g., "linux/amd64"). Defaults to current platform.
	Platform string
	// Username for registry authentication
	Username string
	// Password for registry authentication
	Password string
	// Insecure allows insecure registry connections
	Insecure bool
	// Verbose enables verbose output
	Verbose bool
	// NoCache disables use of cached binaries
	NoCache bool
	// CacheDir overrides the default cache directory (~/.cache/bolter)
	CacheDir string
	// UseExec uses syscall.Exec() to replace the current process (CLI behavior)
	// If false, uses exec.Command() and returns after execution
	UseExec bool
}

// BinaryInfo contains information about a pulled binary
type BinaryInfo struct {
	// Path to the binary file
	Path string
	// Digest of the manifest
	Digest string
	// Size of the binary in bytes
	Size int64
	// OS of the binary
	OS string
	// Architecture of the binary
	Architecture string
	// Cached indicates if the binary was served from cache
	Cached bool
}

// Pull downloads a binary from an OCI registry
func Pull(ctx context.Context, ref string, opts PullOptions) (*BinaryInfo, error) {
	targetOS, targetArch := parsePlatform(opts.Platform)

	if opts.Verbose {
		fmt.Printf("Pulling for %s/%s...\n", targetOS, targetArch)
	}

	repo, err := createRepository(ref, opts.Insecure)
	if err != nil {
		return nil, fmt.Errorf("failed to create repository: %w", err)
	}

	// Setup authentication
	if err := setupAuth(repo, opts.Username, opts.Password, opts.Verbose); err != nil {
		return nil, err
	}

	descriptor, err := repo.Resolve(ctx, repo.Reference.Reference)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve reference: %w", err)
	}

	var manifestDesc *ocispec.Descriptor

	if descriptor.MediaType == ocispec.MediaTypeImageIndex {
		manifestDesc, err = findManifestForPlatform(ctx, repo, descriptor, targetOS, targetArch)
		if err != nil {
			return nil, fmt.Errorf("failed to find manifest for platform: %w", err)
		}
	} else if descriptor.MediaType == ocispec.MediaTypeImageManifest {
		manifestDesc = &descriptor
	} else {
		return nil, fmt.Errorf("unsupported media type: %s", descriptor.MediaType)
	}

	// Determine output path
	outputPath := opts.Output
	var cacheDir string
	var cachedBinary string

	if opts.UseCache {
		cacheDir, err = getCacheDir(opts.CacheDir)
		if err != nil {
			return nil, fmt.Errorf("failed to get cache directory: %w", err)
		}
		cachedBinary = getCachePathForRef(cacheDir, repo.Reference.Registry, repo.Reference.Repository, repo.Reference.Reference, targetOS, targetArch)

		// If no output specified, use cache path
		if outputPath == "" {
			outputPath = cachedBinary
		}
	} else if outputPath == "" {
		return nil, fmt.Errorf("output path required when caching is disabled")
	}

	// Pull the binary
	if err := pullBinary(ctx, repo, *manifestDesc, outputPath); err != nil {
		return nil, fmt.Errorf("failed to pull binary: %w", err)
	}

	if err := os.Chmod(outputPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to make binary executable: %w", err)
	}

	// Get file info
	fileInfo, err := os.Stat(outputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat binary: %w", err)
	}

	info := &BinaryInfo{
		Path:         outputPath,
		Digest:       manifestDesc.Digest.String(),
		Size:         fileInfo.Size(),
		OS:           targetOS,
		Architecture: targetArch,
		Cached:       false,
	}

	// Save to cache if using cache and output is not the cache path
	if opts.UseCache && outputPath != cachedBinary {
		if err := os.MkdirAll(filepath.Dir(cachedBinary), 0755); err == nil {
			if err := copyFile(outputPath, cachedBinary); err == nil {
				os.Chmod(cachedBinary, 0755)
				if err := saveCacheMetadata(cacheDir, repo.Reference.Registry, repo.Reference.Repository, repo.Reference.Reference, targetOS, targetArch, manifestDesc.Digest.String(), fileInfo.Size()); err != nil && opts.Verbose {
					fmt.Printf("Warning: failed to save cache metadata: %v\n", err)
				}
			} else if opts.Verbose {
				fmt.Printf("Warning: failed to cache binary: %v\n", err)
			}
		}
	} else if opts.UseCache {
		// Save cache metadata
		if err := saveCacheMetadata(cacheDir, repo.Reference.Registry, repo.Reference.Repository, repo.Reference.Reference, targetOS, targetArch, manifestDesc.Digest.String(), fileInfo.Size()); err != nil && opts.Verbose {
			fmt.Printf("Warning: failed to save cache metadata: %v\n", err)
		}
	}

	if opts.Verbose {
		fmt.Printf("Successfully pulled to %s\n", outputPath)
	}

	return info, nil
}

// Run downloads (if not cached) and executes a binary from an OCI registry
func Run(ctx context.Context, ref string, args []string, opts RunOptions) error {
	targetOS, targetArch := parsePlatform(opts.Platform)

	cacheDir, err := getCacheDir(opts.CacheDir)
	if err != nil {
		return fmt.Errorf("failed to get cache directory: %w", err)
	}

	repo, err := createRepository(ref, opts.Insecure)
	if err != nil {
		return fmt.Errorf("failed to create repository: %w", err)
	}

	cachedBinary := getCachePathForRef(cacheDir, repo.Reference.Registry, repo.Reference.Repository, repo.Reference.Reference, targetOS, targetArch)

	// Check cache first
	if !opts.NoCache {
		if _, err := os.Stat(cachedBinary); err == nil {
			if opts.Verbose {
				fmt.Printf("Using cached binary: %s\n", cachedBinary)
			}
			return executeBinary(cachedBinary, args, opts.UseExec)
		}
	}

	if opts.Verbose {
		fmt.Printf("Pulling binary for %s/%s...\n", targetOS, targetArch)
	}

	// Setup authentication
	if err := setupAuth(repo, opts.Username, opts.Password, opts.Verbose); err != nil {
		return err
	}

	descriptor, err := repo.Resolve(ctx, repo.Reference.Reference)
	if err != nil {
		return fmt.Errorf("failed to resolve reference: %w", err)
	}

	var manifestDesc *ocispec.Descriptor

	if descriptor.MediaType == ocispec.MediaTypeImageIndex {
		manifestDesc, err = findManifestForPlatform(ctx, repo, descriptor, targetOS, targetArch)
		if err != nil {
			return fmt.Errorf("failed to find manifest for platform: %w", err)
		}
	} else if descriptor.MediaType == ocispec.MediaTypeImageManifest {
		manifestDesc = &descriptor
	} else {
		return fmt.Errorf("unsupported media type: %s", descriptor.MediaType)
	}

	if err := os.MkdirAll(filepath.Dir(cachedBinary), 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	if err := pullBinary(ctx, repo, *manifestDesc, cachedBinary); err != nil {
		return fmt.Errorf("failed to pull binary: %w", err)
	}

	if err := os.Chmod(cachedBinary, 0755); err != nil {
		return fmt.Errorf("failed to make binary executable: %w", err)
	}

	// Save cache metadata
	fileInfo, err := os.Stat(cachedBinary)
	if err == nil {
		if err := saveCacheMetadata(cacheDir, repo.Reference.Registry, repo.Reference.Repository, repo.Reference.Reference, targetOS, targetArch, manifestDesc.Digest.String(), fileInfo.Size()); err != nil && opts.Verbose {
			fmt.Printf("Warning: failed to save cache metadata: %v\n", err)
		}
	}

	if opts.Verbose {
		fmt.Printf("Pulled to cache: %s\n", cachedBinary)
	}

	return executeBinary(cachedBinary, args, opts.UseExec)
}

// Helper functions

func parsePlatform(platform string) (goos, goarch string) {
	if platform == "" {
		return runtime.GOOS, runtime.GOARCH
	}

	for i, ch := range platform {
		if ch == '/' {
			return platform[:i], platform[i+1:]
		}
	}
	return runtime.GOOS, runtime.GOARCH
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

func setupAuth(repo *remote.Repository, username, password string, verbose bool) error {
	// Try Docker config if credentials not provided
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
			Client: retry.DefaultClient,
			Cache:  auth.NewCache(),
			Credential: auth.StaticCredential(repo.Reference.Registry, auth.Credential{
				Username: username,
				Password: password,
			}),
		}
	}

	return nil
}

func findManifestForPlatform(ctx context.Context, repo *remote.Repository, indexDesc ocispec.Descriptor, targetOS, targetArch string) (*ocispec.Descriptor, error) {
	rc, err := repo.Fetch(ctx, indexDesc)
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	indexBytes, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}

	var index ocispec.Index
	if err := json.Unmarshal(indexBytes, &index); err != nil {
		return nil, err
	}

	for _, manifest := range index.Manifests {
		if manifest.Platform != nil &&
			manifest.Platform.OS == targetOS &&
			manifest.Platform.Architecture == targetArch {
			return &manifest, nil
		}
	}

	return nil, fmt.Errorf("no manifest found for %s/%s", targetOS, targetArch)
}

func pullBinary(ctx context.Context, repo *remote.Repository, manifestDesc ocispec.Descriptor, output string) error {
	rc, err := repo.Fetch(ctx, manifestDesc)
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

	if len(manifest.Layers) == 0 {
		return fmt.Errorf("manifest has no layers")
	}

	layerDesc := manifest.Layers[0]

	rc, err = repo.Fetch(ctx, layerDesc)
	if err != nil {
		return err
	}
	defer rc.Close()

	outputDir := filepath.Dir(output)
	if outputDir != "." && outputDir != "" {
		if err := os.MkdirAll(outputDir, 0755); err != nil {
			return err
		}
	}

	outFile, err := os.Create(output)
	if err != nil {
		return err
	}
	defer outFile.Close()

	_, err = io.Copy(outFile, rc)
	return err
}

func executeBinary(binaryPath string, args []string, useExec bool) error {
	binary, err := exec.LookPath(binaryPath)
	if err != nil {
		return err
	}

	if useExec {
		// Replace current process (CLI behavior)
		execArgs := append([]string{binary}, args...)
		env := os.Environ()
		return syscall.Exec(binary, execArgs, env)
	}

	// Run as subprocess (library behavior)
	cmd := exec.Command(binary, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("binary exited with code %d", exitErr.ExitCode())
		}
		return err
	}

	return nil
}

func getCacheDir(customCacheDir string) (string, error) {
	if customCacheDir != "" {
		return customCacheDir, nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(homeDir, ".cache", "bolter"), nil
}

func getCachePathForRef(cacheDir, registry, repository, tag, goos, arch string) string {
	// Normalize registry name
	registry = strings.ReplaceAll(registry, ":", "_")
	repository = strings.ReplaceAll(repository, "/", "_")

	return filepath.Join(cacheDir, registry, repository, tag, fmt.Sprintf("%s-%s", goos, arch))
}

func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}

type cacheMetadata struct {
	Registry     string    `json:"registry"`
	Repository   string    `json:"repository"`
	Tag          string    `json:"tag"`
	OS           string    `json:"os"`
	Architecture string    `json:"architecture"`
	Digest       string    `json:"digest"`
	Size         int64     `json:"size"`
	CachedAt     time.Time `json:"cached_at"`
}

func saveCacheMetadata(cacheDir, registry, repository, tag, goos, arch, digest string, size int64) error {
	registry = strings.ReplaceAll(registry, ":", "_")
	repository = strings.ReplaceAll(repository, "/", "_")

	metaPath := filepath.Join(cacheDir, registry, repository, tag, "metadata.json")

	meta := cacheMetadata{
		Registry:     registry,
		Repository:   repository,
		Tag:          tag,
		OS:           goos,
		Architecture: arch,
		Digest:       digest,
		Size:         size,
		CachedAt:     time.Now(),
	}

	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(metaPath), 0755); err != nil {
		return err
	}

	return os.WriteFile(metaPath, data, 0644)
}
