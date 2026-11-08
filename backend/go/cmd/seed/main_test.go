package main

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
)

func TestRequireExplicitDemoFlag(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	for _, args := range [][]string{nil, {"-demo=false"}, {"-unknown"}, {"-demo", "unexpected"}} {
		if err := run(context.Background(), logger, args); err == nil {
			t.Fatal("expected explicit demo opt-in error")
		}
	}
}

func TestValidatePasswordsBeforeConnecting(t *testing.T) {
	t.Setenv("DEV_EMPLOYEE_A_PASSWORD", "")
	err := run(context.Background(), slog.New(slog.NewJSONHandler(io.Discard, nil)), []string{"-demo"})
	if err == nil || !strings.Contains(err.Error(), "DEV_EMPLOYEE_A_PASSWORD") {
		t.Fatal("expected missing demo password error before database connection")
	}
}
