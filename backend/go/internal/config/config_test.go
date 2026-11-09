package config

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func validEnv() map[string]string {
	return map[string]string{
		"IDENTITY_DATABASE_URL": "postgres://go_identity:secret@127.0.0.1:5433/cyberguard_identity?sslmode=disable",
		"INCIDENT_DATABASE_URL": "postgres://go_incidents:secret@127.0.0.1:5434/cyberguard_incidents?sslmode=disable",
	}
}

func lookup(values map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}
}

func unsetEnv(t *testing.T, name string) {
	t.Helper()
	// Setenv registers cleanup restoring the developer's original environment.
	t.Setenv(name, "")
	if err := os.Unsetenv(name); err != nil {
		t.Fatal(err)
	}
}

func TestDefaults(t *testing.T) {
	t.Parallel()
	env := validEnv()
	cfg, err := load(lookup(env))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != "127.0.0.1:8080" || cfg.DatabaseTimeout != 3*time.Second || cfg.ShutdownTimeout != 10*time.Second {
		t.Fatal("unexpected configuration defaults")
	}
	if cfg.IdentityDatabaseURL != env["IDENTITY_DATABASE_URL"] || cfg.IncidentDatabaseURL != env["INCIDENT_DATABASE_URL"] {
		t.Fatal("database URLs were not loaded independently")
	}
}

func TestOverrides(t *testing.T) {
	t.Parallel()
	env := validEnv()
	env["HTTP_ADDR"] = "[::1]:9000"
	env["DATABASE_TIMEOUT"] = "500ms"
	env["SHUTDOWN_TIMEOUT"] = "15s"
	cfg, err := load(lookup(env))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != "[::1]:9000" || cfg.DatabaseTimeout != 500*time.Millisecond || cfg.ShutdownTimeout != 15*time.Second {
		t.Fatal("configuration overrides were not applied")
	}
}

func TestLoadFromEnvironment(t *testing.T) {
	for name, value := range validEnv() {
		unsetEnv(t, name+"_FILE")
		t.Setenv(name, value)
	}
	t.Setenv("HTTP_ADDR", "127.0.0.1:9090")
	t.Setenv("DATABASE_TIMEOUT", "1s")
	t.Setenv("SHUTDOWN_TIMEOUT", "2s")
	t.Setenv("APP_ENV", "production")
	t.Setenv("SESSION_COOKIE_SECURE", "true")
	t.Setenv("TRUSTED_ORIGINS", "")
	t.Setenv("TRUSTED_PROXY_CIDRS", "")
	t.Setenv("SESSION_LIFETIME", "8h")
	t.Setenv("SESSION_IDLE_TIMEOUT", "30m")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != "127.0.0.1:9090" || cfg.DatabaseTimeout != time.Second || cfg.ShutdownTimeout != 2*time.Second {
		t.Fatal("Load did not read environment variables")
	}
}

func TestInvalidConfiguration(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{"empty identity URL", "IDENTITY_DATABASE_URL", ""},
		{"empty incident URL", "INCIDENT_DATABASE_URL", " "},
		{"invalid URL", "IDENTITY_DATABASE_URL", "postgres://user:sensitive%zz@localhost/db"},
		{"wrong scheme", "INCIDENT_DATABASE_URL", "https://user:sensitive@localhost/db"},
		{"missing host", "IDENTITY_DATABASE_URL", "postgres://user:sensitive@/db"},
		{"missing user", "IDENTITY_DATABASE_URL", "postgres://localhost/db"},
		{"missing database", "IDENTITY_DATABASE_URL", "postgres://user:sensitive@localhost/"},
		{"fragment", "IDENTITY_DATABASE_URL", "postgres://user:sensitive@localhost/db#fragment"},
		{"invalid database port", "INCIDENT_DATABASE_URL", "postgres://user:sensitive@localhost:70000/db"},
		{"empty address", "HTTP_ADDR", ""},
		{"missing HTTP port", "HTTP_ADDR", "localhost"},
		{"non-numeric HTTP port", "HTTP_ADDR", "localhost:http"},
		{"zero HTTP port", "HTTP_ADDR", "localhost:0"},
		{"invalid HTTP port", "HTTP_ADDR", "localhost:65536"},
		{"empty timeout", "DATABASE_TIMEOUT", ""},
		{"malformed timeout", "DATABASE_TIMEOUT", "sensitive"},
		{"zero timeout", "DATABASE_TIMEOUT", "0s"},
		{"negative timeout", "SHUTDOWN_TIMEOUT", "-1s"},
		{"excessive timeout", "SHUTDOWN_TIMEOUT", "2m"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := maps.Clone(validEnv())
			env[tt.key] = tt.value
			_, err := load(lookup(env))
			if err == nil || !strings.Contains(err.Error(), tt.key) {
				t.Fatalf("expected validation error naming %s", tt.key)
			}
			if strings.Contains(err.Error(), "sensitive") {
				t.Fatal("validation error leaked the configured value")
			}
		})
	}
}

