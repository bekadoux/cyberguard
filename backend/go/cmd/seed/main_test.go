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
	for _, args := range [][]string{nil, {"-demo=false"}, {"-unknown"}, {"-demo", "unexpected"},
		{"-create-owner"}, {"-demo", "-owner", "admin"}, {"-owner", ""}} {
		if err := run(context.Background(), logger, args, strings.NewReader("")); err == nil {
			t.Fatal("expected explicit demo opt-in error")
		}
	}
}

func TestValidatePasswordsBeforeConnecting(t *testing.T) {
	t.Setenv("DEV_EMPLOYEE_A_PASSWORD", "")
	err := run(context.Background(), slog.New(slog.NewJSONHandler(io.Discard, nil)), []string{"-demo"}, strings.NewReader(""))
	if err == nil || !strings.Contains(err.Error(), "DEV_EMPLOYEE_A_PASSWORD") {
		t.Fatal("expected missing demo password error before database connection")
	}
}

func TestReadOwnerPassword(t *testing.T) {
	for _, value := range []string{"", "short", strings.Repeat("x", 129), strings.Repeat(" ", 12)} {
		if _, err := readOwnerPassword(strings.NewReader(value)); err == nil {
			t.Fatal("invalid owner password accepted")
		}
	}
	hash, err := readOwnerPassword(strings.NewReader(strings.Repeat("x", 128) + "\r\n"))
	if err != nil || !strings.HasPrefix(hash, "$argon2id$") {
		t.Fatal("maximum-length owner password with CRLF was not hashed")
	}
}
