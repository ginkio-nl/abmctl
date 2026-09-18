// Package auth implements the Apple Business Manager API's OAuth2
// client-credentials flow, authenticated with a JWT client assertion
// signed by the organization's ES256 API private key.
//
// Flow (mirrors Apple's "client secret as JWT" pattern used elsewhere,
// e.g. Sign in with Apple):
//  1. Build a short-lived JWT ("client assertion") with header
//     {alg: ES256, kid: <Key ID>, typ: JWT} and claims
//     {iss: <Team ID>, sub: <Client ID>, aud: <fixed Apple audience>,
//     iat, exp, jti}, signed with the private key generated in ABM.
//  2. POST it to the token endpoint as a client_credentials grant to
//     get a short-lived (~1h) bearer access token.
//  3. Use that access token as `Authorization: Bearer <token>` on API
//     calls, refreshing shortly before it expires.
//
// The JWT is built by hand with only the standard library (no JOSE/JWT
// dependency) since it's just two base64url-encoded JSON objects and an
// ES256 (ECDSA P-256 + SHA-256) signature over them.
package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultTokenURL is Apple's OAuth2 token endpoint for the Business/School Manager API.
	DefaultTokenURL = "https://account.apple.com/auth/oauth2/token"
	// DefaultAudience is the fixed `aud` claim Apple expects on the client assertion JWT.
	// Note this deliberately differs from DefaultTokenURL (it carries a /v2/ segment the
	// real token endpoint does not) -- that mismatch is intentional on Apple's side.
	DefaultAudience = "https://account.apple.com/auth/oauth2/v2/token"
	// DefaultScope is the OAuth scope requested for the Business Manager API.
	DefaultScope = "business.api"

	assertionLifetime = 5 * time.Minute
	// Renew this long before the token actually expires, to absorb clock skew
	// and the time an in-flight request takes.
	expiryLeeway = 60 * time.Second
)

// Config holds everything needed to authenticate against the ABM API.
type Config struct {
	ClientID       string // e.g. "BUSINESSAPI.xxxxxxxx-xxxx-...."
	TeamID         string // issuer shown alongside your API key in ABM
	KeyID          string // the API key's Key ID
	PrivateKeyPath string // path to the *unencrypted* PKCS#8 EC (P-256) private key, PEM-encoded

	TokenURL string
	Audience string
	Scope    string
}

func (c Config) withDefaults() Config {
	if c.TokenURL == "" {
		c.TokenURL = DefaultTokenURL
	}
	if c.Audience == "" {
		c.Audience = DefaultAudience
	}
	if c.Scope == "" {
		c.Scope = DefaultScope
	}
	return c
}

// TokenSource fetches and caches ABM API bearer access tokens, transparently
// refreshing them (via a fresh client assertion) once they're near expiry.
type TokenSource struct {
	cfg  Config
	key  *ecdsa.PrivateKey
	http *http.Client

	mu          sync.Mutex
	accessToken string
	expiresAt   time.Time

	// Debug, when set, is called with human-readable trace lines for
	// troubleshooting auth failures (never includes the private key or
	// full token values).
	Debug func(line string)
}

// NewTokenSource loads the private key from cfg.PrivateKeyPath and returns a
// ready-to-use TokenSource.
func NewTokenSource(cfg Config, httpClient *http.Client) (*TokenSource, error) {
	cfg = cfg.withDefaults()
	if cfg.ClientID == "" || cfg.TeamID == "" || cfg.KeyID == "" || cfg.PrivateKeyPath == "" {
		return nil, fmt.Errorf("auth: client ID, team ID, key ID, and private key path are all required")
	}
	key, err := LoadPrivateKey(cfg.PrivateKeyPath)
	if err != nil {
		return nil, err
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &TokenSource{cfg: cfg, key: key, http: httpClient}, nil
}

// LoadPrivateKey reads and parses an EC P-256 private key in PEM format.
// Apple issues keys that often need converting first with:
//
//	openssl pkcs8 -topk8 -inform PEM -outform PEM -in key.pem -out key-pkcs8.pem -nocrypt
func LoadPrivateKey(path string) (*ecdsa.PrivateKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("auth: reading private key %q: %w", path, err)
	}

	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, fmt.Errorf("auth: %q does not contain a PEM block", path)
	}

	hint := "if this key came straight from Apple, convert it to an unencrypted PKCS#8 PEM first: " +
		"openssl pkcs8 -topk8 -inform PEM -outform PEM -in <key> -out key-pkcs8.pem -nocrypt"

	if strings.Contains(block.Type, "ENCRYPTED") {
		return nil, fmt.Errorf("auth: %q is an encrypted private key; %s", path, hint)
	}

	key, err := parseECKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("auth: could not parse %q as a PKCS#8 or SEC1 EC private key; %s (%w)", path, hint, err)
	}
	if key.Curve != elliptic.P256() {
		return nil, fmt.Errorf("auth: %q is not a P-256 (prime256v1) key; Apple Business Manager API keys must be EC P-256", path)
	}
	return key, nil
}

