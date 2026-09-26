package tui

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ngavilan-dogfy/jira-cli/config"
	"github.com/ngavilan-dogfy/jira-cli/jira"

	"github.com/charmbracelet/bubbles/cursor"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// fakeJira is an in-memory Jira Cloud good enough for the endpoints the TUI
// calls. Every write is recorded so tests can assert on what was sent.
type fakeJira struct {
	mu       sync.Mutex
	issues   map[string]*jira.Issue
	comments map[string][]jira.Comment
	watching map[string]bool
	calls    []string
	next     int
}

var (
	meUser  = jira.UserField{AccountID: "acc-me", DisplayName: "Sam Tester"}
	leoUser = jira.UserField{AccountID: "acc-leo", DisplayName: "Leo Martín"}
	anaUser = jira.UserField{AccountID: "acc-ana", DisplayName: "Ana Díaz"}

	stBacklog  = statusSample("new", "Backlog")
	stRefine   = statusSample("new", "In Refinement")
	stProgress = statusSample("indeterminate", "In Progress")
	stDone     = statusSample("done", "Done")

	tEpic = jira.IssueTypeField{ID: "1", Name: "Epic", HierarchyLevel: 1}
	tTask = jira.IssueTypeField{ID: "2", Name: "Task"}
	tBug  = jira.IssueTypeField{ID: "3", Name: "Bug"}
	tSub  = jira.IssueTypeField{ID: "4", Name: "Sub-task (tech)", Subtask: true, HierarchyLevel: -1}
	tStry = jira.IssueTypeField{ID: "5", Name: "Story"}
)

var transitionIDs = map[string]jira.StatusField{"11": stBacklog, "21": stRefine, "31": stProgress, "41": stDone}

func jtime(t time.Time) string { return t.Format("2006-01-02T15:04:05.000-0700") }

func adfPara(text string) json.RawMessage {
	b, _ := json.Marshal(map[string]any{"type": "doc", "version": 1, "content": []any{
		map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": text}}},
	}})
	return b
}

func newFakeJira(t *testing.T) (*fakeJira, *httptest.Server) {
	fj := &fakeJira{issues: map[string]*jira.Issue{}, comments: map[string][]jira.Comment{}, watching: map[string]bool{}, next: 9}
	base := time.Now().Add(-time.Hour)
	add := func(n int, typ jira.IssueTypeField, st jira.StatusField, sum string, as *jira.UserField, parent string, prio string, ago time.Duration) {
		key := fmt.Sprintf("TST-%d", n)
		is := &jira.Issue{ID: fmt.Sprint(1000 + n), Key: key}
		f := &is.Fields
		f.Summary, f.Status, f.IssueType, f.Assignee = sum, st, typ, as
		f.Reporter = &meUser
		f.Priority = &jira.NameField{Name: prio}
		f.Created = jtime(base.Add(-30 * 24 * time.Hour))
		f.Updated = jtime(base.Add(-ago))
		f.Description = adfPara("Description of " + sum)
		if parent != "" {
			p := fj.issues[parent]
			f.Parent = &jira.ParentField{Key: parent}
			f.Parent.Fields.Summary = p.Fields.Summary
			f.Parent.Fields.IssueType = p.Fields.IssueType
			f.Parent.Fields.Status = p.Fields.Status
			if typ.Subtask {
				rel := jira.RelatedIssue{Key: key}
				rel.Fields.Summary, rel.Fields.Status, rel.Fields.IssueType = sum, st, typ
				p.Fields.Subtasks = append(p.Fields.Subtasks, rel)
			}
		}
		fj.issues[key] = is
	}
	add(1, tEpic, stProgress, "Platform migration", &meUser, "", "Medium", 2*time.Hour)
	add(2, tTask, stProgress, "Migrate api to Cloud Run", &meUser, "TST-1", "High", 3*time.Hour)
	add(3, tSub, stProgress, "Dockerfile for api", &meUser, "TST-2", "Medium", 4*time.Hour)
	add(4, tSub, stBacklog, "Terraform module", &meUser, "TST-2", "Medium", 5*time.Hour)
	add(5, tBug, stBacklog, "Login redirect loop on Safari", &meUser, "", "Highest", 6*time.Hour)
	add(6, tTask, stRefine, "Rotate credentials", &meUser, "", "Medium", 7*time.Hour)
	add(7, tTask, stDone, "Update docs", &meUser, "", "Low", 8*time.Hour)
	add(8, tTask, stProgress, "Someone else's task", &leoUser, "", "Medium", 9*time.Hour)
	add(9, tStry, stBacklog, "Migración de la web pública", nil, "", "Medium", 10*time.Hour)
	fj.comments["TST-2"] = []jira.Comment{
		{ID: "c1", Author: leoUser, Body: adfPara("First look: needs a VPC connector."), Created: jtime(base.Add(-48 * time.Hour))},
		{ID: "c2", Author: meUser, Body: adfPara("Done in infra PR #12."), Created: jtime(base.Add(-2 * time.Hour))},
	}
	srv := httptest.NewServer(http.HandlerFunc(fj.serve))
	t.Cleanup(srv.Close)
	return fj, srv
}

