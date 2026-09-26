package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ngavilan-dogfy/jira-cli/jira"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// detailScreen shows one issue: title block, section tabs, the section
// body, and a metadata sidebar on wide terminals. Sub-tasks and links are
// navigable: enter opens them on top, esc comes back.
type detailScreen struct {
	ctx  *appCtx
	key  string
	w, h int

	tab    int
	scroll [numTabs]int
	cursor [numTabs]int

	cache    [numTabs]previewCache
	askedFor map[string]bool // lazy loads already requested
	curLine  int             // body line of the selected row (list tabs)
}

const (
	tabOverview = iota
	tabWork
	tabComments
	tabChildren
	tabLinks
	tabHistory
	numTabs
)

// listTab reports tabs whose rows are selectable.
func listTab(t int) bool { return t == tabWork || t == tabChildren || t == tabLinks }

const sidebarMinWidth = 110

func newDetail(ctx *appCtx, key string) *detailScreen {
	return &detailScreen{ctx: ctx, key: key, askedFor: map[string]bool{}}
}

func (d *detailScreen) issue() *jira.Issue { return d.ctx.store.issue(d.key) }

func (d *detailScreen) Init() tea.Cmd {
	s := d.ctx.store
	cmds := []tea.Cmd{cmdFetchIssue(d.ctx, d.key)}
	if l := s.commentsOf(d.key); !l.loading {
		l.loading = true
		cmds = append(cmds, cmdFetchComments(d.ctx, d.key))
	}
	if l := s.watchOf(d.key); !l.loading {
		l.loading = true
		cmds = append(cmds, cmdFetchWatch(d.ctx, d.key))
	}
	cmds = append(cmds, d.maybeChildren(), ensurePRDetails(d.ctx, d.key))
	return tea.Batch(cmds...)
}

// maybeChildren fetches children once we know the issue can have them.
func (d *detailScreen) maybeChildren() tea.Cmd {
	is := d.issue()
	if is == nil || d.askedFor["children"] {
		return nil
	}
	if !isEpic(is.Fields.IssueType) && len(is.Fields.Subtasks) == 0 {
		return nil
	}
	d.askedFor["children"] = true
	l := d.ctx.store.childrenOf(d.key)
	l.loading = true
	return cmdFetchChildren(d.ctx, d.key)
}

func (d *detailScreen) SetSize(w, h int) { d.w, d.h = w, h }
func (d *detailScreen) Capturing() bool  { return false }

func (d *detailScreen) Hints() []hint {
	if d.tab == tabWork {
		return []hint{{gEnter, "go"}, {"C", "Claude"}, {"X", "remove worktree"}, {"tab", "section"},
			{"m", "move"}, {"c", "comment"}, {"esc", "back"}, {"?", "help"}}
	}
	hs := []hint{{"tab", "section"}, {"C", "Claude"}, {"m", "move"}, {"c", "comment"}, {"e", "edit"}}
	if d.tab == tabChildren || d.tab == tabLinks {
		hs = append([]hint{{gEnter, "open"}}, hs...)
	}
	if is := d.issue(); is != nil && is.Fields.Parent != nil {
		hs = append(hs, hint{"P", "parent"})
	}
	return append(hs, hint{"o", "browser"}, hint{"esc", "back"}, hint{"?", "help"})
}

func (d *detailScreen) Update(msg tea.Msg) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return d, d.key_(msg.String())
	case issueLoadedMsg:
		if msg.key == d.key {
			if msg.err != nil && d.issue() == nil {
				return d, toastError(msg.err)
			}
			return d, d.maybeChildren()
		}
	}
	return d, nil
}

func (d *detailScreen) setTab(t int) tea.Cmd {
	d.tab = (t + numTabs) % numTabs
	if d.tab == tabHistory && !d.askedFor["history"] {
		d.askedFor["history"] = true
		l := d.ctx.store.historyOf(d.key)
		l.loading = true
		return cmdFetchHistory(d.ctx, d.key)
	}
	return nil
}

