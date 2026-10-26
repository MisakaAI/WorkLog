package integration

import (
	"reflect"
	"testing"

	"worklog/internal/domain"
	"worklog/internal/service"
)

func testProjectDeletion(t *testing.T, driver string) {
	for _, mode := range []string{"delete_project", "replace_projects", "replace_all_projects", "snapshot"} {
		t.Run(mode, func(t *testing.T) {
			f := setup(t, driver)
			d := snapshotFixture()
			d.Projects = append(d.Projects, domain.Project{ID: "q", Name: "Keep"}, domain.Project{ID: "a", Name: "Archived", Archived: true})
			d.Tasks = append(d.Tasks,
				domain.Task{ID: domain.StringID("done"), Title: "done", ProjectID: "p", Status: "done", Tag: "A", Priority: "中", Color: "white"},
				domain.Task{ID: domain.StringID("keep"), Title: "keep", ProjectID: "q", Status: "planned", Tag: "A", Priority: "中", Color: "white"},
				domain.Task{ID: domain.StringID("unassigned"), Title: "unassigned", Status: "planned", Tag: "A", Priority: "中", Color: "white"},
				domain.Task{ID: domain.StringID("archived"), Title: "archived", ProjectID: "a", Status: "planned", Tag: "A", Priority: "中", Color: "white"},
			)
			_, err := f.svc.SaveDashboard(ctx, owner, d)
			must(t, err)
			keepArchived := mode == "delete_project" || mode == "replace_projects"
			wantProjects, wantTasks := d.Projects[1:2], d.Tasks[3:5]
			if keepArchived {
				wantProjects, wantTasks = d.Projects[1:], d.Tasks[3:]
			}
			switch mode {
			case "delete_project":
				// Stale revisions cannot partially delete the project or its tasks.
				_, err = f.svc.Execute(ctx, owner, mode, raw(service.DeleteProject{ID: "p", ExpectedRevision: 0}))
				status(t, err, 409)
				unchanged, err := f.svc.Dashboard(ctx, owner, true)
				must(t, err)
				if len(unchanged.Tasks) != len(d.Tasks) || len(unchanged.Projects) != len(d.Projects) {
					t.Fatal("conflict partially deleted data", unchanged)
				}
				result := typed[service.CollectionResult](t, invoke(t, f, owner, mode, service.DeleteProject{ID: "p", ExpectedRevision: 1}))
				if len(result.UpdatedTasks) != 0 {
					t.Fatal("deleted tasks must not be reported as updated", result)
				}
			case "replace_projects", "replace_all_projects":
				httpOK(t, f, "PUT", "/api/projects", map[string]any{"projectRevision": 1, "includeArchived": !keepArchived, "projects": wantProjects[:1]})
			case "snapshot":
				d.Revision, d.IncludeArchived = 1, true
				d.Projects, d.Tasks = wantProjects, wantTasks
				httpOK(t, f, "PUT", "/api/dashboard", d)
			}
			got, err := f.svc.Dashboard(ctx, owner, true)
			must(t, err)
			if !reflect.DeepEqual(got.Projects, wantProjects) || !reflect.DeepEqual(got.Tasks, wantTasks) || !reflect.DeepEqual(got.Todos, d.Todos) {
				t.Fatal("project deletion affected unrelated data", got)
			}
			for _, id := range []string{"t1", "t2", "done"} {
				_, err = f.svc.Execute(ctx, owner, "get_task", raw(service.IDInput{ID: id}))
				status(t, err, 404)
			}
		})
	}
}
