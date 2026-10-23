package service

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCalendarConfiguration(t *testing.T) {
	good := `{"workingWeekdays":[1,2,3,4,5],"holidays":[{"name":"假期","startDate":"2026-10-01","endDate":"2026-10-03"}],"adjustedWorkdays":[{"name":"调休优先","date":"2026-10-02"}]}`
	tests := []struct {
		raw string
		ok  bool
	}{{good, true}, {`{"workingWeekdays":[1,1],"holidays":[],"adjustedWorkdays":[]}`, false}, {`{"workingWeekdays":[7],"holidays":[],"adjustedWorkdays":[]}`, false}, {`{"workingWeekdays":[],"holidays":[{"name":"a","startDate":"2026-01-01","endDate":"2026-01-03"},{"name":"b","startDate":"2026-01-03","endDate":"2026-01-04"}],"adjustedWorkdays":[]}`, false}, {`{"workingWeekdays":[],"holidays":[],"adjustedWorkdays":[{"name":"a","date":"2026-02-30"}]}`, false}, {`{"workingWeekdays":[],"holidays":[],"adjustedWorkdays":[{"name":"a","date":"2026-01-01"},{"name":"b","date":"2026-01-01"}]}`, false}, {`{"workingWeekdays":[],"holidays":[],"adjustedWorkdays":[],"secret":"x"}`, false}}
	path := filepath.Join(t.TempDir(), "calendar.json")
	for _, tt := range tests {
		if e := os.WriteFile(path, []byte(tt.raw), 0600); e != nil {
			t.Fatal(e)
		}
		_, e := LoadCalendar(path)
		if (e == nil) != tt.ok {
			t.Errorf("valid=%v error=%v document=%s", tt.ok, e, tt.raw)
		}
	}
}
