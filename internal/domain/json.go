package domain

import (
	"bytes"
	"encoding/json"
	"io"
	"unicode/utf8"
)

// UnambiguousJSON 拒绝重复键，避免不同 JSON 客户端对字段值、归属或版本
// 采用不同解释；同时检查编码、嵌套深度及多余的 JSON 值。
func UnambiguousJSON(b []byte) error {
	if !utf8.Valid(b) {
		return Bad("JSON 必须是有效 UTF-8")
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var value func(int) error
	value = func(depth int) error {
		if depth > 128 {
			return Bad("JSON 嵌套过深")
		}
		tok, e := dec.Token()
		if e != nil {
			return Bad("JSON 格式非法")
		}
		delim, ok := tok.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for dec.More() {
				tok, e = dec.Token()
				if e != nil {
					return Bad("JSON 格式非法")
				}
				k, ok := tok.(string)
				if !ok || seen[k] {
					return Bad("JSON 属性重复或非法")
				}
				seen[k] = true
				if e = value(depth + 1); e != nil {
					return e
				}
			}
		case '[':
			for dec.More() {
				if e = value(depth + 1); e != nil {
					return e
				}
			}
		default:
			return Bad("JSON 格式非法")
		}
		_, e = dec.Token()
		if e != nil {
			return Bad("JSON 格式非法")
		}
		return nil
	}
	if e := value(0); e != nil {
		return e
	}
	if _, e := dec.Token(); e != io.EOF {
		return Bad("只能提交一个 JSON 值")
	}
	return nil
}
