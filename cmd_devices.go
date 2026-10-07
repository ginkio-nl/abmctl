package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"abmctl/internal/apiclient"
	"abmctl/internal/coveragecache"
)

// DevicesCmd groups organization device commands.
type DevicesCmd struct {
	List DevicesListCmd `cmd:"" name:"list" help:"List organization devices."`
	Get  DevicesGetCmd  `cmd:"" name:"get" help:"Get a single device by ID."`
}

var deviceColumns = []column{
	{header: "SERIAL", keys: []string{"serialNumber"}},
	{header: "MODEL", keys: []string{"deviceModel", "model"}},
	{header: "STATUS", keys: []string{"status", "orderStatus"}},
	{header: "ORDERED", keys: []string{"orderDateTime"}},
	{header: "ADDED", keys: []string{"addedToOrgDateTime"}},
}

// DevicesListCmd lists devices, optionally scoped to one MDM server.
type DevicesListCmd struct {
	All         bool   `name:"all" hidden:"" help:"No-op, kept for existing scripts: every device is always listed."`
	MDMServerID string `name:"mdm-server-id" help:"List only devices assigned to this MDM server (fetches each device individually, so it's slower for large servers)."`
	CoverageFlags
}

func (c *DevicesListCmd) Run(g *Globals, client *apiclient.Client) error {
	ctx := context.Background()
	var resources []apiclient.Resource
	var err error
	if c.MDMServerID != "" {
		resources, err = listServerDevices(ctx, client, c.MDMServerID)
	} else {
		resources, err = client.GetList(ctx, "/orgDevices", nil)
	}
	if err != nil {
		return err
	}
	sortByOrderDate(resources)
	if !c.enabled() {
		return printResources(g.Output, resources, deviceColumns)
	}
	coverage, err := c.fetch(ctx, g, client, resources)
	if err != nil {
		return err
	}
	if g.Output == "json" {
		return printJSON(withCoverage(resources, coverage))
	}
	return printResources(g.Output, resources, slices.Concat(deviceColumns, coverageColumns(coverage)))
}

// CoverageFlags are the AppleCare/warranty coverage options shared by
// `devices list` and `devices get`.
type CoverageFlags struct {
	Coverage        bool     `name:"coverage" help:"Also show AppleCare/warranty coverage. Uncached devices cost one extra request each (roughly 1s per device)."`
	RefreshCoverage bool     `name:"refresh-coverage" help:"Ignore cached coverage and fetch it fresh (implies --coverage)."`
	CoverageMaxAge  duration `name:"coverage-max-age" default:"7d" help:"Reuse cached coverage fetched within this long, e.g. 12h, 7d, 30d. Devices without coverage are re-checked after at most 1d."`
}

func (f CoverageFlags) enabled() bool { return f.Coverage || f.RefreshCoverage }

// fetch returns coverage for devices, served from the on-disk cache where
// it's fresh enough and fetched (then cached) otherwise.
func (f CoverageFlags) fetch(ctx context.Context, g *Globals, client *apiclient.Client, devices []apiclient.Resource) (map[string][]apiclient.Resource, error) {
	var debug func(string)
	if g.Debug {
		debug = func(line string) { fmt.Fprintln(os.Stderr, "[coverage]", line) }
	}
	var cache *coveragecache.Cache
	if path, err := coveragecache.DefaultPath(g.ClientID); err == nil {
		cache = coveragecache.Load(path, g.ClientID, debug)
		cache.MaxAge = time.Duration(f.CoverageMaxAge)
		defer cache.Save() // also after an error, so a long run that fails partway keeps its progress
	}
	return fetchCoverage(ctx, client, devices, cache, f.RefreshCoverage)
}

// duration is a time.Duration flag that also accepts whole days ("7d"),
// since coverage cache ages are naturally measured in days.
type duration time.Duration

func (d *duration) UnmarshalText(text []byte) error {
	s := string(text)
	if days, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil || n < 0 {
			return fmt.Errorf("invalid duration %q: want e.g. 12h, 7d, 30d", s)
		}
		*d = duration(time.Duration(n) * 24 * time.Hour)
		return nil
	}
	v, err := time.ParseDuration(s)
	if err != nil || v < 0 {
		return fmt.Errorf("invalid duration %q: want e.g. 12h, 7d, 30d", s)
	}
	*d = duration(v)
	return nil
}

// sortByOrderDate sorts devices oldest order first, with devices that have
// no (parseable) order date last. Ties fall back to serial number so the
// order is stable between runs. The API has no sort parameter, which is
// why this happens client-side.
func sortByOrderDate(devices []apiclient.Resource) {
	ordered := func(d apiclient.Resource) (time.Time, bool) {
		t, err := time.Parse(time.RFC3339, d.Str("orderDateTime"))
		return t, err == nil
	}
	slices.SortStableFunc(devices, func(a, b apiclient.Resource) int {
		ta, okA := ordered(a)
		tb, okB := ordered(b)
		switch {
		case okA && !okB:
			return -1
		case !okA && okB:
			return 1
		case okA && okB && !ta.Equal(tb):
			return ta.Compare(tb)
		}
		return strings.Compare(a.Str("serialNumber"), b.Str("serialNumber"))
	})
}

