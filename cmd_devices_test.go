package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"abmctl/internal/apiclient"
	"abmctl/internal/coveragecache"
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
	devices, err := listServerDevices(context.Background(), client, "srv1")
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
	coverage, err := fetchCoverage(context.Background(), client, devices, nil, false)
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

func TestFetchCoverageUsesCacheUnlessRefreshing(t *testing.T) {
	requests := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests[r.URL.Path]++
		_, _ = w.Write([]byte(`{"data":[{"type":"appleCareCoverage","id":"c1","attributes":{"description":"fresh"}}],"links":{}}`))
	}))
	defer srv.Close()

	client := apiclient.New(srv.URL, staticTokens{}, srv.Client())
	cache := coveragecache.Load(filepath.Join(t.TempDir(), "coverage.json"), "client", nil)
	cache.Put("A", []apiclient.Resource{{ID: "c0", Attributes: map[string]any{"description": "cached"}}})
	devices := []apiclient.Resource{{ID: "A"}, {ID: "B"}}

	coverage, err := fetchCoverage(context.Background(), client, devices, cache, false)
	if err != nil {
		t.Fatalf("fetchCoverage: %v", err)
	}
	if got := coverage["A"][0].Str("description"); got != "cached" {
		t.Errorf("A description = %q, want cached", got)
	}
	if requests["/orgDevices/A/appleCareCoverage"] != 0 || requests["/orgDevices/B/appleCareCoverage"] != 1 {
		t.Errorf("requests = %v, want only B fetched", requests)
	}
	if _, ok := cache.Get("B"); !ok {
		t.Error("B's fetched coverage should have been cached")
	}

	coverage, err = fetchCoverage(context.Background(), client, devices, cache, true)
	if err != nil {
		t.Fatalf("fetchCoverage (refresh): %v", err)
	}
	if got := coverage["A"][0].Str("description"); got != "fresh" {
		t.Errorf("after refresh, A description = %q, want fresh", got)
	}
}

func TestDurationAcceptsDaysAndGoDurations(t *testing.T) {
	for in, want := range map[string]time.Duration{"7d": 7 * 24 * time.Hour, "0d": 0, "12h": 12 * time.Hour, "90m": 90 * time.Minute} {
		var d duration
		if err := d.UnmarshalText([]byte(in)); err != nil || time.Duration(d) != want {
			t.Errorf("%q -> %v, %v; want %v", in, time.Duration(d), err, want)
		}
	}
	for _, in := range []string{"", "d", "1.5d", "-1d", "-2h", "week"} {
		var d duration
		if err := d.UnmarshalText([]byte(in)); err == nil {
			t.Errorf("%q: expected an error", in)
		}
	}
}

func TestSortByOrderDate(t *testing.T) {
	device := func(serial, ordered string) apiclient.Resource {
		attrs := map[string]any{"serialNumber": serial}
		if ordered != "" {
			attrs["orderDateTime"] = ordered
		}
		return apiclient.Resource{ID: serial, Attributes: attrs}
	}
	devices := []apiclient.Resource{
		device("NODATE", ""),
		device("NEW", "2025-01-01T00:00:00Z"),
		device("OLD", "2019-06-01T12:00:00.5Z"),
		device("TIE-B", "2022-01-01T00:00:00Z"),
		device("TIE-A", "2022-01-01T00:00:00Z"),
	}
	sortByOrderDate(devices)

	var got []string
	for _, d := range devices {
		got = append(got, d.ID)
	}
	want := []string{"OLD", "TIE-A", "TIE-B", "NEW", "NODATE"}
	if !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}
