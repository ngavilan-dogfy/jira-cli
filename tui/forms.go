package tui

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/ngavilan-dogfy/jira-cli/jira"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// cursorMode is how text cursors behave; tests make them static so no
// blink timers are in flight.
var cursorMode = cursor.CursorBlink

func newInput(placeholder string, limit int) textinput.Model {
	ti := textinput.New()
	ti.Cursor.SetMode(cursorMode)
	ti.Prompt = ""
	ti.Placeholder = placeholder
	ti.PlaceholderStyle = sMuted()
	ti.CharLimit = limit
	return ti
}

func newTextarea(placeholder string) textarea.Model {
	ta := textarea.New()
	ta.Cursor.SetMode(cursorMode)
	ta.Placeholder = placeholder
	ta.ShowLineNumbers = false
	ta.Prompt = ""
	ta.CharLimit = 20000
	ta.MaxHeight = 0
	for _, st := range []*textarea.Style{&ta.FocusedStyle, &ta.BlurredStyle} {
		st.Base = lipgloss.NewStyle()
		st.CursorLine = lipgloss.NewStyle()
		st.Placeholder = sMuted()
		st.EndOfBuffer = sFaint()
		st.Text = lipgloss.NewStyle()
	}
	return ta
}

var editorSeq int

func nextEditorID() string {
	editorSeq++
	return fmt.Sprintf("ed%d", editorSeq)
}

// ─── comment composer ────────────────────────────────────────────

type composerModal struct {
	ctx        *appCtx
	key        string
	ta         textarea.Model
	edID       string
	confirming bool
	w, h       int
}

func newCommentModal(ctx *appCtx, key string) modal {
	ta := newTextarea("Write in markdown: **bold**, `code`, - lists, [links](https://…)")
	return &composerModal{ctx: ctx, key: key, ta: ta, edID: nextEditorID()}
}

func (m *composerModal) boxW() int { return max(40, min(96, m.w-8)) }

func (m *composerModal) SetSize(w, h int) {
	m.w, m.h = w, h
	m.ta.SetWidth(m.boxW() - 4)
	m.ta.SetHeight(max(4, min(14, h-14)))
}

func (m *composerModal) Init() tea.Cmd { return m.ta.Focus() }

func (m *composerModal) Update(msg tea.Msg) (modal, tea.Cmd) {
	switch msg := msg.(type) {
	case editorDoneMsg:
		if msg.id != m.edID {
			return m, nil
		}
		if msg.err != nil {
			return m, toastError(msg.err)
		}
		m.ta.SetValue(msg.text)
		return m, m.ta.Focus()
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			if strings.TrimSpace(m.ta.Value()) == "" || m.confirming {
				return m, closeModal
			}
			m.confirming = true
			return m, nil
		case "ctrl+s":
			body := strings.TrimSpace(m.ta.Value())
			if body == "" {
				return m, nil
			}
			return m, tea.Sequence(closeModal, toastNote("Sending comment…"), cmdComment(m.ctx, m.key, body))
		case "ctrl+e":
			return m, openEditor(m.edID, m.key+"-comment", m.ta.Value())
		}
		m.confirming = false
	}
	var cmd tea.Cmd
	m.ta, cmd = m.ta.Update(msg)
	return m, cmd
}

func (m *composerModal) View() string {
	w := m.boxW()
	var lines []string
	if is := m.ctx.store.issue(m.key); is != nil {
		lines = append(lines, sMuted().Render(trunc(is.Fields.Summary, w-4)), "")
	}
	lines = append(lines, m.ta.View(), "")
	if m.confirming {
		lines = append(lines, fg(th.yellow).Render("Discard this comment? esc again to discard, any key to keep writing"))
	} else {
		lines = append(lines, hintBar([]hint{{"ctrl+s", "send"}, {"ctrl+e", "open in $EDITOR"}, {"esc", "cancel"}}, w-4))
	}
	return box("Comment on "+m.key, strings.Join(lines, "\n"), w, th.accent)
}

// ─── search prompt ───────────────────────────────────────────────

type promptModal struct {
	ctx     *appCtx
	input   textinput.Model
	history []string
	hi      int
	w, h    int
}

func newSearchModal(ctx *appCtx) modal {
	in := newInput("words, an issue key, or JQL", 400)
	return &promptModal{ctx: ctx, input: in, history: ctx.state.JQLHistory, hi: -1}
}

