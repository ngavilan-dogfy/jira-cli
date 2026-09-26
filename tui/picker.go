package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/ngavilan-dogfy/jira-cli/jira"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// pickerModal is the one list-picker behind every "choose one" dialog: move,
// assign, sort, status filter, command palette. Type to filter, arrows to
// move, enter to pick; multi mode toggles with space.
type pickerModal struct {
	title      string
	items      []pickItem
	view       []int
	cursor     int
	input      textinput.Model
	filterable bool
	digits     bool // 1-9 pick directly (when the filter is empty)
	multi      bool
	chosen     map[string]bool
	loading    bool
	err        error
	empty      string
	boxW       int
	w, h       int

	onPick  func(pickItem) tea.Cmd
	onMulti func(map[string]bool) tea.Cmd
	onMsg   func(p *pickerModal, msg tea.Msg) tea.Cmd
	dynamic func(query string) []pickItem // extra items computed from the query
}

type pickItem struct {
	id     string
	label  string // plain, used for matching
	styled string // optional styled rendering of label
	right  string // styled, right-aligned
	extra  string // more text to match against
	dim    bool
}

func newPicker(title string, items []pickItem) *pickerModal {
	ti := newInput("type to filter", 80)
	p := &pickerModal{title: title, items: items, input: ti, filterable: true, boxW: 60, chosen: map[string]bool{}}
	p.refilter()
	return p
}

func (p *pickerModal) Init() tea.Cmd {
	if p.filterable {
		return p.input.Focus()
	}
	return nil
}

func (p *pickerModal) SetSize(w, h int) { p.w, p.h = w, h }

func (p *pickerModal) setItems(items []pickItem) {
	p.items = items
	p.loading = false
	p.refilter()
}

// all returns the static items plus the ones derived from the query:
// "open this key" goes first, "search Jira for…" last, so real matches win
// the enter key.
func (p *pickerModal) all() []pickItem {
	if p.dynamic == nil {
		return p.items
	}
	var top, bottom []pickItem
	for _, it := range p.dynamic(p.input.Value()) {
		if strings.HasPrefix(it.id, "dyn:search:") {
			bottom = append(bottom, it)
		} else {
			top = append(top, it)
		}
	}
	out := append(append(top, p.items...), bottom...)
	return out
}

func (p *pickerModal) refilter() {
	terms := queryTerms(p.input.Value())
	items := p.all()
	p.view = p.view[:0]
	for i, it := range items {
		if len(terms) > 0 {
			hay := fold(it.label + " " + it.extra + " " + it.id)
			ok := true
			for _, t := range terms {
				if !strings.Contains(hay, t) {
					ok = false
					break
				}
			}
			if !ok && !strings.HasPrefix(it.id, "dyn:") {
				continue
			}
		}
		p.view = append(p.view, i)
	}
	p.cursor = max(0, min(p.cursor, len(p.view)-1))
}

func (p *pickerModal) current() (pickItem, bool) {
	items := p.all()
	if p.cursor >= 0 && p.cursor < len(p.view) {
		return items[p.view[p.cursor]], true
	}
	return pickItem{}, false
}

func (p *pickerModal) Update(msg tea.Msg) (modal, tea.Cmd) {
	if p.onMsg != nil {
		if _, isKey := msg.(tea.KeyMsg); !isKey {
			if cmd := p.onMsg(p, msg); cmd != nil {
				return p, cmd
			}
		}
	}
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		var cmd tea.Cmd
		p.input, cmd = p.input.Update(msg)
		return p, cmd
	}
	switch k := km.String(); k {
	case "esc":
		return p, closeModal
	case "up", "ctrl+p", "ctrl+k":
		p.cursor = max(0, p.cursor-1)
		return p, nil
	case "down", "ctrl+n", "ctrl+j":
		p.cursor = min(len(p.view)-1, p.cursor+1)
		return p, nil
	case "pgup":
		p.cursor = max(0, p.cursor-p.rows())
		return p, nil
	case "pgdown":
		p.cursor = min(len(p.view)-1, p.cursor+p.rows())
		return p, nil
	case " ":
		if p.multi {
			if it, ok := p.current(); ok {
				p.chosen[it.id] = !p.chosen[it.id]
			}
			return p, nil
		}
	case "enter":
		if p.multi {
			sel := map[string]bool{}
			for id, on := range p.chosen {
				if on {
					sel[id] = true
				}
			}
			return p, tea.Sequence(closeModal, p.onMulti(sel))
		}
		if it, ok := p.current(); ok && !it.dim {
			return p, tea.Sequence(closeModal, p.onPick(it))
		}
		return p, nil
	default:
		if p.digits && p.input.Value() == "" && len(k) == 1 && k[0] >= '1' && k[0] <= '9' {
			i := int(k[0] - '1')
			if i < len(p.view) {
				p.cursor = i
				if it, ok := p.current(); ok && !it.dim {
					return p, tea.Sequence(closeModal, p.onPick(it))
				}
			}
			return p, nil
		}
	}
	if !p.filterable {
		return p, nil
	}
	var cmd tea.Cmd
	before := p.input.Value()
	p.input, cmd = p.input.Update(km)
	if p.input.Value() != before {
		p.cursor = 0
		p.refilter()
	}
	return p, cmd
}

