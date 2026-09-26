// Package demo is a made-up Jira Cloud site for 'jira ui --demo': the Acme
// Shop team's SHOP project, with epics, stories, bugs and sub-tasks in every
// status, comments, history, links and five people. It lives in memory and
// answers the REST calls the TUI makes through an http.RoundTripper, so
// nothing goes over the network; changes last until you quit.
package demo

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ngavilan-dogfy/jira-cli/config"
	"github.com/ngavilan-dogfy/jira-cli/jira"
)

// Project is the demo project's key.
const Project = "SHOP"

// SiteURL is where the demo pretends to live. Links are never opened.
const SiteURL = "https://acme-shop.example"

// Profile is the demo's own profile: it never touches the saved ones.
func Profile() *config.Profile {
	return &config.Profile{Name: "demo", AuthMethod: "token", Email: "sam@acme.example", Token: "demo",
		SiteURL: SiteURL, Project: Project}
}

// Client returns a Jira client that talks to a fresh demo site.
func Client() *jira.Client {
	c := jira.NewBasicClient(SiteURL, "sam@acme.example", "demo")
	c.SetTransport(transport{newSite()})
	return c
}

type transport struct{ s *site }

func (t transport) RoundTrip(r *http.Request) (*http.Response, error) {
	in := r.Clone(r.Context())
	if in.Body == nil { // client requests may have none; handlers expect one
		in.Body = http.NoBody
	}
	rec := httptest.NewRecorder()
	t.s.ServeHTTP(rec, in)
	resp := rec.Result()
	resp.Request = r
	return resp, nil
}

type site struct {
	mu       sync.Mutex
	start    time.Time
	issues   map[string]*jira.Issue
	order    []string
	comments map[string][]jira.Comment
	history  map[string][]jira.Changelog
	watching map[string]bool
	next     int
}

func newSite() *site {
	s := &site{start: time.Now(), issues: map[string]*jira.Issue{}, comments: map[string][]jira.Comment{},
		history: map[string][]jira.Changelog{}, watching: map[string]bool{}}
	s.seed()
	return s
}

var reIssuePath = regexp.MustCompile(`^/rest/api/3/issue/([A-Z]+-\d+)(/[a-z]+)?$`)

