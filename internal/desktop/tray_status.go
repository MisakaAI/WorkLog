package desktop

import "strings"

// COLORREF values (0x00bbggrr), not Unicode/emoji indicators.
const (
	statusGreen uint32 = 0x00408020
	statusAmber uint32 = 0x000099E6
	statusRed   uint32 = 0x003636D9
	statusGray  uint32 = 0x00808080
)

// Preserve arbitrary caller text, removing only the legacy leading marker.
// Unknown states are neutral rather than falsely indicating a healthy service.
func trayStatusPresentation(status string) (string, uint32) {
	text := strings.TrimSpace(strings.ReplaceAll(status, "\x00", "\uFFFD"))
	text = strings.TrimSpace(strings.TrimPrefix(text, "●"))
	color := statusGray
	switch text {
	case "服务运行中":
		color = statusGreen
	case "服务启动中", "服务重启中", "正在导出备份", "正在导入备份":
		color = statusAmber
	case "服务异常":
		color = statusRed
	case "服务已停止":
		color = statusGray
	}
	return text, color
}
