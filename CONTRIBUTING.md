# Contributing

Thanks for taking the time. This guide covers the development loop, the
rules a change has to follow, and how releases happen. For where things live
and why, read [ARCHITECTURE.md](ARCHITECTURE.md).

## Development loop

You need Go 1.25 or later. A Jira site isn't required for most work.

```bash
make check      # go vet, the tests and a build into bin/jira
./bin/jira …    # try a change
jira ui --demo  # or ./bin/jira ui --demo: the UI on a made-up team
make install    # copy the build to ~/.local/bin/jira
```

`jira ui --demo` runs against a made-up Jira (`internal/demo`) plus real git
repositories in a temporary folder and simulated pull requests and agents
(`tui/demo.go`), so UI changes can be checked and screenshotted without
touching a real site.

`make e2e` builds `bin/jira-e2e`, which points setup and doctor at
`$JIRA_E2E_BASE` (a fake Jira) and logs the pages it would open to
`$JIRA_E2E_LOG` (see `cmd/e2e_hooks.go`); it is never part of a release.

Reading commands (`ls`, `show`, `context`, `epics`…) are safe to try against
a real Jira. Never try a command that changes Jira against a real project.

## What a change needs

- **Tests.** Commands and setup run against a fake Jira over HTTP
  (`cmd/setup_test.go`); the UI through the harness in `tui/fakejira_test.go`,
  which drives the app key by key and fails if a frame isn't exactly the
  terminal's size. Mutations are tested there, never against a real site.
- **The output contract.** Every command renders a styled view in a
  terminal, TSV or plain text when piped and JSON with `--json`, and puts
  nothing but data on stdout. JSON field names are a public interface:
  renaming or removing one is a breaking change.
- **Keys through `normalizeKey`**, so short numbers and Jira links work in
  every command that takes a key.
- **Help that teaches.** Each command's `Long` help explains what it's for
  and ends with `Output:` and `Examples:` sections.
- **The skill.** If commands or flags change, update
  `cmd/skill_data/SKILL.md` and copy it to `.claude/skills/jira/SKILL.md`.
- **No real data.** Screenshots come from `jira ui --demo`; examples use
  made-up projects (`PROJ`, `OPS`), people and keys.

## Commit messages

Commits on `main` follow [Conventional Commits](https://www.conventionalcommits.org).
They pick the next version and become the release notes, so write the subject
for someone reading "what's new" before updating — one feature per commit.

| Commit | Release |
|---|---|
| `feat: …`, `feat(tui): …` | minor — v1.**3**.0 |
| `fix: …`, `perf: …` | patch — v1.2.**1** |
| `feat!: …`, or `BREAKING CHANGE:` in the body | major — v**2**.0.0 |
| `docs:`, `refactor:`, `test:`, `build:`, `ci:`, `chore:` | none |

`[skip release]` in a message keeps that commit out of the next release.
Pull requests are squash-merged, so the pull request's title is the commit
subject: write it the same way.

## Releases

Nobody cuts releases by hand. On every push to `main`,
[Auto Version](.github/workflows/auto-version.yml):

1. works out the next version from the commits since the last tag
   (`scripts/next-version.sh`);
2. runs the tests;
3. builds `jira-<os>-<arch>` for macOS, Linux and Windows, plus
   `checksums.txt` (`scripts/build-release.sh`);
4. writes the notes from the commit subjects (`scripts/release-notes.sh`);
5. publishes the GitHub release, which creates the tag.

`jira update`, `install.sh` and the update notice all read those releases, so
a release reaches everyone as soon as it's published. To publish a specific
version (a major bump, say), push a tag — `git tag v2.0.0 && git push origin
v2.0.0` — and [Release](.github/workflows/release.yml) builds it the same
way. `make release VERSION=v1.2.3` builds the release files into `dist/`
locally, to look at before pushing.

## Screenshots and images

- Screenshots in `assets/` are taken from `jira ui --demo`.
- `assets/setup.gif` is recorded with `make e2e && scripts/record-setup.sh`
  (macOS, [vhs](https://github.com/charmbracelet/vhs)): a tiny fake Jira, a
  made-up token, a throwaway home directory, and your clipboard restored
  afterwards. Look at the result frame by frame before committing it.
- `assets/social-preview.png` is the card GitHub shows when the repository is
  shared (*Settings → General → Social preview*). It's rendered from
  `scripts/record/social-preview.html` at 1280×640 — for example with
  `chrome --headless=new --window-size=1280,640 --screenshot=assets/social-preview.png scripts/record/social-preview.html`
  — whenever the tagline or the hero screenshot changes.
