//go:build windows && amd64

package desktop

import "unsafe"

// Array assignment requires identical constant lengths. These assertions fail
// at compile time if a structure's size or an ABI-critical field offset drifts
// from the Windows SDK's amd64 layout, even when tests cannot be run natively.
var (
	_ [8]byte   = [unsafe.Sizeof(nativePoint{})]byte{}
	_ [48]byte  = [unsafe.Sizeof(nativeMessage{})]byte{}
	_ [16]byte  = [unsafe.Offsetof(nativeMessage{}.WParam)]byte{}
	_ [24]byte  = [unsafe.Offsetof(nativeMessage{}.LParam)]byte{}
	_ [36]byte  = [unsafe.Offsetof(nativeMessage{}.Point)]byte{}
	_ [80]byte  = [unsafe.Sizeof(nativeWindowClass{})]byte{}
	_ [8]byte   = [unsafe.Offsetof(nativeWindowClass{}.WndProc)]byte{}
	_ [24]byte  = [unsafe.Offsetof(nativeWindowClass{}.Instance)]byte{}
	_ [64]byte  = [unsafe.Offsetof(nativeWindowClass{}.ClassName)]byte{}
	_ [976]byte = [unsafe.Sizeof(nativeNotifyIcon{})]byte{}
	_ [8]byte   = [unsafe.Offsetof(nativeNotifyIcon{}.Window)]byte{}
	_ [32]byte  = [unsafe.Offsetof(nativeNotifyIcon{}.Icon)]byte{}
	_ [40]byte  = [unsafe.Offsetof(nativeNotifyIcon{}.Tip)]byte{}
	_ [304]byte = [unsafe.Offsetof(nativeNotifyIcon{}.Info)]byte{}
	_ [816]byte = [unsafe.Offsetof(nativeNotifyIcon{}.TimeoutVersion)]byte{}
	_ [952]byte = [unsafe.Offsetof(nativeNotifyIcon{}.GUID)]byte{}
	_ [968]byte = [unsafe.Offsetof(nativeNotifyIcon{}.BalloonIcon)]byte{}
)
