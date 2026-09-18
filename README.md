# abmctl

A small, read-only CLI for the Apple Business Manager (ABM) API. Currently
covers MDM servers and organization devices.

Commands are built on [Kong](https://github.com/alecthomas/kong): the whole
command tree, flags, env-var bindings, and `--help` text are all generated
from struct tags on `Globals`/`CLI` in `main.go`, so adding a subcommand
means adding a field and a `Run` method there -- not hand-editing a usage
string or a switch statement.

```bash
go get github.com/alecthomas/kong@latest
go mod tidy
go build -o abmctl .
```

## 1. Create an API account in Apple Business Manager

1. You need the **Organization Administrator** role to do this (the only
   role that can create API accounts). Go to **Settings > API > Add API
   Account**.
2. Give it a name (e.g. `abmctl`) and a **Role Access** of **IT
   Administrator** -- that's the role scoped to devices and device
   management services, which is all this tool touches. (Organization
   Administrator would also work, but grants far more than a read-only
   device/MDM-server tool needs.)
3. Apple generates a private key for you to download -- **save it
   immediately, it cannot be downloaded again.**
4. Note the **Client ID** (looks like `BUSINESSAPI.xxxxxxxx-xxxx-...`) and
   the **Key ID** shown alongside it. There's no separate "Team ID" field in
   the UI -- see the note below.

## 2. Prepare the private key

Apple's downloaded key sometimes isn't in the exact format Go's standard
library expects (unencrypted PKCS#8). Convert it once:

```bash
# If you got a .p12, extract the key first:
openssl pkcs12 -in abm_key.p12 -nocerts -nodes -out abm_key.pem

# Then ensure it's unencrypted PKCS#8:
openssl pkcs8 -topk8 -inform PEM -outform PEM -in abm_key.pem -out abm_key_pkcs8.pem -nocrypt
```

`abmctl` will tell you plainly if the key it's given can't be parsed, and
repeats this conversion command in the error message.

## 3. Configure credentials

Set these environment variables (or pass the equivalent flags on every
command):

```bash
export ABM_CLIENT_ID="BUSINESSAPI.xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
export ABM_KEY_ID="your-key-id"
export ABM_PRIVATE_KEY_PATH="/path/to/abm_key_pkcs8.pem"
```

`ABM_TEAM_ID` is deliberately not set here: Apple Business's API account
screen only shows a Client ID and a Key ID, no separate Team ID, and in
practice the JWT's `iss` claim is just the Client ID again. `abmctl`
defaults `--team-id`/`ABM_TEAM_ID` to your Client ID automatically -- only
set it explicitly if you ever hit an auth error suggesting your account
needs something different.

## 4. Build and try it

```bash
go get github.com/alecthomas/kong@latest  # only needed once
go mod tidy
go build -o abmctl .
./abmctl auth test
```

`auth test` runs the full flow (builds a signed JWT client assertion,
exchanges it for a bearer access token) without touching any business
endpoint -- it's the fastest way to confirm your credentials and key are
set up correctly before doing anything else.

## Commands

```
abmctl auth test

abmctl mdm-servers list [--all]
abmctl mdm-servers get <id>

abmctl devices list [--all] [--mdm-server-id ID]
abmctl devices get <id>
```

Global flags (valid on every command): `--client-id`, `--team-id`,
`--key-id`, `--private-key`, `--output table|json` (default `table`),
`--debug` (prints request/response trace to stderr, never the key or full
token), `--no-token-cache` (see below).

`--all` follows pagination (`links.next`) and fetches every page; without
it you get just the first page, which is faster for a quick look.

`--output json` prints the full, untouched API response for each resource --
useful both for scripting and for seeing fields the table view doesn't show.

## Access token caching

Every `abmctl` command is a fresh OS process, so without help it would
re-authenticate (a full JWT build + token exchange round trip to Apple)
on *every single invocation*, even two commands run seconds apart.
`abmctl` avoids that by caching the access token to disk between runs, at
`<user config dir>/abmctl/token-<client id>.json` -- for example
`~/Library/Application Support/abmctl/token-BUSINESSAPI.xxx.json` on macOS,
or `~/.config/abmctl/token-<client id>.json` on Linux. The cache file is
per Client ID, so switching between multiple ABM accounts doesn't thrash a
shared cache, and it's written with `0600` permissions since it holds a
live bearer token.

A cached token is only reused while it's still valid (Apple's tokens last
about an hour) and matches the current client ID and scope; anything else
falls back to a normal token exchange automatically. Pass `--no-token-cache`
to bypass the cache entirely -- useful for troubleshooting auth, or if
you're testing against a couple of different keys in quick succession.
Caching is best-effort: if the cache can't be read or written for any
reason (permissions, no config dir, ...), `abmctl` just falls back to
authenticating fresh rather than failing the command.

## How authentication works

Apple's Business Manager API uses OAuth2 `client_credentials` with a JWT
"client assertion" in place of a client secret (the same pattern Apple uses
for Sign in with Apple):

1. Build a JWT: header `{alg: ES256, kid: <Key ID>, typ: JWT}`, claims
   `{iss: <Client ID>, sub: <Client ID>, aud: <fixed Apple audience URL>, iat,
   exp, jti}` (ABM has no separate Team ID, so both `iss` and `sub` are your
   Client ID), signed with your EC P-256 private key.
2. POST it to `https://account.apple.com/auth/oauth2/token` as a
   `client_credentials` grant to get a bearer access token (~1h lifetime).
3. Send `Authorization: Bearer <token>` on API calls to
   `https://api-business.apple.com/v1`, re-authenticating automatically
   when the cached token is close to expiry.

## A note on accuracy

Apple's official Business Manager API reference wasn't directly reachable
while building this, so the request/response shapes here (JWT claim names,
the `aud` value, endpoint paths like `/orgDevices` and `/mdmServers`, and
the JSON:API `links.next` pagination style) are reconstructed from several
independent third-party write-ups and open-source Go/Swift clients for this
same API, cross-checked against each other (and, for the role/account setup
steps and the `iss` claim, against the real account creation flow). `auth
test` and `mdm-servers list` are the two commands to run first against your
real account -- if either behaves unexpectedly, the exact device/server
attribute names are the most likely remaining culprit, and are easy to
adjust in `cmd_devices.go` / `cmd_mdmservers.go`. `--output json` always shows the raw
payload regardless, so nothing is hidden if the table view's column guesses
are off.

## Project layout

```
main.go                     CLI struct (Kong), global flags, command tree
cmd_auth.go                 `auth test`
cmd_mdmservers.go           `mdm-servers list|get`
cmd_devices.go              `devices list|get`
output.go                   table/JSON rendering
internal/auth/              JWT client assertion + OAuth2 token exchange
internal/apiclient/         HTTP client, pagination, generic resource type
```

### Adding a new subcommand

Because the CLI is Kong-driven, adding e.g. `abmctl users list` doesn't touch
`main.go`'s command tree logic at all:

1. Add a `Users UsersCmd `cmd:"" name:"users" help:"..."`` field to the `CLI`
   struct in `main.go`.
2. Add a new `cmd_users.go` with a `UsersCmd` struct (nesting `List`/`Get`
   the same way `DevicesCmd` does) and a `Run(g *Globals, client
   *apiclient.Client) error` method on each leaf command.

`--help` at every level, env-var binding, and flag validation all come for
free from the struct tags -- there's no usage string or switch statement to
remember to update.
