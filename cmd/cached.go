package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var cachedCmd = &cobra.Command{
	Use:   "cached",
	Short: "List locally cached binaries",
	Long:  `List all binaries that have been cached locally from previous pull or run operations.`,
	Run:   runCached,
}

func init() {
	rootCmd.AddCommand(cachedCmd)
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

func runCached(cmd *cobra.Command, args []string) {
	verbose, _ := cmd.Flags().GetBool("verbose")

	cacheDir, err := getCacheDir()
	if err != nil {
		exitWithError("failed to get cache directory", err)
	}

	if _, err := os.Stat(cacheDir); os.IsNotExist(err) {
		fmt.Println("No cached binaries found")
		return
	}

	var entries []cacheEntry
	err = filepath.Walk(cacheDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Look for metadata files
		if !info.IsDir() && filepath.Base(path) == "metadata.json" {
			data, err := os.ReadFile(path)
			if err != nil {
				if verbose {
					fmt.Printf("Warning: failed to read %s: %v\n", path, err)
				}
				return nil
			}

			var meta cacheMetadata
			if err := json.Unmarshal(data, &meta); err != nil {
				if verbose {
					fmt.Printf("Warning: failed to parse %s: %v\n", path, err)
				}
				return nil
			}

			binaryPath := filepath.Join(filepath.Dir(path), fmt.Sprintf("%s-%s", meta.OS, meta.Architecture))
			binaryInfo, err := os.Stat(binaryPath)
			if err != nil {
				if verbose {
					fmt.Printf("Warning: binary not found for metadata %s\n", path)
				}
				return nil
			}

			entries = append(entries, cacheEntry{
				metadata:   meta,
				binaryPath: binaryPath,
				binarySize: binaryInfo.Size(),
			})
		}

		return nil
	})

	if err != nil {
		exitWithError("failed to scan cache directory", err)
	}

	if len(entries) == 0 {
		fmt.Println("No cached binaries found")
		return
	}

	fmt.Printf("Cached binaries (%d):\n\n", len(entries))
	for _, entry := range entries {
		ref := fmt.Sprintf("%s/%s:%s", entry.metadata.Registry, entry.metadata.Repository, entry.metadata.Tag)
		platform := fmt.Sprintf("%s/%s", entry.metadata.OS, entry.metadata.Architecture)

		fmt.Printf("  %s\n", ref)
		fmt.Printf("    Platform: %s\n", platform)
		fmt.Printf("    Digest: %s\n", entry.metadata.Digest)
		fmt.Printf("    Size: %s\n", formatSize(entry.binarySize))
		fmt.Printf("    Cached: %s\n", formatTime(entry.metadata.CachedAt))
		if verbose {
			fmt.Printf("    Path: %s\n", entry.binaryPath)
		}
		fmt.Println()
	}
}

type cacheEntry struct {
	metadata   cacheMetadata
	binaryPath string
	binarySize int64
}

func formatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func formatTime(t time.Time) string {
	duration := time.Since(t)

	if duration < time.Minute {
		return "just now"
	} else if duration < time.Hour {
		minutes := int(duration.Minutes())
		if minutes == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", minutes)
	} else if duration < 24*time.Hour {
		hours := int(duration.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	} else if duration < 7*24*time.Hour {
		days := int(duration.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	} else {
		return t.Format("2006-01-02")
	}
}

func getCacheDir() (string, error) {
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

func getCacheMetadataPath(cacheDir, registry, repository, tag string) string {
	registry = strings.ReplaceAll(registry, ":", "_")
	repository = strings.ReplaceAll(repository, "/", "_")

	return filepath.Join(cacheDir, registry, repository, tag, "metadata.json")
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
