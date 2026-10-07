// Command abmctl is a small, read-only CLI for the Apple Business Manager
// API, covering MDM servers, organization devices, and users.
//
// Built on Kong (github.com/alecthomas/kong): the whole command tree is
// described by the CLI struct below via struct tags, so adding a new
// subcommand is adding a field there plus a Run method -- Kong generates
// --help, flag parsing, and env-var binding from that alone.
package main

import (
	"fmt"
	"net/http"
	"os"
	"runtime/debug"
	"strings"

	"github.com/alecthomas/kong"

	"github.com/bart-lute/abmctl/internal/apiclient"
	"github.com/bart-lute/abmctl/internal/auth"
	"github.com/bart-lute/abmctl/internal/config"
)

// Globals holds the flags/env vars shared by every subcommand: ABM
// credentials, API endpoints, and output preferences. Kong makes every
// field here usable anywhere on the command line, before or after the
// subcommand path.
type Globals struct {
	ClientID       string `name:"client-id" env:"ABM_CLIENT_ID" help:"Apple Business Manager API client ID (looks like BUSINESSAPI.xxxxxxxx-...). Falls back to the config file if unset (see --config)."`
	TeamID         string `name:"team-id" env:"ABM_TEAM_ID" help:"JWT issuer override. ABM's UI has no separate Team ID field -- leave unset and it defaults to --client-id."`
	KeyID          string `name:"key-id" env:"ABM_KEY_ID" help:"API key ID from Apple Business Manager. Falls back to the config file if unset (see --config)."`
	PrivateKeyPath string `name:"private-key" env:"ABM_PRIVATE_KEY_PATH" type:"path" help:"Path to the unencrypted PKCS#8 EC (P-256) private key (.pem). Falls back to the config file if unset (see --config)."`

	ConfigPath string `name:"config" env:"ABM_CONFIG" type:"path" help:"Path to a config file holding one or more named ABM accounts, used only when --client-id/--key-id/--private-key aren't otherwise given (default: <user config dir>/abmctl/config.json, if it exists)."`
	Account    string `name:"account" env:"ABM_ACCOUNT" help:"Which account to use from the config file (default: its default_account, or its only account if there's just one)."`

	BaseURL  string `name:"base-url" env:"ABM_BASE_URL" default:"${defaultBaseURL}" hidden:"" help:"ABM API base URL."`
	TokenURL string `name:"token-url" env:"ABM_TOKEN_URL" default:"${defaultTokenURL}" hidden:"" help:"OAuth2 token endpoint."`
	Audience string `name:"token-audience" env:"ABM_TOKEN_AUDIENCE" default:"${defaultAudience}" hidden:"" help:"Client assertion 'aud' claim."`
	Scope    string `name:"scope" env:"ABM_SCOPE" default:"${defaultScope}" hidden:"" help:"OAuth2 scope requested."`

	Output       string `name:"output" short:"o" enum:"table,json,csv" default:"table" help:"Output format: table, json, or csv."`
	Debug        bool   `name:"debug" help:"Print request/response trace to stderr for troubleshooting auth and API calls."`
	NoTokenCache bool   `name:"no-token-cache" help:"Always fetch a fresh access token instead of reusing one cached on disk from a previous run."`
}

// CLI is the root command. Each field tagged cmd:"" is one command (or,
// nested further, a subcommand); Kong builds the "abmctl <command>
// <subcommand>" tree directly from this shape.
type CLI struct {
	Globals

	Version kong.VersionFlag `name:"version" help:"Print the abmctl version and exit."`

	Auth       AuthCmd       `cmd:"" name:"auth" help:"Authentication utilities."`
	Accounts   AccountsCmd   `cmd:"" name:"accounts" help:"Inspect the config file's accounts."`
	MDMServers MDMServersCmd `cmd:"" name:"mdm-servers" help:"List and inspect MDM servers."`
	Devices    DevicesCmd    `cmd:"" name:"devices" help:"List and inspect organization devices."`
	Users      UsersCmd      `cmd:"" name:"users" help:"List and inspect organization users."`
}

// version is set at build time by the Makefile (-ldflags "-X
// main.version=..."); see buildVersion for builds without it.
var version = ""

// buildVersion returns the version to report: the ldflags value if set,
// else the module version Go records in the binary (the tag for
// `go install ...@vX.Y.Z`, a pseudo-version for a build from a git
// checkout), else "dev".
func buildVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

