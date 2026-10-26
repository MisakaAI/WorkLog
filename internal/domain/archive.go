package domain

// VisibleDashboard 隐藏归档项目及其任务，不改变持久化快照。
func VisibleDashboard(d Dashboard) Dashboard {
	hidden := map[string]bool{}
	projects := []Project{}
	for _, p := range d.Projects {
		if p.Archived {
			hidden[p.ID] = true
		} else {
			projects = append(projects, p)
		}
	}
	tasks := []Task{}
	for _, t := range d.Tasks {
		if !hidden[t.ProjectID] {
			tasks = append(tasks, t)
		}
	}
	d.Projects, d.Tasks, d.IncludeArchived = projects, tasks, false
	return d
}

// PreserveArchived 合并默认快照中未下发的数据，避免日常保存误删归档内容。
func PreserveArchived(old Dashboard, next *Dashboard) {
	hidden := map[string]bool{}
	for _, p := range old.Projects {
		if p.Archived {
			hidden[p.ID] = true
			// 默认视图不能修改归档项目；完整管理快照才可取消归档或删除。
			for i := 0; i < len(next.Projects); i++ {
				if next.Projects[i].ID == p.ID {
					next.Projects = append(next.Projects[:i], next.Projects[i+1:]...)
					i--
				}
			}
			next.Projects = append(next.Projects, p)
		}
	}
	for _, t := range old.Tasks {
		if hidden[t.ProjectID] {
			// 分类可能已在默认视图中删除。
			found := false
			for _, c := range next.Categories {
				if c == t.Tag {
					found = true
				}
			}
			if !found && len(next.Categories) > 0 {
				t.Tag = next.Categories[0]
			}
			next.Tasks = append(next.Tasks, t)
		}
	}
}
