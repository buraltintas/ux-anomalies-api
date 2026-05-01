package http

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"golang.org/x/time/rate"

	"ux-anomalies-api/internal/anomalies"
	"ux-anomalies-api/internal/auth"
	"ux-anomalies-api/internal/config"
	"ux-anomalies-api/internal/http/middleware"
	"ux-anomalies-api/internal/response"
	"ux-anomalies-api/internal/storage"
	"ux-anomalies-api/internal/validation"
)

func NewRouter(cfg config.Config, logger *slog.Logger, tokens *auth.TokenManager, oauthSvc *auth.OAuthService, st storage.Store, s Services) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.Logging(logger), middleware.Recover(logger))
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		response.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Get("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		response.JSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})

	r.Route("/v1", func(v1 chi.Router) {
		v1.Use(middleware.BFFSecret(cfg.BFFSecret))
		v1.Group(func(pub chi.Router) {
			pub.Use(middleware.RateLimit(rate.Limit(20), 40))
			pub.Get("/feed", handleFeed(st, cfg.GCSSignedURLTTL, s))
			pub.Get("/users/search", handleUserSearch(s))
			pub.Get("/anomalies/{id}", handleGetAnomaly(st, cfg.GCSSignedURLTTL, s))
			pub.Get("/anomalies/{id}/comments", handleListComments(s))
			pub.Get("/users/{nickname}", handleGetPublicUser(st, cfg.GCSSignedURLTTL, s))
		})
		v1.Group(func(authed chi.Router) {
			authed.Use(middleware.RequireAuth(tokens))
			authed.Get("/me", handleMe(s))
			authed.Patch("/me", handleUpdateMe(s))
			authed.Post("/anomalies", handleCreateAnomaly(cfg, st, s))
			authed.Patch("/anomalies/{id}", handlePatchAnomaly(s))
			authed.Post("/anomalies/{id}/like", handleLike(s))
			authed.Delete("/anomalies/{id}/like", handleUnlike(s))
			authed.Post("/anomalies/{id}/comments", handleCreateComment(s))
		})
		v1.Group(func(internal chi.Router) {
			internal.Use(middleware.RateLimit(rate.Limit(5), 10))
			internal.Post("/internal/auth/oauth-upsert", handleOAuthUpsert(oauthSvc))
		})
	})
	return r
}

func limit(r *http.Request, def int) int {
	v := strings.TrimSpace(r.URL.Query().Get("limit"))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func handleFeed(st storage.Store, ttl time.Duration, s Services) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		filter := anomalies.FeedFilter{
			Q:              strings.TrimSpace(r.URL.Query().Get("q")),
			Category:       anomalies.Category(strings.TrimSpace(r.URL.Query().Get("category"))),
			AuthorNickname: strings.TrimSpace(r.URL.Query().Get("author_nickname")),
		}
		items, next, err := s.Anomalies.GetFeed(r.Context(), filter, r.URL.Query().Get("cursor"), limit(r, 20))
		if err != nil {
			response.Error(w, http.StatusBadRequest, "invalid_input", err.Error())
			return
		}
		for i := range items {
			if strings.HasPrefix(items[i].ThumbnailURL, "gcs://") {
				b, o, ok := parseGCSRef(items[i].ThumbnailURL)
				if ok {
					u, serr := st.SignedGetURL(r.Context(), b, o, ttl)
					if serr == nil {
						items[i].ThumbnailURL = u
					}
				}
			}
		}
		response.JSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": next})
	}
}

func handleUserSearch(s Services) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		items, next, err := s.Users.Search(r.Context(), q, r.URL.Query().Get("cursor"), limit(r, 20))
		if err != nil {
			response.Error(w, http.StatusInternalServerError, "internal_error", "internal server error")
			return
		}
		response.JSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": next})
	}
}

func handleGetAnomaly(st storage.Store, ttl time.Duration, s Services) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			response.Error(w, http.StatusBadRequest, "invalid_input", "invalid anomaly id")
			return
		}
		a, err := s.Anomalies.GetByID(r.Context(), id)
		if err != nil {
			response.Error(w, http.StatusNotFound, "not_found", "not found")
			return
		}
		for i := range a.Images {
			u, err := st.SignedGetURL(r.Context(), a.Images[i].Bucket, a.Images[i].ObjectName, ttl)
			if err == nil {
				a.Images[i].URL = u
			}
		}
		response.JSON(w, http.StatusOK, a)
	}
}

func handleListComments(s Services) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			response.Error(w, http.StatusBadRequest, "invalid_input", "invalid anomaly id")
			return
		}
		items, next, err := s.Comments.ListByAnomaly(r.Context(), id, r.URL.Query().Get("cursor"), limit(r, 50))
		if err != nil {
			response.Error(w, http.StatusInternalServerError, "internal_error", "internal server error")
			return
		}
		response.JSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": next})
	}
}