func (fj *fakeJira) record(s string) { fj.calls = append(fj.calls, s) }

// writes returns the recorded write calls.
func (fj *fakeJira) writes() []string {
	fj.mu.Lock()
	defer fj.mu.Unlock()
	return append([]string(nil), fj.calls...)
}

var (
	reKeyInJQL  = regexp.MustCompile(`key in \(([^)]*)\)`)
	reIssuePath = regexp.MustCompile(`^/rest/api/3/issue/([A-Z]+-\d+)(/[a-z]+)?$`)
	reParentJQL = regexp.MustCompile(`parent = ([A-Z]+-\d+)`)
	reTextJQL   = regexp.MustCompile(`text ~ "([^"]*)"`)
)

func (fj *fakeJira) serve(w http.ResponseWriter, r *http.Request) {
	fj.mu.Lock()
	defer fj.mu.Unlock()
	body, _ := io.ReadAll(r.Body)
	reply := func(v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	p := r.URL.Path
	switch {
	case p == "/rest/api/3/myself":
		reply(jira.Myself{AccountID: meUser.AccountID, DisplayName: meUser.DisplayName})
		return
	case p == "/rest/api/3/search/jql":
		var q struct{ JQL string }
		_ = json.Unmarshal(body, &q)
		reply(map[string]any{"issues": fj.search(q.JQL), "isLast": true})
		return
	case strings.HasPrefix(p, "/rest/api/3/project/") && strings.HasSuffix(p, "/statuses"):
		reply([]any{map[string]any{"name": "Task", "statuses": []jira.StatusField{stRefine, stDone, stBacklog, stProgress}}})
		return
	case p == "/rest/api/3/user/assignable/search":
		reply([]jira.UserField{meUser, leoUser, anaUser})
		return
	case p == "/rest/api/3/priority":
		reply([]jira.NameField{{Name: "Highest"}, {Name: "High"}, {Name: "Medium"}, {Name: "Low"}, {Name: "Lowest"}})
		return
	case strings.HasPrefix(p, "/rest/api/3/issue/createmeta/"):
		reply(map[string]any{"issueTypes": []jira.IssueTypeMeta{
			{ID: "1", Name: "Epic"}, {ID: "2", Name: "Task"}, {ID: "3", Name: "Bug"},
			{ID: "4", Name: "Sub-task (tech)", Subtask: true}, {ID: "6", Name: "Test Plan"},
		}})
		return
	case p == "/rest/api/3/issue" && r.Method == "POST":
		fj.create(w, body)
		return
	}

	m := reIssuePath.FindStringSubmatch(p)
	if m == nil {
		http.Error(w, `{"errorMessages":["not found: `+p+`"]}`, 404)
		return
	}
	key, sub := m[1], m[2]
	is := fj.issues[key]
	if is == nil {
		w.WriteHeader(404)
		reply(map[string]any{"errorMessages": []string{"Issue does not exist or you do not have permission to see it."}})
		return
	}
	switch {
	case sub == "" && r.Method == "GET":
		reply(is)
	case sub == "" && r.Method == "PUT":
		var in struct{ Fields map[string]json.RawMessage }
		_ = json.Unmarshal(body, &in)
		var names []string
		for k, v := range in.Fields {
			names = append(names, k)
			switch k {
			case "summary":
				_ = json.Unmarshal(v, &is.Fields.Summary)
			case "priority":
				var pr jira.NameField
				_ = json.Unmarshal(v, &pr)
				is.Fields.Priority = &pr
			case "labels":
				_ = json.Unmarshal(v, &is.Fields.Labels)
			case "duedate":
				is.Fields.DueDate = ""
				_ = json.Unmarshal(v, &is.Fields.DueDate)
			case "description":
				is.Fields.Description = append(json.RawMessage(nil), v...)
			}
		}
		sort.Strings(names)
		is.Fields.Updated = jtime(time.Now())
		fj.record("edit " + key + " " + strings.Join(names, ","))
		w.WriteHeader(204)
	case sub == "" && r.Method == "DELETE":
		delete(fj.issues, key)
		fj.record("delete " + key)
		w.WriteHeader(204)
	case sub == "/assignee":
		var in struct{ AccountID *string }
		_ = json.Unmarshal(body, &in)
		is.Fields.Assignee = nil
		who := "none"
		for _, u := range []jira.UserField{meUser, leoUser, anaUser} {
			if in.AccountID != nil && u.AccountID == *in.AccountID {
				u := u
				is.Fields.Assignee = &u
				who = u.AccountID
			}
		}
		fj.record("assign " + key + " " + who)
		w.WriteHeader(204)
	case sub == "/transitions" && r.Method == "GET":
		var ts []jira.Transition
		for _, id := range []string{"11", "21", "31", "41"} {
			ts = append(ts, jira.Transition{ID: id, Name: "To " + transitionIDs[id].Name, To: transitionIDs[id]})
		}
		reply(map[string]any{"transitions": ts})
	case sub == "/transitions":
		var in struct{ Transition struct{ ID string } }
		_ = json.Unmarshal(body, &in)
		is.Fields.Status = transitionIDs[in.Transition.ID]
		is.Fields.Updated = jtime(time.Now())
		fj.record("transition " + key + " " + is.Fields.Status.Name)
	case sub == "/comment" && r.Method == "GET":
		reply(map[string]any{"comments": fj.comments[key], "total": len(fj.comments[key])})
	case sub == "/comment":
		var in struct{ Body json.RawMessage }
		_ = json.Unmarshal(body, &in)
		fj.comments[key] = append(fj.comments[key], jira.Comment{ID: fmt.Sprint("c", len(fj.comments[key])+10), Author: meUser, Body: in.Body, Created: jtime(time.Now())})
		fj.record("comment " + key + " " + jira.ADFToText(in.Body))
		w.WriteHeader(201)
		reply(map[string]string{"id": "x"})
	case sub == "/watchers" && r.Method == "GET":
		n := 1
		if fj.watching[key] {
			n = 2
		}
		reply(map[string]any{"watchCount": n, "isWatching": fj.watching[key]})
	case sub == "/watchers" && r.Method == "POST":
		fj.watching[key] = true
		fj.record("watch " + key)
		w.WriteHeader(204)
	case sub == "/watchers" && r.Method == "DELETE":
		fj.watching[key] = false
		fj.record("unwatch " + key)
		w.WriteHeader(204)
	case sub == "/changelog":
		reply(map[string]any{"total": 1, "values": []jira.Changelog{{ID: "h1", Author: meUser, Created: is.Fields.Updated,
			Items: []jira.ChangelogItem{{Field: "status", FromString: "Backlog", ToString: is.Fields.Status.Name}}}}})
	default:
		http.Error(w, `{"errorMessages":["unsupported"]}`, 400)
	}
}

func (fj *fakeJira) search(jql string) []jira.Issue {
	var out []jira.Issue
	keys := make([]string, 0, len(fj.issues))
	for k := range fj.issues {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keyLess(keys[j], keys[i]) })
	for _, k := range keys {
		is := fj.issues[k]
		f := is.Fields
		switch {
		case reParentJQL.MatchString(jql):
			if f.Parent == nil || f.Parent.Key != reParentJQL.FindStringSubmatch(jql)[1] {
				continue
			}
		case strings.Contains(jql, "issuetype = Epic") && !isEpic(f.IssueType):
			continue
		case strings.Contains(jql, "assignee = currentUser()") && (f.Assignee == nil || f.Assignee.AccountID != meUser.AccountID):
			continue
		case strings.Contains(jql, "watcher = currentUser()") && !fj.watching[k]:
			continue
		}
		if m := reTextJQL.FindStringSubmatch(jql); m != nil && !strings.Contains(fold(f.Summary), fold(m[1])) {
			continue
		}
		if m := reKeyInJQL.FindStringSubmatch(jql); m != nil && !strings.Contains(", "+m[1]+",", ", "+k+",") {
			continue
		}
		out = append(out, *is)
	}
	return out
}

