package domain

import "encoding/json"

// resourceJSON 先提取资源版本，再将剩余字段交给任务或待办模型严格校验。
func resourceJSON(b []byte, version *int64) ([]byte, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(b, &fields) != nil {
		return nil, Bad("资源必须是对象")
	}
	v, ok := fields["version"]
	if !ok || json.Unmarshal(v, version) != nil || *version < 1 || *version > MaxVersion {
		return nil, Bad("资源版本非法")
	}
	delete(fields, "version")
	return json.Marshal(fields)
}
func (t *TaskResource) UnmarshalJSON(b []byte) error {
	rest, e := resourceJSON(b, &t.Version)
	if e != nil {
		return e
	}
	return json.Unmarshal(rest, &t.Task)
}
func (t *TodoResource) UnmarshalJSON(b []byte) error {
	rest, e := resourceJSON(b, &t.Version)
	if e != nil {
		return e
	}
	return json.Unmarshal(rest, &t.Todo)
}
