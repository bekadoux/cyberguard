//go:build integration

package database

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexedwards/argon2id"
	"github.com/alexedwards/scs/pgxstore"
	"github.com/alexedwards/scs/v2"
	"github.com/bekadoux/cyberguard/backend/go/internal/db/identity"
	"github.com/bekadoux/cyberguard/backend/go/internal/demoseed"
	"github.com/bekadoux/cyberguard/backend/go/internal/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
)

func TestIdentityPersistenceIntegration(t *testing.T) {
	migrationURL := os.Getenv("TEST_IDENTITY_MIGRATION_DATABASE_URL")
	runtimeURL := os.Getenv("TEST_IDENTITY_DATABASE_URL")
	if migrationURL == "" || runtimeURL == "" {
		t.Fatal("set TEST_IDENTITY_MIGRATION_DATABASE_URL and TEST_IDENTITY_DATABASE_URL to a dedicated test instance")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	migrationConfig, err := pgxpool.ParseConfig(migrationURL)
	if err != nil {
		t.Fatal("invalid test migration configuration")
	}
	migrationConfig.MaxConns = 2
	migrationConfig.ConnConfig.ConnectTimeout = 3 * time.Second
	migrator, err := pgxpool.NewWithConfig(ctx, migrationConfig)
	if err != nil {
		t.Fatal("could not initialize test migration pool")
	}
	t.Cleanup(migrator.Close)

	// Every run owns a new schema. Down/reapply never touches application tables.
	schema := "persistence_test_" + strings.ToLower(rand.Text())
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := migrator.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatal("could not create isolated test schema")
	}
	t.Cleanup(func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if _, err := migrator.Exec(cleanupCtx, "DROP SCHEMA "+quotedSchema+" CASCADE"); err != nil {
			t.Error("could not remove the isolated test schema")
		}
	})
	if _, err := migrator.Exec(ctx, "GRANT USAGE ON SCHEMA "+quotedSchema+" TO go_identity"); err != nil {
		t.Fatal("could not grant test schema usage")
	}
	migrationSQLConfig := migrationConfig.ConnConfig.Copy()
	migrationSQLConfig.RuntimeParams["search_path"] = schema
	db := stdlib.OpenDB(*migrationSQLConfig)
	defer db.Close()
	provider, err := migrations.NewProvider(db, os.DirFS("../../../../db/migrations/identity"))
	if err != nil {
		t.Fatal("could not load identity migrations")
	}
	results, err := provider.Up(ctx)
	if err != nil || len(results) != 3 {
		t.Fatal("identity migrations did not apply cleanly")
	}
	results, err = provider.Up(ctx)
	if err != nil || len(results) != 0 {
		t.Fatal("repeated migrations must be a no-op")
	}
	runtimeConfig, err := pgxpool.ParseConfig(runtimeURL)
	if err != nil {
		t.Fatal("invalid test runtime configuration")
	}
	runtimeConfig.MaxConns = 4
	runtimeConfig.ConnConfig.ConnectTimeout = 3 * time.Second
	runtimeConfig.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, runtimeConfig)
	if err != nil {
		t.Fatal("could not initialize test runtime pool")
	}
	defer pool.Close()
	queries := identity.New(pool)
	hash, err := argon2id.CreateHash(rand.Text(), argon2id.DefaultParams)
	if err != nil {
		t.Fatal("could not hash test password")
	}

	t.Run("constraints and revocation queries", func(t *testing.T) {
		user, err := queries.CreateUser(ctx, identity.CreateUserParams{Username: "fixture_user", PasswordHash: hash, Role: "employee"})
		if err != nil || !user.ID.Valid || user.AuthVersion != 1 || user.IsBlocked {
			t.Fatal("invalid new-user defaults")
		}
		found, err := queries.GetUserByID(ctx, user.ID)
		if err != nil || found.Username != user.Username {
			t.Fatal("user UUID lookup failed")
		}
		user, err = queries.SetUserPassword(ctx, identity.SetUserPasswordParams{ID: user.ID, PasswordHash: hash})
		if err != nil || user.AuthVersion != 2 {
			t.Fatal("password change must increment auth_version")
		}
		user, err = queries.SetUserBlocked(ctx, identity.SetUserBlockedParams{ID: user.ID, IsBlocked: true})
		if err != nil || !user.IsBlocked || user.AuthVersion != 3 {
			t.Fatal("blocking must increment auth_version")
		}
		user, err = queries.SetUserBlocked(ctx, identity.SetUserBlockedParams{ID: user.ID, IsBlocked: false})
		if err != nil || user.IsBlocked || user.AuthVersion != 4 {
			t.Fatal("unblocking must not restore old session versions")
		}
		user, err = queries.SetUserRole(ctx, identity.SetUserRoleParams{ID: user.ID, Role: "analyst"})
		if err != nil || user.Role != "analyst" || user.AuthVersion != 5 {
			t.Fatal("role change must increment auth_version")
		}
		if user.UpdatedAt.Time.Before(user.CreatedAt.Time) {
			t.Fatal("invalid user timestamps")
		}
		for _, test := range []struct {
			username, passwordHash, role, code string
		}{
			{"fixture_user", hash, "employee", "23505"},
			{"UPPERCASE", hash, "employee", "23514"},
			{"ab", hash, "employee", "23514"},
			{strings.Repeat("a", 65), hash, "employee", "23514"},
			{"invalid_role", hash, "superuser", "23514"},
			{"invalid_hash", "plaintext", "employee", "23514"},
		} {
			_, err := queries.CreateUser(ctx, identity.CreateUserParams{Username: test.username, PasswordHash: test.passwordHash, Role: test.role})
			assertSQLState(t, err, test.code)
		}
		_, err = pool.Exec(ctx, "UPDATE users SET auth_version = 0 WHERE username = 'fixture_user'")
		assertSQLState(t, err, "23514")
	})

	t.Run("runtime cannot perform DDL or erase users", func(t *testing.T) {
		for _, statement := range []string{
			"CREATE TABLE forbidden_table (id int)",
			"DELETE FROM users WHERE username = 'fixture_user'",
			"SELECT * FROM goose_db_version",
		} {
			_, err := pool.Exec(ctx, statement)
			assertSQLState(t, err, "42501")
		}
	})

	t.Run("SCS session persistence", func(t *testing.T) {
		store := pgxstore.NewWithCleanupInterval(pool, 0)
		manager := scs.New()
		manager.Store = store
		manager.Cookie.Secure = false // httptest uses local HTTP only.
		handler := manager.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/put" {
				manager.Put(r.Context(), "user_id", "fixture-user-id")
				return
			}
			if manager.GetString(r.Context(), "user_id") != "fixture-user-id" {
				t.Error("session was not restored from PostgreSQL")
			}
		}))
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/put", nil))
		cookies := recorder.Result().Cookies()
		if len(cookies) == 0 {
			t.Fatal("SCS did not create a session cookie")
		}
		request := httptest.NewRequest(http.MethodGet, "/read", nil)
		for _, cookie := range cookies {
			request.AddCookie(cookie)
		}
		handler.ServeHTTP(httptest.NewRecorder(), request)
		if err := store.CommitCtx(ctx, "expired-test-token", []byte("payload"), time.Now().Add(-time.Minute)); err != nil {
			t.Fatal("could not persist expired session fixture")
		}
		if _, found, err := store.FindCtx(ctx, "expired-test-token"); err != nil || found {
			t.Fatal("expired sessions must not be returned")
		}
		if err := store.DeleteCtx(ctx, "expired-test-token"); err != nil {
			t.Fatal("session deletion failed")
		}
	})

	t.Run("demo seed is repeatable and preserves accounts", func(t *testing.T) {
		accounts := []demoseed.Account{
			{Username: "employee_a", Role: "employee", Password: rand.Text()},
			{Username: "employee_b", Role: "employee", Password: rand.Text()},
			{Username: "analyst", Role: "analyst", Password: rand.Text()},
			{Username: "admin", Role: "admin", Password: rand.Text()},
		}
		result, err := demoseed.Seed(ctx, pool, accounts)
		if err != nil || result.Created != 4 || result.Existing != 0 {
			t.Fatal("first demo seed did not create four accounts")
		}
		original, err := queries.GetUserByUsername(ctx, "employee_a")
		if err != nil {
			t.Fatal("could not read seeded employee")
		}
		match, err := argon2id.ComparePasswordAndHash(accounts[0].Password, original.PasswordHash)
		if err != nil || !match || bytes.Contains([]byte(original.PasswordHash), []byte(accounts[0].Password)) {
			t.Fatal("seed must store a verifiable Argon2id hash, not plaintext")
		}
		blocked, err := queries.SetUserBlocked(ctx, identity.SetUserBlockedParams{ID: original.ID, IsBlocked: true})
		if err != nil {
			t.Fatal("could not block fixture account")
		}
		accounts[0].Password = rand.Text()
		result, err = demoseed.Seed(ctx, pool, accounts)
		if err != nil || result.Created != 0 || result.Existing != 4 {
			t.Fatal("repeat seed must not create or reset existing accounts")
		}
		preserved, err := queries.GetUserByUsername(ctx, "employee_a")
		if err != nil || preserved.PasswordHash != original.PasswordHash || !preserved.IsBlocked || preserved.AuthVersion != blocked.AuthVersion {
			t.Fatal("repeat seed changed the existing account")
		}
	})

	t.Run("demo seed rolls back on conflicting role", func(t *testing.T) {
		_, err := queries.CreateUser(ctx, identity.CreateUserParams{Username: "rollback_b", PasswordHash: hash, Role: "employee"})
		if err != nil {
			t.Fatal("could not create role-conflict fixture")
		}
		_, err = demoseed.Seed(ctx, pool, []demoseed.Account{
			{Username: "rollback_a", Role: "employee", Password: rand.Text()},
			{Username: "rollback_b", Role: "admin", Password: rand.Text()},
		})
		if err == nil {
			t.Fatal("seed accepted an existing account with a different role")
		}
		if _, err := queries.GetUserByUsername(ctx, "rollback_a"); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatal("failed seed left a partial account")
		}
	})

	t.Run("concurrent demo seeds do not duplicate accounts", func(t *testing.T) {
		accounts := []demoseed.Account{{Username: "concurrent_user", Role: "employee", Password: rand.Text()}}
		var group sync.WaitGroup
		results := make(chan demoseed.Result, 2)
		for range 2 {
			group.Go(func() {
				result, err := demoseed.Seed(ctx, pool, accounts)
				if err != nil {
					t.Error("concurrent seed failed")
				}
				results <- result
			})
		}
		group.Wait()
		first, second := <-results, <-results
		if first.Created+second.Created != 1 || first.Existing+second.Existing != 1 {
			t.Fatal("concurrent seed did not preserve uniqueness")
		}
	})

	t.Run("down and reapply in isolated schema", func(t *testing.T) {
		if _, err := provider.DownTo(ctx, 0); err != nil {
			t.Fatal("could not roll back identity migrations")
		}
		results, err := provider.Up(ctx)
		if err != nil || len(results) != 3 {
			t.Fatal("could not reapply identity migrations")
		}
	})
}

func assertSQLState(t *testing.T, err error, code string) {
	t.Helper()
	var pgError *pgconn.PgError
	if !errors.As(err, &pgError) || pgError.Code != code {
		t.Fatalf("expected PostgreSQL SQLSTATE %s", code)
	}
}
