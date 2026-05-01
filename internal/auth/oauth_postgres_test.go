package auth

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"ux-anomalies-api/internal/config"
)

func TestPostgresOAuthRepo_IntegrationScenarios(t *testing.T) {
	dbURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if dbURL == "" {
		dbURL = strings.TrimSpace(os.Getenv("DATABASE_URL"))
	}
	if dbURL == "" || strings.Contains(dbURL, "replace-me") {
		t.Skip("set TEST_DATABASE_URL (or DATABASE_URL) to run postgres auth integration tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect db: %v", err)
	}
	defer pool.Close()

	repo := NewPostgresOAuthRepo(pool)
	svc := NewOAuthService(repo, NewTokenManager(config.Config{
		JWTIssuer: "uxanomalies-api", JWTAudience: "uxanomalies-bff", JWTSigningKey: "test-key",
	}))

	t.Run("existing provider account login", func(t *testing.T) {
		resetAuthSchema(t, ctx, pool)
		seedUserAndOAuth(t, ctx, pool, "existing@example.com", "existing@example.com", "google", "g-111")
		u, _, err := svc.OAuthUpsert(ctx, OAuthUpsertInput{
			Provider:       ProviderGoogle,
			ProviderUserID: "g-111",
			Email:          "existing@example.com",
			EmailVerified:  true,
			AvatarURL:      "https://cdn.example.com/new-avatar.png",
		})
		if err != nil {
			t.Fatal(err)
		}
		if u.Email != "existing@example.com" {
			t.Fatalf("unexpected user returned: %+v", u)
		}
		if u.AvatarURL != "https://cdn.example.com/new-avatar.png" {
			t.Fatalf("expected refreshed avatar_url, got %q", u.AvatarURL)
		}
	})

	t.Run("existing user by email gets provider linked", func(t *testing.T) {
		resetAuthSchema(t, ctx, pool)
		userID := seedUserOnly(t, ctx, pool, "linkme@example.com", "linkme@example.com")
		u, _, err := svc.OAuthUpsert(ctx, OAuthUpsertInput{
			Provider:       ProviderGitHub,
			ProviderUserID: "gh-222",
			Email:          "LINKME@example.com",
			EmailVerified:  true,
			AvatarURL:      "https://cdn.example.com/linked-avatar.png",
		})
		if err != nil {
			t.Fatal(err)
		}
		if u.UserID.String() != userID {
			t.Fatalf("expected linked user_id=%s got=%s", userID, u.UserID.String())
		}
		if u.AvatarURL != "https://cdn.example.com/linked-avatar.png" {
			t.Fatalf("expected updated avatar_url, got %q", u.AvatarURL)
		}
		assertCount(t, ctx, pool, "SELECT COUNT(*) FROM oauth_accounts WHERE provider='github' AND provider_user_id='gh-222'", 1)
	})

	t.Run("empty incoming avatar does not erase existing avatar", func(t *testing.T) {
		resetAuthSchema(t, ctx, pool)
		seedUserAndOAuthWithAvatar(t, ctx, pool, "keep@example.com", "keep@example.com", "google", "g-keep", "https://cdn.example.com/original.png")
		u, _, err := svc.OAuthUpsert(ctx, OAuthUpsertInput{
			Provider:       ProviderGoogle,
			ProviderUserID: "g-keep",
			Email:          "keep@example.com",
			EmailVerified:  true,
			AvatarURL:      "   ",
		})
		if err != nil {
			t.Fatal(err)
		}
		if u.AvatarURL != "https://cdn.example.com/original.png" {
			t.Fatalf("expected avatar preserved, got %q", u.AvatarURL)
		}
	})

	t.Run("brand new user creation", func(t *testing.T) {
		resetAuthSchema(t, ctx, pool)
		u, _, err := svc.OAuthUpsert(ctx, OAuthUpsertInput{
			Provider:       ProviderGoogle,
			ProviderUserID: "g-333",
			Email:          "newuser@example.com",
			EmailVerified:  true,
		})
		if err != nil {
			t.Fatal(err)
		}
		if u.Nickname != "newuser@example.com" {
			t.Fatalf("unexpected nickname: %s", u.Nickname)
		}
		assertCount(t, ctx, pool, "SELECT COUNT(*) FROM users", 1)
		assertCount(t, ctx, pool, "SELECT COUNT(*) FROM oauth_accounts", 1)
	})

	t.Run("duplicate/race-safe uniqueness behavior", func(t *testing.T) {
		resetAuthSchema(t, ctx, pool)
		const workers = 8
		var wg sync.WaitGroup
		errs := make(chan error, workers)
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _, err := svc.OAuthUpsert(context.Background(), OAuthUpsertInput{
					Provider:       ProviderGoogle,
					ProviderUserID: "g-race",
					Email:          "race@example.com",
					EmailVerified:  true,
				})
				errs <- err
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatalf("unexpected race error: %v", err)
			}
		}
		assertCount(t, ctx, pool, "SELECT COUNT(*) FROM users", 1)
		assertCount(t, ctx, pool, "SELECT COUNT(*) FROM oauth_accounts", 1)
	})
}

