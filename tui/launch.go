package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ngavilan-dogfy/jira-cli/jira"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// Starting Claude Code on a ticket: pick where (an existing worktree, the
// Claude already running, or a new worktree in some repo), then a fresh
// worktree gets a conventional branch (<type>/<KEY>-<slug>) off the
// repo's up-to-date default branch, and `claude` opens in a new iTerm tab
// with the ticket as its first prompt. The TUI keeps focus, so several
// agents can be dispatched in a row.

type launchPlan struct {
	key, summary string
	repo         repoInfo
	branch       string
	base         string // default branch on origin, resolved lazily
	worktree     string
	extra        string // user's extra instructions
	jira         bool   // assign me + move to In Progress
}

type agentLaunchedMsg struct {
	key, repo, worktree, branch string
	resumed                     bool
	copied                      string // command copied instead (no iTerm)
	err                         error
}

type baseResolvedMsg struct {
	repoPath, base string
}

// newLaunchPicker is step one: where should Claude work on key? Items
// refresh when a work scan lands (at startup the repos may not be known yet).
func newLaunchPicker(ctx *appCtx, key string) modal {
	p := newPicker("Start Claude on "+key, nil)
	p.boxW = 76
	p.input.Placeholder = "type a repo name"
	fill := func(p *pickerModal) {
		is := ctx.store.issue(key)
		w := ctx.store.work
		tw := w.forKey(key, ctx.keyRe())
		var items []pickItem
		for _, a := range tw.agents {
			label := "Go to the Claude working on it"
			if needsYou(a.state) {
				label = "Go to the Claude waiting for you"
			}
			items = append(items, pickItem{id: "focus:" + a.tty, label: label, extra: "agent claude",
				styled: agentDot(a.state) + " " + label, right: sMuted().Render(tildify(a.cwd))})
		}
		for _, b := range tw.branches {
			if b.worktree == "" || b.prunable {
				continue
			}
			label := "Resume in " + b.repo + " · " + b.name
			items = append(items, pickItem{id: "resume:" + b.worktree, label: label, extra: b.worktree,
				right: sMuted().Render(tildify(b.worktree))})
		}
		for _, r := range rankRepos(ctx, is, tw) {
			items = append(items, pickItem{id: "new:" + r.path, label: "New worktree in " + r.name, extra: r.slug})
		}
		p.setItems(items)
		root := ctx.reposRoot()
		switch {
		case root == "":
			p.empty = "No repositories folder found — : → Set repositories folder"
		case w.gitAt.IsZero():
			p.loading = true
		default:
			p.empty = "No git repositories in " + tildify(root)
		}
	}
	fill(p)
	p.onMsg = func(p *pickerModal, msg tea.Msg) tea.Cmd {
		switch msg.(type) {
		case gitScannedMsg, agentsScannedMsg:
			fill(p)
		}
		return nil
	}
	p.onPick = func(it pickItem) tea.Cmd {
		switch {
		case strings.HasPrefix(it.id, "focus:"):
			return cmdFocusAgent(strings.TrimPrefix(it.id, "focus:"))
		case strings.HasPrefix(it.id, "resume:"):
			return cmdResumeClaude(key, strings.TrimPrefix(it.id, "resume:"))
		case strings.HasPrefix(it.id, "new:"):
			path := strings.TrimPrefix(it.id, "new:")
			for _, r := range ctx.store.work.repos {
				if r.path == path {
					return openModal(newLaunchForm(ctx, key, r))
				}
			}
		}
		return nil
	}
	return p
}

// rankRepos orders repos by how likely they are to be the ticket's: repos
// with its branches, repos named in its summary/description, recently used
// ones, then the rest alphabetically.
func rankRepos(ctx *appCtx, is *jira.Issue, tw ticketWork) []repoInfo {
	repos := append([]repoInfo(nil), ctx.store.work.repos...)
	score := map[string]float64{}
	for _, b := range tw.branches {
		score[b.repoPath] += 100
	}
	if is != nil {
		text := strings.ToLower(is.Fields.Summary + " " + strings.Join(is.Fields.Labels, " ") + " " + jira.ADFToText(is.Fields.Description))
		for _, r := range repos {
			n := countWord(text, strings.ToLower(r.name))
			score[r.path] += float64(min(n, 5)) * 10
		}
	}
	for i, name := range ctx.state.RecentRepos {
		for _, r := range repos {
			if r.name == name {
				score[r.path] += float64(max(1, 8-i))
			}
		}
	}
	sort.SliceStable(repos, func(i, j int) bool {
		si, sj := score[repos[i].path], score[repos[j].path]
		if si != sj {
			return si > sj
		}
		return repos[i].name < repos[j].name
	})
	return repos
}

