package validation

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

var nicknameRe = regexp.MustCompile(`^[a-zA-Z0-9._@-]{3,120}$`)

func NormalizeEmail(v string) string {
	return strings.ToLower(strings.TrimSpace(v))
}

func NormalizeNickname(v string) string {
	return strings.ToLower(strings.TrimSpace(v))
}

func ValidateNickname(v string) error {
	n := strings.TrimSpace(v)
	if !nicknameRe.MatchString(n) {
		return errors.New("nickname format is invalid")
	}
	return nil
}

func NormalizeOptionalURL(v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", nil
	}
	u, err := url.Parse(v)
	if err != nil {
		return "", errors.New("invalid url")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", errors.New("url must be http or https")
	}
	return u.String(), nil
}
