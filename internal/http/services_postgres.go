package http

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"ux-anomalies-api/internal/anomalies"
	"ux-anomalies-api/internal/comments"
	"ux-anomalies-api/internal/users"
	"ux-anomalies-api/internal/validation"
)

func NewPostgresServices(pool *pgxpool.Pool) Services {
	return Services{
		Users:     &pgUsers{pool: pool},
		Anomalies: &pgAnomalies{pool: pool},
		Comments:  &pgComments{pool: pool},
	}
}

type pgUsers struct{ pool *pgxpool.Pool }
type pgAnomalies struct{ pool *pgxpool.Pool }
type pgComments struct{ pool *pgxpool.Pool }

func encodeCursor(ts time.Time, id uuid.UUID) string {
	raw := fmt.Sprintf("%d|%s", ts.UnixNano(), id.String())
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeCursor(cur string) (time.Time, uuid.UUID, error) {
	if strings.TrimSpace(cur) == "" {
		return time.Now().Add(24 * time.Hour), uuid.MustParse("ffffffff-ffff-ffff-ffff-ffffffffffff"), nil
	}
	b, err := base64.RawURLEncoding.DecodeString(cur)
	if err != nil {
		return time.Time{}, uuid.Nil, err
	}
	parts := strings.Split(string(b), "|")
	if len(parts) != 2 {
		return time.Time{}, uuid.Nil, errors.New("bad cursor")
	}
	var nanos int64
	_, err = fmt.Sscanf(parts[0], "%d", &nanos)
	if err != nil {
		return time.Time{}, uuid.Nil, err
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return time.Time{}, uuid.Nil, err
	}
	return time.Unix(0, nanos), id, nil
}

func (p *pgUsers) GetByID(ctx context.Context, id uuid.UUID) (users.User, error) {
	var u users.User
	err := p.pool.QueryRow(ctx, `SELECT id,email,nickname,COALESCE(avatar_url,''),created_at FROM users WHERE id=$1`, id).
		Scan(&u.ID, &u.Email, &u.Nickname, &u.AvatarURL, &u.CreatedAt)
	return u, err
}

func (p *pgUsers) UpdateMe(ctx context.Context, userID uuid.UUID, nickname, avatarURL string) (users.User, error) {
	if nickname != "" {
		_, err := p.pool.Exec(ctx, `UPDATE users SET nickname=$1, nickname_normalized=$2, avatar_url=$3, updated_at=now() WHERE id=$4`,
			nickname, validation.NormalizeNickname(nickname), avatarURL, userID)
		if err != nil {
			return users.User{}, err
		}
	} else {
		_, err := p.pool.Exec(ctx, `UPDATE users SET avatar_url=$1, updated_at=now() WHERE id=$2`, avatarURL, userID)
		if err != nil {
			return users.User{}, err
		}
	}
	return p.GetByID(ctx, userID)
}

func (p *pgUsers) GetPublicByNickname(ctx context.Context, nickname string) (users.PublicProfile, error) {
	var u users.PublicProfile
	err := p.pool.QueryRow(ctx, `SELECT nickname, COALESCE(avatar_url,''), created_at FROM users WHERE nickname_normalized=$1`,
		validation.NormalizeNickname(nickname)).Scan(&u.Nickname, &u.AvatarURL, &u.CreatedAt)
	return u, err
}

func (p *pgUsers) Search(ctx context.Context, q, cursor string, limit int) ([]users.SearchUser, string, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	ts, id, err := decodeCursor(cursor)
	if err != nil {
		return nil, "", err
	}
	q = strings.TrimSpace(q)
	rows, err := p.pool.Query(ctx, `
		SELECT id, nickname, COALESCE(avatar_url,''), created_at
		FROM users
		WHERE ($1 = '' OR nickname ILIKE '%' || $1 || '%')
		  AND (created_at, id) < ($2, $3)
		ORDER BY created_at DESC, id DESC
		LIMIT $4
	`, q, ts, id, limit+1)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	items := []users.SearchUser{}
	ids := []uuid.UUID{}
	var next string
	for rows.Next() {
		var it users.SearchUser
		var uid uuid.UUID
		if err := rows.Scan(&uid, &it.Nickname, &it.AvatarURL, &it.CreatedAt); err != nil {
			return nil, "", err
		}
		items = append(items, it)
		ids = append(ids, uid)
	}
	if len(items) > limit {
		last := items[limit-1]
		next = encodeCursor(last.CreatedAt, ids[limit-1])
		items = items[:limit]
	}
	return items, next, rows.Err()
}

func (p *pgAnomalies) Create(ctx context.Context, authorID uuid.UUID, in anomalies.CreateInput) (anomalies.Anomaly, error) {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return anomalies.Anomaly{}, err
	}
	defer tx.Rollback(ctx)
	var a anomalies.Anomaly
	err = tx.QueryRow(ctx, `
		INSERT INTO anomalies (author_id, category, description, source_url)
		VALUES ($1,$2,$3,$4)
		RETURNING id,author_id,category,description,COALESCE(source_url,''),like_count,comment_count,created_at,updated_at
	`, authorID, in.Category, strings.TrimSpace(in.Description), in.SourceURL).
		Scan(&a.ID, &a.AuthorID, &a.Category, &a.Description, &a.SourceURL, &a.LikeCount, &a.CommentCount, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return anomalies.Anomaly{}, err
	}
	a.Images = make([]anomalies.Image, 0, len(in.Images))
	for _, img := range in.Images {
		_, err := tx.Exec(ctx, `
			INSERT INTO anomaly_images (anomaly_id,bucket,object_name,mime_type,byte_size,position)
			VALUES ($1,$2,$3,$4,$5,$6)
		`, a.ID, img.Bucket, img.ObjectName, img.MimeType, img.ByteSize, img.Position)
		if err != nil {
			return anomalies.Anomaly{}, err
		}
		a.Images = append(a.Images, img)
	}
	if err := tx.Commit(ctx); err != nil {
		return anomalies.Anomaly{}, err
	}
	return a, nil
}