func TestMissingDatabaseURL(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"IDENTITY_DATABASE_URL", "INCIDENT_DATABASE_URL"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			env := validEnv()
			delete(env, name)
			_, err := load(lookup(env))
			if err == nil || err.Error() != name+" is required" {
				t.Fatalf("expected required-variable error for %s", name)
			}
		})
	}
}

func TestLoadIdentityMigrationURL(t *testing.T) {
	unsetEnv(t, "IDENTITY_MIGRATION_DATABASE_URL_FILE")
	valid := "postgres://identity_migrator:test@127.0.0.1:5433/cyberguard_identity?sslmode=disable"
	for _, value := range []string{"", "postgres://user:sensitive%zz@localhost/db", valid} {
		t.Setenv("IDENTITY_MIGRATION_DATABASE_URL", value)
		loaded, err := LoadIdentityMigrationURL()
		if value == valid {
			if err != nil || loaded != valid {
				t.Fatal("migration configuration was not loaded independently")
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), "IDENTITY_MIGRATION_DATABASE_URL") || strings.Contains(err.Error(), "sensitive") {
			t.Fatal("expected safe migration configuration error")
		}
	}
}

func TestLoadIncidentMigrationURL(t *testing.T) {
	unsetEnv(t, "INCIDENT_MIGRATION_DATABASE_URL_FILE")
	valid := "postgres://incidents_migrator:test@127.0.0.1:5434/cyberguard_incidents?sslmode=disable"
	for _, value := range []string{"", "postgres://user:sensitive%zz@localhost/db", valid} {
		t.Setenv("INCIDENT_MIGRATION_DATABASE_URL", value)
		loaded, err := LoadIncidentMigrationURL()
		if value == valid {
			if err != nil || loaded != valid {
				t.Fatal("incident migration configuration was not loaded independently")
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), "INCIDENT_MIGRATION_DATABASE_URL") || strings.Contains(err.Error(), "sensitive") {
			t.Fatal("expected safe incident migration configuration error")
		}
	}
}

func TestDatabaseURLFiles(t *testing.T) {
	t.Parallel()
	env := validEnv()
	for _, name := range []string{"IDENTITY_DATABASE_URL", "INCIDENT_DATABASE_URL", "IDENTITY_MIGRATION_DATABASE_URL", "INCIDENT_MIGRATION_DATABASE_URL"} {
		value := "postgres://role:secret@127.0.0.1:5432/database?sslmode=disable"
		path := filepath.Join(t.TempDir(), "connection")
		if err := os.WriteFile(path, []byte(value+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		delete(env, name)
		env[name+"_FILE"] = path
		if got, err := databaseURL(lookup(env), name); err != nil || got != value {
			t.Fatal("file-backed database URL not validated")
		}
		if err := os.WriteFile(path, []byte("postgres://user:sensitive%zz@host/db"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := databaseURL(lookup(env), name); err == nil || strings.Contains(err.Error(), "sensitive") {
			t.Fatal("invalid file URL bypassed safe validation")
		}
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := load(lookup(env)); err != nil {
		t.Fatal("runtime configuration could not load both file URLs")
	}
	env["IDENTITY_DATABASE_URL"] = ""
	if _, err := load(lookup(env)); err == nil {
		t.Fatal("empty direct value silently overrode file configuration")
	}
}

func TestLoadMigrationURLFiles(t *testing.T) {
	for _, test := range []struct {
		name string
		load func() (string, error)
	}{
		{"IDENTITY_MIGRATION_DATABASE_URL", LoadIdentityMigrationURL},
		{"INCIDENT_MIGRATION_DATABASE_URL", LoadIncidentMigrationURL},
	} {
		unsetEnv(t, test.name)
		path := filepath.Join(t.TempDir(), "connection")
		value := "postgres://migrator:secret@127.0.0.1:5432/database?sslmode=disable"
		if err := os.WriteFile(path, []byte(value+"\r\n"), 0600); err != nil {
			t.Fatal(err)
		}
		t.Setenv(test.name+"_FILE", path)
		if got, err := test.load(); err != nil || got != value {
			t.Fatal("migration entry point did not read its own file setting")
		}
	}
}
