//go:build windows

package desktop

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

// SecureDataDir protects an existing application data directory with an
// explicit, non-inherited DACL. Only the process user and SYSTEM receive full
// access, inheritable by files and subdirectories. Call after MkdirAll and
// before opening logs, configuration, databases or secrets, or changing the
// working directory. After securing the root, it validates every existing entry:
// reparse points, untrusted owners and non-private/unsupported DACLs fail closed.
// Existing child ACLs are not repaired (normal root ACL inheritance still applies).
// Explicit paths outside this tree remain the caller's responsibility.
func SecureDataDir(path string) (err error) {
	if path == "" {
		return errors.New("data directory path is empty")
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return fmt.Errorf("query data directory user SID: %w", err)
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;" + user.User.Sid.String() + ")(A;OICI;FA;;;SY)")
	if err != nil {
		return fmt.Errorf("create data directory security descriptor: %w", err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return fmt.Errorf("get data directory DACL: %w", err)
	}
	if dacl == nil {
		return errors.New("data directory security descriptor has a null DACL")
	}
	name, err := windows.UTF16PtrFromString(filepath.Clean(path))
	if err != nil {
		return fmt.Errorf("encode data directory path: %w", err)
	}
	// Apply permissions through the same handle used to validate the final
	// component, rather than following a junction/symlink or reopening a path
	// that could have changed between validation and SetNamedSecurityInfo.
	handle, err := windows.CreateFile(name, windows.READ_CONTROL|windows.WRITE_DAC,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return fmt.Errorf("open data directory for securing: %w", err)
	}
	defer func() {
		if closeErr := windows.CloseHandle(handle); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close secured data directory: %w", closeErr))
		}
	}()
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return fmt.Errorf("inspect data directory: %w", err)
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
		return errors.New("data directory path is not a directory")
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return errors.New("data directory must not be a reparse point")
	}
	err = windows.SetSecurityInfo(handle, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, dacl, nil)
	// dacl points into sd's backing allocation, not an independent object.
	runtime.KeepAlive(sd)
	if err != nil {
		return fmt.Errorf("protect data directory DACL: %w", err)
	}
	return filepath.WalkDir(filepath.Clean(path), func(path string, _ fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("walk existing data directory: %w", walkErr)
		}
		return validateDataDirEntry(path, user.User.Sid)
	})
}

// Inspect the actual object, including non-symlink reparse points such as
// junctions, without opening its contents or following its final component.
func validateDataDirEntry(path string, user *windows.SID) (err error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return fmt.Errorf("encode data directory entry path: %w", err)
	}
	handle, err := windows.CreateFile(name, windows.READ_CONTROL|windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return fmt.Errorf("open data directory entry for validation: %w", err)
	}
	defer func() {
		if closeErr := windows.CloseHandle(handle); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close validated data directory entry: %w", closeErr))
		}
	}()
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return fmt.Errorf("inspect data directory entry: %w", err)
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return errors.New("data directory tree must not contain reparse points")
	}
	sd, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("query data directory entry security: %w", err)
	}
	return validateDataSecurityDescriptor(sd, user)
}

// Check principals, not effective access: a deny ACE never excuses a broad
// allow ACE. Inherited private ACEs are fine; protected children need not inherit.
func validateDataSecurityDescriptor(sd *windows.SECURITY_DESCRIPTOR, user *windows.SID) error {
	privateError := errors.New("data directory tree must be private: restrict Windows ownership and ACLs to the current user, SYSTEM and Administrators")
	if sd == nil || !sd.IsValid() || user == nil || !user.IsValid() {
		return privateError
	}
	defer runtime.KeepAlive(sd)
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return fmt.Errorf("query SYSTEM SID: %w", err)
	}
	admins, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return fmt.Errorf("query Administrators SID: %w", err)
	}
	trusted := func(sid *windows.SID) bool {
		return sid != nil && sid.IsValid() && (sid.Equals(user) || sid.Equals(system) || sid.Equals(admins))
	}
	// Owners can rewrite the DACL even without an allow ACE.
	owner, _, err := sd.Owner()
	if err != nil || !trusted(owner) {
		return privateError
	}
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil {
		return privateError // Missing and NULL DACLs grant unrestricted access.
	}
	for i := uint32(0); i < uint32(dacl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			return privateError
		}
		switch ace.Header.AceType {
		case windows.ACCESS_DENIED_ACE_TYPE:
			continue
		case windows.ACCESS_ALLOWED_ACE_TYPE:
			// Also check inherit-only grants: on directories these can expose
			// future files even though they do not grant access to the directory.
			const sidOffset = unsafe.Offsetof(windows.ACCESS_ALLOWED_ACE{}.SidStart)
			if uintptr(ace.Header.AceSize) < sidOffset+8 {
				return privateError
			}
			sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
			if !sid.IsValid() || uintptr(sid.Len()) > uintptr(ace.Header.AceSize)-sidOffset || !trusted(sid) {
				return privateError
			}
		default:
			// Object/callback/other ACE layouts are unsupported, not ordinary
			// allow ACEs. Fail closed rather than overlook an access grant.
			return privateError
		}
	}
	return nil
}
