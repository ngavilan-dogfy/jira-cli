# jira-cli

Read [AGENTS.md](AGENTS.md): how to use this CLI and how to work on it.
[ARCHITECTURE.md](ARCHITECTURE.md) maps the code; [CONTRIBUTING.md](CONTRIBUTING.md)
covers the workflow and releases.

Essentials:

- `make check` (vet, tests, build) before committing; `jira ui --demo` to
  look at UI changes without a real Jira.
- One file per command in `cmd/`, registered in `init()`, with the three
  output modes (terminal, TSV, `--json`); keys through `normalizeKey`.
- Markdown in (`jira.MarkdownToADF`), ADF out (`jira.ADFToText`).
- In the UI: screens never call the API; writes go through `mutate()`;
  external programs through `sys`; every frame exactly the terminal's size.
- Never read or change a real Jira in tests.
- The skill is `cmd/skill_data/SKILL.md`, embedded in the binary;
  `.claude/skills/jira/SKILL.md` must stay identical.
- Conventional commits pick the next version and write the release notes.
