//go:build windows

package desktop

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// SDK MEASUREITEMSTRUCT/DRAWITEMSTRUCT: UINTs remain 32-bit; handles and
// ULONG_PTR have native pointer alignment (including padding on 64-bit).
type nativeMeasureItem struct {
	ControlType, ControlID, ItemID uint32
	ItemWidth, ItemHeight          uint32
	ItemData                       uintptr
}

type nativeRect struct{ Left, Top, Right, Bottom int32 }

type nativeDrawItem struct {
	ControlType, ControlID, ItemID uint32
	ItemAction, ItemState          uint32
	ItemWindow, DC                 uintptr
	Rect                           nativeRect
	ItemData                       uintptr
}

type nativeLogFont struct {
	Height, Width, Escapement, Orientation, Weight int32
	Italic, Underline, StrikeOut, CharSet          byte
	OutPrecision, ClipPrecision, Quality, Pitch    byte
	FaceName                                       [32]uint16
}

type nativeNonClientMetrics struct {
	Size                                   uint32
	BorderWidth, ScrollWidth, ScrollHeight int32
	CaptionWidth, CaptionHeight            int32
	CaptionFont                            nativeLogFont
	SmallCaptionWidth, SmallCaptionHeight  int32
	SmallCaptionFont                       nativeLogFont
	MenuWidth, MenuHeight                  int32
	MenuFont, StatusFont, MessageFont      nativeLogFont
	PaddedBorderWidth                      int32
}

// Compile-time checks run during cross compilation too (386/amd64/arm64).
const trayPointerSize = unsafe.Sizeof(uintptr(0))

var (
	_ [16 + 2*trayPointerSize]byte = [unsafe.Sizeof(nativeMeasureItem{})]byte{}
	_ [16 + trayPointerSize]byte   = [unsafe.Offsetof(nativeMeasureItem{}.ItemData)]byte{}
	_ [32 + 4*trayPointerSize]byte = [unsafe.Sizeof(nativeDrawItem{})]byte{}
	_ [16 + trayPointerSize]byte   = [unsafe.Offsetof(nativeDrawItem{}.ItemWindow)]byte{}
	_ [16 + 3*trayPointerSize]byte = [unsafe.Offsetof(nativeDrawItem{}.Rect)]byte{}
	_ [32 + 3*trayPointerSize]byte = [unsafe.Offsetof(nativeDrawItem{}.ItemData)]byte{}
	_ [92]byte                     = [unsafe.Sizeof(nativeLogFont{})]byte{}
	_ [504]byte                    = [unsafe.Sizeof(nativeNonClientMetrics{})]byte{}
	_ [224]byte                    = [unsafe.Offsetof(nativeNonClientMetrics{}.MenuFont)]byte{}

	gdi32                      = windows.NewLazySystemDLL("gdi32.dll")
	createFontIndirectW        = gdi32.NewProc("CreateFontIndirectW")
	selectObject               = gdi32.NewProc("SelectObject")
	deleteObject               = gdi32.NewProc("DeleteObject")
	getStockObject             = gdi32.NewProc("GetStockObject")
	createSolidBrush           = gdi32.NewProc("CreateSolidBrush")
	ellipse                    = gdi32.NewProc("Ellipse")
	setTextColor               = gdi32.NewProc("SetTextColor")
	setBkMode                  = gdi32.NewProc("SetBkMode")
	saveDC                     = gdi32.NewProc("SaveDC")
	restoreDC                  = gdi32.NewProc("RestoreDC")
	getDC                      = user32.NewProc("GetDC")
	releaseDC                  = user32.NewProc("ReleaseDC")
	getDpiForWindow            = user32.NewProc("GetDpiForWindow")
	systemParametersInfoW      = user32.NewProc("SystemParametersInfoW")
	systemParametersInfoForDpi = user32.NewProc("SystemParametersInfoForDpi")
	getSysColor                = user32.NewProc("GetSysColor")
	getSysColorBrush           = user32.NewProc("GetSysColorBrush")
	fillRect                   = user32.NewProc("FillRect")
	drawTextW                  = user32.NewProc("DrawTextW")
)

const (
	ownerDrawMenu   = 1            // ODT_MENU
	statusTextFlags = 0x20 | 0x800 // DT_SINGLELINE | DT_NOPREFIX
)

func (t *Tray) statusDPI() uint32 {
	if getDpiForWindow.Find() == nil {
		if dpi, _, _ := getDpiForWindow.Call(t.window); dpi != 0 {
			return uint32(dpi)
		}
	}
	return 96
}

func statusPixels(value int32, dpi uint32) int32 {
	return (value*int32(dpi) + 48) / 96
}