func (fj *fakeJira) create(w http.ResponseWriter, body []byte) {
	var in struct {
		Fields struct {
			Summary   string
			IssueType struct{ Name string } `json:"issuetype"`
			Parent    *struct{ Key string }
		}
	}
	_ = json.Unmarshal(body, &in)
	fj.next++
	key := fmt.Sprintf("TST-%d", fj.next)
	is := &jira.Issue{ID: fmt.Sprint(1000 + fj.next), Key: key}
	f := &is.Fields
	f.Summary, f.Status = in.Fields.Summary, stBacklog
	f.IssueType = jira.IssueTypeField{Name: in.Fields.IssueType.Name, Subtask: strings.HasPrefix(in.Fields.IssueType.Name, "Sub-task")}
	f.Reporter = &meUser
	f.Priority = &jira.NameField{Name: "Medium"}
	f.Created, f.Updated = jtime(time.Now()), jtime(time.Now())
	parent := ""
	if in.Fields.Parent != nil {
		parent = in.Fields.Parent.Key
		f.Parent = &jira.ParentField{Key: parent}
		if p := fj.issues[parent]; p != nil {
			f.Parent.Fields.Summary = p.Fields.Summary
		}
	}
	fj.issues[key] = is
	fj.record(fmt.Sprintf("create %s %s %q parent=%s", key, f.IssueType.Name, f.Summary, parent))
	w.WriteHeader(201)
	_ = json.NewEncoder(w).Encode(map[string]string{"id": is.ID, "key": key})
}

