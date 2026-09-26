package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ngavilan-dogfy/jira-cli/jira"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// browseScreen is the home screen: the issues of the active tab grouped by
// status (sub-tasks nested under their parents), with a live preview of the
// selected issue on wide terminals.
type browseScreen struct {
	ctx  *appCtx
	w, h int

	rows     []brow
	cursor   int
	offset   int
	selKey   string // issue under the cursor
	selGroup string // group under the cursor (header rows)
	wantKey  string // select this issue as soon as it appears
	lay      listLayout

	filter   textinput.Model
	typing   bool
	query    string
	statuses map[string]bool // status filter; empty = all
	total    int             // issues in scope before filtering
	matched  int

	pvKey    string
	pvScroll int
	pvCache  previewCache

	builtRev int // ctx.rev the rows were derived at
}

type brow struct {
	header bool
	group  string // status name
	status jira.StatusField
	count  int
	folded bool

	key    string
	guides string // tree guides, plain text
	parent string // parent key shown as a tag
}

type listLayout struct {
	keyW     int
	assignee bool
	aW       int
	prio     bool
	parent   bool
	statusW  int // flat lists only: there are no group headers to tell status
	work     bool
}

type previewCache struct {
	key, stamp string
	w          int
	lines      []string
}

type previewSettleMsg struct{ key string }
type refreshMsg struct{}

func newBrowse(ctx *appCtx) *browseScreen {
	ti := newInput("key, words, status, person, label…", 120)
	return &browseScreen{ctx: ctx, filter: ti, statuses: map[string]bool{}}
}

func (b *browseScreen) Init() tea.Cmd { return nil }

func (b *browseScreen) SetSize(w, h int) {
	b.w, b.h = w, h
	b.filter.Width = max(10, w/2)
	b.rebuild()
}

func (b *browseScreen) Capturing() bool { return b.typing }

func (b *browseScreen) Hints() []hint {
	if b.typing {
		return []hint{{"enter", "keep"}, {"esc", "clear"}, {"↑↓", "move"}}
	}
	if r := b.current(); r != nil && r.header {
		return []hint{{gEnter, "fold"}, {"{ }", "groups"}, {"Z", "fold all"}, {"/", "filter"},
			{"tab", "tabs"}, {"n", "new"}, {"b", "board"}, {":", "commands"}, {"?", "help"}}
	}
	return []hint{{gEnter, "open"}, {"C", "Claude"}, {"m", "move"}, {"c", "comment"}, {"/", "filter"},
		{"tab", "tabs"}, {"n", "new"}, {"b", "board"}, {":", "commands"}, {"?", "help"}}
}

func (b *browseScreen) current() *brow {
	if b.cursor >= 0 && b.cursor < len(b.rows) {
		return &b.rows[b.cursor]
	}
	return nil
}

func (b *browseScreen) selectedIssue() *jira.Issue {
	if r := b.current(); r != nil && !r.header {
		return b.ctx.store.issue(r.key)
	}
	return nil
}

func (b *browseScreen) Update(msg tea.Msg) (screen, tea.Cmd) {
	var cmd tea.Cmd
	prev := b.selKey
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if b.typing {
			cmd = b.typeFilter(msg)
		} else {
			cmd = b.key(msg)
		}
	case switchScopeMsg:
		b.cursor, b.offset, b.selKey, b.selGroup, b.pvScroll = 0, 0, "", "", 0
		b.statuses = map[string]bool{}
	case mutationDoneMsg:
		if msg.created != "" {
			b.wantKey = msg.created
		}
	case previewSettleMsg:
		if msg.key == b.selKey {
			cmd = b.prefetch(msg.key)
		}
	default:
		if b.typing {
			b.filter, cmd = b.filter.Update(msg)
		}
	}
	b.rebuild()
	if b.selKey != prev && b.selKey != "" {
		key := b.selKey
		cmd = tea.Batch(cmd, tickFn(250*time.Millisecond, func(time.Time) tea.Msg { return previewSettleMsg{key} }))
	}
	return b, cmd
}