func (m *promptModal) boxW() int { return max(40, min(90, m.w-8)) }

func (m *promptModal) SetSize(w, h int) {
	m.w, m.h = w, h
	m.input.Width = m.boxW() - 8
}

func (m *promptModal) Init() tea.Cmd { return m.input.Focus() }

func (m *promptModal) Update(msg tea.Msg) (modal, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "esc":
			return m, closeModal
		case "enter":
			q := m.input.Value()
			if strings.TrimSpace(q) == "" {
				return m, nil
			}
			return m, tea.Sequence(closeModal, runSearch(m.ctx, q))
		case "up":
			if m.hi+1 < len(m.history) {
				m.hi++
				m.input.SetValue(m.history[m.hi])
				m.input.CursorEnd()
			}
			return m, nil
		case "down":
			if m.hi > 0 {
				m.hi--
				m.input.SetValue(m.history[m.hi])
			} else {
				m.hi = -1
				m.input.SetValue("")
			}
			m.input.CursorEnd()
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *promptModal) View() string {
	w := m.boxW()
	p := m.ctx.project()
	lines := []string{
		sAccent().Bold(true).Render("› ") + m.input.View(),
		"",
		sMuted().Render("Words search summaries, descriptions and comments in " + p + "."),
		sMuted().Render("An issue key (or just its number) opens it. JQL works as is:"),
		"  " + fg(th.cyan).Render("assignee = currentUser() AND labels = infra"),
		"  " + fg(th.cyan).Render(`project = SW AND text ~ "checkout"`),
		"",
	}
	hints := []hint{{gEnter, "search"}, {"esc", "cancel"}}
	if len(m.history) > 0 {
		hints = append([]hint{{"↑↓", "history"}}, hints...)
	}
	lines = append(lines, hintBar(hints, w-4))
	return box("Search Jira", strings.Join(lines, "\n"), w, th.accent)
}

// ─── create issue ────────────────────────────────────────────────

const (
	fType = iota
	fSummary
	fParent
	fAssignee
	fPriority
	fDesc
	nFields
)

type createModal struct {
	ctx      *appCtx
	summary  textinput.Model
	parent   textinput.Model
	desc     textarea.Model
	typeName string
	prio     string
	assignMe bool
	focus    int
	err      string
	edID     string
	w, h     int
}

func newCreateModal(ctx *appCtx, parent string) modal {
	m := &createModal{
		ctx:      ctx,
		summary:  newInput("what needs to happen (required)", 255),
		parent:   newInput("optional, e.g. 306", 30),
		desc:     newTextarea("Description in markdown (optional) — ctrl+e opens $EDITOR"),
		assignMe: true,
		focus:    fSummary,
		edID:     nextEditorID(),
	}
	m.parent.SetValue(parent)
	return m
}

func (m *createModal) boxW() int { return max(50, min(92, m.w-8)) }

func (m *createModal) SetSize(w, h int) {
	m.w, m.h = w, h
	inner := m.boxW() - 4
	m.summary.Width = inner - 14
	m.parent.Width = 20
	m.desc.SetWidth(inner)
	m.desc.SetHeight(max(3, min(10, h-22)))
}

func (m *createModal) Init() tea.Cmd {
	var cmds []tea.Cmd
	s := m.ctx.store
	if !s.types.loaded && !s.types.loading {
		s.types.loading = true
		cmds = append(cmds, cmdFetchTypes(m.ctx))
	}
	if !s.priorities.loaded && !s.priorities.loading {
		s.priorities.loading = true
		cmds = append(cmds, cmdFetchPriorities(m.ctx))
	}
	m.defaults()
	cmds = append(cmds, m.summary.Focus())
	return tea.Batch(cmds...)
}

// creatableTypes hides test-management types (Xray and friends).
func creatableTypes(ctx *appCtx) []jira.IssueTypeMeta {
	var out []jira.IssueTypeMeta
	for _, t := range ctx.store.types.val {
		n := strings.ToLower(t.Name)
		if strings.Contains(n, "test") || strings.Contains(n, "precondition") {
			continue
		}
		out = append(out, t)
	}
	return out
}