// countWord counts whole-word occurrences (hyphens and slashes delimit).
func countWord(text, word string) int {
	if word == "" {
		return 0
	}
	n := 0
	for i := 0; ; {
		j := strings.Index(text[i:], word)
		if j < 0 {
			return n
		}
		start, end := i+j, i+j+len(word)
		before := start == 0 || !isAlnum(text[start-1])
		after := end == len(text) || !isAlnum(text[end])
		if before && after {
			n++
		}
		i = end
	}
}

// ─── step two: the form ─────────────────────────────────────────

const (
	lfBranch = iota
	lfJira
	lfExtra
	nLaunchFields
)

type launchForm struct {
	ctx      *appCtx
	existing bool // the branch already exists: it gets a worktree, not a new branch
	plan     launchPlan
	branch   textinput.Model
	extra    textarea.Model
	showJira bool
	jiraWhat string
	focus    int
	err      string
	w, h     int
}

func newLaunchForm(ctx *appCtx, key string, repo repoInfo) modal {
	is := ctx.store.issue(key)
	plan := launchPlan{key: key, repo: repo}
	if is != nil {
		plan.summary = is.Fields.Summary
		plan.branch = branchName(is)
	} else {
		plan.branch = "feat/" + key
	}
	plan.worktree = worktreePath(ctx.reposRoot(), repo.name, key)
	// The ticket already has a live branch in this repo: continue it
	// instead of starting a parallel one.
	existing := false
	for _, b := range ctx.store.work.forKey(key, ctx.keyRe()).branches {
		if b.repoPath == repo.path && b.live() {
			plan.branch, existing = b.name, true
			break
		}
	}
	f := &launchForm{ctx: ctx, plan: plan, existing: existing,
		branch: newInput("branch name", 120),
		extra:  newTextarea("Anything else Claude should know (optional)")}
	f.branch.SetValue(plan.branch)
	f.branch.CursorEnd()
	// Offer the Jira bookkeeping that goes with starting work (assign,
	// move to In Progress), when there's any to do.
	if is != nil {
		var parts []string
		if !ctx.isMe(is.Fields.Assignee) {
			parts = append(parts, "assign to me")
		}
		if k := kindOf(is.Fields.Status); k == kindTodo || k == kindReady {
			if st, ok := progressStatus(ctx); ok {
				parts = append(parts, "move to "+st.Name)
			}
		}
		if len(parts) > 0 {
			f.showJira, f.plan.jira = true, true
			f.jiraWhat = strings.Join(parts, " + ")
		}
	}
	return f
}

// progressStatus finds the project's "In Progress"-like status.
func progressStatus(ctx *appCtx) (jira.StatusField, bool) {
	for _, st := range ctx.store.knownStatuses(ctx.state.Scope) {
		if kindOf(st) == kindProgress {
			return st, true
		}
	}
	return jira.StatusField{}, false
}

// worktreePath is <root>/.worktrees/<repo>-<KEY>.
func worktreePath(root, repo, key string) string {
	return filepath.Join(root, ".worktrees", repo+"-"+key)
}

func (f *launchForm) boxW() int { return max(56, min(96, f.w-8)) }

func (f *launchForm) SetSize(w, h int) {
	f.w, f.h = w, h
	f.branch.Width = f.boxW() - 18
	f.extra.SetWidth(f.boxW() - 4)
	f.extra.SetHeight(max(2, min(5, h-24)))
}

func (f *launchForm) Init() tea.Cmd {
	repoPath := f.plan.repo.path
	return tea.Batch(f.branch.Focus(), func() tea.Msg {
		return baseResolvedMsg{repoPath: repoPath, base: resolveBase(repoPath)}
	})
}

