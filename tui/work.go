package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// The work index links Jira tickets to what is happening on this machine
// and on GitHub, using the ticket key the branch names already carry
// (feat/PROJ-324-…, codex/PROJ-323-…):
//
//   - local branches and worktrees in the repos folder,
//   - pull requests that mention the key (one GitHub search for the org),
//   - Claude Code sessions running in those checkouts, and whether they
//     are working or waiting for you.
//
// All of it is best-effort and read-only; failures just leave gaps.

type repoInfo struct {
	name  string // directory name
	path  string
	owner string // GitHub owner of origin
	slug  string // owner/name
}

type gitBranch struct {
	repo, repoPath string
	name           string
	sha            string
	when           time.Time
	upstream       string
	track          string // "ahead 2, behind 1"
	worktree       string // where it's checked out ("" = nowhere)
	mainCheckout   bool   // checked out in the repo's own folder
	prunable       bool   // its worktree folder is gone
}

// live reports a branch that still looks like work in progress: not merged
// away (upstream gone), its worktree folder not deleted, and either checked
// out in a worktree of its own or touched in the last month.
func (b gitBranch) live() bool {
	if b.prunable || strings.Contains(b.track, "gone") {
		return false
	}
	if b.worktree != "" && !b.mainCheckout {
		return true
	}
	return now().Sub(b.when) < 30*24*time.Hour
}

type pullRequest struct {
	repo    string // owner/name
	number  int
	title   string
	url     string
	state   string // open, draft, merged, closed
	updated time.Time
}

// prDetail is the slower per-PR info fetched when a ticket is looked at.
type prDetail struct {
	checks  string // pass, fail, pending, none
	review  string // APPROVED, CHANGES_REQUESTED, REVIEW_REQUIRED
	loading bool
	err     error
}

type agentSession struct {
	pid     int
	tty     string // /dev/ttys003
	cwd     string
	branch  string
	state   string // working, waiting, unknown
	lastAt  time.Time
	lastMsg string
}

type ticketWork struct {
	branches []gitBranch
	prs      []pullRequest
	agents   []agentSession
}

func (t ticketWork) empty() bool {
	return len(t.branches) == 0 && len(t.prs) == 0 && len(t.agents) == 0
}

type workIndex struct {
	root  string
	owner string
	repos []repoInfo

	git    map[string][]gitBranch
	prs    map[string][]pullRequest
	agents []agentSession

	gitAt, prsAt, agentsAt       time.Time
	gitBusy, prsBusy, agentsBusy bool
	gitErr, prsErr               error

	details map[string]*prDetail // by PR url
}

func newWorkIndex() *workIndex {
	return &workIndex{git: map[string][]gitBranch{}, prs: map[string][]pullRequest{}, details: map[string]*prDetail{}}
}

// forKey gathers everything known about one ticket.
func (w *workIndex) forKey(key string, re *regexp.Regexp) ticketWork {
	t := ticketWork{branches: w.git[key], prs: w.prs[key]}
	for _, a := range w.agents {
		if agentKey(a, w, re) == key {
			t.agents = append(t.agents, a)
		}
	}
	return t
}

// agentKey finds the ticket an agent works on: from its branch, else from
// its folder (a worktree named after the key).
func agentKey(a agentSession, w *workIndex, re *regexp.Regexp) string {
	if re == nil {
		return ""
	}
	if k := keyIn(a.branch, re); k != "" {
		return k
	}
	for _, bs := range w.git {
		for _, b := range bs {
			if b.worktree != "" && (a.cwd == b.worktree || strings.HasPrefix(a.cwd, b.worktree+"/")) {
				return keyIn(b.name, re)
			}
		}
	}
	return keyIn(filepath.Base(a.cwd), re)
}

// activeKeys lists tickets with work in flight: a running agent, an open
// PR, a live worktree, or a branch touched in the last two weeks.
func (w *workIndex) activeKeys(re *regexp.Regexp) []string {
	set := map[string]bool{}
	for _, a := range w.agents {
		if k := agentKey(a, w, re); k != "" {
			set[k] = true
		}
	}
	for k, prs := range w.prs {
		for _, p := range prs {
			if p.state == "open" || p.state == "draft" {
				set[k] = true
			}
		}
	}
	for k, bs := range w.git {
		for _, b := range bs {
			if b.live() {
				set[k] = true
			}
		}
	}
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keyLess(keys[i], keys[j]) })
	return keys
}

