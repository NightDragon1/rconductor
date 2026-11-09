# Adding a game with an HTTP API

Most games speak Source RCON. Some have an HTTPS API instead, such as the
Satisfactory dedicated server. RCONductor reaches them through the protocol
`http-api`: a common base in `internal/rcon/httpapi` does transport, TLS,
token, limits and parallel requests; each game adds a small **dialect** in
the code. A game profile only names the dialect; it never describes HTTP
requests of its own.

## What the base does for you

| Topic | Behaviour |
| --- | --- |
| Address | `https://<host>:<port>`, the port of the server in the panel |
| Certificate | pinned by its SHA-256 fingerprint; the RCON admin trusts it after the connection test. The pin is the fingerprint only, the end of validity is shown, never checked |
| Token | the API token of the server, sent as `Authorization: Bearer …`, only over a connection whose certificate matched the pin; never before a pin, never on a mismatch |
| Redirects | never followed: an answer 3xx is an error |
| Size | an answer over `MaxResponse` (64 KiB) is an error, not cut |
| Parallel requests | at most `Parallel` per server (default 4) |
| Context | every request ends with the command's deadline |

## The Dialect interface

```go
type Dialect interface {
    Name() string                                                  // value of http_api.dialect
    Health(ctx context.Context, c Client) error                    // needs the token, so a wrong one shows at once
    Execute(ctx context.Context, c Client, command string) (string, error)
    Players(ctx context.Context, c Client) (Players, error)        // count and limit; names if the game has them
}
```

`Client.Do` sends one request (path and JSON body); the base adds scheme,
address, `Authorization` and `Content-Type`. Look at `satisfactory.go` as the
example.

Rules for a dialect:

- Standard library only (D-071).
- Authenticate with the token the game's console makes; no password login.
- Treat every answer as foreign data (SEC-006): decode only the fields you
  need, put no answer text into errors except a short quoted code.
- Map a refused token to `rcon.ErrAuth` (the base does this for HTTP 401
  and 403).
- When the game ran a command and reports it as failed (for Satisfactory
  `returnValue: false`), return its text together with
  `rcon.ErrCommandFailed`; the console then shows it as an error with the
  text (D-073).
- `Players` returns `Names` only if the game gives names; otherwise the
  interface shows the count, and a `{player}` field in a template stays a
  free text field.

## Steps

1. Add `internal/rcon/httpapi/<game>.go` with the dialect and register it
   in `init()` next to `Satisfactory{}`.
2. Add a fake server for it to `httpapitest.Fixtures` in
   `internal/rcon/httpapi/httpapitest`. The common test suite
   (`conformance_test.go`: pinning, wrong token, redirect, size limit,
   cancel, parallel limit, failed command, expired certificate) and the
   pipeline test in `internal/commands` then run for the new dialect, too;
   a dialect without a fixture fails them.
3. Add the profile under `profiles/` with
   `protocol: { type: http-api, default_port: <port> }` and
   `http_api: { dialect: <name> }`, the game ports, `researched_version`,
   `sources` and `status: untested`. List it in `docs/user/technical.md`.
4. Run `go test -race ./internal/rcon/... ./internal/commands/ ./internal/servers/ ./internal/profiles/`.
5. If you can, test against a real server and note it in the pull request;
   only then the profile becomes `status: tested`.