func (d *detailScreen) key_(k string) tea.Cmd {
	is := d.issue()
	switch k {
	case "esc", "backspace":
		return pop
	case "tab", "right", "l":
		return d.setTab(d.tab + 1)
	case "shift+tab", "left", "h":
		return d.setTab(d.tab - 1)
	case "1", "2", "3", "4", "5", "6":
		return d.setTab(int(k[0] - '1'))
	case "up", "k":
		d.step(-1)
	case "down", "j":
		d.step(1)
	case "ctrl+u", "pgup":
		d.step(-max(1, d.bodyH()/2))
	case "ctrl+d", "pgdown", " ":
		d.step(max(1, d.bodyH()/2))
	case "home", "g":
		d.step(-1 << 20)
	case "end", "G":
		d.step(1 << 20)
	case "enter":
		if d.tab == tabWork {
			return d.workAction("enter")
		}
		if key := d.selectedRelated(); key != "" {
			return openIssue(key)
		}
	case "C", "X":
		if d.tab == tabWork {
			return d.workAction(k)
		}
		if k == "C" {
			if cmd, ok := issueAction(d.ctx, k, is); ok {
				return cmd
			}
		}
	case "P":
		if is != nil && is.Fields.Parent != nil {
			return openIssue(is.Fields.Parent.Key)
		}
		return toastNote("This issue has no parent")
	case "r":
		d.askedFor = map[string]bool{}
		cmds := []tea.Cmd{cmdFetchIssue(d.ctx, d.key), cmdFetchComments(d.ctx, d.key), cmdFetchWatch(d.ctx, d.key)}
		if d.tab == tabHistory {
			cmds = append(cmds, d.setTab(tabHistory))
		}
		if l := d.ctx.store.children[d.key]; l != nil && l.loaded {
			cmds = append(cmds, cmdFetchChildren(d.ctx, d.key))
		}
		return tea.Batch(append(cmds, toastNote("Reloading "+d.key))...)
	default:
		if cmd, ok := issueAction(d.ctx, k, is); ok {
			return cmd
		}
	}
	return nil
}

// step scrolls text sections or moves the cursor in list sections.
func (d *detailScreen) step(n int) {
	if listTab(d.tab) {
		count := len(d.relatedKeys())
		if d.tab == tabWork {
			count = len(d.workRows())
		}
		d.cursor[d.tab] = max(0, min(d.cursor[d.tab]+n, count-1))
		return
	}
	d.scroll[d.tab] = max(0, d.scroll[d.tab]+n) // clamped at render time
}

func (d *detailScreen) relatedKeys() []string {
	is := d.issue()
	if is == nil {
		return nil
	}
	switch d.tab {
	case tabChildren:
		return childKeys(d.ctx, is)
	case tabLinks:
		var keys []string
		for _, l := range linksOf(is) {
			keys = append(keys, l.key)
		}
		return keys
	}
	return nil
}

func (d *detailScreen) selectedRelated() string {
	keys := d.relatedKeys()
	if c := d.cursor[d.tab]; c >= 0 && c < len(keys) {
		return keys[c]
	}
	return ""
}

// ─── layout ──────────────────────────────────────────────────────

func (d *detailScreen) sidebarW() int {
	if d.w < sidebarMinWidth {
		return 0
	}
	return min(40, max(32, d.w/4))
}

func (d *detailScreen) mainW() int {
	if sb := d.sidebarW(); sb > 0 {
		return d.w - sb - 5 // 2 margin + separator + 2 gap
	}
	return d.w - 4
}

func (d *detailScreen) titleLines() []string {
	is := d.issue()
	w := d.w - 4
	if is == nil {
		return []string{"  " + sAccent().Render(d.ctx.spinner()) + " " + sBold().Render(d.key)}
	}
	f := is.Fields
	head := typeIcon(f.IssueType) + " " + sMuted().Render(f.IssueType.Name) + "  " + sBold().Render(is.Key)
	if priorityLevel(f.Priority) != 0 {
		head += "  " + priorityLabel(f.Priority)
	}
	out := []string{"  " + spread(head, " "+statusLabel(f.Status), w)}
	sum := wrapPlain(f.Summary, w)
	if len(sum) > 3 {
		sum = sum[:3]
		sum[2] = trunc(sum[2]+" "+gEllipsis, w)
	}
	for _, l := range sum {
		out = append(out, "  "+lipgloss.NewStyle().Bold(true).Foreground(th.yellow).Render(l))
	}
	return out
}

