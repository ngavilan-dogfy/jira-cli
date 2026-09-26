# AGENTS.md

Guidance for AI agents: first for using the `jira` CLI to work in someone's
Jira, then for working on this repository. Claude Code users get the same
guidance, with playbooks, as a skill: `jira skill install`.

## Using the CLI

### Ground rules

1. **Load a ticket with `jira context KEY`.** One call returns its fields,
   description, every comment in order, links, sub-tasks, attachments,
   history and the transitions available now — as markdown for reasoning,
   or `--json`. Don't chain `show`, `history` and `transitions`.
2. **Read freely. Write what was asked; confirm what you would infer.**
   "Move PROJ-12 to Done" is an instruction; "clean up my board" is a
   proposal to show before acting. Never delete unless that deletion was
   asked for.
3. **Write markdown, never Jira wiki markup.** Descriptions and comments take
   headings, lists, bold, code and `[links](url)`; the CLI converts them.
4. **Workflows are strict.** A ticket can only move to the statuses in
   `availableTransitions`; step through intermediate statuses when needed.
5. **Keys are forgiving.** `100` means `PROJ-100` in the default project, and
   a pasted Jira link works wherever a key does.

### Reading

```sh
jira context PROJ-123 [--json] [--fast]           # one ticket, everything
jira ls --jql 'assignee = currentUser() AND statusCategory != Done' --json
jira ls -a --since 7d --json                      # updated recently (30m, 4h, 2w, YYYY-MM-DD)
jira inbox --since 1d --json                      # what changed in issues I watch, report or own
jira export --jql 'parent = PROJ-10' --format md --comments   # a whole epic as one document
jira attachments PROJ-123 --download -o /tmp/proj-123
```

`ls` returns 50 issues by default (`-n` for more); `export` pages past the
API's 100 per page. Jira's search returns no total count: don't report one.

### Writing

```sh
jira create "Summary" -t Bug --priority High --parent PROJ-10 -d - --json <<'MD'
## Context
…
MD
jira move PROJ-123 "In Review"
jira comment PROJ-123 <<'MD'
Findings in **markdown**.
MD
printf 'PROJ-1\nPROJ-2\n' | jira batch move "Done"
jira wait PROJ-123 --status Done --timeout 30m    # exit 0 when reached, 2 on timeout
```

`-d -` and a piped `comment` read from stdin, so quotes and newlines are safe.

### Conventions you can rely on

- stdout carries data only: JSON with `--json`, TSV with a header row when
  piped, tables in a terminal.
- Exit codes: `0` success, `1` failure (message on stderr), `2` when
  `jira wait` times out.
- The client retries 429 and 5xx on reads; don't add retries.
- JSON field names are stable: renaming one is a breaking change.
- Never ask for an API token in a conversation and never print one: signing
  in is the user's step (`! jira setup`).

## Working on this repository

Read [ARCHITECTURE.md](ARCHITECTURE.md) first: it maps the code and explains
the decisions behind it. [CONTRIBUTING.md](CONTRIBUTING.md) covers the
workflow and releases. The rules that matter most:

- **`make check`** (vet, tests, build) before every commit; `jira ui --demo`
  to look at UI changes.
- **Every command has three output modes**: styled in a terminal, TSV/plain
  when piped, JSON with `--json`. No exceptions: agents depend on them.
- **Every issue-key argument goes through `normalizeKey`**, so short keys and
  links work everywhere.
- **Markdown in, ADF out**: write through `jira.MarkdownToADF`, read through
  `jira.ADFToText`. Errors from the API go through `parseAPIError`.
- **Retries live in `client.do()`** (429 on any method, 5xx on reads). Don't
  add ad-hoc retries.
- **UI rules**: screens never call the API or touch the store; writes go
  through `mutate()`; every external program runs through `sys`; every frame
  is exactly the terminal's size, measured with `x/ansi`, never `len()`.
- **Help teaches**: each command's `Long` has `Output:` and `Examples:`
  sections — it's the primary documentation for people and agents.
- **Never touch a real Jira in tests**, and never try a mutation against a
  real project. Screenshots come from `jira ui --demo`; examples use made-up
  projects, people and keys.
- **Keep the skill current**: when commands or flags change, update
  `cmd/skill_data/SKILL.md` and its copy in `.claude/skills/jira/` (a test
  checks they match).
- **Shared files** — `internal/selfupdate/` and `cmd/keyinput.go` also live in
  the other ngavilan-dogfy CLIs; port changes to them.
- **Conventional commits**: the subject becomes a line in the release notes.
