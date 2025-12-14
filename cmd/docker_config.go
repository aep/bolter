package cmd

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type dockerConfig struct {
	Auths map[string]dockerAuthConfig `json:"auths"`
}

type dockerAuthConfig struct {
	Auth string `json:"auth"`
}

func getDockerConfigRegistries() []string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	configPath := filepath.Join(homeDir, ".docker", "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil
	}

	var config dockerConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil
	}

	registries := make([]string, 0, len(config.Auths))
	for reg := range config.Auths {
		registries = append(registries, reg)
	}
	return registries
}

func getDockerCredentials(registry string) (username, password string, found bool) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", "", false
	}

	configPath := filepath.Join(homeDir, ".docker", "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return "", "", false
	}

	var config dockerConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return "", "", false
	}

	// Try multiple registry key formats
	registryKeys := []string{
		registry,
		normalizeRegistry(registry),
		"https://" + registry,
		strings.TrimPrefix(registry, "docker.io/"),
	}

	// Add Docker Hub specific variants
	if strings.Contains(registry, "docker.io") || registry == "docker.io" {
		registryKeys = append(registryKeys,
			"https://index.docker.io/v1/",
			"index.docker.io",
			"docker.io",
		)
	}

	// Try to find auth for this registry
	for _, key := range registryKeys {
		if authConfig, ok := config.Auths[key]; ok && authConfig.Auth != "" {
			// Decode base64 auth string
			decoded, err := base64.StdEncoding.DecodeString(authConfig.Auth)
			if err != nil {
				continue
			}

			// Auth string is in format "username:password"
			parts := strings.SplitN(string(decoded), ":", 2)
			if len(parts) == 2 {
				return parts[0], parts[1], true
			}
		}
	}

	return "", "", false
}

func normalizeRegistry(registry string) string {
	// Remove protocol if present
	registry = strings.TrimPrefix(registry, "https://")
	registry = strings.TrimPrefix(registry, "http://")

	// Docker Hub special cases
	if registry == "docker.io" || registry == "index.docker.io" {
		return "https://index.docker.io/v1/"
	}

	return registry
}