func (s *site) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	body, _ := io.ReadAll(r.Body)
	reply := func(code int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(v)
	}
	fail := func(code int, msg string) { reply(code, map[string]any{"errorMessages": []string{msg}}) }
	p := r.URL.Path
	switch {
	case p == "/rest/api/3/myself":
		reply(200, jira.Myself{AccountID: me.AccountID, DisplayName: me.DisplayName})
		return
	case p == "/rest/api/3/serverInfo":
		reply(200, map[string]any{"baseUrl": SiteURL, "deploymentType": "Cloud", "serverTitle": "Acme Shop"})
		return
	case p == "/rest/api/3/search/jql":
		var q struct {
			JQL        string `json:"jql"`
			MaxResults int    `json:"maxResults"`
		}
		_ = json.Unmarshal(body, &q)
		if q.JQL == "" {
			q.JQL = r.URL.Query().Get("jql")
		}
		found := s.search(q.JQL)
		if q.MaxResults > 0 && len(found) > q.MaxResults {
			found = found[:q.MaxResults]
		}
		reply(200, map[string]any{"issues": found, "isLast": true})
		return
	case strings.HasPrefix(p, "/rest/api/3/project/") && strings.HasSuffix(p, "/statuses"):
		var out []any
		for _, t := range issueTypes {
			out = append(out, map[string]any{"id": t.ID, "name": t.Name, "statuses": statuses})
		}
		reply(200, out)
		return
	case p == "/rest/api/3/user/assignable/search" || p == "/rest/api/3/user/search":
		q := strings.ToLower(r.URL.Query().Get("query"))
		var out []jira.UserField
		for _, u := range people {
			if q == "" || strings.Contains(strings.ToLower(u.DisplayName), q) {
				out = append(out, u)
			}
		}
		reply(200, out)
		return
	case p == "/rest/api/3/priority":
		reply(200, []jira.NameField{{ID: "1", Name: "Highest"}, {ID: "2", Name: "High"}, {ID: "3", Name: "Medium"}, {ID: "4", Name: "Low"}, {ID: "5", Name: "Lowest"}})
		return
	case strings.HasPrefix(p, "/rest/api/3/issue/createmeta/"):
		var out []jira.IssueTypeMeta
		for _, t := range issueTypes {
			out = append(out, jira.IssueTypeMeta{ID: t.ID, Name: t.Name, Subtask: t.Subtask})
		}
		reply(200, map[string]any{"issueTypes": out, "values": out})
		return
	case p == "/rest/api/3/issue" && r.Method == http.MethodPost:
		s.create(w, body)
		return
	}

	m := reIssuePath.FindStringSubmatch(p)
	if m == nil {
		fail(404, "The demo doesn't know "+p)
		return
	}
	key, sub := m[1], m[2]
	is := s.issues[key]
	if is == nil {
		fail(404, "Issue does not exist or you do not have permission to see it.")
		return
	}
	switch {
	case sub == "" && r.Method == http.MethodGet:
		reply(200, is)
	case sub == "" && r.Method == http.MethodPut:
		var in struct{ Fields map[string]json.RawMessage }
		_ = json.Unmarshal(body, &in)
		for k, v := range in.Fields {
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
		s.touch(is)
		w.WriteHeader(204)
	case sub == "" && r.Method == http.MethodDelete:
		delete(s.issues, key)
		w.WriteHeader(204)
	case sub == "/assignee":
		var in struct{ AccountID *string }
		_ = json.Unmarshal(body, &in)
		is.Fields.Assignee = nil
		for _, u := range people {
			if in.AccountID != nil && u.AccountID == *in.AccountID {
				u := u
				is.Fields.Assignee = &u
			}
		}
		s.touch(is)
		w.WriteHeader(204)
	case sub == "/transitions" && r.Method == http.MethodGet:
		var ts []jira.Transition
		for _, st := range statuses {
			if st.Name != is.Fields.Status.Name {
				ts = append(ts, jira.Transition{ID: st.ID, Name: st.Name, To: st})
			}
		}
		reply(200, map[string]any{"transitions": ts})
	case sub == "/transitions":
		var in struct{ Transition struct{ ID string } }
		_ = json.Unmarshal(body, &in)
		for _, st := range statuses {
			if st.ID == in.Transition.ID {
				s.history[key] = append(s.history[key], jira.Changelog{ID: fmt.Sprint("h", len(s.history[key])+100), Author: me,
					Created: jtime(time.Now()), Items: []jira.ChangelogItem{{Field: "status", FromString: is.Fields.Status.Name, ToString: st.Name}}})
				is.Fields.Status = st
			}
		}
		is.Fields.Resolution = nil
		if is.Fields.Status.CategoryKey() == "done" {
			is.Fields.Resolution = &jira.NameField{Name: "Done"}
		}
		s.touch(is)
		w.WriteHeader(204)
	case sub == "/comment" && r.Method == http.MethodGet:
		reply(200, map[string]any{"comments": s.comments[key], "total": len(s.comments[key]), "startAt": 0, "maxResults": 100})
	case sub == "/comment":
		var in struct{ Body json.RawMessage }
		_ = json.Unmarshal(body, &in)
		s.next++
		c := jira.Comment{ID: fmt.Sprint("c", s.next), Author: me, Body: in.Body, Created: jtime(time.Now())}
		s.comments[key] = append(s.comments[key], c)
		s.touch(is)
		reply(201, c)
	case sub == "/watchers" && r.Method == http.MethodGet:
		n := 1
		if s.watching[key] {
			n = 2
		}
		reply(200, map[string]any{"watchCount": n, "isWatching": s.watching[key]})
	case sub == "/watchers" && r.Method == http.MethodPost:
		s.watching[key] = true
		w.WriteHeader(204)
	case sub == "/watchers" && r.Method == http.MethodDelete:
		s.watching[key] = false
		w.WriteHeader(204)
	case sub == "/changelog":
		h := s.history[key]
		out := make([]jira.Changelog, len(h))
		for i := range h { // newest first
			out[i] = h[len(h)-1-i]
		}
		reply(200, map[string]any{"total": len(out), "startAt": 0, "maxResults": 100, "isLast": true, "values": out})
	default:
		fail(400, "The demo doesn't support "+r.Method+" "+p)
	}
}

func (s *site) touch(is *jira.Issue) { is.Fields.Updated = jtime(time.Now()) }

