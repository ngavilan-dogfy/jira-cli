package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// The demo ('jira ui --demo') runs the real TUI against a made-up Jira site
// (internal/demo). This file gives it made-up local work too: two git
// repositories with branches and a worktree per ticket, pull requests on a
// pretend GitHub and two Claude Code sessions, one working and one waiting.
// Everything lives in a temporary folder; nothing is opened or launched.

// demoMode turns off side effects that make no sense with made-up data:
// opening the browser and starting Claude Code.
var demoMode bool

// demoDir, when set, holds the TUI's state and caches instead of the
// user's config and cache folders.
var demoDir string

// EnableDemo prepares the made-up local work and points the TUI at it. The
// returned function removes the temporary files.
func EnableDemo() (cleanup func(), err error) {
	dir, err := os.MkdirTemp("", "jira-demo-")
	if err != nil {
		return nil, err
	}
	cleanup = func() { os.RemoveAll(dir) }
	dir, _ = filepath.EvalSymlinks(dir)
	repos := filepath.Join(dir, "repos")
	wt, err := demoRepos(dir, repos)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("preparing the demo's git repositories: %w", err)
	}
	claude := filepath.Join(dir, "claude")
	demoTranscripts(claude, wt)

	demoMode, demoDir = true, dir
	os.Setenv("JIRA_REPOS", repos)
	sys = demoRunner{root: dir, worktrees: wt}
	claudeHome = func() string { return claude }
	lookPath = func(name string) (string, error) {
		switch name {
		case "gh", "claude", "git":
			return "/usr/local/bin/" + name, nil
		}
		return exec.LookPath(name)
	}
	return cleanup, nil
}

