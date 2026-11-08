//go:build integration

package sessionstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bekadoux/cyberguard/backend/go/internal/testutil"
)

func TestPostgresConditionalPersistence(t *testing.T) {
	fixture := testutil.Identity(t, "../../../../db/migrations/identity")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s := NewPostgres(fixture.Runtime)
	expiry := time.Now().Add(time.Hour)
	created := WithRequest(ctx)
	if err := s.CommitCtx(created, "token", []byte("original"), expiry); err != nil {
		t.Fatal("could not create session")
	}
	// An unrelated new-token commit must not overwrite an existing row.
	if err := s.CommitCtx(WithRequest(ctx), "token", []byte("collision"), expiry); err == nil {
		t.Fatal("duplicate creation overwrote an existing session")
	}
	stale := WithRequest(ctx)
	if data, found, err := s.FindCtx(stale, "token"); err != nil || !found || string(data) != "original" {
		t.Fatal("duplicate creation modified the stored data")
	}
	if err := s.CommitCtx(stale, "token", []byte("refreshed"), expiry); err != nil {
		t.Fatal("live session refresh failed")
	}
	if _, err := fixture.Runtime.Exec(ctx, "UPDATE sessions SET expiry = current_timestamp - interval '1 second' WHERE token = 'token'"); err != nil {
		t.Fatal("could not expire session")
	}
	if err := s.CommitCtx(stale, "token", []byte("expired"), expiry); !errors.Is(err, ErrRevoked) {
		t.Fatal("stale refresh extended an expired session")
	}
	if err := s.DeleteCtx(ctx, "token"); err != nil {
		t.Fatal("could not delete session")
	}
	if err := s.CommitCtx(stale, "token", []byte("deleted"), expiry); !errors.Is(err, ErrRevoked) {
		t.Fatal("stale refresh recreated a deleted session")
	}
	var rows int
	if err := fixture.Runtime.QueryRow(ctx, "SELECT count(*) FROM sessions").Scan(&rows); err != nil || rows != 0 {
		t.Fatal("revoked session remained in PostgreSQL")
	}
	if err := s.CommitCtx(stale, "rotated-token", []byte("fresh"), expiry); err != nil {
		t.Fatal("rotation could not create a new token")
	}
}
