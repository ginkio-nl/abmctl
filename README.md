# abmctl

A small, read-only CLI for the Apple Business Manager (ABM) API. Currently
covers MDM servers, organization devices, and organization users.

Commands are built on [Kong](https://github.com/alecthomas/kong): the whole
command tree, flags, env-var bindings, and `--help` text are all generated
from struct tags on `Globals`/`CLI` in `main.go`, so adding a subcommand
means adding a field and a `Run` method there -- not hand-editing a usage
string or a switch statement.

```bash
go install github.com/bart-lute/abmctl@latest
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

There are two ways to give `abmctl` credentials; pick whichever fits how
many ABM accounts you manage.

**Environment variables or flags** -- the simplest option for a single
account:

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

**A config file** -- lets one file hold several named ABM accounts, so you
can switch between them with `--account` instead of re-exporting env vars.
See [Multiple accounts (config file)](#multiple-accounts-config-file)
below.

Flags and env vars always take priority over the config file, so you can
still override a single value (e.g. `--client-id`) on the command line even
when a config file is in use.

## 4. Install and try it

```bash
go install github.com/bart-lute/abmctl@latest
abmctl auth test
```

`go install` places `abmctl` in `$(go env GOBIN)` or `$(go env GOPATH)/bin`
-- make sure that directory is on your `PATH`. Pin a release with
`@v0.1.0` instead of `@latest`.

From a checkout, use the included `Makefile`: `make build` (`./abmctl`),
`make run`, `make test`, and `make install`. These stamp the output of
`git describe` into the binary, so `abmctl --version` shows exactly which
commit you're running.

`auth test` runs the full flow (builds a signed JWT client assertion,
exchanges it for a bearer access token) without touching any business
endpoint -- it's the fastest way to confirm your credentials and key are
set up correctly before doing anything else.

## Commands

```
abmctl auth test

abmctl accounts list
abmctl accounts set-default <name>

abmctl mdm-servers list
abmctl mdm-servers get <id>

abmctl devices list [--mdm-server-id ID] [--coverage] [--refresh-coverage] [--coverage-max-age 7d]
abmctl devices get <id> [--coverage] [--refresh-coverage] [--coverage-max-age 7d]

abmctl users list
abmctl users get <id>
```

Global flags (valid on every command): `--client-id`, `--team-id`,
`--key-id`, `--private-key`, `--config`, `--account` (see below),
`--output table|json|csv` (default `table`), `--debug` (prints
request/response trace to stderr, never the key or full token),
`--no-token-cache` (see below), and `--version`.

`list` commands always return every result. The API pages its responses
(100 per page by default); `abmctl` asks for its maximum of 1000 per page
and follows `links.next` until the last one, so up to 1000 results take a
single request. Pipe through `head` for a quick look. The old `--all` flag
is still accepted, but does nothing.

`devices list --mdm-server-id` is slower than a plain `devices list`: the
API only returns the IDs of a server's devices, so `abmctl` fetches each
device's details with a separate request.

The devices table shows `ORDERED` (`orderDateTime`, when the order was
placed) and `ADDED` (`addedToOrgDateTime`, when the device joined your
organization) as separate columns; a missing value shows as `-` in the table and an empty
cell in CSV.

`devices list` is sorted by `ORDERED`, oldest first, with devices that have
no order date last -- in every output format. The API can't sort, so
`abmctl` does this itself after fetching every device.

`--coverage` adds AppleCare/warranty coverage (including Apple's Limited
Warranty) as `COVERAGE`, `COVERAGE STATUS`, and `COVERAGE END` columns. A
device can have several coverage records; the table shows an active one
over an expired one, then the one ending last. `--output json` includes
every record under a `coverage` field on each device. It's off by default
because it costs one extra request per device -- about a second each -- and
the requests run one at a time, since Apple's rate limit is low enough that
parallel requests mostly get rejected. A progress counter is shown on
stderr while it runs. Coverage is cached between runs (see
[Coverage caching](#coverage-caching)), so only the first run is slow.

Requests rejected by Apple's rate limit (HTTP 429) are retried
automatically, waiting 2s and doubling up to 60s between attempts, so a
long `--coverage` or `--mdm-server-id` run slows down rather than failing.

`--output json` prints the full, untouched API response for each resource --
useful both for scripting and for seeing fields the table view doesn't show.

`--output csv` prints the same columns as the table view, but as
comma-separated values suitable for piping into a
spreadsheet or another tool. Unlike the table view, missing fields are
written as empty cells rather than `-`.

Device output has no separate `ID` column: a device's ID is its serial
number, so `SERIAL` is what you pass to `devices get`. Other resources
show `ID` as their first column.

## Multiple accounts (config file)

If you manage more than one ABM account, put them all in one config file
instead of re-exporting `ABM_*` env vars every time you switch:

```json
{
  "default_account": "acme",
  "accounts": {
    "acme": {
      "client_id": "BUSINESSAPI.acme-xxxx-...",
      "key_id": "acme-key-id",
      "private_key": "acme.pem"
    },
    "globex": {
      "client_id": "BUSINESSAPI.globex-xxxx-...",
      "key_id": "globex-key-id",
      "private_key": "globex.pem"
    }
  }
}
```

By default `abmctl` looks for this file at `<user config dir>/abmctl/config.json`
-- e.g. `~/Library/Application Support/abmctl/config.json` on macOS, or
`~/.config/abmctl/config.json` on Linux -- and only uses it if it happens to
exist; without one, `abmctl` behaves exactly as it did before, reading
credentials from flags/env vars. Pass `--config <path>` (or `ABM_CONFIG`) to
point at a file elsewhere instead; unlike the default path, an explicit
`--config` must exist.

A relative `private_key` path (as above) is resolved relative to the config
file's own directory, not your current directory, so a config file and its
keys can live together and be referenced from anywhere.

Which account is used:

1. `--account <name>` (or `ABM_ACCOUNT`), if given.
2. Otherwise the file's `default_account`.
3. Otherwise, if the file defines exactly one account, that one.
4. Otherwise it's ambiguous and `abmctl` errors out naming the accounts you
   can choose from.

Run `abmctl accounts list` to see every account defined in the config file
and which one is the default, and `abmctl accounts set-default <name>` to
change it (this edits `default_account` in the config file in place). Like
`list`, `set-default` doesn't need any ABM credentials itself, so both work
even in case 4 above, before you've decided which `--account` to use.

`team_id` is also a valid per-account field, for the same rare case
`--team-id`/`ABM_TEAM_ID` exists for (see above). `--client-id`,
`--key-id`, `--private-key`, and their `ABM_*` env vars still work as
before and take priority over the config file field by field -- e.g.
`--client-id` alone overrides just the client ID from the selected account,
leaving its key ID and private key from the config file untouched.

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

## Coverage caching

Coverage rarely changes, so `--coverage` caches each device's records at
`<user config dir>/abmctl/coverage-<client id>.json`, one file per Client
ID like the token cache. A device's cached coverage is reused until:

- it's older than `--coverage-max-age` (default `7d`; accepts days like
  `30d` or Go durations like `12h`),
- it's older than 1 day and the device had no coverage records (new
  devices can pick up their warranty a little after purchase), or
- one of its records has passed its end date since it was cached, so a
  cached `ACTIVE` is never shown after the coverage actually ends.

Only devices that aren't fresh in the cache are fetched, so a warm run is
about as fast as a plain `devices list`. `--refresh-coverage` ignores the
cache, fetches everything again, and updates the cache. A run that fails
partway (e.g. on a network error) still saves what it fetched, so the
next run picks up where it left off. Like the token cache it's
best-effort: if it can't be read or written, `abmctl` just fetches live.

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

## API notes

Endpoints, attribute names, and pagination follow Apple's
[Apple Business API reference](https://developer.apple.com/documentation/applebusinessapi),
and every command has been run against a real Apple Business account.
A few behaviors the reference doesn't spell out, observed in practice:

- **Pages** hold 100 results unless `limit` is set (maximum 1000), and
  responses carry no total count -- only a `links.next` while more remain.
- **Rate limiting** is strict: a handful of parallel requests already gets
  `429 RATE_LIMIT_EXCEEDED`, with no `Retry-After` or rate-limit headers.
  It recovers within about a minute, which is what `abmctl`'s backoff is
  sized for.
- **A device's ID is its serial number.**

`--output json` always shows the raw payload, so if Apple adds or renames
fields, they're visible there before the table columns catch up.

## Project layout

```
main.go                     CLI struct (Kong), global flags, command tree
cmd_auth.go                 `auth test`
cmd_accounts.go             `accounts list|set-default`
cmd_mdmservers.go           `mdm-servers list|get`
cmd_devices.go              `devices list|get`
cmd_users.go                `users list|get`
output.go                   table/JSON/CSV rendering
internal/auth/              JWT client assertion + OAuth2 token exchange
internal/apiclient/         HTTP client, pagination, generic resource type
internal/config/            multi-account config file
internal/coveragecache/     on-disk AppleCare coverage cache
```

### Adding a new subcommand

Because the CLI is Kong-driven, adding a new resource (say, a hypothetical
`abmctl widgets list`) doesn't touch `main.go`'s command tree logic at all.
`cmd_users.go` is a compact template to copy:

1. Add a ``Widgets WidgetsCmd `cmd:"" name:"widgets" help:"..."` `` field to
   the `CLI` struct in `main.go`.
2. Add a new `cmd_widgets.go` with a `WidgetsCmd` struct (nesting
   `List`/`Get` the same way `UsersCmd` does), a `[]column` slice for the
   table/CSV view, and a `Run(g *Globals, client *apiclient.Client) error`
   method on each leaf command.

`--help` at every level, env-var binding, and flag validation all come for
free from the struct tags -- there's no usage string or switch statement to
remember to update.

## License

MIT -- see [LICENSE](LICENSE).
