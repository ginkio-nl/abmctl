package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func newTestTokenSource(t *testing.T, cachePath string) *TokenSource {
	t.Helper()
	path := writeTestKey(t)
	cfg := Config{ClientID: "BUSINESSAPI.client", KeyID: "KEYID456"}.withDefaults()
	key, err := LoadPrivateKey(path)
	if err != nil {
		t.Fatalf("LoadPrivateKey: %v", err)
	}
	return &TokenSource{cfg: cfg, key: key, http: http.DefaultClient, cachePath: cachePath}
}

func TestCacheRoundTrip(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "token.json")

	writer := newTestTokenSource(t, cachePath)
	writer.accessToken = "tok-abc"
	writer.expiresAt = time.Now().Add(time.Hour)
	writer.saveCachedToken()

	reader := newTestTokenSource(t, cachePath) // fresh instance, as a new abmctl process would be
	tok, ok := reader.loadCachedToken()
	if !ok {
		t.Fatal("expected the cached token to load successfully")
	}
	if tok.AccessToken != "tok-abc" {
		t.Fatalf("AccessToken = %q, want tok-abc", tok.AccessToken)
	}
}

func TestCacheIgnoresDifferentClient(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "token.json")

	writer := newTestTokenSource(t, cachePath)
	writer.accessToken = "tok-abc"
	writer.expiresAt = time.Now().Add(time.Hour)
	writer.saveCachedToken()

	reader := newTestTokenSource(t, cachePath)
	reader.cfg.ClientID = "BUSINESSAPI.someone-else"
	if _, ok := reader.loadCachedToken(); ok {
		t.Fatal("expected a cached token for a different client to be rejected")
	}
}

func TestCacheIgnoresExpired(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "token.json")

	writer := newTestTokenSource(t, cachePath)
	writer.accessToken = "tok-abc"
	writer.expiresAt = time.Now().Add(-time.Minute) // already expired
	writer.saveCachedToken()

	reader := newTestTokenSource(t, cachePath)
	if _, ok := reader.loadCachedToken(); ok {
		t.Fatal("expected an expired cached token to be rejected")
	}
}

func TestCacheMissingFileIsNotAnError(t *testing.T) {
	reader := newTestTokenSource(t, filepath.Join(t.TempDir(), "does-not-exist.json"))
	if _, ok := reader.loadCachedToken(); ok {
		t.Fatal("expected no cached token when the cache file doesn't exist")
	}
}

func TestAccessTokenReusesCacheAcrossInstances(t *testing.T) {
	var tokenRequests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenRequests++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok-123","token_type":"Bearer","expires_in":3600}`))
	}))
	defer srv.Close()

	cachePath := filepath.Join(t.TempDir(), "token.json")
	newSource := func() *TokenSource {
		ts := newTestTokenSource(t, cachePath)
		ts.cfg.TokenURL = srv.URL
		ts.http = srv.Client()
		return ts
	}

	// First "process": no cache yet, must hit the token endpoint.
	first := newSource()
	if _, err := first.AccessToken(context.Background()); err != nil {
		t.Fatalf("AccessToken (first instance): %v", err)
	}

	// A second, entirely separate TokenSource -- simulating a second abmctl
	// invocation -- should pick up the cached token from disk rather than
	// authenticating again.
	second := newSource()
	token, err := second.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("AccessToken (second instance): %v", err)
	}
	if token != "tok-123" {
		t.Fatalf("token = %q, want tok-123", token)
	}

	if tokenRequests != 1 {
		t.Fatalf("expected exactly 1 token request across both instances, got %d", tokenRequests)
	}
}

func TestSanitizeForFilename(t *testing.T) {
	got := sanitizeForFilename("BUSINESSAPI.9703f56c-10ce/../weird id")
	for _, r := range got {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			// fine
		default:
			t.Fatalf("sanitizeForFilename produced an unsafe character %q in %q", r, got)
		}
	}
}
