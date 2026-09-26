package cmd

import "testing"

func TestNormalizeKeyTakesLinks(t *testing.T) {
	for in, want := range map[string]string{
		"proj-12": "PROJ-12",
		"https://acme.atlassian.net/browse/OPS-7":                                                "OPS-7",
		"https://acme.atlassian.net/browse/ops-7?focusedCommentId=1":                             "OPS-7",
		"https://acme.atlassian.net/jira/software/c/projects/WEB/boards/3?selectedIssue=WEB-306": "WEB-306",
		"acme.atlassian.net/jira/your-work?selectedIssue=HR2-5":                                  "HR2-5",
	} {
		if got := normalizeKey(in); got != want {
			t.Errorf("normalizeKey(%q) = %q, want %q", in, got, want)
		}
	}
}
