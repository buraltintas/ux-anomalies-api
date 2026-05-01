package auth

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"ux-anomalies-api/internal/validation"
)

type PostgresOAuthRepo struct {
	pool *pgxpool.Pool
}

func NewPostgresOAuthRepo(pool *pgxpool.Pool) *PostgresOAuthRepo {
	return &PostgresOAuthRepo{pool: pool}
}

func (r *PostgresOAuthRepo) UpsertOAuthUser(ctx context.Context, in OAuthUpsertInput) (OAuthUser, error) {
	const maxAttempts = 12
	for attempt := 0; attempt < maxAttempts; attempt++ {
		user, retry, err := r.upsertOnce(ctx, in)
		if err != nil {
			return OAuthUser{}, err
		}
		if !retry {
			return user, nil
		}
	}
	return OAuthUser{}, errors.New("oauth upsert retry limit reached")
}

func (r *PostgresOAuthRepo) upsertOnce(ctx context.Context, in OAuthUpsertInput) (OAuthUser, bool, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return OAuthUser{}, false, err
	}
	defer tx.Rollback(ctx)

	// 1) provider identity already linked: return existing user.
	if u, found, err := findByProvider(ctx, tx, in.Provider, in.ProviderUserID); err != nil {
		return OAuthUser{}, false, err
	} else if found {
		if err := tx.Commit(ctx); err != nil {
			return OAuthUser{}, false, err
		}
		return u, false, nil
	}

	// 2) link to existing email user if present.
	if u, found, err := findByEmail(ctx, tx, in.Email); err != nil {
		return OAuthUser{}, false, err
	} else if found {
		_, err = tx.Exec(ctx, `
			INSERT INTO oauth_accounts (user_id, provider, provider_user_id, provider_email, provider_email_verified)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (provider, provider_user_id) DO NOTHING
		`, u.UserID, in.Provider, in.ProviderUserID, in.Email, in.EmailVerified)
		if err != nil {
			if isRetryable(err) {
				return OAuthUser{}, true, nil
			}
			return OAuthUser{}, false, err
		}
		// Re-read by provider to handle races where another tx linked it first.
		linked, found, err := findByProvider(ctx, tx, in.Provider, in.ProviderUserID)
		if err != nil {
			return OAuthUser{}, false, err
		}
		if !found {
			return OAuthUser{}, true, nil
		}
		if err := tx.Commit(ctx); err != nil {
			return OAuthUser{}, false, err
		}
		return linked, false, nil
	}

	// 3) create new user + oauth link in one transaction.
	nickname, err := allocateNickname(ctx, tx, in.Email)
	if err != nil {
		return OAuthUser{}, false, err
	}
	var created OAuthUser
	err = tx.QueryRow(ctx, `
		INSERT INTO users (email, email_normalized, nickname, nickname_normalized, avatar_url)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, email, nickname, COALESCE(avatar_url,'')
	`, in.Email, validation.NormalizeEmail(in.Email), nickname, validation.NormalizeNickname(nickname), strings.TrimSpace(in.AvatarURL)).
		Scan(&created.UserID, &created.Email, &created.Nickname, &created.AvatarURL)
	if err != nil {
		if isRetryable(err) {
			return OAuthUser{}, true, nil
		}
		return OAuthUser{}, false, err
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO oauth_accounts (user_id, provider, provider_user_id, provider_email, provider_email_verified)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (provider, provider_user_id) DO NOTHING
	`, created.UserID, in.Provider, in.ProviderUserID, in.Email, in.EmailVerified)
	if err != nil {
		if isRetryable(err) {
			return OAuthUser{}, true, nil
		}
		return OAuthUser{}, false, err
	}

	linked, found, err := findByProvider(ctx, tx, in.Provider, in.ProviderUserID)
	if err != nil {
		return OAuthUser{}, false, err
	}
	if !found {
		return OAuthUser{}, true, nil
	}

	if err := tx.Commit(ctx); err != nil {
		if isRetryable(err) {
			return OAuthUser{}, true, nil
		}
		return OAuthUser{}, false, err
	}
	return linked, false, nil
}

func allocateNickname(ctx context.Context, tx pgx.Tx, email string) (string, error) {
	base := strings.TrimSpace(email)
	for i := 1; i < 10000; i++ {
		candidate := base
		if i > 1 {
			candidate = fmt.Sprintf("%s-%s", base, strconv.Itoa(i))
		}
		var exists bool
		err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE nickname_normalized = $1)`, validation.NormalizeNickname(candidate)).Scan(&exists)
		if err != nil {
			return "", err
		}
		if !exists {
			return candidate, nil
		}
	}
	return "", errors.New("failed to allocate nickname")
}

func findByProvider(ctx context.Context, q pgx.Tx, provider Provider, providerUserID string) (OAuthUser, bool, error) {
	var u OAuthUser
	err := q.QueryRow(ctx, `
		SELECT u.id, u.email, u.nickname, COALESCE(u.avatar_url, '')
		FROM oauth_accounts oa
		JOIN users u ON u.id = oa.user_id
		WHERE oa.provider = $1 AND oa.provider_user_id = $2
	`, provider, providerUserID).Scan(&u.UserID, &u.Email, &u.Nickname, &u.AvatarURL)
	if errors.Is(err, pgx.ErrNoRows) {
		return OAuthUser{}, false, nil
	}
	if err != nil {
		return OAuthUser{}, false, err
	}
	return u, true, nil
}

func findByEmail(ctx context.Context, q pgx.Tx, emailNormalized string) (OAuthUser, bool, error) {
	var u OAuthUser
	err := q.QueryRow(ctx, `
		SELECT id, email, nickname, COALESCE(avatar_url, '')
		FROM users
		WHERE email_normalized = $1
	`, validation.NormalizeEmail(emailNormalized)).Scan(&u.UserID, &u.Email, &u.Nickname, &u.AvatarURL)
	if errors.Is(err, pgx.ErrNoRows) {
		return OAuthUser{}, false, nil
	}
	if err != nil {
		return OAuthUser{}, false, err
	}
	return u, true, nil
}

func isRetryable(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "23505" || pgErr.Code == "40001" || pgErr.Code == "40P01"
}