func (p *pickerModal) width() int { return max(30, min(p.boxW, p.w-6)) }
func (p *pickerModal) rows() int  { return max(3, min(14, p.h-12)) }

func (p *pickerModal) View() string {
	w := p.width()
	inner := w - 4
	var lines []string
	if p.filterable {
		lines = append(lines, sAccent().Bold(true).Render("› ")+p.input.View(), "")
	}
	items := p.all()
	switch {
	case p.loading:
		lines = append(lines, sMuted().Render("Loading…"))
	case p.err != nil:
		lines = append(lines, wrapStyled(fg(th.red).Render("× "+p.err.Error()), inner)...)
	case len(p.view) == 0:
		e := p.empty
		if e == "" {
			e = "No matches"
		}
		lines = append(lines, sMuted().Render(e))
	}
	n := p.rows()
	start := 0
	if p.cursor >= n {
		start = p.cursor - n + 1
	}
	for vi := start; vi < len(p.view) && vi < start+n; vi++ {
		it := items[p.view[vi]]
		label := it.styled
		if label == "" {
			label = it.label
		}
		if it.dim {
			label = sMuted().Render(it.label)
		}
		prefix := ""
		if p.multi {
			if p.chosen[it.id] {
				prefix = fg(th.green).Render("▣") + " "
			} else {
				prefix = sMuted().Render("□") + " "
			}
		} else if p.digits && vi < 9 {
			prefix = sFaint().Render(strconv.Itoa(vi+1)) + " "
		}
		// The label wins; the right column gets what's left (paths keep
		// their end), or nothing when that's too little to be useful.
		right := it.right
		if room := inner - 2 - sw(prefix+label) - 2; sw(right) > room {
			if room < 12 {
				right = ""
			} else {
				right = truncLeft(right, room)
			}
		}
		row := spread(prefix+label, " "+right, inner-2)
		if vi == p.cursor {
			row = paintBg(sAccent().Render(gSel)+" "+row, th.sel)
		} else {
			row = "  " + row
		}
		lines = append(lines, row)
	}
	if len(p.view) > n {
		lines = append(lines, sMuted().Render(fmt.Sprintf("  %d/%d", p.cursor+1, len(p.view))))
	}
	lines = append(lines, "")
	var hints []hint
	switch {
	case p.multi:
		hints = []hint{{"space", "toggle"}, {gEnter, "apply"}, {"esc", "cancel"}}
	default:
		hints = []hint{{"↑↓", "choose"}, {gEnter, "select"}, {"esc", "cancel"}}
	}
	lines = append(lines, hintBar(hints, inner))
	return box(p.title, strings.Join(lines, "\n"), w, th.accent)
}

// ─── concrete pickers ────────────────────────────────────────────

func newMoveModal(ctx *appCtx, key string) modal {
	p := newPicker("Move "+key, nil)
	p.loading = true
	p.digits = true
	p.boxW = 56
	p.empty = "No transitions available from this status"
	var current string
	if is := ctx.store.issue(key); is != nil {
		current = is.Fields.Status.Name
		p.title = "Move " + key + sMuted().Render("  from ") + statusLabel(is.Fields.Status)
	}
	byID := map[string]jira.Transition{}
	p.onMsg = func(p *pickerModal, msg tea.Msg) tea.Cmd {
		m, ok := msg.(transitionsLoadedMsg)
		if !ok || m.key != key {
			return nil
		}
		if m.err != nil {
			p.loading, p.err = false, m.err
			return nil
		}
		ts := append([]jira.Transition(nil), m.transitions...)
		sort.SliceStable(ts, func(i, j int) bool { return workflowRank(ts[i].To) < workflowRank(ts[j].To) })
		var items []pickItem
		for _, t := range ts {
			byID[t.ID] = t
			it := pickItem{id: t.ID, label: t.To.Name, extra: t.Name, styled: statusLabel(t.To)}
			if !strings.EqualFold(t.Name, t.To.Name) {
				it.right = sMuted().Render(t.Name)
			}
			if t.To.Name == current {
				it.right = sMuted().Render("current")
				it.dim = true
			}
			items = append(items, it)
		}
		p.setItems(items)
		// Start on the first real move.
		for i, vi := range p.view {
			if !p.items[vi].dim {
				p.cursor = i
				break
			}
		}
		return nil
	}
	p.onPick = func(it pickItem) tea.Cmd { return cmdTransition(ctx, key, byID[it.id]) }
	return &initPicker{pickerModal: p, init: cmdFetchTransitions(ctx, key)}
}

