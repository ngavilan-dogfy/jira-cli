package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ngavilan-dogfy/jira-cli/config"
	"github.com/ngavilan-dogfy/jira-cli/jira"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// screen is a full-window view in the navigation stack (browser, board,
// issue detail). It renders everything except the footer.
type screen interface {
	Init() tea.Cmd
	Update(tea.Msg) (screen, tea.Cmd)
	View() string
	SetSize(w, h int)
	Hints() []hint
	// Capturing reports that a text input inside the screen has focus, so
	// global single-key shortcuts must not fire.
	Capturing() bool
}

// modal is a box drawn over the (dimmed) screens.
type modal interface {
	Init() tea.Cmd
	Update(tea.Msg) (modal, tea.Cmd)
	View() string
	SetSize(w, h int)
}

type App struct {
	ctx     *appCtx
	screens []screen
	modals  []modal

	toast   *toastMsg
	toastID int

	tickGen int
	open    string // issue to open once the screen is sized

	descEdits map[string]descEdit // editor sessions editing a description
	workJQL   string              // query the Work tab was last loaded with
}

// How often the work index refreshes: agents are cheap (pgrep + a few
// transcript tails), git spawns a couple of commands per repo, PR search
// hits GitHub's search API.
const (
	agentsEvery = 10 * time.Second
	gitEvery    = time.Minute
	prsEvery    = 3 * time.Minute
)

type descEdit struct {
	key      string
	original []byte
	md       string
}

const (
	minW = 50
	minH = 12

	autoRefreshEvery = 3 * time.Minute
)

// Options tweak how the TUI starts.
type Options struct {
	Open string // issue key to open on top of the list
}

// New builds the TUI. It queries the terminal palette, so it must be called
// before the Bubble Tea program starts.
func New(cfg *config.Profile, client *jira.Client, opts Options) *App {
	initTheme()
	ctx := &appCtx{cfg: cfg, client: client, store: newStore(), state: loadState(cfg.Name)}
	a := &App{ctx: ctx, open: opts.Open, descEdits: map[string]descEdit{}}
	if ctx.state.Scope == scopeJQL && ctx.state.JQL == "" {
		ctx.state.Scope = scopeMine
	}
	if ctx.state.Board {
		a.screens = []screen{newBoard(ctx)}
	} else {
		a.screens = []screen{newBrowse(ctx)}
	}
	return a
}

func (a *App) Init() tea.Cmd {
	var open tea.Cmd
	if a.open != "" {
		open = openIssue(a.open)
	}
	w := a.ctx.store.work
	w.gitBusy, w.agentsBusy = true, true
	return tea.Batch(
		open,
		cmdScanGit(a.ctx),
		cmdScanAgents(),
		tickFn(agentsEvery, func(time.Time) tea.Msg { return workTickMsg{"agents"} }),
		tickFn(gitEvery, func(time.Time) tea.Msg { return workTickMsg{"git"} }),
		tickFn(prsEvery, func(time.Time) tea.Msg { return workTickMsg{"prs"} }),
		cmdFetchMyself(a.ctx),
		cmdFetchStatuses(a.ctx),
		a.ensureScope(a.ctx.state.Scope, false),
		a.screens[0].Init(),
		a.kickTick(),
		tickFn(autoRefreshEvery, func(time.Time) tea.Msg { return autoRefreshMsg{} }),
	)
}

func (a *App) top() screen { return a.screens[len(a.screens)-1] }

// ensureScope loads a scope: instantly from the disk cache when it's the
// first time, then from the API. Fresh data (under a minute old) is reused
// unless force is set.
func (a *App) ensureScope(id string, force bool) tea.Cmd {
	s := a.ctx.store
	d := s.scope(id)
	if d.loading {
		return nil
	}
	if d.loaded && !d.cached && !force && time.Since(d.at) < time.Minute {
		return nil
	}
	jql := a.ctx.scopeJQLFor(id)
	if jql == "" {
		return nil
	}
	if !d.loaded {
		if c := readScopeCache(a.ctx.cfg.Name, id, jql); c != nil {
			s.put(c.Issues...)
			d.keys = keysOf(c.Issues)
			d.loaded, d.cached, d.at = true, true, c.At
		}
	}
	d.loading = true
	d.err = nil
	return tea.Batch(cmdFetchScope(a.ctx, id), a.kickTick())
}

