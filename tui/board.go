package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ngavilan-dogfy/jira-cli/jira"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// boardScreen is the Kanban view: one column per workflow status, each
// scrolling on its own. H/L move the selected card to the neighbouring
// status through the matching Jira transition.
type boardScreen struct {
	ctx  *appCtx
	w, h int

	cols     []boardCol
	col      int               // focused column
	sel      map[string]string // status → selected key
	off      map[string]int    // status → first visible card
	firstCol int               // horizontal scroll
	builtRev int
}

type boardCol struct {
	status jira.StatusField
	keys   []string
}

const (
	cardH       = 4 // three lines of card + one of air
	minColWidth = 26
)

func newBoard(ctx *appCtx) *boardScreen {
	return &boardScreen{ctx: ctx, sel: map[string]string{}, off: map[string]int{}}
}

func (v *boardScreen) Init() tea.Cmd { return nil }

func (v *boardScreen) SetSize(w, h int) {
	v.w, v.h = w, h
	v.rebuild()
}

func (v *boardScreen) Capturing() bool { return false }

func (v *boardScreen) Hints() []hint {
	return []hint{{"h l", "column"}, {"j k", "card"}, {"H L", "move card"}, {gEnter, "open"},
		{"m", "move"}, {"c", "comment"}, {"tab", "tabs"}, {"b", "list"}, {":", "commands"}, {"?", "help"}}
}

func (v *boardScreen) rebuild() {
	ctx := v.ctx
	v.builtRev = ctx.rev
	byStatus := map[string][]*jira.Issue{}
	for _, is := range ctx.store.scopeIssues(ctx.state.Scope) {
		byStatus[is.Fields.Status.Name] = append(byStatus[is.Fields.Status.Name], is)
	}
	var cols []boardCol
	for _, st := range ctx.store.knownStatuses(ctx.state.Scope) {
		list := byStatus[st.Name]
		sortIssues(list, sortUpdated)
		c := boardCol{status: st}
		for _, is := range list {
			c.keys = append(c.keys, is.Key)
		}
		cols = append(cols, c)
	}
	v.cols = cols
	if v.col >= len(cols) {
		v.col = max(0, len(cols)-1)
	}
	// Keep each column's selection valid.
	for _, c := range cols {
		name := c.status.Name
		if idx(c.keys, v.sel[name]) < 0 {
			if len(c.keys) > 0 {
				v.sel[name] = c.keys[min(v.off[name], len(c.keys)-1)]
			} else {
				v.sel[name] = ""
			}
		}
	}
	v.clampScroll()
}

func idx(keys []string, k string) int {
	for i, x := range keys {
		if x == k {
			return i
		}
	}
	return -1
}

func (v *boardScreen) focused() *boardCol {
	if v.col < len(v.cols) {
		return &v.cols[v.col]
	}
	return nil
}

func (v *boardScreen) selectedIssue() *jira.Issue {
	if c := v.focused(); c != nil {
		return v.ctx.store.issue(v.sel[c.status.Name])
	}
	return nil
}

func (v *boardScreen) visibleCols() int {
	return max(1, min(len(v.cols), v.w/minColWidth))
}

func (v *boardScreen) cardsPerCol() int { return max(1, (v.h-3)/cardH) }

func (v *boardScreen) clampScroll() {
	n := v.visibleCols()
	if v.col < v.firstCol {
		v.firstCol = v.col
	}
	if v.col >= v.firstCol+n {
		v.firstCol = v.col - n + 1
	}
	v.firstCol = max(0, min(v.firstCol, len(v.cols)-n))
	per := v.cardsPerCol()
	for _, c := range v.cols {
		name := c.status.Name
		i := idx(c.keys, v.sel[name])
		if i < 0 {
			continue
		}
		off := v.off[name]
		if i < off {
			off = i
		}
		if i >= off+per {
			off = i - per + 1
		}
		v.off[name] = max(0, min(off, len(c.keys)-per))
	}
}

