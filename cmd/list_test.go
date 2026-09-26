package cmd

import (
	"testing"

	"github.com/ngavilan-dogfy/jira-cli/config"
)

func TestSinceToJQL(t *testing.T) {
	cases := map[string]string{
		"":           "",
		"7d":         "updated >= -7d",
		"30m":        "updated >= -30m",
		"4h":         "updated >= -4h",
		"2w":         "updated >= -2w",
		"2026-01-31": `updated >= "2026-01-31"`,
	}
	for in, want := range cases {
		if got := sinceToJQL(in); got != want {
			t.Errorf("sinceToJQL(%q) = %q, want %q", in, got, want)
		}
	}
}

func resetListFlags() {
	listStatus, listType, listJQL = "", "", ""
	listLabel, listAssignee, listReporter = "", "", ""
	listParent, listQuery, listSince = "", "", ""
	listAll = false
}

func TestBuildListJQL(t *testing.T) {
	cfg = &config.Profile{Project: "PROJ"}
	defer func() { cfg = nil; resetListFlags() }()

	cases := []struct {
		name  string
		setup func()
		want  string
	}{
		{
			name:  "default is my issues",
			setup: func() {},
			want:  "project = PROJ AND assignee = currentUser() ORDER BY statusCategory ASC, updated DESC",
		},
		{
			name:  "all drops assignee clause",
			setup: func() { listAll = true },
			want:  "project = PROJ ORDER BY statusCategory ASC, updated DESC",
		},
		{
			name:  "assignee none",
			setup: func() { listAssignee = "none" },
			want:  "project = PROJ AND assignee IS EMPTY ORDER BY statusCategory ASC, updated DESC",
		},
		{
			name:  "query drops default assignee scope",
			setup: func() { listQuery = "timeout" },
			want:  `project = PROJ AND text ~ "timeout" ORDER BY statusCategory ASC, updated DESC`,
		},
		{
			name:  "parent drops default assignee scope",
			setup: func() { listParent = "10" },
			want:  "project = PROJ AND parent = PROJ-10 ORDER BY statusCategory ASC, updated DESC",
		},
		{
			name:  "multiple labels use IN",
			setup: func() { listAll = true; listLabel = "backend, urgent" },
			want:  `project = PROJ AND labels IN ("backend", "urgent") ORDER BY statusCategory ASC, updated DESC`,
		},
		{
			name:  "combined filters",
			setup: func() { listStatus = "In Progress"; listSince = "7d" },
			want:  `project = PROJ AND assignee = currentUser() AND status = "In Progress" AND updated >= -7d ORDER BY statusCategory ASC, updated DESC`,
		},
		{
			name:  "raw JQL wins",
			setup: func() { listJQL = "labels = foo" },
			want:  "labels = foo",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetListFlags()
			tc.setup()
			if got := buildListJQL(); got != tc.want {
				t.Errorf("buildListJQL()\n  got:  %s\n  want: %s", got, tc.want)
			}
		})
	}
}