func (p *pgAnomalies) Update(ctx context.Context, authorID, anomalyID uuid.UUID, in anomalies.UpdateInput) (anomalies.Anomaly, error) {
	tag, err := p.pool.Exec(ctx, `
		UPDATE anomalies
		SET category=$1, description=$2, source_url=$3, updated_at=now()
		WHERE id=$4 AND author_id=$5
	`, in.Category, strings.TrimSpace(in.Description), in.SourceURL, anomalyID, authorID)
	if err != nil {
		return anomalies.Anomaly{}, err
	}
	if tag.RowsAffected() == 0 {
		return anomalies.Anomaly{}, errors.New("forbidden")
	}
	return p.GetByID(ctx, anomalyID)
}

func (p *pgAnomalies) GetByID(ctx context.Context, id uuid.UUID) (anomalies.Anomaly, error) {
	var a anomalies.Anomaly
	err := p.pool.QueryRow(ctx, `
		SELECT id,author_id,category,description,COALESCE(source_url,''),like_count,comment_count,created_at,updated_at
		FROM anomalies WHERE id=$1
	`, id).Scan(&a.ID, &a.AuthorID, &a.Category, &a.Description, &a.SourceURL, &a.LikeCount, &a.CommentCount, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return anomalies.Anomaly{}, err
	}
	rows, err := p.pool.Query(ctx, `
		SELECT bucket, object_name, mime_type, byte_size, position
		FROM anomaly_images WHERE anomaly_id=$1 ORDER BY position ASC
	`, id)
	if err != nil {
		return anomalies.Anomaly{}, err
	}
	defer rows.Close()
	a.Images = []anomalies.Image{}
	for rows.Next() {
		var img anomalies.Image
		if err := rows.Scan(&img.Bucket, &img.ObjectName, &img.MimeType, &img.ByteSize, &img.Position); err != nil {
			return anomalies.Anomaly{}, err
		}
		a.Images = append(a.Images, img)
	}
	return a, rows.Err()
}

func (p *pgAnomalies) GetFeed(ctx context.Context, filter anomalies.FeedFilter, cursor string, limit int) ([]anomalies.FeedItem, string, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	ts, id, err := decodeCursor(cursor)
	if err != nil {
		return nil, "", err
	}
	q := strings.TrimSpace(filter.Q)
	authorNorm := validation.NormalizeNickname(filter.AuthorNickname)
	category := string(filter.Category)
	if filter.Category != "" && !anomalies.IsValidCategory(filter.Category) {
		return nil, "", errors.New("invalid category")
	}
	rows, err := p.pool.Query(ctx, `
		SELECT a.id,a.category,a.description,a.like_count,a.comment_count,a.created_at,u.id,u.nickname,COALESCE(u.avatar_url,''),
		       COALESCE(img.bucket,''),COALESCE(img.object_name,'')
		FROM anomalies a
		JOIN users u ON u.id=a.author_id
		LEFT JOIN LATERAL (
			SELECT bucket,object_name FROM anomaly_images ai WHERE ai.anomaly_id=a.id ORDER BY position ASC LIMIT 1
		) img ON TRUE
		WHERE (a.created_at, a.id) < ($1,$2)
		  AND ($3 = '' OR a.description ILIKE '%' || $3 || '%')
		  AND ($4 = '' OR a.category = $4)
		  AND ($5 = '' OR u.nickname_normalized = $5)
		ORDER BY a.created_at DESC, a.id DESC
		LIMIT $6
	`, ts, id, q, category, authorNorm, limit+1)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	items := []anomalies.FeedItem{}
	var next string
	for rows.Next() {
		var it anomalies.FeedItem
		var bkt, obj string
		if err := rows.Scan(&it.ID, &it.Category, &it.Description, &it.LikeCount, &it.CommentCount, &it.CreatedAt, &it.Author.ID, &it.Author.Nickname, &it.Author.Avatar, &bkt, &obj); err != nil {
			return nil, "", err
		}
		if bkt != "" && obj != "" {
			it.ThumbnailURL = "gcs://" + bkt + "/" + obj
		}
		items = append(items, it)
	}
	if len(items) > limit {
		last := items[limit-1]
		next = encodeCursor(last.CreatedAt, last.ID)
		items = items[:limit]
	}
	return items, next, rows.Err()
}