// defaults picks a sensible type (child of an epic → Task, child of a task
// → sub-task) and priority once the lists are known.
func (m *createModal) defaults() {
	types := creatableTypes(m.ctx)
	if m.typeName == "" && len(types) > 0 {
		want := "task"
		parentKey := normalizeKey(m.parent.Value(), m.ctx.project())
		if parentKey != "" {
			p := m.ctx.store.issue(parentKey)
			if p == nil || !isEpic(p.Fields.IssueType) {
				want = "sub"
			}
		}
		m.typeName = types[0].Name
		for _, t := range types {
			n := strings.ToLower(t.Name)
			if (want == "task" && n == "task") || (want == "sub" && t.Subtask) {
				m.typeName = t.Name
				break
			}
		}
	}
	if m.prio == "" && len(m.ctx.store.priorities.val) > 0 {
		m.prio = m.ctx.store.priorities.val[0].Name
		for _, p := range m.ctx.store.priorities.val {
			if strings.EqualFold(p.Name, "medium") {
				m.prio = p.Name
			}
		}
	}
}

func (m *createModal) cycle(d int) {
	switch m.focus {
	case fType:
		types := creatableTypes(m.ctx)
		for i, t := range types {
			if t.Name == m.typeName {
				m.typeName = types[(i+d+len(types))%len(types)].Name
				return
			}
		}
	case fPriority:
		ps := m.ctx.store.priorities.val
		for i, p := range ps {
			if p.Name == m.prio {
				m.prio = ps[(i+d+len(ps))%len(ps)].Name
				return
			}
		}
	case fAssignee:
		m.assignMe = !m.assignMe
	}
}

func (m *createModal) setFocus(f int) tea.Cmd {
	m.focus = (f + nFields) % nFields
	m.summary.Blur()
	m.parent.Blur()
	m.desc.Blur()
	switch m.focus {
	case fSummary:
		return m.summary.Focus()
	case fParent:
		return m.parent.Focus()
	case fDesc:
		return m.desc.Focus()
	}
	return nil
}

func (m *createModal) Update(msg tea.Msg) (modal, tea.Cmd) {
	switch msg := msg.(type) {
	case typesLoadedMsg, prioritiesLoadedMsg:
		m.defaults()
		return m, nil
	case editorDoneMsg:
		if msg.id == m.edID {
			if msg.err != nil {
				return m, toastError(msg.err)
			}
			m.desc.SetValue(msg.text)
			return m, m.setFocus(fDesc)
		}
		return m, nil
	case tea.KeyMsg:
		k := msg.String()
		switch k {
		case "esc":
			return m, closeModal
		case "ctrl+s":
			return m, m.submit()
		case "ctrl+e":
			return m, openEditor(m.edID, "new-issue", m.desc.Value())
		case "tab":
			return m, m.setFocus(m.focus + 1)
		case "shift+tab":
			return m, m.setFocus(m.focus - 1)
		case "enter":
			if m.focus != fDesc {
				return m, m.setFocus(m.focus + 1)
			}
		case "up":
			if m.focus != fDesc {
				return m, m.setFocus(m.focus - 1)
			}
		case "down":
			if m.focus != fDesc {
				return m, m.setFocus(m.focus + 1)
			}
		case "left", "right", " ":
			if m.focus == fType || m.focus == fPriority || m.focus == fAssignee {
				d := 1
				if k == "left" {
					d = -1
				}
				m.cycle(d)
				return m, nil
			}
		}
	}
	var cmd tea.Cmd
	switch m.focus {
	case fSummary:
		m.summary, cmd = m.summary.Update(msg)
		m.err = ""
	case fParent:
		before := m.parent.Value()
		m.parent, cmd = m.parent.Update(msg)
		if m.parent.Value() != before {
			m.typeName = "" // re-derive the default type for the new parent
			m.defaults()
		}
	case fDesc:
		m.desc, cmd = m.desc.Update(msg)
	default:
		// Cursor blink for whichever input is focused.
		m.summary, cmd = m.summary.Update(msg)
	}
	return m, cmd
}

func (m *createModal) submit() tea.Cmd {
	summary := strings.TrimSpace(m.summary.Value())
	if summary == "" {
		m.err = "A summary is required"
		return m.setFocus(fSummary)
	}
	parent := normalizeKey(m.parent.Value(), m.ctx.project())
	var isSub bool
	for _, t := range creatableTypes(m.ctx) {
		if t.Name == m.typeName {
			isSub = t.Subtask
		}
	}
	if isSub && parent == "" {
		m.err = m.typeName + " needs a parent issue"
		return m.setFocus(fParent)
	}
	typeName := m.typeName
	if typeName == "" {
		typeName = "Task"
	}
	n := newIssue{project: m.ctx.project(), issueType: typeName, summary: summary, parent: parent,
		description: strings.TrimSpace(m.desc.Value())}
	if !strings.EqualFold(m.prio, "medium") {
		n.priority = m.prio
	}
	if me := m.ctx.myself(); m.assignMe && me != nil {
		n.assignee = &jira.UserField{AccountID: me.AccountID, DisplayName: me.DisplayName}
	}
	return tea.Sequence(closeModal, toastNote("Creating issue…"), cmdCreate(m.ctx, n))
}

