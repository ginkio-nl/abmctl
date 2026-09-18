package main

import (
	"context"
	"fmt"
)

var mdmServerColumns = []column{
	{header: "NAME", keys: []string{"serverName", "name"}},
	{header: "TYPE", keys: []string{"serverType", "type"}},
	{header: "UPDATED", keys: []string{"updatedDateTime", "updatedAt"}},
}

func dispatchMDMServers(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: abmctl mdm-servers <list|get> ...")
	}
	switch args[0] {
	case "list":
		return runMDMServersList(args[1:])
	case "get":
		return runMDMServersGet(args[1:])
	default:
		return fmt.Errorf("usage: abmctl mdm-servers <list|get> ...")
	}
}

func runMDMServersList(args []string) error {
	fs, g := newFlagSet("abmctl mdm-servers list")
	all := fs.Bool("all", false, "Follow pagination and fetch every page (default: first page only).")
	if err := fs.Parse(args); err != nil {
		return err
	}

	client, err := buildClient(g)
	if err != nil {
		return err
	}

	resources, err := client.GetList(context.Background(), "/mdmServers", nil, *all)
	if err != nil {
		return err
	}
	return printResources(g.Output, resources, mdmServerColumns)
}

func runMDMServersGet(args []string) error {
	fs, g := newFlagSet("abmctl mdm-servers get")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: abmctl mdm-servers get <id>")
	}
	id := fs.Arg(0)

	client, err := buildClient(g)
	if err != nil {
		return err
	}

	resource, err := client.GetOne(context.Background(), "/mdmServers/"+id)
	if err != nil {
		return err
	}
	return printResource(g.Output, resource, mdmServerColumns)
}
