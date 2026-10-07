package main

import (
	"context"

	"abmctl/internal/apiclient"
)

// MDMServersCmd groups MDM server commands.
type MDMServersCmd struct {
	List MDMServersListCmd `cmd:"" name:"list" help:"List MDM servers in the organization."`
	Get  MDMServersGetCmd  `cmd:"" name:"get" help:"Get a single MDM server by ID."`
}

var mdmServerColumns = []column{
	idColumn,
	{header: "NAME", keys: []string{"serverName", "name"}},
	{header: "TYPE", keys: []string{"serverType", "type"}},
	{header: "UPDATED", keys: []string{"updatedDateTime", "updatedAt"}},
}

// MDMServersListCmd lists all MDM servers.
type MDMServersListCmd struct {
	All bool `name:"all" help:"Follow pagination and fetch every page (default: first page only)."`
}

func (c *MDMServersListCmd) Run(g *Globals, client *apiclient.Client) error {
	resources, err := client.GetList(context.Background(), "/mdmServers", nil, c.All)
	if err != nil {
		return err
	}
	return printResources(g.Output, resources, mdmServerColumns)
}

// MDMServersGetCmd fetches one MDM server by ID.
type MDMServersGetCmd struct {
	ID string `arg:"" name:"id" help:"MDM server ID."`
}

func (c *MDMServersGetCmd) Run(g *Globals, client *apiclient.Client) error {
	resource, err := client.GetOne(context.Background(), "/mdmServers/"+c.ID)
	if err != nil {
		return err
	}
	return printResource(g.Output, resource, mdmServerColumns)
}
