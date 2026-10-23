package diagnostics

import (
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"strings"
	"syscall"
	"testing"
)

func TestSafeConfigurationReason(t *testing.T) {
	b, err := json.Marshal(Fields(Wrap("load_configuration", Safe("invalid business timezone"))))
	if err != nil || !strings.Contains(string(b), "invalid business timezone") {
		t.Fatal(string(b), err)
	}
}

func TestFieldsPreserveCodesNotSensitiveMessages(t *testing.T) {
	secret := "postgres://user:password@db/private SELECT private_body"
	err := Wrap("migrate_database", errors.Join(&pgconn.PgError{Code: "23505", Message: secret, Detail: secret, Where: secret}, syscall.EACCES))
	b, e := json.Marshal(Fields(err))
	if e != nil {
		t.Fatal(e)
	}
	text := string(b)
	if strings.Contains(text, secret) || strings.Contains(text, "password") {
		t.Fatal(text)
	}
	for _, want := range []string{"23505", "migrate_database", "pgconn.PgError", "errno"} {
		if !strings.Contains(text, want) {
			t.Fatal("missing diagnostic", want, text)
		}
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatal("wrapper exposed cause")
	}
}
