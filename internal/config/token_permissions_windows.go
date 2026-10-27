package config

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"

	"worklog/internal/diagnostics"
)

func tokenUserSID() (*windows.SID, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	return user.User.Sid, nil
}

func checkTokenPermissions(file *os.File, _ os.FileInfo) error {
	sd, err := windows.GetSecurityInfo(windows.Handle(file.Fd()), windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return err // Fail closed on filesystems that cannot report ACLs.
	}
	user, err := tokenUserSID()
	if err != nil {
		return err
	}
	return validateTokenSecurityDescriptor(sd, user)
}

// Only the process user, LocalSystem and built-in Administrators may have access.
// This is intentionally stricter than an effective-access calculation: a broad
// allow ACE is rejected even if another ACE currently denies the same access.
func validateTokenSecurityDescriptor(sd *windows.SECURITY_DESCRIPTOR, user *windows.SID) error {
	privateError := diagnostics.Safe("token file must be private: restrict its Windows ACL to the current user, SYSTEM and Administrators")
	if sd == nil || !sd.IsValid() || user == nil || !user.IsValid() {
		return privateError
	}
	defer runtime.KeepAlive(sd)
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return err
	}
	admins, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return err
	}
	trusted := func(sid *windows.SID) bool {
		return sid != nil && sid.IsValid() && (sid.Equals(user) || sid.Equals(system) || sid.Equals(admins))
	}
	// An untrusted owner can rewrite the DACL even without an allow ACE.
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
			return err
		}
		if ace.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0 {
			continue // Does not grant access to this file.
		}
		switch ace.Header.AceType {
		case windows.ACCESS_DENIED_ACE_TYPE:
			continue
		case windows.ACCESS_ALLOWED_ACE_TYPE:
			// A SID has an 8-byte fixed header, following the ACE header/mask.
			const sidOffset = unsafe.Offsetof(windows.ACCESS_ALLOWED_ACE{}.SidStart)
			if uintptr(ace.Header.AceSize) < sidOffset+8 {
				return privateError
			}
			sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
			if !sid.IsValid() || uintptr(sid.Len()) > uintptr(ace.Header.AceSize)-sidOffset || !trusted(sid) {
				return privateError
			}
		default:
			// Object/callback/other ACE layouts must not be interpreted as
			// ordinary allow ACEs. Fail closed rather than miss an access grant.
			return privateError
		}
	}
	return nil
}

func createPrivateTokenTemp(dir string) (*os.File, error) {
	user, err := tokenUserSID()
	if err != nil {
		return nil, err
	}
	// Supply a protected DACL at creation, not after writing or publishing:
	// even a permissive parent directory must never expose token contents.
	sid := user.String()
	sd, err := windows.SecurityDescriptorFromString("O:" + sid + "D:P(A;;FA;;;" + sid + ")(A;;FA;;;SY)(A;;FA;;;BA)")
	if err != nil {
		return nil, err
	}
	sa := windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		SecurityDescriptor: sd,
	}
	for attempt := 0; attempt < 100; attempt++ {
		var random [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			return nil, err
		}
		name := filepath.Join(dir, ".mcp-token-"+hex.EncodeToString(random[:]))
		// Absolute paths also avoid Win32's special treatment of relative paths.
		absolute, err := filepath.Abs(name)
		if err != nil {
			return nil, err
		}
		path, err := windows.UTF16PtrFromString(absolute)
		if err != nil {
			return nil, err
		}
		handle, err := windows.CreateFile(path, windows.GENERIC_READ|windows.GENERIC_WRITE,
			windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
			&sa, windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL, 0)
		if err != nil {
			if os.IsExist(err) {
				continue
			}
			return nil, &os.PathError{Op: "createtemp", Path: name, Err: err}
		}
		file := os.NewFile(uintptr(handle), name)
		// Some remote/non-NTFS filesystems ignore supplied security descriptors.
		// Verify before writing any credentials; never fall back to chmod.
		if err := checkTokenPermissions(file, nil); err != nil {
			file.Close()
			os.Remove(name)
			return nil, err
		}
		return file, nil
	}
	return nil, &os.PathError{Op: "createtemp", Path: dir, Err: os.ErrExist}
}
