package storage

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/stdlib"
)

// These tests never contact PostgreSQL and must not run in parallel: t.Setenv
// intentionally models polluted deployment environments.
func cleanPostgresEnvironment(t *testing.T) {
	t.Helper()
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(strings.ToUpper(key), "PG") {
			t.Setenv(key, "")
		}
	}
	// Isolate test fixtures from the runner's standard pgx home-file defaults.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("APPDATA", home)
}

func TestPostgresRejectsEnvironmentBeforeParsingOrConnecting(t *testing.T) {
	cleanPostgresEnvironment(t)
	// Actual parseEnvSettings list in pgconn/config.go at pgx v5.11.0,
	// not the shorter list in its ParseConfig documentation.
	keys := []string{
		"PGHOST", "PGPORT", "PGDATABASE", "PGUSER", "PGPASSWORD", "PGPASSFILE",
		"PGAPPNAME", "PGCONNECT_TIMEOUT", "PGSSLMODE", "PGSSLKEY", "PGSSLCERT",
		"PGSSLSNI", "PGSSLROOTCERT", "PGSSLPASSWORD", "PGSSLNEGOTIATION",
		"PGTARGETSESSIONATTRS", "PGSERVICE", "PGSERVICEFILE", "PGTZ", "PGOPTIONS",
		"PGMINPROTOCOLVERSION", "PGMAXPROTOCOLVERSION", "PGCHANNELBINDING", "PGREQUIREAUTH",
		// Fail closed for future driver additions too.
		"PGFUTURESETTING",
	}
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, "environment-secret")
			_, err := Open(context.Background(), "postgres", "malformed-dsn-secret")
			if err == nil || !strings.Contains(err.Error(), "PG*") || !strings.Contains(err.Error(), "JSON") {
				t.Fatalf("expected explicit environment rejection, got %v", err)
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatalf("error exposes configuration: %v", err)
			}
			if os.Getenv(key) != "environment-secret" {
				t.Fatal("storage modified process environment")
			}
		})
	}
}

func TestPostgresRejectsEmptyOrInvalidDSN(t *testing.T) {
	cleanPostgresEnvironment(t)
	for _, dsn := range []string{"", " \t\r\n", "malformed-dsn-secret"} {
		if _, err := postgresConfig(dsn); err == nil || strings.Contains(err.Error(), "secret") {
			t.Errorf("expected safe DSN rejection, got %v", err)
		}
	}
}

func TestPostgresPreservesRawDSN(t *testing.T) {
	cleanPostgresEnvironment(t)
	const uri = "postgres://alice@db.example:5433/worklog?search_path=test_schema&"
	const keyword = "host=db.example port=5433 user=alice dbname=worklog search_path=test_schema sslmode=disable "
	for _, tt := range []struct {
		name, dsn, password, host string
		tls, verify               bool
		fallbackHost              string
	}{
		{"ssl alias last", uri + "sslmode=disable&ssl=true", "", "db.example", true, false, ""},
		{"sslmode last", uri + "ssl=true&sslmode=disable", "", "db.example", false, false, ""},
		{"verify-full last", uri + "ssl=true&sslmode=verify-full", "", "db.example", true, true, ""},
		{"verify-full superseded", uri + "sslmode=verify-full&ssl=true", "", "db.example", true, false, ""},
		{"query password space", uri + "sslmode=disable&password=space%20secret", "space secret", "db.example", false, false, ""},
		{"query password plus", uri + "sslmode=disable&password=literal+plus", "literal+plus", "db.example", false, false, ""},
		{"userinfo password space", "postgres://alice:space%20secret@db.example:5433/worklog?sslmode=disable&search_path=test_schema", "space secret", "db.example", false, false, ""},
		{"keyword empty password", keyword + "password=", "", "db.example", false, false, ""},
		{"keyword empty parameter", keyword + "password=secret application_name=", "secret", "db.example", false, false, ""},
		{"keyword password space", keyword + "password='space secret'", "space secret", "db.example", false, false, ""},
		{"keyword trailing backslash", keyword + "password=secret\\", "secret", "db.example", false, false, ""},
		{"IPv6 multi-host", "postgres://alice:secret@[::1]:5433,[::2]:5434/worklog?sslmode=disable&search_path=test_schema", "secret", "::1", false, false, "::2"},
		{"socket URI", "postgres://alice:secret@%2Ftmp%2Fpg:5433/worklog?sslmode=disable&search_path=test_schema", "secret", "/tmp/pg", false, false, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			config, err := postgresConfig(tt.dsn)
			if err != nil {
				t.Fatal(err)
			}
			if config.ConnString() != tt.dsn {
				t.Fatal("original DSN was rewritten")
			}
			if config.Password != tt.password {
				t.Fatal("password changed during parsing")
			}
			if tt.name == "keyword empty parameter" {
				if value, present := config.RuntimeParams["application_name"]; !present || value != "" {
					t.Fatal("empty trailing runtime parameter changed")
				}
			}
			if config.Host != tt.host || config.Port != 5433 || config.User != "alice" || config.Database != "worklog" || config.RuntimeParams["search_path"] != "test_schema" {
				t.Fatal("connection identity or runtime parameters changed")
			}
			if (config.TLSConfig != nil) != tt.tls {
				t.Fatal("TLS enablement changed")
			}
			if tt.tls && (config.TLSConfig.InsecureSkipVerify == tt.verify || (tt.verify && config.TLSConfig.ServerName != tt.host)) {
				t.Fatal("TLS verification changed")
			}
			if tt.fallbackHost != "" {
				if len(config.Fallbacks) != 1 || config.Fallbacks[0].Host != tt.fallbackHost || config.Fallbacks[0].Port != 5434 {
					t.Fatal("multi-host fallback changed")
				}
			} else if len(config.Fallbacks) != 0 {
				t.Fatal("unexpected connection fallback")
			}
		})
	}
}