func (f *launchForm) setFocus(n int) tea.Cmd {
	dir := 1
	if n < f.focus {
		dir = -1
	}
	f.focus = (n + nLaunchFields) % nLaunchFields
	if f.focus == lfJira && !f.showJira {
		f.focus = (f.focus + dir + nLaunchFields) % nLaunchFields
	}
	f.branch.Blur()
	f.extra.Blur()
	switch f.focus {
	case lfBranch:
		return f.branch.Focus()
	case lfExtra:
		return f.extra.Focus()
	}
	return nil
}

func (f *launchForm) Update(msg tea.Msg) (modal, tea.Cmd) {
	switch msg := msg.(type) {
	case baseResolvedMsg:
		if msg.repoPath == f.plan.repo.path && f.plan.base == "" {
			f.plan.base = msg.base
		}
		return f, nil
	case tea.KeyMsg:
		switch k := msg.String(); k {
		case "esc":
			return f, closeModal
		case "ctrl+s":
			return f, f.start()
		case "enter":
			if f.focus != lfExtra {
				return f, f.start()
			}
		case "tab", "down":
			if f.focus != lfExtra || k == "tab" {
				return f, f.setFocus(f.focus + 1)
			}
		case "shift+tab", "up":
			if f.focus != lfExtra || k == "shift+tab" {
				return f, f.setFocus(f.focus - 1)
			}
		case " ", "left", "right":
			if f.focus == lfJira {
				f.plan.jira = !f.plan.jira
				return f, nil
			}
		}
		f.err = ""
	}
	var cmd tea.Cmd
	switch f.focus {
	case lfBranch:
		f.branch, cmd = f.branch.Update(msg)
	case lfExtra:
		f.extra, cmd = f.extra.Update(msg)
	}
	return f, cmd
}

func (f *launchForm) start() tea.Cmd {
	branch := strings.TrimSpace(f.branch.Value())
	if branch == "" || strings.ContainsAny(branch, " ~^:?*[\\") {
		f.err = "That isn't a valid branch name"
		return f.setFocus(lfBranch)
	}
	plan := f.plan
	plan.branch = branch
	plan.extra = strings.TrimSpace(f.extra.Value())
	ctx := f.ctx
	cmds := []tea.Cmd{closeModal}
	if plan.jira {
		// Quick Jira bookkeeping first, so the last word is the launch.
		cmds = append(cmds, jiraStartWork(ctx, plan.key))
	}
	cmds = append(cmds, toastNote("Preparing a worktree for "+plan.key+"…"), cmdLaunchClaude(plan))
	ctx.state.touchRepo(plan.repo.name)
	return tea.Sequence(cmds...)
}

// jiraStartWork assigns the ticket to you and moves it to In Progress,
// whichever of the two is still needed.
func jiraStartWork(ctx *appCtx, key string) tea.Cmd {
	is := ctx.store.issue(key)
	if is == nil {
		return nil
	}
	var cmds []tea.Cmd
	if me := ctx.myself(); me != nil && !ctx.isMe(is.Fields.Assignee) {
		cmds = append(cmds, cmdAssign(ctx, key, &jira.UserField{AccountID: me.AccountID, DisplayName: me.DisplayName}))
	}
	if k := kindOf(is.Fields.Status); k == kindTodo || k == kindReady {
		if st, ok := progressStatus(ctx); ok {
			cmds = append(cmds, cmdMoveToStatus(ctx, key, st))
		}
	}
	return tea.Sequence(cmds...)
}