// initPicker is a picker that fires a load command when opened.
type initPicker struct {
	*pickerModal
	init tea.Cmd
}

func (p *initPicker) Init() tea.Cmd { return tea.Batch(p.pickerModal.Init(), p.init) }

func (p *initPicker) Update(msg tea.Msg) (modal, tea.Cmd) {
	_, cmd := p.pickerModal.Update(msg)
	return p, cmd
}

func newAssignModal(ctx *appCtx, key string) modal {
	p := newPicker("Assign "+key, nil)
	p.boxW = 56
	fill := func(p *pickerModal) {
		var items []pickItem
		if me := ctx.myself(); me != nil {
			items = append(items, pickItem{id: me.AccountID, label: me.DisplayName, styled: sBold().Render(me.DisplayName), right: sMuted().Render("me"), extra: "me"})
		}
		items = append(items, pickItem{id: "", label: "Unassigned", styled: sMuted().Render("Unassigned"), extra: "nobody none"})
		users := append([]jira.UserField(nil), ctx.store.users.val...)
		sort.Slice(users, func(i, j int) bool { return fold(users[i].DisplayName) < fold(users[j].DisplayName) })
		for _, u := range users {
			if me := ctx.myself(); me != nil && u.AccountID == me.AccountID {
				continue
			}
			items = append(items, pickItem{id: u.AccountID, label: u.DisplayName})
		}
		if is := ctx.store.issue(key); is != nil {
			cur := ""
			if is.Fields.Assignee != nil {
				cur = is.Fields.Assignee.AccountID
			}
			for i := range items {
				if items[i].id == cur {
					items[i].right = sMuted().Render("current")
					items[i].dim = true
				}
			}
		}
		p.setItems(items)
		p.loading = ctx.store.users.loading
	}
	var init tea.Cmd
	if !ctx.store.users.loaded {
		ctx.store.users.loading = true
		init = cmdFetchUsers(ctx)
	}
	fill(p)
	p.onMsg = func(p *pickerModal, msg tea.Msg) tea.Cmd {
		if m, ok := msg.(usersLoadedMsg); ok {
			if m.err != nil {
				p.err = m.err
			}
			fill(p)
		}
		return nil
	}
	p.onPick = func(it pickItem) tea.Cmd {
		if it.id == "" {
			return cmdAssign(ctx, key, nil)
		}
		return cmdAssign(ctx, key, &jira.UserField{AccountID: it.id, DisplayName: it.label})
	}
	return &initPicker{pickerModal: p, init: init}
}

func newStatusFilterModal(ctx *appCtx, current map[string]bool, apply func(map[string]bool)) modal {
	counts := map[string]int{}
	for _, is := range ctx.store.scopeIssues(ctx.state.Scope) {
		counts[is.Fields.Status.Name]++
	}
	var items []pickItem
	for _, st := range ctx.store.knownStatuses(ctx.state.Scope) {
		items = append(items, pickItem{id: st.Name, label: st.Name, styled: statusLabel(st),
			right: sMuted().Render(strconv.Itoa(counts[st.Name]))})
	}
	p := newPicker("Show statuses", items)
	p.multi = true
	p.filterable = false
	p.boxW = 44
	for k, v := range current {
		p.chosen[k] = v
	}
	p.onMulti = func(sel map[string]bool) tea.Cmd {
		apply(sel)
		return nil
	}
	return p
}

func newSortModal(ctx *appCtx) modal {
	v := ctx.state.view(ctx.state.Scope)
	var items []pickItem
	for _, id := range []string{sortUpdated, sortCreated, sortPriority, sortKey} {
		it := pickItem{id: id, label: sortLabels[id]}
		if id == v.Sort {
			it.right = sAccent().Render("●")
		}
		items = append(items, it)
	}
	p := newPicker("Sort by", items)
	p.filterable = false
	p.digits = true
	p.boxW = 40
	for i, it := range items {
		if it.id == v.Sort {
			p.cursor = i
		}
	}
	p.onPick = func(it pickItem) tea.Cmd {
		v.Sort = it.id
		ctx.state.save()
		return toastNote("Sorted by " + strings.ToLower(sortLabels[it.id]))
	}
	return p
}

