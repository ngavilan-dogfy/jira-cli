# jira-cli

A fast CLI for Jira Cloud in Go, with a Bubble Tea TUI. Built for people at a
terminal and for AI agents and scripts: every command has structured output.

## Build and test

```sh
make check        # go vet + tests + build (bin/jira)
make install      # build and copy to ~/.local/bin/jira (copy + mv, never overwrite in place)
make e2e          # bin/jira-e2e with the hooks in cmd/e2e_hooks.go (build tag e2e)
jira ui --demo    # the TUI on a made-up team: look at UI changes here
```

Try changes with `./bin/jira <cmd>`. Reads (ls, show, context, epics…) are
safe against a real Jira; never try mutations (create/edit/move/delete)
against a real project unless asked. Screenshots for the README come from
`jira ui --demo`, never from a real site.

## Layout

- `cmd/jira/main.go` → `cmd.Execute()`. The module is
  `github.com/ngavilan-dogfy/jira-cli`, so `go install …/cmd/jira@latest`
  gives a `jira` binary.
- `cmd/` — one file per command (cobra). `root.go`'s PersistentPreRunE loads
  the profile and builds the client (`buildClient`, OAuth refresh under a
  lock); with no configuration and a terminal it offers the wizard and then
  runs the command that was asked for (`offerSetup`); without a terminal,
  `notSetUpError` says exactly what's missing. Commands that work without
  Jira go in `needsAuth`. `output.go` has the shared output helpers.
- `jira/` — the REST v3 client (`client.go`, `client_extras.go`), types
  (`types.go`), ADF ↔ text/markdown (`adf.go`).
- `config/` — profiles in `~/.config/jira-cli/` (OAuth or API token).
- `tui/` — `jira ui` (see below). `ui/` — Lip Gloss styles shared with the CLI.
- `internal/demo/` — the made-up Jira site behind `jira ui --demo`.
- `internal/selfupdate/` — `jira update` and the update notice; the same
  files as in the other ngavilan-dogfy CLIs (change one, copy to the others).

## Conventions

1. **Three output modes on every command**: TTY → colors; pipe → TSV/plain;
   `--json` → structured JSON. No exceptions: agents depend on it.
2. **Short keys**: `normalizeKey("100")` → `PROJ-100` with the profile's
   project. Apply it to every issue-key argument.
3. **Markdown, not wiki markup**: descriptions and comments are written in
   markdown and converted with `jira.MarkdownToADF`; `jira.ADFToText` reads
   them back.
4. **Clean errors**: `jira.parseAPIError` turns API error JSON into one
   readable line. `SilenceUsage: true` on root.
5. **Retries**: `client.do()` retries 429 (any method) and 5xx (GET only)
   with backoff, honoring `Retry-After`. Don't add ad-hoc retries.
6. **Search**: `POST /rest/api/3/search/jql` returns no `total`, only
   `isLast`/`nextPageToken`; don't show counts based on a total.
   `client.Search` pages transparently (≤100 per page) up to `maxResults`.
7. **Dev info** (`GetDevStatus`, the internal `/rest/dev-status/1.0/…`) gets
   401 "scope does not match" with OAuth and works with API tokens: it's
   best effort, callers ignore its errors.
8. **Exit codes**: 0 ok · 1 error · 2 `jira wait` timeout.
9. **Help per command**: `Long` with "Output:" and "Examples:" sections. It's
   the primary documentation for people and agents.

## Setup, doctor, install, releases

- `install.sh` (POSIX sh, `curl | sh`): detects OS/arch, resolves the latest
  release through the `/releases/latest` redirect (no API), downloads
  `checksums.txt` and the `jira-<os>-<arch>` binary, verifies sha256,
  installs with copy + `mv` into `~/.local/bin`, offers to fix PATH and run
  `jira setup` (questions through `/dev/tty`), refreshes the Claude Code
  skill. No release or no build for the platform → `go install`.
  `JIRA_RELEASES_URL` points it at a fake server for testing.
