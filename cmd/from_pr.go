package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/ngavilan-dogfy/jira-cli/ui"

	"github.com/spf13/cobra"
)

var (
	fromPRType     string
	fromPRPriority string
	fromPRJSON     bool
	fromPRNoLink   bool
)

var fromPRCmd = &cobra.Command{
	Use:   "from-pr <pr-url-or-number>",
	Short: "Create a Jira issue from a GitHub PR",
	Long: `Create a Jira issue using the GitHub PR's title and body as summary/description.

Requires the GitHub CLI (gh) on PATH and an authenticated session.

Examples:
  jira from-pr https://github.com/acme/api/pull/1234
  jira from-pr 1234                       # in a repo, gh resolves it
  jira from-pr 1234 --type Bug --priority High
  jira from-pr 1234 --no-link             # don't add a "relates to" link comment`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if _, err := exec.LookPath("gh"); err != nil {
			return fmt.Errorf("gh CLI not found on PATH — install it from https://cli.github.com/")
		}

		pr, err := fetchPR(args[0])
		if err != nil {
			return err
		}

		summary := strings.TrimSpace(pr.Title)
		if summary == "" {
			summary = fmt.Sprintf("Work for PR #%d", pr.Number)
		}

		desc := buildPRDescription(pr)
		issue, err := client.CreateIssue(cfg.Project, fromPRType, summary, "", desc, "")
		if err != nil {
			return fmt.Errorf("failed to create issue: %w", err)
		}

		if !fromPRNoLink {
			// Best-effort: add a comment pointing back to the PR (Jira will detect
			// the URL and offer a smart link).
			body := fmt.Sprintf("Related GitHub PR: %s", pr.URL)
			_ = client.AddComment(issue.Key, body)
		}

		if fromPRJSON {
			return printJSON(map[string]string{
				"key": issue.Key,
				"url": client.BrowseURL(issue.Key),
				"pr":  pr.URL,
			})
		}
		if !isTTY() {
			fmt.Println(issue.Key)
			return nil
		}
		fmt.Println(ui.SuccessStyle.Render(fmt.Sprintf("  OK  Created %s from %s#%d", issue.Key, pr.HeadRepo, pr.Number)))
		fmt.Println(ui.Dimmed.Render("  " + client.BrowseURL(issue.Key)))
		return nil
	},
}

type ghPR struct {
	Number   int    `json:"number"`
	Title    string `json:"title"`
	Body     string `json:"body"`
	URL      string `json:"url"`
	State    string `json:"state"`
	HeadRepo string `json:"-"`
	Author   struct {
		Login string `json:"login"`
	} `json:"author"`
}

// fetchPR uses `gh pr view <ref> --json ...` to get PR metadata.
func fetchPR(ref string) (*ghPR, error) {
	cmd := exec.Command("gh", "pr", "view", ref, "--json", "number,title,body,url,state,author")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("gh pr view failed: %s", strings.TrimSpace(stderr.String()))
	}
	var pr ghPR
	if err := json.Unmarshal(stdout.Bytes(), &pr); err != nil {
		return nil, fmt.Errorf("failed to parse gh output: %w", err)
	}
	// Derive a friendly repo string from the URL: https://github.com/OWNER/REPO/pull/N
	parts := strings.Split(pr.URL, "/")
	if len(parts) >= 5 {
		pr.HeadRepo = parts[3] + "/" + parts[4]
	}
	return &pr, nil
}

func buildPRDescription(pr *ghPR) string {
	var b strings.Builder
	b.WriteString("## Source PR\n\n")
	b.WriteString(fmt.Sprintf("**Repo:** %s · **#%d** · state: %s · author: @%s\n\n",
		pr.HeadRepo, pr.Number, pr.State, pr.Author.Login))
	b.WriteString(fmt.Sprintf("**URL:** %s\n\n", pr.URL))
	b.WriteString("---\n\n")
	if strings.TrimSpace(pr.Body) != "" {
		b.WriteString("## PR description\n\n")
		b.WriteString(pr.Body)
		b.WriteString("\n")
	}
	return b.String()
}

func init() {
	fromPRCmd.Flags().StringVarP(&fromPRType, "type", "t", "Task", "Issue type")
	fromPRCmd.Flags().StringVar(&fromPRPriority, "priority", "", "Priority to set after creation")
	fromPRCmd.Flags().BoolVar(&fromPRJSON, "json", false, "Output as JSON")
	fromPRCmd.Flags().BoolVar(&fromPRNoLink, "no-link", false, "Don't add a 'related PR' comment after creation")
	rootCmd.AddCommand(fromPRCmd)
}
