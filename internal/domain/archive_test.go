package domain

import "testing"

func TestVisibleAndPreserveArchived(t *testing.T) {
	old := Empty().Dashboard
	old.Projects = []Project{{ID: "p", Name: "active"}, {ID: "a", Name: "archive", Archived: true}}
	old.Tasks = []Task{{ID: StringID("t"), ProjectID: "p", Tag: "开发"}, {ID: StringID("hidden"), ProjectID: "a", Tag: "开发"}}
	visible := VisibleDashboard(old)
	if len(visible.Projects) != 1 || len(visible.Tasks) != 1 || visible.IncludeArchived {
		t.Fatal(visible)
	}
	if len(old.Projects) != 2 || len(old.Tasks) != 2 {
		t.Fatal("filter changed original")
	}
	next := visible
	next.Categories = []string{"新分类"}
	PreserveArchived(old, &next)
	if len(next.Projects) != 2 || !next.Projects[1].Archived || len(next.Tasks) != 2 {
		t.Fatal(next)
	}
	if next.Tasks[1].Tag != "新分类" {
		t.Fatal("hidden task has invalid category")
	}
	if old.Tasks[1].Tag != "开发" {
		t.Fatal("merge changed original")
	}
}
