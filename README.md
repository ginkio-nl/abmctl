# abmctl

A small, dependency-free, read-only CLI for the Apple Business Manager (ABM)
API. Currently covers MDM servers and organization devices.

Built with the Go standard library only (no third-party modules) -- `go
build` works completely offline once you have the source.

## 1. Create an API account in Apple Business Manager

1. Sign in to Apple Business Manager as an Administrator or Site Manager and
   go to **Preferences > API > Get Started**.
2. Create an API account (give it a label). Apple generates a private key
   for you to download -- **save it immediately, it cannot be downloaded
   again.**
3. Note the **Client ID** (looks like `BUSINESSAPI.xxxxxxxx-xxxx-...`), the
   **Key ID**, and the **Team ID** (issuer) shown alongside it.

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
export ABM_TEAM_ID="your-team-id"
export ABM_KEY_ID="your-key-id"
export ABM_PRIVATE_KEY_PATH="/path/to/abm_key_pkcs8.pem"
```

## 4. Build and try it

```bash
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
token).

`--all` follows pagination (`links.next`) and fetches every page; without
it you get just the first page, which is faster for a quick look.

`--output json` prints the full, untouched API response for each resource --
useful both for scripting and for seeing fields the table view doesn't show.

## How authentication works

Apple's Business Manager API uses OAuth2 `client_credentials` with a JWT
"client assertion" in place of a client secret (the same pattern Apple uses
for Sign in with Apple):

1. Build a JWT: header `{alg: ES256, kid: <Key ID>, typ: JWT}`, claims
   `{iss: <Team ID>, sub: <Client ID>, aud: <fixed Apple audience URL>, iat,
   exp, jti}`, signed with your EC P-256 private key.
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
same API, cross-checked against each other. `auth test` and `mdm-servers
list` are the two commands to run first against your real account -- if
either behaves unexpectedly, the most likely culprits are the `iss` claim
(some sources suggest it may need to be the Client ID again rather than the
Team ID) or the exact device/server attribute names, both of which are easy
to adjust in `internal/auth/auth.go` and `cmd_devices.go` /
`cmd_mdmservers.go` respectively. `--output json` always shows the raw
payload regardless, so nothing is hidden if the table view's column guesses
are off.

## Project layout

```
main.go                     CLI entry point, flag parsing, command dispatch
cmd_auth.go                 `auth test`
cmd_mdmservers.go           `mdm-servers list|get`
cmd_devices.go              `devices list|get`
output.go                   table/JSON rendering
internal/auth/              JWT client assertion + OAuth2 token exchange
internal/apiclient/         HTTP client, pagination, generic resource type
```