// demoRepos makes storefront and payments-api, each pushed to a local
// "origin", with a branch per ticket and two worktrees where agents work.
func demoRepos(dir, repos string) ([]string, error) {
	origins := filepath.Join(dir, "origins")
	if err := os.MkdirAll(origins, 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(repos, 0o755); err != nil {
		return nil, err
	}
	git := func(cwd string, ago time.Duration, args ...string) error {
		cmd := exec.Command("git", args...)
		cmd.Dir = cwd
		when := time.Now().Add(-ago).Format(time.RFC3339)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Sam Rivera", "GIT_AUTHOR_EMAIL=sam@acme.example",
			"GIT_COMMITTER_NAME=Sam Rivera", "GIT_COMMITTER_EMAIL=sam@acme.example",
			"GIT_AUTHOR_DATE="+when, "GIT_COMMITTER_DATE="+when,
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	type branch struct {
		name   string
		ago    time.Duration
		wt     bool
		pushed bool
	}
	layout := map[string][]branch{
		"storefront": {
			{"feat/SHOP-110-one-page-checkout", 3 * time.Hour, false, true},
			{"feat/SHOP-111-address-autocomplete", 30 * time.Minute, true, false},
			{"test/SHOP-114-checkout-e2e", 80 * time.Minute, true, true},
			{"fix/SHOP-112-coupon-shipping", 5 * time.Hour, false, true},
		},
		"payments-api": {
			{"fix/SHOP-150-email-timezone", 25 * time.Minute, false, true},
			{"fix/SHOP-156-idempotency-keys", 4 * time.Hour, false, true},
		},
	}
	var worktrees []string
	for _, name := range []string{"storefront", "payments-api"} {
		bare := filepath.Join(origins, name+".git")
		repo := filepath.Join(repos, name)
		steps := [][]string{
			{origins, "init", "-q", "--bare", "-b", "main", bare},
			{repos, "init", "-q", "-b", "main", repo},
		}
		for _, s := range steps {
			if err := git(s[0], 48*time.Hour, s[1:]...); err != nil {
				return nil, err
			}
		}
		if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# "+name+"\n"), 0o644); err != nil {
			return nil, err
		}
		for _, s := range [][]string{{"add", "."}, {"commit", "-q", "-m", "chore: start " + name}, {"remote", "add", "origin", bare}, {"push", "-q", "-u", "origin", "main"}} {
			if err := git(repo, 48*time.Hour, s...); err != nil {
				return nil, err
			}
		}
		for _, b := range layout[name] {
			// Each branch gets a commit of its own at its time, and most are
			// pushed, like real work in progress.
			cwd := repo
			if b.wt {
				// feat/SHOP-111-address-autocomplete → .worktrees/storefront-SHOP-111
				f := strings.SplitN(strings.SplitN(b.name, "/", 2)[1], "-", 3)
				cwd = filepath.Join(repos, ".worktrees", name+"-"+f[0]+"-"+f[1])
				if err := git(repo, b.ago, "worktree", "add", "-q", "-b", b.name, cwd, "main"); err != nil {
					return nil, err
				}
				worktrees = append(worktrees, cwd)
			} else if err := git(repo, b.ago, "checkout", "-q", "-b", b.name, "main"); err != nil {
				return nil, err
			}
			if err := git(cwd, b.ago, "commit", "-q", "--allow-empty", "-m", "wip: "+b.name); err != nil {
				return nil, err
			}
			if b.pushed {
				if err := git(cwd, b.ago, "push", "-q", "-u", "origin", b.name); err != nil {
					return nil, err
				}
			}
			if !b.wt {
				if err := git(repo, b.ago, "checkout", "-q", "main"); err != nil {
					return nil, err
				}
			}
		}
	}
	return worktrees, nil
}

// demoTranscripts writes one Claude Code session per worktree: the first
// is busy running the tests, the second finished and waits for an answer.
func demoTranscripts(claude string, worktrees []string) {
	type entry = map[string]any
	text := func(s string) entry { return entry{"type": "text", "text": s} }
	write := func(wt string, entries ...entry) {
		dir := projectDirIn(claude, wt)
		_ = os.MkdirAll(dir, 0o755)
		var b strings.Builder
		for _, e := range entries {
			line, _ := json.Marshal(e)
			b.Write(line)
			b.WriteByte('\n')
		}
		_ = os.WriteFile(filepath.Join(dir, "session.jsonl"), []byte(b.String()), 0o644)
	}
	ts := func(ago time.Duration) string { return time.Now().Add(-ago).UTC().Format(time.RFC3339Nano) }
	if len(worktrees) > 0 {
		write(worktrees[0],
			entry{"type": "permission-mode", "permissionMode": "acceptEdits"},
			entry{"type": "user", "timestamp": ts(12 * time.Minute), "gitBranch": "feat/SHOP-111-address-autocomplete",
				"message": entry{"role": "user", "content": []entry{text("Work on SHOP-111: address autocomplete with postal code lookup.")}}},
			entry{"type": "assistant", "timestamp": ts(20 * time.Second), "gitBranch": "feat/SHOP-111-address-autocomplete",
				"message": entry{"role": "assistant", "stop_reason": "tool_use", "content": []entry{
					text("The lookup works; running the checkout tests before I commit."),
					{"type": "tool_use", "id": "t1", "name": "Bash", "input": entry{"command": "npm test -- checkout"}}}}})
	}
	if len(worktrees) > 1 {
		write(worktrees[1],
			entry{"type": "permission-mode", "permissionMode": "default"},
			entry{"type": "assistant", "timestamp": ts(6 * time.Minute), "gitBranch": "test/SHOP-114-checkout-e2e",
				"message": entry{"role": "assistant", "stop_reason": "end_turn", "content": []entry{
					text("All 42 checkout tests pass with the new layout. Shall I push the branch and open the PR?")}}})
	}
}

// demoRunner answers the programs the Work tab runs: real git inside the
// demo folder, and made-up answers for GitHub, the process list and iTerm.
type demoRunner struct {
	root      string
	worktrees []string
}

func (d demoRunner) run(timeout time.Duration, dir, name string, args ...string) ([]byte, error) {
	switch name {
	case "git":
		if len(args) == 3 && args[0] == "remote" && args[1] == "get-url" {
			return []byte("https://github.com/acme-shop/" + filepath.Base(dir) + ".git\n"), nil
		}
		if dir == "" || !strings.HasPrefix(dir, d.root) {
			return nil, fmt.Errorf("git: the demo only touches its own folder")
		}
		for _, a := range args {
			if a == "fetch" || a == "ls-remote" || a == "push" {
				return nil, fmt.Errorf("git %s: the demo has no network", a)
			}
		}
		return execRunner{}.run(timeout, dir, name, args...)
	case "gh":
		return demoGH(args)
	case "pgrep":
		if len(d.worktrees) == 0 {
			return nil, fmt.Errorf("pgrep: exit status 1")
		}
		return []byte("4242\n4243\n"), nil
	case "ps":
		if len(args) > 1 && args[1] == "tty=" {
			return []byte("ttys001\n"), nil // this terminal
		}
		return []byte("4242 ttys003\n4243 ttys004\n"), nil
	case "lsof":
		var b strings.Builder
		for i, wt := range d.worktrees {
			fmt.Fprintf(&b, "p%d\nfcwd\nn%s\n", 4242+i, wt)
		}
		return []byte(b.String()), nil
	case "osascript":
		return []byte("ok\n"), nil
	}
	return nil, fmt.Errorf("%s isn't available in the demo", name)
}

// demoGH answers 'gh search prs' and 'gh pr view' for the demo's PRs.
func demoGH(args []string) ([]byte, error) {
	type pr = map[string]any
	mk := func(n int, repo, title, author string, draft bool, ago time.Duration) pr {
		return pr{"number": n, "title": title, "body": "", "state": "open", "isDraft": draft,
			"url":        fmt.Sprintf("https://github.com/acme-shop/%s/pull/%d", repo, n),
			"updatedAt":  time.Now().Add(-ago).UTC().Format(time.RFC3339),
			"repository": pr{"nameWithOwner": "acme-shop/" + repo, "name": repo},
			"author":     pr{"login": author, "type": "User"}}
	}
	joined := strings.Join(args, " ")
	switch {
	case len(args) > 0 && args[0] == "search" && strings.Contains(joined, "--merged"):
		return json.Marshal([]pr{
			mk(119, "storefront", "feat(checkout): rollout flag for checkout v2 (SHOP-116)", "sam-rivera", false, 4*24*time.Hour),
			mk(117, "storefront", "chore: Node 22 (SHOP-153)", "sam-rivera", false, 3*24*time.Hour),
		})
	case len(args) > 0 && args[0] == "search":
		return json.Marshal([]pr{
			mk(128, "storefront", "feat(checkout): one-page layout (SHOP-110)", "sam-rivera", false, 2*time.Hour),
			mk(131, "storefront", "fix(checkout): keep the coupon when shipping changes (SHOP-112)", "leo-martin", true, 5*time.Hour),
			mk(87, "payments-api", "fix(email): order time in the customer's time zone (SHOP-150)", "sam-rivera", false, 25*time.Minute),
			mk(85, "payments-api", "fix(payments): idempotency keys for retries (SHOP-156)", "maya-chen", false, 4*time.Hour),
		})
	case len(args) > 2 && args[0] == "pr" && args[1] == "view":
		url := args[2]
		checks := `[{"status":"COMPLETED","conclusion":"SUCCESS"},{"status":"COMPLETED","conclusion":"SUCCESS"}]`
		review := "REVIEW_REQUIRED"
		switch {
		case strings.HasSuffix(url, "/128"):
			review = "APPROVED"
		case strings.HasSuffix(url, "/87"):
			checks = `[{"status":"COMPLETED","conclusion":"FAILURE"},{"status":"COMPLETED","conclusion":"SUCCESS"}]`
		case strings.HasSuffix(url, "/131"):
			checks = `[{"status":"IN_PROGRESS","conclusion":""}]`
		}
		return []byte(`{"reviewDecision":"` + review + `","statusCheckRollup":` + checks + `}`), nil
	}
	return nil, fmt.Errorf("gh %s isn't available in the demo", joined)
}
