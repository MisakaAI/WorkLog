package domain

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestStrictJSONBoundary(t *testing.T) {
	for _, b := range []string{
		`{"id":"1","title":"x","done":false,"Done":true}`,
		`{"id":"1","title":"x","done":false,"done":true}`,
		`{"id":"1","title":"x","done":false} {}`,
		`{"id":9007199254740992,"title":"x","done":false}`,
		`{"id":1.5,"title":"x","done":false}`,
		`{"id":"1","title":"x","done":false,"owner":"evil"}`,
	} {
		var v Todo
		if json.Unmarshal([]byte(b), &v) == nil {
			t.Error("accepted", b)
		}
	}
	raw := []byte(`{"id":"1","title":"`)
	raw = append(raw, 255)
	raw = append(raw, []byte(`","done":false}`)...)
	var v Todo
	if json.Unmarshal(raw, &v) == nil {
		t.Fatal("invalid UTF8")
	}
}
func TestSnapshotFailures(t *testing.T) {
	valid := func() *State {
		s := Empty()
		s.Projects = []Project{{ID: "p", Name: "name"}}
		s.Tasks = []Task{{ID: StringID("t"), Title: "x", Status: "planned", ProjectID: "p", Tag: "开发", Priority: "中", Color: "white"}}
		s.Todos = []Todo{{ID: StringID("todo"), Title: "todo"}}
		return s
	}
	tests := map[string]func(*State){"empty categories": func(s *State) { s.Categories = []string{} }, "duplicate categories": func(s *State) { s.Categories = []string{"x", " X "} }, "duplicate projects": func(s *State) { s.Projects = append(s.Projects, Project{ID: "p2", Name: " NAME "}) }, "sentinel": func(s *State) { s.Projects[0].ID = "__all__" }, "invalid ref": func(s *State) { s.Tasks[0].ProjectID = "other" }, "invalid category": func(s *State) { s.Tasks[0].Tag = "other" }, "invalid status": func(s *State) { s.Tasks[0].Status = "in-progress" }, "invalid priority": func(s *State) { s.Tasks[0].Priority = "urgent" }, "invalid color": func(s *State) { s.Tasks[0].Color = "#ffffff" }, "invalid dates": func(s *State) { s.Tasks[0].StartDate = "2026-10-21"; s.Tasks[0].Date = "2026-10-20" }, "UTF16 title": func(s *State) { s.Tasks[0].Title = strings.Repeat("😀", 51) }, "raw label length": func(s *State) { s.PersonalLabel = strings.Repeat(" ", 256) }, "too large revision": func(s *State) { s.Revision = MaxVersion }, "duplicate todos": func(s *State) { s.Todos = append(s.Todos, s.Todos[0]) }, "empty title": func(s *State) { s.Todos[0].Title = " \uFEFF " }}
	for name, modify := range tests {
		t.Run(name, func(t *testing.T) {
			s := valid()
			modify(s)
			if Validate(&s.Dashboard) == nil {
				t.Fatal("accepted invalid snapshot")
			}
		})
	}
	s := valid()
	s.Projects[0].Name = "  " + strings.Repeat("a", 40) + "  "
	if e := Validate(&s.Dashboard); e != nil || len(s.Projects[0].Name) != 40 {
		t.Fatal("trim-before-length", e)
	}
	if Public(context.DeadlineExceeded).Code != "TIMEOUT" || Public(context.Canceled).Status != 503 {
		t.Fatal("inconsistent timeout mapping")
	}
}
