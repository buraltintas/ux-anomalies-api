package middleware

import (
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/time/rate"

	"ux-anomalies-api/internal/auth"
	"ux-anomalies-api/internal/response"
)

func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := r.Header.Get("X-Request-ID")
		if reqID == "" {
			reqID = uuid.NewString()
		}
		w.Header().Set("X-Request-ID", reqID)
		next.ServeHTTP(w, r.WithContext(WithRequestID(r.Context(), reqID)))
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func Logging(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			start := time.Now()
			next.ServeHTTP(sw, r)
			logger.Info("request",
				"request_id", RequestIDFrom(r.Context()),
				"method", r.Method,
				"path", r.URL.Path,
				"status", sw.status,
				"duration_ms", time.Since(start).Milliseconds(),
			)
		})
	}
}

func Recover(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("panic recovered", "request_id", RequestIDFrom(r.Context()))
					response.Error(w, http.StatusInternalServerError, "internal_error", "internal server error")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

func BFFSecret(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.TrimSpace(r.Header.Get("X-BFF-SECRET")) != secret {
				response.Error(w, http.StatusForbidden, "forbidden", "forbidden")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func RequireAuth(tokens *auth.TokenManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := strings.TrimSpace(r.Header.Get("Authorization"))
			if !strings.HasPrefix(h, "Bearer ") {
				response.Error(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
				return
			}
			claims, err := tokens.Parse(strings.TrimSpace(strings.TrimPrefix(h, "Bearer ")))
			if err != nil {
				response.Error(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
				return
			}
			uid, err := uuid.Parse(claims.UserID)
			if err != nil {
				response.Error(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
				return
			}
			next.ServeHTTP(w, r.WithContext(WithUserID(r.Context(), uid)))
		})
	}
}

func RateLimit(rps rate.Limit, burst int) func(http.Handler) http.Handler {
	limiters := map[string]*rate.Limiter{}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := clientIP(r)
			l, ok := limiters[key]
			if !ok {
				l = rate.NewLimiter(rps, burst)
				limiters[key] = l
			}
			if !l.Allow() {
				response.Error(w, http.StatusTooManyRequests, "rate_limited", "too many requests")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
