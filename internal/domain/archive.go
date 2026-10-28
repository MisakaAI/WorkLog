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
		}
	}
	visible := []Project{}
	for _, p := range next.Projects {
		if !hidden[p.ID] {
			visible = append(visible, p)
		}
	}
	// 默认视图不能修改归档项目；保留它们在旧完整顺序中的位置，
	// 按提交顺序填入可见项目，新增项目追加到末尾。
	projects := []Project{}
	i := 0
	for _, p := range old.Projects {
		if p.Archived {
			projects = append(projects, p)
		} else if i < len(visible) {
			projects = append(projects, visible[i])
			i++
		}
	}
	next.Projects = append(projects, visible[i:]...)
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
