package integration

import (
	"strings"
	"testing"

	"worklog/internal/domain"
	"worklog/internal/repository"
	"worklog/internal/service"
)

func TestDashboardCapacityAllWriters(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" && postgresAbsent() {
				t.Skip("set WORKLOG_TEST_POSTGRES_DSN")
			}
			f := setup(t, driver)
			initial := domain.Empty().Dashboard
			initial.Tasks = []domain.Task{{ID: domain.StringID("large"), Title: "large", Description: strings.Repeat("a", 7<<20), Status: "planned", Tag: "开发", Priority: "中", Color: "white"}}
			saved, err := f.svc.SaveDashboard(ctx, owner, initial)
			must(t, err)
			create := service.CreateTask{Title: "overflow", Description: strings.Repeat("b", 2<<20), IdempotencyKey: "capacity-retry"}
			code, out, _ := req(t, f, "POST", "/api/tasks", create, nil)
			if code != 413 || out["error"].(map[string]any)["code"] != "DASHBOARD_CAPACITY_EXCEEDED" {
				t.Fatal(code, out)
			}
			after, err := f.svc.Dashboard(ctx, owner)
			must(t, err)
			if after.Revision != saved.Revision || len(after.Tasks) != 1 {
				t.Fatal("partial write", after.Revision, len(after.Tasks))
			}
			// 根快照入口也必须应用同一个预算。
			after.Tasks[0].Description = strings.Repeat("c", 9<<20)
			_, err = f.svc.SaveDashboard(ctx, owner, after)
			if domain.Public(err).Code != "DASHBOARD_CAPACITY_EXCEEDED" {
				t.Fatal(err)
			}
			// 拒绝操作不能留下幂等记录；原键可用于修正后的首次成功创建。
			create.Description = "small"
			created := typed[domain.TaskResource](t, invoke(t, f, owner, "create_task", create))
			if created.Version != 1 {
				t.Fatal(created.Version)
			}
			after, err = f.svc.Dashboard(ctx, owner)
			must(t, err)
			code, _, _ = req(t, f, "PUT", "/api/dashboard", after, nil)
			if code != 200 {
				t.Fatal("accepted state must remain writable by browser", code)
			}

			// 模拟升级前已有的 >10 MiB 数据，不通过新的业务校验制造非法状态。
			must(t, f.store.Within(ctx, owner.User, true, func(u repository.Unit) error {
				st, err := u.Load()
				if err != nil {
					return err
				}
				st.Tasks[0].Description = strings.Repeat("x", 11<<20)
				return u.Save(st)
			}))
			large := typed[domain.TaskResource](t, invoke(t, f, owner, "get_task", service.IDInput{ID: "large"}))
			reduced := strings.Repeat("x", 9<<20)
			large = typed[domain.TaskResource](t, invoke(t, f, owner, "update_task", service.UpdateTask{ID: "large", ExpectedVersion: large.Version, Description: &reduced}))
			if len(large.Description) != 9<<20 {
				t.Fatal("legacy reduction failed")
			}
			// 多步恢复：删掉超大任务后，正常写入重新可用。
			invoke(t, f, owner, "delete_task", service.DeleteResource{ID: "large", ExpectedVersion: large.Version})
			invoke(t, f, owner, "create_todo", service.CreateTodo{Title: "recovered", IdempotencyKey: "recovered"})
		})
	}
}