// Select the user's menu font, not the GDI default bitmap font. Newer Windows
// can supply metrics for the owner's DPI; older Windows uses system metrics.
// The returned cleanup restores the DC before deleting our font.
func selectStatusFont(dc uintptr, dpi uint32) (int32, func()) {
	metrics := nativeNonClientMetrics{Size: uint32(unsafe.Sizeof(nativeNonClientMetrics{}))}
	var ok uintptr
	if systemParametersInfoForDpi.Find() == nil {
		ok, _, _ = systemParametersInfoForDpi.Call(0x29, uintptr(metrics.Size), uintptr(unsafe.Pointer(&metrics)), 0, uintptr(dpi))
	}
	if ok == 0 {
		ok, _, _ = systemParametersInfoW.Call(0x29, uintptr(metrics.Size), uintptr(unsafe.Pointer(&metrics)), 0)
	}
	var font uintptr
	if ok != 0 {
		font, _, _ = createFontIndirectW.Call(uintptr(unsafe.Pointer(&metrics.MenuFont)))
	}
	owned := font != 0
	if font == 0 {
		font, _, _ = getStockObject.Call(17) // DEFAULT_GUI_FONT, borrowed
	}
	old, _, _ := selectObject.Call(dc, font)
	return metrics.MenuHeight, func() {
		if old != 0 && old != ^uintptr(0) {
			selectObject.Call(dc, old)
		}
		if owned {
			deleteObject.Call(font)
		}
	}
}

func (t *Tray) measureStatus(item *nativeMeasureItem) bool {
	if item.ControlType != ownerDrawMenu || item.ItemID != menuStatus {
		return false
	}
	dpi := t.statusDPI()
	text, _ := trayStatusPresentation(t.menuStatusText)
	label, _ := windows.UTF16PtrFromString(text)
	dc, _, _ := getDC.Call(t.window)
	if dc == 0 {
		// Conservative fallback if a DC cannot be acquired.
		item.ItemWidth, item.ItemHeight = uint32(statusPixels(240, dpi)), uint32(statusPixels(28, dpi))
		return true
	}
	defer releaseDC.Call(t.window, dc)
	menuHeight, cleanup := selectStatusFont(dc, dpi)
	defer cleanup()
	var rect nativeRect
	drawTextW.Call(dc, uintptr(unsafe.Pointer(label)), ^uintptr(0), uintptr(unsafe.Pointer(&rect)), statusTextFlags|0x400) // DT_CALCRECT
	item.ItemWidth = uint32(max(0, rect.Right-rect.Left) + statusPixels(12+10+8+12, dpi))
	item.ItemHeight = uint32(max(menuHeight, max(rect.Bottom-rect.Top, statusPixels(10, dpi))+statusPixels(8, dpi)))
	return true
}

func (t *Tray) drawStatus(item *nativeDrawItem) bool {
	if item.ControlType != ownerDrawMenu || item.ItemID != menuStatus || item.ItemWindow != t.menu {
		return false
	}
	dc := item.DC
	saved, _, _ := saveDC.Call(dc)
	if saved == 0 {
		return true // Do not modify a DC whose state we cannot restore.
	}
	defer restoreDC.Call(dc, saved)
	dpi := t.statusDPI()
	_, cleanup := selectStatusFont(dc, dpi)
	defer cleanup()
	// Disabled row: no selection/focus decoration. System menu colors respect
	// user contrast settings; the status dot itself always conveys state.
	background, _, _ := getSysColorBrush.Call(4) // COLOR_MENU, borrowed
	fillRect.Call(dc, uintptr(unsafe.Pointer(&item.Rect)), background)
	foreground, _, _ := getSysColor.Call(17) // COLOR_GRAYTEXT
	setTextColor.Call(dc, foreground)
	setBkMode.Call(dc, 1) // TRANSPARENT
	text, color := trayStatusPresentation(t.menuStatusText)
	diameter := statusPixels(10, dpi)
	x := item.Rect.Left + statusPixels(12, dpi)
	y := item.Rect.Top + (item.Rect.Bottom-item.Rect.Top-diameter)/2
	brush, _, _ := createSolidBrush.Call(uintptr(color))
	if brush != 0 {
		oldBrush, _, _ := selectObject.Call(dc, brush)
		pen, _, _ := getStockObject.Call(8) // NULL_PEN, borrowed
		oldPen, _, _ := selectObject.Call(dc, pen)
		ellipse.Call(dc, uintptr(x), uintptr(y), uintptr(x+diameter), uintptr(y+diameter))
		selectObject.Call(dc, oldPen)
		selectObject.Call(dc, oldBrush)
		deleteObject.Call(brush)
	}
	rect := item.Rect
	rect.Left = x + diameter + statusPixels(8, dpi)
	rect.Right -= statusPixels(12, dpi)
	label, _ := windows.UTF16PtrFromString(text)
	drawTextW.Call(dc, uintptr(unsafe.Pointer(label)), ^uintptr(0), uintptr(unsafe.Pointer(&rect)), statusTextFlags|0x4) // DT_VCENTER
	return true
}
