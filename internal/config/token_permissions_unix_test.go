//go:build !windows

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func makeTokenInsecure(path string) error {
	return os.Chmod(path, 0644)
}

func TestUnixTokenPermissions(t *testing.T) {
	for _, mode := range []os.FileMode{0400, 0600, 0640, 0604, 0666} {
		t.Run(fmt.Sprintf("%04o", mode), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "token")
			contents := strings.Repeat("x", 64)
			if err := publishToken(path, contents); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, mode); err != nil {
				t.Fatal(err)
			}
			got, err := loadOrCreateToken(path)
			wantError := mode&0077 != 0
			if (err != nil) != wantError {
				t.Fatalf("mode %04o: error=%v, wantError=%v", mode, err, wantError)
			}
			if !wantError && got != contents {
				t.Fatal("existing credentials changed")
			}
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != mode {
				t.Fatalf("existing permissions changed: info=%v err=%v", info, err)
			}
			assertTokenContents(t, path, contents)
		})
	}
}

func TestUnixTokenTempPrivate(t *testing.T) {
	file, err := createPrivateTokenTemp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() != 0 {
		t.Fatalf("new token temp is not empty and private: %v", info)
	}
}
