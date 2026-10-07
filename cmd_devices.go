package main

import (
	"context"
	"fmt"

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
	return printResources(g.Output, resources, deviceColumns)
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
	ID string `arg:"" name:"id" help:"Device ID."`
}

func (c *DevicesGetCmd) Run(g *Globals, client *apiclient.Client) error {
	resource, err := client.GetOne(context.Background(), "/orgDevices/"+c.ID)
	if err != nil {
		return err
	}
	return printResource(g.Output, resource, deviceColumns)
}
