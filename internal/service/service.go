package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"maps"
	"reflect"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"worklog/internal/diagnostics"
	"worklog/internal/domain"
	"worklog/internal/repository"
)

// Service 承载 REST 与 MCP 共用的业务规则，统一处理权限、版本和数据校验。
// Now 可在测试中替换；业务日期按 Location 计算，而不是使用服务器本地时区。
type Service struct {
	Repo     repository.Repository
	Location *time.Location
	Now      func() time.Time
}

func New(repo repository.Repository, loc *time.Location) *Service {
	return &Service{Repo: repo, Location: loc, Now: time.Now}
}
func (s *Service) Today() string { return s.Now().In(s.Location).Format("2006-01-02") }
func (s *Service) Dashboard(ctx context.Context, id domain.Identity, includeArchived ...bool) (domain.Dashboard, error) {
	var out domain.Dashboard
	if err := id.Check(false, false); err != nil {
		return out, err
	}
	err := s.Repo.Within(ctx, id.User, false, func(u repository.Unit) error {
		st, e := u.Load()
		if e == nil {
			out = st.Dashboard
			if len(includeArchived) > 0 && includeArchived[0] {
				out.IncludeArchived = true
			} else {
				out = domain.VisibleDashboard(out)
			}
		}
		return e
	})
	return out, err
}
func copyDashboard(d domain.Dashboard) domain.Dashboard {
	d.Projects = slices.Clone(d.Projects)
	d.Categories = slices.Clone(d.Categories)
	d.Tasks = slices.Clone(d.Tasks)
	d.Todos = slices.Clone(d.Todos)
	return d
}

// copyState 复制本次修改会涉及的切片和版本表，保留修改前的状态用于比较。
func copyState(st *domain.State) *domain.State {
	out := *st
	out.Dashboard = copyDashboard(st.Dashboard)
	out.TaskVersions = maps.Clone(st.TaskVersions)
	out.TodoVersions = maps.Clone(st.TodoVersions)
	return &out
}
func increment(v *int64) error {
	if !domain.ValidVersion(*v) {
		return domain.Bad("版本已达到上限")
	}
	*v++
	return nil
}