// keyPattern matches issue keys of the given projects inside branch names,
// folders and PR titles, case-insensitively (branch names get lowercased).
// Boundaries are checked by hand (Go regexps have no lookarounds), so
// adjacent keys like "PROJ-1,PROJ-2" are both found.
func keyPattern(projects []string) *regexp.Regexp {
	var alts []string
	seen := map[string]bool{}
	for _, p := range projects {
		p = strings.ToUpper(strings.TrimSpace(p))
		if p != "" && !seen[p] {
			seen[p] = true
			alts = append(alts, regexp.QuoteMeta(p))
		}
	}
	if len(alts) == 0 {
		return nil
	}
	sort.Strings(alts)
	return regexp.MustCompile(`(?i)(?:` + strings.Join(alts, "|") + `)-\d+`)
}

func isAlnum(b byte) bool {
	return b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

func keysIn(s string, re *regexp.Regexp) []string {
	if re == nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, loc := range re.FindAllStringIndex(s, -1) {
		if loc[0] > 0 && isAlnum(s[loc[0]-1]) {
			continue // "XPROJ-1"
		}
		if loc[1] < len(s) && s[loc[1]] >= '0' && s[loc[1]] <= '9' {
			continue
		}
		k := strings.ToUpper(s[loc[0]:loc[1]])
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}

func keyIn(s string, re *regexp.Regexp) string {
	if ks := keysIn(s, re); len(ks) > 0 {
		return ks[0]
	}
	return ""
}

// projectKeys are the projects whose keys we look for: the profile's plus
// any seen in loaded issues.
func (c *appCtx) projectKeys() []string {
	set := map[string]bool{strings.ToUpper(c.project()): true}
	for k := range c.store.issues {
		if i := strings.LastIndexByte(k, '-'); i > 0 {
			set[k[:i]] = true
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (c *appCtx) keyRe() *regexp.Regexp { return keyPattern(c.projectKeys()) }

// ─── repos folder ────────────────────────────────────────────────

// reposRoot is where the user keeps checkouts: the saved setting, else
// $JIRA_REPOS, else the first usual suspect holding at least two repos.
func (c *appCtx) reposRoot() string {
	if c.state.ReposRoot != "" {
		return expandHome(c.state.ReposRoot)
	}
	if env := os.Getenv("JIRA_REPOS"); env != "" {
		return expandHome(env)
	}
	if c.rootGuess == nil {
		g := guessReposRoot()
		c.rootGuess = &g
	}
	return *c.rootGuess
}

func guessReposRoot() string {
	home, _ := os.UserHomeDir()
	var candidates []string
	globbed, _ := filepath.Glob(filepath.Join(home, "*", "github"))
	candidates = append(candidates, globbed...)
	for _, d := range []string{"repos", "code", "src", "dev", "projects", "git", "work", "workspace", "github"} {
		candidates = append(candidates, filepath.Join(home, d))
	}
	for _, d := range candidates {
		if len(findRepos(d)) >= 2 {
			return d
		}
	}
	return ""
}

// ReposRoot is the repos folder jira ui would use for a profile (saved,
// $JIRA_REPOS or guessed) and how many git repos it holds.
func ReposRoot(profile string) (string, int) {
	ctx := &appCtx{state: loadState(profile)}
	root := ctx.reposRoot()
	if root == "" {
		return "", 0
	}
	return root, len(findRepos(root))
}

// ReposIn expands dir and counts the git repos directly inside it.
func ReposIn(dir string) (string, int) {
	dir = expandHome(strings.TrimSpace(dir))
	return dir, len(findRepos(dir))
}

// SetReposRoot saves the repos folder for a profile's jira ui.
func SetReposRoot(profile, dir string) error {
	dir, n := ReposIn(dir)
	if n == 0 {
		return fmt.Errorf("no git repositories directly inside %s", tildify(dir))
	}
	st := loadState(profile)
	st.ReposRoot = tildify(dir)
	st.save()
	return nil
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[2:])
	}
	return p
}

// tildify shortens a path under the home folder for display.
func tildify(p string) string {
	home, _ := os.UserHomeDir()
	if home != "" && strings.HasPrefix(p, home+"/") {
		return "~" + p[len(home):]
	}
	return p
}

// findRepos lists the git repositories directly under root (worktrees,
// whose .git is a file, are found through their repo instead).
func findRepos(root string) []repoInfo {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []repoInfo
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		p := filepath.Join(root, e.Name())
		if fi, err := os.Stat(filepath.Join(p, ".git")); err == nil && fi.IsDir() {
			out = append(out, repoInfo{name: e.Name(), path: p})
		}
	}
	return out
}

// ─── git scan ────────────────────────────────────────────────────

type gitScannedMsg struct {
	root  string
	repos []repoInfo
	owner string
	byKey map[string][]gitBranch
}

func cmdScanGit(c *appCtx) tea.Cmd {
	root, re := c.reposRoot(), c.keyRe()
	return func() tea.Msg { return scanGit(root, re) }
}

func scanGit(root string, re *regexp.Regexp) gitScannedMsg {
	msg := gitScannedMsg{root: root, byKey: map[string][]gitBranch{}}
	if root == "" || re == nil {
		return msg
	}
	repos := findRepos(root)
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i := range repos {
		wg.Add(1)
		go func(r *repoInfo) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			r.owner, r.slug = originSlug(r.path)
			wts := worktrees(r.path)
			for _, b := range branches(r.path) {
				key := keyIn(b.name, re)
				if key == "" {
					continue
				}
				b.repo, b.repoPath = r.name, r.path
				if wt, ok := wts[b.name]; ok {
					b.worktree, b.prunable = wt.path, wt.prunable
					b.mainCheckout = wt.path == r.path
				}
				mu.Lock()
				msg.byKey[key] = append(msg.byKey[key], b)
				mu.Unlock()
			}
		}(&repos[i])
	}
	wg.Wait()
	for k := range msg.byKey {
		bs := msg.byKey[k]
		sort.SliceStable(bs, func(i, j int) bool {
			if li, lj := bs[i].live(), bs[j].live(); li != lj {
				return li // live work first
			}
			return bs[i].when.After(bs[j].when)
		})
	}
	msg.repos = repos
	msg.owner = commonOwner(repos)
	return msg
}

func commonOwner(repos []repoInfo) string {
	count := map[string]int{}
	best := ""
	for _, r := range repos {
		if r.owner == "" {
			continue
		}
		count[r.owner]++
		if best == "" || count[r.owner] > count[best] || (count[r.owner] == count[best] && r.owner < best) {
			best = r.owner
		}
	}
	return best
}

var reGitHubRemote = regexp.MustCompile(`github\.com[:/]([^/]+)/([^/]+?)(?:\.git)?/?$`)

func originSlug(repo string) (owner, slug string) {
	out, err := sys.run(5*time.Second, repo, "git", "remote", "get-url", "origin")
	if err != nil {
		return "", ""
	}
	m := reGitHubRemote.FindStringSubmatch(strings.TrimSpace(string(out)))
	if m == nil {
		return "", ""
	}
	return m[1], m[1] + "/" + m[2]
}

func branches(repo string) []gitBranch {
	out, err := sys.run(10*time.Second, repo, "git", "for-each-ref",
		"--format=%(refname:short)%09%(objectname:short)%09%(committerdate:unix)%09%(upstream:short)%09%(upstream:track,nobracket)",
		"refs/heads")
	if err != nil {
		return nil
	}
	return parseBranches(string(out))
}

func parseBranches(out string) []gitBranch {
	var bs []gitBranch
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Split(line, "\t")
		if len(f) < 3 || f[0] == "" {
			continue
		}
		b := gitBranch{name: f[0], sha: f[1]}
		if sec, err := strconv.ParseInt(f[2], 10, 64); err == nil {
			b.when = time.Unix(sec, 0)
		}
		if len(f) > 3 {
			b.upstream = f[3]
		}
		if len(f) > 4 {
			b.track = f[4]
		}
		bs = append(bs, b)
	}
	return bs
}

