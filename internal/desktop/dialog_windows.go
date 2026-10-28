//go:build windows

package desktop

import (
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// BROWSEINFOW and OPENFILENAMEW use natural pointer alignment on Windows.
type nativeBrowseInfo struct {
	Owner       uintptr
	Root        uintptr
	DisplayName *uint16
	Title       *uint16
	Flags       uint32
	Callback    uintptr
	Param       uintptr
	Image       int32
}

type nativeOpenFileName struct {
	Size             uint32
	Owner            uintptr
	Instance         uintptr
	Filter           *uint16
	CustomFilter     *uint16
	MaxCustomFilter  uint32
	FilterIndex      uint32
	File             *uint16
	MaxFile          uint32
	FileTitle        *uint16
	MaxFileTitle     uint32
	InitialDir       *uint16
	Title            *uint16
	Flags            uint32
	FileOffset       uint16
	FileExtension    uint16
	DefaultExtension *uint16
	CustomData       uintptr
	Hook             uintptr
	TemplateName     *uint16
	Reserved         uintptr
	ReservedWord     uint32
	FlagsEx          uint32
}

var (
	dialogShell32        = windows.NewLazySystemDLL("shell32.dll")
	browseForFolderW     = dialogShell32.NewProc("SHBrowseForFolderW")
	getPathFromIDListEx  = dialogShell32.NewProc("SHGetPathFromIDListEx")
	coTaskMemFree        = windows.NewLazySystemDLL("ole32.dll").NewProc("CoTaskMemFree")
	comdlg32             = windows.NewLazySystemDLL("comdlg32.dll")
	getOpenFileNameW     = comdlg32.NewProc("GetOpenFileNameW")
	commDlgExtendedError = comdlg32.NewProc("CommDlgExtendedError")
)

// ShowInfo blocks until dismissed, like ShowError. Call it from an application
// worker, never from a tray message-loop callback.
func ShowInfo(message string) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	text, _ := windows.UTF16PtrFromString(strings.ReplaceAll(message, "\x00", "\uFFFD"))
	title, _ := windows.UTF16PtrFromString("Worklog")
	if _, err := windows.MessageBox(0, text, title, windows.MB_OK|windows.MB_ICONINFORMATION|windows.MB_TASKMODAL|windows.MB_SETFOREGROUND); err != nil {
		slog.Error("show information dialog", "error", err)
	}
}

func (t *Tray) requestBackup(importDB bool) {
	if t.onBackup == nil || !t.backupBusy.CompareAndSwap(false, true) {
		return
	}
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer t.backupBusy.Store(false)

		// These dialogs deliberately have no tray HWND owner: a modal dialog must
		// not disable the tray or depend on its window surviving application exit.
		var path string
		var err error
		if importDB {
			path, err = selectBackupFile()
		} else {
			path, err = selectBackupDirectory()
		}
		if err != nil {
			slog.Error("backup_selection_failed", "error", err)
			ShowError("无法选择备份位置：" + err.Error())
			return
		}
		if path == "" { // User cancellation is not an error or a request.
			return
		}
		if importDB {
			confirmed, err := confirmBackupImport()
			if err != nil {
				ShowError("无法确认导入备份：" + err.Error())
				return
			}
			if !confirmed {
				return
			}
		}
		t.mu.Lock()
		closed := t.closing || t.hwnd == 0
		t.mu.Unlock()
		if !closed {
			t.onBackup(BackupRequest{Import: importDB, Path: path})
		}
	}()
}

// selectBackupDirectory must run on a locked OS thread. BIF_NEWDIALOGSTYLE
// requires an STA; balance both S_OK and S_FALSE before releasing that thread.
func selectBackupDirectory() (string, error) {
	err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED|windows.COINIT_DISABLE_OLE1DDE)
	if err != nil && !errors.Is(err, syscall.Errno(1)) {
		return "", fmt.Errorf("initialize folder dialog COM: %w", err)
	}
	defer windows.CoUninitialize()
	var display [260]uint16
	title, _ := windows.UTF16PtrFromString("选择导出备份的目录")
	info := nativeBrowseInfo{DisplayName: &display[0], Title: title, Flags: 0x1 | 0x40} // BIF_RETURNONLYFSDIRS | BIF_NEWDIALOGSTYLE
	pidl, _, _ := browseForFolderW.Call(uintptr(unsafe.Pointer(&info)))
	if pidl == 0 {
		return "", nil // SHBrowseForFolderW has no extended error API.
	}
	defer coTaskMemFree.Call(pidl)
	var path [32768]uint16
	ok, _, err := getPathFromIDListEx.Call(pidl, uintptr(unsafe.Pointer(&path[0])), uintptr(len(path)), 0)
	if ok == 0 {
		return "", nativeError("resolve selected directory", err)
	}
	return windows.UTF16ToString(path[:]), nil
}

func selectBackupFile() (string, error) {
	var path [32768]uint16
	// Embedded NULs separate filter labels/patterns, followed by a double NUL.
	filter := utf16.Encode([]rune("SQLite 备份 (*.db)\x00*.db\x00\x00"))
	title, _ := windows.UTF16PtrFromString("选择要导入的备份数据库")
	extension, _ := windows.UTF16PtrFromString("db")
	info := nativeOpenFileName{
		Size:   uint32(unsafe.Sizeof(nativeOpenFileName{})),
		Filter: &filter[0], FilterIndex: 1, File: &path[0], MaxFile: uint32(len(path)),
		Title: title, DefaultExtension: extension,
		// OFN_NOCHANGEDIR | OFN_PATHMUSTEXIST | OFN_FILEMUSTEXIST |
		// OFN_EXPLORER | OFN_HIDEREADONLY: never mutate the process working directory.
		Flags: 0x8 | 0x800 | 0x1000 | 0x80000 | 0x4,
	}
	ok, _, _ := getOpenFileNameW.Call(uintptr(unsafe.Pointer(&info)))
	if ok == 0 {
		code, _, _ := commDlgExtendedError.Call()
		if code != 0 {
			return "", fmt.Errorf("GetOpenFileNameW failed (0x%x)", code)
		}
		return "", nil
	}
	selected := windows.UTF16ToString(path[:])
	if !strings.EqualFold(filepath.Ext(selected), ".db") {
		return "", errors.New("请选择 .db 数据库文件")
	}
	return selected, nil
}

func confirmBackupImport() (bool, error) {
	text, _ := windows.UTF16PtrFromString("导入备份将替换当前视图，原数据库会保留。\n\n请先停止所有外部写入程序，确保没有其他程序写入数据库。\n\n确定要继续导入吗？")
	title, _ := windows.UTF16PtrFromString("Worklog — 导入备份")
	result, err := windows.MessageBox(0, text, title, windows.MB_YESNO|windows.MB_DEFBUTTON2|windows.MB_ICONWARNING|windows.MB_TASKMODAL|windows.MB_SETFOREGROUND)
	return result == 6, err // IDYES
}
