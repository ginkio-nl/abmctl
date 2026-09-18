package main

import (
	"context"

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
	{header: "ADDED", keys: []string{"addedToOrgDateTime", "orderDateTime"}},
}

// DevicesListCmd lists devices, optionally scoped to one MDM server.
type DevicesListCmd struct {
	All         bool   `name:"all" help:"Follow pagination and fetch every page (default: first page only)."`
	MDMServerID string `name:"mdm-server-id" help:"List only devices assigned to this MDM server."`
}

func (c *DevicesListCmd) Run(g *Globals, client *apiclient.Client) error {
	path := "/orgDevices"
	if c.MDMServerID != "" {
		path = "/mdmServers/" + c.MDMServerID + "/relationships/devices"
	}
	resources, err := client.GetList(context.Background(), path, nil, c.All)
	if err != nil {
		return err
	}
	return printResources(g.Output, resources, deviceColumns)
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