// ─── harness ─────────────────────────────────────────────────────

// harness drives the App like Bubble Tea would, but synchronously: every
// command runs to completion and its message is fed back.
type harness struct {
	t         *testing.T
	app       *App
	fj        *fakeJira
	fr        *fakeRunner
	w, h      int
	quit      bool
	depth     int
	editor    func(text string) string // what the fake $EDITOR saves
	clipboard string
}

// fakeRunner stands in for the machine: real git (only inside the test's
// folders), scripted pgrep/ps/lsof/gh, and osascript calls recorded instead
// of opening iTerm tabs.
type fakeRunner struct {
	mu        sync.Mutex
	gitRoots  []string // real git allowed only under these
	pgrep     string   // "" = no claude processes
	ps        string
	lsof      string
	gh        func(args []string) ([]byte, error)
	calls     [][]string
	claudeDir string // where transcripts live ("" = under the test HOME)
}

func (f *fakeRunner) record(name string, args []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, append([]string{name}, args...))
}

func (f *fakeRunner) callsTo(name string) [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out [][]string
	for _, c := range f.calls {
		if c[0] == name {
			out = append(out, c)
		}
	}
	return out
}

func (f *fakeRunner) run(timeout time.Duration, dir, name string, args ...string) ([]byte, error) {
	switch name {
	case "git":
		if len(args) == 3 && args[0] == "remote" && args[1] == "get-url" {
			return []byte("https://github.com/Acme/" + filepath.Base(dir) + ".git\n"), nil
		}
		for _, root := range f.gitRoots {
			if dir != "" && strings.HasPrefix(dir, root) {
				return execRunner{}.run(timeout, dir, name, args...)
			}
		}
		return nil, fmt.Errorf("git outside the test folders: %s", dir)
	case "pgrep":
		if f.pgrep == "" {
			return nil, fmt.Errorf("pgrep: exit status 1")
		}
		return []byte(f.pgrep), nil
	case "ps":
		f.record(name, args)
		if len(args) > 1 && args[1] == "tty=" {
			return []byte("ttys007\n"), nil // the TUI's own terminal
		}
		return []byte(f.ps), nil
	case "lsof":
		return []byte(f.lsof), nil
	case "gh":
		f.record(name, args)
		if f.gh == nil {
			return nil, fmt.Errorf("gh: not configured")
		}
		return f.gh(args)
	case "osascript":
		f.record(name, args)
		if len(args) > 1 && strings.Contains(args[1], "create tab") {
			return []byte("sess-1 /dev/ttys099\n"), nil
		}
		return []byte("ok\n"), nil
	}
	return nil, fmt.Errorf("unexpected command %s", name)
}

