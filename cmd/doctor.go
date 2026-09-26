package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/ngavilan-dogfy/jira-cli/config"
	"github.com/ngavilan-dogfy/jira-cli/internal/selfupdate"
	"github.com/ngavilan-dogfy/jira-cli/jira"
	"github.com/ngavilan-dogfy/jira-cli/tui"

	"github.com/spf13/cobra"
)

// check is one line of 'jira doctor': what was checked, how it went and,
// when something's off, how to fix it.
type check struct {
	Section string `json:"section"`
	Name    string `json:"name"`
	Status  string `json:"status"` // ok, warn, fail, info
	Detail  string `json:"detail"`
	Fix     string `json:"fix,omitempty"`
}

func printCheck(c check) {
	switch c.Status {
	case "ok":
		sayOK(c.Detail)
		printFix(c.Fix)
	case "warn":
		sayWarn(c.Detail, c.Fix)
	case "fail":
		sayFail(c.Detail, c.Fix)
	default:
		sayInfo(c.Detail)
		printFix(c.Fix)
	}
}

var doctorJSON bool

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check that everything works, with a fix for each problem",
	Long: `Check this install from top to bottom and explain how to fix anything
that's off: the binary and your PATH, the Jira site, your sign-in, the
default project, and the optional tools jira ui uses (git, GitHub CLI,
Claude Code, iTerm2, your repos folder).

Output:
  TTY → a checklist with fixes · --json → {"ok": bool, "checks": [...]}
  Exit code 1 when a check fails (warnings don't count).

Examples:
  jira doctor
  jira doctor --json | jq '.checks[] | select(.status != "ok")'`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDoctor(cfg, doctorJSON)
	},
}

var errChecksFailed = errors.New("some checks failed")

