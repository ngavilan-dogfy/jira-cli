# Architecture

This document is a map: where things live, how a request flows through the
code, and the decisions that shaped it. It describes what is true of the code
today; if you change one of the invariants below, change this file in the
same commit.

## Bird's-eye view

`jira` is one Go binary with two faces over one REST client:

```
 terminal, scripts, agents
              │
   ┌──────────▼───────────┐                 ┌──────────────────────┐
   │ cmd/                 │                 │ tui/                 │
   │ cobra commands       │──── jira ui ───▶│ Bubble Tea app       │
   │ table · TSV · --json │                 │ store · screens      │
   └──────────────────────┘                 └──────────────────────┘
              │                                   │           │
   ┌──────────▼───────────────────────────────────▼──┐   ┌────▼──────────────┐
   │ jira/   REST v3 client · ADF ↔ markdown          │   │ tui/sys.go        │
   └──────────────────────────────────────────────────┘   │ git · gh · claude │
              │                                           └───────────────────┘
      your Jira site, Atlassian auth
```

Commands are short-lived and print once. The UI is a long-lived model that
keeps a store of issues, refreshes in the background, and also reads the
world outside Jira — local git repositories, pull requests through `gh`, and
Claude Code sessions — to show the work in flight on each ticket.

## Code map

| Path | What lives there |
|---|---|
| `cmd/jira/main.go` | Entry point: `cmd.Execute()`. |
| `cmd/` | One file per command; each registers itself in `init()`. `root.go` loads the profile and builds the client (refreshing OAuth tokens under a lock); with no configuration and a terminal it offers the setup and then runs the command that was asked for. `output.go` has the TTY/TSV/JSON helpers; `normalizeKey` turns `100` or a Jira link into a full key. |
| `cmd/` — onboarding | `setup.go` (site → sign-in → project → extras), `keyinput.go` (the secret prompt that watches the clipboard), `wizard_ui.go`, `doctor.go`, `update.go`, `skill.go` (the embedded Claude Code skill). |
| `jira/` | The REST v3 client (`client.go`, `client_extras.go`), OAuth and API-token auth (`auth.go`), types, and the conversion between Atlassian Document Format and text or markdown (`adf.go`). |
| `config/` | Profiles on disk, environment overrides, and the per-profile file lock used while refreshing OAuth tokens. |
| `tui/` | `jira ui`. `app.go` is the root model: a stack of screens (browse, board, detail) with modals on top, a footer with hints and toasts. `store.go` holds the data shared through the app context; `commands.go` and `messages.go` carry the asynchronous work. |
| `tui/` — work in flight | `work.go` indexes branches and worktrees (git) and pull requests (`gh`) by key; `agents.go` finds running `claude` processes and reads their state from the tail of their transcripts; `launch.go` creates the worktree, branch and prompt for a new agent; `iterm.go` opens it in iTerm2. |
| `tui/` — rendering | `adf.go` renders descriptions straight from ADF, marks included; `text.go` does all width arithmetic; `theme.go` builds the palette from the terminal's own colors. |
| `internal/demo` | The made-up Jira site behind `jira ui --demo`, served through an `http.RoundTripper`; `tui/demo.go` adds real git repositories in a temporary folder, simulated pull requests and two Claude sessions. |
| `internal/selfupdate` | `jira update` and the daily update notice. Shared, file for file, with the other ngavilan-dogfy CLIs. |
| `ui/` | Lip Gloss styles for command output. |
| `scripts/`, `install.sh`, `.github/workflows/` | Release pipeline, installer and the setup recording (see [CONTRIBUTING.md](CONTRIBUTING.md)). |

## How a command runs

1. `cobra` parses flags; issue keys go through `normalizeKey`.
2. `PersistentPreRunE` resolves the profile — the file on disk, or
   `JIRA_DOMAIN`/`JIRA_EMAIL`/`JIRA_TOKEN` alone — and builds the client. An
   expired OAuth access token is refreshed under the profile's lock.
3. The command calls the client. `client.do()` retries 429 on any method and
   5xx on reads, honoring `Retry-After`, and `parseAPIError` turns Jira's
   error bodies into one readable line.
4. Output goes to its destination: a styled table in a terminal, TSV when
   piped, JSON with `--json`. Nothing but data goes to stdout.
5. Failures exit 1 with the message on stderr; `jira wait` exits 2 on timeout.

## The UI's data flow

Screens never fetch or mutate directly. They return commands; commands run
off the main loop and come back as messages; the root model applies messages
to the store and bumps a revision counter, and screens re-derive their rows
from the store when it changes. Writes go through `mutate()`: an optimistic
local edit, the API call, then a refetch that confirms or reverts it. Each tab
is cached on disk, so the UI paints the last known state instantly and
refreshes in the background.

Every external program — `git`, `gh`, `ps`, `lsof`, `claude` — runs through
one interface (`sys.go`). Tests replace it with a fake runner and the demo
with a simulated one, so neither ever touches the real machine.

## Decisions

**Markdown is the writing format.** People and agents write markdown; the
CLI converts it to Atlassian Document Format with `jira.MarkdownToADF` and
reads it back with `jira.ADFToText`. The UI renders ADF directly instead,
because the text conversion drops marks; and it only offers `$EDITOR` for a
description when the round trip through markdown is lossless.

**OAuth refresh is safe under concurrency.** Atlassian rotates refresh
tokens: using an old one ends the session. Several processes — you and a few
agents — refresh under a per-profile file lock, re-read the profile inside
it, and reuse a token another process already refreshed. Profiles are written
atomically.

**Search pages without totals.** Jira's JQL search endpoint returns
`isLast` and a page token but no total; the client pages transparently up to
the requested limit and never shows counts it doesn't have.

**Development info is best effort.** Branch and pull request data from
Jira's internal dev-status endpoint works with API tokens and is refused
with OAuth scopes; callers ignore its errors. The UI's own git and `gh`
lookups don't depend on it.

**Agents' state comes from their transcripts.** Whether a Claude Code
session is working, waiting or needs approval is read from the tail of its
transcript under `~/.claude/projects/`. That format is internal to Claude
Code, so the parsing is defensive and a failure only hides the state.

**Setup validates before it saves.** Every step is checked against Jira;
nothing is written until the end. The network-facing pieces are variables
(`siteProbeURL`, `apiBaseFor`, `openURL`), so tests and the `e2e` build point
them at a fake site.

## Invariants

- Every frame the UI renders is exactly the terminal's width and height;
  widths are measured with `x/ansi`, never `len()`. The UI tests check it.
- Screens don't call the API; only commands do, and only the root model
  touches the store.
- Every external command goes through `sys`.
- Nothing in the repository contains data from a real Jira site: screenshots
  come from `--demo`, examples use made-up projects, people and keys.
- Tests never call a real Jira, and never mutate one.

## Testing

| Layer | How |
|---|---|
| Client and conversions | `jira/`: ADF ↔ markdown round trips, request building, error parsing. |
| Commands and setup | `cmd/`: a fake Jira over HTTP (`fakeSite`), injectable tool lookups for `doctor`, key prompt tests with a fake clipboard. |
| UI | `tui/fakejira_test.go`: an in-memory Jira and a harness that drives the app key by key and checks every frame's size; mutations are tested here. |
| Work in flight | `tui/work_test.go`: real git repositories with a local origin in a temporary folder; everything else through the fake runner. |
| Demo | `tui/demo_test.go` walks the demo; `internal/demo` tests the fake site. |
| Release | CI builds and vets on Linux and macOS (including the `e2e` build), shellchecks the installer and renders release notes. |