func (m *createModal) View() string {
	w := m.boxW()
	inner := w - 4
	label := func(f int, s string) string {
		st := sMuted()
		if m.focus == f {
			st = sAccent().Bold(true)
		}
		return st.Render(fit(s, 12))
	}
	choice := func(f int, v string) string {
		if m.focus == f {
			return sAccent().Render("‹ ") + v + sAccent().Render(" ›")
		}
		return v
	}
	var lines []string

	typeVal := sMuted().Render("loading…")
	if m.typeName != "" {
		typeVal = typeIcon(jira.IssueTypeField{Name: m.typeName, Subtask: isSubtask(jira.IssueTypeField{Name: m.typeName})}) + " " + m.typeName
	}
	lines = append(lines, label(fType, "Type")+choice(fType, typeVal))
	lines = append(lines, label(fSummary, "Summary")+m.summary.View())

	pv := m.parent.View()
	if k := normalizeKey(m.parent.Value(), m.ctx.project()); k != "" {
		if p := m.ctx.store.issue(k); p != nil {
			pv += "  " + sMuted().Render(trunc(p.Fields.Summary, max(10, inner-12-24)))
		}
	}
	lines = append(lines, label(fParent, "Parent")+pv)

	who := "me"
	if !m.assignMe {
		who = sMuted().Render("unassigned")
	}
	lines = append(lines, label(fAssignee, "Assignee")+choice(fAssignee, who))

	prio := sMuted().Render("loading…")
	if m.prio != "" {
		prio = priorityLabel(&jira.NameField{Name: m.prio})
	}
	lines = append(lines, label(fPriority, "Priority")+choice(fPriority, prio))
	lines = append(lines, "", label(fDesc, "Description"), m.desc.View(), "")

	if m.err != "" {
		lines = append(lines, fg(th.red).Bold(true).Render("× "+m.err))
	} else {
		lines = append(lines, hintBar([]hint{{"ctrl+s", "create"}, {"tab", "next"}, {"← →", "change"}, {"ctrl+e", "$EDITOR"}, {"esc", "cancel"}}, inner))
	}
	return box("New issue in "+m.ctx.project(), strings.Join(lines, "\n"), w, th.accent)
}

// ─── edit issue ──────────────────────────────────────────────────

const (
	eSummary = iota
	ePriority
	eLabels
	eDue
	nEdit
)

type editModal struct {
	ctx     *appCtx
	key     string
	summary textinput.Model
	labels  textinput.Model
	due     textinput.Model
	prio    string
	focus   int
	err     string
	w, h    int
}

func newEditModal(ctx *appCtx, key string) modal {
	m := &editModal{ctx: ctx, key: key,
		summary: newInput("summary", 255),
		labels:  newInput("space or comma separated", 255),
		due:     newInput("YYYY-MM-DD (empty clears)", 10),
	}
	if is := ctx.store.issue(key); is != nil {
		m.summary.SetValue(is.Fields.Summary)
		m.labels.SetValue(strings.Join(is.Fields.Labels, " "))
		m.due.SetValue(is.Fields.DueDate)
		if is.Fields.Priority != nil {
			m.prio = is.Fields.Priority.Name
		}
	}
	return m
}

func (m *editModal) boxW() int { return max(50, min(90, m.w-8)) }

func (m *editModal) SetSize(w, h int) {
	m.w, m.h = w, h
	m.summary.Width = m.boxW() - 18
	m.labels.Width = m.boxW() - 18
	m.due.Width = 12
}

func (m *editModal) Init() tea.Cmd {
	var cmd tea.Cmd
	if s := m.ctx.store; !s.priorities.loaded && !s.priorities.loading {
		s.priorities.loading = true
		cmd = cmdFetchPriorities(m.ctx)
	}
	m.summary.CursorEnd()
	return tea.Batch(cmd, m.summary.Focus())
}