- Releases: conventional commits on `main` → `.github/workflows/auto-version.yml`
  (`scripts/next-version.sh`, `build-release.sh`, `release-notes.sh`). Asset
  names are what `install.sh` and `internal/selfupdate` expect: don't change
  them separately. See CONTRIBUTING.md.
- `cmd/setup.go`: huh wizard (site → sign-in → project → extras), helpers in
  `wizard_ui.go`. No terminal or `--token-stdin` → no questions. Nothing is
  saved until the end. In huh, Esc does NOT cancel (only Ctrl+C).
- `cmd/doctor.go`: every `check` carries a `Fix`; `--json`, exit 1 when
  something fails (via `quietError`, which `Execute` doesn't print again).
- Tests: `setup_test.go` runs a fake Jira (`fakeSite`) and swaps
  `siteProbeURL`, `apiBaseFor` and `openURL` (never open the browser);
  `doctor` uses injectable `lookTool`/`probeTool` (never the real `gh` or
  `claude`). To see the wizard for real: `make e2e` + `JIRA_E2E_BASE=<fake
  Jira>`, recorded with vhs under a temporary `HOME` (never your own config).

## TUI (`tui/`)

- `app.go` — root model: a stack of screens (browse/board/detail) with
  modals on top, a footer with hints and toasts, auto refresh. Data lives in
  `store.go` (shared through `appCtx`); screens keep only keys and re-derive
  rows when `ctx.rev` changes.
- Async messages in `commands.go`/`messages.go`: commands never touch the
  store, they return messages. Writes go through `mutate()` (optimistic local
  edit + API call + refetch that confirms or reverts).
- Rendering: `adf.go` renders ADF directly (not through `jira.ADFToText`,
  which drops marks), wrapped to the width; `text.go` does all width math
  with `x/ansi`: **never `len()` on visible text**. Every line a screen
  returns must be exactly the screen's width.
- Theme (`theme.go`): the terminal's ANSI colors 1-8 plus greys mixed from
  its real background (OSC 11 query before Bubble Tea starts). Only glyphs
  that exist in programming fonts.
- `adfmd.go`: ADF → markdown for editing descriptions in `$EDITOR`, offered
  only when the round trip through `jira.MarkdownToADF` is lossless.
- Agents: `work.go` (index by key: branches/worktrees via git, PRs via `gh
  search prs`, CI/review via `gh pr view`), `agents.go` (`claude` processes →
  tty/cwd with `pgrep`/`ps`/`lsof`, state from the tail of their transcript in
  `~/.claude/projects/<cwd>`: Claude Code's internal format, best effort),
  `launch.go` (worktree + branch + prompt), `iterm.go` (AppleScript with
  argv, never interpolated).
- **Every external command goes through `sys` (`sys.go`)**; tests replace it
  with `fakeRunner` (real git only in temporary folders, everything else
  simulated) and the demo with `demoRunner` (`demo.go`).
- Tests: `fakejira_test.go` is an in-memory Jira plus a harness that drives
  the app key by key and checks every frame is exactly W×H. Mutations are
  tested there, never against a real Jira. `work_test.go` builds real git
  repos with a local origin for the launcher; `demo_test.go` walks the demo.

## Concurrency (several agents at once)

The OAuth refresh uses `config.LockProfile` (flock on
`profiles/<name>.lock`) through `jira.TokenSync`: under the lock the profile
is re-read and, if another process already refreshed, its token is reused
(Atlassian refresh tokens rotate: reusing an old one kills the session).
`config.Save` writes atomically (temp + rename).

## For AI agents

`jira context <key>` (alias `ctx`) is the command built for agents: fields,
description, all comments (chronological), links, sub-tasks, attachments,
history, available transitions, epic children, remote links, watchers,
worklogs and dev info in ONE call, as markdown (for prompts) or `--json`.
`--fast` skips the enrichment calls. Also: `jira export` (bulk, paginated),
`jira attachments KEY --download`, `jira fields -q` + `jira edit --field`.

The Claude Code skill is `cmd/skill_data/SKILL.md`, embedded in the binary
(`jira skill install`); `.claude/skills/jira/SKILL.md` must stay identical
(a test checks). Update it when commands or flags change.
