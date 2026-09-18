// Command abmctl is a small, read-only CLI for the Apple Business Manager
// API, currently covering MDM servers and organization devices.
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

	"github.com/alecthomas/kong"

	"abmctl/internal/apiclient"
	"abmctl/internal/auth"
)

// Globals holds the flags/env vars shared by every subcommand: ABM
// credentials, API endpoints, and output preferences. Kong makes every
// field here usable anywhere on the command line, before or after the
// subcommand path.
type Globals struct {
	ClientID       string `name:"client-id" env:"ABM_CLIENT_ID" required:"" help:"Apple Business Manager API client ID (looks like BUSINESSAPI.xxxxxxxx-...)."`
	TeamID         string `name:"team-id" env:"ABM_TEAM_ID" help:"JWT issuer override. ABM's UI has no separate Team ID field -- leave unset and it defaults to --client-id."`
	KeyID          string `name:"key-id" env:"ABM_KEY_ID" required:"" help:"API key ID from Apple Business Manager."`
	PrivateKeyPath string `name:"private-key" env:"ABM_PRIVATE_KEY_PATH" required:"" type:"path" help:"Path to the unencrypted PKCS#8 EC (P-256) private key (.pem)."`

	BaseURL  string `name:"base-url" env:"ABM_BASE_URL" default:"${defaultBaseURL}" hidden:"" help:"ABM API base URL."`
	TokenURL string `name:"token-url" env:"ABM_TOKEN_URL" default:"${defaultTokenURL}" hidden:"" help:"OAuth2 token endpoint."`
	Audience string `name:"token-audience" env:"ABM_TOKEN_AUDIENCE" default:"${defaultAudience}" hidden:"" help:"Client assertion 'aud' claim."`
	Scope    string `name:"scope" env:"ABM_SCOPE" default:"${defaultScope}" hidden:"" help:"OAuth2 scope requested."`

	Output       string `name:"output" short:"o" enum:"table,json" default:"table" help:"Output format: table or json."`
	Debug        bool   `name:"debug" help:"Print request/response trace to stderr for troubleshooting auth and API calls."`
	NoTokenCache bool   `name:"no-token-cache" help:"Always fetch a fresh access token instead of reusing one cached on disk from a previous run."`
}

// CLI is the root command. Each field tagged cmd:"" is one command (or,
// nested further, a subcommand); Kong builds the "abmctl <command>
// <subcommand>" tree directly from this shape.
type CLI struct {
	Globals

	Auth       AuthCmd       `cmd:"" name:"auth" help:"Authentication utilities."`
	MDMServers MDMServersCmd `cmd:"" name:"mdm-servers" help:"List and inspect MDM servers."`
	Devices    DevicesCmd    `cmd:"" name:"devices" help:"List and inspect organization devices."`
}

func main() {
	var cli CLI
	parser := kong.Must(&cli,
		kong.Name("abmctl"),
		kong.Description("A CLI for the Apple Business Manager API (read-only)."),
		kong.UsageOnError(),
		kong.Vars{
			"defaultBaseURL":  apiclient.DefaultBaseURL,
			"defaultTokenURL": auth.DefaultTokenURL,
			"defaultAudience": auth.DefaultAudience,
			"defaultScope":    auth.DefaultScope,
		},
	)

	kctx, err := parser.Parse(os.Args[1:])
	parser.FatalIfErrorf(err)

	client, err := buildClient(&cli.Globals)
	if err != nil {
		fmt.Fprintln(os.Stderr, "abmctl:", err)
		os.Exit(1)
	}

	err = kctx.Run(&cli.Globals, client)
	kctx.FatalIfErrorf(err)
}

// buildClient wires up the token source and API client from parsed globals.
// Auth setup failures (bad key, missing file, ...) are surfaced immediately
// with a clear message rather than as a generic HTTP error later.
func buildClient(g *Globals) (*apiclient.Client, error) {
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