// bodyH is what's left under the breadcrumb, title, blank line, tabs and rule.
func (d *detailScreen) bodyH() int {
	return max(1, d.h-len(d.titleLines())-4)
}

func (d *detailScreen) View() string {
	lines := []string{d.breadcrumb()}
	lines = append(lines, d.titleLines()...)
	lines = append(lines, "", d.tabStrip(), "  "+sFaint().Render(strings.Repeat(gRule, max(0, d.w-4))))

	bodyH := d.bodyH()
	mw, sb := d.mainW(), d.sidebarW()
	main := d.window(d.body(mw), bodyH)
	if sb > 0 {
		side := d.sidebar(sb - 1)
		sep := sFaint().Render(gTreeBar)
		for i := 0; i < bodyH; i++ {
			s := ""
			if i < len(side) {
				s = side[i]
			}
			lines = append(lines, "  "+fit(main[i], mw)+"  "+sep+" "+fit(s, sb-1))
		}
	} else {
		for _, l := range main {
			lines = append(lines, "  "+l)
		}
	}
	return strings.Join(lines, "\n")
}

// breadcrumb: the chain of open issues, plus watch state.
func (d *detailScreen) breadcrumb() string {
	crumbs := []string{sMuted().Render(d.ctx.project())}
	if is := d.issue(); is != nil && is.Fields.Parent != nil {
		crumbs = append(crumbs, sMuted().Render(is.Fields.Parent.Key))
	}
	crumbs = append(crumbs, sBold().Render(d.key))
	left := " " + strings.Join(crumbs, sFaint().Render(" › "))

	right := ""
	if l := d.ctx.store.watch[d.key]; l != nil && l.loaded {
		if l.val.watching {
			right = fg(th.cyan).Render("◉ watching")
		} else {
			right = sMuted().Render("○ not watching")
		}
		right += sMuted().Render(fmt.Sprintf(" · %d watcher%s", l.val.count, plural(l.val.count)))
	}
	if d.loadingAny() {
		right = sAccent().Render(d.ctx.spinner()) + "  " + right
	}
	return paintBg(spread(left, right+" ", d.w), th.surface)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func (d *detailScreen) loadingAny() bool {
	s := d.ctx.store
	if s.issue(d.key) == nil || !s.full[d.key] {
		return true
	}
	if l := s.comments[d.key]; l != nil && l.loading {
		return true
	}
	if l := s.children[d.key]; l != nil && l.loading {
		return true
	}
	return false
}

func (d *detailScreen) tabStrip() string {
	is := d.issue()
	counts := [numTabs]string{}
	if l := d.ctx.store.comments[d.key]; l != nil && l.loaded {
		counts[tabComments] = strconv.Itoa(len(l.val))
	}
	childLabel := "Sub-tasks"
	if is != nil {
		if isEpic(is.Fields.IssueType) {
			childLabel = "Issues"
		}
		if n := len(childKeys(d.ctx, is)); n > 0 {
			counts[tabChildren] = strconv.Itoa(n)
		}
		if n := len(linksOf(is)); n > 0 {
			counts[tabLinks] = strconv.Itoa(n)
		}
	}
	tw := d.ctx.store.work.forKey(d.key, d.ctx.keyRe())
	if n := len(tw.agents) + len(tw.prs) + len(tw.branches); n > 0 {
		counts[tabWork] = strconv.Itoa(n)
		if a, ok := bestAgent(tw.agents); ok {
			counts[tabWork] += " " + agentDot(a.state)
		}
	}
	names := [numTabs]string{"Description", "Work", "Comments", childLabel, "Links", "History"}
	var b strings.Builder
	b.WriteString("  ")
	for i, n := range names {
		label := n
		if counts[i] != "" {
			label += " " + counts[i]
		}
		if i == d.tab {
			b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(th.accent).Underline(true).Render(label))
		} else {
			b.WriteString(sMuted().Render(label))
		}
		b.WriteString("   ")
	}
	return b.String()
}