func keysOf(issues []jira.Issue) []string {
	keys := make([]string, len(issues))
	for i, is := range issues {
		keys[i] = is.Key
	}
	return keys
}

// loading reports whether anything user-visible is in flight (drives the
// header spinner).
func (a *App) loading() bool {
	for _, d := range a.ctx.store.scopes {
		if d.loading {
			return true
		}
	}
	return false
}

func (a *App) kickTick() tea.Cmd {
	a.tickGen++
	gen := a.tickGen
	return tickFn(120*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{gen: gen} })
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	s := a.ctx.store
	a.ctx.rev++

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.ctx.w, a.ctx.h = msg.Width, msg.Height
		for _, sc := range a.screens {
			sc.SetSize(msg.Width, max(1, msg.Height-1))
		}
		for _, m := range a.modals {
			m.SetSize(msg.Width, msg.Height)
		}
		return a, nil

	case tickMsg:
		if msg.gen != a.tickGen {
			return a, nil
		}
		a.ctx.spin++
		d := 15 * time.Second
		if a.loading() {
			d = 120 * time.Millisecond
		}
		gen := a.tickGen
		return a, tickFn(d, func(time.Time) tea.Msg { return tickMsg{gen: gen} })

	case autoRefreshMsg:
		cmds = append(cmds, tickFn(autoRefreshEvery, func(time.Time) tea.Msg { return autoRefreshMsg{} }))
		if len(a.modals) == 0 && !a.top().Capturing() {
			cmds = append(cmds, a.ensureScope(a.ctx.state.Scope, true))
		}
		return a, tea.Batch(cmds...)

	case tea.KeyMsg:
		return a, a.handleKey(msg)

	// ── navigation ──
	case pushScreenMsg:
		msg.s.SetSize(a.ctx.w, max(1, a.ctx.h-1))
		a.screens = append(a.screens, msg.s)
		return a, msg.s.Init()
	case popScreenMsg:
		if len(a.screens) > 1 {
			a.screens = a.screens[:len(a.screens)-1]
		}
		return a, nil
	case replaceBaseMsg:
		msg.s.SetSize(a.ctx.w, max(1, a.ctx.h-1))
		a.screens[0] = msg.s
		_, isBoard := msg.s.(*boardScreen)
		a.ctx.state.Board = isBoard
		a.ctx.state.save()
		return a, msg.s.Init()
	case openModalMsg:
		msg.m.SetSize(a.ctx.w, a.ctx.h)
		a.modals = append(a.modals, msg.m)
		return a, msg.m.Init()
	case closeModalMsg:
		if len(a.modals) > 0 {
			a.modals = a.modals[:len(a.modals)-1]
		}
		return a, nil
	case openIssueMsg:
		return a, push(newDetail(a.ctx, msg.key))
	case switchScopeMsg:
		a.ctx.state.Scope = msg.id
		a.ctx.state.save()
		if msg.id == scopeWork {
			a.workJQL = a.ctx.scopeJQLFor(scopeWork)
		}
		cmds = append(cmds, a.ensureScope(msg.id, false))

	// ── feedback ──
	case toastMsg:
		a.toast = &msg
		a.toastID++
		id := a.toastID
		d := 3 * time.Second
		switch msg.kind {
		case toastErr:
			d = 8 * time.Second
		case toastInfo:
			d = 4 * time.Second
		}
		return a, tickFn(d, func(time.Time) tea.Msg { return clearToastMsg{id} })
	case clearToastMsg:
		if msg.id == a.toastID {
			a.toast = nil
		}
		return a, nil
	case errActionMsg:
		return a, toastError(msg.err)

	// ── data ──
	case scopeLoadedMsg:
		d := s.scope(msg.id)
		d.loading = false
		if msg.err != nil {
			d.err = msg.err
			cmds = append(cmds, toastError(msg.err))
			break
		}
		if msg.jql != a.ctx.scopeJQLFor(msg.id) {
			break // the query changed while this one was in flight
		}
		s.put(msg.issues...)
		d.keys = keysOf(msg.issues)
		d.loaded, d.cached, d.truncated, d.at = true, false, msg.truncated, time.Now()
		profile, id, jql, issues := a.ctx.cfg.Name, msg.id, msg.jql, msg.issues
		cmds = append(cmds, func() tea.Msg {
			writeScopeCache(profile, id, jql, issues)
			return nil
		})
	case issueLoadedMsg:
		if msg.err == nil && msg.issue != nil {
			s.put(*msg.issue)
			s.full[msg.key] = true
		}
	case commentsLoadedMsg:
		l := s.commentsOf(msg.key)
		l.loading, l.loaded, l.err, l.at = false, msg.err == nil, msg.err, time.Now()
		if msg.err == nil {
			l.val = msg.comments
		}
	case historyLoadedMsg:
		l := s.historyOf(msg.key)
		l.loading, l.loaded, l.err = false, msg.err == nil, msg.err
		if msg.err == nil {
			l.val = msg.entries
		}
	case childrenLoadedMsg:
		l := s.childrenOf(msg.key)
		l.loading, l.loaded, l.err = false, msg.err == nil, msg.err
		if msg.err == nil {
			s.put(msg.issues...)
			l.val = keysOf(msg.issues)
			sort.Slice(l.val, func(i, j int) bool { return keyLess(l.val[i], l.val[j]) })
		}
	case watchLoadedMsg:
		l := s.watchOf(msg.key)
		l.loading, l.loaded, l.err = false, msg.err == nil, msg.err
		if msg.err == nil {
			l.val = msg.info
		}
	case myselfLoadedMsg:
		if msg.err != nil {
			cmds = append(cmds, toastError(msg.err))
		} else {
			s.myself = msg.me
		}
	case usersLoadedMsg:
		s.users.loading, s.users.loaded, s.users.err = false, msg.err == nil, msg.err
		if msg.err == nil {
			s.users.val = msg.users
		}
	case prioritiesLoadedMsg:
		s.priorities.loading, s.priorities.loaded, s.priorities.err = false, msg.err == nil, msg.err
		if msg.err == nil {
			s.priorities.val = msg.priorities
		}
	case typesLoadedMsg:
		s.types.loading, s.types.loaded, s.types.err = false, msg.err == nil, msg.err
		if msg.err == nil {
			s.types.val = msg.types
		}
	case statusesLoadedMsg:
		s.statuses.loading, s.statuses.loaded = false, msg.err == nil
		if msg.err == nil {
			s.statuses.val = msg.statuses
		}
	case localEditMsg:
		if is := s.issue(msg.key); is != nil {
			cp := *is
			msg.apply(&cp)
			s.issues[msg.key] = &cp
		}
	case mutationDoneMsg:
		cmds = append(cmds, a.afterMutation(msg))
	case descEditReadyMsg:
		return a, a.startDescEdit(msg)

	// ── work index ──
	case workTickMsg:
		cmds = append(cmds, a.workTick(msg.kind))
		return a, tea.Batch(cmds...)
	case rescanWorkMsg:
		cmds = append(cmds, a.scanWork("git"), a.scanWork("agents"))
	case gitScannedMsg:
		w := s.work
		w.gitBusy, w.gitAt = false, time.Now()
		w.root, w.repos, w.git = msg.root, msg.repos, msg.byKey
		if msg.owner != "" {
			w.owner = msg.owner
		}
		if w.prsAt.IsZero() {
			cmds = append(cmds, a.scanWork("prs"))
		}
		cmds = append(cmds, a.syncWorkScope())
	case prsScannedMsg:
		w := s.work
		w.prsBusy, w.prsAt, w.prsErr = false, time.Now(), msg.err
		if msg.err == nil || len(msg.byKey) > 0 {
			w.prs = msg.byKey
		}
		cmds = append(cmds, a.syncWorkScope())
	case agentsScannedMsg:
		w := s.work
		w.agentsBusy, w.agentsAt = false, time.Now()
		w.agents = msg.agents
		cmds = append(cmds, a.syncWorkScope())
	case prDetailMsg:
		d := msg.detail
		s.work.details[msg.url] = &d
	case agentLaunchedMsg:
		switch {
		case msg.err != nil:
			cmds = append(cmds, toastError(fmt.Errorf("couldn't start Claude on %s: %w", msg.key, msg.err)))
		case msg.copied != "":
			cmds = append(cmds, toastNote("Not in iTerm2 — command copied, paste it in a terminal"))
		case msg.resumed:
			cmds = append(cmds, toast("Claude resumed on "+msg.key+" in a new tab"))
		default:
			cmds = append(cmds, toast("Claude started on "+msg.key+" · "+msg.repo+" ("+msg.branch+") in a new tab"))
		}
		// The new process and worktree show up on the next scans; nudge them.
		cmds = append(cmds, a.scanWork("git"),
			tickFn(3*time.Second, func(time.Time) tea.Msg { return rescanWorkMsg{} }))
	case worktreeRemovedMsg:
		if msg.err != nil {
			cmds = append(cmds, toastError(msg.err))
		} else {
			cmds = append(cmds, toast("Removed "+tildify(msg.path)))
		}
		cmds = append(cmds, a.scanWork("git"))
	case editorDoneMsg:
		if ed, ok := a.descEdits[msg.id]; ok {
			delete(a.descEdits, msg.id)
			switch {
			case msg.err != nil:
				return a, toastError(msg.err)
			case strings.TrimSpace(msg.text) == strings.TrimSpace(ed.md):
				return a, toastNote("Description unchanged")
			}
			return a, tea.Sequence(toastNote("Saving description…"), cmdSaveDescription(a.ctx, ed.key, ed.original, msg.text))
		}
	}

	// Data and navigation messages reach every screen and modal: views keep
	// no copies of issues, only keys, so they just re-derive what they show.
	for i, sc := range a.screens {
		ns, c := sc.Update(msg)
		a.screens[i] = ns
		cmds = append(cmds, c)
	}
	for i, m := range a.modals {
		nm, c := m.Update(msg)
		a.modals[i] = nm
		cmds = append(cmds, c)
	}
	return a, tea.Batch(cmds...)
}