func handleGetPublicUser(st storage.Store, ttl time.Duration, s Services) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, err := url.PathUnescape(chi.URLParam(r, "nickname"))
		if err != nil {
			response.Error(w, http.StatusBadRequest, "invalid_input", "invalid nickname path")
			return
		}
		prof, err := s.Users.GetPublicByNickname(r.Context(), raw)
		if err != nil {
			response.Error(w, http.StatusNotFound, "not_found", "not found")
			return
		}
		items, next, err := s.Anomalies.ListByAuthorNickname(r.Context(), raw, r.URL.Query().Get("cursor"), limit(r, 20))
		if err != nil {
			response.Error(w, http.StatusInternalServerError, "internal_error", "internal server error")
			return
		}
		for i := range items {
			if strings.HasPrefix(items[i].ThumbnailURL, "gcs://") {
				b, o, ok := parseGCSRef(items[i].ThumbnailURL)
				if ok {
					u, serr := st.SignedGetURL(r.Context(), b, o, ttl)
					if serr == nil {
						items[i].ThumbnailURL = u
					}
				}
			}
		}
		response.JSON(w, http.StatusOK, map[string]any{
			"profile":     prof,
			"anomalies":   items,
			"next_cursor": next,
		})
	}
}

func parseGCSRef(v string) (bucket, object string, ok bool) {
	if !strings.HasPrefix(v, "gcs://") {
		return "", "", false
	}
	parts := strings.SplitN(strings.TrimPrefix(v, "gcs://"), "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func handleMe(s Services) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid, ok := middleware.UserIDFrom(r.Context())
		if !ok {
			response.Error(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
			return
		}
		u, err := s.Users.GetByID(r.Context(), uid)
		if err != nil {
			response.Error(w, http.StatusNotFound, "not_found", "not found")
			return
		}
		response.JSON(w, http.StatusOK, u)
	}
}

func handleUpdateMe(s Services) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid, ok := middleware.UserIDFrom(r.Context())
		if !ok {
			response.Error(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
			return
		}
		var req struct {
			Nickname  string `json:"nickname"`
			AvatarURL string `json:"avatar_url"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
			response.Error(w, http.StatusBadRequest, "invalid_input", "invalid request body")
			return
		}
		if req.Nickname != "" {
			if err := validation.ValidateNickname(req.Nickname); err != nil {
				response.Error(w, http.StatusBadRequest, "invalid_input", err.Error())
				return
			}
		}
		urlVal, err := validation.NormalizeOptionalURL(req.AvatarURL)
		if err != nil {
			response.Error(w, http.StatusBadRequest, "invalid_input", err.Error())
			return
		}
		u, err := s.Users.UpdateMe(r.Context(), uid, req.Nickname, urlVal)
		if err != nil {
			response.Error(w, http.StatusBadRequest, "invalid_input", err.Error())
			return
		}
		response.JSON(w, http.StatusOK, u)
	}
}

func handleOAuthUpsert(s *auth.OAuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req auth.OAuthUpsertInput
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
			response.Error(w, http.StatusBadRequest, "invalid_input", "invalid request body")
			return
		}
		user, token, err := s.OAuthUpsert(r.Context(), req)
		if err != nil {
			response.Error(w, http.StatusBadRequest, "auth_failed", err.Error())
			return
		}
		response.JSON(w, http.StatusOK, map[string]any{"user": user, "token": token})
	}
}

func handleCreateAnomaly(cfg config.Config, st storage.Store, s Services) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid, ok := middleware.UserIDFrom(r.Context())
		if !ok {
			response.Error(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, cfg.MaxMultipartBytes)
		if err := r.ParseMultipartForm(cfg.MaxMultipartBytes); err != nil {
			response.Error(w, http.StatusBadRequest, "invalid_input", "invalid multipart form")
			return
		}
		category := anomalies.Category(r.FormValue("category"))
		desc := r.FormValue("description")
		src, err := validation.NormalizeOptionalURL(r.FormValue("source_url"))
		if err != nil {
			response.Error(w, http.StatusBadRequest, "invalid_input", err.Error())
			return
		}
		files := r.MultipartForm.File["images"]
		if len(files) < 1 || len(files) > 3 {
			response.Error(w, http.StatusBadRequest, "invalid_input", "images must be 1..3")
			return
		}
		uploaded := make([]anomalies.Image, 0, len(files))
		for i, fh := range files {
			if fh.Size <= 0 || fh.Size > cfg.MaxImageBytes {
				response.Error(w, http.StatusBadRequest, "invalid_input", "image exceeds size limits")
				return
			}
			if !validation.IsAllowedExtension(fh.Filename) {
				response.Error(w, http.StatusBadRequest, "invalid_input", "unsupported image extension")
				return
			}
			f, err := fh.Open()
			if err != nil {
				response.Error(w, http.StatusBadRequest, "invalid_input", "invalid image")
				return
			}
			data, err := io.ReadAll(io.LimitReader(f, cfg.MaxImageBytes+1))
			_ = f.Close()
			if err != nil {
				response.Error(w, http.StatusBadRequest, "invalid_input", "failed to read image")
				return
			}
			if int64(len(data)) > cfg.MaxImageBytes {
				response.Error(w, http.StatusBadRequest, "invalid_input", "image exceeds size limits")
				return
			}
			mimeType, err := validation.ValidateImageContent(data)
			if err != nil {
				response.Error(w, http.StatusBadRequest, "invalid_input", err.Error())
				return
			}
			objectName := fmt.Sprintf("%s/%s/%s_%d_%s", strings.Trim(cfg.GCSUploadPrefix, "/"), uid.String(), uuid.NewString(), i+1, filepath.Base(fh.Filename))
			up, err := st.UploadImage(r.Context(), objectName, mimeType, data)
			if err != nil {
				response.Error(w, http.StatusInternalServerError, "internal_error", "failed to upload image")
				return
			}
			uploaded = append(uploaded, anomalies.Image{
				Bucket:     up.Bucket,
				ObjectName: up.ObjectName,
				MimeType:   up.MimeType,
				ByteSize:   up.ByteSize,
				Position:   i + 1,
			})
		}
		a, err := s.Anomalies.Create(r.Context(), uid, anomalies.CreateInput{
			Category:    category,
			Description: desc,
			SourceURL:   src,
			Images:      uploaded,
		})
		if err != nil {
			response.Error(w, http.StatusBadRequest, "invalid_input", err.Error())
			return
		}
		for i := range a.Images {
			u, err := st.SignedGetURL(r.Context(), a.Images[i].Bucket, a.Images[i].ObjectName, cfg.GCSSignedURLTTL)
			if err == nil {
				a.Images[i].URL = u
			}
		}
		response.JSON(w, http.StatusCreated, a)
	}
}

func handlePatchAnomaly(s Services) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid, ok := middleware.UserIDFrom(r.Context())
		if !ok {
			response.Error(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
			return
		}
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			response.Error(w, http.StatusBadRequest, "invalid_input", "invalid anomaly id")
			return
		}
		var req anomalies.UpdateInput
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
			response.Error(w, http.StatusBadRequest, "invalid_input", "invalid request body")
			return
		}
		if req.SourceURL, err = validation.NormalizeOptionalURL(req.SourceURL); err != nil {
			response.Error(w, http.StatusBadRequest, "invalid_input", err.Error())
			return
		}
		a, err := s.Anomalies.Update(r.Context(), uid, id, req)
		if err != nil {
			response.Error(w, http.StatusBadRequest, "invalid_input", err.Error())
			return
		}
		response.JSON(w, http.StatusOK, a)
	}
}

func handleLike(s Services) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid, _ := middleware.UserIDFrom(r.Context())
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			response.Error(w, http.StatusBadRequest, "invalid_input", "invalid anomaly id")
			return
		}
		if err := s.Anomalies.Like(r.Context(), uid, id); err != nil {
			response.Error(w, http.StatusBadRequest, "invalid_input", err.Error())
			return
		}
		response.JSON(w, http.StatusOK, map[string]string{"status": "liked"})
	}
}

func handleUnlike(s Services) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid, _ := middleware.UserIDFrom(r.Context())
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			response.Error(w, http.StatusBadRequest, "invalid_input", "invalid anomaly id")
			return
		}
		if err := s.Anomalies.Unlike(r.Context(), uid, id); err != nil {
			response.Error(w, http.StatusBadRequest, "invalid_input", err.Error())
			return
		}
		response.JSON(w, http.StatusOK, map[string]string{"status": "unliked"})
	}
}

func handleCreateComment(s Services) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid, _ := middleware.UserIDFrom(r.Context())
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			response.Error(w, http.StatusBadRequest, "invalid_input", "invalid anomaly id")
			return
		}
		var req struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
			response.Error(w, http.StatusBadRequest, "invalid_input", "invalid request body")
			return
		}
		c, err := s.Comments.Create(r.Context(), id, uid, req.Text)
		if err != nil {
			response.Error(w, http.StatusBadRequest, "invalid_input", err.Error())
			return
		}
		response.JSON(w, http.StatusCreated, c)
	}
}