func newHarness(t *testing.T, width, height int) *harness {
	return newHarnessWith(t, width, height, &fakeRunner{})
}

func newHarnessWith(t *testing.T, width, height int, fr *fakeRunner) *harness {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("NO_COLOR", "1")
	origSys, origLook, origClaude := sys, lookPath, claudeHome
	sys = fr
	lookPath = func(string) (string, error) { return "/usr/bin/true", nil }
	claudeDir := fr.claudeDir
	if claudeDir == "" {
		claudeDir = filepath.Join(os.Getenv("HOME"), ".claude")
	}
	claudeHome = func() string { return claudeDir }
	t.Cleanup(func() { sys, lookPath, claudeHome = origSys, origLook, origClaude })
	// No timers in flight: every command either returns promptly or is a bug.
	origTick, origCursor, origEditor, origClip := tickFn, cursorMode, openEditor, writeClipboard
	tickFn = func(time.Duration, func(time.Time) tea.Msg) tea.Cmd { return nil }
	cursorMode = cursor.CursorStatic
	t.Cleanup(func() { tickFn, cursorMode, openEditor, writeClipboard = origTick, origCursor, origEditor, origClip })

	fj, srv := newFakeJira(t)
	cfg := &config.Profile{Name: "test", Project: "TST", Domain: "example"}
	client := jira.NewBasicClient(srv.URL, "me@example.com", "token")
	h := &harness{t: t, app: New(cfg, client, Options{}), fj: fj, fr: fr, w: width, h: height}
	writeClipboard = func(text string) error { h.clipboard = text; return nil }
	// $EDITOR stand-in: tests script what the "user" types.
	openEditor = func(id, name, text string) tea.Cmd {
		return func() tea.Msg {
			reply := text
			if h.editor != nil {
				reply = h.editor(text)
			}
			return editorDoneMsg{id: id, text: reply}
		}
	}
	h.run(h.app.Init())
	h.dispatch(tea.WindowSizeMsg{Width: width, Height: height})
	return h
}