func (m *editModal) setFocus(f int) tea.Cmd {
	m.focus = (f + nEdit) % nEdit
	m.summary.Blur()
	m.labels.Blur()
	m.due.Blur()
	switch m.focus {
	case eSummary:
		return m.summary.Focus()
	case eLabels:
		return m.labels.Focus()
	case eDue:
		return m.due.Focus()
	}
	return nil
}

var labelSplit = regexp.MustCompile(`[,\s]+`)

func (m *editModal) Update(msg tea.Msg) (modal, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch k := km.String(); k {
		case "esc":
			return m, closeModal
		case "ctrl+s", "enter":
			if k == "enter" && m.focus != nEdit-1 {
				return m, m.setFocus(m.focus + 1)
			}
			return m, m.submit()
		case "tab", "down":
			return m, m.setFocus(m.focus + 1)
		case "shift+tab", "up":
			return m, m.setFocus(m.focus - 1)
		case "left", "right", " ":
			if m.focus == ePriority {
				ps := m.ctx.store.priorities.val
				d := 1
				if k == "left" {
					d = -1
				}
				for i, p := range ps {
					if p.Name == m.prio {
						m.prio = ps[(i+d+len(ps))%len(ps)].Name
						break
					}
				}
				if m.prio == "" && len(ps) > 0 {
					m.prio = ps[0].Name
				}
				return m, nil
			}
		}
		m.err = ""
	}
	var cmd tea.Cmd
	switch m.focus {
	case eSummary:
		m.summary, cmd = m.summary.Update(msg)
	case eLabels:
		m.labels, cmd = m.labels.Update(msg)
	case eDue:
		m.due, cmd = m.due.Update(msg)
	}
	return m, cmd
}

func (m *editModal) submit() tea.Cmd {
	is := m.ctx.store.issue(m.key)
	if is == nil {
		return closeModal
	}
	fields := map[string]any{}
	summary := strings.TrimSpace(m.summary.Value())
	if summary == "" {
		m.err = "The summary can't be empty"
		return nil
	}
	if summary != is.Fields.Summary {
		fields["summary"] = summary
	}
	oldPrio := ""
	if is.Fields.Priority != nil {
		oldPrio = is.Fields.Priority.Name
	}
	if m.prio != "" && m.prio != oldPrio {
		fields["priority"] = map[string]string{"name": m.prio}
	}
	var labels []string
	for _, l := range labelSplit.Split(strings.TrimSpace(m.labels.Value()), -1) {
		if l != "" {
			labels = append(labels, l)
		}
	}
	if strings.Join(labels, " ") != strings.Join(is.Fields.Labels, " ") {
		if labels == nil {
			labels = []string{}
		}
		fields["labels"] = labels
	}
	due := strings.TrimSpace(m.due.Value())
	if due != is.Fields.DueDate {
		if due == "" {
			fields["duedate"] = nil
		} else if _, err := time.Parse("2006-01-02", due); err != nil {
			m.err = "Due date must look like 2026-10-31"
			return m.setFocus(eDue)
		} else {
			fields["duedate"] = due
		}
	}
	if len(fields) == 0 {
		return tea.Sequence(closeModal, toastNote("Nothing changed"))
	}
	prio := m.prio
	local := func(x *jira.Issue) {
		x.Fields.Summary = summary
		x.Fields.Labels = labels
		x.Fields.DueDate = due
		if prio != "" {
			x.Fields.Priority = &jira.NameField{Name: prio}
		}
	}
	return tea.Sequence(closeModal, cmdEdit(m.ctx, m.key, fields, local))
}

func (m *editModal) View() string {
	w := m.boxW()
	label := func(f int, s string) string {
		st := sMuted()
		if m.focus == f {
			st = sAccent().Bold(true)
		}
		return st.Render(fit(s, 12))
	}
	prio := sMuted().Render("—")
	if m.prio != "" {
		prio = priorityLabel(&jira.NameField{Name: m.prio})
	}
	if m.focus == ePriority {
		prio = sAccent().Render("‹ ") + prio + sAccent().Render(" ›")
	}
	lines := []string{
		label(eSummary, "Summary") + m.summary.View(),
		label(ePriority, "Priority") + prio,
		label(eLabels, "Labels") + m.labels.View(),
		label(eDue, "Due date") + m.due.View(),
		"",
	}
	if m.err != "" {
		lines = append(lines, fg(th.red).Bold(true).Render("× "+m.err))
	} else {
		lines = append(lines, hintBar([]hint{{"ctrl+s", "save"}, {"tab", "next"}, {"← →", "priority"}, {"esc", "cancel"}}, w-4))
	}
	return box("Edit "+m.key, strings.Join(lines, "\n"), w, th.accent)
}