func main() {
	var cli CLI
	parser := kong.Must(&cli,
		kong.Name("abmctl"),
		kong.Description("A CLI for the Apple Business Manager API (read-only)."),
		kong.UsageOnError(),
		kong.Vars{
			"version":         buildVersion(),
			"defaultBaseURL":  apiclient.DefaultBaseURL,
			"defaultTokenURL": auth.DefaultTokenURL,
			"defaultAudience": auth.DefaultAudience,
			"defaultScope":    auth.DefaultScope,
		},
	)

	kctx, err := parser.Parse(os.Args[1:])
	parser.FatalIfErrorf(err)

	// Every "accounts" subcommand only reads/writes the config file --
	// unlike every other command, none of them need ABM credentials, so
	// they skip config-account resolution and client setup (which would
	// otherwise fail outright for the exact case "accounts list" exists to
	// help with: a config file with several accounts and no default,
	// before you know which --account to pass).
	if kctx.Command() == "accounts" || strings.HasPrefix(kctx.Command(), "accounts ") {
		kctx.FatalIfErrorf(kctx.Run(&cli.Globals))
		return
	}

	if err := applyConfigAccount(&cli.Globals); err != nil {
		fmt.Fprintln(os.Stderr, "abmctl:", err)
		os.Exit(1)
	}

	client, err := buildClient(&cli.Globals)
	if err != nil {
		fmt.Fprintln(os.Stderr, "abmctl:", err)
		os.Exit(1)
	}

	err = kctx.Run(&cli.Globals, client)
	kctx.FatalIfErrorf(err)
}

// applyConfigAccount fills in ClientID/TeamID/KeyID/PrivateKeyPath from a
// config file account wherever flags/env vars left them empty, so a config
// file holding several named ABM accounts can stand in for always passing
// --client-id/--key-id/--private-key or setting ABM_* env vars. Flags and
// env vars always win over the config file when both are given.
//
// --config points at an explicit file, which must exist; with no --config,
// the default path is used only if it happens to exist, so abmctl without
// a config file behaves exactly as before.
func applyConfigAccount(g *Globals) error {
	path := g.ConfigPath
	explicit := path != ""
	if !explicit {
		p, err := config.DefaultPath()
		if err != nil {
			return nil
		}
		path = p
	}

	file, err := config.Load(path)
	if err != nil {
		return err
	}
	if file == nil {
		if explicit {
			return fmt.Errorf("config file not found: %s", path)
		}
		return nil
	}

	acct, name, err := file.Resolve(g.Account)
	if err != nil {
		return err
	}

	if g.ClientID == "" {
		g.ClientID = acct.ClientID
	}
	if g.TeamID == "" {
		g.TeamID = acct.TeamID
	}
	if g.KeyID == "" {
		g.KeyID = acct.KeyID
	}
	if g.PrivateKeyPath == "" {
		g.PrivateKeyPath = acct.PrivateKeyPath
	}

	if g.Debug {
		fmt.Fprintf(os.Stderr, "[config] using account %q from %s\n", name, path)
	}
	return nil
}

// buildClient wires up the token source and API client from parsed globals.
// Auth setup failures (bad key, missing file, ...) are surfaced immediately
// with a clear message rather than as a generic HTTP error later.
func buildClient(g *Globals) (*apiclient.Client, error) {
	if g.ClientID == "" || g.KeyID == "" || g.PrivateKeyPath == "" {
		return nil, fmt.Errorf("missing credentials: set --client-id/--key-id/--private-key (or ABM_CLIENT_ID/ABM_KEY_ID/ABM_PRIVATE_KEY_PATH), or configure an account in a config file (see --config/--account)")
	}

	authCfg := auth.Config{
		ClientID:       g.ClientID,
		TeamID:         g.TeamID,
		KeyID:          g.KeyID,
		PrivateKeyPath: g.PrivateKeyPath,
		TokenURL:       g.TokenURL,
		Audience:       g.Audience,
		Scope:          g.Scope,
		NoCache:        g.NoTokenCache,
	}

	httpClient := http.DefaultClient

	tokens, err := auth.NewTokenSource(authCfg, httpClient)
	if err != nil {
		return nil, err
	}
	if g.Debug {
		tokens.Debug = func(line string) { fmt.Fprintln(os.Stderr, "[auth]", line) }
	}

	client := apiclient.New(g.BaseURL, tokens, httpClient)
	if g.Debug {
		client.Debug = func(line string) { fmt.Fprintln(os.Stderr, "[api]", line) }
	}

	return client, nil
}
