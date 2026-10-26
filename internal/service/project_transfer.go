package service

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"
	"worklog/internal/domain"
	"worklog/internal/repository"
)

// ProjectBundle 是跨数据库、跨浏览器的项目交换格式，不是数据库备份。
type ProjectBundle struct {
	Format     string           `json:"format"`
	Version    int              `json:"version"`
	Projects   []domain.Project `json:"projects"`
	Categories []string         `json:"categories"`
	Tasks      []domain.Task    `json:"tasks"`
}

func (s *Service) ExportProjects(ctx context.Context, id domain.Identity, projectID string) ([]byte, error) {
	d, err := s.Dashboard(ctx, id, true)
	if err != nil {
		return nil, err
	}
	b := ProjectBundle{Format: "worklog-projects", Version: 1, Projects: []domain.Project{}, Categories: d.Categories, Tasks: []domain.Task{}}
	selected := map[string]bool{}
	for _, p := range d.Projects {
		if projectID == "" || p.ID == projectID {
			b.Projects = append(b.Projects, p)
			selected[p.ID] = true
		}
	}
	if projectID != "" && !selected[projectID] {
		return nil, domain.NotFound()
	}
	for _, t := range d.Tasks {
		if selected[t.ProjectID] {
			b.Tasks = append(b.Tasks, t)
		}
	}
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	f, err := zw.Create("projects.json")
	if err == nil {
		err = json.NewEncoder(f).Encode(b)
	}
	if err != nil {
		return nil, err
	}
	if err = zw.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// ImportProjects 在单个用户事务中追加数据，不修改已有项目或任务。
func (s *Service) ImportProjects(ctx context.Context, id domain.Identity, data []byte) (domain.Dashboard, error) {
	var out domain.Dashboard
	if err := id.Check(true, false); err != nil {
		return out, err
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil || len(zr.File) != 1 || zr.File[0].Name != "projects.json" {
		return out, domain.Bad("请选择包含 projects.json 的项目 ZIP 文件")
	}
	f := zr.File[0]
	if f.UncompressedSize64 > MaxDashboardBytes {
		return out, domain.Bad("项目数据解压后不能超过 8 MiB")
	}
	rc, err := f.Open()
	if err != nil {
		return out, domain.Bad("无法读取项目 ZIP 文件")
	}
	raw, err := io.ReadAll(io.LimitReader(rc, MaxDashboardBytes+1))
	rc.Close()
	if err != nil || len(raw) > MaxDashboardBytes {
		return out, domain.Bad("项目 ZIP 文件损坏或过大")
	}
	var b ProjectBundle
	if err = domain.StrictJSON(raw, &b); err != nil {
		return out, err
	}
	if b.Format != "worklog-projects" || b.Version != 1 {
		return out, domain.Bad("不支持的项目导出格式或版本")
	}
	d := domain.Empty().Dashboard
	d.Projects, d.Categories, d.Tasks = b.Projects, b.Categories, b.Tasks
	if err = domain.Validate(&d); err != nil {
		return out, err
	}
	for _, t := range d.Tasks {
		if t.ProjectID == "" {
			return out, domain.Bad("项目导入不能包含未分配任务")
		}
	}
	err = s.Repo.Within(ctx, id.User, true, func(u repository.Unit) error {
		old, e := u.Load()
		if e != nil {
			return e
		}
		st := copyState(old)
		names := map[string]bool{}
		for _, p := range st.Projects {
			names[strings.ToLower(p.Name)] = true
		}
		ids := map[string]string{}
		for _, p := range d.Projects {
			original := p.Name
			for n := 1; names[strings.ToLower(p.Name)]; n++ {
				suffix := fmt.Sprintf(" (导入 %d)", n)
				runes := []rune(original)
				for domain.Units(string(runes)+suffix) > 40 && len(runes) > 0 {
					runes = runes[:len(runes)-1]
				}
				p.Name = string(runes) + suffix
			}
			names[strings.ToLower(p.Name)] = true
			newID := uuid.NewString()
			ids[p.ID] = newID
			p.ID = newID
			st.Projects = append(st.Projects, p)
		}
		categories := map[string]string{}
		for _, c := range st.Categories {
			categories[strings.ToLower(c)] = c
		}
		for _, c := range d.Categories {
			if _, ok := categories[strings.ToLower(c)]; !ok {
				st.Categories = append(st.Categories, c)
				categories[strings.ToLower(c)] = c
			}
		}
		for _, t := range d.Tasks {
			t.ID = domain.StringID(uuid.NewString())
			t.ProjectID = ids[t.ProjectID]
			t.Tag = categories[strings.ToLower(t.Tag)]
			st.Tasks = append(st.Tasks, t)
		}
		if e = finalize(old, st, false, false); e != nil {
			return e
		}
		if e = u.Save(st); e != nil {
			return e
		}
		out = st.Dashboard
		out.IncludeArchived = true
		return nil
	})
	return out, err
}