// prefetch loads what the preview of a settled selection needs beyond the
// search result: the children of epics.
func (b *browseScreen) prefetch(key string) tea.Cmd {
	cmds := []tea.Cmd{ensurePRDetails(b.ctx, key)}
	is := b.ctx.store.issue(key)
	if is != nil && isEpic(is.Fields.IssueType) {
		if l := b.ctx.store.childrenOf(key); !l.loaded && !l.loading {
			l.loading = true
			cmds = append(cmds, cmdFetchChildren(b.ctx, key))
		}
	}
	return tea.Batch(cmds...)
}

func (b *browseScreen) key(msg tea.KeyMsg) tea.Cmd {
	k := msg.String()
	switch k {
	case "up", "k":
		b.move(-1)
	case "down", "j":
		b.move(1)
	case "pgup", "ctrl+u":
		b.move(-max(1, b.listH()/2))
	case "pgdown", "ctrl+d":
		b.move(max(1, b.listH()/2))
	case "home", "g":
		b.moveTo(0)
	case "end", "G":
		b.moveTo(len(b.rows) - 1)
	case "{":
		b.jumpGroup(-1)
	case "}":
		b.jumpGroup(1)
	case "enter":
		if r := b.current(); r != nil {
			if r.header {
				b.toggleFold(r.group)
				return nil
			}
			return openIssue(r.key)
		}
	case " ", "z":
		if r := b.current(); r != nil && r.group != "" {
			b.toggleFold(r.group)
		}
	case "Z":
		b.toggleAllFolds()
	case "/":
		b.typing = true
		b.filter.SetValue(b.query)
		b.filter.CursorEnd()
		return b.filter.Focus()
	case "esc":
		switch {
		case b.query != "":
			b.query = ""
			b.filter.SetValue("")
		case len(b.statuses) > 0:
			b.statuses = map[string]bool{}
		}
	case "f":
		return openModal(newStatusFilterModal(b.ctx, b.statuses, func(sel map[string]bool) {
			b.statuses = sel
		}))
	case "s":
		return openModal(newSortModal(b.ctx))
	case "v":
		v := b.ctx.state.view(b.ctx.state.Scope)
		v.Flat = !v.Flat
		b.ctx.state.save()
		if v.Flat {
			return toastNote("Flat list")
		}
		return toastNote("Grouped by status")
	case "p":
		on := !b.previewPref()
		b.ctx.state.Preview = &on
		b.ctx.state.save()
		if on && b.w < previewMinWidth {
			return toastNote(fmt.Sprintf("Preview needs a terminal at least %d columns wide", previewMinWidth))
		}
	case "J", "shift+down":
		b.pvScroll += 3
	case "K", "shift+up":
		b.pvScroll = max(0, b.pvScroll-3)
	case "b":
		return func() tea.Msg { return replaceBaseMsg{newBoard(b.ctx)} }
	case "r":
		return func() tea.Msg { return refreshMsg{} }
	default:
		if cmd, ok := scopeKey(b.ctx, k); ok {
			return cmd
		}
		if cmd, ok := issueAction(b.ctx, k, b.selectedIssue()); ok {
			return cmd
		}
	}
	return nil
}

func (b *browseScreen) typeFilter(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		b.typing = false
		b.query = ""
		b.filter.SetValue("")
		b.filter.Blur()
		return nil
	case "enter":
		b.typing = false
		b.filter.Blur()
		if b.query == "" {
			return nil
		}
		// Enter on a single match opens it straight away.
		if b.matched == 1 {
			for _, r := range b.rows {
				if !r.header {
					return openIssue(r.key)
				}
			}
		}
		return nil
	case "up", "ctrl+p":
		b.move(-1)
		return nil
	case "down", "ctrl+n":
		b.move(1)
		return nil
	}
	var cmd tea.Cmd
	b.filter, cmd = b.filter.Update(msg)
	if q := b.filter.Value(); q != b.query {
		b.query = q
		b.selKey, b.selGroup = "", "" // jump to the best (first) match
	}
	return cmd
}

// ─── movement & folding ──────────────────────────────────────────

func (b *browseScreen) move(d int) { b.moveTo(b.cursor + d) }