// finalize 在保存前校验完整状态，并统一推进工作台、集合及资源的版本。
// 集合操作可强制消耗版本；普通快照保存只更新实际发生变化的资源版本。
func finalize(old, st *domain.State, forceProjects, forceCategories bool) error {
	if err := domain.Validate(&st.Dashboard); err != nil {
		return err
	}
	if err := increment(&st.Revision); err != nil {
		return err
	}
	if forceProjects || !reflect.DeepEqual(old.Projects, st.Projects) {
		if err := increment(&st.ProjectRevision); err != nil {
			return err
		}
	}
	if forceCategories || !reflect.DeepEqual(old.Categories, st.Categories) {
		if err := increment(&st.CategoryRevision); err != nil {
			return err
		}
	}
	oldTasks := map[string]domain.Task{}
	for _, t := range old.Tasks {
		oldTasks[t.ID.Key()] = t
	}
	for _, t := range st.Tasks {
		key := t.ID.Key()
		prev, ok := oldTasks[key]
		if !ok || !reflect.DeepEqual(prev, t) {
			v := old.TaskVersions[key]
			if err := increment(&v); err != nil {
				return err
			}
			st.TaskVersions[key] = v
		}
	}
	oldTodos := map[string]domain.Todo{}
	for _, t := range old.Todos {
		oldTodos[t.ID.Key()] = t
	}
	for _, t := range st.Todos {
		key := t.ID.Key()
		prev, ok := oldTodos[key]
		if !ok || prev != t {
			v := old.TodoVersions[key]
			if err := increment(&v); err != nil {
				return err
			}
			st.TodoVersions[key] = v
		}
	}
	return validateDashboardCapacity(old.Dashboard, st.Dashboard)
}
func (s *Service) SaveDashboard(ctx context.Context, id domain.Identity, d domain.Dashboard) (domain.Dashboard, error) {
	// 校验会规范化名称，先复制切片，避免改动调用方持有的数据。
	d = copyDashboard(d)
	var out domain.Dashboard
	if err := id.Check(true, false); err != nil {
		return out, err
	}
	if err := domain.Validate(&d); err != nil {
		return out, err
	}
	err := s.Repo.Within(ctx, id.User, true, func(u repository.Unit) error {
		old, e := u.Load()
		if e != nil {
			return e
		}
		if old.Revision != d.Revision {
			return domain.Conflict()
		}
		if !d.IncludeArchived {
			domain.PreserveArchived(old.Dashboard, &d)
		}
		if !id.Destructive && removesResources(old.Dashboard, d) {
			return domain.Forbidden()
		}
		st := copyState(old)
		st.Dashboard = d
		st.IncludeArchived = false
		if e = finalize(old, st, false, false); e != nil {
			return e
		}
		if e = u.Save(st); e == nil {
			out = st.Dashboard
			if d.IncludeArchived {
				out.IncludeArchived = true
			} else {
				out = domain.VisibleDashboard(out)
			}
		}
		return e
	})
	return out, err
}
func removesResources(old, next domain.Dashboard) bool {
	projects := map[string]bool{}
	for _, p := range next.Projects {
		projects[p.ID] = true
	}
	for _, p := range old.Projects {
		if !projects[p.ID] {
			return true
		}
	}
	categories := map[string]bool{}
	for _, c := range next.Categories {
		categories[c] = true
	}
	for _, c := range old.Categories {
		if !categories[c] {
			return true
		}
	}
	tasks := map[string]bool{}
	for _, t := range next.Tasks {
		tasks[t.ID.Key()] = true
	}
	for _, t := range old.Tasks {
		if !tasks[t.ID.Key()] {
			return true
		}
	}
	todos := map[string]bool{}
	for _, t := range next.Todos {
		todos[t.ID.Key()] = true
	}
	for _, t := range old.Todos {
		if !todos[t.ID.Key()] {
			return true
		}
	}
	return false
}
func (s *Service) Worklog(ctx context.Context, id domain.Identity, date string) (domain.Worklog, error) {
	var out domain.Worklog
	if err := id.Check(false, false); err != nil {
		return out, err
	}
	if !domain.ValidDate(date) {
		return out, domain.Bad("日期必须是合法 YYYY-MM-DD")
	}
	err := s.Repo.Within(ctx, id.User, false, func(u repository.Unit) error { var e error; out, e = u.Worklog(date); return e })
	if err == nil && out.UpdatedAt != nil {
		// 读取时统一旧记录的时间精度，无需迁移已存储的 RFC3339Nano 数据。
		t, e := time.Parse(time.RFC3339Nano, *out.UpdatedAt)
		if e != nil {
			return out, diagnostics.Wrap("decode_stored_worklog_timestamp", e)
		}
		value := formatTimestamp(t)
		out.UpdatedAt = &value
	}
	return out, err
}
func formatTimestamp(t time.Time) string {
	return t.UTC().Truncate(time.Millisecond).Format("2006-01-02T15:04:05.000Z")
}

