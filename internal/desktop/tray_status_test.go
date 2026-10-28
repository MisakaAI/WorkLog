package desktop

import "testing"

func TestTrayStatusColors(t *testing.T) {
	// COLORREF is BGR, not RGB: guard against accidentally drawing blue.
	red := func(c uint32) uint32 { return c & 0xff }
	green := func(c uint32) uint32 { return (c >> 8) & 0xff }
	blue := func(c uint32) uint32 { return (c >> 16) & 0xff }
	if green(statusGreen) <= red(statusGreen) || green(statusGreen) <= blue(statusGreen) {
		t.Fatal("normal status must be green")
	}
	if red(statusAmber) <= green(statusAmber) || green(statusAmber) <= blue(statusAmber) {
		t.Fatal("transition status must be orange/amber")
	}
	if red(statusRed) <= green(statusRed) || red(statusRed) <= blue(statusRed) {
		t.Fatal("error status must be red")
	}
	if red(statusGray) != green(statusGray) || green(statusGray) != blue(statusGray) {
		t.Fatal("stopped status must be gray")
	}
}

func TestTrayStatusPresentation(t *testing.T) {
	for _, tc := range []struct {
		text  string
		color uint32
	}{
		{"服务运行中", statusGreen},
		{"服务启动中", statusAmber},
		{"服务重启中", statusAmber},
		{"正在导出备份", statusAmber},
		{"正在导入备份", statusAmber},
		{"服务异常", statusRed},
		{"服务已停止", statusGray},
		{"custom & status", statusGray},
		{"", statusGray},
	} {
		for _, prefix := range []string{"", "● ", "  ●  "} {
			text, color := trayStatusPresentation(prefix + tc.text)
			if text != tc.text || color != tc.color {
				t.Errorf("presentation(%q) = (%q, %#x), want (%q, %#x)", prefix+tc.text, text, color, tc.text, tc.color)
			}
		}
	}
	text, color := trayStatusPresentation("● custom\x00 & ● status")
	if text != "custom\uFFFD & ● status" || color != statusGray {
		t.Fatalf("custom text not preserved: %q, %#x", text, color)
	}
}
