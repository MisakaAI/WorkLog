// Package diagnostics 提取不含敏感信息的错误诊断字段，不记录原始错误消息、
// SQL、连接串、路径或用户内容。Wrap 的操作名必须使用固定常量。
package diagnostics

import (
	"context"
	"errors"
	"fmt"
	"syscall"

	"github.com/jackc/pgx/v5/pgconn"
	"modernc.org/sqlite"
	"worklog/internal/domain"
)

// Safe 只接受代码中预先编写、不含敏感信息的配置或用法说明。
// 不得传入驱动消息、路径、请求值或任意 err.Error()。
func Safe(message string) error { return safeError(message) }

type safeError string

func (e safeError) Error() string { return string(e) }

type operationError struct {
	operation string
	cause     error
}

func (e *operationError) Error() string { return e.operation + " failed" }
func (e *operationError) Unwrap() error { return e.cause }
func Wrap(operation string, err error) error {
	if err == nil {
		return nil
	}
	return &operationError{operation, err}
}

func Fields(err error) []any {
	fields := []any{"error_type", fmt.Sprintf("%T", err)}
	types, operations := []string{}, []string{}
	// 遍历合并及包装后的错误；限制深度，防止第三方错误链异常或循环引用。
	var walk func(error, int)
	walk = func(e error, depth int) {
		if e == nil || depth > 12 {
			return
		}
		types = append(types, fmt.Sprintf("%T", e))
		if op, ok := e.(*operationError); ok {
			operations = append(operations, op.operation)
		}
		switch e := e.(type) {
		case interface{ Unwrap() []error }:
			for _, child := range e.Unwrap() {
				walk(child, depth+1)
			}
		case interface{ Unwrap() error }:
			walk(e.Unwrap(), depth+1)
		}
	}
	walk(err, 0)
	fields = append(fields, "error_chain", types, "operations", operations)
	var safe safeError
	if errors.As(err, &safe) {
		fields = append(fields, "reason", string(safe))
	}
	var public *domain.Error
	if errors.As(err, &public) {
		fields = append(fields, "public_code", public.Code, "public_message", public.Message)
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		fields = append(fields, "sqlstate", pg.Code)
	}
	var sq *sqlite.Error
	if errors.As(err, &sq) {
		fields = append(fields, "sqlite_code", sq.Code())
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		fields = append(fields, "errno", int(errno))
	}
	if errors.Is(err, context.DeadlineExceeded) {
		fields = append(fields, "timeout", true)
	}
	if errors.Is(err, context.Canceled) {
		fields = append(fields, "canceled", true)
	}
	return fields
}