// ─── command palette ─────────────────────────────────────────────

// newPalette lists every action available on the current screen. Typing an
// issue key (or just its number) offers to open it; any other text offers a
// Jira search.
func newPalette(ctx *appCtx, top screen) modal {
	sctx := screenContext(top)
	var items []pickItem
	add := func(b binding) {
		items = append(items, pickItem{id: "key:" + b.send, label: b.desc, right: sAccent().Render(b.keys), extra: b.keys})
	}
	hasIssue := false
	switch s := top.(type) {
	case *browseScreen:
		hasIssue = s.selectedIssue() != nil
	case *boardScreen:
		hasIssue = s.selectedIssue() != nil
	case *detailScreen:
		hasIssue = s.issue() != nil
	}
	for _, b := range bindings {
		if !b.palette || b.send == "" {
			continue
		}
		if b.ctx == sctx || (b.ctx == ctxIssue && hasIssue) {
			add(b)
		}
	}
	if sctx != ctxDetail {
		for i, sd := range ctx.visibleScopes() {
			items = append(items, pickItem{id: "scope:" + sd.id, label: "Go to tab: " + sd.title,
				right: sAccent().Render(strconv.Itoa(i + 1)), extra: "tab scope"})
		}
	}
	items = append(items, pickItem{id: "help", label: "Keyboard shortcuts", right: sAccent().Render("?")})
	items = append(items, pickItem{id: "repos", label: "Set repositories folder…", extra: "worktrees agents claude git",
		right: sMuted().Render(tildify(ctx.reposRoot()))})

	p := newPicker("Commands", items)
	p.boxW = 72
	p.input.Placeholder = "action, issue key (e.g. 306), or text to search Jira"
	p.dynamic = func(q string) []pickItem {
		q = strings.TrimSpace(q)
		if q == "" {
			return nil
		}
		var out []pickItem
		if keyish.MatchString(q) {
			k := normalizeKey(q, ctx.project())
			label := "Open " + k
			if is := ctx.store.issue(k); is != nil {
				label += "  " + is.Fields.Summary
			}
			out = append(out, pickItem{id: "dyn:open:" + k, label: label, styled: sBold().Render(trunc(label, 60)), right: sAccent().Render(gEnter)})
		}
		out = append(out, pickItem{id: "dyn:search:" + q, label: "Search Jira for “" + q + "”", right: sMuted().Render("S")})
		return out
	}
	p.refilter()
	p.onPick = func(it pickItem) tea.Cmd {
		switch {
		case strings.HasPrefix(it.id, "key:"):
			return replayKey(strings.TrimPrefix(it.id, "key:"))
		case strings.HasPrefix(it.id, "scope:"):
			id := strings.TrimPrefix(it.id, "scope:")
			return func() tea.Msg { return switchScopeMsg{id} }
		case strings.HasPrefix(it.id, "dyn:open:"):
			return openIssue(strings.TrimPrefix(it.id, "dyn:open:"))
		case strings.HasPrefix(it.id, "dyn:search:"):
			return runSearch(ctx, strings.TrimPrefix(it.id, "dyn:search:"))
		case it.id == "help":
			return openModal(newHelpModal(ctx, top))
		case it.id == "repos":
			return openModal(newReposRootModal(ctx))
		}
		return nil
	}
	return p
}

// replayKey sends a key press as if typed, after the palette has closed.
func replayKey(k string) tea.Cmd {
	return func() tea.Msg {
		switch k {
		case "enter":
			return tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			return tea.KeyMsg{Type: tea.KeyEscape}
		case "tab":
			return tea.KeyMsg{Type: tea.KeyTab}
		case " ":
			return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
		}
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	}
}

// runSearch runs a text/JQL query in the Search tab.
func runSearch(ctx *appCtx, input string) tea.Cmd {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil
	}
	if keyish.MatchString(input) {
		return openIssue(normalizeKey(input, ctx.project()))
	}
	ctx.state.JQL = toJQL(input, ctx.project())
	ctx.state.pushJQL(input)
	ctx.state.save()
	ctx.store.scopes[scopeJQL] = &scopeData{}
	return func() tea.Msg { return switchScopeMsg{scopeJQL} }
}

// keyLabel renders a key cap for help screens.
func keyLabel(k string) string {
	return lipgloss.NewStyle().Foreground(th.accent).Bold(true).Render(k)
}