func (h *harness) run(cmd tea.Cmd) {
	if cmd == nil || h.quit {
		return
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		h.dispatch(msg)
	case <-time.After(5 * time.Second):
		h.t.Fatalf("a command blocked for 5s (an unexpected timer?)")
	}
}

func (h *harness) dispatch(msg tea.Msg) {
	if msg == nil || h.quit {
		return
	}
	h.depth++
	defer func() { h.depth-- }()
	if h.depth > 200 {
		h.t.Fatalf("message loop too deep (last %T)", msg)
	}
	switch m := msg.(type) {
	case tea.BatchMsg:
		for _, c := range m {
			h.run(c)
		}
		return
	case tea.QuitMsg:
		h.quit = true
		return
	}
	if v := reflect.ValueOf(msg); v.Kind() == reflect.Slice && strings.HasSuffix(v.Type().String(), "sequenceMsg") {
		for i := 0; i < v.Len(); i++ {
			if c, ok := v.Index(i).Interface().(tea.Cmd); ok {
				h.run(c)
			}
		}
		return
	}
	_, cmd := h.app.Update(msg)
	h.run(cmd)
}

// keys types a sequence: named keys in angle brackets (<enter>, <esc>,
// <tab>, <down>, <ctrl+s>...), anything else rune by rune.
func (h *harness) keys(seq string) {
	h.t.Helper()
	for len(seq) > 0 {
		if strings.HasPrefix(seq, "<") {
			end := strings.Index(seq, ">")
			h.dispatch(namedKey(seq[1:end]))
			seq = seq[end+1:]
			continue
		}
		r := []rune(seq)[0]
		h.dispatch(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		seq = seq[len(string(r)):]
	}
}

func namedKey(name string) tea.KeyMsg {
	switch name {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEscape}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "ctrl+s":
		return tea.KeyMsg{Type: tea.KeyCtrlS}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	}
	panic("unknown key " + name)
}

// screen renders the current frame and checks the layout invariants: exactly
// h lines, each exactly w cells wide.
func (h *harness) screen() string {
	h.t.Helper()
	view := h.app.View()
	lines := strings.Split(view, "\n")
	if len(lines) != h.h {
		h.t.Fatalf("frame has %d lines, want %d:\n%s", len(lines), h.h, ansi.Strip(view))
	}
	for i, l := range lines {
		if got := ansi.StringWidth(l); got != h.w {
			h.t.Fatalf("line %d is %d cells wide, want %d: %q\n%s", i, got, h.w, ansi.Strip(l), ansi.Strip(view))
		}
	}
	return ansi.Strip(view)
}

func (h *harness) expect(want ...string) {
	h.t.Helper()
	s := h.screen()
	for _, w := range want {
		if !strings.Contains(s, w) {
			h.t.Fatalf("screen lacks %q:\n%s", w, s)
		}
	}
}

func (h *harness) reject(unwanted ...string) {
	h.t.Helper()
	s := h.screen()
	for _, w := range unwanted {
		if strings.Contains(s, w) {
			h.t.Fatalf("screen unexpectedly shows %q:\n%s", w, s)
		}
	}
}

func (h *harness) expectWrite(want string) {
	h.t.Helper()
	for _, c := range h.fj.writes() {
		if strings.HasPrefix(c, want) {
			return
		}
	}
	h.t.Fatalf("no write starting with %q; got %q", want, h.fj.writes())
}

// selectKey moves the list cursor down until it reaches key.
func (h *harness) selectKey(key string) {
	h.t.Helper()
	for i := 0; i < 50 && h.selected() != key; i++ {
		h.keys("j")
	}
	if h.selected() != key {
		h.t.Fatalf("couldn't reach %s in the list", key)
	}
}

// selected returns the key of the issue under the list cursor.
func (h *harness) selected() string {
	if b, ok := h.app.screens[0].(*browseScreen); ok {
		return b.selKey
	}
	return ""
}