func TestPostgresPreservesStandardFileSupport(t *testing.T) {
	cleanPostgresEnvironment(t)
	dir := t.TempDir()
	passfile, servicefile := filepath.Join(dir, "pgpass"), filepath.Join(dir, "pgservice.conf")
	if err := os.WriteFile(passfile, []byte("db.example:5433:worklog:alice:file-secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(servicefile, []byte("[test]\nhost=db.example\nport=5433\nuser=alice\ndbname=worklog\nsslmode=disable\nsearch_path=test_schema\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// Forward slashes also work on Windows and avoid keyword backslash escapes.
	passfile, servicefile = filepath.ToSlash(passfile), filepath.ToSlash(servicefile)
	for _, dsn := range []string{
		"host=db.example port=5433 user=alice dbname=worklog sslmode=disable search_path=test_schema passfile='" + passfile + "'",
		"service=test servicefile='" + servicefile + "' passfile='" + passfile + "'",
	} {
		config, err := postgresConfig(dsn)
		if err != nil {
			t.Fatal(err)
		}
		if config.ConnString() != dsn || config.Host != "db.example" || config.Port != 5433 || config.User != "alice" || config.Database != "worklog" || config.Password != "file-secret" || config.RuntimeParams["search_path"] != "test_schema" {
			t.Fatal("standard DSN file support changed")
		}
	}
}

func TestPostgresExplicitTLSAndMultiHost(t *testing.T) {
	cleanPostgresEnvironment(t)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certPath, keyPath := filepath.Join(dir, "client.crt"), filepath.Join(dir, "client.key")
	for path, data := range map[string][]byte{
		certPath: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		keyPath:  pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}),
	} {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	q := url.Values{"sslmode": {"verify-full"}, "sslrootcert": {certPath}, "sslcert": {certPath}, "sslkey": {keyPath}, "search_path": {"test_schema,public"}}
	for _, dsn := range []string{
		"postgres://alice:json-secret@db.example:5433,backup.example:5434/worklog?" + q.Encode(),
		"host=db.example,backup.example port=5433,5434 user=alice password=json-secret dbname=worklog sslmode=verify-full sslrootcert='" + certPath + "' sslcert='" + certPath + "' sslkey='" + keyPath + "' search_path='test_schema,public'",
	} {
		config, err := postgresConfig(dsn)
		if err != nil {
			t.Fatal(err)
		}
		if config.Password != "json-secret" || config.RuntimeParams["search_path"] != "test_schema,public" || len(config.Fallbacks) != 1 || config.Fallbacks[0].Host != "backup.example" || config.Fallbacks[0].Port != 5434 {
			t.Fatal("lost explicit credentials, runtime params or multi-host fallbacks")
		}
		if config.TLSConfig == nil || config.TLSConfig.ServerName != "db.example" || config.TLSConfig.InsecureSkipVerify || config.TLSConfig.RootCAs == nil || len(config.TLSConfig.Certificates) != 1 {
			t.Fatal("lost explicit TLS configuration")
		}
		if config.Fallbacks[0].TLSConfig == nil || config.Fallbacks[0].TLSConfig.ServerName != "backup.example" {
			t.Fatal("lost fallback TLS configuration")
		}
	}
}

func TestPostgresConnectorDoesNotReparseEnvironment(t *testing.T) {
	cleanPostgresEnvironment(t)
	config, err := postgresConfig("postgres://alice:json-secret@db.example:5433/worklog?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PGSERVICE", "must-not-be-read")
	t.Setenv("PGHOST", "unexpected.invalid")
	dialed := false
	config.DialFunc = func(_ context.Context, network, address string) (net.Conn, error) {
		dialed = true
		if network != "tcp" || address != "192.0.2.1:5433" {
			t.Errorf("unexpected dial: %s %s", network, address)
		}
		return nil, errors.New("intentional offline dial failure")
	}
	config.LookupFunc = func(_ context.Context, host string) ([]string, error) {
		if host != "db.example" {
			t.Errorf("unexpected host: %s", host)
		}
		return []string{"192.0.2.1"}, nil
	}
	db := stdlib.OpenDB(*config)
	defer db.Close()
	if err := db.PingContext(context.Background()); err == nil || !dialed {
		t.Fatalf("expected parsed-config offline dial, got dialed=%v err=%v", dialed, err)
	}
}
