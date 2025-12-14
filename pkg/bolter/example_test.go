package bolter_test

import (
	"context"
	"fmt"
	"log"

	"github.com/aep/bolter/pkg/bolter"
)

// Example demonstrating how to pull a binary from a registry
func ExamplePull() {
	ctx := context.Background()

	opts := bolter.PullOptions{
		Output:   "./myapp",
		Platform: "linux/amd64",
		Verbose:  true,
		UseCache: true,
	}

	info, err := bolter.Pull(ctx, "myregistry.io/myapp:latest", opts)
	if err != nil {
		log.Fatalf("Failed to pull binary: %v", err)
	}

	fmt.Printf("Binary downloaded to: %s\n", info.Path)
	fmt.Printf("Digest: %s\n", info.Digest)
	fmt.Printf("Size: %d bytes\n", info.Size)
}

// Example demonstrating how to run a binary from a registry
func ExampleRun() {
	ctx := context.Background()

	opts := bolter.RunOptions{
		Platform: "linux/amd64",
		Verbose:  true,
		UseExec:  false, // Use exec.Command instead of syscall.Exec
	}

	// Run the binary with arguments
	err := bolter.Run(ctx, "myregistry.io/myapp:latest", []string{"--help"}, opts)
	if err != nil {
		log.Fatalf("Failed to run binary: %v", err)
	}
}

// Example demonstrating authenticated pull
func ExamplePull_withAuth() {
	ctx := context.Background()

	opts := bolter.PullOptions{
		Output:   "./myapp",
		Username: "myuser",
		Password: "mypassword",
		UseCache: true,
	}

	info, err := bolter.Pull(ctx, "myregistry.io/private/myapp:v1.0", opts)
	if err != nil {
		log.Fatalf("Failed to pull binary: %v", err)
	}

	fmt.Printf("Binary downloaded to: %s\n", info.Path)
}

// Example demonstrating pull without cache
func ExamplePull_noCache() {
	ctx := context.Background()

	opts := bolter.PullOptions{
		Output:   "./myapp",
		UseCache: false, // Don't use cache
	}

	_, err := bolter.Pull(ctx, "myregistry.io/myapp:latest", opts)
	if err != nil {
		log.Fatalf("Failed to pull binary: %v", err)
	}
}

// Example demonstrating run with custom cache directory
func ExampleRun_customCache() {
	ctx := context.Background()

	opts := bolter.RunOptions{
		CacheDir: "/tmp/mycache",
		UseExec:  false,
	}

	err := bolter.Run(ctx, "myregistry.io/myapp:latest", []string{"version"}, opts)
	if err != nil {
		log.Fatalf("Failed to run binary: %v", err)
	}
}