func (s *site) create(w http.ResponseWriter, body []byte) {
	var in struct {
		Fields struct {
			Summary   string
			IssueType struct {
				ID   string
				Name string
			} `json:"issuetype"`
			Parent   *struct{ Key string }
			Assignee *struct{ AccountID string }
			Priority *jira.NameField
			Labels   []string
		}
	}
	_ = json.Unmarshal(body, &in)
	typ := tTask
	for _, t := range issueTypes {
		if t.ID == in.Fields.IssueType.ID || t.Name == in.Fields.IssueType.Name {
			typ = t
		}
	}
	n := 200
	for k := range s.issues {
		if v, err := strconv.Atoi(strings.TrimPrefix(k, Project+"-")); err == nil && v >= n {
			n = v + 1
		}
	}
	sd := seed{n: n, typ: typ, st: stBacklog, summary: in.Fields.Summary, reporter: me, prio: "Medium", labels: in.Fields.Labels}
	if in.Fields.Priority != nil && in.Fields.Priority.Name != "" {
		sd.prio = in.Fields.Priority.Name
	}
	if in.Fields.Assignee != nil {
		for _, u := range people {
			if u.AccountID == in.Fields.Assignee.AccountID {
				u := u
				sd.assignee = &u
			}
		}
	}
	if in.Fields.Parent != nil {
		if v, err := strconv.Atoi(strings.TrimPrefix(in.Fields.Parent.Key, Project+"-")); err == nil && s.issues[in.Fields.Parent.Key] != nil {
			sd.parent = v
		}
	}
	s.add(sd)
	key := fmt.Sprintf("%s-%d", Project, n)
	s.touch(s.issues[key])
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(201)
	_ = json.NewEncoder(w).Encode(map[string]string{"id": s.issues[key].ID, "key": key})
}

// ─── JQL ─────────────────────────────────────────────────────────

var (
	reOrder    = regexp.MustCompile(`(?i)\s+order\s+by\s+(.+)$`)
	reKeyIn    = regexp.MustCompile(`(?i)^key\s+in\s*\(([^)]*)\)$`)
	reClause   = regexp.MustCompile(`(?i)^(project|assignee|status|statuscategory|issuetype|type|parent|labels?|priority|reporter|text|summary|updated|created|watcher)\s*(!=|>=|<=|=|~|>|<|\bnot in\b|\bin\b|\bis not\b|\bis\b)\s*(.+)$`)
	reDuration = regexp.MustCompile(`^-(\d+)([dhwm])$`)
)

// search understands the JQL the TUI and people type: AND, OR inside
// parentheses, and clauses on the common fields. Clauses it doesn't know
// match everything, so a search never comes back empty by surprise.
func (s *site) search(jql string) []jira.Issue {
	order := ""
	if m := reOrder.FindStringSubmatch(jql); m != nil {
		order = strings.ToLower(m[1])
		jql = jql[:len(jql)-len(m[0])]
	}
	var out []jira.Issue
	for _, k := range s.order {
		is, ok := s.issues[k]
		if !ok {
			continue
		}
		if s.matchAll(splitTop(jql, "AND"), is) {
			out = append(out, *is)
		}
	}
	for k, is := range s.issues { // created during the demo
		if !contains(s.order, k) && s.matchAll(splitTop(jql, "AND"), is) {
			out = append(out, *is)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].Fields, out[j].Fields
		if strings.HasPrefix(order, "statuscategory") && rank(a.Status) != rank(b.Status) {
			return rank(a.Status) < rank(b.Status)
		}
		return a.Updated > b.Updated
	})
	return out
}

func rank(st jira.StatusField) int {
	return map[string]int{"indeterminate": 0, "new": 1, "done": 2}[st.CategoryKey()]
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// splitTop splits on a keyword outside parentheses and quotes.
func splitTop(s, word string) []string {
	var parts []string
	depth, quoted, last := 0, false, 0
	up := strings.ToUpper(s)
	sep := " " + word + " "
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			quoted = !quoted
		case '(':
			if !quoted {
				depth++
			}
		case ')':
			if !quoted {
				depth--
			}
		}
		if depth == 0 && !quoted && strings.HasPrefix(up[i:], sep) {
			parts = append(parts, strings.TrimSpace(s[last:i]))
			last = i + len(sep)
			i += len(sep) - 1
		}
	}
	return append(parts, strings.TrimSpace(s[last:]))
}

