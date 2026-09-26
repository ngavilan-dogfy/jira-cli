package tui

import (
	"strings"

	"github.com/ngavilan-dogfy/jira-cli/jira"

	tea "github.com/charmbracelet/bubbletea"
)

// helpModal lists the shortcuts for the current screen, built from the same
// registry the palette uses.
type helpModal struct {
	ctx    *appCtx
	lines  []string
	scroll int
	w, h   int
	sctx   string
}

func newHelpModal(ctx *appCtx, top screen) modal {
	return &helpModal{ctx: ctx, sctx: screenContext(top)}
}

func (m *helpModal) Init() tea.Cmd { return nil }

func (m *helpModal) boxW() int { return max(44, min(100, m.w-6)) }

func (m *helpModal) SetSize(w, h int) {
	m.w, m.h = w, h
	m.lines = m.build(m.boxW() - 4)
}

func (m *helpModal) build(w int) []string {
	titles := map[string]string{
		ctxBrowse: "Issue list",
		ctxBoard:  "Board",
		ctxDetail: "Issue",
		ctxIssue:  "Actions on the selected issue",
		ctxGlobal: "Everywhere",
	}
	twoCols := w >= 90
	cw := w
	if twoCols {
		cw = (w - 3) / 2
	}
	section := func(c string) []string {
		var rows []binding
		for _, b := range bindings {
			if b.ctx == c {
				rows = append(rows, b)
			}
		}
		keyW := 0
		for _, b := range rows {
			keyW = max(keyW, sw(b.keys))
		}
		out := []string{sectionRule(titles[c], "", cw)}
		for _, b := range rows {
			out = append(out, trunc("  "+keyLabel(fit(b.keys, keyW))+"  "+b.desc, cw))
		}
		return append(out, "")
	}
	left := append(section(m.sctx), section(ctxIssue)...)
	right := section(ctxGlobal)
	right = append(right, sectionRule("Legend", "", cw))
	for _, k := range []struct{ st jira.StatusField }{
		{statusSample("indeterminate", "In Progress")},
		{statusSample("indeterminate", "In Review")},
		{statusSample("new", "In Refinement")},
		{statusSample("new", "Backlog")},
		{statusSample("done", "Done")},
	} {
		right = append(right, "  "+statusLabel(k.st))
	}
	right = append(right, "",
		"  "+priorityMark(namePtr("Highest"))+" "+sMuted().Render("highest   ")+priorityMark(namePtr("High"))+" "+sMuted().Render("high"),
		"  "+priorityMark(namePtr("Low"))+" "+sMuted().Render("low       ")+priorityMark(namePtr("Lowest"))+" "+sMuted().Render("lowest"),
		"  "+sMuted().Render("Medium, the default, shows no mark."),
		"",
		"  "+typeIcon(jira.IssueTypeField{Name: "Epic", HierarchyLevel: 1})+" epic   "+
			typeIcon(jira.IssueTypeField{Name: "Story"})+" story   "+
			typeIcon(jira.IssueTypeField{Name: "Task"})+" task",
		"  "+typeIcon(jira.IssueTypeField{Name: "Bug"})+" bug    "+
			typeIcon(jira.IssueTypeField{Name: "Sub-task", Subtask: true})+" sub-task",
		"",
		"  "+agentDot("waiting")+" "+sMuted().Render("Claude waiting for you   ")+agentDot("working")+" "+sMuted().Render("working"),
		"  "+agentDot("approval")+" "+sMuted().Render("Claude may need your approval"),
		"  "+fg(prColor("open")).Render("#412")+" "+sMuted().Render("open PR  ")+
			fg(prColor("merged")).Render("#412")+" "+sMuted().Render("merged  ")+sMuted().Render("wip")+" "+sMuted().Render("branch, no PR"))

	if !twoCols {
		return append(left, right...)
	}
	var out []string
	for i := 0; i < max(len(left), len(right)); i++ {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		out = append(out, fit(l, cw)+"   "+fit(r, cw))
	}
	return out
}

func (m *helpModal) rows() int { return max(4, m.h-8) }

func (m *helpModal) Update(msg tea.Msg) (modal, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "esc", "?", "q", "enter":
			return m, closeModal
		case "down", "j":
			m.scroll++
		case "up", "k":
			m.scroll--
		case "pgdown", " ", "ctrl+d":
			m.scroll += m.rows() / 2
		case "pgup", "ctrl+u":
			m.scroll -= m.rows() / 2
		}
		m.scroll = max(0, min(m.scroll, len(m.lines)-m.rows()))
	}
	return m, nil
}

func (m *helpModal) View() string {
	w := m.boxW()
	n := m.rows()
	var vis []string
	for i := m.scroll; i < len(m.lines) && len(vis) < n; i++ {
		vis = append(vis, m.lines[i])
	}
	foot := hintBar([]hint{{"esc", "close"}, {":", "palette runs any of these"}}, w-4)
	if len(m.lines) > n {
		foot = hintBar([]hint{{"j k", "scroll"}, {"esc", "close"}}, w-4)
	}
	vis = append(vis, foot)
	return box("Keyboard shortcuts", strings.Join(vis, "\n"), w, th.accent)
}
