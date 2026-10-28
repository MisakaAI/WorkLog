package repository

import (
	"context"
	"encoding/json"
	"worklog/internal/domain"
)

// Repository 定义业务层所需的存储能力。Within 表示一次原子操作，
// 实现必须在数据库层串行化同一用户的写入，不能只依赖进程内的锁。
type Repository interface {
	Within(context.Context, string, bool, func(Unit) error) error
	Ping(context.Context) error
}

// Unit 仅在 Within 回调内有效；所有读写及幂等记录共用同一事务和用户范围。
type Unit interface {
	Load() (*domain.State, error)
	// ResourceVersions 按涉及的 ID 查询历史版本，包括已删除资源；kind 为 task 或 todo。
	ResourceVersions(kind string, ids []string) (map[string]int64, error)
	Save(*domain.State) error
	Worklog(string) (domain.Worklog, error)
	SaveWorklog(domain.Worklog, bool) error
	Dates(string, string) ([]string, error)
	Idempotency(string) (string, json.RawMessage, error)
	Remember(string, string, json.RawMessage) error
}
