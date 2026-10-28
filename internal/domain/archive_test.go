package domain

import (
	"reflect"
	"testing"
)

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

func TestPreserveArchivedProjectOrder(t *testing.T) {
	archived := func(id string) Project { return Project{ID: id, Name: id, Archived: true} }
	active := func(id string) Project { return Project{ID: id, Name: id} }
	old := Empty().Dashboard
	old.Projects = []Project{archived("a"), active("b"), archived("c"), active("d"), archived("e")}
	for _, tt := range []struct {
		name string
		next []Project
		want []Project
	}{
		{"unchanged", []Project{active("b"), active("d")}, old.Projects},
		{"reorder and add", []Project{active("d"), active("b"), active("f")}, []Project{archived("a"), active("d"), archived("c"), active("b"), archived("e"), active("f")}},
		{"delete visible", []Project{active("d")}, []Project{archived("a"), active("d"), archived("c"), archived("e")}},
		{"delete all visible", []Project{}, []Project{archived("a"), archived("c"), archived("e")}},
		{"cannot edit archived", []Project{active("e"), active("b"), active("a"), active("d")}, old.Projects},
	} {
		t.Run(tt.name, func(t *testing.T) {
			next := Empty().Dashboard
			next.Projects = tt.next
			PreserveArchived(old, &next)
			if !reflect.DeepEqual(next.Projects, tt.want) {
				t.Fatalf("got %v, want %v", next.Projects, tt.want)
			}
		})
	}
}
