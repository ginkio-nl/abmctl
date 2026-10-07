package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"

	"abmctl/internal/apiclient"
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
	All         bool   `name:"all" help:"Follow pagination and fetch every page (default: first page only)."`
	MDMServerID string `name:"mdm-server-id" help:"List only devices assigned to this MDM server (fetches each device individually, so it's slower for large servers)."`
	Coverage    bool   `name:"coverage" help:"Also fetch AppleCare/warranty coverage (one extra request per device, so roughly 1s per device)."`
}

func (c *DevicesListCmd) Run(g *Globals, client *apiclient.Client) error {
	ctx := context.Background()
	var resources []apiclient.Resource
	var err error
	if c.MDMServerID != "" {
		resources, err = listServerDevices(ctx, client, c.MDMServerID, c.All)
	} else {
		resources, err = client.GetList(ctx, "/orgDevices", nil, c.All)
	}
	if err != nil {
		return err
	}
	if !c.Coverage {
		return printResources(g.Output, resources, deviceColumns)
	}
	coverage, err := fetchCoverage(ctx, client, resources)
	if err != nil {
		return err
	}
	if g.Output == "json" {
		return printJSON(withCoverage(resources, coverage))
	}
	return printResources(g.Output, resources, slices.Concat(deviceColumns, coverageColumns(coverage)))
}

// listServerDevices returns the full device resources assigned to an MDM
// server. The server's relationships endpoint only returns linkages (type
// and ID, no attributes), and /orgDevices can't be filtered by server, so
// each linked device is fetched individually.
func listServerDevices(ctx context.Context, client *apiclient.Client, serverID string, all bool) ([]apiclient.Resource, error) {
	links, err := client.GetList(ctx, "/mdmServers/"+serverID+"/relationships/devices", nil, all)
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
	ID       string `arg:"" name:"id" help:"Device ID."`
	Coverage bool   `name:"coverage" help:"Also fetch AppleCare/warranty coverage."`
}

func (c *DevicesGetCmd) Run(g *Globals, client *apiclient.Client) error {
	ctx := context.Background()
	resource, err := client.GetOne(ctx, "/orgDevices/"+c.ID)
	if err != nil {
		return err
	}
	if !c.Coverage {
		return printResource(g.Output, resource, deviceColumns)
	}
	coverage, err := fetchCoverage(ctx, client, []apiclient.Resource{resource})
	if err != nil {
		return err
	}
	if g.Output == "json" {
		return printJSON(withCoverage([]apiclient.Resource{resource}, coverage)[0])
	}
	return printResource(g.Output, resource, slices.Concat(deviceColumns, coverageColumns(coverage)))
}

// fetchCoverage returns each device's AppleCare/warranty coverage records,
// keyed by device ID. Requests run one at a time: Apple's rate limit is low
// enough that parallel requests mostly earn 429s. Progress is shown on
// stderr when it's a terminal and there's more than one device.
func fetchCoverage(ctx context.Context, client *apiclient.Client, devices []apiclient.Resource) (map[string][]apiclient.Resource, error) {
	showProgress := len(devices) > 1 && isTerminal(os.Stderr)
	coverage := make(map[string][]apiclient.Resource, len(devices))
	for i, d := range devices {
		if showProgress {
			fmt.Fprintf(os.Stderr, "\rFetching coverage %d/%d", i+1, len(devices))
		}
		records, err := client.GetList(ctx, "/orgDevices/"+d.ID+"/appleCareCoverage", nil, true)
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
