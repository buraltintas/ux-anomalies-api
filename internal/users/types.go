package users

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type User struct {
	ID        uuid.UUID `json:"id"`
	Email     string    `json:"email,omitempty"`
	Nickname  string    `json:"nickname"`
	AvatarURL string    `json:"avatar_url,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type PublicProfile struct {
	Nickname  string    `json:"nickname"`
	AvatarURL string    `json:"avatar_url,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type SearchUser struct {
	Nickname  string    `json:"nickname"`
	AvatarURL string    `json:"avatar_url,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type Service interface {
	GetByID(ctx context.Context, id uuid.UUID) (User, error)
	UpdateMe(ctx context.Context, userID uuid.UUID, nickname, avatarURL string) (User, error)
	GetPublicByNickname(ctx context.Context, nickname string) (PublicProfile, error)
	Search(ctx context.Context, q, cursor string, limit int) ([]SearchUser, string, error)
}
