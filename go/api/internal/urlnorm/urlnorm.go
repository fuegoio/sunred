// Package urlnorm canonicalizes URLs so that textually-different but
// semantically-equivalent URLs collapse to a single key. This is what
// makes star / read / share state uniform across feeds and sources: two
// entries whose article URLs differ only by scheme casing, default
// ports, trailing slashes, fragment, or tracking query parameters map
// to the same normalized string and thus to the same state row.
//
// Normalization is intentionally conservative: it only rewrites URLs it
// can fully parse with a non-empty scheme and host. Anything it cannot
// parse (relative links, malformed input) is returned unchanged so the
// caller still stores something deterministic.
package urlnorm

import (
	"net/url"
	"strings"
)

// exactTrackingParams are query parameters that carry no identity and are
// stripped during normalization. utm_* is matched by prefix; these by
// exact (lowercased) name.
var exactTrackingParams = map[string]bool{
	"fbclid":  true,
	"gclid":   true,
	"igshid":  true,
	"mc_cid":  true,
	"mc_eid":  true,
	"ref":     true,
	"ref_src": true,
	"ref_url": true,
	"source":  true,
	"yclid":   true,
	"msclkid": true,
	"_hsenc":  true,
	"_hsmi":   true,
	"spm":     true,
	"ito":     true,
	"cid":     true,
	"si":      true, // YouTube share-source
}

// URL returns the canonical form of s. If s is empty or not an absolute
// URL with a host, it is returned unchanged.
func URL(s string) string {
	if s == "" {
		return ""
	}
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil {
		return s
	}
	if u.Scheme == "" || u.Host == "" {
		return s
	}

	u.Scheme = strings.ToLower(u.Scheme)

	// Lowercase the host and drop default ports.
	host := strings.ToLower(u.Hostname())
	if port := u.Port(); port != "" {
		if (u.Scheme == "https" && port != "443") ||
			(u.Scheme == "http" && port != "80") {
			host = host + ":" + port
		}
	}
	u.Host = host

	// Drop the fragment — it never affects identity.
	u.Fragment = ""

	// Strip trailing slash from non-root paths so "/a/" and "/a" match.
	if len(u.Path) > 1 {
		u.Path = strings.TrimRight(u.Path, "/")
	}

	// Drop tracking parameters. url.Values.Encode sorts the survivors,
	// so query order does not affect identity.
	q := u.Query()
	for k := range q {
		lk := strings.ToLower(k)
		if strings.HasPrefix(lk, "utm_") || exactTrackingParams[lk] {
			q.Del(k)
		}
	}
	u.RawQuery = q.Encode()

	return u.String()
}