func (b *browseScreen) moveTo(i int) {
	if len(b.rows) == 0 {
		return
	}
	b.cursor = max(0, min(i, len(b.rows)-1))
	b.syncSelection()
}

func (b *browseScreen) syncSelection() {
	if r := b.current(); r != nil {
		b.selKey, b.selGroup = r.key, r.group
		if r.header {
			b.selKey = ""
		}
	}
	if b.selKey != b.pvKey {
		b.pvKey, b.pvScroll = b.selKey, 0
	}
}

// jumpGroup moves to the previous/next group header and scrolls it to the
// top, so the group's issues come into view below it.
func (b *browseScreen) jumpGroup(dir int) {
	for i := b.cursor + dir; i >= 0 && i < len(b.rows); i += dir {
		if b.rows[i].header {
			b.moveTo(i)
			b.offset = max(0, min(i, len(b.rows)-b.listH()))
			return
		}
	}
}

func (b *browseScreen) toggleFold(group string) {
	b.ctx.state.Collapsed[group] = !b.ctx.state.Collapsed[group]
	b.ctx.state.save()
	// Keep the cursor on the header of the group being folded.
	b.selKey, b.selGroup = "", group
}

func (b *browseScreen) toggleAllFolds() {
	anyOpen := false
	for _, r := range b.rows {
		if r.header && !r.folded {
			anyOpen = true
		}
	}
	for _, r := range b.rows {
		if r.header {
			b.ctx.state.Collapsed[r.group] = anyOpen
		}
	}
	b.ctx.state.save()
	if r := b.current(); r != nil && anyOpen {
		b.selKey = ""
		b.selGroup = r.group
	}
}

// ─── rows ────────────────────────────────────────────────────────

func (b *browseScreen) rebuild() {
	ctx := b.ctx
	b.builtRev = ctx.rev
	scope := ctx.state.Scope
	view := ctx.state.view(scope)
	all := ctx.store.scopeIssues(scope)
	terms := queryTerms(b.query)

	list := make([]*jira.Issue, 0, len(all))
	for _, is := range all {
		if len(b.statuses) > 0 && !b.statuses[is.Fields.Status.Name] {
			continue
		}
		if len(terms) > 0 && !issueMatches(is, terms) {
			continue
		}
		list = append(list, is)
	}
	b.total, b.matched = len(all), len(list)
	sortIssues(list, view.Sort)
	b.lay = layoutFor(ctx, list)
	if view.Flat {
		for _, is := range list {
			b.lay.statusW = max(b.lay.statusW, sw(is.Fields.Status.Name)+2)
		}
		b.lay.statusW = min(b.lay.statusW, 18)
	}

	var rows []brow
	if view.Flat {
		for _, is := range list {
			rows = append(rows, brow{key: is.Key, parent: parentKey(is)})
		}
	} else {
		for _, g := range groupByStatus(list) {
			folded := ctx.state.Collapsed[g.status.Name] && len(terms) == 0
			rows = append(rows, brow{header: true, group: g.status.Name, status: g.status, count: len(g.issues), folded: folded})
			if !folded {
				rows = append(rows, treeRows(g.status.Name, g.issues)...)
			}
		}
	}
	b.rows = rows
	b.restoreSelection()
}

func (b *browseScreen) restoreSelection() {
	find := func(pred func(brow) bool) int {
		for i, r := range b.rows {
			if pred(r) {
				return i
			}
		}
		return -1
	}
	i := -1
	if b.wantKey != "" {
		if i = find(func(r brow) bool { return !r.header && r.key == b.wantKey }); i >= 0 {
			b.wantKey = ""
		}
	}
	if i < 0 && b.selKey != "" {
		i = find(func(r brow) bool { return !r.header && r.key == b.selKey })
	}
	if i < 0 && b.selKey == "" && b.selGroup != "" {
		i = find(func(r brow) bool { return r.header && r.group == b.selGroup })
	}
	if i < 0 && b.selKey == "" && b.selGroup == "" {
		// Fresh list: land on the first issue, not on a header.
		i = find(func(r brow) bool { return !r.header })
	}
	if i < 0 {
		i = b.cursor // the selected issue vanished: stay at the same height
	}
	b.cursor = max(0, min(i, len(b.rows)-1))
	b.syncSelection()
	b.scrollIntoView()
}

