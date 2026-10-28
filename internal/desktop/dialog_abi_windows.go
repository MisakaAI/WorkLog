//go:build windows

package desktop

import "unsafe"

// Compile-time SDK layout checks also run when cross-compiling tests. The
// formulas cover Windows 386 (4-byte pointers), amd64 and arm64 (8-byte).
const dialogPointerSize = unsafe.Sizeof(uintptr(0))

var (
	_ [8 * dialogPointerSize]byte     = [unsafe.Sizeof(nativeBrowseInfo{})]byte{}
	_ [5 * dialogPointerSize]byte     = [unsafe.Offsetof(nativeBrowseInfo{}.Callback)]byte{}
	_ [16*dialogPointerSize + 24]byte = [unsafe.Sizeof(nativeOpenFileName{})]byte{}
	_ [5*dialogPointerSize + 8]byte   = [unsafe.Offsetof(nativeOpenFileName{}.File)]byte{}
	_ [11*dialogPointerSize + 8]byte  = [unsafe.Offsetof(nativeOpenFileName{}.Flags)]byte{}
	_ [16*dialogPointerSize + 20]byte = [unsafe.Offsetof(nativeOpenFileName{}.FlagsEx)]byte{}
)
