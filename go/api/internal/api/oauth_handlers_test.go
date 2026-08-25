package api

import (
	"errors"
	"testing"
)

func TestIsHandleResolutionError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"invalid identifier", errors.New("not a valid account identifier (foo.bar): bad syntax"), true},
		{"unresolvable username", errors.New("failed to resolve username (nope.bsky.social): not found"), true},
		{"auth server metadata", errors.New("fetching auth server metadata: 502"), false},
		{"resolving auth server", errors.New("resolving auth server: connection refused"), false},
		{"par request", errors.New("auth request failed: 400"), false},
		{"unrelated", errors.New("something else"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isHandleResolutionError(tt.err); got != tt.want {
				t.Errorf("isHandleResolutionError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
