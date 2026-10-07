package main

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/ginkio-nl/abmctl/internal/apiclient"
	"github.com/ginkio-nl/abmctl/internal/config"
)

// AccountsCmd groups config-file account management commands.
type AccountsCmd struct {
	List       AccountsListCmd       `cmd:"" name:"list" help:"List the accounts defined in the config file."`
	SetDefault AccountsSetDefaultCmd `cmd:"" name:"set-default" help:"Set the config file's default account."`
}

// loadConfigFile resolves g.ConfigPath (or the default locations) and
// loads it, producing a clear error either way: an explicit --config must
// exist, while no file at any default location just means "no config file".
func loadConfigFile(g *Globals) (*config.File, string, error) {
	path := g.ConfigPath
	explicit := path != ""
	if !explicit {
		path = config.FindDefault()
		if path == "" {
			return nil, "", fmt.Errorf("no config file found at %s -- see --config, or use --client-id/--key-id/--private-key (or ABM_* env vars) instead", strings.Join(config.DefaultPaths(), " or "))
		}
	}

	file, err := config.Load(path)
	if err != nil {
		return nil, "", err
	}
	if file == nil {
		return nil, "", fmt.Errorf("config file not found: %s", path)
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
		// Typically a system-wide config deployed read-only by an
		// administrator or MDM: point at the per-run alternative.
		if errors.Is(err, fs.ErrPermission) {
			return fmt.Errorf("can't update %s: permission denied (it may be managed by an administrator); select an account per run with --account/ABM_ACCOUNT instead", path)
		}
		return err
	}

	fmt.Printf("Default account set to %q in %s\n", c.Name, path)
	return nil
}
