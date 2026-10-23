package service

import (
	"encoding/json"

	"worklog/internal/domain"
)

// MaxDashboardBytes 为规范化后的完整快照预留 HTTP 10 MiB 上限内的余量。
// 按 Go JSON（包含 HTML 转义）计量，浏览器重新序列化不会扩大到请求上限。
const MaxDashboardBytes = 8 << 20

func validateDashboardCapacity(old, next domain.Dashboard) error {
	data, err := json.Marshal(next)
	if err != nil {
		return err
	}
	if len(data) <= MaxDashboardBytes {
		return nil
	}
	previous, err := json.Marshal(old)
	if err != nil {
		return err
	}
	// 旧库可能已经超限，允许通过细粒度接口逐步缩减，不能把恢复操作也锁死。
	if len(data) < len(previous) {
		return nil
	}
	return &domain.Error{Status: 413, Code: "DASHBOARD_CAPACITY_EXCEEDED", Message: "工作台容量超过 8 MiB，请缩短任务描述或删除不再需要的任务；旧超限数据可通过细粒度接口逐步缩减"}
}
