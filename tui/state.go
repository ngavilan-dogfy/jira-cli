package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/ngavilan-dogfy/jira-cli/config"
	"github.com/ngavilan-dogfy/jira-cli/jira"
)

// uiState is what the TUI remembers between runs: where you were and how
// you like to look at things. Lives next to the profiles.
type uiState struct {
	Scope      string                `json:"scope"`
	Board      bool                  `json:"board"`
	Preview    *bool                 `json:"preview,omitempty"`
	Views      map[string]*scopeView `json:"views,omitempty"`
	Collapsed  map[string]bool       `json:"collapsed,omitempty"` // status name → folded
	JQL        string                `json:"jql,omitempty"`
	JQLHistory []string              `json:"jql_history,omitempty"`

	ReposRoot   string   `json:"repos_root,omitempty"`   // folder holding the git checkouts
	RecentRepos []string `json:"recent_repos,omitempty"` // where agents were started, newest first

	path string
}

// touchRepo records a repo as the most recently used for agents.
func (s *uiState) touchRepo(name string) {
	list := []string{name}
	for _, r := range s.RecentRepos {
		if r != name && len(list) < 10 {
			list = append(list, r)
		}
	}
	s.RecentRepos = list
	s.save()
}

// scopeView holds per-tab layout preferences.
type scopeView struct {
	Flat bool   `json:"flat,omitempty"`
	Sort string `json:"sort,omitempty"`
}

func statePath(profile string) string {
	if demoDir != "" {
		return filepath.Join(demoDir, "tui-"+profile+".json")
	}
	return filepath.Join(config.Dir(), "tui-"+profile+".json")
}

func loadState(profile string) *uiState {
	st := &uiState{path: statePath(profile)}
	if data, err := os.ReadFile(st.path); err == nil {
		_ = json.Unmarshal(data, st)
	}
	if st.Views == nil {
		st.Views = map[string]*scopeView{}
	}
	if st.Collapsed == nil {
		// Finished work starts folded.
		st.Collapsed = map[string]bool{"Done": true, "Closed": true, "Resolved": true}
	}
	if st.Scope == "" {
		st.Scope = scopeMine
	}
	return st
}

func (s *uiState) save() {
	if s.path == "" {
		return
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(s.path), 0o700)
	_ = os.WriteFile(s.path, data, 0o600)
}

func (s *uiState) view(scope string) *scopeView {
	v := s.Views[scope]
	if v == nil {
		v = &scopeView{Flat: scopeByID(scope).flat, Sort: sortUpdated}
		s.Views[scope] = v
	}
	if v.Sort == "" {
		v.Sort = sortUpdated
	}
	return v
}

func (s *uiState) pushJQL(q string) {
	hist := []string{q}
	for _, h := range s.JQLHistory {
		if h != q && len(hist) < 20 {
			hist = append(hist, h)
		}
	}
	s.JQLHistory = hist
}

// ─── scope cache ─────────────────────────────────────────────────
//
// The last result of each scope is kept on disk so the TUI paints real data
// instantly on startup and refreshes in the background.

type scopeCache struct {
	JQL    string       `json:"jql"`
	At     time.Time    `json:"at"`
	Issues []jira.Issue `json:"issues"`
}

func cachePath(profile, scope string) string {
	dir, err := os.UserCacheDir()
	if demoDir != "" {
		dir, err = demoDir, nil
	}
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "jira-cli", "tui", profile, scope+".json")
}

func readScopeCache(profile, scope, jql string) *scopeCache {
	p := cachePath(profile, scope)
	if p == "" {
		return nil
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var c scopeCache
	if json.Unmarshal(data, &c) != nil || c.JQL != jql {
		return nil
	}
	return &c
}

func writeScopeCache(profile, scope, jql string, issues []jira.Issue) {
	p := cachePath(profile, scope)
	if p == "" {
		return
	}
	data, err := json.Marshal(scopeCache{JQL: jql, At: time.Now(), Issues: issues})
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	tmp := p + ".tmp"
	if os.WriteFile(tmp, data, 0o600) == nil {
		_ = os.Rename(tmp, p)
	}
}
