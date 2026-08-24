package urlnorm

import (
	"testing"
)

func TestURL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		// Scheme / host casing and default ports collapse.
		{"http upper host", "HTTP://Example.COM/a", "http://example.com/a"},
		{"https default port", "https://example.com:443/a", "https://example.com/a"},
		{"http default port", "http://example.com:80/a", "http://example.com/a"},
		{"non-default port kept", "https://example.com:8443/a", "https://example.com:8443/a"},

		// Trailing slash collapses except for root.
		{"trailing slash", "https://example.com/a/", "https://example.com/a"},
		{"root slash kept", "https://example.com/", "https://example.com/"},
		{"no path", "https://example.com", "https://example.com"},

		// Fragment dropped.
		{"fragment dropped", "https://example.com/a#section", "https://example.com/a"},
		{"fragment with query", "https://example.com/a?b=1#c", "https://example.com/a?b=1"},

		// Tracking params stripped, utm_ by prefix.
		{"utm stripped", "https://example.com/a?utm_source=feed&id=5", "https://example.com/a?id=5"},
		{"fbclid stripped", "https://example.com/a?fbclid=xyz&id=5", "https://example.com/a?id=5"},
		{"all tracking stripped", "https://example.com/a?utm_source=x&gclid=y&ref=z&keep=1", "https://example.com/a?keep=1"},

		// Query order is canonicalized.
		{"query order sorted", "https://example.com/a?b=2&a=1", "https://example.com/a?a=1&b=2"},
		{"query order sorted with tracking", "https://example.com/a?b=2&a=1&utm_source=x", "https://example.com/a?a=1&b=2"},

		// Whitespace trimmed.
		{"leading space", "  https://example.com/a", "https://example.com/a"},

		// Empty / relative / unparseable pass through unchanged.
		{"empty", "", ""},
		{"relative", "/path/only", "/path/only"},
		{"no scheme", "example.com/a", "example.com/a"},
		{"javascript", "javascript:void(0)", "javascript:void(0)"},
		{"mailto", "mailto:user@example.com", "mailto:user@example.com"},

		// Identity across sources: the canonical forms below must match.
		{"same article a", "https://Example.com/a/?utm_source=feed#top", "https://example.com/a"},
		{"same article b", "http://example.com/a/", "http://example.com/a"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := URL(c.in)
			if got != c.want {
				t.Errorf("URL(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// TestURL_CrossSourceIdentity verifies the motivating property: URLs that
// differ only by scheme/port/trailing-slash/fragment/tracking/order must
// normalize to the same string so star/read/share state is shared.
func TestURL_CrossSourceIdentity(t *testing.T) {
	groups := [][]string{
		{
			"https://example.com/article",
			"https://example.com/article/",
			"https://example.com/article#top",
			"https://example.com:443/article",
			"https://example.com/article?utm_source=feed",
			"https://example.com/article?utm_source=feed&fbclid=abc",
		},
		{
			"https://site.com/p?b=2&a=1",
			"https://site.com/p?a=1&b=2",
			"https://site.com/p/?a=1&b=2#frag",
		},
	}
	for _, g := range groups {
		first := URL(g[0])
		for _, raw := range g[1:] {
			if got := URL(raw); got != first {
				t.Errorf("identity mismatch: %q and %q -> %q vs %q", g[0], raw, first, got)
			}
		}
	}
}
