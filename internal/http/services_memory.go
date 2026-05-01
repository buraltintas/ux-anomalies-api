package http

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"ux-anomalies-api/internal/anomalies"
	"ux-anomalies-api/internal/comments"
	"ux-anomalies-api/internal/users"
	"ux-anomalies-api/internal/validation"
)

type Services struct {
	Users     users.Service
	Anomalies anomalies.Service
	Comments  comments.Service
}

func NewInMemoryServices() Services {
	state := &memoryState{
		usersByID:       map[uuid.UUID]users.User{},
		usersByNickNorm: map[string]uuid.UUID{},
		anomalies:       map[uuid.UUID]anomalies.Anomaly{},
		likes:           map[uuid.UUID]map[uuid.UUID]struct{}{},
		comments:        map[uuid.UUID][]comments.Comment{},
	}
	return Services{
		Users:     &memoryUsers{state: state},
		Anomalies: &memoryAnomalies{state: state},
		Comments:  &memoryComments{state: state},
	}
}

type memoryState struct {
	mu              sync.RWMutex
	usersByID       map[uuid.UUID]users.User
	usersByNickNorm map[string]uuid.UUID
	anomalies       map[uuid.UUID]anomalies.Anomaly
	likes           map[uuid.UUID]map[uuid.UUID]struct{}
	comments        map[uuid.UUID][]comments.Comment
}

type memoryUsers struct{ state *memoryState }
type memoryAnomalies struct{ state *memoryState }
type memoryComments struct{ state *memoryState }

func (m *memoryUsers) GetByID(_ context.Context, id uuid.UUID) (users.User, error) {
	m.state.mu.RLock()
	defer m.state.mu.RUnlock()
	u, ok := m.state.usersByID[id]
	if !ok {
		return users.User{}, errors.New("not found")
	}
	return u, nil
}

func (m *memoryUsers) UpdateMe(_ context.Context, userID uuid.UUID, nickname, avatarURL string) (users.User, error) {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	u, ok := m.state.usersByID[userID]
	if !ok {
		return users.User{}, errors.New("not found")
	}
	if nickname != "" {
		if err := validation.ValidateNickname(nickname); err != nil {
			return users.User{}, err
		}
		norm := validation.NormalizeNickname(nickname)
		if owner, exists := m.state.usersByNickNorm[norm]; exists && owner != userID {
			return users.User{}, errors.New("nickname already taken")
		}
		delete(m.state.usersByNickNorm, validation.NormalizeNickname(u.Nickname))
		m.state.usersByNickNorm[norm] = userID
		u.Nickname = nickname
	}
	u.AvatarURL = avatarURL
	m.state.usersByID[userID] = u
	return u, nil
}

func (m *memoryUsers) GetPublicByNickname(_ context.Context, nickname string) (users.PublicProfile, error) {
	m.state.mu.RLock()
	defer m.state.mu.RUnlock()
	norm := validation.NormalizeNickname(nickname)
	id, ok := m.state.usersByNickNorm[norm]
	if !ok {
		return users.PublicProfile{}, errors.New("not found")
	}
	u := m.state.usersByID[id]
	return users.PublicProfile{Nickname: u.Nickname, AvatarURL: u.AvatarURL, CreatedAt: u.CreatedAt}, nil
}

