---
name: jira
description: Work with Jira Cloud through the `jira` CLI. Find and summarize issues, epics and backlogs; create, edit, move, assign, comment on and link issues; turn a ticket into a branch; and see what changed. Use it when the user mentions Jira, a key like ABC-123, their tickets, the backlog, an epic or sprint, what to work on next, wants a ticket created, updated or moved, or pastes a Jira link.
argument-hint: "[question | issue key | JQL]"
allowed-tools: Bash, Read
---

# Jira copilot

You work in the user's Jira through the `jira` CLI. Read before you write:
load the ticket, understand its state and history, then answer or act. You
never change Jira on a guess. Do what the user asked, and confirm anything
you'd be inferring.

## 0. Make sure it works

- `command -v jira` fails → it isn't installed:
  `curl -fsSL https://raw.githubusercontent.com/ngavilan-dogfy/jira-cli/main/install.sh | sh`
- "isn't connected to Jira yet", "not signed in" or a 401 → the user has
  to sign in themselves, because it's interactive (browser or API token).
  Tell them to type `! jira setup`. **Never ask for an API token in the chat
  and never print one.**
- Anything else odd: `jira doctor --json` checks the site, the session, the
  project and local tools, and says how to fix each problem.
- In CI: `JIRA_DOMAIN`, `JIRA_EMAIL` and `JIRA_TOKEN` work without setup.

## 1. Ground rules

1. **Load a ticket with `jira context KEY`.** One call gets its fields,
   description, comments, links, sub-tasks, attachments, recent history and
   the transitions available now. Don't chain `show` + `history` +
   `transitions`.
2. **Short keys and links work.** `100` means `PROJ-100` with the project
   from the user's profile, and a pasted Jira link (`…/browse/PROJ-12`, a
   board with `?selectedIssue=PROJ-12`) works wherever a key does:
   `jira context "<link>"`. `-p OTHER` (on `ls`) queries another project
   without switching profiles.
3. **Read freely. Write what was asked; confirm what you'd infer.** "Move
   PROJ-12 to Done" → do it. "Clean up my board" → propose the moves as a
   list and wait for a yes. Never delete unless asked for that deletion.
4. **Markdown, never Jira wiki markup.** Descriptions and comments take
   `## headings`, `- lists`, `**bold**`, `` `code` ``, fenced code and
   `[links](url)`; the CLI converts them. Don't write `h2.`, `{code}` or
   `[text|url]`. Give links a readable text (`[PR #12](url)`,
   `[PROJ-7](…/browse/PROJ-7)`) rather than pasting the URL, and mention
   people with `@[Full Name]` so they are notified: the name must match one
   person, or nothing is posted and the candidates are listed
   (`@[Name](accountId)` picks one).
5. **Workflows are strict.** A ticket can only move to the statuses in
   `availableTransitions` (from `context`, or `jira transitions KEY --json`).
   If the target isn't there, step through the intermediate status.
6. **Use `--json` when you read output, and summarize.** Don't paste raw
   JSON or long descriptions back at the user.

## 2. Reading

```bash
jira context PROJ-123                 # markdown: the whole ticket, ready to reason over
jira context PROJ-123 --json          # the same, structured (fields, links, history,
                                      # availableTransitions, attachments…)
jira context PROJ-123 --fast          # skips remote links, watchers, worklogs, dev info
jira ls --json                        # my issues, open ones first
jira ls -a -s "In Progress" --json    # everyone's, one status
jira ls -a --since 7d --json          # updated in the last week (30m, 4h, 2w, YYYY-MM-DD)
jira ls --parent PROJ-10 --json       # an epic's children
jira ls -a -q "timeout" --json        # free text: summary, description, comments
jira ls --label backend,urgent --json # any of these labels
jira ls --assignee none --json        # unassigned (me | none | account id)
jira ls --jql 'JQL' --json            # anything else
jira inbox --since 1d --json          # what changed in issues I watch, report or own
jira epics --json                     # the project's epics
jira export --jql 'parent = PROJ-10' --format md --comments   # a whole epic, as markdown
jira attachments PROJ-123 --download -o /tmp/proj-123          # then Read the files
```

`ls` returns 50 issues by default (`-n` for more). `export` pages past the
API's 100 per page, up to `-n` (200 by default), as JSONL or markdown
(`--format md`); `--comments` adds each issue's comments.

JQL that comes up often:

| Want | JQL |
|---|---|
| Mine, not done | `assignee = currentUser() AND statusCategory != Done` |
| Changed recently | `updated >= -2d` |
| An epic's children | `parent = PROJ-10` |
| Bugs by priority | `issuetype = Bug ORDER BY priority DESC` |
| Unassigned in the backlog | `assignee is EMPTY AND status = Backlog` |
| Due soon | `duedate <= 7d AND statusCategory != Done` |
| Words anywhere | `text ~ "payment timeout"` |
| Current sprint | `sprint in openSprints()` |

## 3. Playbooks

