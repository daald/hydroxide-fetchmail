# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

hydroxide is a third-party, open-source ProtonMail bridge written in Go. It runs as a headless CLI/server that translates standard protocols (SMTP, IMAP, CardDAV) into ProtonMail API requests, so any standard e-mail/CardDAV client can be pointed at it instead of talking to ProtonMail directly. It also has a `fetchmail`-style mode that pushes newly detected mail out to an external SMTP relay instead of serving IMAP.

```
+-----------------+             +-------------+  ProtonMail  +--------------+
|  E-mail client  | IMAP, SMTP  |  hydroxide  |     API      |  ProtonMail  |
|                 <------------->             <-------------->              |
+-----------------+             +-------------+              +--------------+
```

## Commands

```shell
go build ./...          # build everything
go build ./cmd/hydroxide # build just the CLI binary
go vet ./...             # static checks; keep this clean, it's enforced in CI
go test ./...            # run all tests
go test ./fetchmail/...  # run tests for a single package
go test -run TestDeliver ./fetchmail/...  # run a single test
```

There is no Makefile, linter config, or config-file format in this repo — everything is plain `go` tooling, and hydroxide itself is configured entirely via CLI flags and environment variables (see below), never a settings file.

CI: `.github/workflows/build.yml` (GitHub Actions, on the `main` branch) runs `go build`, `go vet`, and `go test` on every push/PR, and cross-compiles + publishes a GitHub Release (linux/darwin, amd64/arm64) whenever a `v*` tag is pushed. There's also a pre-existing `.build.yml` at the repo root — a sourcehut (sr.ht) build config predating the GitHub Actions workflow, doing a plain `go build -v ./...`.

## Architecture

### Single CLI entrypoint, subcommand switch

Everything is wired up in `cmd/hydroxide/main.go`: one `flag.CommandLine` for global flags, one `flag.NewFlagSet` per subcommand, and a plain `switch cmd := flag.Arg(0)`. There's no subcommand framework (no cobra) — adding a subcommand means adding a `flag.NewFlagSet` and a `case` block, following the existing ones (e.g. `sendmail`, `export-messages`, `fetchmail`) as a template. Subcommands: `auth`, `status`, `export-secret-keys`, `import-messages`, `export-messages`, `sendmail`, `fetchmail`, `smtp`, `imap`, `carddav`, `serve` (runs SMTP+IMAP+CardDAV concurrently).

### ProtonMail API client (`protonmail/`)

`protonmail.Client` is a hand-written HTTP client for ProtonMail's private API (no official SDK exists) — auth/SRP login, messages, events, contacts, calendar, crypto, users, addresses, labels. All calls go through `newRequest`/`doJSON`, which transparently re-authenticates on HTTP 401 via `Client.ReAuth`. This package is the only thing that talks to ProtonMail; every other package builds on top of it.

### Auth / credential storage (`auth/`)

`hydroxide auth <username>` does ProtonMail's full SRP login flow once, generates a random 32-byte "bridge password", and encrypts the resulting session (refresh token, mailbox password, key salts) with that key into `auth.json` (`config.Path("auth.json")`). At runtime, every protocol backend authenticates end-users with `auth.Manager.Auth(username, bridgePassword)`, which decrypts the cached session, calls `AuthRefresh`/`Unlock` to get a live `*protonmail.Client` + decrypted OpenPGP private keys, and caches that in memory per-process. The bridge password is never stored — only shown once at `auth` time — and is a completely different secret from any per-feature credential (e.g. fetchmail's outbound SMTP relay password).

### New-mail detection: polling, not push (`events/`)

ProtonMail exposes no webhook/IDLE-style push; new mail is detected by polling `GET /events/{id}` (`protonmail.Client.GetEvent`). `events.Manager`/`events.Receiver` (`events/events.go`) wrap this in a single shared 30-second-ticker poll loop per logged-in username, fanning out each `*protonmail.Event` to every registered consumer channel (IMAP and CardDAV both register against the same `events.Manager`). An `Event.Refresh & EventRefreshMail` bit signals "the event log rotated, resync everything from scratch" — every consumer of this package must handle that case, not just incremental `EventMessage` entries. The `fetchmail` package deliberately does **not** use this package (see below) — it polls via a direct `ListMessages` diff instead, since it needs to run as a single one-shot pass as well as a daemon.

