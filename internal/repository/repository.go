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
	Save(*domain.State) error
	Worklog(string) (domain.Worklog, error)
	SaveWorklog(domain.Worklog, bool) error
	Dates(string, string) ([]string, error)
	Idempotency(string) (string, json.RawMessage, error)
	Remember(string, string, json.RawMessage) error
}
