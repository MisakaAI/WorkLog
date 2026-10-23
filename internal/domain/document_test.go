package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDocuments(t *testing.T) {
	tests := []struct {
		name, raw  string
		valid, has bool
	}{
		{"empty", `{"type":"doc","content":[{"type":"paragraph"}]}`, true, false},
		{"spaces", `{"type":"doc","content":[{"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"  \n\t"}]}]}`, true, false},
		{"rule", `{"type":"doc","content":[{"type":"horizontalRule"}]}`, true, true},
		{"nested", `{"type":"doc","content":[{"type":"blockquote","content":[{"type":"orderedList","attrs":{"start":4,"type":"A"},"content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"进展","marks":[{"type":"bold"},{"type":"italic"},{"type":"underline"},{"type":"strike"},{"type":"textStyle","attrs":{"color":"rgb(198, 40, 40)"}}]},{"type":"hardBreak"}]},{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph"}]}]}]}]}]},{"type":"codeBlock","attrs":{"language":"go"},"content":[{"type":"text","text":"fmt.Println(1)"}]},{"type":"heading","attrs":{"level":3},"content":[{"type":"text","text":"代码","marks":[{"type":"code"}]}]}]}`, true, true},
		{"default attributes", `{"type":"doc","content":[{"type":"orderedList","attrs":{"start":1,"type":null},"content":[{"type":"listItem","content":[{"type":"paragraph"}]}]},{"type":"codeBlock","attrs":{"language":null}},{"type":"paragraph","content":[{"type":"text","text":"x","marks":[{"type":"textStyle","attrs":{"color":null}}]}]}]}`, true, true},
		{"unknown node", `{"type":"doc","content":[{"type":"image","attrs":{"src":"https://bad"}}]}`, false, false},
		{"bad parent", `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"blockquote"}]}]}`, false, false},
		{"bad item", `{"type":"doc","content":[{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"heading","attrs":{"level":2}}]}]}]}`, false, false},
		{"bad heading", `{"type":"doc","content":[{"type":"heading","attrs":{"level":1}}]}`, false, false},
		{"script attr", `{"type":"doc","content":[{"type":"paragraph","attrs":{"onclick":"evil()"}}]}`, false, false},
		{"url color", `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"x","marks":[{"type":"textStyle","attrs":{"color":"url(https://bad)"}}]}]}]}`, false, false},
		{"css expression", `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"x","marks":[{"type":"textStyle","attrs":{"color":"red; background:url(x)"}}]}]}]}`, false, false},
		{"link", `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"x","marks":[{"type":"link","attrs":{"href":"x"}}]}]}]}`, false, false},
		{"bad rgb", `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"x","marks":[{"type":"textStyle","attrs":{"color":"rgb(999,0,0)"}}]}]}]}`, false, false},
		{"empty root", `{"type":"doc"}`, false, false},
		{"nontext text", `{"type":"doc","content":[{"type":"paragraph","text":"x"}]}`, false, false},
		{"empty text", `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":""}]}]}`, false, false},
		{"missing level", `{"type":"doc","content":[{"type":"heading"}]}`, false, false},
		{"unknown key", `{"type":"doc","html":"<script>"}`, false, false},
		{"code marks", `{"type":"doc","content":[{"type":"codeBlock","content":[{"type":"text","text":"x","marks":[{"type":"bold"}]}]}]}`, false, false},
		{"zero start", `{"type":"doc","content":[{"type":"orderedList","attrs":{"start":0},"content":[{"type":"listItem","content":[{"type":"paragraph"}]}]}]}`, true, false},
		{"negative start", `{"type":"doc","content":[{"type":"orderedList","attrs":{"start":-3},"content":[{"type":"listItem","content":[{"type":"paragraph"}]}]}]}`, true, false},
		{"null start", `{"type":"doc","content":[{"type":"orderedList","attrs":{"start":null},"content":[{"type":"listItem","content":[{"type":"paragraph"}]}]}]}`, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			has, e := ValidateDocument(json.RawMessage(tt.raw))
			if (e == nil) != tt.valid || e == nil && has != tt.has {
				t.Fatalf("has=%v error=%v", has, e)
			}
		})
	}
	nested := `{"type":"paragraph"}`
	for range 70 {
		nested = `{"type":"blockquote","content":[` + nested + `]}`
	}
	if _, e := ValidateDocument([]byte(`{"type":"doc","content":[` + nested + `]}`)); e == nil {
		t.Fatal("deep document accepted")
	}
	if _, e := ValidateDocument([]byte(strings.Repeat(" ", (1<<20)+1))); Public(e).Status != 413 {
		t.Fatal("size limit")
	}
}
func TestColors(t *testing.T) {
	for _, c := range []string{"#abc", "#123456", "#123456ff", "rgb(0, 128, 255)", "rgba(1,2,3,0.5)", "red", "rebeccapurple", "hsl(120,50%,30%)", "rgb(10% 20% 30%)", "rgba(1,2,3)", "rgb(1,2,3,1)", "hsl(1.5turn 50% 30% / 50%)"} {
		if !safeColor(c) {
			t.Error(c)
		}
	}
	for _, c := range []string{"#12345", "hsl(1,2,3)", "rgb(1% 20% 101%)", "rgb(1,2,3,1.1)", "expression(x)", "rgb(256,0,0)", "var(--secret)"} {
		if safeColor(c) {
			t.Error("unsafe", c)
		}
	}
}
func TestSnapshotValidation(t *testing.T) {
	for _, d := range []string{"2024-02-29", "2026-12-31"} {
		if !ValidDate(d) {
			t.Error(d)
		}
	}
	for _, d := range []string{"2025-02-29", "2026-2-01", "2026-04-31", "0000-01-01", "2026-10-21T00:00:00Z"} {
		if ValidDate(d) {
			t.Error(d)
		}
	}
	if has, e := ValidateDocument([]byte(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"\ufeff"}]}]}`)); e != nil || has {
		t.Fatal("JS trim BOM mismatch", has, e)
	}
	if has, e := ValidateDocument([]byte(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"\u0085"}]}]}`)); e != nil || !has {
		t.Fatal("JS trim NEL mismatch", has, e)
	}
	if Units("😀") != 2 {
		t.Fatal("UTF16")
	}
	s := Empty()
	s.Tasks = []Task{{ID: StringID("1"), Title: "x", Status: "planned", Tag: "开发", Priority: "中", Color: "white"}, {ID: ID("1"), Title: "x", Status: "planned", Tag: "开发", Priority: "中", Color: "white"}}
	if Validate(&s.Dashboard) == nil {
		t.Fatal("numeric/string duplicate")
	}
	for _, raw := range []string{`{"id":"t","title":"x"}`, `{"id":"t","title":"x","done":null}`, `{"id":"t","title":"x","done":"false"}`, `{"id":"t","title":"x","done":false,"userId":"other"}`} {
		var todo Todo
		if json.Unmarshal([]byte(raw), &todo) == nil {
			t.Error("invalid todo", raw)
		}
	}
}
