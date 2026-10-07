package main

import (
	"context"

	"github.com/ginkio-nl/abmctl/internal/apiclient"
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
	{header: "UPDATED", keys: []string{"updatedDateTime", "updatedAt"}, date: true},
}

// MDMServersListCmd lists all MDM servers.
type MDMServersListCmd struct {
	All bool `name:"all" hidden:"" help:"No-op, kept for existing scripts: every result is always listed."`
}

func (c *MDMServersListCmd) Run(g *Globals, client *apiclient.Client) error {
	resources, err := client.GetList(context.Background(), "/mdmServers", nil)
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