func (s *Service) SaveWorklog(ctx context.Context, id domain.Identity, date string, content json.RawMessage, version int64) (domain.Worklog, error) {
	var out domain.Worklog
	if err := id.Check(true, false); err != nil {
		return out, err
	}
	if !domain.ValidDate(date) || !domain.ValidVersion(version) {
		return out, domain.Bad("日期或 version 非法")
	}
	has, err := domain.ValidateDocument(content)
	if err != nil {
		return out, err
	}
	err = s.Repo.Within(ctx, id.User, true, func(u repository.Unit) error {
		old, e := u.Worklog(date)
		if e != nil {
			return e
		}
		if old.Version != version {
			return domain.Conflict()
		}
		// 空文档意味着清空日志，也需要单独的删除类操作权限。
		if !has {
			if e = id.Check(true, true); e != nil {
				return e
			}
		}
		now := formatTimestamp(s.Now())
		out = domain.Worklog{Date: date, Content: content, Version: version + 1, UpdatedAt: &now}
		return u.SaveWorklog(out, has)
	})
	return out, err
}
func (s *Service) Dates(ctx context.Context, id domain.Identity, start, end string) ([]string, error) {
	if err := id.Check(false, false); err != nil {
		return nil, err
	}
	if start != "" && !domain.ValidDate(start) || end != "" && !domain.ValidDate(end) || start != "" && end != "" && start > end {
		return nil, domain.Bad("日期范围非法")
	}
	var dates []string
	err := s.Repo.Within(ctx, id.User, false, func(u repository.Unit) error { var e error; dates, e = u.Dates(start, end); return e })
	return dates, err
}

type Filter struct {
	ProjectID *string `json:"projectId,omitempty"`
	Status    string  `json:"status,omitempty"`
	Tag       string  `json:"tag,omitempty"`
	Priority  string  `json:"priority,omitempty"`
	Search    string  `json:"search,omitempty"`
	StartDate string  `json:"startDate,omitempty"`
	EndDate   string  `json:"endDate,omitempty"`
	Done      *bool   `json:"done,omitempty"`
}
type IDInput struct {
	ID string `json:"id"`
}
type ProjectChange struct {
	IncludeArchived  bool             `json:"includeArchived,omitempty"`
	ExpectedRevision int64            `json:"expectedRevision"`
	Projects         []domain.Project `json:"projects"`
}
type CategoryDraft struct {
	OriginalName string `json:"originalName"`
	Name         string `json:"name"`
}
type CategoryChange struct {
	ExpectedRevision int64           `json:"expectedRevision"`
	Categories       []CategoryDraft `json:"categories"`
}
type CreateProject struct {
	ExpectedRevision int64  `json:"expectedRevision"`
	Name             string `json:"name"`
	IdempotencyKey   string `json:"idempotencyKey"`
}
type RenameProject struct {
	ExpectedRevision int64  `json:"expectedRevision"`
	ID               string `json:"id"`
	Name             string `json:"name"`
}
type DeleteProject struct {
	ExpectedRevision int64  `json:"expectedRevision"`
	ID               string `json:"id"`
}
type ReorderProjects struct {
	ExpectedRevision int64    `json:"expectedRevision"`
	IDs              []string `json:"ids"`
}
type CreateTask struct {
	Title          string `json:"title"`
	Description    string `json:"description,omitempty"`
	ProjectID      string `json:"projectId,omitempty"`
	Status         string `json:"status,omitempty"`
	Tag            string `json:"tag,omitempty"`
	Priority       string `json:"priority,omitempty"`
	StartDate      string `json:"startDate,omitempty"`
	Date           string `json:"date,omitempty"`
	Color          string `json:"color,omitempty"`
	IdempotencyKey string `json:"idempotencyKey"`
}

// UpdateTask 用指针区分“未提供字段”与“明确清空字段”，避免局部更新覆盖其他内容。
type UpdateTask struct {
	ID              string  `json:"id"`
	ExpectedVersion int64   `json:"expectedVersion"`
	Title           *string `json:"title,omitempty"`
	Description     *string `json:"description,omitempty"`
	ProjectID       *string `json:"projectId,omitempty"`
	Status          *string `json:"status,omitempty"`
	Tag             *string `json:"tag,omitempty"`
	Priority        *string `json:"priority,omitempty"`
	StartDate       *string `json:"startDate,omitempty"`
	Date            *string `json:"date,omitempty"`
	Color           *string `json:"color,omitempty"`
}
type DeleteResource struct {
	ID              string `json:"id"`
	ExpectedVersion int64  `json:"expectedVersion"`
}
type CreateTodo struct {
	Title          string `json:"title"`
	Done           bool   `json:"done,omitempty"`
	IdempotencyKey string `json:"idempotencyKey"`
}
type UpdateTodo struct {
	ID              string  `json:"id"`
	ExpectedVersion int64   `json:"expectedVersion"`
	Title           *string `json:"title,omitempty"`
	Done            *bool   `json:"done,omitempty"`
}
type CollectionResult struct {
	Projects         []domain.Project      `json:"projects"`
	Categories       []string              `json:"categories"`
	ProjectRevision  int64                 `json:"projectRevision"`
	CategoryRevision int64                 `json:"categoryRevision"`
	Revision         int64                 `json:"revision"`
	UpdatedTasks     []domain.TaskResource `json:"updatedTasks"`
}

