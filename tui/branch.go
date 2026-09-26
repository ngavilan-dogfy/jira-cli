package tui

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"unicode"

	"github.com/ngavilan-dogfy/jira-cli/jira"

	tea "github.com/charmbracelet/bubbletea"
)

// branchName mirrors `jira branch`: <prefix>/<KEY>-<slug>, max 80 chars.
func branchName(issue *jira.Issue) string {
	name := fmt.Sprintf("%s/%s-%s", inferBranchPrefix(issue.Fields.IssueType.Name), issue.Key, slugify(issue.Fields.Summary))
	if len(name) > 80 {
		name = strings.TrimRight(name[:80], "-")
	}
	return name
}

// cmdCreateBranch runs `git checkout -b` in the current directory. Outside a
// git repo it copies the branch name instead, so the key still does
// something useful.
func cmdCreateBranch(issue *jira.Issue) tea.Cmd {
	return func() tea.Msg {
		name := branchName(issue)
		inRepo := false
		if _, err := exec.LookPath("git"); err == nil {
			inRepo = exec.Command("git", "rev-parse", "--git-dir").Run() == nil
		}
		if !inRepo {
			if err := writeClipboard(name); err != nil {
				return toastMsg{text: "not a git repo, and clipboard failed: " + err.Error(), kind: toastErr}
			}
			return toastMsg{text: "Not in a git repo — copied " + name, kind: toastInfo}
		}
		out, err := exec.Command("git", "checkout", "-b", name).CombinedOutput()
		if err != nil {
			return toastMsg{text: "git checkout: " + strings.TrimSpace(string(out)), kind: toastErr}
		}
		return toastMsg{text: "Switched to new branch " + name, kind: toastOK}
	}
}

func inferBranchPrefix(issueType string) string {
	switch strings.ToLower(issueType) {
	case "bug":
		return "fix"
	case "spike", "chore", "task chore":
		return "chore"
	case "epic":
		return "epic"
	}
	return "feat"
}

var slugReTUI = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	var b strings.Builder
	for _, r := range fold(s) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
		default:
			b.WriteRune('-')
		}
	}
	slug := slugReTUI.ReplaceAllString(b.String(), "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		slug = "task"
	}
	if len(slug) > 50 {
		// Cut at a word boundary: "...-y-alertas", not "...-y-alertas-b".
		cut := slug[:50]
		if i := strings.LastIndexByte(cut, '-'); i > 20 {
			cut = cut[:i]
		}
		slug = strings.TrimRight(cut, "-")
	}
	return slug
}
