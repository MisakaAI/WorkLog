package service

import (
	"encoding/json"
	"strings"
	"testing"

	"worklog/internal/domain"
)

func TestDashboardCapacityBoundaryAndRecovery(t *testing.T) {
	d := domain.Empty().Dashboard
	d.Tasks = []domain.Task{{ID: domain.StringID("t"), Title: "task", Description: "", Status: "planned", Tag: "开发", Priority: "中", Color: "white"}}
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	d.Tasks[0].Description = strings.Repeat("a", MaxDashboardBytes-len(b))
	if err := validateDashboardCapacity(domain.Empty().Dashboard, d); err != nil {
		t.Fatal("exact limit", err)
	}
	d.Tasks[0].Description += "a"
	if err := validateDashboardCapacity(domain.Empty().Dashboard, d); domain.Public(err).Code != "DASHBOARD_CAPACITY_EXCEEDED" {
		t.Fatal("above limit", err)
	}
	old := copyDashboard(d)
	old.Tasks[0].Description += "legacy oversize"
	if err := validateDashboardCapacity(old, d); err != nil {
		t.Fatal("must allow reduction", err)
	}
	if err := validateDashboardCapacity(d, d); err == nil {
		t.Fatal("must reject oversized no-op")
	}
	// HTML 转义和 UTF-8 都计入容量，而不是按字符数估算。
	d.Tasks[0].Description = strings.Repeat("<中😀", MaxDashboardBytes/10)
	if err := validateDashboardCapacity(domain.Empty().Dashboard, d); err == nil {
		t.Fatal("must count encoded bytes")
	}
}
