//go:build darwin && cgo

package desktop

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// SecureDataDir validates existing data without changing its permissions.
// Reject symlinks, other owners, shared mode bits, special files and extended
// ACLs (which can grant access despite chmod 600/700). External configured paths
// remain the user's responsibility, as in the Windows host.
func SecureDataDir(root string) error {
	if root == "" {
		return fmt.Errorf("data directory is empty")
	}
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		return validatePrivateEntry(path, path == root)
	})
}

// SecureDataRoot is safe before instance election: do not walk a running
// primary's database/WAL files or transient notification markers.
func SecureDataRoot(root string) error {
	if root == "" {
		return fmt.Errorf("data directory is empty")
	}
	return validatePrivateEntry(root, true)
}

func validatePrivateEntry(path string, directory bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if directory && !info.IsDir() || !info.IsDir() && !info.Mode().IsRegular() {
		return fmt.Errorf("data directory must contain only regular files and directories: %s", path)
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	return checkPrivateDescriptor(fd, path, directory, false)
}

// CheckPrivateFile checks a newly created private file before writing secrets,
// including inherited macOS ACLs which are not removed by a 0600 mode.
func CheckPrivateFile(file *os.File) error {
	return checkPrivateDescriptor(int(file.Fd()), file.Name(), false, true)
}

func checkPrivateDescriptor(fd int, path string, directory, regular bool) error {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return err
	}
	if regular && stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return fmt.Errorf("private file must be regular: %s", path)
	}
	if directory && stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Mode&unix.S_IFMT != unix.S_IFDIR && stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return fmt.Errorf("data directory contains a special file: %s", path)
	}
	if stat.Uid != uint32(os.Geteuid()) || stat.Mode&0077 != 0 {
		return fmt.Errorf("data directory tree must be owned by the current user and private (directories 700, files 600): %s", path)
	}
	acl, err := hasExtendedACL(fd)
	if err != nil {
		return fmt.Errorf("inspect data directory ACL: %w", err)
	}
	if acl {
		return fmt.Errorf("data directory tree must not have extended ACLs: %s", path)
	}
	return nil
}
