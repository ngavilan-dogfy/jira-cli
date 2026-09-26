package tui

import (
	"sort"
	"strings"
	"time"

	"github.com/ngavilan-dogfy/jira-cli/config"
	"github.com/ngavilan-dogfy/jira-cli/jira"
)

// appCtx is the state every screen and modal shares. Bubble Tea runs Update
// on a single goroutine, so the store is only ever mutated there; async
// commands return data in messages and never touch it directly.
type appCtx struct {
	cfg    *config.Profile
	client *jira.Client
	store  *store
	state  *uiState
	w, h   int
	spin   int // spinner frame, advanced by the app's tick
	rev    int // bumped on every message; screens rebuild derived rows when it moves

	rootGuess *string // auto-detected repos folder, resolved once
}

func (c *appCtx) spinner() string { return spinnerFrames[c.spin%len(spinnerFrames)] }

func (c *appCtx) project() string { return c.cfg.Project }

func (c *appCtx) myself() *jira.Myself { return c.store.myself }

// isMe reports whether a user is the logged-in account.
func (c *appCtx) isMe(u *jira.UserField) bool {
	return u != nil && c.store.myself != nil && u.AccountID == c.store.myself.AccountID
}

type store struct {
	issues map[string]*jira.Issue
	scopes map[string]*scopeData

	comments map[string]*loadable[[]jira.Comment]
	history  map[string]*loadable[[]jira.Changelog]
	children map[string]*loadable[[]string]
	watch    map[string]*loadable[watchInfo]
	full     map[string]bool // issue was fetched individually (fresh)

	myself     *jira.Myself
	users      *loadable[[]jira.UserField]
	priorities *loadable[[]jira.NameField]
	types      *loadable[[]jira.IssueTypeMeta]
	statuses   *loadable[[]jira.StatusField] // project workflow statuses

	work *workIndex // branches, worktrees, PRs and agents per ticket
}

type loadable[T any] struct {
	val     T
	loading bool
	loaded  bool
	err     error
	at      time.Time
}

type watchInfo struct {
	watching bool
	count    int
}

// scopeData is the result of one scope's query: issue keys in API order.
type scopeData struct {
	keys      []string
	loading   bool
	loaded    bool
	cached    bool // keys came from the disk cache, a live fetch is pending
	truncated bool // the query hit the fetch limit
	err       error
	at        time.Time
}

func newStore() *store {
	return &store{
		issues:     map[string]*jira.Issue{},
		scopes:     map[string]*scopeData{},
		comments:   map[string]*loadable[[]jira.Comment]{},
		history:    map[string]*loadable[[]jira.Changelog]{},
		children:   map[string]*loadable[[]string]{},
		watch:      map[string]*loadable[watchInfo]{},
		full:       map[string]bool{},
		users:      &loadable[[]jira.UserField]{},
		priorities: &loadable[[]jira.NameField]{},
		types:      &loadable[[]jira.IssueTypeMeta]{},
		statuses:   &loadable[[]jira.StatusField]{},
		work:       newWorkIndex(),
	}
}

func (s *store) scope(id string) *scopeData {
	d, ok := s.scopes[id]
	if !ok {
		d = &scopeData{}
		s.scopes[id] = d
	}
	return d
}

// put stores issues, keeping the newest copy of each.
func (s *store) put(issues ...jira.Issue) {
	for i := range issues {
		is := issues[i]
		s.issues[is.Key] = &is
	}
}

func (s *store) issue(key string) *jira.Issue { return s.issues[key] }

// scopeIssues resolves a scope's keys to issues (skipping deleted ones).
func (s *store) scopeIssues(id string) []*jira.Issue {
	d := s.scopes[id]
	if d == nil {
		return nil
	}
	out := make([]*jira.Issue, 0, len(d.keys))
	for _, k := range d.keys {
		if is := s.issues[k]; is != nil {
			out = append(out, is)
		}
	}
	return out
}

// forget removes a deleted issue everywhere.
func (s *store) forget(key string) {
	delete(s.issues, key)
	for _, d := range s.scopes {
		d.keys = removeKey(d.keys, key)
	}
	for _, c := range s.children {
		c.val = removeKey(c.val, key)
	}
}

func removeKey(keys []string, key string) []string {
	out := keys[:0]
	for _, k := range keys {
		if k != key {
			out = append(out, k)
		}
	}
	return out
}

func (s *store) commentsOf(key string) *loadable[[]jira.Comment] {
	if s.comments[key] == nil {
		s.comments[key] = &loadable[[]jira.Comment]{}
	}
	return s.comments[key]
}

func (s *store) historyOf(key string) *loadable[[]jira.Changelog] {
	if s.history[key] == nil {
		s.history[key] = &loadable[[]jira.Changelog]{}
	}
	return s.history[key]
}

func (s *store) childrenOf(key string) *loadable[[]string] {
	if s.children[key] == nil {
		s.children[key] = &loadable[[]string]{}
	}
	return s.children[key]
}

func (s *store) watchOf(key string) *loadable[watchInfo] {
	if s.watch[key] == nil {
		s.watch[key] = &loadable[watchInfo]{}
	}
	return s.watch[key]
}

// knownStatuses returns every status seen in the project workflow or in
// loaded issues, in workflow order. Used for board columns and filters.
func (s *store) knownStatuses(scopeID string) []jira.StatusField {
	seen := map[string]jira.StatusField{}
	var order []string
	add := func(st jira.StatusField) {
		if st.Name == "" {
			return
		}
		if _, ok := seen[st.Name]; !ok {
			order = append(order, st.Name)
		}
		if st.StatusCategory != nil || seen[st.Name].StatusCategory == nil {
			seen[st.Name] = st
		}
	}
	if scopeID != scopeJQL {
		for _, st := range s.statuses.val {
			add(st)
		}
	}
	for _, is := range s.scopeIssues(scopeID) {
		add(is.Fields.Status)
	}
	out := make([]jira.StatusField, 0, len(order))
	for _, n := range order {
		out = append(out, seen[n])
	}
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := workflowRank(out[i]), workflowRank(out[j])
		if ri != rj {
			return ri < rj
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}
