package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"reflect"
	"sync/atomic"
	"testing"
	"worklog/internal/repository"
)

// A command recorder tests transaction protocol itself, not repository data.
// Actual persistence and rollback are exercised against real databases in integration.
type commandDriver struct{ commands *[]string }
type commandConn struct{ commands *[]string }

func (d commandDriver) Open(string) (driver.Conn, error)  { return commandConn{d.commands}, nil }
func (c commandConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unsupported") }
func (c commandConn) Close() error                        { return nil }
func (c commandConn) Begin() (driver.Tx, error)           { return nil, errors.New("unsupported") }
func (c commandConn) ExecContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Result, error) {
	*c.commands = append(*c.commands, q)
	return driver.RowsAffected(0), nil
}

var recorderSequence atomic.Int64

func TestTransactionTerminalCommands(t *testing.T) {
	commands := []string{}
	name := fmt.Sprintf("worklog-command-recorder-%d", recorderSequence.Add(1))
	sql.Register(name, commandDriver{&commands})
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := &Store{DB: db}
	for _, mode := range []string{"success", "error", "panic"} {
		commands = nil
		func() {
			defer func() {
				p := recover()
				if mode == "panic" && p == nil {
					t.Error("panic swallowed")
				}
			}()
			e := store.Within(context.Background(), "owner", false, func(repository.Unit) error {
				if mode == "panic" {
					panic("injected")
				}
				if mode == "error" {
					return errors.New("injected")
				}
				return nil
			})
			if (e != nil) != (mode == "error") {
				t.Error(e)
			}
		}()
		terminal := "COMMIT"
		if mode != "success" {
			terminal = "ROLLBACK"
		}
		if !reflect.DeepEqual(commands, []string{"BEGIN", terminal}) {
			t.Fatal(mode, commands)
		}
	}
}
