//go:build darwin && cgo

package desktop

/*
#cgo CFLAGS: -x objective-c -fobjc-arc -fblocks
#cgo LDFLAGS: -framework AppKit -framework UniformTypeIdentifiers
#include <stdlib.h>
#include "native_darwin.h"
*/
import "C"

import (
	"errors"
	"log/slog"
	"net/url"
	"path/filepath"
	"strings"
	"unsafe"
)

type MenuBar struct{}

// Only the AppKit main thread accesses these callbacks. Service callbacks only
// enqueue work; login-item callbacks perform small local configuration I/O.
var menuCallbacks struct {
	ready               func(*MenuBar)
	open, restart, quit func()
	autostart           *Autostart
	backup              func(BackupRequest)
}

// RunMenuBar must run on the process's original OS thread (locked in main init).
// It returns after Close, so the caller can release the single-instance lock.
func RunMenuBar(ready func(*MenuBar), open, restart, quit func(), autostart *Autostart, backup func(BackupRequest)) {
	menuCallbacks.ready, menuCallbacks.open, menuCallbacks.restart, menuCallbacks.quit = ready, open, restart, quit
	menuCallbacks.autostart, menuCallbacks.backup = autostart, backup
	C.worklog_run_menu()
	menuCallbacks.ready, menuCallbacks.open, menuCallbacks.restart, menuCallbacks.quit = nil, nil, nil, nil
	menuCallbacks.autostart, menuCallbacks.backup = nil, nil
}

//export worklogMenuCommand
func worklogMenuCommand(command C.int) {
	switch command {
	case 0:
		if menuCallbacks.ready != nil {
			menuCallbacks.ready(&MenuBar{})
		}
	case 1:
		if menuCallbacks.open != nil {
			menuCallbacks.open()
		}
	case 2:
		if menuCallbacks.restart != nil {
			menuCallbacks.restart()
		}
	case 3:
		if menuCallbacks.quit != nil {
			menuCallbacks.quit()
		}
	case 4:
		if a := menuCallbacks.autostart; a != nil {
			enabled, err := a.Enabled()
			if err == nil {
				err = a.SetEnabled(!enabled)
			}
			if err != nil {
				slog.Error("desktop_autostart_failed", "error", err)
				(&MenuBar{}).Notify(true, "无法更改登录后启动：\n"+err.Error())
			}
		}
		refreshLoginState()
	case 5:
		refreshLoginState()
	}
}

func refreshLoginState() {
	state := C.int(-1)
	if a := menuCallbacks.autostart; a != nil {
		enabled, err := a.Enabled()
		if err != nil {
			slog.Error("desktop_autostart_status_failed", "error", err)
		} else {
			state = 0
			if enabled {
				state = 1
			}
		}
	}
	C.worklog_set_login_state(state)
}

//export worklogBackupSelected
func worklogBackupSelected(importDB C.int, selected *C.char) {
	path := C.GoString(selected)
	if !filepath.IsAbs(path) {
		return
	}
	if importDB != 0 && !strings.EqualFold(filepath.Ext(path), ".db") {
		(&MenuBar{}).Notify(true, "请选择 .db 数据库备份文件")
		return
	}
	if menuCallbacks.backup != nil {
		menuCallbacks.backup(BackupRequest{Import: importDB != 0, Path: path})
	}
}

func (*MenuBar) SetStatus(status string) {
	text := C.CString(strings.ReplaceAll(status, "\x00", "\uFFFD"))
	defer C.free(unsafe.Pointer(text))
	C.worklog_set_status(text)
}

func (*MenuBar) Close() { C.worklog_stop_menu() }

// Notify queues an operational alert on AppKit's main thread. It never waits
// for a user response, so service cleanup is independent of open dialogs.
func (*MenuBar) Notify(failed bool, message string) {
	text := C.CString(strings.ReplaceAll(message, "\x00", "\uFFFD"))
	defer C.free(unsafe.Pointer(text))
	flag := C.int(0)
	if failed {
		flag = 1
	}
	C.worklog_notify(text, flag)
}

func OpenBrowser(address string) error {
	parsed, err := url.Parse(address)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || strings.ContainsAny(address, "\x00\r\n") {
		return errors.New("browser URL must be an absolute HTTP(S) URL")
	}
	text := C.CString(address)
	defer C.free(unsafe.Pointer(text))
	if C.worklog_open_browser(text) == 0 {
		return errors.New("default browser could not open URL")
	}
	return nil
}

// ShowError is used before the menu loop, on the original OS thread.
func ShowError(message string) {
	text := C.CString(strings.ReplaceAll(message, "\x00", "\uFFFD"))
	defer C.free(unsafe.Pointer(text))
	C.worklog_show_error(text)
}

func hasExtendedACL(fd int) (bool, error) {
	result, err := C.worklog_has_acl(C.int(fd))
	if result < 0 {
		return false, err
	}
	return result != 0, nil
}