// window slices the body to the visible height, clamping the scroll.
func (d *detailScreen) window(lines []string, h int) []string {
	if listTab(d.tab) {
		// Keep the selected row visible.
		c := d.curLine
		s := d.scroll[d.tab]
		if c < s {
			s = c
		}
		if c >= s+h {
			s = c - h + 1
		}
		d.scroll[d.tab] = s
	}
	maxScroll := max(0, len(lines)-h)
	d.scroll[d.tab] = min(d.scroll[d.tab], maxScroll)
	out := make([]string, 0, h)
	for i := d.scroll[d.tab]; i < len(lines) && len(out) < h; i++ {
		out = append(out, lines[i])
	}
	for len(out) < h {
		out = append(out, "")
	}
	if d.scroll[d.tab] < maxScroll && h > 3 {
		out[h-1] = sMuted().Render(fmt.Sprintf("↓ %d more lines", maxScroll-d.scroll[d.tab]))
	}
	return out
}

func (d *detailScreen) body(w int) []string {
	is := d.issue()
	if is == nil {
		return []string{"", sMuted().Render("Loading…")}
	}
	switch d.tab {
	case tabWork:
		return d.work(w)
	case tabComments:
		return d.comments(w)
	case tabChildren:
		return d.children(w)
	case tabLinks:
		return d.links(w)
	case tabHistory:
		return d.history(w)
	}
	return d.overview(w)
}

func (d *detailScreen) overview(w int) []string {
	is := d.issue()
	c := &d.cache[tabOverview]
	stamp := is.Fields.Updated
	if c.key == d.key && c.stamp == stamp && c.w == w && d.sidebarW() > 0 {
		return c.lines
	}
	var out []string
	if d.sidebarW() == 0 {
		out = append(out, renderMeta(metaRows(d.ctx, is, true), w)...)
		out = append(out, "", sectionRule("Description", "", w))
	}
	if desc := renderADF(is.Fields.Description, w); len(desc) > 0 {
		out = append(out, desc...)
	} else {
		out = append(out, sMuted().Italic(true).Render("No description"))
	}
	if n := len(is.Fields.Attachments); n > 0 {
		out = append(out, "", sectionRule("Attachments", strconv.Itoa(n), w))
		for _, a := range is.Fields.Attachments {
			out = append(out, spread("▣ "+a.Filename, " "+sMuted().Render(humanSize(a.Size)), w))
		}
		out = append(out, sMuted().Render("o opens the issue in the browser to view them"))
	}
	*c = previewCache{key: d.key, stamp: stamp, w: w, lines: out}
	return out
}

func (d *detailScreen) comments(w int) []string {
	l := d.ctx.store.comments[d.key]
	if l == nil || (!l.loaded && l.loading) {
		return []string{"", sAccent().Render(d.ctx.spinner()) + " Loading comments…"}
	}
	if l.err != nil && !l.loaded {
		return []string{"", fg(th.red).Render("× " + l.err.Error())}
	}
	if len(l.val) == 0 {
		return []string{"", sMuted().Render("No comments yet — c writes the first one.")}
	}
	c := &d.cache[tabComments]
	stamp := fmt.Sprintf("%d|%s", len(l.val), l.at)
	if c.key == d.key && c.stamp == stamp && c.w == w {
		return c.lines
	}
	var out []string
	// Newest first: the latest word is usually what you came for.
	for i := len(l.val) - 1; i >= 0; i-- {
		cm := l.val[i]
		mine := d.ctx.isMe(&cm.Author)
		barC := th.faint
		if mine {
			barC = th.accent
		}
		bar := fg(barC).Render("▌") + " "
		who := sBold().Render(cm.Author.DisplayName)
		if mine {
			who += sMuted().Render(" (you)")
		}
		head := bar + who + sMuted().Render("  "+ago(jira.ParseTime(cm.Created)))
		out = append(out, head)
		body := renderADF(cm.Body, w-2)
		if len(body) == 0 {
			body = []string{sMuted().Italic(true).Render("(empty)")}
		}
		out = append(out, prefixLines(body, bar, bar)...)
		out = append(out, "")
	}
	*c = previewCache{key: d.key, stamp: stamp, w: w, lines: out}
	return out
}

