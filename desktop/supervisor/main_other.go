//go:build !windows

package main

import (
	"fmt"
	"os"
)

// Keep root-level `go build ./...` usable on non-Windows development hosts.
func main() {
	fmt.Fprintln(os.Stderr, "the desktop supervisor requires Windows; use desktop/build-win-x64.sh")
	os.Exit(1)
}