func (b *browseScreen) listH() int {
	h := b.h - 1
	if b.filterBarShown() {
		h--
	}
	return max(1, h)
}

func (b *browseScreen) scrollIntoView() {
	h := b.listH()
	margin := min(3, h/4)
	headerOnTop := b.cursor == b.offset && b.cursor < len(b.rows) && b.rows[b.cursor].header
	if b.cursor < b.offset+margin && !headerOnTop {
		b.offset = b.cursor - margin
	}
	if b.cursor > b.offset+h-1-margin {
		b.offset = b.cursor - h + 1 + margin
	}
	b.offset = max(0, min(b.offset, len(b.rows)-h))
}

func (b *browseScreen) filterBarShown() bool {
	return b.typing || b.query != "" || len(b.statuses) > 0
}

// ─── grouping, sorting, matching ─────────────────────────────────

type statusGroup struct {
	status jira.StatusField
	issues []*jira.Issue
}

func groupByStatus(list []*jira.Issue) []statusGroup {
	idx := map[string]int{}
	var groups []statusGroup
	for _, is := range list {
		name := is.Fields.Status.Name
		i, ok := idx[name]
		if !ok {
			i = len(groups)
			idx[name] = i
			groups = append(groups, statusGroup{status: is.Fields.Status})
		}
		groups[i].issues = append(groups[i].issues, is)
	}
	sort.SliceStable(groups, func(i, j int) bool {
		a, b := groups[i].status, groups[j].status
		if ra, rb := groupRank(a), groupRank(b); ra != rb {
			return ra < rb
		}
		if ra, rb := workflowRank(a), workflowRank(b); ra != rb {
			return ra > rb // within a family, the most advanced first
		}
		return a.Name < b.Name
	})
	return groups
}

// treeRows nests issues under a parent present in the same group. Roots keep
// the list order; children follow key order (their natural sequence).
func treeRows(group string, issues []*jira.Issue) []brow {
	in := map[string]bool{}
	for _, is := range issues {
		in[is.Key] = true
	}
	kids := map[string][]*jira.Issue{}
	var roots []*jira.Issue
	for _, is := range issues {
		if p := parentKey(is); p != "" && p != is.Key && in[p] {
			kids[p] = append(kids[p], is)
		} else {
			roots = append(roots, is)
		}
	}
	for _, ks := range kids {
		sort.Slice(ks, func(i, j int) bool { return keyLess(ks[i].Key, ks[j].Key) })
	}
	var rows []brow
	seen := map[string]bool{}
	var walk func(is *jira.Issue, prefix string, last bool, depth int)
	walk = func(is *jira.Issue, prefix string, last bool, depth int) {
		if seen[is.Key] {
			return
		}
		seen[is.Key] = true
		guides := ""
		childPrefix := ""
		if depth > 0 {
			if last {
				guides = prefix + gTreeEnd + " "
				childPrefix = prefix + "  "
			} else {
				guides = prefix + gTreeMid + " "
				childPrefix = prefix + gTreeBar + " "
			}
		}
		r := brow{key: is.Key, group: group, guides: guides}
		if depth == 0 {
			r.parent = parentKey(is)
		}
		rows = append(rows, r)
		ch := kids[is.Key]
		for i, c := range ch {
			walk(c, childPrefix, i == len(ch)-1, depth+1)
		}
	}
	for _, is := range roots {
		walk(is, "", true, 0)
	}
	// Cycles can't happen in Jira, but never drop an issue if they did.
	for _, is := range issues {
		if !seen[is.Key] {
			rows = append(rows, brow{key: is.Key, group: group, parent: parentKey(is)})
		}
	}
	return rows
}

func parentKey(is *jira.Issue) string {
	if is.Fields.Parent != nil {
		return is.Fields.Parent.Key
	}
	return ""
}

// keyLess orders PROJ-9 before PROJ-10.
func keyLess(a, b string) bool {
	pa, na := splitKey(a)
	pb, nb := splitKey(b)
	if pa != pb {
		return pa < pb
	}
	return na < nb
}

