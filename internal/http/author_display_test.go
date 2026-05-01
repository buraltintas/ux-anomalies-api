package http

import "testing"

func TestSafeAuthorDisplayName(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "email-like nickname uses local-part", input: "burak.altintas@yahoo.com.tr", expected: "burak.altintas"},
		{name: "normal nickname unchanged", input: "Burak_Alt", expected: "Burak_Alt"},
		{name: "empty nickname falls back", input: "  ", expected: authorFallbackName},
		{name: "email does not leak", input: "x+y@domain.com", expected: "xy"},
	}
	for _, tc := range cases {
		if got := safeAuthorDisplayName(tc.input); got != tc.expected {
			t.Fatalf("%s: expected %q got %q", tc.name, tc.expected, got)
		}
		if got := safeAuthorDisplayName(tc.input); stringsContainsAt(got) {
			t.Fatalf("%s: expected no email-like output got %q", tc.name, got)
		}
	}
}

func stringsContainsAt(v string) bool {
	for _, r := range v {
		if r == '@' {
			return true
		}
	}
	return false
}
