package service

import (
	"os"
	"sort"
	"strings"
	"worklog/internal/domain"
)

type Holiday struct {
	Name      string `json:"name"`
	StartDate string `json:"startDate"`
	EndDate   string `json:"endDate"`
}
type AdjustedWorkday struct {
	Name string `json:"name"`
	Date string `json:"date"`
}
// Calendar 使用自然日期描述日历；星期编号为 0（周日）至 6（周六）。
// 调休上班日可覆盖假期，具体优先级由前端 getCalendarDay 统一处理。
type Calendar struct {
	WorkingWeekdays  []int             `json:"workingWeekdays"`
	Holidays         []Holiday         `json:"holidays"`
	AdjustedWorkdays []AdjustedWorkday `json:"adjustedWorkdays"`
}

func LoadCalendar(path string) (Calendar, error) {
	c := Calendar{[]int{1, 2, 3, 4, 5}, []Holiday{}, []AdjustedWorkday{}}
	if path == "" {
		return c, nil
	}
	b, e := os.ReadFile(path)
	if e != nil {
		return c, e
	}
	if len(b) > 1<<20 {
		return c, domain.Bad("日历配置过大")
	}
	if e = domain.StrictJSON(b, &c, "workingWeekdays", "holidays", "adjustedWorkdays"); e != nil {
		return c, e
	}
	return c, c.Validate()
}
func (c Calendar) Validate() error {
	seen := map[int]bool{}
	for _, d := range c.WorkingWeekdays {
		if d < 0 || d > 6 || seen[d] {
			return domain.Bad("每周工作日非法或重复")
		}
		seen[d] = true
	}
	holidays := append([]Holiday{}, c.Holidays...)
	sort.Slice(holidays, func(i, j int) bool { return holidays[i].StartDate < holidays[j].StartDate })
	for i, h := range holidays {
		if strings.TrimSpace(h.Name) == "" || !domain.ValidDate(h.StartDate) || !domain.ValidDate(h.EndDate) || h.StartDate > h.EndDate || i > 0 && h.StartDate <= holidays[i-1].EndDate {
			return domain.Bad("假期配置非法或重叠")
		}
	}
	dates := map[string]bool{}
	for _, d := range c.AdjustedWorkdays {
		if strings.TrimSpace(d.Name) == "" || !domain.ValidDate(d.Date) || dates[d.Date] {
			return domain.Bad("调休配置非法或重复")
		}
		dates[d.Date] = true
	}
	return nil
}
func (h *Holiday) UnmarshalJSON(b []byte) error {
	type plain Holiday
	return domain.StrictJSON(b, (*plain)(h), "name", "startDate", "endDate")
}
func (d *AdjustedWorkday) UnmarshalJSON(b []byte) error {
	type plain AdjustedWorkday
	return domain.StrictJSON(b, (*plain)(d), "name", "date")
}
