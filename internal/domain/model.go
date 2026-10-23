package domain

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
)

// MaxVersion 与 JavaScript 的最大安全整数一致，避免前后端传递版本时丢失精度。
const MaxVersion int64 = 9007199254740991

type Error struct {
	Status  int    `json:"-"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Message }
func Bad(message string) error { return &Error{400, "INVALID_ARGUMENT", message} }
func Conflict() error {
	return &Error{409, "VERSION_CONFLICT", "数据已被其他页面修改，请备份草稿并重新加载"}
}
func Forbidden() error { return &Error{403, "FORBIDDEN", "没有执行此操作的授权"} }
func NotFound() error  { return &Error{404, "NOT_FOUND", "资源不存在"} }
// Public 仅向客户端暴露业务错误，隐藏数据库、路径及其他内部错误细节。
func Public(err error) *Error {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return &Error{503, "TIMEOUT", "操作超时，请重新读取确认保存结果"}
	}
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return &Error{500, "INTERNAL_ERROR", "服务暂时不可用，请稍后重试"}
}

type Identity struct {
	User        string
	Write       bool
	Destructive bool
}

func (i Identity) Check(write, destructive bool) error {
	if i.User == "" {
		return &Error{401, "UNAUTHENTICATED", "需要认证"}
	}
	if write && !i.Write || destructive && !i.Destructive {
		return Forbidden()
	}
	return nil
}

// ID 在快照读写时保留旧版数字 ID 的类型，比较时则统一使用字符串值。
type ID string

func (id ID) Key() string {
	if len(id) > 0 && id[0] == '"' {
		var s string
		_ = json.Unmarshal([]byte(id), &s)
		return s
	}
	return string(id)
}
func StringID(s string) ID { b, _ := json.Marshal(s); return ID(b) }
func (id ID) MarshalJSON() ([]byte, error) {
	if id == "" {
		return []byte(`""`), nil
	}
	return []byte(id), nil
}
func (id *ID) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		var s string
		if json.Unmarshal(b, &s) != nil || s == "" {
			return Bad("ID 必须为非空字符串或安全整数")
		}
		*id = StringID(s)
		return nil
	}
	n, err := strconv.ParseInt(string(b), 10, 64)
	if err != nil || n < -MaxVersion || n > MaxVersion {
		return Bad("ID 必须为非空字符串或安全整数")
	}
	*id = ID(strconv.FormatInt(n, 10))
	return nil
}

type Project struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type Task struct {
	ID          ID     `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	ProjectID   string `json:"projectId"`
	Status      string `json:"status"`
	Tag         string `json:"tag"`
	Priority    string `json:"priority"`
	StartDate   string `json:"startDate"`
	Date        string `json:"date"`
	Color       string `json:"color"`
}
type Todo struct {
	ID    ID     `json:"id"`
	Title string `json:"title"`
	Done  bool   `json:"done"`
}
type Dashboard struct {
	Revision      int64     `json:"revision"`
	PersonalLabel string    `json:"personalLabel"`
	Projects      []Project `json:"projects"`
	Categories    []string  `json:"categories"`
	Tasks         []Task    `json:"tasks"`
	Todos         []Todo    `json:"todos"`
}
// State 在工作台快照之外维护集合和单项资源的版本，供细粒度接口检测并发修改。
type State struct {
	Dashboard
	ProjectRevision  int64
	CategoryRevision int64
	TaskVersions     map[string]int64
	TodoVersions     map[string]int64
}

func Empty() *State {
	return &State{Dashboard: Dashboard{PersonalLabel: "个人工作台", Projects: []Project{}, Categories: []string{"开发", "临时", "个人", "产品", "设计", "调研"}, Tasks: []Task{}, Todos: []Todo{}}, TaskVersions: map[string]int64{}, TodoVersions: map[string]int64{}}
}

type TaskResource struct {
	Task
	Version int64 `json:"version"`
}
type TodoResource struct {
	Todo
	Version int64 `json:"version"`
}
type Worklog struct {
	Date      string          `json:"date"`
	Content   json.RawMessage `json:"content"`
	UpdatedAt *string         `json:"updatedAt"`
	Version   int64           `json:"version"`
}

func EmptyWorklog(date string) Worklog {
	return Worklog{Date: date, Content: json.RawMessage(`{"type":"doc","content":[{"type":"paragraph"}]}`)}
}
func ValidDate(s string) bool {
	if len(s) != 10 || s[4] != '-' || s[7] != '-' {
		return false
	}
	t, e := time.Parse("2006-01-02", s)
	return e == nil && t.Year() >= 1 && t.Format("2006-01-02") == s
}
func ValidVersion(v int64) bool { return v >= 0 && v < MaxVersion }
// Units 按 UTF-16 码元计数，与浏览器 String.length 的长度规则保持一致。
func Units(s string) int        { return len(utf16.Encode([]rune(s))) }

