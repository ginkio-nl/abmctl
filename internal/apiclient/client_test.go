package apiclient

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
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
	resources, err := c.GetList(context.Background(), "/mdmServers", nil, false)
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

func TestGetListFollowsPaginationWhenAll(t *testing.T) {
	pages := 0
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pages++
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page") == "2" {
			_, _ = w.Write([]byte(`{"data":[{"id":"2","type":"mdmServers"}],"links":{"self":"x"}}`))
			return
		}
		fmt.Fprintf(w, `{"data":[{"id":"1","type":"mdmServers"}],"links":{"self":"x","next":"%s/mdmServers?page=2"}}`, srv.URL)
	}))
	defer srv.Close()

	c := New(srv.URL, staticTokens{}, srv.Client())

	// all=false: stop after first page.
	resources, err := c.GetList(context.Background(), "/mdmServers", nil, false)
	if err != nil {
		t.Fatalf("GetList: %v", err)
	}
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource without --all, got %d", len(resources))
	}

	// all=true: follow links.next.
	resources, err = c.GetList(context.Background(), "/mdmServers", nil, true)
	if err != nil {
		t.Fatalf("GetList (all): %v", err)
	}
	if len(resources) != 2 {
		t.Fatalf("expected 2 resources with --all, got %d", len(resources))
	}
	if resources[0].ID != "1" || resources[1].ID != "2" {
		t.Fatalf("unexpected resource order: %+v", resources)
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
