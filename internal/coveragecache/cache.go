// Package coveragecache keeps AppleCare/warranty coverage records on disk
// between runs. Coverage changes rarely but costs one rate-limited request
// per device to fetch, so reusing recent answers makes `--coverage` on a
// large fleet take seconds instead of minutes.
package coveragecache

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ginkio-nl/abmctl/internal/apiclient"
	"github.com/ginkio-nl/abmctl/internal/config"
)

// DefaultMaxAge is how long a device's coverage is reused before it's
// fetched again.
const DefaultMaxAge = 7 * 24 * time.Hour

// EmptyMaxAge caps how long "no coverage records" is reused: a newly
// purchased device can pick up its warranty record a little later, and
// that's worth noticing sooner than a change to existing coverage.
const EmptyMaxAge = 24 * time.Hour

// Entry is one device's cached coverage records.
type Entry struct {
	FetchedAt time.Time            `json:"fetched_at"`
	Records   []apiclient.Resource `json:"records"`
}

type file struct {
	ClientID string           `json:"client_id"`
	Devices  map[string]Entry `json:"devices"`
}

// Cache holds one account's cached coverage. Like the token cache it's
// strictly best-effort: a cache that can't be read starts empty, and a
// failed save is traced via Debug and otherwise ignored.
type Cache struct {
	MaxAge time.Duration

	// Debug, when set, receives human-readable trace lines.
	Debug func(line string)

	path    string
	data    file
	dirty   bool
	nowFunc func() time.Time
}

// DefaultPath returns <user config dir>/abmctl/coverage-<client id>.json,
// next to the token cache.
func DefaultPath(clientID string) (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("coveragecache: locating user config dir: %w", err)
	}
	return filepath.Join(dir, "abmctl", "coverage-"+config.FilenameSafe(clientID)+".json"), nil
}

// Load reads the cache at path for clientID. It never fails: a missing,
// unreadable, corrupt, or other-account cache file yields an empty cache.
func Load(path, clientID string, debug func(string)) *Cache {
	c := &Cache{
		MaxAge:  DefaultMaxAge,
		Debug:   debug,
		path:    path,
		data:    file{ClientID: clientID, Devices: map[string]Entry{}},
		nowFunc: time.Now,
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return c // most commonly: no cache file yet
	}
	var f file
	if err := json.Unmarshal(raw, &f); err != nil {
		c.trace("ignoring coverage cache at %s: %v", path, err)
		return c
	}
	if f.ClientID != clientID || f.Devices == nil {
		c.trace("ignoring coverage cache at %s: written for a different client", path)
		return c
	}
	c.data = f
	return c
}

// Get returns deviceID's cached coverage records if they're still fresh.
func (c *Cache) Get(deviceID string) ([]apiclient.Resource, bool) {
	e, ok := c.data.Devices[deviceID]
	if !ok || !c.fresh(e) {
		return nil, false
	}
	return e.Records, true
}

// fresh reports whether e can still be used. Beyond plain age, a record
// whose end date has passed since it was fetched is stale: its status will
// have flipped from ACTIVE to INACTIVE.
func (c *Cache) fresh(e Entry) bool {
	now := c.nowFunc()
	maxAge := c.MaxAge
	if len(e.Records) == 0 {
		maxAge = min(maxAge, EmptyMaxAge)
	}
	if now.Sub(e.FetchedAt) >= maxAge {
		return false
	}
	for _, r := range e.Records {
		end, err := time.Parse(time.RFC3339, r.Str("endDateTime"))
		if err == nil && end.After(e.FetchedAt) && !end.After(now) {
			return false
		}
	}
	return true
}

// Put records freshly fetched coverage for deviceID.
func (c *Cache) Put(deviceID string, records []apiclient.Resource) {
	c.data.Devices[deviceID] = Entry{FetchedAt: c.nowFunc(), Records: records}
	c.dirty = true
}

// Save writes the cache to disk if anything changed.
func (c *Cache) Save() {
	if !c.dirty {
		return
	}
	raw, err := json.Marshal(c.data)
	if err != nil {
		c.trace("not saving coverage cache: %v", err)
		return
	}

	dir := filepath.Dir(c.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		c.trace("not saving coverage cache: creating %s: %v", dir, err)
		return
	}

	// Write via a temp file + rename so a crash or a concurrent abmctl
	// process never observes a half-written cache file.
	tmp, err := os.CreateTemp(dir, "coverage-*.tmp")
	if err != nil {
		c.trace("not saving coverage cache: %v", err)
		return
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once the rename below succeeds

	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		c.trace("not saving coverage cache: writing %s: %v", tmpPath, err)
		return
	}
	if err := tmp.Close(); err != nil {
		c.trace("not saving coverage cache: %v", err)
		return
	}
	if err := os.Rename(tmpPath, c.path); err != nil {
		c.trace("not saving coverage cache: renaming into place: %v", err)
		return
	}
	c.dirty = false
	c.trace("saved coverage for %d devices to %s", len(c.data.Devices), c.path)
}

func (c *Cache) trace(format string, args ...any) {
	if c.Debug != nil {
		c.Debug(fmt.Sprintf(format, args...))
	}
}
