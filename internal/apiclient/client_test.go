package apiclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type staticTokens struct{}

func (staticTokens) AccessToken(ctx context.Context) (string, error) { return "test-token", nil }

func TestGetListSinglePage(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"1","type":"mdmServers","attributes":{"serverName":"A"}}],"links":{"self":"x"}}`))
	}))
	defer srv.Close()

	c := New(srv.URL, staticTokens{}, srv.Client())
	resources, err := c.GetList(context.Background(), "/mdmServers", nil)
	if err != nil {
		t.Fatalf("GetList: %v", err)
	}
	if len(resources) != 1 || resources[0].ID != "1" {
		t.Fatalf("unexpected resources: %+v", resources)
	}
	if gotAuth != "Bearer test-token" {
		t.Fatalf("Authorization header = %q", gotAuth)
	}
	if got := resources[0].Str("serverName"); got != "A" {
		t.Fatalf("serverName = %q, want A", got)
	}
}

func TestGetListFollowsPagination(t *testing.T) {
	var gotLimit string
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page") == "2" {
			_, _ = w.Write([]byte(`{"data":[{"id":"2","type":"mdmServers"}],"links":{"self":"x"}}`))
			return
		}
		gotLimit = r.URL.Query().Get("limit")
		fmt.Fprintf(w, `{"data":[{"id":"1","type":"mdmServers"}],"links":{"self":"x","next":"%s/mdmServers?page=2"}}`, srv.URL)
	}))
	defer srv.Close()

	c := New(srv.URL, staticTokens{}, srv.Client())
	resources, err := c.GetList(context.Background(), "/mdmServers", nil)
	if err != nil {
		t.Fatalf("GetList: %v", err)
	}
	if len(resources) != 2 || resources[0].ID != "1" || resources[1].ID != "2" {
		t.Fatalf("unexpected resources: %+v", resources)
	}
	if gotLimit != "1000" {
		t.Fatalf("first request limit = %q, want 1000", gotLimit)
	}
}

func TestGetOneDecodesSingleResource(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":"abc","type":"orgDevices","attributes":{"serialNumber":"C02XYZ"}}}`))
	}))
	defer srv.Close()

	c := New(srv.URL, staticTokens{}, srv.Client())
	resource, err := c.GetOne(context.Background(), "/orgDevices/abc")
	if err != nil {
		t.Fatalf("GetOne: %v", err)
	}
	if resource.ID != "abc" || resource.Str("serialNumber") != "C02XYZ" {
		t.Fatalf("unexpected resource: %+v", resource)
	}
}

func TestGetOneReturnsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":[{"detail":"not found"}]}`))
	}))
	defer srv.Close()

	c := New(srv.URL, staticTokens{}, srv.Client())
	_, err := c.GetOne(context.Background(), "/orgDevices/missing")
	if err == nil {
		t.Fatal("expected an error for a 404 response")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != http.StatusNotFound {
		t.Fatalf("StatusCode = %d, want 404", apiErr.StatusCode)
	}
}

func TestGetRetriesOnTooManyRequests(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls <= 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"data":{"id":"abc","type":"orgDevices"}}`))
	}))
	defer srv.Close()

	c := New(srv.URL, staticTokens{}, srv.Client())
	c.RetryBaseDelay = time.Millisecond
	resource, err := c.GetOne(context.Background(), "/orgDevices/abc")
	if err != nil {
		t.Fatalf("GetOne: %v", err)
	}
	if resource.ID != "abc" || calls != 3 {
		t.Fatalf("resource.ID = %q after %d calls, want abc after 3", resource.ID, calls)
	}
}

func TestGetGivesUpAfterMaxRetries(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := New(srv.URL, staticTokens{}, srv.Client())
	c.MaxRetries = 2
	c.RetryBaseDelay = time.Millisecond
	_, err := c.GetOne(context.Background(), "/orgDevices/abc")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected a 429 *APIError, got %v", err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3 (1 + 2 retries)", calls)
	}
}
