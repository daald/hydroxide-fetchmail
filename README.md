# hydroxide

[![Casual Maintenance Intended](https://casuallymaintained.tech/badge.svg)](https://casuallymaintained.tech/)

A third-party, open-source ProtonMail bridge. For power users only, designed to
run on a server.

hydroxide supports CardDAV, IMAP and SMTP.

Rationale:

* No GUI, only a CLI (so it runs in headless environments)
* Standard-compliant (we don't care about Microsoft Outlook)
* Fully open-source

Feel free to join the IRC channel: #emersion on Libera Chat.

## How does it work?

hydroxide is a server that translates standard protocols (SMTP, IMAP, CardDAV)
into ProtonMail API requests. It allows you to use your preferred e-mail clients
and `git-send-email` with ProtonMail.

    +-----------------+             +-------------+  ProtonMail  +--------------+
    |                 | IMAP, SMTP  |             |     API      |              |
    |  E-mail client  <------------->  hydroxide  <-------------->  ProtonMail  |
    |                 |             |             |              |              |
    +-----------------+             +-------------+              +--------------+

## Setup

### Go

hydroxide is implemented in Go. Head to [Go website](https://golang.org) for
setup information.

### Installing

Start by installing hydroxide:

```shell
git clone https://github.com/emersion/hydroxide.git
go build ./cmd/hydroxide
```

Then you'll need to login to ProtonMail via hydroxide, so that hydroxide can
retrieve e-mails from ProtonMail. You can do so with this command:

```shell
hydroxide auth <username>
```

Once you're logged in, a "bridge password" will be printed. Don't close your
terminal yet, as this password is not stored anywhere by hydroxide and will be
needed when configuring your e-mail client.

Your ProtonMail credentials are stored on disk encrypted with this bridge
password (a 32-byte random password generated when logging in).

## Usage

hydroxide can be used in multiple modes.

> Don't start hydroxide multiple times, instead you can use `hydroxide serve`.
> This requires ports 1025 (smtp), 1143 (imap), and 8080 (carddav).

### SMTP

To run hydroxide as an SMTP server:

```shell
hydroxide smtp
```

Once the bridge is started, you can configure your e-mail client with the
following settings:

* Hostname: `localhost`
* Port: 1025
* Security: none
* Username: your ProtonMail username
* Password: the bridge password (not your ProtonMail password)

### CardDAV

You must setup an HTTPS reverse proxy to forward requests to `hydroxide`.

```shell
hydroxide carddav
```

Tested on GNOME (Evolution) and Android (DAVDroid).

### IMAP

⚠️  **Warning**: IMAP support is work-in-progress. Here be dragons.

For now, it only supports unencrypted local connections.

```shell
hydroxide imap
```

### Fetchmail

Instead of waiting for an e-mail client to connect, `hydroxide fetchmail` polls
one or more ProtonMail folders and forwards newly detected mail to an external
SMTP server, similar to the classic Unix `fetchmail` tool (just targeting SMTP
instead of a local mailbox).

```shell
hydroxide fetchmail -smtp-host mail.example.com <username>
```

By default it runs a single pass and exits, making it a good fit for a cron
job or systemd timer; each run only forwards mail it hasn't forwarded yet,
using a small on-disk state file (similar to fetchmail's own UID list file,
see `-idfile`). Pass `-daemon <interval>` (e.g. `-daemon 5m`) to instead run
continuously, polling on that interval.

The first run forwards the full backlog of the configured folder(s) (default:
`Inbox`); use `-all` on a later run to ignore the state file and re-forward
everything again.

hydroxide never modifies your ProtonMail mailbox unless you opt in:
`-markseen` marks forwarded messages as read, and `-deleteafter <days>`
deletes a message from ProtonMail once it has been sitting in the state file
as forwarded for that many days (a message that failed to forward is never
deleted, since it's never recorded as forwarded).

Run `hydroxide fetchmail -h` for the full list of options (folders, SMTP
relay authentication/TLS, envelope overrides, etc). Note that the SMTP relay
password (if the relay requires auth) is a separate secret from your
ProtonMail bridge password — set it with `HYDROXIDE_FETCHMAIL_SMTP_PASS` if
you don't want to be prompted for it.

## Contributing

This project is [casually maintained]: pull requests are welcome, but the
maintainer is busy with lots of other things and will be slow to respond.

Also see [CONTRIBUTING.md].

## License

MIT

[casually maintained]: https://casuallymaintained.tech/
[CONTRIBUTING.md]: https://github.com/emersion/.github/blob/main/CONTRIBUTING.md