func (m *memoryUsers) Search(_ context.Context, q, _ string, limit int) ([]users.SearchUser, string, error) {
	m.state.mu.RLock()
	defer m.state.mu.RUnlock()
	out := []users.SearchUser{}
	q = strings.ToLower(strings.TrimSpace(q))
	for _, u := range m.state.usersByID {
		if q != "" && !strings.Contains(strings.ToLower(u.Nickname), q) {
			continue
		}
		out = append(out, users.SearchUser{Nickname: u.Nickname, AvatarURL: u.AvatarURL, CreatedAt: u.CreatedAt})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, "", nil
}

func (m *memoryAnomalies) GetFeed(_ context.Context, filter anomalies.FeedFilter, _ string, limit int) ([]anomalies.FeedItem, string, error) {
	m.state.mu.RLock()
	defer m.state.mu.RUnlock()
	items := make([]anomalies.FeedItem, 0, len(m.state.anomalies))
	for _, a := range m.state.anomalies {
		if filter.Category != "" && a.Category != filter.Category {
			continue
		}
		if filter.Q != "" && !strings.Contains(strings.ToLower(a.Description), strings.ToLower(filter.Q)) {
			continue
		}
		item := anomalies.FeedItem{ID: a.ID, Slug: a.Slug, Title: a.Title, Category: a.Category, Description: a.Description, ImageCount: len(a.Images), LikeCount: a.LikeCount, CommentCount: a.CommentCount, CreatedAt: a.CreatedAt}
		if len(a.Images) > 0 {
			item.ThumbnailURL = a.Images[0].URL
		}
		if u, ok := m.state.usersByID[a.AuthorID]; ok {
			if filter.AuthorNickname != "" && validation.NormalizeNickname(u.Nickname) != validation.NormalizeNickname(filter.AuthorNickname) {
				continue
			}
			item.Author.ID = u.ID
			item.Author.DisplayName = safeAuthorDisplayName(u.Nickname)
			item.Author.Nickname = item.Author.DisplayName
			item.Author.Avatar = u.AvatarURL
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	if len(items) > limit {
		items = items[:limit]
	}
	return items, "", nil
}

func (m *memoryAnomalies) ListByAuthorNickname(_ context.Context, nickname, _ string, limit int) ([]anomalies.FeedItem, string, error) {
	m.state.mu.RLock()
	defer m.state.mu.RUnlock()
	norm := validation.NormalizeNickname(nickname)
	uid, ok := m.state.usersByNickNorm[norm]
	if !ok {
		return []anomalies.FeedItem{}, "", nil
	}
	items := make([]anomalies.FeedItem, 0)
	for _, a := range m.state.anomalies {
		if a.AuthorID != uid {
			continue
		}
		item := anomalies.FeedItem{ID: a.ID, Category: a.Category, Description: a.Description, ImageCount: len(a.Images), LikeCount: a.LikeCount, CommentCount: a.CommentCount, CreatedAt: a.CreatedAt}
		item.Slug = a.Slug
		item.Title = a.Title
		if len(a.Images) > 0 {
			item.ThumbnailURL = a.Images[0].URL
		}
		if u, ok := m.state.usersByID[a.AuthorID]; ok {
			item.Author.ID = u.ID
			item.Author.DisplayName = safeAuthorDisplayName(u.Nickname)
			item.Author.Nickname = item.Author.DisplayName
			item.Author.Avatar = u.AvatarURL
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	if len(items) > limit {
		items = items[:limit]
	}
	return items, "", nil
}

func (m *memoryAnomalies) GetByID(_ context.Context, id uuid.UUID) (anomalies.Anomaly, error) {
	m.state.mu.RLock()
	defer m.state.mu.RUnlock()
	a, ok := m.state.anomalies[id]
	if !ok {
		return anomalies.Anomaly{}, errors.New("not found")
	}
	return a, nil
}

func (m *memoryAnomalies) Create(_ context.Context, authorID uuid.UUID, in anomalies.CreateInput) (anomalies.Anomaly, error) {
	if strings.TrimSpace(in.Description) == "" {
		return anomalies.Anomaly{}, errors.New("description is required")
	}
	if len(in.Images) < 1 || len(in.Images) > 3 {
		return anomalies.Anomaly{}, errors.New("images must be 1..3")
	}
	if !anomalies.IsValidCategory(in.Category) {
		return anomalies.Anomaly{}, errors.New("invalid category")
	}
	now := time.Now()
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = strings.TrimSpace(in.Description)
	}
	a := anomalies.Anomaly{
		ID:          uuid.New(),
		Slug:        slugify(title),
		Title:       title,
		AuthorID:    authorID,
		Category:    in.Category,
		Description: strings.TrimSpace(in.Description),
		SourceURL:   strings.TrimSpace(in.SourceURL),
		Images:      in.Images,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	m.state.anomalies[a.ID] = a
	return a, nil
}

func (m *memoryAnomalies) GetPublicBySlug(_ context.Context, slug string) (anomalies.Anomaly, error) {
	m.state.mu.RLock()
	defer m.state.mu.RUnlock()
	for _, a := range m.state.anomalies {
		if a.Slug == slug {
			return a, nil
		}
	}
	return anomalies.Anomaly{}, errors.New("not found")
}

func (m *memoryAnomalies) GetPublicSitemap(_ context.Context, _ string, limit int) ([]anomalies.SitemapItem, string, error) {
	m.state.mu.RLock()
	defer m.state.mu.RUnlock()
	out := make([]anomalies.SitemapItem, 0, len(m.state.anomalies))
	for _, a := range m.state.anomalies {
		out = append(out, anomalies.SitemapItem{ID: a.ID, Slug: a.Slug, UpdatedAt: a.UpdatedAt})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, "", nil
}

func slugify(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	v = strings.ReplaceAll(v, "_", "-")
	var b strings.Builder
	lastDash := false
	for _, r := range v {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			lastDash = false
		} else if !lastDash {
			b.WriteRune('-')
			lastDash = true
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		return fmt.Sprintf("anomaly-%d", time.Now().Unix())
	}
	return s
}

func (m *memoryAnomalies) Update(_ context.Context, authorID, anomalyID uuid.UUID, in anomalies.UpdateInput) (anomalies.Anomaly, error) {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	a, ok := m.state.anomalies[anomalyID]
	if !ok {
		return anomalies.Anomaly{}, errors.New("not found")
	}
	if a.AuthorID != authorID {
		return anomalies.Anomaly{}, errors.New("forbidden")
	}
	if !anomalies.IsValidCategory(in.Category) {
		return anomalies.Anomaly{}, errors.New("invalid category")
	}
	if strings.TrimSpace(in.Description) == "" {
		return anomalies.Anomaly{}, errors.New("description is required")
	}
	a.Category = in.Category
	a.Description = strings.TrimSpace(in.Description)
	a.SourceURL = strings.TrimSpace(in.SourceURL)
	a.UpdatedAt = time.Now()
	m.state.anomalies[anomalyID] = a
	return a, nil
}

func (m *memoryAnomalies) Like(_ context.Context, userID, anomalyID uuid.UUID) error {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	a, ok := m.state.anomalies[anomalyID]
	if !ok {
		return errors.New("not found")
	}
	if _, ok := m.state.likes[anomalyID]; !ok {
		m.state.likes[anomalyID] = map[uuid.UUID]struct{}{}
	}
	if _, exists := m.state.likes[anomalyID][userID]; exists {
		return nil
	}
	m.state.likes[anomalyID][userID] = struct{}{}
	a.LikeCount++
	m.state.anomalies[anomalyID] = a
	return nil
}

func (m *memoryAnomalies) Unlike(_ context.Context, userID, anomalyID uuid.UUID) error {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	a, ok := m.state.anomalies[anomalyID]
	if !ok {
		return errors.New("not found")
	}
	if _, ok := m.state.likes[anomalyID][userID]; !ok {
		return nil
	}
	delete(m.state.likes[anomalyID], userID)
	if a.LikeCount > 0 {
		a.LikeCount--
	}
	m.state.anomalies[anomalyID] = a
	return nil
}

func (m *memoryAnomalies) ViewerHasLiked(_ context.Context, userID, anomalyID uuid.UUID) (bool, error) {
	m.state.mu.RLock()
	defer m.state.mu.RUnlock()
	if _, ok := m.state.likes[anomalyID][userID]; ok {
		return true, nil
	}
	return false, nil
}

func (m *memoryAnomalies) ViewerHasLikedMap(_ context.Context, userID uuid.UUID, anomalyIDs []uuid.UUID) (map[uuid.UUID]bool, error) {
	m.state.mu.RLock()
	defer m.state.mu.RUnlock()
	out := make(map[uuid.UUID]bool, len(anomalyIDs))
	for _, id := range anomalyIDs {
		_, liked := m.state.likes[id][userID]
		out[id] = liked
	}
	return out, nil
}

func (m *memoryComments) ListByAnomaly(_ context.Context, anomalyID uuid.UUID, _ string, limit int) ([]comments.Comment, string, error) {
	m.state.mu.RLock()
	defer m.state.mu.RUnlock()
	out := append([]comments.Comment{}, m.state.comments[anomalyID]...)
	for i := range out {
		if u, ok := m.state.usersByID[out[i].AuthorID]; ok {
			out[i].Author.ID = u.ID
			out[i].Author.DisplayName = safeAuthorDisplayName(u.Nickname)
			out[i].Author.Nickname = out[i].Author.DisplayName
			out[i].Author.Avatar = u.AvatarURL
		} else {
			out[i].Author.ID = out[i].AuthorID
			out[i].Author.DisplayName = authorFallbackName
			out[i].Author.Nickname = authorFallbackName
		}
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, "", nil
}

func (m *memoryComments) Create(_ context.Context, anomalyID, authorID uuid.UUID, text string) (comments.Comment, error) {
	if strings.TrimSpace(text) == "" {
		return comments.Comment{}, errors.New("comment text is required")
	}
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	a, ok := m.state.anomalies[anomalyID]
	if !ok {
		return comments.Comment{}, errors.New("anomaly not found")
	}
	c := comments.Comment{ID: uuid.New(), AnomalyID: anomalyID, AuthorID: authorID, Text: strings.TrimSpace(text), CreatedAt: time.Now()}
	if u, ok := m.state.usersByID[authorID]; ok {
		c.Author.ID = u.ID
		c.Author.DisplayName = safeAuthorDisplayName(u.Nickname)
		c.Author.Nickname = c.Author.DisplayName
		c.Author.Avatar = u.AvatarURL
	} else {
		c.Author.ID = authorID
		c.Author.DisplayName = authorFallbackName
		c.Author.Nickname = authorFallbackName
	}
	m.state.comments[anomalyID] = append(m.state.comments[anomalyID], c)
	a.CommentCount++
	m.state.anomalies[anomalyID] = a
	return c, nil
}
