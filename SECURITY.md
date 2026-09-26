# Security

## Reporting a vulnerability

Please report vulnerabilities privately through GitHub: **Security → Report a
vulnerability** on this repository. Don't open a public issue. You'll get an
acknowledgement within a few days and a fix, or an explanation, as soon as
it's understood.

Only the latest release receives fixes; `jira update` gets you there.

## What the CLI handles, and how

**Credentials.** Depending on how you sign in, a profile holds an API token
or OAuth access and refresh tokens, in plain text in
`~/.config/jira-cli/profiles/<profile>.yaml`, written atomically with mode
`0600`. The OAuth app's client id and secret, when you use browser login,
live next to them with the same mode. Credentials are sent only to
Atlassian, over HTTPS. Anyone who can read your home directory can read
them; on shared machines prefer `JIRA_DOMAIN`, `JIRA_EMAIL` and `JIRA_TOKEN`
from your secret manager.

**Scope of access.** The CLI acts as you, with your Jira permissions: it can
change what you can change. There is no read-only mode; give agents a
separate profile or account if they need less.

**Browser login.** OAuth uses a short-lived callback server on `localhost`
to receive the authorization code. Refresh tokens rotate; refreshes happen
under a per-profile file lock so concurrent processes don't invalidate each
other's session.

**Clipboard.** During `jira setup`, while the token prompt is on screen, the
clipboard is read a few times per second. Only a value shaped like an
Atlassian API token is taken; nothing else is kept or logged. Once the
profile is saved, the token is removed from the clipboard if it is still
there.

**Local programs.** The UI runs `git`, `gh`, `ps` and `lsof` to show the work
in flight on each ticket, passing arguments directly, never through a shell.
Starting an agent types one command into a new iTerm2 tab — `cd` into the
worktree and run `claude` with the ticket's prompt file — built from
shell-quoted paths and handed to AppleScript as an argument, never
interpolated into the script.

**Network.** The CLI talks to your Jira site, Atlassian's authentication and
API hosts, and GitHub for updates. There is no telemetry.

**Updates and the installer.** Releases are built by GitHub Actions from
this repository. `install.sh` and `jira update` verify each download against
the SHA-256 in the release's `checksums.txt` and check that the new binary
runs before replacing the old one. Checksums protect against corrupted or
truncated downloads; they are published with the release, so they are not a
signature against a compromised release.