// scanWork starts one kind of work scan unless it's already running.
func (a *App) scanWork(kind string) tea.Cmd {
	w := a.ctx.store.work
	switch kind {
	case "agents":
		if !w.agentsBusy {
			w.agentsBusy = true
			return cmdScanAgents()
		}
	case "git":
		if !w.gitBusy {
			w.gitBusy = true
			return cmdScanGit(a.ctx)
		}
	case "prs":
		if !w.prsBusy && w.owner != "" {
			w.prsBusy = true
			return cmdScanPRs(a.ctx)
		}
	}
	return nil
}

// workTick runs a periodic scan and schedules the next one.
func (a *App) workTick(kind string) tea.Cmd {
	every := map[string]time.Duration{"agents": agentsEvery, "git": gitEvery, "prs": prsEvery}[kind]
	next := tickFn(every, func(time.Time) tea.Msg { return workTickMsg{kind} })
	return tea.Batch(next, a.scanWork(kind))
}

// syncWorkScope reloads the Work tab when the set of tickets in flight
// changed (its JQL is a key list derived from the work index).
func (a *App) syncWorkScope() tea.Cmd {
	jql := a.ctx.scopeJQLFor(scopeWork)
	if jql == a.workJQL {
		return nil
	}
	a.workJQL = jql
	if jql == "" {
		d := a.ctx.store.scope(scopeWork)
		d.keys, d.loaded, d.loading, d.at = nil, true, false, time.Now()
		return nil
	}
	if a.ctx.state.Scope != scopeWork {
		// Reload lazily when the tab is opened.
		a.ctx.store.scopes[scopeWork] = &scopeData{}
		return nil
	}
	return a.ensureScope(scopeWork, true)
}

