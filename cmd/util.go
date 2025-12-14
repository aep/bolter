package cmd

import (
	"io"
	"os"
	"runtime"
)

func parsePlatform(platform string) (goos, goarch string) {
	for i, ch := range platform {
		if ch == '/' {
			return platform[:i], platform[i+1:]
		}
	}
	return runtime.GOOS, runtime.GOARCH
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
