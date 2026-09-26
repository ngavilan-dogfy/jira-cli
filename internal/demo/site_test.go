package demo

import (
	"strings"
	"testing"
)

func keys(s *site, jql string) string {
	var out []string
	for _, is := range s.search(jql) {
		out = append(out, is.Key)
	}
	return strings.Join(out, " ")
}

// The TUI's tabs are JQL queries: the demo site must answer each one the
// way Jira would.
func TestSearchAnswersTheTabs(t *testing.T) {
	s := newSite()
	recent := `(statusCategory != Done OR updated >= -14d)`
	for _, c := range []struct {
		jql, want, not string
	}{
		{"project = SHOP AND assignee = currentUser() AND " + recent + " ORDER BY updated DESC", "SHOP-150", "SHOP-120"},
		{"project = SHOP AND watcher = currentUser() AND " + recent + " ORDER BY updated DESC", "SHOP-156", "SHOP-150"},
		{"project = SHOP AND issuetype = Epic AND (statusCategory != Done OR updated >= -30d) ORDER BY updated DESC", "SHOP-101", "SHOP-110"},
		{"key in (SHOP-110, SHOP-112) ORDER BY updated DESC", "SHOP-110", "SHOP-150"},
		{"parent = SHOP-110", "SHOP-114", "SHOP-111"},
		{`project = SHOP AND text ~ "coupon" ORDER BY updated DESC`, "SHOP-112", "SHOP-150"},
		{`status = "In Review"`, "SHOP-156", "SHOP-150"},
		{"assignee is EMPTY", "SHOP-113", "SHOP-150"},
		{"labels = payments", "SHOP-140", "SHOP-150"},
	} {
		got := " " + keys(s, c.jql) + " "
		if !strings.Contains(got, " "+c.want+" ") || strings.Contains(got, " "+c.not+" ") {
			t.Errorf("%s\n  got %s (want %s, not %s)", c.jql, got, c.want, c.not)
		}
	}
}