func parseECKey(der []byte) (*ecdsa.PrivateKey, error) {
	if key, err := x509.ParsePKCS8PrivateKey(der); err == nil {
		ecKey, ok := key.(*ecdsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("PKCS#8 key is %T, not an EC key", key)
		}
		return ecKey, nil
	}
	return x509.ParseECPrivateKey(der)
}

// AccessToken returns a valid bearer token, fetching a new one if the cached
// one is missing or about to expire.
func (t *TokenSource) AccessToken(ctx context.Context) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.accessToken != "" && time.Now().Add(expiryLeeway).Before(t.expiresAt) {
		return t.accessToken, nil
	}

	assertion, err := t.buildClientAssertion(time.Now())
	if err != nil {
		return "", err
	}

	token, expiresIn, err := t.exchangeToken(ctx, assertion)
	if err != nil {
		return "", err
	}

	t.accessToken = token
	t.expiresAt = time.Now().Add(expiresIn)
	return t.accessToken, nil
}

// buildClientAssertion hand-builds an ES256-signed JWT: two base64url JSON
// segments (header, claims) plus a base64url P1363-encoded (r||s) signature
// over "header.claims" -- the standard JWS compact serialization.
func (t *TokenSource) buildClientAssertion(now time.Time) (string, error) {
	jti, err := randomJTI()
	if err != nil {
		return "", fmt.Errorf("auth: generating jti: %w", err)
	}

	header := map[string]string{"alg": "ES256", "kid": t.cfg.KeyID, "typ": "JWT"}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", fmt.Errorf("auth: encoding jwt header: %w", err)
	}

	claims := map[string]any{
		"iss": t.cfg.TeamID,
		"sub": t.cfg.ClientID,
		"aud": t.cfg.Audience,
		"iat": now.Unix(),
		"exp": now.Add(assertionLifetime).Unix(),
		"jti": jti,
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("auth: encoding jwt claims: %w", err)
	}

	signingInput := base64.RawURLEncoding.EncodeToString(headerJSON) + "." +
		base64.RawURLEncoding.EncodeToString(claimsJSON)

	hash := sha256.Sum256([]byte(signingInput))
	r, s, err := ecdsa.Sign(rand.Reader, t.key, hash[:])
	if err != nil {
		return "", fmt.Errorf("auth: signing client assertion: %w", err)
	}

	size := (t.key.Curve.Params().BitSize + 7) / 8 // 32 for P-256
	sig := make([]byte, 2*size)
	r.FillBytes(sig[:size])
	s.FillBytes(sig[size:])

	signed := signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)
	t.trace("built client assertion (kid=%s, iss=%s, exp=%s)", t.cfg.KeyID, t.cfg.TeamID, now.Add(assertionLifetime).Format(time.RFC3339))
	return signed, nil
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
}

func (t *TokenSource) exchangeToken(ctx context.Context, assertion string) (string, time.Duration, error) {
	form := url.Values{
		"grant_type":            {"client_credentials"},
		"client_id":             {t.cfg.ClientID},
		"client_assertion_type": {"urn:ietf:params:oauth:client-assertion-type:jwt-bearer"},
		"client_assertion":      {assertion},
		"scope":                 {t.cfg.Scope},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", 0, fmt.Errorf("auth: building token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	t.trace("POST %s (grant_type=client_credentials, scope=%s)", t.cfg.TokenURL, t.cfg.Scope)

	resp, err := t.http.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("auth: requesting token: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, fmt.Errorf("auth: reading token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("auth: token request failed: %s: %s", resp.Status, snippet(body))
	}

	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return "", 0, fmt.Errorf("auth: decoding token response: %w", err)
	}
	if tr.AccessToken == "" {
		return "", 0, fmt.Errorf("auth: token response had no access_token: %s", snippet(body))
	}
	if tr.ExpiresIn <= 0 {
		tr.ExpiresIn = 3600 // Apple's docs/practice: ~1h; fall back sanely if omitted.
	}

	t.trace("obtained access token, expires in %ds", tr.ExpiresIn)
	return tr.AccessToken, time.Duration(tr.ExpiresIn) * time.Second, nil
}

func (t *TokenSource) trace(format string, args ...any) {
	if t.Debug != nil {
		t.Debug(fmt.Sprintf(format, args...))
	}
}

func randomJTI() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func snippet(body []byte) string {
	const max = 500
	s := strings.TrimSpace(string(body))
	if len(s) > max {
		return s[:max] + "...(truncated)"
	}
	return s
}
