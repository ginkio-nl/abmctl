package main

import (
	"fmt"

	"abmctl/internal/apiclient"
	"abmctl/internal/config"
)

// AccountsCmd groups config-file account management commands.
type AccountsCmd struct {
	List       AccountsListCmd       `cmd:"" name:"list" help:"List the accounts defined in the config file."`
	SetDefault AccountsSetDefaultCmd `cmd:"" name:"set-default" help:"Set the config file's default account."`
}

// loadConfigFile resolves g.ConfigPath (or the default location) and loads
// it, producing a clear error either way: an explicit --config must exist,
// while the default path just means "no config file" if missing.
func loadConfigFile(g *Globals) (*config.File, string, error) {
	path := g.ConfigPath
	explicit := path != ""
	if !explicit {
		p, err := config.DefaultPath()
		if err != nil {
			return nil, "", err
		}
		path = p
	}

	file, err := config.Load(path)
	if err != nil {
		return nil, "", err
	}
	if file == nil {
		if explicit {
			return nil, "", fmt.Errorf("config file not found: %s", path)
		}
		return nil, "", fmt.Errorf("no config file found at %s -- see --config, or use --client-id/--key-id/--private-key (or ABM_* env vars) instead", path)
	}
	return file, path, nil
}

var accountColumns = []column{
	idColumn,
	{header: "CLIENT ID", keys: []string{"client_id"}},
	{header: "KEY ID", keys: []string{"key_id"}},
	{header: "DEFAULT", keys: []string{"default"}},
}

// AccountsListCmd lists the accounts defined in the config file (see
// --config/--account). Like every "accounts" subcommand, it only reads/
// writes that file -- it needs no ABM credentials of its own, so main
// special-cases the whole "accounts" command group to skip credential/
// client setup entirely.
type AccountsListCmd struct{}

func (c *AccountsListCmd) Run(g *Globals) error {
	file, _, err := loadConfigFile(g)
	if err != nil {
		return err
	}

	resources := make([]apiclient.Resource, 0, len(file.List()))
	for _, a := range file.List() {
		def := "false"
		if a.IsDefault {
			def = "true"
		}
		resources = append(resources, apiclient.Resource{
			ID:   a.Name,
			Type: "account",
			Attributes: map[string]any{
				"client_id": a.ClientID,
				"key_id":    a.KeyID,
				"default":   def,
			},
		})
	}
	return printResources(g.Output, resources, accountColumns)
}

// AccountsSetDefaultCmd sets which account is used when --account/
// ABM_ACCOUNT isn't given.
type AccountsSetDefaultCmd struct {
	Name string `arg:"" name:"name" help:"Account name to make the default (see 'accounts list')."`
}

func (c *AccountsSetDefaultCmd) Run(g *Globals) error {
	file, path, err := loadConfigFile(g)
	if err != nil {
		return err
	}

	if err := file.SetDefault(c.Name); err != nil {
		return err
	}
	if err := file.Save(); err != nil {
		return err
	}

	fmt.Printf("Default account set to %q in %s\n", c.Name, path)
	return nil
}
