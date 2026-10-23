package service

import (
	"encoding/json"
	"worklog/internal/domain"
)

func (c *CategoryDraft) UnmarshalJSON(b []byte) error {
	type plain CategoryDraft
	return domain.StrictJSON(b, (*plain)(c), "originalName", "name")
}

var _ json.Unmarshaler = (*CategoryDraft)(nil)
