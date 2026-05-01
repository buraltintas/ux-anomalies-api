package validation

import (
	"bytes"
	"errors"
	"net/http"
	"strings"
)

var allowedImageMIME = map[string]struct{}{
	"image/jpeg": {},
	"image/png":  {},
	"image/webp": {},
}

func ValidateImageContent(data []byte) (string, error) {
	if len(data) == 0 {
		return "", errors.New("empty image")
	}
	ct := http.DetectContentType(data)
	if _, ok := allowedImageMIME[ct]; !ok {
		return "", errors.New("unsupported image format; allowed: jpg,jpeg,png,webp")
	}
	// Explicitly reject svg text payloads if mislabeled.
	if bytes.Contains(bytes.ToLower(data[:min(len(data), 512)]), []byte("<svg")) {
		return "", errors.New("svg is not allowed")
	}
	return ct, nil
}

func IsAllowedExtension(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	return strings.HasSuffix(n, ".jpg") || strings.HasSuffix(n, ".jpeg") || strings.HasSuffix(n, ".png") || strings.HasSuffix(n, ".webp")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
