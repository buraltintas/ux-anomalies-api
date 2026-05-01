package http

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"ux-anomalies-api/internal/auth"
	"ux-anomalies-api/internal/config"
	"ux-anomalies-api/internal/storage"
)

type noopStore struct{}

func (n noopStore) UploadImage(_ context.Context, objectName, mimeType string, data []byte) (storage.UploadResult, error) {
	return storage.UploadResult{Bucket: "b", ObjectName: objectName, MimeType: mimeType, ByteSize: int64(len(data))}, nil
}
func (n noopStore) SignedGetURL(_ context.Context, _ string, objectName string, _ time.Duration) (string, error) {
	return "https://signed.example/" + objectName, nil
}

type oauthStubRepo struct{ user auth.OAuthUser }

func (o oauthStubRepo) UpsertOAuthUser(_ context.Context, _ auth.OAuthUpsertInput) (auth.OAuthUser, error) {
	return o.user, nil
}

func TestRouterSecurityAndBasicFlows(t *testing.T) {
	cfg := config.Config{
		AppAddr:         ":8080",
		BFFSecret:       "bff-secret",
		JWTIssuer:       "iss",
		JWTAudience:     "aud",
		JWTSigningKey:   "key",
		GCSSignedURLTTL: time.Hour,
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	tokens := auth.NewTokenManager(cfg)
	services := NewInMemoryServices()
	oauthSvc := auth.NewOAuthService(oauthStubRepo{user: auth.OAuthUser{
		UserID: uuid.New(), Email: "u@example.com", Nickname: "u@example.com",
	}}, tokens)
	r := NewRouter(cfg, logger, tokens, oauthSvc, noopStore{}, services)

	// healthz is public and does not require BFF secret
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz expected 200 got %d", rec.Code)
	}

	// v1 public endpoint requires BFF secret
	req = httptest.NewRequest(http.MethodGet, "/v1/feed", nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("feed without bff expected 403 got %d", rec.Code)
	}
	assertErrorShape(t, rec.Body.String())

	// auth-required endpoint with bff but missing bearer should be 401
	req = httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	req.Header.Set("X-BFF-SECRET", "bff-secret")
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("me without bearer expected 401 got %d", rec.Code)
	}
	assertErrorShape(t, rec.Body.String())

	// internal oauth upsert route with bff should work
	body := strings.NewReader(`{"provider":"google","provider_user_id":"g-1","email":"user@example.com","email_verified":true}`)
	req = httptest.NewRequest(http.MethodPost, "/v1/internal/auth/oauth-upsert", body)
	req.Header.Set("X-BFF-SECRET", "bff-secret")
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("oauth upsert expected 200 got %d body=%s", rec.Code, rec.Body.String())
	}
}

func assertErrorShape(t *testing.T, body string) {
	t.Helper()
	var parsed map[string]any
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("invalid json error body: %v", err)
	}
	errObj, ok := parsed["error"].(map[string]any)
	if !ok {
		t.Fatalf("error envelope missing: %s", body)
	}
	if _, ok := errObj["code"].(string); !ok {
		t.Fatalf("error code missing: %s", body)
	}
	if _, ok := errObj["message"].(string); !ok {
		t.Fatalf("error message missing: %s", body)
	}
}
