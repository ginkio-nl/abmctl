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

func coverageRecord(status, start, end string) apiclient.Resource {
	return apiclient.Resource{Type: "appleCareCoverage", Attributes: map[string]any{
		"status": status, "startDateTime": start, "endDateTime": end,
	}}
}

func TestPrimaryCoveragePrefersActiveThenLatestEnd(t *testing.T) {
	expired := coverageRecord("INACTIVE", "2023-01-01T00:00:00Z", "2024-01-01T00:00:00Z")
	activeOld := coverageRecord("ACTIVE", "2023-01-01T00:00:00Z", "2026-12-01T00:00:00Z")
	activeNew := coverageRecord("ACTIVE", "2024-01-01T00:00:00Z", "2027-12-01T00:00:00Z")

	if got := primaryCoverage([]apiclient.Resource{expired, activeOld, activeNew}); got.Str("endDateTime") != "2027-12-01T00:00:00Z" {
		t.Errorf("picked end %q, want the latest active record", got.Str("endDateTime"))
	}
	if got := primaryCoverage([]apiclient.Resource{activeOld, expired}); got.Str("status") != "ACTIVE" {
		t.Errorf("picked status %q, want ACTIVE over a later-listed inactive record", got.Str("status"))
	}
	if got := primaryCoverage(nil); got.Str("status") != "" {
		t.Errorf("picked %+v for no records, want zero Resource", got)
	}
}

func TestFetchCoverageTreatsNotFoundAsNoCoverage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/orgDevices/A/appleCareCoverage":
			_, _ = w.Write([]byte(`{"data":[{"type":"appleCareCoverage","id":"c1","attributes":{"description":"Limited Warranty"}}],"links":{}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := apiclient.New(srv.URL, staticTokens{}, srv.Client())
	devices := []apiclient.Resource{{ID: "A"}, {ID: "B"}}
	coverage, err := fetchCoverage(context.Background(), client, devices)
	if err != nil {
		t.Fatalf("fetchCoverage: %v", err)
	}
	if len(coverage["A"]) != 1 || coverage["A"][0].Str("description") != "Limited Warranty" {
		t.Errorf("coverage[A] = %+v", coverage["A"])
	}
	if len(coverage["B"]) != 0 {
		t.Errorf("coverage[B] = %+v, want none", coverage["B"])
	}
	if got := withCoverage(devices, coverage)[1].Coverage; got == nil {
		t.Error("withCoverage left a nil slice; JSON should show [] not null")
	}
}