func splitKey(k string) (string, int) {
	i := strings.LastIndexByte(k, '-')
	if i < 0 {
		return k, 0
	}
	n, _ := strconv.Atoi(k[i+1:])
	return k[:i], n
}

const (
	sortUpdated  = "updated"
	sortCreated  = "created"
	sortPriority = "priority"
	sortKey      = "key"
)

var sortLabels = map[string]string{
	sortUpdated:  "Recently updated",
	sortCreated:  "Recently created",
	sortPriority: "Priority",
	sortKey:      "Key (newest first)",
}

func sortIssues(list []*jira.Issue, mode string) {
	upd := func(is *jira.Issue) time.Time { return jira.ParseTime(is.Fields.Updated) }
	switch mode {
	case sortCreated:
		sort.SliceStable(list, func(i, j int) bool {
			return jira.ParseTime(list[i].Fields.Created).After(jira.ParseTime(list[j].Fields.Created))
		})
	case sortPriority:
		sort.SliceStable(list, func(i, j int) bool {
			pi, pj := priorityLevel(list[i].Fields.Priority), priorityLevel(list[j].Fields.Priority)
			if pi != pj {
				return pi > pj
			}
			return upd(list[i]).After(upd(list[j]))
		})
	case sortKey:
		sort.SliceStable(list, func(i, j int) bool { return keyLess(list[j].Key, list[i].Key) })
	default:
		sort.SliceStable(list, func(i, j int) bool { return upd(list[i]).After(upd(list[j])) })
	}
}

func issueMatches(is *jira.Issue, terms []string) bool {
	f := is.Fields
	var b strings.Builder
	b.WriteString(is.Key + " " + f.Summary + " " + f.Status.Name + " " + f.IssueType.Name)
	if f.Assignee != nil {
		b.WriteString(" " + f.Assignee.DisplayName)
	} else {
		b.WriteString(" unassigned")
	}
	for _, l := range f.Labels {
		b.WriteString(" " + l)
	}
	for _, c := range f.Components {
		b.WriteString(" " + c.Name)
	}
	if f.Parent != nil {
		b.WriteString(" " + f.Parent.Key + " " + f.Parent.Fields.Summary)
	}
	hay := fold(b.String())
	for _, t := range terms {
		if !strings.Contains(hay, t) {
			return false
		}
	}
	return true
}

// layoutFor decides which optional columns earn their space for this list.
func layoutFor(ctx *appCtx, list []*jira.Issue) listLayout {
	lay := listLayout{keyW: 7}
	for _, is := range list {
		lay.keyW = max(lay.keyW, sw(is.Key))
		if priorityLevel(is.Fields.Priority) != 0 {
			lay.prio = true
		}
		if !ctx.isMe(is.Fields.Assignee) {
			lay.assignee = true
		}
		if is.Fields.Assignee != nil && !ctx.isMe(is.Fields.Assignee) {
			lay.aW = max(lay.aW, sw(shortName(is.Fields.Assignee.DisplayName)))
		}
	}
	lay.aW = min(max(lay.aW, 6), 14)
	re := ctx.keyRe()
	for _, is := range list {
		if workCell(ctx.store.work.forKey(is.Key, re)) != "" {
			lay.work = true
			break
		}
	}
	return lay
}

// ─── view ────────────────────────────────────────────────────────

const previewMinWidth = 100

func (b *browseScreen) previewPref() bool {
	if p := b.ctx.state.Preview; p != nil {
		return *p
	}
	return true
}

// split returns the list and preview widths (preview 0 = hidden).
func (b *browseScreen) split() (int, int) {
	if !b.previewPref() || b.w < previewMinWidth {
		return b.w, 0
	}
	pw := min(max(b.w*42/100, 44), 96)
	return b.w - pw, pw
}

