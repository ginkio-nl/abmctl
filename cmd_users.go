package main

import (
	"context"

	"abmctl/internal/apiclient"
)

// UsersCmd groups organization user commands.
type UsersCmd struct {
	List UsersListCmd `cmd:"" name:"list" help:"List organization users."`
	Get  UsersGetCmd  `cmd:"" name:"get" help:"Get a single user by ID."`
}

var userColumns = []column{
	idColumn,
	{header: "EMAIL", keys: []string{"email"}},
	{header: "FIRST NAME", keys: []string{"firstName"}},
	{header: "LAST NAME", keys: []string{"lastName"}},
	{header: "STATUS", keys: []string{"status"}},
}

// UsersListCmd lists all users in the organization.
type UsersListCmd struct {
	All bool `name:"all" help:"Follow pagination and fetch every page (default: first page only)."`
}

func (c *UsersListCmd) Run(g *Globals, client *apiclient.Client) error {
	resources, err := client.GetList(context.Background(), "/users", nil, c.All)
	if err != nil {
		return err
	}
	return printResources(g.Output, resources, userColumns)
}

// UsersGetCmd fetches one user by ID.
type UsersGetCmd struct {
	ID string `arg:"" name:"id" help:"User ID."`
}

func (c *UsersGetCmd) Run(g *Globals, client *apiclient.Client) error {
	resource, err := client.GetOne(context.Background(), "/users/"+c.ID)
	if err != nil {
		return err
	}
	return printResource(g.Output, resource, userColumns)
}
