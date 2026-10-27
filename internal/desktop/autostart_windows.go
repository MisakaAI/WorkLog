//go:build windows

package desktop

import (
	"encoding/binary"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows/registry"
)

// Autostart manages the current user's Worklog desktop startup entry.
// Construct it with NewAutostart; construction does not access the registry.
type Autostart struct {
	command   string
	keyPath   string
	valueName string
}

// NewAutostart builds a startup command from an absolute executable path and an
// optional absolute configuration path. Pass the selected config path even when
// it was discovered automatically, so startup uses the same configuration.
// Paths are preserved verbatim, without expanding environment variables.
func NewAutostart(executable, conf string) (*Autostart, error) {
	if strings.ContainsRune(executable, 0) || !filepath.IsAbs(executable) {
		return nil, errors.New("autostart executable must be an absolute path without NUL")
	}
	if strings.ContainsRune(conf, 0) || (conf != "" && !filepath.IsAbs(conf)) {
		return nil, errors.New("autostart configuration must be an absolute path without NUL")
	}

	exe := syscall.EscapeArg(executable)
	if !strings.HasPrefix(exe, `"`) {
		// EscapeArg only adds surrounding quotes for whitespace. Always quote
		// the executable, also escaping any backslashes before the new quote.
		trailingSlashes := len(exe) - len(strings.TrimRight(exe, `\`))
		exe = `"` + exe + strings.Repeat(`\`, trailingSlashes) + `"`
	}
	command := exe + " --autostart"
	if conf != "" {
		command += " --conf " + syscall.EscapeArg(conf)
	}
	return &Autostart{
		command:   command,
		keyPath:   `Software\Microsoft\Windows\CurrentVersion\Run`,
		valueName: "WorklogDesktop",
	}, nil
}

// Enabled reports whether the entry exactly matches this executable and config.
// Missing keys/values and stale commands are disabled; no key is created.
func (a *Autostart) Enabled() (bool, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, a.keyPath, registry.QUERY_VALUE)
	if autostartNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("open autostart registry key: %w", err)
	}
	defer key.Close()

	expected, err := syscall.UTF16FromString(a.command)
	if err != nil {
		return false, fmt.Errorf("encode autostart command: %w", err)
	}
	// Compare the raw, terminated UTF-16 REG_SZ, rather than accepting a
	// truncated string with embedded NULs or malformed data. A larger value
	// cannot match, so limit the read to the expected command's size.
	data := make([]byte, 2*len(expected))
	n, typ, err := key.GetValue(a.valueName, data)
	if autostartNotExist(err) {
		return false, nil
	}
	if err != nil && !errors.Is(err, registry.ErrShortBuffer) {
		return false, fmt.Errorf("read autostart registry value: %w", err)
	}
	if typ != registry.SZ {
		return false, fmt.Errorf("autostart registry value must be REG_SZ (got type %d): %w", typ, registry.ErrUnexpectedType)
	}
	if err != nil || n != len(data) {
		return false, nil
	}
	for i, unit := range expected {
		if binary.LittleEndian.Uint16(data[2*i:]) != unit {
			return false, nil
		}
	}
	return true, nil
}

// SetEnabled writes a raw REG_SZ command when enabling, creating the key only
// then. Disabling removes only Worklog's value and is idempotent if absent.
func (a *Autostart) SetEnabled(enabled bool) error {
	if enabled {
		key, _, err := registry.CreateKey(registry.CURRENT_USER, a.keyPath, registry.SET_VALUE)
		if err != nil {
			return fmt.Errorf("create autostart registry key: %w", err)
		}
		defer key.Close()
		if err := key.SetStringValue(a.valueName, a.command); err != nil {
			return fmt.Errorf("write autostart registry value: %w", err)
		}
		return nil
	}

	key, err := registry.OpenKey(registry.CURRENT_USER, a.keyPath, registry.SET_VALUE)
	if autostartNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open autostart registry key for disabling: %w", err)
	}
	defer key.Close()
	if err := key.DeleteValue(a.valueName); err != nil && !autostartNotExist(err) {
		return fmt.Errorf("delete autostart registry value: %w", err)
	}
	return nil
}

func autostartNotExist(err error) bool {
	return errors.Is(err, registry.ErrNotExist) || errors.Is(err, syscall.ERROR_PATH_NOT_FOUND)
}