// listServerDevices returns the full device resources assigned to an MDM
// server. The server's relationships endpoint only returns linkages (type
// and ID, no attributes), and /orgDevices can't be filtered by server, so
// each linked device is fetched individually.
func listServerDevices(ctx context.Context, client *apiclient.Client, serverID string) ([]apiclient.Resource, error) {
	links, err := client.GetList(ctx, "/mdmServers/"+serverID+"/relationships/devices", nil)
	if err != nil {
		return nil, err
	}
	devices := make([]apiclient.Resource, 0, len(links))
	for _, l := range links {
		d, err := client.GetOne(ctx, "/orgDevices/"+l.ID)
		if err != nil {
			return nil, fmt.Errorf("fetching device %s: %w", l.ID, err)
		}
		devices = append(devices, d)
	}
	return devices, nil
}

// DevicesGetCmd fetches one device by ID.
type DevicesGetCmd struct {
	ID string `arg:"" name:"id" help:"Device ID."`
	CoverageFlags
}

func (c *DevicesGetCmd) Run(g *Globals, client *apiclient.Client) error {
	ctx := context.Background()
	resource, err := client.GetOne(ctx, "/orgDevices/"+c.ID)
	if err != nil {
		return err
	}
	if !c.enabled() {
		return printResource(g.Output, resource, deviceColumns)
	}
	coverage, err := c.fetch(ctx, g, client, []apiclient.Resource{resource})
	if err != nil {
		return err
	}
	if g.Output == "json" {
		return printJSON(withCoverage([]apiclient.Resource{resource}, coverage)[0])
	}
	return printResource(g.Output, resource, slices.Concat(deviceColumns, coverageColumns(coverage)))
}

// fetchCoverage returns each device's AppleCare/warranty coverage records,
// keyed by device ID. Fresh entries in cache (which may be nil) are used
// as-is unless refresh is set; everything else is fetched and cached.
// Requests run one at a time: Apple's rate limit is low enough that
// parallel requests mostly earn 429s. Progress is shown on stderr when it's
// a terminal and more than one device needs fetching.
func fetchCoverage(ctx context.Context, client *apiclient.Client, devices []apiclient.Resource, cache *coveragecache.Cache, refresh bool) (map[string][]apiclient.Resource, error) {
	coverage := make(map[string][]apiclient.Resource, len(devices))
	var todo []apiclient.Resource
	for _, d := range devices {
		if cache != nil && !refresh {
			if records, ok := cache.Get(d.ID); ok {
				coverage[d.ID] = records
				continue
			}
		}
		todo = append(todo, d)
	}

	showProgress := len(todo) > 1 && isTerminal(os.Stderr)
	for i, d := range todo {
		if showProgress {
			fmt.Fprintf(os.Stderr, "\rFetching coverage %d/%d (%d cached)", i+1, len(todo), len(devices)-len(todo))
		}
		records, err := client.GetList(ctx, "/orgDevices/"+d.ID+"/appleCareCoverage", nil)
		var apiErr *apiclient.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			records, err = nil, nil
		}
		if err != nil {
			if showProgress {
				fmt.Fprintln(os.Stderr)
			}
			return nil, fmt.Errorf("fetching coverage for device %s: %w", d.ID, err)
		}
		coverage[d.ID] = records
		if cache != nil {
			cache.Put(d.ID, records)
		}
	}
	if showProgress {
		fmt.Fprint(os.Stderr, "\r\033[K")
	}
	return coverage, nil
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// deviceWithCoverage is a device as printed by --output json --coverage:
// the untouched API resource plus its coverage records.
type deviceWithCoverage struct {
	apiclient.Resource
	Coverage []apiclient.Resource `json:"coverage"`
}

func withCoverage(devices []apiclient.Resource, coverage map[string][]apiclient.Resource) []deviceWithCoverage {
	out := make([]deviceWithCoverage, len(devices))
	for i, d := range devices {
		records := coverage[d.ID]
		if records == nil {
			records = []apiclient.Resource{}
		}
		out[i] = deviceWithCoverage{Resource: d, Coverage: records}
	}
	return out
}

// coverageColumns summarizes each device's primary coverage record (see
// primaryCoverage) for the table and CSV views.
func coverageColumns(coverage map[string][]apiclient.Resource) []column {
	attr := func(key string) func(apiclient.Resource) string {
		return func(d apiclient.Resource) string { return primaryCoverage(coverage[d.ID]).Str(key) }
	}
	return []column{
		{header: "COVERAGE", value: attr("description")},
		{header: "COVERAGE STATUS", value: attr("status")},
		{header: "COVERAGE END", value: attr("endDateTime")},
	}
}

// primaryCoverage picks the one coverage record to show for a device: an
// ACTIVE record over an inactive one, then the latest end date, then the
// latest start date. It returns a zero Resource when there are no records.
// Apple's timestamps are UTC ISO 8601, so they compare correctly as strings.
func primaryCoverage(records []apiclient.Resource) apiclient.Resource {
	var best apiclient.Resource
	for i, r := range records {
		if i == 0 || coverageRank(r).after(coverageRank(best)) {
			best = r
		}
	}
	return best
}

type rank struct {
	active     bool
	end, start string
}

func coverageRank(r apiclient.Resource) rank {
	return rank{active: r.Str("status") == "ACTIVE", end: r.Str("endDateTime"), start: r.Str("startDateTime")}
}

func (a rank) after(b rank) bool {
	if a.active != b.active {
		return a.active
	}
	if a.end != b.end {
		return a.end > b.end
	}
	return a.start > b.start
}
