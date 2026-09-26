<h1 align="center">jira</h1>

<p align="center"><strong>Jira Cloud in your terminal, for you and for your agents.</strong><br>
A fast UI that links every ticket to its branches, pull requests and Claude Code sessions, and a CLI whose commands all speak JSON, so scripts and agents can read and update Jira as easily as you do.</p>

<p align="center">
  <a href="https://github.com/ngavilan-dogfy/jira-cli/releases/latest"><img src="https://img.shields.io/github/v/release/ngavilan-dogfy/jira-cli?style=flat-square&color=0052cc&label=release" alt="Release"></a>
  <a href="https://github.com/ngavilan-dogfy/jira-cli/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/ngavilan-dogfy/jira-cli/ci.yml?style=flat-square&label=tests" alt="Tests"></a>
  <img src="https://img.shields.io/badge/platform-macOS%20%7C%20Linux%20%7C%20Windows-lightgrey?style=flat-square" alt="Platform">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-green?style=flat-square" alt="License"></a>
</p>

<p align="center">
  <img src="assets/mine.png" alt="jira ui: your tickets grouped by status, with pull requests and agents next to them, and the selected ticket on the right" width="860">
</p>

## Get started in two minutes

```bash
curl -fsSL https://raw.githubusercontent.com/ngavilan-dogfy/jira-cli/main/install.sh | sh
jira setup
jira ui
```

No Jira at hand? `jira ui --demo` opens a made-up team's project, branches and agents included, so you can look around first. Nothing leaves your machine.

The installer picks the build for your computer, checks it against the release's checksums, puts it in `~/.local/bin` (no sudo), offers to add that folder to your `PATH`, to teach Claude Code this CLI if you use it, and to run `jira setup`. Run it again any time: it updates in place.

`jira setup` walks you through four steps:

<p align="center"><img src="assets/setup.gif" alt="jira setup, step by step" width="720"></p>

1. **Your Jira site**: paste any link from Jira (a board, a ticket) or just the site name. If the link was a ticket, its project is noted too.
2. **Sign in**: with an **API token** (recommended; works with Google and SSO accounts). Your email comes filled in from git — the work one when you keep several — and setup opens the page where you create the token and says what to click. Click **Copy** and setup takes the token from your clipboard: no pasting. If Jira rejects it, it says why (wrong email, a password instead of a token, an expired token). Companies that turned API tokens off can use the **browser login (OAuth)**, and setup guides the one-time app registration click by click.
3. **Default project**: where `jira ls` and `jira ui` start, and what short keys expand to (`306` → `OPS-306`).
4. **Extras for `jira ui`** (optional): checks git, the GitHub CLI and Claude Code, asks where your repositories live to link branches and pull requests to their tickets, signs the GitHub CLI in if it isn't (`gh auth login`), and offers the `/jira` skill for Claude Code.

Nothing is saved until the end. Settings stay in `~/.config/jira-cli/`, readable only by you, and setup clears the token from your clipboard once it's saved. Run `jira setup` again whenever you want: it offers to check everything, switch project, or start over.

## The terminal UI

`jira ui` opens on your last tab. `jira ui 306` jumps straight into a ticket. Anywhere a command takes a key, a short number (`306`) or a pasted Jira link works too.