func (s *site) matchAll(clauses []string, is *jira.Issue) bool {
	for _, c := range clauses {
		if !s.match(c, is) {
			return false
		}
	}
	return true
}

func (s *site) match(c string, is *jira.Issue) bool {
	c = strings.TrimSpace(c)
	if c == "" {
		return true
	}
	if strings.HasPrefix(c, "(") && strings.HasSuffix(c, ")") {
		inner := c[1 : len(c)-1]
		if ors := splitTop(inner, "OR"); len(ors) > 1 {
			for _, o := range ors {
				if s.match(o, is) {
					return true
				}
			}
			return false
		}
		return s.matchAll(splitTop(inner, "AND"), is)
	}
	if m := reKeyIn.FindStringSubmatch(c); m != nil {
		for _, k := range strings.Split(m[1], ",") {
			if strings.TrimSpace(k) == is.Key {
				return true
			}
		}
		return false
	}
	m := reClause.FindStringSubmatch(c)
	if m == nil {
		return true
	}
	field, op, val := strings.ToLower(m[1]), strings.ToLower(m[2]), strings.TrimSpace(m[3])
	f := is.Fields
	values := func() []string {
		v := strings.Trim(val, "()")
		var out []string
		for _, x := range strings.Split(v, ",") {
			out = append(out, strings.ToLower(strings.Trim(strings.TrimSpace(x), `"'`)))
		}
		return out
	}
	eq := func(got string) bool {
		got = strings.ToLower(got)
		in := contains(values(), got)
		switch op {
		case "!=", "not in":
			return !in
		}
		return in
	}
	switch field {
	case "project":
		return eq(strings.Split(is.Key, "-")[0])
	case "assignee", "reporter":
		u := f.Assignee
		if field == "reporter" {
			u = f.Reporter
		}
		switch {
		case op == "is" || op == "is not":
			empty := u == nil
			if op == "is" {
				return empty
			}
			return !empty
		case strings.EqualFold(val, "currentUser()"):
			hit := u != nil && u.AccountID == me.AccountID
			if op == "!=" {
				return !hit
			}
			return hit
		case u == nil:
			return op == "!=" || op == "not in"
		default:
			return eq(u.AccountID) || eq(u.DisplayName)
		}
	case "status":
		return eq(f.Status.Name)
	case "statuscategory":
		return eq(map[string]string{"new": "to do", "indeterminate": "in progress", "done": "done"}[f.Status.CategoryKey()])
	case "issuetype", "type":
		if strings.EqualFold(val, "subTaskIssueTypes()") {
			return f.IssueType.Subtask == (op == "in")
		}
		return eq(f.IssueType.Name)
	case "parent":
		if f.Parent == nil {
			return op == "!=" || op == "not in"
		}
		return eq(f.Parent.Key)
	case "label", "labels":
		if op == "is" || op == "is not" {
			return (len(f.Labels) == 0) == (op == "is")
		}
		for _, l := range f.Labels {
			if contains(values(), strings.ToLower(l)) {
				return op != "!=" && op != "not in"
			}
		}
		return op == "!=" || op == "not in"
	case "priority":
		if f.Priority == nil {
			return false
		}
		return eq(f.Priority.Name)
	case "watcher":
		return s.watching[is.Key] == (op == "=")
	case "text", "summary":
		q := strings.ToLower(strings.Trim(val, `"'`))
		hay := strings.ToLower(f.Summary + " " + is.Key + " " + string(f.Description))
		return strings.Contains(hay, q)
	case "updated", "created":
		d := reDuration.FindStringSubmatch(strings.Trim(val, `"'`))
		if d == nil {
			return true
		}
		n, _ := strconv.Atoi(d[1])
		unit := map[string]time.Duration{"d": 24 * time.Hour, "h": time.Hour, "w": 7 * 24 * time.Hour, "m": time.Minute}[d[2]]
		ts := f.Updated
		if field == "created" {
			ts = f.Created
		}
		t, err := time.Parse("2006-01-02T15:04:05.000-0700", ts)
		if err != nil {
			return true
		}
		cut := time.Now().Add(-time.Duration(n) * unit)
		if op == ">=" || op == ">" {
			return t.After(cut)
		}
		return t.Before(cut)
	}
	return true
}