// ─── delete confirmation ─────────────────────────────────────────

type deleteModal struct {
	ctx   *appCtx
	key   string
	input textinput.Model
	w, h  int
}

func newDeleteModal(ctx *appCtx, key string) modal {
	return &deleteModal{ctx: ctx, key: key, input: newInput(key, 20)}
}

func (m *deleteModal) SetSize(w, h int) { m.w, m.h = w, h; m.input.Width = 20 }
func (m *deleteModal) Init() tea.Cmd    { return m.input.Focus() }

func (m *deleteModal) matches() bool {
	v := strings.ToUpper(strings.TrimSpace(m.input.Value()))
	_, num := splitKey(m.key)
	return v == m.key || v == fmt.Sprint(num)
}

func (m *deleteModal) Update(msg tea.Msg) (modal, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "esc":
			return m, closeModal
		case "enter":
			if m.matches() {
				return m, tea.Sequence(closeModal, cmdDelete(m.ctx, m.key))
			}
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *deleteModal) View() string {
	w := max(44, min(64, m.w-8))
	var lines []string
	if is := m.ctx.store.issue(m.key); is != nil {
		lines = append(lines, sBold().Render(m.key)+"  "+trunc(is.Fields.Summary, w-6-sw(m.key)))
		if n := len(childKeys(m.ctx, is)); n > 0 {
			lines = append(lines, fg(th.yellow).Render(fmt.Sprintf("It has %d sub-task(s); Jira may refuse or delete them too.", n)))
		}
	}
	lines = append(lines, "", fg(th.red).Render("This can't be undone."), "",
		"Type "+sBold().Render(m.key)+" to confirm:", "  "+m.input.View(), "")
	if m.matches() {
		lines = append(lines, fg(th.red).Bold(true).Render(gEnter+" delete"))
	} else {
		lines = append(lines, hintBar([]hint{{"esc", "cancel"}}, w-4))
	}
	return box("Delete issue", strings.Join(lines, "\n"), w, th.red)
}

// ─── repositories folder ─────────────────────────────────────────

type reposRootModal struct {
	ctx   *appCtx
	input textinput.Model
	err   string
	w, h  int
}

func newReposRootModal(ctx *appCtx) modal {
	in := newInput("~/code", 300)
	in.SetValue(tildify(ctx.reposRoot()))
	in.CursorEnd()
	return &reposRootModal{ctx: ctx, input: in}
}

func (m *reposRootModal) boxW() int { return max(50, min(84, m.w-8)) }

func (m *reposRootModal) SetSize(w, h int) {
	m.w, m.h = w, h
	m.input.Width = m.boxW() - 8
}

func (m *reposRootModal) Init() tea.Cmd { return m.input.Focus() }

func (m *reposRootModal) Update(msg tea.Msg) (modal, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "esc":
			return m, closeModal
		case "enter":
			dir := expandHome(strings.TrimSpace(m.input.Value()))
			if n := len(findRepos(dir)); n == 0 {
				m.err = "No git repositories directly inside " + tildify(dir)
				return m, nil
			}
			m.ctx.state.ReposRoot = tildify(dir)
			m.ctx.state.save()
			m.ctx.rootGuess = nil
			return m, tea.Sequence(closeModal, toast("Watching "+tildify(dir)), func() tea.Msg { return rescanWorkMsg{} })
		}
		m.err = ""
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *reposRootModal) View() string {
	w := m.boxW()
	lines := []string{
		sAccent().Bold(true).Render("› ") + m.input.View(),
		"",
		sMuted().Render("The folder holding your git checkouts (one level deep). Branches,"),
		sMuted().Render("worktrees and Claude sessions there are linked to tickets by key,"),
		sMuted().Render("and new agent worktrees go to <folder>/.worktrees/."),
		"",
	}
	if m.err != "" {
		lines = append(lines, fg(th.red).Bold(true).Render("× "+m.err))
	} else {
		lines = append(lines, hintBar([]hint{{gEnter, "save"}, {"esc", "cancel"}}, w-4))
	}
	return box("Repositories folder", strings.Join(lines, "\n"), w, th.accent)
}
