package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"ux-anomalies-api/internal/config"
)

type stubRepo struct {
	user OAuthUser
	err  error
}

func (s stubRepo) UpsertOAuthUser(_ context.Context, _ OAuthUpsertInput) (OAuthUser, error) {
	if s.err != nil {
		return OAuthUser{}, s.err
	}
	return s.user, nil
}

func TestOAuthUpsert_RejectsUnverifiedGitHub(t *testing.T) {
	svc := NewOAuthService(stubRepo{}, NewTokenManager(config.Config{JWTIssuer: "x", JWTAudience: "y", JWTSigningKey: "z"}))
	_, _, err := svc.OAuthUpsert(context.Background(), OAuthUpsertInput{
		Provider:       ProviderGitHub,
		ProviderUserID: "gh-1",
		Email:          "user@example.com",
		EmailVerified:  false,
	})
	if err == nil {
		t.Fatal("expected github verified email rejection")
	}
}

func TestOAuthUpsert_RejectsUnverifiedEmail(t *testing.T) {
	svc := NewOAuthService(stubRepo{}, NewTokenManager(config.Config{JWTIssuer: "x", JWTAudience: "y", JWTSigningKey: "z"}))
	_, _, err := svc.OAuthUpsert(context.Background(), OAuthUpsertInput{
		Provider:       ProviderGoogle,
		ProviderUserID: "g-1",
		Email:          "user@example.com",
		EmailVerified:  false,
	})
	if err == nil {
		t.Fatal("expected verified email rejection")
	}
}

func TestOAuthUpsert_MintsTokenWhenRepoSucceeds(t *testing.T) {
	uid := uuid.New()
	svc := NewOAuthService(stubRepo{user: OAuthUser{UserID: uid, Email: "x@example.com", Nickname: "x@example.com"}}, NewTokenManager(config.Config{
		JWTIssuer: "x", JWTAudience: "y", JWTSigningKey: "z",
	}))
	_, tok, err := svc.OAuthUpsert(context.Background(), OAuthUpsertInput{
		Provider:       ProviderGoogle,
		ProviderUserID: "g-1",
		Email:          "x@example.com",
		EmailVerified:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if tok == "" {
		t.Fatal("expected token")
	}
}

func TestOAuthUpsert_PropagatesRepoError(t *testing.T) {
	svc := NewOAuthService(stubRepo{err: errors.New("boom")}, NewTokenManager(config.Config{
		JWTIssuer: "x", JWTAudience: "y", JWTSigningKey: "z",
	}))
	_, _, err := svc.OAuthUpsert(context.Background(), OAuthUpsertInput{
		Provider:       ProviderGoogle,
		ProviderUserID: "g-1",
		Email:          "x@example.com",
		EmailVerified:  true,
	})
	if err == nil {
		t.Fatal("expected repo error")
	}
}
