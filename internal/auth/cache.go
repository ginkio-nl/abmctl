package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ginkio-nl/abmctl/internal/config"
)

// cachedToken is the on-disk shape of a cached access token. Only one
// token is kept per cache file, one file per Client ID (see
// DefaultCachePath), so switching between Apple Business accounts doesn't
// mean thrashing a single shared cache.
type cachedToken struct {
	ClientID    string    `json:"client_id"`
	Scope       string    `json:"scope"`
	AccessToken string    `json:"access_token"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// DefaultCachePath returns the on-disk location for the given Client ID's
// cached access token: <user config dir>/abmctl/token-<client id>.json --
// e.g. ~/Library/Application Support/abmctl/token-BUSINESSAPI.xxx.json on
// macOS, or ~/.config/abmctl/token-BUSINESSAPI.xxx.json on Linux.
func DefaultCachePath(clientID string) (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("auth: locating user config dir: %w", err)
	}
	return filepath.Join(dir, "abmctl", "token-"+config.FilenameSafe(clientID)+".json"), nil
}

// loadCachedToken reads and validates the on-disk cache. It returns
// ok == false whenever the cache can't be used for any reason (missing,
// unreadable, corrupt, for a different client/scope, or expired) -- in
// every case the caller should just fall back to a normal token exchange,
// so this never returns an error.
func (t *TokenSource) loadCachedToken() (cachedToken, bool) {
	if t.cachePath == "" {
		return cachedToken{}, false
	}

	raw, err := os.ReadFile(t.cachePath)
	if err != nil {
		return cachedToken{}, false // most commonly: no cache file yet
	}

	var tok cachedToken
	if err := json.Unmarshal(raw, &tok); err != nil {
		t.trace("ignoring cached token at %s: %v", t.cachePath, err)
		return cachedToken{}, false
	}

	if tok.ClientID != t.cfg.ClientID || tok.Scope != t.cfg.Scope {
		t.trace("ignoring cached token at %s: issued for a different client/scope", t.cachePath)
		return cachedToken{}, false
	}

	if !time.Now().Add(expiryLeeway).Before(tok.ExpiresAt) {
		t.trace("ignoring cached token at %s: expired at %s", t.cachePath, tok.ExpiresAt.Format(time.RFC3339))
		return cachedToken{}, false
	}

	return tok, true
}

// saveCachedToken persists the current access token to disk so the next
// abmctl invocation can reuse it. Caching is strictly best-effort: any
// failure (permissions, disk full, no cache dir, ...) is traced via Debug
// and otherwise swallowed -- a command should never fail just because it
// couldn't write a convenience cache after successfully authenticating.
func (t *TokenSource) saveCachedToken() {
	if t.cachePath == "" {
		return
	}

	tok := cachedToken{
		ClientID:    t.cfg.ClientID,
		Scope:       t.cfg.Scope,
		AccessToken: t.accessToken,
		ExpiresAt:   t.expiresAt,
	}
	raw, err := json.MarshalIndent(tok, "", "  ")
	if err != nil {
		t.trace("not caching token: %v", err)
		return
	}

	dir := filepath.Dir(t.cachePath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.trace("not caching token: creating %s: %v", dir, err)
		return
	}

	// Write via a temp file + rename so a crash or a concurrent abmctl
	// process never observes a half-written cache file.
	tmp, err := os.CreateTemp(dir, "token-*.tmp")
	if err != nil {
		t.trace("not caching token: %v", err)
		return
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once the rename below succeeds

	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		t.trace("not caching token: writing %s: %v", tmpPath, err)
		return
	}
	if err := tmp.Close(); err != nil {
		t.trace("not caching token: %v", err)
		return
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		t.trace("not caching token: %v", err)
		return
	}
	if err := os.Rename(tmpPath, t.cachePath); err != nil {
		t.trace("not caching token: renaming into place: %v", err)
		return
	}

	t.trace("cached access token at %s (valid until %s)", t.cachePath, t.expiresAt.Format(time.RFC3339))
}