type worktreeInfo struct {
	path     string
	prunable bool
}

// worktrees maps branch name → checkout, from `git worktree list --porcelain`.
func worktrees(repo string) map[string]worktreeInfo {
	out, err := sys.run(10*time.Second, repo, "git", "worktree", "list", "--porcelain")
	if err != nil {
		return nil
	}
	return parseWorktrees(string(out))
}

func parseWorktrees(out string) map[string]worktreeInfo {
	res := map[string]worktreeInfo{}
	var cur worktreeInfo
	branch := ""
	flush := func() {
		if branch != "" {
			res[branch] = cur
		}
		cur, branch = worktreeInfo{}, ""
	}
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			cur.path = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "branch "):
			branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		case strings.HasPrefix(line, "prunable"):
			cur.prunable = true
		}
	}
	flush()
	return res
}

// ─── pull requests ───────────────────────────────────────────────

type prsScannedMsg struct {
	byKey map[string][]pullRequest
	err   error
}

func cmdScanPRs(c *appCtx) tea.Cmd {
	owner, projects, re := c.store.work.owner, c.projectKeys(), c.keyRe()
	return func() tea.Msg { return scanPRs(owner, projects, re) }
}

type ghSearchPR struct {
	Author struct {
		Login string `json:"login"`
		Type  string `json:"type"`
	} `json:"author"`
	Number     int    `json:"number"`
	Title      string `json:"title"`
	Body       string `json:"body"`
	URL        string `json:"url"`
	State      string `json:"state"`
	IsDraft    bool   `json:"isDraft"`
	UpdatedAt  string `json:"updatedAt"`
	Repository struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
}