### IMAP backend (`imap/`)

`imap/user.go` builds one `user` per logged-in ProtonMail account, backed by a per-user BoltDB (`imap/database/`, `config.Path(username + ".db")`) that maps ProtonMail message IDs to IMAP UIDs/sequence numbers per mailbox (keyed by ProtonMail label ID — see `systemMailboxes` in `imap/user.go`). `mailbox.sync()` does a full paginated `ListMessages` reconciliation; `user.receiveEvents` consumes the shared `events.Receiver` to apply incremental updates between reconciliations, and on `EventRefreshMail` resets everything and resyncs from scratch.

### SMTP (`smtp/`)

Only an SMTP *server* — `smtp.SendMail` takes an incoming RFC822 message, encrypts it, uploads it as a ProtonMail draft, resolves recipients' public keys, and calls `Client.SendMessage`. This is reused directly by both the `smtp` subcommand (server) and the `sendmail` subcommand (one-shot `sendmail(1)`-style CLI). It is unrelated to `fetchmail`'s outbound SMTP *client* role (see below) — hydroxide has never had outbound-SMTP-client code before `fetchmail`.

### Export/import (`exports/`, `imports/`)

`exports.WriteMessage` (exported so other packages can reuse it — originally private, exported specifically for `fetchmail`) assembles a decrypted ProtonMail message into an RFC822 MIME message. `exports.ExportMessage`/`ExportMessageMbox`/`ExportConversationMbox` build on it for the `export-messages` CLI subcommand (mbox output). `imports.ImportMessage` is the inverse: parses RFC822, re-encrypts, and calls `Client.Import`.

### Fetchmail (`fetchmail/`)

Forwards newly detected mail to an external SMTP relay instead of serving IMAP, borrowing classic `fetchmail`'s vocabulary/defaults where they fit. Key design points, since they're easy to get wrong:

- **No persistent event subscription** — `fetchmail.RunOnce` does a single paginated `ListMessages` pass per configured folder (same pagination shape as `imap/mailbox.go`'s `sync()`), diffs against a local JSON state file, and forwards anything not yet recorded. Daemon mode (`-daemon <interval>`) is just this same function called on a ticker; the default (no `-daemon`) runs one pass and exits, making it cron/systemd-timer friendly.
- **State file (`fetchmail/state.go`)** is the fetchmail-style "fetchids" UID list, keyed strictly by ProtonMail message ID (never by timestamp) — `Time`/`ForwardedAt` inside each entry are informational/decision fields only.
- **Read-only towards ProtonMail by default.** `-markseen` and `-deleteafter <days>` are explicit opt-ins to mutate ProtonMail state. `-deleteafter`'s invariant: a message can only be deleted once it's already recorded in the state file as forwarded (entries are only added *after* a successful forward), so a failed/unsent message can never age into deletion.
- Two independent secrets: the ProtonMail bridge password (`HYDROXIDE_BRIDGE_PASS`, required always) and the outbound SMTP relay's own optional AUTH password (`HYDROXIDE_FETCHMAIL_SMTP_PASS`) — these must never be conflated in code or docs.

### Config/state file conventions

There is no general settings-file format anywhere in this codebase — every subcommand is configured via CLI flags plus environment variables for secrets (`HYDROXIDE_BRIDGE_PASS`, `HYDROXIDE_FETCHMAIL_SMTP_PASS`). Persistent state on disk always goes through `config.Path(filename)` (`config/config.go`, wraps `os.UserConfigDir()/hydroxide/...`, creates the dir `0700`), using either plain JSON (`auth.json`, `fetchmail`'s state file) or BoltDB (`imap/database`) — follow whichever of those two patterns fits when adding new persisted state, rather than introducing a new config format.
