package integration

import (
	"os"
	"reflect"
	"testing"
	"worklog/internal/domain"
	"worklog/internal/service"
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

func TestArchivedCategoryMappingAndStableProjectRevision(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" && os.Getenv("WORKLOG_TEST_POSTGRES_DSN") == "" {
				t.Skip("WORKLOG_TEST_POSTGRES_DSN is not set")
			}
			f := setup(t, driver)
			d := snapshotFixture()
			d.Projects = []domain.Project{{ID: "a", Name: "Archived", Archived: true}, d.Projects[0]}
			d.Tasks[1].ProjectID = "a"
			saved := typed[domain.Dashboard](t, httpOK(t, f, "PUT", "/api/dashboard", d))
			before := httpOK(t, f, "GET", "/api/projects", nil)["projectRevision"]
			saved.PersonalLabel = "new label"
			httpOK(t, f, "PUT", "/api/dashboard", saved)
			if after := httpOK(t, f, "GET", "/api/projects", nil)["projectRevision"]; after != before {
				t.Fatalf("ordinary save changed project revision: %v -> %v", before, after)
			}
			for i, change := range []struct {
				drafts []service.CategoryDraft
				tags   []string
			}{
				{[]service.CategoryDraft{{OriginalName: "A", Name: "A"}, {OriginalName: "B", Name: "Visual"}}, []string{"A", "Visual"}},
				{[]service.CategoryDraft{{OriginalName: "A", Name: "Visual"}, {OriginalName: "Visual", Name: "A"}}, []string{"Visual", "A"}},
				{[]service.CategoryDraft{{OriginalName: "Visual", Name: "Only"}}, []string{"Only", "Only"}},
			} {
				httpOK(t, f, "PUT", "/api/categories", map[string]any{"categoryRevision": i + 1, "categories": change.drafts})
				full := typed[domain.Dashboard](t, httpOK(t, f, "GET", "/api/dashboard?includeArchived=true", nil))
				if tags := []string{full.Tasks[0].Tag, full.Tasks[1].Tag}; !reflect.DeepEqual(tags, change.tags) {
					t.Fatalf("category mapping %d: got %v, want %v", i, tags, change.tags)
				}
				if !reflect.DeepEqual(full.Projects, d.Projects) {
					t.Fatal("ordinary save changed archived project order", full.Projects)
				}
			}
			full := typed[domain.Dashboard](t, httpOK(t, f, "GET", "/api/dashboard?includeArchived=true", nil))
			full.Projects[0].Archived = false
			restored := typed[domain.Dashboard](t, httpOK(t, f, "PUT", "/api/dashboard", full))
			if restored.Projects[0].ID != "a" || len(restored.Tasks) != 2 || restored.Tasks[1].Tag != "Only" {
				t.Fatal("restore lost order or category mapping", restored)
			}
		})
	}
}
