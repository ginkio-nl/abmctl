package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeTestKey(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating test key: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshaling test key: %v", err)
	}
	block := &pem.Block{Type: "PRIVATE KEY", Bytes: der}

	path := filepath.Join(t.TempDir(), "key.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatalf("writing test key: %v", err)
	}
	return path
}

func TestLoadPrivateKeyRoundTrip(t *testing.T) {
	path := writeTestKey(t)
	key, err := LoadPrivateKey(path)
	if err != nil {
		t.Fatalf("LoadPrivateKey: %v", err)
	}
	if key.Curve != elliptic.P256() {
		t.Fatalf("expected P-256 key, got curve %v", key.Curve)
	}
}

func TestLoadPrivateKeyRejectsEncrypted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "key.pem")
	block := &pem.Block{Type: "ENCRYPTED PRIVATE KEY", Bytes: []byte("not-a-real-key")}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatalf("writing test key: %v", err)
	}
	if _, err := LoadPrivateKey(path); err == nil {
		t.Fatal("expected an error for an encrypted key, got nil")
	} else if !strings.Contains(err.Error(), "encrypted") {
		t.Fatalf("expected error to mention 'encrypted', got: %v", err)
	}
}

func TestConfigDefaultsTeamIDToClientID(t *testing.T) {
	cfg := Config{ClientID: "BUSINESSAPI.client", KeyID: "KEYID456"}.withDefaults()
	if cfg.TeamID != cfg.ClientID {
		t.Fatalf("TeamID = %q, want it to default to ClientID %q", cfg.TeamID, cfg.ClientID)
	}

	// An explicit TeamID is still honored (e.g. if an account ever needs one
	// different from its Client ID).
	cfg = Config{ClientID: "BUSINESSAPI.client", TeamID: "OVERRIDE", KeyID: "KEYID456"}.withDefaults()
	if cfg.TeamID != "OVERRIDE" {
		t.Fatalf("TeamID = %q, want explicit override OVERRIDE to be preserved", cfg.TeamID)
	}
}

func TestBuildClientAssertionShape(t *testing.T) {
	path := writeTestKey(t)
	cfg := Config{ClientID: "BUSINESSAPI.client", TeamID: "TEAMID123", KeyID: "KEYID456"}.withDefaults()
	key, err := LoadPrivateKey(path)
	if err != nil {
		t.Fatalf("LoadPrivateKey: %v", err)
	}
	ts := &TokenSource{cfg: cfg, key: key, http: http.DefaultClient}

	now := time.Now()
	assertion, err := ts.buildClientAssertion(now)
	if err != nil {
		t.Fatalf("buildClientAssertion: %v", err)
	}

	parts := strings.Split(assertion, ".")
	if len(parts) != 3 {
		t.Fatalf("expected a 3-part JWT, got %d parts", len(parts))
	}

	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("decoding header: %v", err)
	}
	var header map[string]string
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		t.Fatalf("unmarshaling header: %v", err)
	}
	if header["alg"] != "ES256" || header["kid"] != cfg.KeyID || header["typ"] != "JWT" {
		t.Fatalf("unexpected header: %+v", header)
	}

	claimsJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decoding claims: %v", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		t.Fatalf("unmarshaling claims: %v", err)
	}
	if claims["iss"] != cfg.TeamID {
		t.Errorf("iss = %v, want %v", claims["iss"], cfg.TeamID)
	}
	if claims["sub"] != cfg.ClientID {
		t.Errorf("sub = %v, want %v", claims["sub"], cfg.ClientID)
	}
	if claims["aud"] != DefaultAudience {
		t.Errorf("aud = %v, want %v", claims["aud"], DefaultAudience)
	}
	if claims["jti"] == "" || claims["jti"] == nil {
		t.Error("jti should not be empty")
	}

	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatalf("decoding signature: %v", err)
	}
	if len(sig) != 64 { // P-256 => 32-byte r || 32-byte s
		t.Fatalf("expected a 64-byte ES256 signature, got %d bytes", len(sig))
	}
}

func TestAccessTokenExchangeAndCache(t *testing.T) {
	var tokenRequests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenRequests++
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parsing token request form: %v", err)
		}
		if got := r.FormValue("grant_type"); got != "client_credentials" {
			t.Errorf("grant_type = %q, want client_credentials", got)
		}
		if got := r.FormValue("client_assertion_type"); got != "urn:ietf:params:oauth:client-assertion-type:jwt-bearer" {
			t.Errorf("unexpected client_assertion_type: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok-123","token_type":"Bearer","expires_in":3600}`))
	}))
	defer srv.Close()

	path := writeTestKey(t)
	cfg := Config{
		ClientID:       "BUSINESSAPI.client",
		TeamID:         "TEAMID123",
		KeyID:          "KEYID456",
		PrivateKeyPath: path,
		TokenURL:       srv.URL,
		// This test is specifically about the in-memory cache inside a single
		// TokenSource; the on-disk cache (shared across process invocations)
		// has its own dedicated tests in cache_test.go and would otherwise
		// have this test read/write the real user config directory.
		NoCache: true,
	}
	ts, err := NewTokenSource(cfg, srv.Client())
	if err != nil {
		t.Fatalf("NewTokenSource: %v", err)
	}

	token, err := ts.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("AccessToken: %v", err)
	}
	if token != "tok-123" {
		t.Fatalf("token = %q, want tok-123", token)
	}

	// Second call within the token's lifetime should be served from cache,
	// not trigger another token request.
	if _, err := ts.AccessToken(context.Background()); err != nil {
		t.Fatalf("AccessToken (cached): %v", err)
	}
	if tokenRequests != 1 {
		t.Fatalf("expected 1 token request (cached second call), got %d", tokenRequests)
	}
}
