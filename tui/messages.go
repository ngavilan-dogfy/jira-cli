package tui

import (
	"github.com/ngavilan-dogfy/jira-cli/jira"

	tea "github.com/charmbracelet/bubbletea"
)

// tickFn is tea.Tick, swappable so tests don't wait on timers.
var tickFn = tea.Tick

// ─── navigation ──────────────────────────────────────────────────

type pushScreenMsg struct{ s screen }
type popScreenMsg struct{}
type replaceBaseMsg struct{ s screen } // swap list ↔ board
type openModalMsg struct{ m modal }
type closeModalMsg struct{}
type openIssueMsg struct{ key string }
type switchScopeMsg struct{ id string }

func push(s screen) tea.Cmd        { return func() tea.Msg { return pushScreenMsg{s} } }
func pop() tea.Msg                 { return popScreenMsg{} }
func openModal(m modal) tea.Cmd    { return func() tea.Msg { return openModalMsg{m} } }
func closeModal() tea.Msg          { return closeModalMsg{} }
func openIssue(key string) tea.Cmd { return func() tea.Msg { return openIssueMsg{key} } }

// ─── feedback ────────────────────────────────────────────────────

type toastKind int

const (
	toastOK toastKind = iota
	toastErr
	toastInfo
)

type toastMsg struct {
	text string
	kind toastKind
}
type clearToastMsg struct{ id int }

func toast(text string) tea.Cmd {
	return func() tea.Msg { return toastMsg{text: text, kind: toastOK} }
}
func toastError(err error) tea.Cmd {
	return func() tea.Msg { return toastMsg{text: err.Error(), kind: toastErr} }
}
func toastNote(text string) tea.Cmd {
	return func() tea.Msg { return toastMsg{text: text, kind: toastInfo} }
}

type errActionMsg struct{ err error }
type branchCreatedMsg struct{ branch string }

// ─── data ────────────────────────────────────────────────────────

type scopeLoadedMsg struct {
	id        string
	jql       string
	issues    []jira.Issue
	truncated bool
	cached    bool
	err       error
}
type issueLoadedMsg struct {
	key   string
	issue *jira.Issue
	err   error
}
type commentsLoadedMsg struct {
	key      string
	comments []jira.Comment
	err      error
}
type historyLoadedMsg struct {
	key     string
	entries []jira.Changelog
	err     error
}
type childrenLoadedMsg struct {
	key    string
	issues []jira.Issue
	err    error
}
type watchLoadedMsg struct {
	key  string
	info watchInfo
	err  error
}
type transitionsLoadedMsg struct {
	key         string
	transitions []jira.Transition
	err         error
}
type myselfLoadedMsg struct {
	me  *jira.Myself
	err error
}
type usersLoadedMsg struct {
	users []jira.UserField
	err   error
}
type prioritiesLoadedMsg struct {
	priorities []jira.NameField
	err        error
}
type typesLoadedMsg struct {
	types []jira.IssueTypeMeta
	err   error
}
type statusesLoadedMsg struct {
	statuses []jira.StatusField
	err      error
}

// localEditMsg applies an optimistic change to the stored issue before the
// API confirms it; the follow-up refetch reconciles either way.
type localEditMsg struct {
	key   string
	apply func(*jira.Issue)
}

// mutationDoneMsg reports the outcome of a write.
type mutationDoneMsg struct {
	key     string
	ok      string // toast on success
	err     error
	refresh bool // refetch the active scope (membership may have changed)
	gone    bool // the issue was deleted
	created string
}

type editorDoneMsg struct {
	id   string
	text string
	err  error
}

// descEditReadyMsg carries a freshly fetched issue whose description is
// about to be edited in $EDITOR.
type descEditReadyMsg struct {
	key   string
	issue *jira.Issue
	err   error
}

type tickMsg struct{ gen int }

// workTickMsg drives the periodic work scans ("agents", "git", "prs").
type workTickMsg struct{ kind string }

// rescanWorkMsg asks for fresh git + agent scans right away.
type rescanWorkMsg struct{}
type autoRefreshMsg struct{}