func (b *browseScreen) View() string {
	// Modals change filters and sorting without messaging the screen; any
	// message since the last build means the rows may be stale.
	if b.builtRev != b.ctx.rev {
		b.rebuild()
	}
	lines := []string{tabsBar(b.ctx, b.w)}
	h := b.listH()
	lw, pw := b.split()
	list := b.renderList(lw, h)
	if pw > 0 {
		pv := b.renderPreview(pw-3, h)
		sep := sFaint().Render(gTreeBar)
		for i := 0; i < h; i++ {
			lines = append(lines, fit(list[i], lw)+sep+" "+fit(pv[i], pw-2))
		}
	} else {
		lines = append(lines, list...)
	}
	if b.filterBarShown() {
		lines = append(lines, b.filterLine())
	}
	return strings.Join(lines, "\n")
}

// tabsBar is the top bar shared by the browser and the board: project,
// tabs with the active count, and sync state. On narrow terminals inactive
// tabs shrink to their number and the right side sheds detail first.
func tabsBar(ctx *appCtx, w int) string {
	tabs := func(compact bool) string {
		var b strings.Builder
		b.WriteString(" " + pill(ctx.project(), th.accent) + " ")
		if demoMode {
			b.WriteString(pill("DEMO", th.yellow) + " ")
		}
		for i, sd := range ctx.visibleScopes() {
			num := sFaint().Render(strconv.Itoa(i + 1))
			waiting := ""
			if sd.id == scopeWork {
				if n := agentsNeedingYou(ctx); n > 0 {
					waiting = " " + fg(th.green).Bold(true).Render(fmt.Sprintf("●%d", n))
				}
			}
			if sd.id == ctx.state.Scope {
				b.WriteString(" " + num + " " + lipgloss.NewStyle().Bold(true).Foreground(th.accent).Render(sd.title))
				if d := ctx.store.scopes[sd.id]; d != nil && d.loaded {
					n := strconv.Itoa(len(d.keys))
					if d.truncated {
						n += "+"
					}
					b.WriteString(" " + sMuted().Render(n))
				}
				b.WriteString(waiting + " ")
				continue
			}
			if compact {
				b.WriteString(" " + num + waiting)
				continue
			}
			b.WriteString(" " + num + " " + sMuted().Render(sd.title) + waiting + " ")
		}
		return b.String()
	}
	for _, v := range []struct {
		compact bool
		right   string
	}{
		{false, syncStatus(ctx, true)},
		{false, syncStatus(ctx, false)},
		{true, syncStatus(ctx, true)},
		{true, syncStatus(ctx, false)},
		{true, ""},
	} {
		left := tabs(v.compact)
		right := v.right
		if right != "" {
			right += " "
		}
		if sw(left)+2+sw(right) <= w {
			return paintBg(spread(left, right, w), th.surface)
		}
	}
	return paintBg(fit(tabs(true), w), th.surface)
}

// syncStatus describes the active tab's freshness, optionally followed by
// the user's name.
func syncStatus(ctx *appCtx, withUser bool) string {
	var s string
	d := ctx.store.scopes[ctx.state.Scope]
	switch {
	case d == nil:
	case d.loading && d.loaded:
		s = sAccent().Render(ctx.spinner() + " refreshing")
	case d.loading:
		s = sAccent().Render(ctx.spinner() + " loading")
	case d.err != nil:
		s = fg(th.red).Render("× offline · r retries")
	case d.loaded:
		s = sMuted().Render("synced " + ago(d.at))
	}
	if me := ctx.myself(); me != nil && withUser {
		if s != "" {
			s += sFaint().Render("  " + gDot + "  ")
		}
		s += sMuted().Render(firstName(me.DisplayName))
	}
	return s
}

