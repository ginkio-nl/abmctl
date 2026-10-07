// Package apiclient is a small, generic client for the Apple Business
// Manager REST API (JSON:API-shaped resources, bearer auth, link-based
// pagination).
package apiclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultBaseURL is the production Apple Business Manager API base URL.
const DefaultBaseURL = "https://api-business.apple.com/v1"

// TokenProvider supplies bearer access tokens, refreshing as needed.
type TokenProvider interface {
	AccessToken(ctx context.Context) (string, error)
}

// Client is a minimal HTTP client for the ABM API.
type Client struct {
	BaseURL string
	HTTP    *http.Client
	Tokens  TokenProvider

	// Debug, when set, receives human-readable trace lines for each request
	// (method, URL, and response status -- never headers or bodies that
	// might carry a token).
	Debug func(line string)

	// MaxRetries is how many times a request rejected with 429 Too Many
	// Requests is retried before giving up. Apple sends no Retry-After
	// header, so each retry waits RetryBaseDelay, doubling per attempt up
	// to RetryMaxDelay.
	MaxRetries     int
	RetryBaseDelay time.Duration
	RetryMaxDelay  time.Duration
}

// New returns a Client ready to call the ABM API.
func New(baseURL string, tokens TokenProvider, httpClient *http.Client) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{
		BaseURL:        strings.TrimRight(baseURL, "/"),
		HTTP:           httpClient,
		Tokens:         tokens,
		MaxRetries:     6,
		RetryBaseDelay: 2 * time.Second,
		RetryMaxDelay:  60 * time.Second,
	}
}

// Resource is a generic JSON:API-style resource. Attributes and
// Relationships are left as raw maps rather than hand-modeled structs,
// since Apple's exact field set is best treated as data: callers can
// look up known keys defensively, and --output json always shows the
// full, authoritative payload untouched.
type Resource struct {
	ID            string         `json:"id"`
	Type          string         `json:"type"`
	Attributes    map[string]any `json:"attributes,omitempty"`
	Relationships map[string]any `json:"relationships,omitempty"`
}

// Str returns Attributes[key] as a string, or "" if absent/not a string.
func (r Resource) Str(key string) string {
	v, ok := r.Attributes[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

// FirstStr returns the first non-empty string among the given attribute keys.
func (r Resource) FirstStr(keys ...string) string {
	for _, k := range keys {
		if s := r.Str(k); s != "" {
			return s
		}
	}
	return ""
}

type listEnvelope struct {
	Data  []Resource `json:"data"`
	Links struct {
		Self string `json:"self"`
		Next string `json:"next"`
	} `json:"links"`
}

type singleEnvelope struct {
	Data Resource `json:"data"`
}

// APIError represents a non-2xx response from the ABM API.
type APIError struct {
	StatusCode int
	Status     string
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("abm api: %s: %s", e.Status, snippet(e.Body))
}

// GetOne fetches a single resource at path (e.g. "/mdmServers/abc123").
func (c *Client) GetOne(ctx context.Context, path string) (Resource, error) {
	body, err := c.get(ctx, path, nil)
	if err != nil {
		return Resource{}, err
	}
	var env singleEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return Resource{}, fmt.Errorf("abm api: decoding response from %s: %w", path, err)
	}
	return env.Data, nil
}

// GetList fetches resources at path. If all is true, it follows the
// response's links.next until exhausted, accumulating every page;
// otherwise it returns just the first page.
func (c *Client) GetList(ctx context.Context, path string, query url.Values, all bool) ([]Resource, error) {
	var out []Resource
	next := c.BaseURL + normalizePath(path)
	if q := query.Encode(); q != "" {
		next += "?" + q
	}

	for next != "" {
		body, err := c.getURL(ctx, next)
		if err != nil {
			return nil, err
		}
		var env listEnvelope
		if err := json.Unmarshal(body, &env); err != nil {
			return nil, fmt.Errorf("abm api: decoding response from %s: %w", next, err)
		}
		out = append(out, env.Data...)

		if !all || env.Links.Next == "" {
			break
		}
		next = env.Links.Next
	}
	return out, nil
}

func (c *Client) get(ctx context.Context, path string, query url.Values) ([]byte, error) {
	u := c.BaseURL + normalizePath(path)
	if q := query.Encode(); q != "" {
		u += "?" + q
	}
	return c.getURL(ctx, u)
}

// getURL GETs fullURL, retrying with exponential backoff while the API
// answers 429 Too Many Requests.
func (c *Client) getURL(ctx context.Context, fullURL string) ([]byte, error) {
	delay := c.RetryBaseDelay
	for attempt := 0; ; attempt++ {
		body, err := c.getOnce(ctx, fullURL)
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusTooManyRequests || attempt >= c.MaxRetries {
			return body, err
		}

		c.trace("rate limited, retrying in %s (retry %d/%d)", delay, attempt+1, c.MaxRetries)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
		delay = min(delay*2, c.RetryMaxDelay)
	}
}

func (c *Client) getOnce(ctx context.Context, fullURL string) ([]byte, error) {
	token, err := c.Tokens.AccessToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("abm api: getting access token: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("abm api: building request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	c.trace("GET %s", fullURL)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("abm api: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("abm api: reading response: %w", err)
	}

	c.trace("-> %s (%d bytes)", resp.Status, len(body))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &APIError{StatusCode: resp.StatusCode, Status: resp.Status, Body: string(body)}
	}
	return body, nil
}

func (c *Client) trace(format string, args ...any) {
	if c.Debug != nil {
		c.Debug(fmt.Sprintf(format, args...))
	}
}

func normalizePath(path string) string {
	if !strings.HasPrefix(path, "/") {
		return "/" + path
	}
	return path
}

func snippet(body string) string {
	const max = 500
	s := strings.TrimSpace(body)
	if len(s) > max {
		return s[:max] + "...(truncated)"
	}
	return s
}
