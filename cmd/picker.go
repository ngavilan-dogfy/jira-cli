package cmd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/ngavilan-dogfy/jira-cli/jira"
)

// pickIssueKey returns an issue key chosen interactively via fzf.
//
// If fzf is not installed, returns an explanatory error suggesting how to
// install it or pass the key explicitly. Uses the user's own issues by default
// (mirroring `jira ls`), filtered by extraJQL when non-empty.
func pickIssueKey(extraJQL string) (string, error) {
	if _, err := exec.LookPath("fzf"); err != nil {
		return "", fmt.Errorf("no issue key provided and fzf not found on PATH — install fzf or pass the key explicitly")
	}
	if client == nil {
		return "", fmt.Errorf("not signed in — run 'jira setup'")
	}

	myself, err := client.GetMyself()
	if err != nil {
		return "", fmt.Errorf("failed to identify current user: %w", err)
	}

	jql := fmt.Sprintf("assignee = \"%s\" AND statusCategory != Done", myself.AccountID)
	if extraJQL != "" {
		jql = extraJQL
	}
	jql += " ORDER BY updated DESC"

	result, err := client.Search(jql, 100)
	if err != nil {
		return "", fmt.Errorf("search failed: %w", err)
	}
	if len(result.Issues) == 0 {
		return "", fmt.Errorf("no issues to pick from")
	}

	var lines []string
	for _, issue := range result.Issues {
		status := issue.Fields.Status.Name
		summary := strings.ReplaceAll(issue.Fields.Summary, "\t", " ")
		lines = append(lines, fmt.Sprintf("%s\t[%s]\t%s", issue.Key, status, summary))
	}

	cmd := exec.Command("fzf", "--ansi", "--with-nth=1..", "--delimiter=\t", "--prompt=issue> ")
	cmd.Stdin = strings.NewReader(strings.Join(lines, "\n"))
	cmd.Stderr = os.Stderr
	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("fzf cancelled or failed: %w", err)
	}
	selected := strings.TrimSpace(out.String())
	if selected == "" {
		return "", fmt.Errorf("no selection made")
	}
	parts := strings.SplitN(selected, "\t", 2)
	return parts[0], nil
}

// resolveKeyOrPick returns the key from args[0] if provided, otherwise
// prompts the user with fzf to pick one.
func resolveKeyOrPick(args []string, extraJQL string) (string, error) {
	if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
		return normalizeKey(args[0]), nil
	}
	return pickIssueKey(extraJQL)
}

// findIssueByKeyPrefix is a small helper used by commands that take optional
// extra context (history, branch, etc.) and want to surface a useful summary.
func findSummary(key string) string {
	if client == nil {
		return ""
	}
	issue, err := client.GetIssue(key)
	if err != nil {
		return ""
	}
	return issue.Fields.Summary
}

// joinNonEmpty is a tiny helper used by several commands.
func joinNonEmpty(sep string, parts ...string) string {
	var out []string
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, sep)
}

// _ keeps the jira import in use even if some files reference it indirectly.
var _ = jira.Client{}