func (b *browseScreen) renderList(w, h int) []string {
	out := make([]string, 0, h)
	d := b.ctx.store.scopes[b.ctx.state.Scope]
	if len(b.rows) == 0 {
		var msg []string
		switch {
		case d == nil || (d.loading && !d.loaded):
			msg = []string{sAccent().Render(b.ctx.spinner()) + " Loading issues…"}
		case d.err != nil && !d.loaded:
			msg = []string{fg(th.red).Render("× Couldn't load this tab"), "",
				sMuted().Render(strings.Join(wrapPlain(d.err.Error(), max(20, w-6)), "\n")), "",
				sMuted().Render("r to retry")}
		case b.query != "" || len(b.statuses) > 0:
			msg = []string{"No issues match the filter.", sMuted().Render("esc clears it")}
		case b.ctx.state.Scope == scopeWork:
			msg = workEmptyState(b.ctx)
		default:
			msg = []string{"Nothing here.", sMuted().Render("tab switches tabs · S searches · n creates an issue")}
		}
		out = append(out, "")
		for _, m := range msg {
			for _, l := range strings.Split(m, "\n") {
				out = append(out, "   "+l)
			}
		}
	} else {
		end := min(len(b.rows), b.offset+h)
		for i := b.offset; i < end; i++ {
			out = append(out, b.renderRow(b.rows[i], w, i == b.cursor))
		}
	}
	for len(out) < h {
		out = append(out, "")
	}
	// Scroll hint on the last line when rows continue below.
	if len(b.rows) > b.offset+h && h > 3 {
		more := len(b.rows) - (b.offset + h)
		out[h-1] = fitRight(sMuted().Render(fmt.Sprintf("↓ %d more ", more)), w)
	}
	return out[:h]
}

func (b *browseScreen) renderRow(r brow, w int, selected bool) string {
	var line string
	if r.header {
		line = b.renderHeader(r, w)
	} else {
		line = b.renderIssue(r, w, selected)
	}
	if selected {
		return paintBg(fit(line, w), th.sel)
	}
	return fit(line, w)
}

func (b *browseScreen) renderHeader(r brow, w int) string {
	fold := sMuted().Render(gExpanded)
	if r.folded {
		fold = sMuted().Render(gCollapsed)
	}
	sel := " "
	if r.group == b.selGroup && b.selKey == "" {
		sel = sAccent().Render(gSel)
	}
	c := statusColor(r.status)
	head := sel + " " + fold + " " + fg(c).Render(statusGlyph(r.status)) + " " +
		fg(c).Bold(true).Render(r.status.Name) + "  " + sMuted().Render(strconv.Itoa(r.count)) + " "
	rule := w - sw(head) - 1
	if rule > 0 {
		head += sFaint().Render(strings.Repeat(gRule, rule))
	}
	return head
}

func (b *browseScreen) renderIssue(r brow, w int, selected bool) string {
	is := b.ctx.store.issue(r.key)
	if is == nil {
		return ""
	}
	f := is.Fields
	lay := b.lay
	done := kindOf(f.Status) == kindDone

	sel := " "
	if selected {
		sel = sAccent().Render(gSel)
	}
	keySt := sMuted()
	sumSt := lipgloss.NewStyle()
	if selected {
		keySt = lipgloss.NewStyle().Bold(true)
		sumSt = sumSt.Bold(true)
	}
	if done && !selected {
		sumSt = sMuted()
	}
	if isEpic(f.IssueType) && !selected {
		keySt = fg(th.magenta)
	}

	left := sel + " " + sFaint().Render(r.guides) + typeIcon(f.IssueType) + " " + keySt.Render(fit(is.Key, lay.keyW)) + "  "
	if lay.statusW > 0 {
		left += fit(statusLabel(f.Status), lay.statusW) + " "
	}

	// Optional columns, dropped right-to-left when the row gets tight.
	showParent := r.parent != ""
	showAssignee := lay.assignee
	showPrio := lay.prio
	right := func() string {
		var s string
		if showParent {
			s += " " + fg(th.magenta).Faint(true).Render(fit(gUp+r.parent, lay.keyW+1))
		}
		if showAssignee {
			name := sFaint().Render("—")
			switch {
			case b.ctx.isMe(f.Assignee):
				name = sMuted().Render("me")
			case f.Assignee != nil:
				name = sMuted().Render(shortName(f.Assignee.DisplayName))
			}
			s += " " + fit(name, lay.aW)
		}
		if showPrio {
			s += " " + priorityMark(f.Priority)
		}
		if lay.work {
			s += " " + fit(workCell(b.ctx.store.work.forKey(is.Key, b.ctx.keyRe())), workCellW)
		}
		s += " " + fitRight(sMuted().Render(age(updatedAt(is))), 6) + " "
		return s
	}
	rs := right()
	for sw(left)+sw(rs)+12 > w && (showParent || showAssignee || showPrio) {
		switch {
		case showParent:
			showParent = false
		case showAssignee:
			showAssignee = false
		default:
			showPrio = false
		}
		rs = right()
	}

	terms := queryTerms(b.query)
	sumW := w - sw(left) - sw(rs)
	summary := highlight(trunc(f.Summary, sumW), terms, sumSt, sumSt.Foreground(th.yellow).Underline(true))
	return left + fit(summary, sumW) + rs
}

