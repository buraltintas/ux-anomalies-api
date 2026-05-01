package auth

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	"ux-anomalies-api/internal/validation"
)

type Provider string

const (
	ProviderGoogle Provider = "google"
	ProviderGitHub Provider = "github"
)

type OAuthUpsertInput struct {
	Provider          Provider `json:"provider"`
	ProviderUserID    string   `json:"provider_user_id"`
	Email             string   `json:"email"`
	EmailVerified     bool     `json:"email_verified"`
	Name              string   `json:"name"`
	AvatarURL         string   `json:"avatar_url"`
	RequireVerifiedGH bool     `json:"-"`
}

type OAuthUser struct {
	UserID    uuid.UUID `json:"user_id"`
	Email     string    `json:"email"`
	Nickname  string    `json:"nickname"`
	AvatarURL string    `json:"avatar_url,omitempty"`
}

type OAuthRepository interface {
	UpsertOAuthUser(ctx context.Context, in OAuthUpsertInput) (OAuthUser, error)
}

type OAuthService struct {
	repo   OAuthRepository
	tokens *TokenManager
}

func NewOAuthService(repo OAuthRepository, tokens *TokenManager) *OAuthService {
	return &OAuthService{repo: repo, tokens: tokens}
}

func (s *OAuthService) OAuthUpsert(ctx context.Context, in OAuthUpsertInput) (OAuthUser, string, error) {
	if in.Provider != ProviderGoogle && in.Provider != ProviderGitHub {
		return OAuthUser{}, "", errors.New("unsupported provider")
	}
	if strings.TrimSpace(in.ProviderUserID) == "" {
		return OAuthUser{}, "", errors.New("provider_user_id is required")
	}
	if in.Provider == ProviderGitHub && !in.EmailVerified {
		return OAuthUser{}, "", errors.New("github verified email is required")
	}

	email := validation.NormalizeEmail(in.Email)
	if email == "" || !in.EmailVerified {
		return OAuthUser{}, "", errors.New("verified email is required")
	}
	in.Email = email

	u, err := s.repo.UpsertOAuthUser(ctx, in)
	if err != nil {
		return OAuthUser{}, "", err
	}
	tok, err := s.tokens.Mint(u.UserID)
	return u, tok, err
}
