package http

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"ux-anomalies-api/internal/anomalies"
)

func TestPostgresServices_Integration(t *testing.T) {
	dbURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if dbURL == "" {
		dbURL = strings.TrimSpace(os.Getenv("DATABASE_URL"))
	}
	if dbURL == "" || strings.Contains(dbURL, "replace-me") {
		t.Skip("set TEST_DATABASE_URL or DATABASE_URL for postgres integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	resetAll(t, ctx, pool)
	svc := NewPostgresServices(pool)

	user1 := seedUser(t, ctx, pool, "one@example.com", "one")
	user2 := seedUser(t, ctx, pool, "two@example.com", "two")

	t.Run("profile read/update", func(t *testing.T) {
		u, err := svc.Users.GetByID(ctx, user1)
		if err != nil || u.Email != "one@example.com" {
			t.Fatalf("get me failed: %v", err)
		}
		updated, err := svc.Users.UpdateMe(ctx, user1, "one-new", "https://example.com/a.png")
		if err != nil || updated.Nickname != "one-new" {
			t.Fatalf("update me failed: %v", err)
		}
	})

	var created anomalies.Anomaly
	t.Run("anomaly create with image metadata + detail read", func(t *testing.T) {
		a, err := svc.Anomalies.Create(ctx, user1, anomalies.CreateInput{
			Title:       "Duplicate Title",
			Category:    anomalies.CategoryWeb,
			Description: "desc",
			SourceURL:   "https://example.com",
			Images: []anomalies.Image{
				{Bucket: "b", ObjectName: "o1", MimeType: "image/png", ByteSize: 10, Position: 1},
				{Bucket: "b", ObjectName: "o2", MimeType: "image/png", ByteSize: 11, Position: 2},
			},
		})
		if err != nil {
			t.Fatalf("create anomaly failed: %v", err)
		}
		created = a
		if created.Slug == "" {
			t.Fatal("expected slug to be generated")
		}
		got, err := svc.Anomalies.GetByID(ctx, a.ID)
		if err != nil || len(got.Images) != 2 {
			t.Fatalf("detail read failed: %v", err)
		}
	})

	t.Run("slug uniqueness", func(t *testing.T) {
		a2, err := svc.Anomalies.Create(ctx, user1, anomalies.CreateInput{
			Title:       "Duplicate Title",
			Category:    anomalies.CategoryWeb,
			Description: "desc2",
			Images:      []anomalies.Image{{Bucket: "b", ObjectName: "u2", MimeType: "image/png", ByteSize: 1, Position: 1}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if a2.Slug == created.Slug {
			t.Fatalf("expected unique slug, got same %s", a2.Slug)
		}
	})

	t.Run("feed pagination", func(t *testing.T) {
		for i := 0; i < 3; i++ {
			_, err := svc.Anomalies.Create(ctx, user1, anomalies.CreateInput{
				Title: "X", Category: anomalies.CategoryWeb, Description: "x", Images: []anomalies.Image{{Bucket: "b", ObjectName: uuid.NewString(), MimeType: "image/png", ByteSize: 1, Position: 1}},
			})
			if err != nil {
				t.Fatal(err)
			}
		}
		p1, next, err := svc.Anomalies.GetFeed(ctx, anomalies.FeedFilter{}, "", 2)
		if err != nil || len(p1) != 2 || next == "" {
			t.Fatalf("feed page1 failed: len=%d next=%q err=%v", len(p1), next, err)
		}
		p2, _, err := svc.Anomalies.GetFeed(ctx, anomalies.FeedFilter{}, next, 2)
		if err != nil || len(p2) == 0 {
			t.Fatalf("feed page2 failed: %v", err)
		}
	})

	t.Run("like/unlike idempotency with counters", func(t *testing.T) {
		if err := svc.Anomalies.Like(ctx, user2, created.ID); err != nil {
			t.Fatal(err)
		}
		if err := svc.Anomalies.Like(ctx, user2, created.ID); err != nil {
			t.Fatal(err)
		}
		a, _ := svc.Anomalies.GetByID(ctx, created.ID)
		if a.LikeCount != 1 {
			t.Fatalf("expected like_count=1 got=%d", a.LikeCount)
		}
		if err := svc.Anomalies.Unlike(ctx, user2, created.ID); err != nil {
			t.Fatal(err)
		}
		if err := svc.Anomalies.Unlike(ctx, user2, created.ID); err != nil {
			t.Fatal(err)
		}
		a, _ = svc.Anomalies.GetByID(ctx, created.ID)
		if a.LikeCount != 0 {
			t.Fatalf("expected like_count=0 got=%d", a.LikeCount)
		}
	})

	t.Run("comment create and comment_count", func(t *testing.T) {
		c, err := svc.Comments.Create(ctx, created.ID, user2, "hello")
		if err != nil {
			t.Fatal(err)
		}
		if c.Author.ID != user2 || c.Author.Nickname == "" {
			t.Fatalf("expected author object in create response, got %+v", c.Author)
		}
		items, _, err := svc.Comments.ListByAnomaly(ctx, created.ID, "", 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) == 0 || items[0].Author.ID == uuid.Nil || items[0].Author.Nickname == "" {
			t.Fatalf("expected author object in list response, got %+v", items)
		}
		a, _ := svc.Anomalies.GetByID(ctx, created.ID)
		if a.CommentCount != 1 {
			t.Fatalf("expected comment_count=1 got=%d", a.CommentCount)
		}
	})

	t.Run("email-like nickname is sanitized for comments", func(t *testing.T) {
		emailNickUser := seedUser(t, ctx, pool, "mailnick@example.com", "mailnick@example.com")
		c, err := svc.Comments.Create(ctx, created.ID, emailNickUser, "safe")
		if err != nil {
			t.Fatal(err)
		}
		if c.Author.Nickname != "UX Anomalies user" {
			t.Fatalf("expected sanitized nickname, got %q", c.Author.Nickname)
		}
		items, _, err := svc.Comments.ListByAnomaly(ctx, created.ID, "", 20)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, it := range items {
			if it.AuthorID == emailNickUser {
				found = true
				if it.Author.Nickname != "UX Anomalies user" {
					t.Fatalf("expected sanitized nickname in list, got %q", it.Author.Nickname)
				}
			}
		}
		if !found {
			t.Fatal("expected comment from email-like nickname user in list")
		}
	})

	t.Run("ownership checks for patch", func(t *testing.T) {
		_, err := svc.Anomalies.Update(ctx, user2, created.ID, anomalies.UpdateInput{
			Category: anomalies.CategoryWeb, Description: "hack", SourceURL: "",
		})
		if err == nil {
			t.Fatal("expected ownership failure")
		}
	})

	t.Run("public profile anomaly listing", func(t *testing.T) {
		items, _, err := svc.Anomalies.ListByAuthorNickname(ctx, "one-new", "", 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) == 0 {
			t.Fatal("expected anomalies for profile user")
		}
	})

	t.Run("public by slug and sitemap visibility", func(t *testing.T) {
		got, err := svc.Anomalies.GetPublicBySlug(ctx, created.Slug)
		if err != nil || got.ID != created.ID {
			t.Fatalf("get public by slug failed: %v", err)
		}
		if _, err := pool.Exec(ctx, `UPDATE anomalies SET is_public=FALSE WHERE id=$1`, created.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Anomalies.GetPublicBySlug(ctx, created.Slug); err == nil {
			t.Fatal("expected non-public anomaly to be hidden")
		}
		items, _, err := svc.Anomalies.GetPublicSitemap(ctx, "", 100)
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range items {
			if it.ID == created.ID {
				t.Fatal("non-public anomaly should not appear in sitemap")
			}
		}
	})
}

func resetAll(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
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
		CREATE TABLE anomalies (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			slug TEXT NOT NULL UNIQUE,
			title TEXT NOT NULL,
			author_id UUID NOT NULL REFERENCES users(id),
			category TEXT NOT NULL,
			description TEXT NOT NULL,
			source_url TEXT,
			like_count INTEGER NOT NULL DEFAULT 0,
			comment_count INTEGER NOT NULL DEFAULT 0,
			is_public BOOLEAN NOT NULL DEFAULT TRUE,
			is_published BOOLEAN NOT NULL DEFAULT TRUE,
			is_approved BOOLEAN NOT NULL DEFAULT TRUE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
		CREATE TABLE anomaly_images (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			anomaly_id UUID NOT NULL REFERENCES anomalies(id) ON DELETE CASCADE,
			bucket TEXT NOT NULL,
			object_name TEXT NOT NULL,
			mime_type TEXT NOT NULL,
			byte_size INTEGER NOT NULL,
			position SMALLINT NOT NULL CHECK (position BETWEEN 1 AND 3),
			UNIQUE (anomaly_id, position),
			UNIQUE (bucket, object_name)
		);
		CREATE TABLE anomaly_likes (
			anomaly_id UUID NOT NULL REFERENCES anomalies(id) ON DELETE CASCADE,
			user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			PRIMARY KEY (anomaly_id, user_id)
		);
		CREATE TABLE anomaly_comments (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			anomaly_id UUID NOT NULL REFERENCES anomalies(id) ON DELETE CASCADE,
			author_id UUID NOT NULL REFERENCES users(id),
			text TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
	`)
	if err != nil {
		t.Fatalf("reset schema: %v", err)
	}
}

func seedUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool, email, nickname string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO users (email,email_normalized,nickname,nickname_normalized) VALUES ($1,lower($1),$2,lower($2)) RETURNING id`, email, nickname).Scan(&id); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return id
}