func (p *pgAnomalies) ListByAuthorNickname(ctx context.Context, nickname, cursor string, limit int) ([]anomalies.FeedItem, string, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	ts, id, err := decodeCursor(cursor)
	if err != nil {
		return nil, "", err
	}
	rows, err := p.pool.Query(ctx, `
		SELECT a.id,a.category,a.description,a.like_count,a.comment_count,a.created_at,u.id,u.nickname,COALESCE(u.avatar_url,''),
		       COALESCE(img.bucket,''),COALESCE(img.object_name,'')
		FROM anomalies a
		JOIN users u ON u.id=a.author_id
		LEFT JOIN LATERAL (
			SELECT bucket,object_name FROM anomaly_images ai WHERE ai.anomaly_id=a.id ORDER BY position ASC LIMIT 1
		) img ON TRUE
		WHERE u.nickname_normalized=$1 AND (a.created_at, a.id) < ($2,$3)
		ORDER BY a.created_at DESC, a.id DESC
		LIMIT $4
	`, validation.NormalizeNickname(nickname), ts, id, limit+1)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	items := []anomalies.FeedItem{}
	var next string
	for rows.Next() {
		var it anomalies.FeedItem
		var bkt, obj string
		if err := rows.Scan(&it.ID, &it.Category, &it.Description, &it.LikeCount, &it.CommentCount, &it.CreatedAt, &it.Author.ID, &it.Author.Nickname, &it.Author.Avatar, &bkt, &obj); err != nil {
			return nil, "", err
		}
		if bkt != "" && obj != "" {
			it.ThumbnailURL = "gcs://" + bkt + "/" + obj
		}
		items = append(items, it)
	}
	if len(items) > limit {
		last := items[limit-1]
		next = encodeCursor(last.CreatedAt, last.ID)
		items = items[:limit]
	}
	return items, next, rows.Err()
}

func (p *pgAnomalies) Like(ctx context.Context, userID, anomalyID uuid.UUID) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `INSERT INTO anomaly_likes (anomaly_id,user_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, anomalyID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		if _, err := tx.Exec(ctx, `UPDATE anomalies SET like_count=like_count+1 WHERE id=$1`, anomalyID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (p *pgAnomalies) Unlike(ctx context.Context, userID, anomalyID uuid.UUID) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `DELETE FROM anomaly_likes WHERE anomaly_id=$1 AND user_id=$2`, anomalyID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		if _, err := tx.Exec(ctx, `UPDATE anomalies SET like_count=GREATEST(0, like_count-1) WHERE id=$1`, anomalyID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (p *pgComments) ListByAnomaly(ctx context.Context, anomalyID uuid.UUID, cursor string, limit int) ([]comments.Comment, string, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	ts, id, err := decodeCursor(cursor)
	if err != nil {
		return nil, "", err
	}
	rows, err := p.pool.Query(ctx, `
		SELECT id,anomaly_id,author_id,text,created_at
		FROM anomaly_comments
		WHERE anomaly_id=$1 AND (created_at,id) < ($2,$3)
		ORDER BY created_at DESC,id DESC
		LIMIT $4
	`, anomalyID, ts, id, limit+1)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	out := []comments.Comment{}
	var next string
	for rows.Next() {
		var c comments.Comment
		if err := rows.Scan(&c.ID, &c.AnomalyID, &c.AuthorID, &c.Text, &c.CreatedAt); err != nil {
			return nil, "", err
		}
		out = append(out, c)
	}
	if len(out) > limit {
		last := out[limit-1]
		next = encodeCursor(last.CreatedAt, last.ID)
		out = out[:limit]
	}
	return out, next, rows.Err()
}

func (p *pgComments) Create(ctx context.Context, anomalyID, authorID uuid.UUID, text string) (comments.Comment, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return comments.Comment{}, err
	}
	defer tx.Rollback(ctx)
	var c comments.Comment
	err = tx.QueryRow(ctx, `
		INSERT INTO anomaly_comments (anomaly_id,author_id,text)
		VALUES ($1,$2,$3)
		RETURNING id,anomaly_id,author_id,text,created_at
	`, anomalyID, authorID, strings.TrimSpace(text)).Scan(&c.ID, &c.AnomalyID, &c.AuthorID, &c.Text, &c.CreatedAt)
	if err != nil {
		return comments.Comment{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE anomalies SET comment_count=comment_count+1 WHERE id=$1`, anomalyID); err != nil {
		return comments.Comment{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return comments.Comment{}, err
	}
	return c, nil
}