// scanPRs asks GitHub once for open PRs and once for recently merged ones
// that mention the projects, then files each PR under the keys in its title
// (or, failing that, the first key in its body — bodies often mention
// related tickets too).
func scanPRs(owner string, projects []string, re *regexp.Regexp) prsScannedMsg {
	msg := prsScannedMsg{byKey: map[string][]pullRequest{}}
	if owner == "" || re == nil {
		return msg
	}
	if _, err := lookPath("gh"); err != nil {
		msg.err = err
		return msg
	}
	query := strings.Join(projects, " OR ")
	fields := "number,title,body,url,state,isDraft,updatedAt,repository,author"
	for _, pass := range []struct {
		args   []string
		merged bool
	}{
		{[]string{"--state", "open", "--limit", "100"}, false},
		{[]string{"--merged", "--sort", "updated", "--limit", "60"}, true},
	} {
		args := append([]string{"search", "prs", "--owner", owner, query, "--json", fields}, pass.args...)
		out, err := sys.run(30*time.Second, "", "gh", args...)
		if err != nil {
			msg.err = err
			continue
		}
		var found []ghSearchPR
		if json.Unmarshal(out, &found) != nil {
			continue
		}
		for _, p := range found {
			keys := keysIn(p.Title, re)
			// Bot PRs (Dependabot…) quote other repos' commit messages in
			// their bodies, full of unrelated keys: trust only titles there.
			bot := p.Author.Type == "Bot" || strings.HasSuffix(p.Author.Login, "[bot]")
			if len(keys) == 0 && !bot {
				if k := keyIn(p.Body, re); k != "" {
					keys = []string{k}
				}
			}
			pr := pullRequest{repo: p.Repository.NameWithOwner, number: p.Number, title: p.Title, url: p.URL, state: "open"}
			switch {
			case pass.merged:
				pr.state = "merged"
			case p.IsDraft:
				pr.state = "draft"
			case p.State == "closed":
				pr.state = "closed"
			}
			pr.updated, _ = time.Parse(time.RFC3339, p.UpdatedAt)
			for _, k := range keys {
				msg.byKey[k] = append(msg.byKey[k], pr)
			}
		}
	}
	return msg
}

type prDetailMsg struct {
	url    string
	detail prDetail
}

// cmdPRDetail fetches CI and review state for one PR.
func cmdPRDetail(url string) tea.Cmd {
	return func() tea.Msg {
		out, err := sys.run(20*time.Second, "", "gh", "pr", "view", url, "--json", "statusCheckRollup,reviewDecision")
		if err != nil {
			return prDetailMsg{url: url, detail: prDetail{err: err}}
		}
		var v struct {
			ReviewDecision    string `json:"reviewDecision"`
			StatusCheckRollup []struct {
				Status     string `json:"status"`
				Conclusion string `json:"conclusion"`
				State      string `json:"state"`
			} `json:"statusCheckRollup"`
		}
		if err := json.Unmarshal(out, &v); err != nil {
			return prDetailMsg{url: url, detail: prDetail{err: err}}
		}
		d := prDetail{review: v.ReviewDecision, checks: "none"}
		pending, failed, ok := 0, 0, 0
		for _, c := range v.StatusCheckRollup {
			s := strings.ToUpper(c.Conclusion + c.State)
			switch {
			case strings.Contains(s, "FAIL") || strings.Contains(s, "ERROR") || strings.Contains(s, "TIMED_OUT") || strings.Contains(s, "CANCEL"):
				failed++
			case c.Status != "" && c.Status != "COMPLETED", s == "PENDING", s == "EXPECTED":
				pending++
			default:
				ok++
			}
		}
		switch {
		case failed > 0:
			d.checks = "fail"
		case pending > 0:
			d.checks = "pending"
		case ok > 0:
			d.checks = "pass"
		}
		return prDetailMsg{url: url, detail: d}
	}
}
