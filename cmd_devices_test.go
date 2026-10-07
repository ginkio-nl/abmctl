package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"abmctl/internal/apiclient"
)

type staticTokens struct{}

func (staticTokens) AccessToken(ctx context.Context) (string, error) { return "test-token", nil }

func TestListServerDevicesFetchesEachLinkedDevice(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/mdmServers/srv1/relationships/devices":
			_, _ = w.Write([]byte(`{"data":[{"type":"orgDevices","id":"A"},{"type":"orgDevices","id":"B"}],"links":{}}`))
		case "/orgDevices/A":
			_, _ = w.Write([]byte(`{"data":{"type":"orgDevices","id":"A","attributes":{"serialNumber":"SA"}}}`))
		case "/orgDevices/B":
			_, _ = w.Write([]byte(`{"data":{"type":"orgDevices","id":"B","attributes":{"serialNumber":"SB"}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := apiclient.New(srv.URL, staticTokens{}, srv.Client())
	devices, err := listServerDevices(context.Background(), client, "srv1", false)
	if err != nil {
		t.Fatalf("listServerDevices: %v", err)
	}
	if len(devices) != 2 {
		t.Fatalf("got %d devices, want 2", len(devices))
	}
	for i, want := range []string{"SA", "SB"} {
		if got := devices[i].Str("serialNumber"); got != want {
			t.Errorf("devices[%d] serialNumber = %q, want %q", i, got, want)
		}
	}
}
