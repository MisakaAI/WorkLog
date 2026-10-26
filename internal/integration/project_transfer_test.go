package integration

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"testing"

	"worklog/internal/domain"
	"worklog/internal/service"
)

func transferZIP(t *testing.T, name string, data []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	z := zip.NewWriter(&out)
	f, err := z.Create(name)
	must(t, err)
	_, err = f.Write(data)
	must(t, err)
	must(t, z.Close())
	return out.Bytes()
}

func TestProjectTransferRoundTrip(t *testing.T) {
	f := setup(t, "sqlite")
	d := snapshotFixture()
	d.IncludeArchived = true
	d.Projects = append(d.Projects, domain.Project{ID: "archived", Name: "历史", Archived: true})
	archivedTask := d.Tasks[0]
	archivedTask.ID = domain.StringID("archived-task")
	archivedTask.ProjectID = "archived"
	d.Tasks = append(d.Tasks, archivedTask)
	_, err := f.svc.SaveDashboard(ctx, owner, d)
	must(t, err)
	for _, projectID := range []string{"", "p", "archived"} {
		data, e := f.svc.ExportProjects(ctx, owner, projectID)
		must(t, e)
		zr, e := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		must(t, e)
		if len(zr.File) != 1 || zr.File[0].Name != "projects.json" {
			t.Fatal("unexpected ZIP")
		}
		rc, e := zr.File[0].Open()
		must(t, e)
		var bundle service.ProjectBundle
		must(t, json.NewDecoder(rc).Decode(&bundle))
		rc.Close()
		if bundle.Format != "worklog-projects" || bundle.Version != 1 {
			t.Fatal(bundle)
		}
		if projectID != "" && (len(bundle.Projects) != 1 || bundle.Projects[0].ID != projectID) {
			t.Fatal(bundle)
		}
		other := domain.Identity{User: "target-" + projectID, Write: true}
		imported, e := f.svc.ImportProjects(ctx, other, data)
		must(t, e)
		if len(imported.Projects) != len(bundle.Projects) || len(imported.Tasks) != len(bundle.Tasks) || len(imported.Todos) != 0 {
			t.Fatal(imported)
		}
		ids := map[string]bool{}
		for i, p := range imported.Projects {
			if p.ID == bundle.Projects[i].ID || p.Archived != bundle.Projects[i].Archived {
				t.Fatal(p)
			}
			ids[p.ID] = true
		}
		for _, task := range imported.Tasks {
			if !ids[task.ProjectID] {
				t.Fatal(task)
			}
		}
		twice, e := f.svc.ImportProjects(ctx, other, data)
		must(t, e)
		if len(twice.Projects) != 2*len(imported.Projects) || twice.Revision != imported.Revision+1 {
			t.Fatal(twice)
		}
	}
	_, err = f.svc.ExportProjects(ctx, owner, "missing")
	status(t, err, 404)
	data, err := f.svc.ExportProjects(ctx, owner, "")
	must(t, err)
	_, err = f.svc.ImportProjects(ctx, domain.Identity{User: "owner"}, data)
	status(t, err, 403)
	// 读取其他用户的数据不能导出当前用户的项目。
	_, err = f.svc.ExportProjects(ctx, domain.Identity{User: "other"}, "p")
	status(t, err, 404)
	before, err := f.svc.Dashboard(ctx, owner, true)
	must(t, err)
	for _, invalid := range [][]byte{[]byte("not zip"), transferZIP(t, "../projects.json", []byte("{}")), transferZIP(t, "projects.json", []byte(`{"format":"other","version":1}`)), transferZIP(t, "projects.json", []byte(`{"format":"worklog-projects","version":1,"projects":[],"categories":[],"tasks":[]}`))} {
		_, err = f.svc.ImportProjects(ctx, owner, invalid)
		status(t, err, 400)
	}
	after, err := f.svc.Dashboard(ctx, owner, true)
	must(t, err)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("failed import modified data")
	}
}

func TestProjectTransferREST(t *testing.T) {
	f := setup(t, "sqlite")
	httpOK(t, f, "PUT", "/api/dashboard", snapshotFixture())
	res, err := http.Get(f.server.URL + "/api/projects/export")
	must(t, err)
	data, err := io.ReadAll(res.Body)
	res.Body.Close()
	must(t, err)
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "application/zip" {
		t.Fatal(res.StatusCode, string(data))
	}
	r, err := http.NewRequest("POST", f.server.URL+"/api/projects/import", bytes.NewReader(data))
	must(t, err)
	r.Header.Set("Content-Type", "application/zip")
	res, err = http.DefaultClient.Do(r)
	must(t, err)
	defer res.Body.Close()
	var saved domain.Dashboard
	must(t, json.NewDecoder(res.Body).Decode(&saved))
	if res.StatusCode != 200 || len(saved.Projects) != 2 || len(saved.Tasks) != 4 || len(saved.Todos) != 1 {
		t.Fatal(saved)
	}
	code, _, _ := req(t, f, "GET", "/api/projects/export?unknown=1", nil, nil)
	if code != 400 {
		t.Fatal(code)
	}
	code, _, _ = req(t, f, "POST", "/api/projects/import", map[string]any{}, nil)
	if code != 400 {
		t.Fatal(code)
	}
}
