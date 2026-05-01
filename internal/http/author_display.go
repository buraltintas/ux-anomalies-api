package http

import "strings"

const authorFallbackName = "UX Anomalies user"
const authorDisplayMaxLen = 32

func safeAuthorDisplayName(raw string) string {
	name := strings.TrimSpace(raw)
	if name == "" {
		return authorFallbackName
	}
	if !isEmailLike(name) {
		return name
	}
	local := strings.TrimSpace(strings.SplitN(name, "@", 2)[0])
	local = strings.ToLower(local)
	local = sanitizeHandle(local)
	local = strings.Trim(local, "._-")
	if local == "" {
		return authorFallbackName
	}
	if len(local) > authorDisplayMaxLen {
		local = local[:authorDisplayMaxLen]
	}
	return local
}

func isEmailLike(v string) bool {
	at := strings.LastIndex(v, "@")
	return at > 0 && at < len(v)-1
}

func sanitizeHandle(v string) string {
	var b strings.Builder
	b.Grow(len(v))
	for _, r := range v {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