func (d *detailScreen) children(w int) []string {
	is := d.issue()
	keys := childKeys(d.ctx, is)
	l := d.ctx.store.children[d.key]
	if len(keys) == 0 {
		if l != nil && l.loading {
			return []string{"", sAccent().Render(d.ctx.spinner()) + " Loading…"}
		}
		if isEpic(is.Fields.IssueType) {
			return []string{"", sMuted().Render("No issues in this epic yet — N creates one.")}
		}
		return []string{"", sMuted().Render("No sub-tasks — N creates one.")}
	}
	done := doneCount(d.ctx, is, keys)
	head := progressBar(done, len(keys), min(24, w/3)) + "  " + sMuted().Render(fmt.Sprintf("%d of %d done", done, len(keys)))
	out := []string{head, ""}
	d.curLine = 2 + d.cursor[d.tab]
	for i, k := range keys {
		st, sum, as := childStub(d.ctx, is, k)
		row := relatedRow(d.ctx, k, st, sum, as, w-2)
		out = append(out, d.cursorRow(i, row, w))
	}
	return out
}

func (d *detailScreen) links(w int) []string {
	is := d.issue()
	links := linksOf(is)
	if len(links) == 0 {
		return []string{"", sMuted().Render("No linked issues.")}
	}
	relW := 0
	for _, l := range links {
		relW = max(relW, sw(l.relation))
	}
	relW = min(relW, w/3)
	out := []string{sMuted().Render(fmt.Sprintf("%d linked issue%s", len(links), plural(len(links)))), ""}
	d.curLine = 2 + d.cursor[d.tab]
	for i, l := range links {
		rel := sMuted().Render(fit(l.relation, relW)) + "  "
		row := rel + relatedRow(d.ctx, l.key, l.status, l.summary, nil, w-2-sw(rel))
		out = append(out, d.cursorRow(i, row, w))
	}
	return out
}

func (d *detailScreen) cursorRow(i int, row string, w int) string {
	if i == d.cursor[d.tab] {
		return paintBg(fit(sAccent().Render(gSel)+" "+row, w), th.sel)
	}
	return fit("  "+row, w)
}

func (d *detailScreen) history(w int) []string {
	l := d.ctx.store.history[d.key]
	if l == nil || (!l.loaded && l.loading) {
		return []string{"", sAccent().Render(d.ctx.spinner()) + " Loading history…"}
	}
	if l.err != nil && !l.loaded {
		return []string{"", fg(th.red).Render("× " + l.err.Error())}
	}
	if len(l.val) == 0 {
		return []string{"", sMuted().Render("No changes recorded.")}
	}
	var out []string
	for _, e := range l.val {
		out = append(out, sBold().Render(e.Author.DisplayName)+sMuted().Render("  "+ago(jira.ParseTime(e.Created))))
		for _, it := range e.Items {
			from, to := strings.TrimSpace(it.FromString), strings.TrimSpace(it.ToString)
			field := fg(th.cyan).Render(it.Field)
			var change string
			switch {
			case strings.EqualFold(it.Field, "description"):
				change = sMuted().Render("edited")
			case from == "":
				change = to
			case to == "":
				change = fg(th.muted).Strikethrough(true).Render(from)
			default:
				change = sMuted().Render(from) + " " + sMuted().Render(gArrow) + " " + to
			}
			for i, line := range wrapStyled(field+"  "+change, w-2) {
				if i == 0 {
					out = append(out, "  "+line)
				} else {
					out = append(out, "    "+line)
				}
			}
		}
		out = append(out, "")
	}
	return out
}

func (d *detailScreen) sidebar(w int) []string {
	is := d.issue()
	if is == nil {
		return nil
	}
	out := renderMeta(metaRows(d.ctx, is, true), w)
	if kids := childKeys(d.ctx, is); len(kids) > 0 {
		done := doneCount(d.ctx, is, kids)
		out = append(out, "", sMuted().Render("Progress"))
		out = append(out, progressBar(done, len(kids), max(4, w-8))+sMuted().Render(fmt.Sprintf(" %d/%d", done, len(kids))))
	}
	return out
}
