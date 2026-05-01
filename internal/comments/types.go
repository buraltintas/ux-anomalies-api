package comments

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type Comment struct {
	ID        uuid.UUID `json:"id"`
	AnomalyID uuid.UUID `json:"anomaly_id"`
	AuthorID  uuid.UUID `json:"author_id"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
	Author    struct {
		ID          uuid.UUID `json:"id"`
		DisplayName string    `json:"display_name"`
		Nickname    string    `json:"nickname"`
		Avatar      string    `json:"avatar_url,omitempty"`
	} `json:"author"`
}

type Service interface {
	ListByAnomaly(ctx context.Context, anomalyID uuid.UUID, cursor string, limit int) ([]Comment, string, error)
	Create(ctx context.Context, anomalyID, authorID uuid.UUID, text string) (Comment, error)
}
