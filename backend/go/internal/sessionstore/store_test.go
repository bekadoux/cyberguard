package sessionstore

import (
	"context"
	"errors"
	"testing"
	"time"
)

type testBackend struct {
	data               map[string][]byte
	creates, refreshes int
}

func (b *testBackend) FindCtx(_ context.Context, token string) ([]byte, bool, error) {
	data, found := b.data[token]
	return data, found, nil
}

func (b *testBackend) DeleteCtx(_ context.Context, token string) error {
	delete(b.data, token)
	return nil
}

func (b *testBackend) CreateCtx(_ context.Context, token string, data []byte, _ time.Time) error {
	b.creates++
	if _, exists := b.data[token]; exists {
		return errors.New("token collision")
	}
	b.data[token] = data
	return nil
}

func (b *testBackend) RefreshCtx(_ context.Context, token string, data []byte, _ time.Time) error {
	b.refreshes++
	if _, exists := b.data[token]; !exists {
		return ErrRevoked
	}
	b.data[token] = data
	return nil
}

func TestRequestScopedCreationAndRefresh(t *testing.T) {
	b := &testBackend{data: make(map[string][]byte)}
	s := New(b)
	ctx := WithRequest(context.Background())
	expiry := time.Now().Add(time.Hour)
	if err := s.CommitCtx(ctx, "new-token", []byte("first"), expiry); err != nil {
		t.Fatal(err)
	}
	if err := s.CommitCtx(ctx, "new-token", []byte("second"), expiry); err != nil {
		t.Fatal(err)
	}
	if b.creates != 1 || b.refreshes != 1 {
		t.Fatal("repeated commit did not switch from creation to refresh")
	}
	stale := WithRequest(context.Background())
	if _, found, err := s.FindCtx(stale, "new-token"); err != nil || !found {
		t.Fatal("could not load existing token")
	}
	if err := s.DeleteCtx(ctx, "new-token"); err != nil {
		t.Fatal(err)
	}
	for _, loaded := range []context.Context{ctx, stale} {
		if err := s.CommitCtx(loaded, "new-token", []byte("stale"), expiry); !errors.Is(err, ErrRevoked) {
			t.Fatal("deleted token was not rejected")
		}
	}
	if b.creates != 1 || len(b.data) != 0 {
		t.Fatal("deleted token was recreated")
	}
	// Rotation uses a new SCS-generated token in a context that loaded the old one.
	if err := s.CommitCtx(stale, "rotated-token", []byte("fresh"), expiry); err != nil {
		t.Fatal(err)
	}
	if b.creates != 2 || string(b.data["rotated-token"]) != "fresh" {
		t.Fatal("rotation did not create an independent token")
	}
}

func TestPersistenceRequiresRequestContext(t *testing.T) {
	b := &testBackend{data: make(map[string][]byte)}
	s := New(b)
	if _, _, err := s.FindCtx(context.Background(), "token"); err == nil {
		t.Fatal("lookup without request state was accepted")
	}
	if err := s.CommitCtx(context.Background(), "token", nil, time.Now()); err == nil {
		t.Fatal("commit without request state was accepted")
	}
	if _, _, err := s.Find("token"); err == nil {
		t.Fatal("non-context lookup was accepted")
	}
	if s.Commit("token", nil, time.Now()) == nil || s.Delete("token") == nil || b.creates != 0 {
		t.Fatal("non-context persistence bypassed the guard")
	}
}
