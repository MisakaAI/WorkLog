//go:build !windows

package config

import (
	"os"

	"worklog/internal/diagnostics"
)

func checkTokenPermissions(_ *os.File, info os.FileInfo) error {
	if info.Mode().Perm()&0077 != 0 {
		return diagnostics.Safe("token file must be regular and private (chmod 600)")
	}
	return nil
}

func createPrivateTokenTemp(dir string) (*os.File, error) {
	return os.CreateTemp(dir, ".mcp-token-*") // Created with mode 0600.
}