func (b *browseScreen) filterLine() string {
	var left string
	switch {
	case b.typing:
		left = sAccent().Bold(true).Render(" / ") + b.filter.View()
	case b.query != "":
		left = sAccent().Render(" / ") + b.query
	}
	if len(b.statuses) > 0 {
		var names []string
		for n := range b.statuses {
			names = append(names, n)
		}
		sort.Strings(names)
		if left != "" {
			left += "   "
		}
		left += " " + sMuted().Render("status ") + strings.Join(names, sMuted().Render(", "))
	}
	right := sMuted().Render(fmt.Sprintf("%d of %d ", b.matched, b.total))
	if !b.typing {
		right = sFaint().Render("esc clears  ") + right
	}
	return paintBg(spread(left, right, b.w), th.surface)
}

// ─── preview ─────────────────────────────────────────────────────

func (b *browseScreen) renderPreview(w, h int) []string {
	var lines []string
	r := b.current()
	switch {
	case r == nil:
	case r.header:
		lines = b.groupSummary(r, w)
	default:
		is := b.ctx.store.issue(r.key)
		if is == nil {
			break
		}
		stamp := is.Fields.Updated + "|" + is.Fields.Status.Name + "|" + strconv.Itoa(len(childKeys(b.ctx, is))) +
			"|" + workStamp(b.ctx, is.Key)
		if a := is.Fields.Assignee; a != nil {
			stamp += a.AccountID
		}
		if b.ctx.store.myself != nil {
			stamp += "|me"
		}
		c := &b.pvCache
		if c.key != is.Key || c.stamp != stamp || c.w != w {
			*c = previewCache{key: is.Key, stamp: stamp, w: w, lines: previewLines(b.ctx, is, w)}
		}
		lines = c.lines
	}

	maxScroll := max(0, len(lines)-h)
	b.pvScroll = min(b.pvScroll, maxScroll)
	out := make([]string, 0, h)
	for i := b.pvScroll; i < len(lines) && len(out) < h; i++ {
		out = append(out, lines[i])
	}
	for len(out) < h {
		out = append(out, "")
	}
	if b.pvScroll < maxScroll && h > 4 {
		out[h-1] = fitRight(sMuted().Render(fmt.Sprintf("↓ %d more lines · J K scroll", maxScroll-b.pvScroll)), w)
	}
	return out
}

// groupSummary is the preview for a status header: who holds what.
func (b *browseScreen) groupSummary(r *brow, w int) []string {
	out := []string{statusLabel(r.status) + "  " + sMuted().Render(fmt.Sprintf("%d issues", r.count)), ""}
	byPerson := map[string]int{}
	byType := map[string]int{}
	for _, is := range b.ctx.store.scopeIssues(b.ctx.state.Scope) {
		if is.Fields.Status.Name != r.group {
			continue
		}
		name := "Unassigned"
		if is.Fields.Assignee != nil {
			name = is.Fields.Assignee.DisplayName
		}
		byPerson[name]++
		byType[is.Fields.IssueType.Name]++
	}
	top := func(m map[string]int) []string {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			if m[keys[i]] != m[keys[j]] {
				return m[keys[i]] > m[keys[j]]
			}
			return keys[i] < keys[j]
		})
		var lines []string
		for i, k := range keys {
			if i == 8 {
				break
			}
			lines = append(lines, spread(k, " "+sMuted().Render(strconv.Itoa(m[k])), w))
		}
		return lines
	}
	out = append(out, sectionRule("People", "", w))
	out = append(out, top(byPerson)...)
	out = append(out, "", sectionRule("Types", "", w))
	out = append(out, top(byType)...)
	out = append(out, "", sMuted().Render(gEnter+" or space folds this group"))
	return out
}
