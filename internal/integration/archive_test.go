package integration

import (
	"testing"
	"worklog/internal/domain"
)

func TestProjectArchive(t *testing.T) {
	f := setup(t, "sqlite")
	d := snapshotFixture()
	d.Projects[0].Archived = true
	saved := typed[domain.Dashboard](t, httpOK(t, f, "PUT", "/api/dashboard", d))
	if len(saved.Projects) != 0 || len(saved.Tasks) != 0 {
		t.Fatal("archive leaked", saved)
	}
	// 默认快照保存不能删除未下发的归档数据。
	saved.PersonalLabel = "changed"
	httpOK(t, f, "PUT", "/api/dashboard", saved)
	full := typed[domain.Dashboard](t, httpOK(t, f, "GET", "/api/dashboard?includeArchived=true", nil))
	if !full.IncludeArchived || len(full.Projects) != 1 || !full.Projects[0].Archived || len(full.Tasks) != 2 {
		t.Fatal(full)
	}
	projects := httpOK(t, f, "GET", "/api/projects", nil)
	if len(projects["projects"].([]any)) != 0 {
		t.Fatal(projects)
	}
	tasks := httpOK(t, f, "GET", "/api/tasks", nil)
	if len(tasks["tasks"].([]any)) != 0 {
		t.Fatal(tasks)
	}
	full.Projects[0].Archived = false
	restored := typed[domain.Dashboard](t, httpOK(t, f, "PUT", "/api/dashboard", full))
	if len(restored.Projects) != 1 || len(restored.Tasks) != 2 {
		t.Fatal(restored)
	}
	// 已归档项目仍可删除，完整快照同时移除其所有任务。
	restored.Projects[0].Archived = true
	httpOK(t, f, "PUT", "/api/dashboard", restored)
	full = typed[domain.Dashboard](t, httpOK(t, f, "GET", "/api/dashboard?includeArchived=true", nil))
	full.Projects = []domain.Project{}
	full.Tasks = []domain.Task{}
	deleted := typed[domain.Dashboard](t, httpOK(t, f, "PUT", "/api/dashboard", full))
	if len(deleted.Projects) != 0 || len(deleted.Tasks) != 0 {
		t.Fatal(deleted)
	}
}