func (f *launchForm) View() string {
	w := f.boxW()
	inner := w - 4
	label := func(n int, s string) string {
		st := sMuted()
		if f.focus == n {
			st = sAccent().Bold(true)
		}
		return st.Render(fit(s, 11))
	}
	info := func(s, v string) string { return sMuted().Render(fit(s, 11)) + v }
	base := sMuted().Render("resolving…")
	if f.plan.base != "" {
		base = "origin/" + f.plan.base
	}
	if f.existing && strings.TrimSpace(f.branch.Value()) == f.plan.branch {
		base = sMuted().Render("existing branch — Claude continues it")
	}
	lines := []string{}
	if f.plan.summary != "" {
		lines = append(lines, sMuted().Render(trunc(f.plan.summary, inner)), "")
	}
	lines = append(lines,
		info("Repo", sBold().Render(f.plan.repo.name)+sMuted().Render("  "+f.plan.repo.slug)),
		label(lfBranch, "Branch")+f.branch.View(),
		info("From", base),
		info("Worktree", truncLeft(tildify(f.plan.worktree), inner-11)),
	)
	if f.showJira {
		mark := sMuted().Render("□ ")
		if f.plan.jira {
			mark = fg(th.green).Render("▣ ")
		}
		lines = append(lines, label(lfJira, "Jira")+mark+f.jiraWhat)
	}
	lines = append(lines, "", label(lfExtra, "Also tell"), f.extra.View(), "")
	if f.err != "" {
		lines = append(lines, fg(th.red).Bold(true).Render("× "+f.err))
	} else {
		lines = append(lines, hintBar([]hint{{gEnter, "start Claude"}, {"tab", "next"}, {"space", "toggle"}, {"ctrl+s", "start"}, {"esc", "cancel"}}, inner))
	}
	return box("Start Claude on "+f.plan.key, strings.Join(lines, "\n"), w, th.accent)
}

// ─── doing it ────────────────────────────────────────────────────

// resolveBase asks origin for its default branch (which some teams rotate,
// e.g. one per sprint), falling back to the local idea of it.
func resolveBase(repo string) string {
	if out, err := sys.run(15*time.Second, repo, "git", "ls-remote", "--symref", "origin", "HEAD"); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			if strings.HasPrefix(line, "ref: refs/heads/") {
				return strings.Fields(strings.TrimPrefix(line, "ref: refs/heads/"))[0]
			}
		}
	}
	if out, err := sys.run(5*time.Second, repo, "git", "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		return strings.TrimPrefix(strings.TrimSpace(string(out)), "origin/")
	}
	return "main"
}

// claudePrompt is Claude's first message: the ticket, where it stands and
// how to read the rest.
func claudePrompt(p launchPlan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Vas a trabajar en el ticket %s de Jira: «%s».\n\n", p.key, p.summary)
	fmt.Fprintf(&b, "- Estás en un worktree dedicado: %s\n", p.worktree)
	fmt.Fprintf(&b, "- Rama %s, creada desde origin/%s.\n", p.branch, p.base)
	fmt.Fprintf(&b, "- Lee el ticket completo con `jira context %s` antes de empezar y cuéntame tu plan.\n", p.key)
	if p.extra != "" {
		fmt.Fprintf(&b, "\n%s\n", p.extra)
	}
	return b.String()
}

func cmdLaunchClaude(p launchPlan) tea.Cmd {
	return func() tea.Msg {
		if demoMode {
			return toastMsg{text: "In the demo nothing starts: with your Jira, C opens Claude Code on " + p.key + " in a new tab", kind: toastInfo}
		}
		fail := func(err error) tea.Msg {
			return agentLaunchedMsg{key: p.key, repo: p.repo.name, err: err}
		}
		if p.base == "" {
			p.base = resolveBase(p.repo.path)
		}
		if _, err := sys.run(60*time.Second, p.repo.path, "git", "fetch", "origin", p.base, "--quiet"); err != nil {
			return fail(fmt.Errorf("git fetch: %w", err))
		}
		path, err := ensureWorktree(p)
		if err != nil {
			return fail(err)
		}
		p.worktree = path
		promptFile, err := writePrompt(p.key, claudePrompt(p))
		if err != nil {
			return fail(err)
		}
		command := "cd " + shellQuote(path) + " && claude \"$(cat " + shellQuote(promptFile) + ")\""
		msg := agentLaunchedMsg{key: p.key, repo: p.repo.name, worktree: path, branch: p.branch}
		return runInTerminal(msg, command, p.key+" · Claude")
	}
}

func cmdResumeClaude(key, worktree string) tea.Cmd {
	return func() tea.Msg {
		if demoMode {
			return toastMsg{text: "In the demo nothing starts: with your Jira, this resumes the Claude Code session", kind: toastInfo}
		}
		command := "cd " + shellQuote(worktree) + " && claude --continue"
		msg := agentLaunchedMsg{key: key, worktree: worktree, resumed: true, repo: filepath.Base(worktree)}
		return runInTerminal(msg, command, key+" · Claude")
	}
}