func resetAuthSchema(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		DROP TABLE IF EXISTS anomaly_comments;
		DROP TABLE IF EXISTS anomaly_likes;
		DROP TABLE IF EXISTS anomaly_images;
		DROP TABLE IF EXISTS anomalies;
		DROP TABLE IF EXISTS oauth_accounts;
		DROP TABLE IF EXISTS users;

		CREATE EXTENSION IF NOT EXISTS pgcrypto;

		CREATE TABLE users (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			email TEXT NOT NULL,
			email_normalized TEXT NOT NULL UNIQUE,
			nickname TEXT NOT NULL,
			nickname_normalized TEXT NOT NULL UNIQUE,
			avatar_url TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);

		CREATE TABLE oauth_accounts (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			provider TEXT NOT NULL CHECK (provider IN ('google','github')),
			provider_user_id TEXT NOT NULL,
			provider_email TEXT,
			provider_email_verified BOOLEAN NOT NULL DEFAULT FALSE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			UNIQUE (provider, provider_user_id)
		);
	`)
	if err != nil {
		t.Fatalf("reset schema: %v", err)
	}
}

func seedUserOnly(t *testing.T, ctx context.Context, pool *pgxpool.Pool, email, nickname string) string {
	t.Helper()
	var id string
	err := pool.QueryRow(ctx, `
		INSERT INTO users (email, email_normalized, nickname, nickname_normalized)
		VALUES ($1, lower($1), $2, lower($2))
		RETURNING id
	`, email, nickname).Scan(&id)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return id
}

func seedUserAndOAuth(t *testing.T, ctx context.Context, pool *pgxpool.Pool, email, nickname, provider, providerID string) {
	t.Helper()
	id := seedUserOnly(t, ctx, pool, email, nickname)
	_, err := pool.Exec(ctx, `
		INSERT INTO oauth_accounts (user_id, provider, provider_user_id, provider_email, provider_email_verified)
		VALUES ($1, $2, $3, $4, TRUE)
	`, id, provider, providerID, email)
	if err != nil {
		t.Fatalf("seed oauth: %v", err)
	}
}

func seedUserAndOAuthWithAvatar(t *testing.T, ctx context.Context, pool *pgxpool.Pool, email, nickname, provider, providerID, avatarURL string) {
	t.Helper()
	var id string
	err := pool.QueryRow(ctx, `
		INSERT INTO users (email, email_normalized, nickname, nickname_normalized, avatar_url)
		VALUES ($1, lower($1), $2, lower($2), $3)
		RETURNING id
	`, email, nickname, avatarURL).Scan(&id)
	if err != nil {
		t.Fatalf("seed user with avatar: %v", err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO oauth_accounts (user_id, provider, provider_user_id, provider_email, provider_email_verified)
		VALUES ($1, $2, $3, $4, TRUE)
	`, id, provider, providerID, email)
	if err != nil {
		t.Fatalf("seed oauth: %v", err)
	}
}

func assertCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, query string, expected int) {
	t.Helper()
	var c int
	if err := pool.QueryRow(ctx, query).Scan(&c); err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	if c != expected {
		t.Fatalf("expected count=%d got=%d for query=%s", expected, c, query)
	}
}