func taskResource(st *domain.State, t domain.Task) domain.TaskResource {
	return domain.TaskResource{Task: t, Version: st.TaskVersions[t.ID.Key()]}
}
func todoResource(st *domain.State, t domain.Todo) domain.TodoResource {
	return domain.TodoResource{Todo: t, Version: st.TodoVersions[t.ID.Key()]}
}
func taskIndex(st *domain.State, id string) int {
	for i, t := range st.Tasks {
		if t.ID.Key() == id {
			return i
		}
	}
	return -1
}
func todoIndex(st *domain.State, id string) int {
	for i, t := range st.Todos {
		if t.ID.Key() == id {
			return i
		}
	}
	return -1
}
func projectIndex(st *domain.State, id string) int {
	for i, p := range st.Projects {
		if p.ID == id {
			return i
		}
	}
	return -1
}
func expected(current, expected int64) error {
	if !domain.ValidVersion(expected) {
		return domain.Bad("并发版本非法")
	}
	if current != expected {
		return domain.Conflict()
	}
	return nil
}
func decode[T any](raw json.RawMessage, keys ...string) (T, error) {
	var v T
	err := domain.StrictJSON(raw, &v, keys...)
	return v, err
}

// fingerprint 将操作名与规范化后的 JSON 一起计算摘要，避免字段顺序影响幂等判断。
func fingerprint(op string, raw json.RawMessage) string {
	var v any
	_ = json.Unmarshal(raw, &v)
	canonical, _ := json.Marshal(v)
	h := sha256.Sum256(append([]byte(op+"\n"), canonical...))
	return hex.EncodeToString(h[:])
}
func collection(st, old *domain.State) CollectionResult {
	visible := domain.VisibleDashboard(st.Dashboard)
	out := CollectionResult{Projects: visible.Projects, Categories: st.Categories, ProjectRevision: st.ProjectRevision, CategoryRevision: st.CategoryRevision, Revision: st.Revision, UpdatedTasks: []domain.TaskResource{}}
	for _, t := range visible.Tasks {
		if st.TaskVersions[t.ID.Key()] != old.TaskVersions[t.ID.Key()] {
			out.UpdatedTasks = append(out.UpdatedTasks, taskResource(st, t))
		}
	}
	return out
}

