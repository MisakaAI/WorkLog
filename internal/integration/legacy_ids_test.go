package integration

import (
	"net/url"
	"testing"

	"worklog/internal/domain"
)

func TestLegacyAndEscapedIDs(t *testing.T) {
	f := setup(t, "sqlite")
	d := snapshotFixture()
	d.Tasks[0].ID = domain.ID("1")
	id := "with/slash%/中文"
	d.Tasks[1].ID = domain.StringID(id)
	_, e := f.svc.SaveDashboard(ctx, owner, d)
	must(t, e)
	numeric := httpOK(t, f, "GET", "/api/tasks/1", nil)
	if numeric["id"] != float64(1) {
		t.Fatal("numeric ID was rewritten", numeric)
	}
	escaped := url.PathEscape(id)
	got := httpOK(t, f, "GET", "/api/tasks/"+escaped, nil)
	if got["id"] != id {
		t.Fatal(got)
	}
	got = httpOK(t, f, "PATCH", "/api/tasks/"+escaped, map[string]any{"version": 1, "title": "escaped ID patched"})
	if got["version"] != float64(2) {
		t.Fatal(got)
	}
}