// Trim 与 JavaScript String.trim 的空白规则一致，包含 BOM，不包含 U+0085。
func Trim(s string) string {
	return strings.TrimFunc(s, func(r rune) bool {
		return r >= 0x9 && r <= 0xd || r == 0x20 || r == 0xa0 || r == 0x1680 || r >= 0x2000 && r <= 0x200a || r == 0x2028 || r == 0x2029 || r == 0x202f || r == 0x205f || r == 0x3000 || r == 0xfeff
	})
}
func Name(s string, max int, empty bool) (string, error) {
	s = Trim(s)
	if Units(s) > max {
		return "", Bad(fmt.Sprintf("文字不能超过 %d UTF-16 码元", max))
	}
	if !empty && s == "" {
		return "", Bad("名称不能为空")
	}
	if strings.ContainsRune(s, 0) {
		return "", Bad("文字不能包含空字符")
	}
	return s, nil
}
func member(s string, values ...string) bool {
	for _, v := range values {
		if v == s {
			return true
		}
	}
	return false
}
// Validate 校验快照的字段、唯一性和引用关系，同时原地规范化名称。
// 调用方若需保留原始输入，应先复制数据。
func Validate(d *Dashboard) error {
	if !ValidVersion(d.Revision) {
		return Bad("revision 非法或已达到上限")
	}
	var err error
	if Units(d.PersonalLabel) > 255 {
		return Bad("工作台文字原始输入不能超过 255 UTF-16 码元")
	}
	d.PersonalLabel, err = Name(d.PersonalLabel, 255, true)
	if err != nil {
		return err
	}
	if d.Projects == nil || d.Categories == nil || d.Tasks == nil || d.Todos == nil {
		return Bad("集合必须为数组")
	}
	projects := map[string]bool{}
	names := map[string]bool{}
	for n := range d.Projects {
		p := &d.Projects[n]
		if p.ID == "" || p.ID == "__all__" || strings.ContainsRune(p.ID, 0) || projects[p.ID] {
			return Bad("项目 ID 非法或重复")
		}
		p.Name, err = Name(p.Name, 40, false)
		if err != nil {
			return err
		}
		key := strings.ToLower(p.Name)
		if names[key] {
			return Bad("项目名称重复")
		}
		names[key] = true
		projects[p.ID] = true
	}
	if len(d.Categories) == 0 {
		return Bad("至少保留一个分类")
	}
	categories := map[string]bool{}
	names = map[string]bool{}
	for n, c := range d.Categories {
		c, err = Name(c, 40, false)
		if err != nil {
			return err
		}
		key := strings.ToLower(c)
		if names[key] {
			return Bad("分类名称重复")
		}
		names[key] = true
		categories[c] = true
		d.Categories[n] = c
	}
	ids := map[string]bool{}
	for n := range d.Tasks {
		t := &d.Tasks[n]
		key := t.ID.Key()
		if key == "" || strings.ContainsRune(key, 0) || ids[key] {
			return Bad("任务 ID 非法或重复")
		}
		ids[key] = true
		t.Title, err = Name(t.Title, 100, false)
		if err != nil {
			return err
		}
		if strings.ContainsRune(t.Description, 0) {
			return Bad("描述不能包含空字符")
		}
		if t.ProjectID != "" && !projects[t.ProjectID] || !categories[t.Tag] {
			return Bad("任务项目或分类引用非法")
		}
		if !member(t.Status, "planned", "progress", "done") || !member(t.Priority, "低", "中", "高", "极高") || !member(t.Color, "white", "red", "orange", "blue", "green") {
			return Bad("任务枚举字段非法")
		}
		if t.StartDate != "" && !ValidDate(t.StartDate) || t.Date != "" && !ValidDate(t.Date) || t.StartDate != "" && t.Date != "" && t.StartDate > t.Date {
			return Bad("任务日期非法或截止早于开始")
		}
	}
	ids = map[string]bool{}
	for n := range d.Todos {
		t := &d.Todos[n]
		key := t.ID.Key()
		if key == "" || strings.ContainsRune(key, 0) || ids[key] {
			return Bad("待办 ID 非法或重复")
		}
		ids[key] = true
		t.Title, err = Name(t.Title, 100, false)
		if err != nil {
			return err
		}
	}
	return nil
}

// StrictJSON 拒绝未知字段、顶层字段的 null 值以及多余的 JSON 值。
// 局部操作由入口指定必填字段，完整快照则由各模型指定。
func StrictJSON(b []byte, out any, required ...string) error {
	if err := UnambiguousJSON(b); err != nil {
		return err
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(b, &obj); err != nil || obj == nil {
		return Bad("请求必须为 JSON 对象")
	}
	for _, k := range required {
		if _, ok := obj[k]; !ok {
			return Bad("缺少字段：" + k)
		}
	}
	// encoding/json 默认忽略结构体字段名的大小写，但接口协议区分大小写，
	// 因此要显式拒绝 Version 等别名，避免同一字段有多种解释。
	typ := reflect.TypeOf(out)
	for typ != nil && typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ != nil && typ.Kind() == reflect.Struct {
		allowed := map[string]bool{}
		for i := range typ.NumField() {
			f := typ.Field(i)
			if !f.IsExported() {
				continue
			}
			tag := strings.Split(f.Tag.Get("json"), ",")[0]
			if tag == "" {
				tag = f.Name
			}
			allowed[tag] = true
		}
		for k := range obj {
			if !allowed[k] {
				return Bad("JSON 包含未知字段")
			}
		}
	}
	for _, v := range obj {
		if bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return Bad("字段不能为 null")
		}
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		var e *Error
		if errors.As(err, &e) {
			return err
		}
		return Bad("JSON 字段或类型非法")
	}
	if dec.Decode(new(any)) != io.EOF {
		return Bad("只能提交一个 JSON 对象")
	}
	return nil
}
func (d *Dashboard) UnmarshalJSON(b []byte) error {
	type plain Dashboard
	return StrictJSON(b, (*plain)(d), "revision", "personalLabel", "projects", "categories", "tasks", "todos")
}
func (t *Task) UnmarshalJSON(b []byte) error {
	type plain Task
	return StrictJSON(b, (*plain)(t), "id", "title", "description", "projectId", "status", "tag", "priority", "startDate", "date", "color")
}
func (t *Todo) UnmarshalJSON(b []byte) error {
	type plain Todo
	return StrictJSON(b, (*plain)(t), "id", "title", "done")
}
func (p *Project) UnmarshalJSON(b []byte) error {
	type plain Project
	return StrictJSON(b, (*plain)(p), "id", "name")
}