### "What's on my plate?"
`jira ls --jql 'assignee = currentUser() AND statusCategory != Done' --json`
and `jira inbox --since 1d --json`. Group by status: what's
in progress, what's blocked (links like "is blocked by"), what's overdue
(`duedate`), what changed since yesterday, and who's waiting on the user
(a recent comment addressed to them, an issue in review). End with a
suggested order for the day.

### "Explain PROJ-123" / "catch me up"
`jira context PROJ-123`. Answer with the goal, the current status and owner,
the decisions and open questions in the comments (who said what, when),
what blocks it, and the obvious next step. Download attachments that matter
(logs, screenshots) and read them.

### "Create a ticket for this"
Draft first: a summary under ~80 characters that says the outcome, a
markdown description (context, what to do, acceptance criteria), type,
priority, labels and parent epic if there is one. Show the draft, then:
```bash
jira create "Summary" -t Bug --priority High --labels backend --parent PROJ-10 -d - --json <<'EOF'
## Context
…
## Acceptance criteria
- …
EOF
```
`-d -` reads the description from stdin, so quotes and newlines are safe.
For a GitHub PR: `jira from-pr <url or number>`.

### "Break this epic down"
Load the epic (`context`) and its children (`ls --parent`). Propose the
missing pieces as a list of summaries with a line each; after a yes,
create them with `--parent EPIC` (`-t Sub-task` for sub-tasks of a story).

### "Update / move / assign / comment"
```bash
jira move PROJ-123 "In Progress"                  # status (check transitions first)
jira assign PROJ-123 --me                         # or: jira unassign PROJ-123
jira edit PROJ-123 --summary "…" --priority High --labels "a,b"
jira edit PROJ-123 --description "## Context\n…"  # replaces the description
jira edit PROJ-123 --due 2026-10-15
jira comment PROJ-123 "Deployed to staging, ready for QA."
jira comment PROJ-123 --dry-run < note.md         # check mentions and links; posts nothing
jira label-add PROJ-123 needs-review              # keeps the other labels
jira link PROJ-123 PROJ-456 --type Blocks
jira watch PROJ-123                               # or unwatch
```
Custom fields: find the id with `jira fields -q "story points" --json`, then
`jira edit PROJ-123 --field customfield_10016=5` (the value is JSON when it
parses as JSON: `--field 'customfield_10020={"value":"Platform"}'`).

Many issues at once: keys on stdin, one per line.
```bash
jira ls -s "In Review" --plain | awk '{print $1}' | jira batch move Done
printf 'PROJ-1\nPROJ-2\n' | jira batch comment "Released in 2.4"
```
`batch` prints `OK <key>` or `ERR <key>: <reason>` per issue. List what it
will touch and ask before running it.

### "Start working on PROJ-123"
`jira assign PROJ-123 --me`, `jira move PROJ-123 "In Progress"` (if the
workflow allows), and `jira branch PROJ-123`, which creates and checks out
`<type>/PROJ-123-<slug>` in the current repo (`--print` only prints the
name, `--push` also pushes). Ask before touching git.

### Standup, review, release notes
`jira ls -a --since 1d --json` (or `--jql 'updated >= -1d'`) grouped by
assignee: done, in progress, blocked. For release notes, `jira export --jql
'status = Done AND updated >= -14d' --format md` and write them for users,
not as a list of tickets.

### In scripts
`jira wait PROJ-123 --status Done --timeout 30m` blocks until the status is
reached: exit 0 when it is, 2 on timeout, 1 on errors.

## 4. Commands

| Read-only | |
|---|---|
| `jira context KEY [--json]` | Everything about one issue |
| `jira show KEY --json` · `show KEY --url` | Fields and comments · just the link |
| `jira ls …` · `jira inbox` · `jira export` | Lists (section 2) |
| `jira history KEY --json` | Who changed what, when |
| `jira transitions KEY --json` | Where it can move now |
| `jira epics` · `users` · `statuses` · `types` · `priorities` · `projects` · `link-types` · `fields` (all `--json`) | What exists |
| `jira attachments KEY [--download -o DIR]` | Files |
| `jira whoami --json` | The signed-in user |

| Changes Jira | |
|---|---|
| `create`, `from-pr` | New issues |
| `edit`, `move`, `assign`, `unassign` | Fields, status, owner |
| `comment`, `comment-edit`, `comment-rm` | Comments |
| `label-add`, `label-rm`, `link`, `attach`, `watch`, `unwatch` | Labels, links, files, notifications |
| `batch <action>` | Many issues at once |
| `delete KEY --confirm` | Only when asked for that exact deletion |

Every command has `--help` with examples; most take `--json`.

## 5. Reporting back

- Lead with the answer. Then the tickets as a short table (key, summary,
  status, owner) or bullets, and a link when it helps
  (`jira show KEY --url`).
- After a change, say exactly what changed: "PROJ-123 → In Progress,
  assigned to you".
- If something failed (a missing transition, a field you can't edit), say
  so and what would fix it.
- For a person who'd rather see it: `jira ui KEY` opens the terminal UI on
  that ticket, `jira open KEY` the browser.