func (v *boardScreen) Update(msg tea.Msg) (screen, tea.Cmd) {
	var cmd tea.Cmd
	if k, ok := msg.(tea.KeyMsg); ok {
		cmd = v.key(k.String())
	}
	if _, ok := msg.(switchScopeMsg); ok {
		v.sel, v.off, v.col, v.firstCol = map[string]string{}, map[string]int{}, 0, 0
	}
	v.rebuild()
	return v, cmd
}

func (v *boardScreen) moveCard(d int) {
	c := v.focused()
	if c == nil || len(c.keys) == 0 {
		return
	}
	i := idx(c.keys, v.sel[c.status.Name])
	i = max(0, min(i+d, len(c.keys)-1))
	v.sel[c.status.Name] = c.keys[i]
}

func (v *boardScreen) key(k string) tea.Cmd {
	switch k {
	case "left", "h":
		v.col = max(0, v.col-1)
	case "right", "l":
		v.col = min(len(v.cols)-1, v.col+1)
	case "up", "k":
		v.moveCard(-1)
	case "down", "j":
		v.moveCard(1)
	case "pgup", "ctrl+u":
		v.moveCard(-v.cardsPerCol())
	case "pgdown", "ctrl+d":
		v.moveCard(v.cardsPerCol())
	case "home", "g":
		v.moveCard(-1 << 20)
	case "end", "G":
		v.moveCard(1 << 20)
	case "H", "shift+left":
		return v.shift(-1)
	case "L", "shift+right":
		return v.shift(1)
	case "enter":
		if is := v.selectedIssue(); is != nil {
			return openIssue(is.Key)
		}
	case "b":
		return func() tea.Msg { return replaceBaseMsg{newBrowse(v.ctx)} }
	case "r":
		return func() tea.Msg { return refreshMsg{} }
	default:
		if cmd, ok := scopeKey(v.ctx, k); ok {
			return cmd
		}
		if cmd, ok := issueAction(v.ctx, k, v.selectedIssue()); ok {
			return cmd
		}
	}
	return nil
}

// shift moves the selected card one column over, optimistically; the card
// and the focus travel together.
func (v *boardScreen) shift(d int) tea.Cmd {
	is := v.selectedIssue()
	t := v.col + d
	if is == nil || t < 0 || t >= len(v.cols) {
		return nil
	}
	target := v.cols[t].status
	v.col = t
	v.sel[target.Name] = is.Key
	return cmdMoveToStatus(v.ctx, is.Key, target)
}

// cmdMoveToStatus finds the transition that lands on target and runs it.
func cmdMoveToStatus(c *appCtx, key string, target jira.StatusField) tea.Cmd {
	client := c.client
	return mutate(key,
		func(is *jira.Issue) { is.Fields.Status = target },
		func() error {
			ts, err := client.GetTransitions(key)
			if err != nil {
				return err
			}
			for _, t := range ts {
				if strings.EqualFold(t.To.Name, target.Name) {
					return client.DoTransition(key, t.ID)
				}
			}
			return fmt.Errorf("no transition from here to %s — press m to see the options", target.Name)
		},
		key+" "+gArrow+" "+target.Name)
}

// ─── view ────────────────────────────────────────────────────────

