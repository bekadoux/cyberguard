// Package demoseed creates explicit development fixtures, never production users.
package demoseed

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/alexedwards/argon2id"
	"github.com/bekadoux/cyberguard/backend/go/internal/db/identity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Account struct {
	Username string
	Role     string
	Password string
}

type Result struct {
	Created  int
	Existing int
}

func LoadAccounts() ([]Account, error) {
	return loadAccounts(os.LookupEnv)
}

func loadAccounts(lookup func(string) (string, bool)) ([]Account, error) {
	specs := []struct {
		username, role, variable string
	}{
		{"employee_a", "employee", "DEV_EMPLOYEE_A_PASSWORD"},
		{"employee_b", "employee", "DEV_EMPLOYEE_B_PASSWORD"},
		{"analyst", "analyst", "DEV_ANALYST_PASSWORD"},
		{"admin", "admin", "DEV_ADMIN_PASSWORD"},
	}
	accounts := make([]Account, 0, len(specs))
	seen := make(map[string]bool, len(specs))
	for _, spec := range specs {
		password, _ := lookup(spec.variable)
		if len(password) < 12 || len(password) > 128 || strings.HasPrefix(password, "REPLACE_") {
			return nil, fmt.Errorf("%s must be a non-placeholder password of 12 to 128 bytes", spec.variable)
		}
		if seen[password] {
			return nil, errors.New("demo accounts must use distinct passwords")
		}
		seen[password] = true
		accounts = append(accounts, Account{Username: spec.username, Role: spec.role, Password: password})
	}
	return accounts, nil
}

// Seed commits all accounts together. Existing accounts are left unchanged,
// including their password, blocked state and session-revocation version.
func Seed(ctx context.Context, pool *pgxpool.Pool, accounts []Account) (Result, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return Result{}, errors.New("could not start demo-account transaction")
	}
	defer func() {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()
	queries := identity.New(tx)
	var result Result
	for _, account := range accounts {
		user, err := queries.GetUserByUsername(ctx, account.Username)
		if errors.Is(err, pgx.ErrNoRows) {
			hash, hashErr := argon2id.CreateHash(account.Password, argon2id.DefaultParams)
			if hashErr != nil {
				return Result{}, errors.New("could not hash demo password")
			}
			user, err = queries.CreateDemoUser(ctx, identity.CreateDemoUserParams{
				Username: account.Username, PasswordHash: hash, Role: account.Role,
			})
			if err == nil {
				result.Created++
				continue
			}
			if errors.Is(err, pgx.ErrNoRows) {
				// Another seed may have committed the same username concurrently.
				user, err = queries.GetUserByUsername(ctx, account.Username)
			}
		}
		if err != nil {
			return Result{}, errors.New("could not read or create demo account")
		}
		if user.Role != account.Role {
			return Result{}, fmt.Errorf("demo account %s already exists with another role", account.Username)
		}
		result.Existing++
	}
	if err := tx.Commit(ctx); err != nil {
		return Result{}, errors.New("could not commit demo accounts")
	}
	return result, nil
}