// startDescEdit opens $EDITOR on the description as markdown, or explains
// why it can't be edited without losing formatting.
func (a *App) startDescEdit(msg descEditReadyMsg) tea.Cmd {
	if msg.err != nil {
		return toastError(msg.err)
	}
	a.ctx.store.put(*msg.issue)
	md, ok := editableMarkdown(msg.issue.Fields.Description)
	if !ok {
		return toastError(fmt.Errorf("%s's description uses formatting the markdown editor can't keep "+
			"(mentions, nested lists, line breaks, images…) — o opens it in the browser", msg.key))
	}
	id := nextEditorID()
	a.descEdits[id] = descEdit{key: msg.key, original: msg.issue.Fields.Description, md: md}
	return openEditor(id, msg.key, md)
}

func (a *App) afterMutation(msg mutationDoneMsg) tea.Cmd {
	var cmds []tea.Cmd
	if msg.err != nil {
		cmds = append(cmds, toastError(msg.err))
	} else if msg.ok != "" {
		cmds = append(cmds, toast(msg.ok))
	}
	switch {
	case msg.gone && msg.err == nil:
		a.ctx.store.forget(msg.key)
		// Close any detail screens showing the deleted issue.
		kept := a.screens[:1]
		for _, sc := range a.screens[1:] {
			if d, ok := sc.(*detailScreen); ok && d.key == msg.key {
				continue
			}
			kept = append(kept, sc)
		}
		a.screens = kept
	case msg.key != "":
		// Confirm (or revert) the optimistic edit with the server's copy.
		cmds = append(cmds, cmdFetchIssue(a.ctx, msg.key))
		if l := a.ctx.store.comments[msg.key]; l != nil && l.loaded {
			cmds = append(cmds, cmdFetchComments(a.ctx, msg.key))
		}
		if l := a.ctx.store.history[msg.key]; l != nil && l.loaded {
			cmds = append(cmds, cmdFetchHistory(a.ctx, msg.key))
		}
	}
	if msg.refresh {
		cmds = append(cmds, a.ensureScope(a.ctx.state.Scope, true))
		// A new sub-task shows up under its parent's children too.
		for key, l := range a.ctx.store.children {
			if l.loaded {
				cmds = append(cmds, cmdFetchChildren(a.ctx, key))
			}
		}
	}
	return tea.Batch(cmds...)
}

