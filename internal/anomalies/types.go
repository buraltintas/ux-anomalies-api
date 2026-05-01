package anomalies

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type Category string

const (
	CategoryWeb               Category = "web"
	CategoryMobileApp         Category = "mobile_app"
	CategoryAutomotiveDisplay Category = "automotive_display"
	CategoryDeviceScreen      Category = "device_screen"
	CategoryTV                Category = "tv"
	CategoryGame              Category = "game"
	CategoryOther             Category = "other"
)

type Anomaly struct {
	ID             uuid.UUID `json:"id"`
	Slug           string    `json:"slug"`
	Title          string    `json:"title"`
	AuthorID       uuid.UUID `json:"author_id"`
	Category       Category  `json:"category"`
	Description    string    `json:"description"`
	SourceURL      string    `json:"source_url,omitempty"`
	LikeCount      int       `json:"like_count"`
	CommentCount   int       `json:"comment_count"`
	ViewerHasLiked bool      `json:"viewer_has_liked"`
	Images         []Image   `json:"images"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type Image struct {
	Bucket     string `json:"bucket"`
	ObjectName string `json:"object_name"`
	MimeType   string `json:"mime_type"`
	ByteSize   int64  `json:"byte_size"`
	Position   int    `json:"position"`
	URL        string `json:"url"`
}

type FeedItem struct {
	ID             uuid.UUID `json:"id"`
	Slug           string    `json:"slug"`
	Title          string    `json:"title"`
	Category       Category  `json:"category"`
	Description    string    `json:"description"`
	ThumbnailURL   string    `json:"thumbnail_url"`
	ImageCount     int       `json:"image_count"`
	LikeCount      int       `json:"like_count"`
	CommentCount   int       `json:"comment_count"`
	ViewerHasLiked bool      `json:"viewer_has_liked"`
	CreatedAt      time.Time `json:"created_at"`
	Author         struct {
		ID          uuid.UUID `json:"id"`
		DisplayName string    `json:"display_name"`
		Nickname    string    `json:"nickname"`
		Avatar      string    `json:"avatar_url,omitempty"`
	} `json:"author"`
}

type CreateInput struct {
	Title       string
	Category    Category
	Description string
	SourceURL   string
	Images      []Image
}

type UpdateInput struct {
	Category    Category `json:"category"`
	Description string   `json:"description"`
	SourceURL   string   `json:"source_url"`
}

type FeedFilter struct {
	Q              string
	Category       Category
	AuthorNickname string
}

type SitemapItem struct {
	ID        uuid.UUID `json:"id"`
	Slug      string    `json:"slug"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type Service interface {
	GetFeed(ctx context.Context, filter FeedFilter, cursor string, limit int) ([]FeedItem, string, error)
	ListByAuthorNickname(ctx context.Context, nickname, cursor string, limit int) ([]FeedItem, string, error)
	GetByID(ctx context.Context, id uuid.UUID) (Anomaly, error)
	GetPublicBySlug(ctx context.Context, slug string) (Anomaly, error)
	GetPublicSitemap(ctx context.Context, cursor string, limit int) ([]SitemapItem, string, error)
	Create(ctx context.Context, authorID uuid.UUID, in CreateInput) (Anomaly, error)
	Update(ctx context.Context, authorID, anomalyID uuid.UUID, in UpdateInput) (Anomaly, error)
	Like(ctx context.Context, userID, anomalyID uuid.UUID) error
	Unlike(ctx context.Context, userID, anomalyID uuid.UUID) error
	ViewerHasLiked(ctx context.Context, userID, anomalyID uuid.UUID) (bool, error)
	ViewerHasLikedMap(ctx context.Context, userID uuid.UUID, anomalyIDs []uuid.UUID) (map[uuid.UUID]bool, error)
}

func IsValidCategory(v Category) bool {
	switch v {
	case CategoryWeb, CategoryMobileApp, CategoryAutomotiveDisplay, CategoryDeviceScreen, CategoryTV, CategoryGame, CategoryOther:
		return true
	default:
		return false
	}
}