// runInTerminal opens the command in a new iTerm2 tab, or copies it when
// the TUI runs in another terminal.
func runInTerminal(msg agentLaunchedMsg, command, title string) tea.Msg {
	if !inITerm() {
		if err := writeClipboard(command); err != nil {
			msg.err = err
			return msg
		}
		msg.copied = command
		return msg
	}
	if _, err := openInITerm(command, title); err != nil {
		msg.err = err
	}
	return msg
}

func cmdFocusAgent(tty string) tea.Cmd {
	return func() tea.Msg {
		if tty == "" {
			return toastMsg{text: "That Claude has no terminal to jump to", kind: toastInfo}
		}
		if !inITerm() {
			return toastMsg{text: "Jumping to tabs needs iTerm2 (its terminal is " + tty + ")", kind: toastInfo}
		}
		if err := focusITermTTY(tty); err != nil {
			return toastMsg{text: err.Error(), kind: toastErr}
		}
		return nil
	}
}

func cmdOpenShell(dir, title string) tea.Cmd {
	return func() tea.Msg {
		msg := runInTerminal(agentLaunchedMsg{}, "cd "+shellQuote(dir), title).(agentLaunchedMsg)
		switch {
		case msg.err != nil:
			return toastMsg{text: msg.err.Error(), kind: toastErr}
		case msg.copied != "":
			return toastMsg{text: "Copied: " + msg.copied, kind: toastInfo}
		}
		return toastMsg{text: "Opened a tab in " + tildify(dir), kind: toastOK}
	}
}

// ensureWorktree returns a worktree for the plan's branch, creating the
// branch off origin/<base> when it doesn't exist yet. A branch already
// checked out somewhere is used where it is.
func ensureWorktree(p launchPlan) (string, error) {
	for branch, wt := range worktrees(p.repo.path) {
		if branch == p.branch && !wt.prunable {
			return wt.path, nil
		}
	}
	path := p.worktree
	if _, err := os.Stat(path); err == nil {
		for i := 2; ; i++ {
			alt := fmt.Sprintf("%s-%d", p.worktree, i)
			if _, err := os.Stat(alt); os.IsNotExist(err) {
				path = alt
				break
			}
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	args := []string{"worktree", "add"}
	if _, err := sys.run(5*time.Second, p.repo.path, "git", "rev-parse", "--verify", "--quiet", "refs/heads/"+p.branch); err == nil {
		args = append(args, path, p.branch)
	} else {
		args = append(args, "-b", p.branch, path, "origin/"+p.base)
	}
	if _, err := sys.run(60*time.Second, p.repo.path, "git", args...); err != nil {
		return "", fmt.Errorf("git worktree add: %w", err)
	}
	return path, nil
}

// promptsDir holds the first prompts handed to Claude (outside the repo, so
// they never show up in git status).
func writePrompt(key, text string) (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir = filepath.Join(dir, "jira-cli", "prompts")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(dir, key+"-*.md")
	if err != nil {
		return "", err
	}
	defer f.Close()
	_, err = f.WriteString(text)
	return f.Name(), err
}

// ─── removing a worktree ────────────────────────────────────────

type worktreeRemovedMsg struct {
	path string
	err  error
}

func cmdRemoveWorktree(repoPath, path string) tea.Cmd {
	return func() tea.Msg {
		// No --force: git refuses when there are uncommitted changes,
		// which is exactly when removing would lose work.
		_, err := sys.run(30*time.Second, repoPath, "git", "worktree", "remove", path)
		return worktreeRemovedMsg{path: path, err: err}
	}
}

// newRemoveWorktreeModal confirms removing a worktree (the branch stays).
func newRemoveWorktreeModal(b gitBranch) modal {
	items := []pickItem{
		{id: "no", label: "Keep it"},
		{id: "yes", label: "Remove the worktree folder (branch " + b.name + " stays)", styled: fg(th.red).Render("Remove the worktree folder") + sMuted().Render(" (branch stays)")},
	}
	p := newPicker("Remove "+tildify(b.worktree)+"?", items)
	p.filterable = false
	p.boxW = 72
	p.onPick = func(it pickItem) tea.Cmd {
		if it.id != "yes" {
			return nil
		}
		return cmdRemoveWorktree(b.repoPath, b.worktree)
	}
	return p
}
