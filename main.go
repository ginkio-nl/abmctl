// Command abmctl is a small, dependency-free, read-only CLI for the Apple
// Business Manager API, currently covering MDM servers and organization
// devices.
//
// Usage:
//
//	abmctl auth test
//	abmctl mdm-servers list [--all]
//	abmctl mdm-servers get <id>
//	abmctl devices list [--all] [--mdm-server-id ID]
//	abmctl devices get <id>
//
// Credentials (client ID, team ID, key ID, private key path) are read from
// ABM_CLIENT_ID / ABM_TEAM_ID / ABM_KEY_ID / ABM_PRIVATE_KEY_PATH, or the
// equivalent --client-id/--team-id/--key-id/--private-key flags.
package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"

	"abmctl/internal/apiclient"
	"abmctl/internal/auth"
)

// Globals holds the values shared by every subcommand: ABM credentials,
// API endpoints, and output preferences.
type Globals struct {
	ClientID       string
	TeamID         string
	KeyID          string
	PrivateKeyPath string

	BaseURL  string
	TokenURL string
	Audience string
	Scope    string

	Output string
	Debug  bool
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "auth":
		err = dispatchAuth(os.Args[2:])
	case "mdm-servers":
		err = dispatchMDMServers(os.Args[2:])
	case "devices":
		err = dispatchDevices(os.Args[2:])
	case "help", "-h", "--help":
		printUsage()
		return
	default:
		fmt.Fprintf(os.Stderr, "abmctl: unknown command %q\n\n", os.Args[1])
		printUsage()
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "abmctl:", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprint(os.Stderr, `abmctl - a CLI for the Apple Business Manager API (read-only)

Usage:
  abmctl auth test
  abmctl mdm-servers list [--all]
  abmctl mdm-servers get <id>
  abmctl devices list [--all] [--mdm-server-id ID]
  abmctl devices get <id>

Global flags (valid on every subcommand):
  --client-id       Apple Business Manager API client ID       (env ABM_CLIENT_ID)
  --team-id         Issuer/team ID shown next to your API key  (env ABM_TEAM_ID)
  --key-id          API key ID from Apple Business Manager     (env ABM_KEY_ID)
  --private-key     Path to the unencrypted PKCS#8 EC private key (env ABM_PRIVATE_KEY_PATH)
  --output          Output format: table or json (default table)
  --debug           Print request/response trace to stderr

Run 'abmctl <command> <subcommand> --help' for flags specific to that command.
`)
}

// newFlagSet returns a FlagSet pre-populated with the global flags (each
// defaulting to its ABM_* environment variable), bound to a fresh Globals.
func newFlagSet(name string) (*flag.FlagSet, *Globals) {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	g := &Globals{}

	fs.StringVar(&g.ClientID, "client-id", os.Getenv("ABM_CLIENT_ID"), "Apple Business Manager API client ID (env ABM_CLIENT_ID).")
	fs.StringVar(&g.TeamID, "team-id", os.Getenv("ABM_TEAM_ID"), "Issuer/team ID shown next to your API key in ABM (env ABM_TEAM_ID).")
	fs.StringVar(&g.KeyID, "key-id", os.Getenv("ABM_KEY_ID"), "API key ID from Apple Business Manager (env ABM_KEY_ID).")
	fs.StringVar(&g.PrivateKeyPath, "private-key", os.Getenv("ABM_PRIVATE_KEY_PATH"), "Path to the unencrypted PKCS#8 EC private key (env ABM_PRIVATE_KEY_PATH).")

	fs.StringVar(&g.BaseURL, "base-url", envOr("ABM_BASE_URL", apiclient.DefaultBaseURL), "ABM API base URL.")
	fs.StringVar(&g.TokenURL, "token-url", envOr("ABM_TOKEN_URL", auth.DefaultTokenURL), "OAuth2 token endpoint.")
	fs.StringVar(&g.Audience, "token-audience", envOr("ABM_TOKEN_AUDIENCE", auth.DefaultAudience), "Client assertion 'aud' claim.")
	fs.StringVar(&g.Scope, "scope", envOr("ABM_SCOPE", auth.DefaultScope), "OAuth2 scope requested.")

	fs.StringVar(&g.Output, "output", "table", "Output format: table or json.")
	fs.BoolVar(&g.Debug, "debug", false, "Print request/response trace to stderr.")

	return fs, g
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// buildClient wires up the token source and API client from parsed globals.
// Auth setup failures (bad key, missing file, ...) are surfaced immediately
// with a clear message rather than as a generic HTTP error later.
func buildClient(g *Globals) (*apiclient.Client, error) {
	if g.Output != "table" && g.Output != "json" {
		return nil, fmt.Errorf("--output must be 'table' or 'json', got %q", g.Output)
	}

	authCfg := auth.Config{
		ClientID:       g.ClientID,
		TeamID:         g.TeamID,
		KeyID:          g.KeyID,
		PrivateKeyPath: g.PrivateKeyPath,
		TokenURL:       g.TokenURL,
		Audience:       g.Audience,
		Scope:          g.Scope,
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