func (v *boardScreen) View() string {
	if v.builtRev != v.ctx.rev {
		v.rebuild()
	}
	lines := []string{tabsBar(v.ctx, v.w)}
	if len(v.cols) == 0 {
		d := v.ctx.store.scopes[v.ctx.state.Scope]
		msg := "Nothing here."
		if d == nil || d.loading {
			msg = sAccent().Render(v.ctx.spinner()) + " Loading issues…"
		}
		return strings.Join(append(lines, "", "   "+msg), "\n")
	}

	n := v.visibleCols()
	colW := (v.w - (n - 1)) / n
	bodyH := v.h - 2
	cols := make([][]string, n)
	for i := 0; i < n; i++ {
		ci := v.firstCol + i
		w := colW
		if i == n-1 {
			w = v.w - (n-1)*(colW+1) // last column absorbs the rounding
		}
		cols[i] = v.renderCol(ci, w, bodyH+1)
	}
	sep := sFaint().Render(gTreeBar)
	for row := 0; row < bodyH+1; row++ {
		var b strings.Builder
		for i := range cols {
			if i > 0 {
				b.WriteString(sep)
			}
			b.WriteString(cols[i][row])
		}
		lines = append(lines, b.String())
	}
	// Columns hidden off-screen.
	if v.firstCol > 0 || v.firstCol+n < len(v.cols) {
		hint := fmt.Sprintf("columns %d–%d of %d ", v.firstCol+1, v.firstCol+n, len(v.cols))
		lines[len(lines)-1] = fitRight(sMuted().Render(hint), v.w)
	}
	return strings.Join(lines, "\n")
}

func (v *boardScreen) renderCol(ci, w, h int) []string {
	c := v.cols[ci]
	name := c.status.Name
	focused := ci == v.col
	color := statusColor(c.status)

	title := fg(color).Render(statusGlyph(c.status)) + " " + fg(color).Bold(true).Render(name) + " " + sMuted().Render(strconv.Itoa(len(c.keys)))
	head := " " + title
	if focused {
		head = paintBg(fit(head, w), th.sel)
	}
	out := []string{fit(head, w)}

	per := v.cardsPerCol()
	off := v.off[name]
	if off > 0 {
		out = append(out, fitRight(sMuted().Render(fmt.Sprintf("↑ %d ", off)), w))
	} else {
		out = append(out, fit("", w))
	}
	for i := off; i < len(c.keys) && i < off+per; i++ {
		sel := focused && c.keys[i] == v.sel[name]
		out = append(out, v.renderCard(c.keys[i], w, sel)...)
	}
	if len(c.keys) == 0 {
		out = append(out, fit("  "+sFaint().Render("empty"), w))
	}
	for len(out) < h {
		out = append(out, fit("", w))
	}
	if rest := len(c.keys) - (off + per); rest > 0 {
		out[h-1] = fitRight(sMuted().Render(fmt.Sprintf("↓ %d ", rest)), w)
	}
	return out[:h]
}

func (v *boardScreen) renderCard(key string, w int, selected bool) []string {
	is := v.ctx.store.issue(key)
	if is == nil {
		return []string{fit("", w), fit("", w), fit("", w), fit("", w)}
	}
	f := is.Fields
	bar := " "
	if selected {
		bar = sAccent().Render(gSel)
	}
	who := ""
	if f.Assignee != nil {
		who = sMuted().Render(firstName(f.Assignee.DisplayName))
		if v.ctx.isMe(f.Assignee) {
			who = sMuted().Render("me")
		}
	}
	keySt := sMuted()
	if selected {
		keySt = lipgloss.NewStyle().Bold(true)
	}
	top := spread(bar+typeIcon(f.IssueType)+" "+keySt.Render(is.Key)+" "+priorityMark(f.Priority), who+" ", w)

	sumW := w - 3
	sum := wrapPlain(f.Summary, sumW)
	if len(sum) > 2 {
		sum = sum[:2]
		sum[1] = trunc(sum[1]+" "+gEllipsis, sumW)
	}
	for len(sum) < 2 {
		sum = append(sum, "")
	}
	st := lipgloss.NewStyle()
	if kindOf(f.Status) == kindDone {
		st = sMuted()
	}
	if selected {
		st = st.Bold(true)
	}
	lines := []string{top,
		fit(bar+"  "+st.Render(sum[0]), w),
		fit(bar+"  "+st.Render(sum[1]), w)}
	if selected {
		for i := range lines {
			lines[i] = paintBg(lines[i], th.sel)
		}
	}
	return append(lines, fit("", w))
}
