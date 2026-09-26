package tui

import (
	"fmt"
	"regexp"
	"strings"
)

// A scope is one tab of the browser: a saved query. Done work only lingers
// for a couple of weeks so the lists stay about what's still moving.

const (
	scopeMine     = "mine"
	scopeWork     = "work"
	scopeTeam     = "team"
	scopeWatching = "watching"
	scopeRecent   = "recent"
	scopeEpics    = "epics"
	scopeJQL      = "jql"
)

type scopeDef struct {
	id    string
	title string
	limit int
	jql   func(project string) string
	flat  bool // default layout: flat list sorted by update time
}

const recentDone = `(statusCategory != Done OR updated >= -14d)`

var scopeDefs = []scopeDef{
	{id: scopeMine, title: "Mine", limit: 500, jql: func(p string) string {
		return fmt.Sprintf("project = %s AND assignee = currentUser() AND %s ORDER BY updated DESC", p, recentDone)
	}},
	// Tickets with work in flight on this machine or on GitHub; its JQL is
	// built from the work index (see scopeJQLFor).
	{id: scopeWork, title: "Work", limit: 300},
	{id: scopeTeam, title: "Team", limit: 600, jql: func(p string) string {
		return fmt.Sprintf("project = %s AND %s ORDER BY updated DESC", p, recentDone)
	}},
	{id: scopeWatching, title: "Watching", limit: 400, jql: func(p string) string {
		return fmt.Sprintf("project = %s AND watcher = currentUser() AND %s ORDER BY updated DESC", p, recentDone)
	}},
	// Latest activity whatever the pace: a quiet week still shows something.
	{id: scopeRecent, title: "Recent", limit: 100, flat: true, jql: func(p string) string {
		return fmt.Sprintf("project = %s ORDER BY updated DESC", p)
	}},
	{id: scopeEpics, title: "Epics", limit: 200, jql: func(p string) string {
		return fmt.Sprintf("project = %s AND issuetype = Epic AND (statusCategory != Done OR updated >= -30d) ORDER BY updated DESC", p)
	}},
	{id: scopeJQL, title: "Search", limit: 300},
}

func scopeByID(id string) scopeDef {
	for _, s := range scopeDefs {
		if s.id == id {
			return s
		}
	}
	return scopeDefs[0]
}

func scopeIndex(id string) int {
	for i, s := range scopeDefs {
		if s.id == id {
			return i
		}
	}
	return 0
}

// scopeJQLFor resolves the query for a scope; the search scope uses the
// user's last query.
func (c *appCtx) scopeJQLFor(id string) string {
	switch id {
	case scopeJQL:
		return c.state.JQL
	case scopeWork:
		keys := c.store.work.activeKeys(c.keyRe())
		if len(keys) == 0 {
			return ""
		}
		return "key in (" + strings.Join(keys, ", ") + ") ORDER BY updated DESC"
	}
	return scopeByID(id).jql(c.project())
}

// visibleScopes hides the search tab until a query exists.
func (c *appCtx) visibleScopes() []scopeDef {
	var out []scopeDef
	for _, s := range scopeDefs {
		if s.id == scopeJQL && c.state.JQL == "" {
			continue
		}
		out = append(out, s)
	}
	return out
}

var jqlish = regexp.MustCompile(`(?i)(\s(=|!=|~|!~|>=|<=|>|<|in|not in|is|was)\s|order\s+by|^\s*(project|assignee|status|labels?|text|summary|key|issuetype|type|parent|reporter|sprint|updated|created)\s*(=|!=|~|in\b|is\b|>|<))`)

var keyish = regexp.MustCompile(`^\s*([A-Za-z][A-Za-z0-9]{1,9}-)?\d+\s*$`)

// toJQL turns what the user typed in the search prompt into JQL: real JQL
// passes through, anything else becomes a full-text search in the project.
func toJQL(input, project string) string {
	q := strings.TrimSpace(input)
	if q == "" {
		return ""
	}
	if jqlish.MatchString(q) {
		return q
	}
	esc := strings.ReplaceAll(q, `"`, `\"`)
	return fmt.Sprintf(`project = %s AND text ~ "%s" ORDER BY updated DESC`, project, esc)
}

// normalizeKey expands "306" to "PROJ-306" and uppercases full keys.
func normalizeKey(s, project string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	if !strings.Contains(s, "-") && project != "" {
		return project + "-" + s
	}
	return s
}
