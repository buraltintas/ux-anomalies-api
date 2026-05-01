package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBFFSecretMiddleware(t *testing.T) {
	h := BFFSecret("secret")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/feed", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 got %d", rec.Code)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/v1/feed", nil)
	req2.Header.Set("X-BFF-SECRET", "secret")
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", rec2.Code)
	}
}