func (a *App) handleKey(msg tea.KeyMsg) tea.Cmd {
	if msg.String() == "ctrl+c" {
		a.ctx.state.save()
		return tea.Quit
	}
	if len(a.modals) > 0 {
		i := len(a.modals) - 1
		m, cmd := a.modals[i].Update(msg)
		a.modals[i] = m
		return cmd
	}
	top := a.top()
	if !top.Capturing() {
		switch msg.String() {
		case "?":
			return openModal(newHelpModal(a.ctx, a.top()))
		case ":", "ctrl+p":
			return openModal(newPalette(a.ctx, a.top()))
		case "q":
			if len(a.screens) == 1 {
				a.ctx.state.save()
				return tea.Quit
			}
			return pop
		}
	}
	sc, cmd := top.Update(msg)
	a.screens[len(a.screens)-1] = sc
	return cmd
}

func (a *App) View() string {
	w, h := a.ctx.w, a.ctx.h
	if w == 0 || h == 0 {
		return ""
	}
	if w < minW || h < minH {
		msg := sMuted().Render("Terminal too small — need at least 50×12")
		return frame("\n  "+msg, w, h)
	}
	out := frame(a.top().View(), w, h-1) + "\n" + a.footer()
	if len(a.modals) > 0 {
		out = dim(out)
		for _, m := range a.modals {
			b := m.View()
			bw, bh := sw(strings.SplitN(b, "\n", 2)[0]), strings.Count(b, "\n")+1
			x, y := center(w, h, bw, bh)
			out = overlay(out, b, x, y)
		}
	}
	// Long errors don't fit the footer: float them above it.
	if t := a.toast; t != nil && t.kind == toastErr && len(a.modals) == 0 && sw(t.text) > w/2 {
		bw := min(w-4, 72)
		body := strings.Join(wrapPlain(t.text, bw-4), "\n")
		b := box("Error", fg(th.red).Render(body), bw, th.red)
		bh := strings.Count(b, "\n") + 1
		out = overlay(out, b, w-bw-1, max(0, h-1-bh))
	}
	return out
}

// footer: context key hints on the left, the latest toast on the right.
func (a *App) footer() string {
	w := a.ctx.w
	right := ""
	if t := a.toast; t != nil && !(t.kind == toastErr && sw(t.text) > w/2) {
		switch t.kind {
		case toastOK:
			right = fg(th.green).Render("● " + t.text)
		case toastErr:
			right = fg(th.red).Bold(true).Render("× " + t.text)
		default:
			right = fg(th.blue).Render("● " + t.text)
		}
		right = trunc(right, w/2) + " "
	}
	var hints []hint
	if len(a.modals) == 0 {
		hints = a.top().Hints()
	}
	left := " " + hintBar(hints, w-sw(right)-2)
	line := spread(left, right, w)
	return paintBg(line, th.surface)
}

// stripped is used by tests to read a frame as plain text.
func stripped(s string) string { return ansi.Strip(s) }
