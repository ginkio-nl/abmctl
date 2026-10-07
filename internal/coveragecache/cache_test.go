package coveragecache

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"abmctl/internal/apiclient"
)

var t0 = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func record(end string) apiclient.Resource {
	return apiclient.Resource{ID: "c", Type: "appleCareCoverage", Attributes: map[string]any{"status": "ACTIVE", "endDateTime": end}}
}

func newCache(t *testing.T, now *time.Time) *Cache {
	t.Helper()
	c := Load(filepath.Join(t.TempDir(), "coverage.json"), "client", nil)
	c.nowFunc = func() time.Time { return *now }
	return c
}

func TestGetHonorsMaxAge(t *testing.T) {
	now := t0
	c := newCache(t, &now)
	c.Put("dev", []apiclient.Resource{record("2030-01-01T00:00:00Z")})

	now = t0.Add(DefaultMaxAge - time.Minute)
	if _, ok := c.Get("dev"); !ok {
		t.Fatal("entry should still be fresh just before MaxAge")
	}
	now = t0.Add(DefaultMaxAge)
	if _, ok := c.Get("dev"); ok {
		t.Fatal("entry should be stale at MaxAge")
	}
}

func TestEmptyEntriesExpireSooner(t *testing.T) {
	now := t0
	c := newCache(t, &now)
	c.Put("dev", nil)

	now = t0.Add(EmptyMaxAge - time.Minute)
	if _, ok := c.Get("dev"); !ok {
		t.Fatal("empty entry should be fresh before EmptyMaxAge")
	}
	now = t0.Add(EmptyMaxAge)
	if _, ok := c.Get("dev"); ok {
		t.Fatal("empty entry should be stale at EmptyMaxAge")
	}
}

func TestEntryGoesStaleWhenCoverageEnds(t *testing.T) {
	now := t0
	c := newCache(t, &now)
	c.Put("dev", []apiclient.Resource{record("2026-10-03T00:00:00Z")})

	now = time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	if _, ok := c.Get("dev"); !ok {
		t.Fatal("entry should be fresh before its coverage ends")
	}
	now = time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	if _, ok := c.Get("dev"); ok {
		t.Fatal("entry should be stale once its coverage end date passes")
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "coverage.json")
	c := Load(path, "client", nil)
	c.Put("dev", []apiclient.Resource{record("2030-01-01T00:00:00Z")})
	c.Save()

	got, ok := Load(path, "client", nil).Get("dev")
	if !ok || len(got) != 1 || got[0].Str("endDateTime") != "2030-01-01T00:00:00Z" {
		t.Fatalf("reloaded cache: ok=%v records=%+v", ok, got)
	}
	if _, ok := Load(path, "other-client", nil).Get("dev"); ok {
		t.Fatal("a cache written for another client must not be used")
	}
}

func TestLoadIgnoresCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "coverage.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := Load(path, "client", nil)
	if _, ok := c.Get("dev"); ok {
		t.Fatal("corrupt cache should load empty")
	}
	c.Put("dev", nil)
	c.Save()
	if _, ok := Load(path, "client", nil).Get("dev"); !ok {
		t.Fatal("saving should replace a corrupt cache file")
	}
}
