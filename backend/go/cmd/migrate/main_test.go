package main

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
)

func TestRejectInvalidArguments(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	for _, args := range [][]string{nil, {"-unknown"}, {"-dir", "migrations", "unexpected"}} {
		if err := run(context.Background(), logger, args); err == nil {
			t.Fatal("expected usage error")
		}
	}
}

func TestRequireMigrationConfiguration(t *testing.T) {
	t.Setenv("IDENTITY_MIGRATION_DATABASE_URL", "")
	err := run(context.Background(), slog.New(slog.NewJSONHandler(io.Discard, nil)), []string{"-dir", "migrations"})
	if err == nil || !strings.Contains(err.Error(), "IDENTITY_MIGRATION_DATABASE_URL") {
		t.Fatal("expected missing migration configuration error")
	}
}
