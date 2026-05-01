package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"ux-anomalies-api/internal/config"
)

type TokenManager struct {
	issuer    string
	audience  string
	signing   []byte
	accessTTL time.Duration
}

type Claims struct {
	UserID string `json:"uid"`
	jwt.RegisteredClaims
}

func NewTokenManager(cfg config.Config) *TokenManager {
	return &TokenManager{
		issuer:    cfg.JWTIssuer,
		audience:  cfg.JWTAudience,
		signing:   []byte(cfg.JWTSigningKey),
		accessTTL: cfg.JWTAccessTTL,
	}
}

func (m *TokenManager) Mint(userID uuid.UUID) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID: userID.String(),
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    m.issuer,
			Subject:   userID.String(),
			Audience:  []string{m.audience},
			ExpiresAt: jwt.NewNumericDate(now.Add(m.accessTTL)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
		},
	}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return t.SignedString(m.signing)
}

func (m *TokenManager) Parse(token string) (*Claims, error) {
	parsed, err := jwt.ParseWithClaims(token, &Claims{}, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("invalid signing method")
		}
		return m.signing, nil
	}, jwt.WithIssuer(m.issuer), jwt.WithAudience(m.audience))
	if err != nil {
		return nil, err
	}
	claims, ok := parsed.Claims.(*Claims)
	if !ok || !parsed.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}
