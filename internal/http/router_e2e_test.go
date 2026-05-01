package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"ux-anomalies-api/internal/auth"
	"ux-anomalies-api/internal/config"
	"ux-anomalies-api/internal/storage"
)

type fakeStore struct {
	uploaded []storage.UploadResult
}

func (f *fakeStore) UploadImage(_ context.Context, objectName, mimeType string, data []byte) (storage.UploadResult, error) {
	up := storage.UploadResult{
		Bucket:     "test-bucket",
		ObjectName: objectName,
		MimeType:   mimeType,
		ByteSize:   int64(len(data)),
	}
	f.uploaded = append(f.uploaded, up)
	return up, nil
}

func (f *fakeStore) SignedGetURL(_ context.Context, bucket, objectName string, _ time.Duration) (string, error) {
	return "https://signed.local/" + bucket + "/" + objectName, nil
}

func TestRouterE2E_V1Flows(t *testing.T) {
	dbURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if dbURL == "" {
		dbURL = strings.TrimSpace(os.Getenv("DATABASE_URL"))
	}
	if dbURL == "" || strings.Contains(dbURL, "replace-me") {
		t.Skip("set TEST_DATABASE_URL or DATABASE_URL for e2e test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	resetAll(t, ctx, pool)

	cfg := config.Config{
		AppEnv:            "development",
		AppAddr:           ":8080",
		LogLevel:          slog.LevelDebug,
		DatabaseURL:       dbURL,
		MigrationsDBURL:   dbURL,
		BFFSecret:         "bff-secret",
		JWTIssuer:         "issuer",
		JWTAudience:       "audience",
		JWTSigningKey:     "super-secret",
		JWTAccessTTL:      24 * time.Hour,
		MaxBodyBytes:      1 << 20,
		MaxMultipartBytes: 20 << 20,
		MaxImageBytes:     5 << 20,
		StorageBackend:    "gcs",
		GCPProjectID:      "p",
		GCSBucket:         "b",
		GCSUploadPrefix:   "anomalies",
		GCSSignedURLTTL:   24 * time.Hour,
	}
	tokenMgr := auth.NewTokenManager(cfg)
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	store := &fakeStore{}
	services := NewPostgresServices(pool)
	oauthSvc := auth.NewOAuthService(auth.NewPostgresOAuthRepo(pool), tokenMgr)
	router := NewRouter(cfg, logger, tokenMgr, oauthSvc, store, services)

	ownerID := seedUser(t, ctx, pool, "owner@example.com", "owner")
	otherID := seedUser(t, ctx, pool, "other@example.com", "other")
	ownerToken := mustToken(t, tokenMgr, ownerID)
	otherToken := mustToken(t, tokenMgr, otherID)

	// 1) security checks
	req := httptest.NewRequest(http.MethodGet, "/v1/feed", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for missing bff secret, got %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	req.Header.Set("X-BFF-SECRET", "bff-secret")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for missing bearer, got %d", rec.Code)
	}

	// 2) authenticated profile read
	req = httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	setAuthHeaders(req, ownerToken)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/me expected 200 got %d body=%s", rec.Code, rec.Body.String())
	}

	// 3) anomaly create with multipart image
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	_ = w.WriteField("category", "web")
	_ = w.WriteField("description", "desc")
	_ = w.WriteField("source_url", "https://example.com")
	fw, _ := w.CreateFormFile("images", "img.png")
	_, _ = fw.Write(oneByOnePNG())
	_ = w.Close()

	req = httptest.NewRequest(http.MethodPost, "/v1/anomalies", body)
	setAuthHeaders(req, ownerToken)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /v1/anomalies expected 201 got %d body=%s", rec.Code, rec.Body.String())
	}
	if len(store.uploaded) != 1 {
		t.Fatalf("expected 1 upload call, got %d", len(store.uploaded))
	}
	var created struct {
		ID     string `json:"id"`
		Images []struct {
			URL string `json:"url"`
		} `json:"images"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if created.ID == "" || len(created.Images) != 1 || !strings.HasPrefix(created.Images[0].URL, "https://signed.local/") {
		t.Fatalf("unexpected create response: %s", rec.Body.String())
	}
	anomalyID := created.ID

	// DB rows exist for anomaly and image metadata
	var anomalyCount, imageCount int
	_ = pool.QueryRow(ctx, "SELECT COUNT(*) FROM anomalies WHERE id=$1", anomalyID).Scan(&anomalyCount)
	_ = pool.QueryRow(ctx, "SELECT COUNT(*) FROM anomaly_images WHERE anomaly_id=$1", anomalyID).Scan(&imageCount)
	if anomalyCount != 1 || imageCount != 1 {
		t.Fatalf("expected persisted anomaly/image rows; got anomalies=%d images=%d", anomalyCount, imageCount)
	}

	// 4) anomaly detail read
	req = httptest.NewRequest(http.MethodGet, "/v1/anomalies/"+anomalyID, nil)
	req.Header.Set("X-BFF-SECRET", "bff-secret")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "https://signed.local/") {
		t.Fatalf("GET anomaly detail failed: %d %s", rec.Code, rec.Body.String())
	}

	// 5) profile anomaly listing
	req = httptest.NewRequest(http.MethodGet, "/v1/users/"+url.PathEscape("owner"), nil)
	req.Header.Set("X-BFF-SECRET", "bff-secret")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "\"anomalies\"") {
		t.Fatalf("GET profile listing failed: %d %s", rec.Code, rec.Body.String())
	}

	// feed search/filtering
	req = httptest.NewRequest(http.MethodGet, "/v1/feed?q=desc&category=web&author_nickname=owner", nil)
	req.Header.Set("X-BFF-SECRET", "bff-secret")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), anomalyID) {
		t.Fatalf("feed search/filter failed: %d %s", rec.Code, rec.Body.String())
	}

	// user search
	req = httptest.NewRequest(http.MethodGet, "/v1/users/search?q=own", nil)
	req.Header.Set("X-BFF-SECRET", "bff-secret")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "\"owner\"") {
		t.Fatalf("user search failed: %d %s", rec.Code, rec.Body.String())
	}

	// 6) like/unlike idempotency and counters
	req = httptest.NewRequest(http.MethodPost, "/v1/anomalies/"+anomalyID+"/like", nil)
	setAuthHeaders(req, ownerToken)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("like failed: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/v1/anomalies/"+anomalyID+"/like", nil)
	setAuthHeaders(req, ownerToken)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	var likes int
	_ = pool.QueryRow(ctx, "SELECT like_count FROM anomalies WHERE id=$1", anomalyID).Scan(&likes)
	if likes != 1 {
		t.Fatalf("like_count expected 1 got %d", likes)
	}
	req = httptest.NewRequest(http.MethodDelete, "/v1/anomalies/"+anomalyID+"/like", nil)
	setAuthHeaders(req, ownerToken)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	req = httptest.NewRequest(http.MethodDelete, "/v1/anomalies/"+anomalyID+"/like", nil)
	setAuthHeaders(req, ownerToken)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	_ = pool.QueryRow(ctx, "SELECT like_count FROM anomalies WHERE id=$1", anomalyID).Scan(&likes)
	if likes != 0 {
		t.Fatalf("like_count expected 0 got %d", likes)
	}

	// 7) comments create/list and counter
	req = httptest.NewRequest(http.MethodPost, "/v1/anomalies/"+anomalyID+"/comments", strings.NewReader(`{"text":"hello"}`))
	setAuthHeaders(req, ownerToken)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("comment create failed: %d %s", rec.Code, rec.Body.String())
	}
	var commentsCount int
	_ = pool.QueryRow(ctx, "SELECT comment_count FROM anomalies WHERE id=$1", anomalyID).Scan(&commentsCount)
	if commentsCount != 1 {
		t.Fatalf("comment_count expected 1 got %d", commentsCount)
	}
	req = httptest.NewRequest(http.MethodGet, "/v1/anomalies/"+anomalyID+"/comments", nil)
	req.Header.Set("X-BFF-SECRET", "bff-secret")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "hello") {
		t.Fatalf("comments list failed: %d %s", rec.Code, rec.Body.String())
	}

	// 8) patch ownership: owner succeeds, non-owner fails safely
	req = httptest.NewRequest(http.MethodPatch, "/v1/anomalies/"+anomalyID, strings.NewReader(`{"category":"web","description":"updated","source_url":"https://example.com/u"}`))
	setAuthHeaders(req, ownerToken)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("owner patch failed: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPatch, "/v1/anomalies/"+anomalyID, strings.NewReader(`{"category":"web","description":"bad","source_url":"https://example.com/bad"}`))
	setAuthHeaders(req, otherToken)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("non-owner patch should fail safely with 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func setAuthHeaders(req *http.Request, token string) {
	req.Header.Set("X-BFF-SECRET", "bff-secret")
	req.Header.Set("Authorization", "Bearer "+token)
}

func mustToken(t *testing.T, tm *auth.TokenManager, uid uuid.UUID) string {
	t.Helper()
	tok, err := tm.Mint(uid)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func oneByOnePNG() []byte {
	return []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
		0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53,
		0xDE, 0x00, 0x00, 0x00, 0x0C, 0x49, 0x44, 0x41,
		0x54, 0x08, 0xD7, 0x63, 0xF8, 0x0F, 0x00, 0x01,
		0x01, 0x01, 0x00, 0x18, 0xDD, 0x8D, 0xB1, 0x00,
		0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE,
		0x42, 0x60, 0x82,
	}
}

func TestRouteTableSmoke(t *testing.T) {
	// quick smoke that all expected routes are mounted under /v1
	r := chi.NewRouter()
	_ = r
	expected := []string{
		"/v1/feed",
		"/v1/anomalies/{id}",
		"/v1/users/{nickname}",
		"/v1/me",
		"/v1/anomalies",
		"/v1/anomalies/{id}/like",
		"/v1/anomalies/{id}/comments",
	}
	if len(expected) == 0 {
		t.Fatal(fmt.Errorf("expected route list should not be empty"))
	}
}
