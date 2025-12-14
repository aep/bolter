![Bolter Logo](logo.png)

# Bolter

[![Go Reference](https://pkg.go.dev/badge/github.com/aep/bolter.svg)](https://pkg.go.dev/github.com/aep/bolter)

a multi platform package manager for self-contained binaries

## CLI Usage

```bash
go install github.com/aep/bolter@latest
bolter push ghcr.io/me/myapp:v1.0.0 -b linux/amd64=linux-bin -b windows/amd64=windows.exe
bolter run ghcr.io/me/myapp:v1.0.0
```

## Library Usage

Bolter can also be used as a Go library to programmatically pull and run binaries from OCI registries:

```go
import "github.com/aep/bolter/pkg/bolter"

// Pull a binary
info, err := bolter.Pull(ctx, "ghcr.io/myuser/myapp:v1.0.0")

// Run a binary
err := bolter.Run(ctx, "ghcr.io/myuser/myapp:v1.0.0", []string{"--help"})
```