// Execute 是 REST 与 MCP 共用的细粒度操作入口。
// 即使传输层已解析参数，这里仍会校验必填字段、资源归属、权限和版本，
// 确保任何接入方式都不能绕过业务约束。
func (s *Service) Execute(ctx context.Context, id domain.Identity, op string, raw json.RawMessage) (any, error) {
	read := strings.HasPrefix(op, "list_") || op == "get_task" || op == "get_dashboard_summary"
	destructive := strings.HasPrefix(op, "delete_") || op == "manage_categories" || op == "replace_projects"
	if err := id.Check(!read, !read && destructive); err != nil {
		return nil, err
	}
	var out any
	err := s.Repo.Within(ctx, id.User, !read, func(u repository.Unit) error {
		old, e := u.Load()
		if e != nil {
			return e
		}
		st := copyState(old)
		if read {
			out, e = readOperation(op, raw, st)
			return e
		}
		key := ""
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil {
			return domain.Bad("请求必须是对象")
		}
		if v, ok := fields["idempotencyKey"]; ok {
			if json.Unmarshal(v, &key) != nil {
				return domain.Bad("幂等键必须是字符串")
			}
		}
		creating := strings.HasPrefix(op, "create_")
		fp := fingerprint(op, raw)
		if creating {
			// 查找幂等记录前仍须校验请求结构，不能让无效或有歧义的请求
			// 因命中历史记录而被当作成功请求。
			switch op {
			case "create_task":
				_, e = decode[CreateTask](raw, "title", "idempotencyKey")
			case "create_todo":
				_, e = decode[CreateTodo](raw, "title", "idempotencyKey")
			case "create_project":
				_, e = decode[CreateProject](raw, "name", "expectedRevision", "idempotencyKey")
			default:
				e = domain.NotFound()
			}
			if e != nil {
				return e
			}
			if strings.TrimSpace(key) == "" || len(key) > 200 || strings.ContainsFunc(key, unicode.IsControl) {
				return domain.Bad("创建必须提供 1–200 字节且不含控制字符的 idempotencyKey")
			}
			prior, response, e := u.Idempotency(key)
			if e != nil {
				return e
			}
			if prior != "" {
				if prior != fp {
					return &domain.Error{Status: 409, Code: "IDEMPOTENCY_CONFLICT", Message: "幂等键已用于不同请求"}
				}
				return json.Unmarshal(response, &out)
			}
		}
		result, e := mutate(op, raw, st)
		if e != nil {
			return e
		}
		forceProjects := op == "replace_projects" || strings.HasSuffix(op, "_project") || op == "reorder_projects"
		if e = finalize(old, st, forceProjects, op == "manage_categories"); e != nil {
			return e
		}
		if e = u.Save(st); e != nil {
			return e
		}
		switch result.kind {
		case "task":
			i := taskIndex(st, result.id)
			out = taskResource(st, st.Tasks[i])
		case "todo":
			i := todoIndex(st, result.id)
			out = todoResource(st, st.Todos[i])
		case "deleted":
			out = map[string]any{"id": result.id, "deleted": true, "revision": st.Revision}
		default:
			out = collection(st, old)
		}
		// 幂等结果与业务修改在同一事务中提交，避免重试时重复创建资源。
		if creating {
			response, e := json.Marshal(out)
			if e != nil {
				return e
			}
			if e = u.Remember(key, fp, response); e != nil {
				return e
			}
		}
		return nil
	})
	return out, err
}

type mutation struct{ kind, id string }

