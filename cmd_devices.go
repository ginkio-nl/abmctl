package main

import (
	"context"
	"fmt"
)

var deviceColumns = []column{
	{header: "SERIAL", keys: []string{"serialNumber"}},
	{header: "MODEL", keys: []string{"deviceModel", "model"}},
	{header: "STATUS", keys: []string{"status", "orderStatus"}},
	{header: "ADDED", keys: []string{"addedToOrgDateTime", "orderDateTime"}},
}

func dispatchDevices(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: abmctl devices <list|get> ...")
	}
	switch args[0] {
	case "list":
		return runDevicesList(args[1:])
	case "get":
		return runDevicesGet(args[1:])
	default:
		return fmt.Errorf("usage: abmctl devices <list|get> ...")
	}
}

func runDevicesList(args []string) error {
	fs, g := newFlagSet("abmctl devices list")
	all := fs.Bool("all", false, "Follow pagination and fetch every page (default: first page only).")
	mdmServerID := fs.String("mdm-server-id", "", "List only devices assigned to this MDM server.")
	if err := fs.Parse(args); err != nil {
		return err
	}

	client, err := buildClient(g)
	if err != nil {
		return err
	}

	path := "/orgDevices"
	if *mdmServerID != "" {
		path = "/mdmServers/" + *mdmServerID + "/relationships/devices"
	}

	resources, err := client.GetList(context.Background(), path, nil, *all)
	if err != nil {
		return err
	}
	return printResources(g.Output, resources, deviceColumns)
}

func runDevicesGet(args []string) error {
	fs, g := newFlagSet("abmctl devices get")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: abmctl devices get <id>")
	}
	id := fs.Arg(0)

	client, err := buildClient(g)
	if err != nil {
		return err
	}

	resource, err := client.GetOne(context.Background(), "/orgDevices/"+id)
	if err != nil {
		return err
	}
	return printResource(g.Output, resource, deviceColumns)
}