func runDoctor(p *config.Profile, jsonOut bool) error {
	var all []check
	section := ""
	emit := func(c check) {
		all = append(all, c)
		if jsonOut {
			return
		}
		if c.Section != section {
			section = c.Section
			fmt.Println()
			fmt.Println("  " + wzBold.Render(section))
		}
		printCheck(c)
	}
	spin := func(label string, fn func()) {
		if jsonOut {
			fn()
			return
		}
		_ = withSpinner(label, func() error { fn(); return nil })
	}

	if !jsonOut {
		fmt.Println()
		fmt.Println(wzAccent.Render("  jira doctor"))
	}
	for _, c := range installChecks() {
		emit(c)
	}
	var upd check
	hasUpd := false
	spin("Looking for updates", func() { upd, hasUpd = updateCheckLine() })
	if hasUpd {
		emit(upd)
	}
	for _, c := range jiraChecks(p, spin) {
		emit(c)
	}
	for _, c := range extraChecks() {
		emit(c)
	}
	profile := config.ActiveName()
	if p != nil {
		profile = p.Name
	}
	emit(reposCheck(profile))

	failed, warned := 0, 0
	for _, c := range all {
		switch c.Status {
		case "fail":
			failed++
		case "warn":
			warned++
		}
	}
	if jsonOut {
		if err := printJSON(map[string]any{"ok": failed == 0, "checks": all}); err != nil {
			return err
		}
	} else {
		fmt.Println()
		switch {
		case failed > 0:
			fmt.Println("  " + wzFail.Render(plural(failed, "problem")+" to fix") + wzMuted.Render(" — see the hints above each one"))
		case warned > 0:
			fmt.Println("  " + wzOK.Render("jira works.") + wzMuted.Render(" "+plural(warned, "warning")+" above, worth a look."))
		default:
			fmt.Println("  " + wzOK.Render("Everything works."))
		}
		fmt.Println()
	}
	if failed > 0 {
		return quietError{errChecksFailed} // the checklist already explains it
	}
	return nil
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// ─── this install ────────────────────────────────────────────────

func installChecks() []check {
	const sec = "This install"
	b := currentBuild()
	exe, _ := os.Executable()
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	out := []check{{Section: sec, Name: "version", Status: "ok",
		Detail: fmt.Sprintf("jira %s · %s · %s", b.Short(), b.Platform, tildePath(exe))}}

	onPath, err := lookTool("jira")
	if err != nil {
		dir := filepath.Dir(exe)
		out = append(out, check{Section: sec, Name: "path", Status: "warn",
			Detail: "Typing 'jira' doesn't find this binary: " + tildePath(dir) + " isn't in your PATH",
			Fix:    pathFix(dir)})
		return out
	}
	if r, err := filepath.EvalSymlinks(onPath); err == nil {
		onPath = r
	}
	if onPath != exe {
		out = append(out, check{Section: sec, Name: "path", Status: "warn",
			Detail: "Typing 'jira' runs a different copy: " + tildePath(onPath),
			Fix: "An older copy of jira? Reinstall over it. A different Jira tool? Put " +
				tildePath(filepath.Dir(exe)) + " before it in your PATH."})
	}
	return out
}

// pathFix is the line to add to the user's shell config.
func pathFix(dir string) string {
	shell := filepath.Base(os.Getenv("SHELL"))
	home, _ := os.UserHomeDir()
	shown := dir
	if strings.HasPrefix(dir, home+"/") {
		shown = "$HOME" + dir[len(home):]
	}
	switch shell {
	case "fish":
		return "Run: fish_add_path " + shown
	case "zsh":
		return "Run: echo 'export PATH=\"" + shown + ":$PATH\"' >> ~/.zshrc  and open a new terminal"
	case "bash":
		rc := "~/.bashrc"
		if runtime.GOOS == "darwin" {
			rc = "~/.bash_profile"
		}
		return "Run: echo 'export PATH=\"" + shown + ":$PATH\"' >> " + rc + "  and open a new terminal"
	}
	return "Add " + shown + " to your PATH in your shell's config file."
}

// updateCheckLine says whether a newer release exists (release builds
// only; silent when GitHub can't be asked).
func updateCheckLine() (check, bool) {
	t := updateTool()
	cur := t.Current
	if !selfupdate.IsRelease(cur) {
		return check{}, false
	}
	rel, err := selfupdate.Latest(t)
	if err != nil {
		return check{}, false
	}
	if selfupdate.Newer(rel.Tag, cur) {
		return check{Section: "This install", Name: "update", Status: "info",
			Detail: rel.Tag + " is out (you have " + cur + ")", Fix: "Run: jira update"}, true
	}
	return check{Section: "This install", Name: "update", Status: "ok", Detail: "Up to date"}, true
}

// ─── jira ────────────────────────────────────────────────────────

func jiraChecks(p *config.Profile, spin func(string, func())) []check {
	const sec = "Jira"
	if p == nil {
		return []check{{Section: sec, Name: "profile", Status: "fail",
			Detail: "Not connected to Jira yet", Fix: "Run: jira setup"}}
	}
	var out []check
	if p.Name == "env" {
		out = append(out, check{Section: sec, Name: "profile", Status: "ok",
			Detail: "Using JIRA_DOMAIN, JIRA_EMAIL and JIRA_TOKEN from the environment"})
	} else {
		path := filepath.Join(config.ProfileDir(), p.Name+".yaml")
		c := check{Section: sec, Name: "profile", Status: "ok",
			Detail: fmt.Sprintf("Profile %q · %s", p.Name, tildePath(path))}
		if fi, err := os.Stat(path); err == nil && runtime.GOOS != "windows" && fi.Mode().Perm()&0o077 != 0 {
			c.Status = "warn"
			c.Detail += " — other users on this machine can read it"
			c.Fix = "It holds your token. Run: chmod 600 " + tildePath(path)
		}
		out = append(out, c)
	}
	if !p.IsAuthenticated() {
		return append(out, check{Section: sec, Name: "signin", Status: "fail",
			Detail: "Not signed in", Fix: "Run: jira setup"})
	}

	ref, err := parseSite(p.BrowseBaseURL())
	if err != nil {
		return append(out, check{Section: sec, Name: "site", Status: "fail", Detail: err.Error(), Fix: "Run: jira setup"})
	}
	spin("Reaching "+ref.domain+".atlassian.net", func() { err = probeSite(ref) })
	if err != nil {
		return append(out, check{Section: sec, Name: "site", Status: "fail", Detail: err.Error(),
			Fix: "Check your connection or VPN. If the site moved, run: jira setup"})
	}
	out = append(out, check{Section: sec, Name: "site", Status: "ok", Detail: ref.domain + ".atlassian.net is reachable"})

	c := buildClient(p)
	var me *jira.Myself
	spin("Checking your sign-in", func() { me, err = c.GetMyself() })
	if err != nil {
		msg, fix := explainAuthError(err, p.Email)
		if fix == "" || p.AuthMethod == "oauth" {
			fix = "Sign in again with: jira setup"
		}
		return append(out, check{Section: sec, Name: "signin", Status: "fail", Detail: msg, Fix: fix})
	}
	out = append(out, check{Section: sec, Name: "signin", Status: "ok",
		Detail: "Signed in as " + me.DisplayName + " (" + authLabel(p) + ")"})

	if p.Project == "" {
		return append(out, check{Section: sec, Name: "project", Status: "warn",
			Detail: "No default project: short keys like 306 can't expand",
			Fix:    "Run: jira setup → Change the default project"})
	}
	var projects []jira.Project
	spin("Checking project "+p.Project, func() { projects, err = c.GetProjects() })
	if err != nil {
		return append(out, check{Section: sec, Name: "project", Status: "warn",
			Detail: "Couldn't list projects: " + err.Error()})
	}
	name := ""
	for _, pr := range projects {
		if strings.EqualFold(pr.Key, p.Project) {
			name = pr.Name
		}
	}
	if name == "" {
		var keys []string
		for _, pr := range projects {
			keys = append(keys, pr.Key)
		}
		sort.Strings(keys)
		if len(keys) > 8 {
			keys = append(keys[:8], "…")
		}
		return append(out, check{Section: sec, Name: "project", Status: "fail",
			Detail: "Default project " + p.Project + " doesn't exist or you can't see it",
			Fix:    "You can see: " + strings.Join(keys, ", ") + ". Pick one with: jira setup"})
	}
	spin("Trying a search", func() { _, err = c.Search("project = \""+p.Project+"\" ORDER BY updated DESC", 1) })
	if err != nil {
		return append(out, check{Section: sec, Name: "project", Status: "fail",
			Detail: "Project " + p.Project + " (" + name + "): search failed: " + err.Error(),
			Fix:    "Ask your Jira admin for 'Browse projects' permission on " + p.Project})
	}
	return append(out, check{Section: sec, Name: "project", Status: "ok",
		Detail: "Default project " + p.Project + " (" + name + ") · search works"})
}

// ─── extras for jira ui ──────────────────────────────────────────

const extrasSection = "Extras for jira ui (optional)"

// probeTool runs a command with a timeout and returns its combined output;
// lookTool finds one. Both are swapped in tests (no real gh or claude).
var (
	probeTool = func(timeout time.Duration, name string, args ...string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	lookTool = exec.LookPath
)

var reGHAccount = regexp.MustCompile(`Logged in to \S+ (?:account|as) (\S+)`)

// extraChecks covers the tools jira ui links tickets with. None is needed
// by the rest of the CLI, so problems are warnings at most.
func extraChecks() []check {
	var out []check
	add := func(name, status, detail, fix string) {
		out = append(out, check{Section: extrasSection, Name: name, Status: status, Detail: detail, Fix: fix})
	}

	if _, err := lookTool("git"); err != nil {
		fix := "Install git: sudo apt install git (or your package manager)"
		if runtime.GOOS == "darwin" {
			fix = "Install git: xcode-select --install"
		}
		add("git", "warn", "git isn't installed — branches can't be linked to tickets", fix)
	} else {
		v, _ := probeTool(5*time.Second, "git", "--version")
		add("git", "ok", "git "+strings.TrimPrefix(v, "git version "), "")
	}

	if _, err := lookTool("gh"); err != nil {
		fix := "Install it from https://cli.github.com, then run: gh auth login"
		if runtime.GOOS == "darwin" {
			fix = "Install: brew install gh (or https://cli.github.com), then run: gh auth login"
		}
		add("gh", "info", "GitHub CLI (gh) isn't installed — pull requests won't show on tickets", fix)
	} else if status, err := probeTool(10*time.Second, "gh", "auth", "status"); err != nil {
		add("gh", "warn", "GitHub CLI isn't signed in — pull requests won't show on tickets", "Run: gh auth login")
	} else {
		who := ""
		if m := reGHAccount.FindStringSubmatch(status); m != nil {
			who = " as " + m[1]
		}
		add("gh", "ok", "GitHub CLI signed in"+who+" — pull requests show on tickets", "")
	}

	if _, err := lookTool("claude"); err != nil {
		add("claude", "info", "Claude Code isn't installed — needed to start agents with C",
			"Install it from https://claude.com/claude-code")
	} else {
		v, _ := probeTool(10*time.Second, "claude", "--version")
		if i := strings.IndexByte(v, '\n'); i >= 0 {
			v = v[:i]
		}
		add("claude", "ok", "Claude Code "+strings.TrimSuffix(v, " (Claude Code)")+" — C starts an agent on a ticket", "")
	}

	switch {
	case os.Getenv("TERM_PROGRAM") == "iTerm.app":
		add("terminal", "ok", "iTerm2 — agents open in new tabs and you can jump to them", "")
	case runtime.GOOS == "darwin":
		add("terminal", "info", "Not iTerm2 — C copies the command to start the agent, for you to paste in a new tab",
			"Opening tabs automatically needs iTerm2 (https://iterm2.com)")
	default:
		add("terminal", "info", "C copies the command to start the agent, for you to paste in a new tab", "")
	}
	return out
}

func reposCheck(profile string) check {
	c := check{Section: extrasSection, Name: "repos"}
	root, n := tui.ReposRoot(profile)
	if root == "" || n == 0 {
		c.Status, c.Detail = "info", "No repos folder yet — local branches aren't linked to tickets"
		c.Fix = "Set it with 'jira setup' (step 4), or in jira ui: press : → Set repositories folder"
		return c
	}
	c.Status, c.Detail = "ok", fmt.Sprintf("Repos folder %s (%d git repos)", tildePath(root), n)
	return c
}

func init() {
	doctorCmd.Flags().BoolVar(&doctorJSON, "json", false, "Output as JSON")
	rootCmd.AddCommand(doctorCmd)
}
