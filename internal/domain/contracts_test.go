package domain

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestNameKeyContract(t *testing.T) {
	data, err := os.ReadFile("testdata/name_keys.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct{ Name, Key string }
	if err = json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		if got := strings.ToLower(c.Name); got != c.Key {
			t.Errorf("%q: %q != %q", c.Name, got, c.Key)
		}
	}
	for _, names := range [][]string{{"ΟΣ", "ος"}, {"İ", "i"}, {"Project", "project"}} {
		d := Empty().Dashboard
		d.Categories = names
		duplicate := strings.ToLower(names[0]) == strings.ToLower(names[1])
		if (Validate(&d) != nil) != duplicate {
			t.Errorf("category contract: %q", names)
		}
		d = Empty().Dashboard
		d.Projects = []Project{{ID: "a", Name: names[0]}, {ID: "b", Name: names[1]}}
		if (Validate(&d) != nil) != duplicate {
			t.Errorf("project contract: %q", names)
		}
	}
}

func TestColorContract(t *testing.T) {
	data, err := os.ReadFile("testdata/colors.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Color string
		Valid bool
	}
	if err = json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		value, _ := json.Marshal(c.Color)
		raw := json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"保留正文","marks":[{"type":"textStyle","attrs":{"color":` + string(value) + `}}]}]}]}`)
		_, err := ValidateDocument(raw)
		if (err == nil) != c.Valid {
			t.Errorf("color %q: valid=%v, err=%v", c.Color, c.Valid, err)
		}
	}
}
