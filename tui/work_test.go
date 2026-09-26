package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestKeysIn(t *testing.T) {
	re := keyPattern([]string{"PROJ", "OPS"})
	cases := map[string][]string{
		"feat/PROJ-324-observabilidad": {"PROJ-324"},
		"codex/proj-323-remove":        {"PROJ-323"},
		"(PROJ-320) (PROJ-319) TEAMS":  {"PROJ-320", "PROJ-319"},
		"PROJ-1,PROJ-2":                {"PROJ-1", "PROJ-2"},
		"release-2026-09":              nil,
		"XPROJ-1 or PROJ-12345x":       {"PROJ-12345"},
		"OPS-7 and PROJ-8":             {"OPS-7", "PROJ-8"},
		"infrastructure-proj323":       nil,
	}
	for in, want := range cases {
		got := keysIn(in, re)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("keysIn(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestParseWorktreesAndBranches(t *testing.T) {
	wts := parseWorktrees(`worktree /r/infra
HEAD 2a1f5b5
branch refs/heads/main

worktree /tmp/gone
HEAD a44e9da
branch refs/heads/fix/PROJ-298-teams
prunable gitdir file points to non-existent location

worktree /r/.worktrees/infra-PROJ-324
HEAD 1b83353
branch refs/heads/feat/PROJ-324-obs
`)
	if wts["main"].path != "/r/infra" || !wts["fix/PROJ-298-teams"].prunable || wts["feat/PROJ-324-obs"].path != "/r/.worktrees/infra-PROJ-324" {
		t.Fatalf("got %+v", wts)
	}
	bs := parseBranches("feat/PROJ-324-obs\t1b83353\t1760000000\torigin/feat/PROJ-324-obs\tahead 2\nmain\t2a1f5b5\t1750000000\t\t\n")
	if len(bs) != 2 || bs[0].track != "ahead 2" || bs[0].when.Unix() != 1760000000 || bs[1].upstream != "" {
		t.Fatalf("got %+v", bs)
	}
}

func TestParseLsofAndProjectDir(t *testing.T) {
	got := parseLsofCwd("p4242\nfcwd\nn/Users/x/repo\np99\nfcwd\nn/Users/x/other\n")
	if got[4242] != "/Users/x/repo" || got[99] != "/Users/x/other" {
		t.Fatalf("got %v", got)
	}
	defer func(f func() string) { claudeHome = f }(claudeHome)
	claudeHome = func() string { return "/c" }
	if d := projectDir("/Users/n/code/github/.worktrees/infra_PROJ-1"); d != "/c/projects/-Users-n-code-github--worktrees-infra-PROJ-1" {
		t.Fatalf("got %s", d)
	}
}

// transcript writes a Claude Code session file with the given entries.
func transcript(t *testing.T, dir string, entries ...map[string]any) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, e := range entries {
		line, _ := json.Marshal(e)
		b.Write(line)
		b.WriteByte('\n')
	}
	f := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(f, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return f
}

func assistant(stop string, ts time.Time, blocks ...map[string]any) map[string]any {
	return map[string]any{"type": "assistant", "timestamp": ts.UTC().Format(time.RFC3339Nano), "gitBranch": "feat/PROJ-1-x",
		"message": map[string]any{"role": "assistant", "stop_reason": stop, "content": blocks}}
}

func text(s string) map[string]any { return map[string]any{"type": "text", "text": s} }
func tool(name string) map[string]any {
	return map[string]any{"type": "tool_use", "name": name, "id": "t1", "input": map[string]any{}}
}

func TestTranscriptStates(t *testing.T) {
	dir := t.TempDir()
	recent, old := time.Now().Add(-10*time.Second), time.Now().Add(-5*time.Minute)
	mode := func(m string) map[string]any { return map[string]any{"type": "permission-mode", "permissionMode": m} }
	cases := []struct {
		name    string
		entries []map[string]any
		state   string
	}{
		{"finished turn", []map[string]any{assistant("end_turn", recent, text("Listo.\n\n¿Aplico el plan?"))}, "waiting"},
		{"asks a question", []map[string]any{assistant("tool_use", recent, text("Una duda"), tool("AskUserQuestion"))}, "waiting"},
		{"plan to approve", []map[string]any{assistant("tool_use", recent, tool("ExitPlanMode"))}, "waiting"},
		{"tool just started", []map[string]any{mode("default"), assistant("tool_use", recent, tool("Bash"))}, "working"},
		{"tool stuck, prompts on", []map[string]any{mode("default"), assistant("tool_use", old, tool("Bash"))}, "approval"},
		{"tool long, bypass mode", []map[string]any{mode("bypassPermissions"), assistant("tool_use", old, tool("Bash"))}, "working"},
		{"tool result back", []map[string]any{assistant("tool_use", old, tool("Bash")),
			{"type": "user", "timestamp": recent.UTC().Format(time.RFC3339Nano), "message": map[string]any{"role": "user", "content": []map[string]any{{"type": "tool_result"}}}}}, "working"},
	}
	for _, c := range cases {
		f := transcript(t, filepath.Join(dir, c.name), c.entries...)
		var a agentSession
		readTranscriptTail(f, &a)
		if a.state != c.state {
			t.Errorf("%s: state %q, want %q", c.name, a.state, c.state)
		}
		if a.branch != "feat/PROJ-1-x" {
			t.Errorf("%s: branch %q", c.name, a.branch)
		}
	}
	var a agentSession
	readTranscriptTail(filepath.Join(dir, "finished turn", "session.jsonl"), &a)
	if a.lastMsg != "Listo. ¿Aplico el plan?" {
		t.Errorf("last message %q", a.lastMsg)
	}
}

func TestCountWordAndRanking(t *testing.T) {
	if n := countWord("repo: infrastructure, no infrastructure-proj323 ni superinfrastructure", "infrastructure"); n != 2 {
		t.Errorf("got %d", n)
	}
}

// ─── end to end ──────────────────────────────────────────────────

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=T",
		"GIT_COMMITTER_EMAIL=t@t", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// workFixture builds a repos folder with two repos pushed to local
// "origins": infra has a worktree for TST-2 and a lowercase TST-5 branch.
func workFixture(t *testing.T) (root, wt string) {
	root, _ = filepath.EvalSymlinks(t.TempDir())
	origins := t.TempDir()
	for _, name := range []string{"infra", "web"} {
		bare := filepath.Join(origins, name+".git")
		gitIn(t, origins, "init", "-q", "--bare", "-b", "main", bare)
		repo := filepath.Join(root, name)
		gitIn(t, root, "init", "-q", "-b", "main", repo)
		os.WriteFile(filepath.Join(repo, "README.md"), []byte(name), 0o644)
		gitIn(t, repo, "add", ".")
		gitIn(t, repo, "commit", "-q", "-m", "init")
		gitIn(t, repo, "remote", "add", "origin", bare)
		gitIn(t, repo, "push", "-q", "-u", "origin", "main")
	}
	infra := filepath.Join(root, "infra")
	wt = filepath.Join(root, ".worktrees", "infra-TST-2")
	gitIn(t, infra, "worktree", "add", "-q", "-b", "feat/TST-2-migrate-api", wt, "main")
	gitIn(t, infra, "branch", "codex/tst-5-login-fix")
	return root, wt
}

func newWorkHarness(t *testing.T) (*harness, string, string) {
	return newWorkHarnessSized(t, 140, 37)
}

func newWorkHarnessSized(t *testing.T, width, height int) (*harness, string, string) {
	root, wt := workFixture(t)
	claudeDir := t.TempDir()
	transcript(t, projectDirIn(claudeDir, wt),
		map[string]any{"type": "permission-mode", "permissionMode": "default"},
		map[string]any{"type": "assistant", "timestamp": time.Now().Add(-2 * time.Minute).UTC().Format(time.RFC3339Nano),
			"gitBranch": "feat/TST-2-migrate-api",
			"message":   map[string]any{"role": "assistant", "stop_reason": "end_turn", "content": []map[string]any{text("Listo, ¿aplico el plan?")}}})

	t.Setenv("JIRA_REPOS", root)
	t.Setenv("TERM_PROGRAM", "iTerm.app")
	fr := &fakeRunner{
		gitRoots:  []string{root},
		pgrep:     "4242\n",
		ps:        "4242 ttys042\n",
		lsof:      "p4242\nfcwd\nn" + wt + "\n",
		claudeDir: claudeDir,
		gh: func(args []string) ([]byte, error) {
			pr := func(n int, title, repo string) map[string]any {
				return map[string]any{"number": n, "title": title, "body": "", "url": fmt.Sprintf("https://github.com/Acme/%s/pull/%d", repo, n),
					"state": "open", "isDraft": false, "updatedAt": time.Now().UTC().Format(time.RFC3339),
					"repository": map[string]any{"nameWithOwner": "Acme/" + repo}}
			}
			switch {
			case args[0] == "search" && strings.Contains(strings.Join(args, " "), "--merged"):
				return json.Marshal([]any{pr(40, "fix: docs (TST-7)", "web")})
			case args[0] == "search":
				return json.Marshal([]any{pr(41, "feat(api): migrate to Cloud Run (TST-2)", "infra")})
			case args[0] == "pr":
				return []byte(`{"reviewDecision":"APPROVED","statusCheckRollup":[{"status":"COMPLETED","conclusion":"SUCCESS"}]}`), nil
			}
			return nil, fmt.Errorf("unexpected gh %v", args)
		},
	}
	h := newHarnessWith(t, width, height, fr)
	return h, root, wt
}

func TestWorkLayoutAtSmallSizes(t *testing.T) {
	for _, sz := range [][2]int{{50, 12}, {64, 18}, {80, 24}, {100, 30}} {
		h, _, _ := newWorkHarnessSized(t, sz[0], sz[1])
		h.screen()
		h.keys("2") // Work tab
		h.screen()
		h.keys("1")
		h.selectKey("TST-2")
		h.keys("<enter>2") // detail → Work
		h.screen()
		h.keys("<esc>C") // launcher
		h.screen()
		h.keys("<down><down><enter>") // a "new worktree" entry → form
		h.screen()
		h.keys("<esc>")
	}
}

func TestWorkShowsAgentsPRsAndBranches(t *testing.T) {
	h, _, wt := newWorkHarness(t)
	// List: TST-2 has a waiting agent and PR #41; TST-5 only a branch.
	h.expect("● #41", "wip", "Work ●1")
	h.keys("j") // TST-2
	h.expect("Claude is waiting for you", "“Listo, ¿aplico el plan?”", "#41 open", "feat/TST-2-migrate-api")

	// The Work tab lists tickets in flight only.
	h.keys("2")
	h.expect("Work 2", "TST-2", "TST-5")
	h.reject("TST-6", "Rotate credentials")

	// Detail → Work: agent, PR (with CI and review) and branch rows.
	h.keys("1")
	h.selectKey("TST-2")
	h.keys("<enter>2")
	h.expect("Claude sessions", "Pull requests", "Branches & worktrees", "checks pass", "approved", "worktree", "/.worktrees/infra-TST-2")
	_ = wt
	h.keys("<enter>") // on the agent: jump to its iTerm tab
	calls := h.fr.callsTo("osascript")
	if len(calls) == 0 || calls[len(calls)-1][len(calls[len(calls)-1])-1] != "/dev/ttys042" {
		t.Fatalf("expected a focus on /dev/ttys042, got %v", calls)
	}
}

func TestStartClaudeInNewWorktree(t *testing.T) {
	h, root, _ := newWorkHarness(t)
	h.selectKey("TST-6")
	h.keys("C")
	h.expect("Start Claude on TST-6", "New worktree in infra", "New worktree in web")
	h.keys("infra<enter>")
	wt := filepath.Join(root, ".worktrees", "infra-TST-6")
	h.expect("feat/TST-6-rotate-credentials", "origin/main", "/.worktrees/infra-TST-6", "move to In Progress")
	h.keys("<enter>")

	if got := gitIn(t, wt, "branch", "--show-current"); got != "feat/TST-6-rotate-credentials" {
		t.Fatalf("worktree on %q", got)
	}
	calls := h.fr.callsTo("osascript")
	if len(calls) == 0 {
		t.Fatal("no iTerm tab was opened")
	}
	last := calls[len(calls)-1]
	command, title, back := last[3], last[4], last[5]
	if !strings.HasPrefix(command, "cd "+shellQuote(wt)+" && claude \"$(cat '") || title != "TST-6 · Claude" || back != "/dev/ttys007" {
		t.Fatalf("unexpected launch %q / %q / %q", command, title, back)
	}
	promptFile := strings.TrimSuffix(strings.SplitN(command, "$(cat '", 2)[1], "')\"")
	prompt, err := os.ReadFile(promptFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"TST-6", "Rotate credentials", "jira context TST-6", "origin/main", wt} {
		if !strings.Contains(string(prompt), want) {
			t.Errorf("prompt lacks %q:\n%s", want, prompt)
		}
	}
	h.expectWrite("transition TST-6 In Progress")
	h.expect("Claude started on TST-6")
}

func TestResumeAndRemoveWorktree(t *testing.T) {
	h, _, wt := newWorkHarness(t)
	h.selectKey("TST-2")
	h.keys("C")
	h.expect("Go to the Claude waiting for you", "Resume in infra · feat/TST-2-migrate-api")
	h.keys("<down><enter>") // the agent comes first, then the worktree
	calls := h.fr.callsTo("osascript")
	if last := calls[len(calls)-1]; last[3] != "cd "+shellQuote(wt)+" && claude --continue" {
		t.Fatalf("unexpected resume %q", last[3])
	}

	// Remove the worktree from the detail's Work tab (the branch stays).
	h.keys("<enter>2")
	for i := 0; i < 3; i++ {
		h.keys("j") // agent → PR → branch
	}
	h.keys("X")
	h.expect("Remove", "Keep it")
	h.keys("<down><enter>")
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatalf("worktree should be gone: %v", err)
	}
	h.expect("Removed")
}

func TestLaunchContinuesAnExistingBranch(t *testing.T) {
	h, root, _ := newWorkHarness(t)
	h.selectKey("TST-5")
	h.keys("Cinfra<enter>")
	h.expect("codex/tst-5-login-fix", "existing branch")
	h.keys("<enter>")
	wt := filepath.Join(root, ".worktrees", "infra-TST-5")
	if got := gitIn(t, wt, "branch", "--show-current"); got != "codex/tst-5-login-fix" {
		t.Fatalf("worktree on %q, want the existing branch", got)
	}
}