func mutate(op string, raw json.RawMessage, st *domain.State) (mutation, error) {
	fail := func(e error) (mutation, error) { return mutation{}, e }
	switch op {
	case "replace_projects":
		in, e := decode[ProjectChange](raw, "expectedRevision", "projects")
		if e != nil {
			return fail(e)
		}
		if e = expected(st.ProjectRevision, in.ExpectedRevision); e != nil {
			return fail(e)
		}
		previousProjects := st.Projects
		st.Projects = in.Projects
		if !in.IncludeArchived {
			for _, p := range previousProjects {
				if p.Archived && projectIndex(st, p.ID) < 0 {
					st.Projects = append(st.Projects, p)
				}
			}
		}
		valid := map[string]bool{}
		for _, p := range st.Projects {
			valid[p.ID] = true
		}
		for i := range st.Tasks {
			if !valid[st.Tasks[i].ProjectID] {
				st.Tasks[i].ProjectID = ""
			}
		}
	case "create_project":
		in, e := decode[CreateProject](raw, "expectedRevision", "name", "idempotencyKey")
		if e != nil {
			return fail(e)
		}
		if e = expected(st.ProjectRevision, in.ExpectedRevision); e != nil {
			return fail(e)
		}
		st.Projects = append(st.Projects, domain.Project{ID: uuid.NewString(), Name: in.Name})
	case "rename_project":
		in, e := decode[RenameProject](raw, "expectedRevision", "id", "name")
		if e != nil {
			return fail(e)
		}
		if e = expected(st.ProjectRevision, in.ExpectedRevision); e != nil {
			return fail(e)
		}
		i := projectIndex(st, in.ID)
		if i < 0 {
			return fail(domain.NotFound())
		}
		st.Projects[i].Name = in.Name
	case "delete_project":
		in, e := decode[DeleteProject](raw, "expectedRevision", "id")
		if e != nil {
			return fail(e)
		}
		if e = expected(st.ProjectRevision, in.ExpectedRevision); e != nil {
			return fail(e)
		}
		i := projectIndex(st, in.ID)
		if i < 0 {
			return fail(domain.NotFound())
		}
		st.Projects = append(st.Projects[:i], st.Projects[i+1:]...)
		for i := range st.Tasks {
			if st.Tasks[i].ProjectID == in.ID {
				st.Tasks[i].ProjectID = ""
			}
		}
	case "reorder_projects":
		in, e := decode[ReorderProjects](raw, "expectedRevision", "ids")
		if e != nil {
			return fail(e)
		}
		if e = expected(st.ProjectRevision, in.ExpectedRevision); e != nil {
			return fail(e)
		}
		if len(in.IDs) != len(st.Projects) {
			return fail(domain.Bad("必须提供全部项目 ID"))
		}
		projects := []domain.Project{}
		seen := map[string]bool{}
		for _, id := range in.IDs {
			i := projectIndex(st, id)
			if i < 0 || seen[id] {
				return fail(domain.Bad("项目 ID 缺失或重复"))
			}
			seen[id] = true
			projects = append(projects, st.Projects[i])
		}
		st.Projects = projects
	case "manage_categories":
		in, e := decode[CategoryChange](raw, "expectedRevision", "categories")
		if e != nil {
			return fail(e)
		}
		if e = expected(st.CategoryRevision, in.ExpectedRevision); e != nil {
			return fail(e)
		}
		if len(in.Categories) == 0 {
			return fail(domain.Bad("至少保留一个分类"))
		}
		mapping := map[string]string{}
		existing := map[string]bool{}
		for _, c := range st.Categories {
			existing[c] = true
		}
		categories := []string{}
		for _, d := range in.Categories {
			name, e := domain.Name(d.Name, 40, false)
			if e != nil {
				return fail(e)
			}
			if d.OriginalName != "" {
				if !existing[d.OriginalName] || mapping[d.OriginalName] != "" {
					return fail(domain.Bad("原分类不存在或重复映射"))
				}
				mapping[d.OriginalName] = name
			}
			categories = append(categories, name)
		}
		for i := range st.Tasks {
			tag := mapping[st.Tasks[i].Tag]
			if tag == "" {
				tag = categories[0]
			}
			st.Tasks[i].Tag = tag
		}
		st.Categories = categories
	case "create_task":
		in, e := decode[CreateTask](raw, "title", "idempotencyKey")
		if e != nil {
			return fail(e)
		}
		t := domain.Task{ID: domain.StringID(uuid.NewString()), Title: in.Title, Description: in.Description, ProjectID: in.ProjectID, Status: in.Status, Tag: in.Tag, Priority: in.Priority, StartDate: in.StartDate, Date: in.Date, Color: in.Color}
		if t.Status == "" {
			t.Status = "planned"
		}
		if t.Tag == "" {
			t.Tag = st.Categories[0]
			for _, c := range st.Categories {
				if c == "开发" {
					t.Tag = c
				}
			}
		}
		if t.Priority == "" {
			t.Priority = "中"
		}
		if t.Color == "" {
			t.Color = "white"
		}
		st.Tasks = append(st.Tasks, t)
		return mutation{"task", t.ID.Key()}, nil
	case "update_task", "move_task":
		required := []string{"id", "expectedVersion"}
		if op == "move_task" {
			required = append(required, "projectId", "status")
		}
		in, e := decode[UpdateTask](raw, required...)
		if e != nil {
			return fail(e)
		}
		i := taskIndex(st, in.ID)
		if i < 0 {
			return fail(domain.NotFound())
		}
		if e = expected(st.TaskVersions[in.ID], in.ExpectedVersion); e != nil {
			return fail(e)
		}
		t := &st.Tasks[i]
		for _, pair := range []struct {
			dst *string
			src *string
		}{{&t.Title, in.Title}, {&t.Description, in.Description}, {&t.ProjectID, in.ProjectID}, {&t.Status, in.Status}, {&t.Tag, in.Tag}, {&t.Priority, in.Priority}, {&t.StartDate, in.StartDate}, {&t.Date, in.Date}, {&t.Color, in.Color}} {
			if pair.src != nil {
				*pair.dst = *pair.src
			}
		}
		// 即使 PATCH 未改变字段值，也要消耗资源版本，防止旧请求继续被接受。
		if e = incrementVersionNoop(st.TaskVersions, in.ID); e != nil {
			return fail(e)
		}
		return mutation{"task", in.ID}, nil
	case "delete_task", "delete_todo":
		in, e := decode[DeleteResource](raw, "id", "expectedVersion")
		if e != nil {
			return fail(e)
		}
		if op == "delete_task" {
			i := taskIndex(st, in.ID)
			if i < 0 {
				return fail(domain.NotFound())
			}
			if e = expected(st.TaskVersions[in.ID], in.ExpectedVersion); e != nil {
				return fail(e)
			}
			st.Tasks = append(st.Tasks[:i], st.Tasks[i+1:]...)
		} else {
			i := todoIndex(st, in.ID)
			if i < 0 {
				return fail(domain.NotFound())
			}
			if e = expected(st.TodoVersions[in.ID], in.ExpectedVersion); e != nil {
				return fail(e)
			}
			st.Todos = append(st.Todos[:i], st.Todos[i+1:]...)
		}
		return mutation{"deleted", in.ID}, nil
	case "create_todo":
		in, e := decode[CreateTodo](raw, "title", "idempotencyKey")
		if e != nil {
			return fail(e)
		}
		t := domain.Todo{ID: domain.StringID(uuid.NewString()), Title: in.Title, Done: in.Done}
		st.Todos = append(st.Todos, t)
		return mutation{"todo", t.ID.Key()}, nil
	case "update_todo", "set_todo_done":
		required := []string{"id", "expectedVersion"}
		if op == "set_todo_done" {
			required = append(required, "done")
		}
		in, e := decode[UpdateTodo](raw, required...)
		if e != nil {
			return fail(e)
		}
		i := todoIndex(st, in.ID)
		if i < 0 {
			return fail(domain.NotFound())
		}
		if e = expected(st.TodoVersions[in.ID], in.ExpectedVersion); e != nil {
			return fail(e)
		}
		if in.Title != nil {
			st.Todos[i].Title = *in.Title
		}
		if in.Done != nil {
			st.Todos[i].Done = *in.Done
		}
		if e = incrementVersionNoop(st.TodoVersions, in.ID); e != nil {
			return fail(e)
		}
		return mutation{"todo", in.ID}, nil
	default:
		return fail(domain.NotFound())
	}
	return mutation{"collection", ""}, nil
}
func incrementVersionNoop(versions map[string]int64, id string) error {
	v := versions[id]
	if e := increment(&v); e != nil {
		return e
	}
	versions[id] = v
	return nil
}
func readOperation(op string, raw json.RawMessage, st *domain.State) (any, error) {
	if op != "list_projects" {
		copy := *st
		copy.Dashboard = domain.VisibleDashboard(st.Dashboard)
		st = &copy
	}
	switch op {
	case "list_projects":
		in, e := decode[struct {
			IncludeArchived bool `json:"includeArchived"`
		}](raw)
		if e != nil {
			return nil, e
		}
		projects := st.Projects
		if !in.IncludeArchived {
			projects = domain.VisibleDashboard(st.Dashboard).Projects
		}
		return map[string]any{"projects": projects, "projectRevision": st.ProjectRevision, "revision": st.Revision}, nil
	case "list_categories":
		if _, e := decode[struct{}](raw); e != nil {
			return nil, e
		}
		return map[string]any{"categories": st.Categories, "categoryRevision": st.CategoryRevision, "revision": st.Revision}, nil
	case "get_task":
		in, e := decode[IDInput](raw, "id")
		if e != nil {
			return nil, e
		}
		i := taskIndex(st, in.ID)
		if i < 0 {
			return nil, domain.NotFound()
		}
		return taskResource(st, st.Tasks[i]), nil
	case "get_dashboard_summary":
		if _, e := decode[struct{}](raw); e != nil {
			return nil, e
		}
		counts := map[string]int{"planned": 0, "progress": 0, "done": 0}
		for _, t := range st.Tasks {
			counts[t.Status]++
		}
		pending := 0
		for _, t := range st.Todos {
			if !t.Done {
				pending++
			}
		}
		return map[string]any{"revision": st.Revision, "taskCounts": counts, "taskCount": len(st.Tasks), "projectCount": len(st.Projects), "todoCount": len(st.Todos), "pendingTodos": pending}, nil
	case "list_tasks", "list_todos":
		f, e := decode[Filter](raw)
		if e != nil {
			return nil, e
		}
		if op == "list_todos" && (f.ProjectID != nil || f.Status != "" || f.Tag != "" || f.Priority != "" || f.StartDate != "" || f.EndDate != "") || op == "list_tasks" && f.Done != nil {
			return nil, domain.Bad("此集合不支持该筛选参数")
		}
		if f.StartDate != "" && !domain.ValidDate(f.StartDate) || f.EndDate != "" && !domain.ValidDate(f.EndDate) || f.StartDate != "" && f.EndDate != "" && f.StartDate > f.EndDate {
			return nil, domain.Bad("日期范围非法")
		}
		if f.Status != "" && f.Status != "planned" && f.Status != "progress" && f.Status != "done" {
			return nil, domain.Bad("状态非法")
		}
		if f.Priority != "" && f.Priority != "低" && f.Priority != "中" && f.Priority != "高" && f.Priority != "极高" {
			return nil, domain.Bad("优先级非法")
		}
		search := strings.ToLower(strings.TrimSpace(f.Search))
		if op == "list_todos" {
			out := []domain.TodoResource{}
			for _, t := range st.Todos {
				if (f.Done == nil || t.Done == *f.Done) && (search == "" || strings.Contains(strings.ToLower(t.Title), search)) {
					out = append(out, todoResource(st, t))
				}
			}
			return map[string]any{"todos": out, "revision": st.Revision}, nil
		}
		out := []domain.TaskResource{}
		for _, t := range st.Tasks {
			if f.ProjectID != nil && t.ProjectID != *f.ProjectID || f.Status != "" && t.Status != f.Status || f.Tag != "" && t.Tag != f.Tag || f.Priority != "" && t.Priority != f.Priority || search != "" && !strings.Contains(strings.ToLower(t.Title+" "+t.Description), search) {
				continue
			}
			start, end := t.StartDate, t.Date
			if start == "" {
				start = end
			}
			if end == "" {
				end = start
			}
			if f.StartDate != "" && (end == "" || end < f.StartDate) || f.EndDate != "" && (start == "" || start > f.EndDate) {
				continue
			}
			out = append(out, taskResource(st, t))
		}
		return map[string]any{"tasks": out, "revision": st.Revision}, nil
	}
	return nil, domain.NotFound()
}