| | |
|---|---|
| **Lists**: tabs for *Mine*, *Work*, *Team*, *Watching*, *Recent* and *Epics*, plus a *Search* (text or JQL). Issues are grouped by status, sub-tasks nested under their parent, finished work folded; wide terminals preview the selected ticket, description rendered with code, tables, checklists and clickable links. | <img src="assets/mine.png" width="420"> |
| **Work**: every ticket with something in flight: local branches and worktrees (found by the key in their name), pull requests with their checks and review state (via `gh`), and the Claude Code sessions working on them, showing whether each one is **working**, **waiting for you** or **needs approval**. | <img src="assets/work.png" width="420"> |
| **Board**: `b` turns any tab into a Kanban board; `H` / `L` move the card to the next status. | <img src="assets/board.png" width="420"> |
| **Detail**: description, work, comments, sub-tasks, links and history, with the fields on the side. `P` goes to the parent; `⏎` opens a sub-task or link. | <img src="assets/detail.png" width="420"> |
| **Start an agent**: `C` on a ticket creates a worktree with a `<type>/<KEY>-<slug>` branch off the default branch (or continues the ticket's branch), and opens `claude` in a new iTerm2 tab with the ticket as its first message, without taking your focus. It can also assign the ticket to you and move it to *In Progress*. | <img src="assets/launch.png" width="420"> |

On any screen: `m` move, `c` comment (`Ctrl+E` opens `$EDITOR`), `e` edit fields, `E` edit the description in `$EDITOR`, `i` / `A` assign, `n` / `N` new issue or sub-task, `w` watch, `B` git branch, `y` / `Y` copy key or link, `o` open in the browser. `:` is the command palette (type a key or a number to jump to it) and `?` lists every key.

The UI uses your terminal's own colors, remembers your tab, layout and folded groups, and caches each tab so it paints instantly and refreshes in the background.

## For scripts and AI agents

Every command adapts to where its output goes: colors in a terminal, tab-separated values when piped, JSON with `--json`. Keys can be short (`100` → `PROJ-100`).

- **`jira context KEY`** is made for agents: fields, description, every comment, links, sub-tasks, attachments, history, available transitions, epic children, watchers and development info, in one call, as markdown ready for a prompt (or `--json`).
- **`jira export --jql '…' --format md`** dumps a whole epic or backlog, with `--comments` if you want them.
- **`jira batch <action>`** applies a move, an assignment, labels or a comment to every key on stdin.
- **`jira wait KEY --status Done`** blocks until a status is reached: exit 0, or 2 on timeout.
- Descriptions and comments are written in markdown; the CLI converts them to Jira's format.

No questions without a terminal: pass everything as flags and the token on stdin, or skip setup with environment variables:

```bash
echo "$JIRA_TOKEN" | jira setup --site acme --email you@acme.com --token-stdin --project OPS
# or
export JIRA_DOMAIN=acme JIRA_EMAIL=you@acme.com JIRA_TOKEN=… JIRA_PROJECT=OPS
```

### Claude Code

```bash
jira skill install
```

This installs the `/jira` skill, so Claude Code knows how to work with this CLI: load tickets with `context`, write in markdown, respect the workflow's transitions, and confirm anything it would be inferring. The skill ships inside the binary and is refreshed when you update.

## Commands

| Area | Commands |
|---|---|
| Find | `ls`, `show`, `context`, `inbox`, `export`, `history`, `attachments`, `epics` |
| Change | `create`, `from-pr`, `edit`, `move`, `assign`, `unassign`, `comment`, `comment-edit`, `comment-rm`, `label-add`, `label-rm`, `link`, `attach`, `watch`, `unwatch`, `delete`, `batch` |
| Discover | `transitions`, `statuses`, `types`, `priorities`, `users`, `projects`, `fields`, `link-types`, `whoami` |
| Work | `ui`, `branch`, `open`, `wait` |
| This CLI | `setup`, `doctor`, `update`, `version`, `skill`, `profile`, `config`, `logout`, `completion` |

`jira <command> --help` explains each one, with examples.

## When something's off

Start with `jira doctor`: it checks the install, your `PATH`, the site, your session, the project and the optional tools, and says how to fix each problem.

| You see | Do this |
|---|---|
| `command not found: jira` | Open a new terminal. Still there? Add `export PATH="$HOME/.local/bin:$PATH"` to your `~/.zshrc` (or `~/.bashrc`). |
| *Jira didn't accept that email + token* | Use the email you sign in to Atlassian with, paste the **API token** (not your password), and create a new one if it expired. |
| *Jira refused access* / API tokens are disabled | `jira setup` → *Browser login (OAuth)*. |
| `jira` runs a different tool | Another `jira` comes first in your `PATH`; `jira doctor` says which. |

## Updating

```bash
jira update
```

It shows what's new, downloads the release for your machine, checks its checksum and that the new binary runs, and only then replaces the old one. When a new version is out, commands you run in a terminal mention it after their output (the check runs in the background, at most once a day); `JIRA_NO_UPDATE_NOTIFIER=1` turns that off.

<details>
<summary><strong>Other ways to install, and uninstalling</strong></summary>

- **A specific version or folder**: `curl -fsSL …/install.sh | JIRA_VERSION=v1.2.0 JIRA_INSTALL_DIR=~/bin sh` (`JIRA_NO_SETUP=1`, `JIRA_NO_SKILL=1` and `JIRA_NO_MODIFY_PATH=1` keep it from asking).
- **With Go**: `go install github.com/ngavilan-dogfy/jira-cli/cmd/jira@latest`
- **By hand**: download `jira-<os>-<arch>` from the [latest release](https://github.com/ngavilan-dogfy/jira-cli/releases/latest), check it against `checksums.txt`, make it executable and put it in your `PATH`. Windows: `jira-windows-amd64.exe`.
- **From source**: `make install` builds and copies to `~/.local/bin`.
- **Uninstall**: `rm ~/.local/bin/jira`, and `rm -rf ~/.config/jira-cli` to also forget your settings and tokens.

</details>

## Contributing

`make check` runs vet, the tests and a build. Releases are cut automatically from [conventional commits](https://www.conventionalcommits.org) on `main`; [CONTRIBUTING.md](CONTRIBUTING.md) explains how.

## License

[MIT](LICENSE)
