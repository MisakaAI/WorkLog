package storage

import (
	"os"
	"strings"

	"github.com/jackc/pgx/v5"
	"worklog/internal/diagnostics"
)

// postgresConfig rejects nonempty PG* variables rather than modifying the
// process environment: pgx v5.11.0 has no environment-free ParseConfig API.
// Reject future PG* additions too, and enforce this for every storage.Open caller.
// The original DSN is passed unchanged to pgx; standard driver defaults and
// certificate, service and password file support are deliberately preserved.
func postgresConfig(dsn string) (*pgx.ConnConfig, error) {
	for _, entry := range os.Environ() {
		key, value, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(strings.ToUpper(key), "PG") && value != "" {
			return nil, diagnostics.Safe("clear nonempty PostgreSQL PG* environment variables; configure the database DSN in JSON instead")
		}
	}
	if strings.TrimSpace(dsn) == "" {
		return nil, diagnostics.Safe("PostgreSQL DSN must not be empty; configure the database DSN in JSON")
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		// Driver parse errors may contain malformed DSN secrets or file paths.
		return nil, diagnostics.Safe("invalid PostgreSQL DSN")
	}
	return config, nil
}
