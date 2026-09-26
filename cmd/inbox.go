package cmd

import (
	"fmt"
	"strings"

	"github.com/ngavilan-dogfy/jira-cli/ui"

	"github.com/spf13/cobra"
)

var (
	inboxSince string
	inboxLimit int
	inboxJSON  bool
	inboxPlain bool
	inboxMine  bool
)

var inboxCmd = &cobra.Command{
	Use:   "inbox",
	Short: "Show issues you watch that changed recently",
	Long: `Show recently updated issues you watch, reported, are assigned to,
or were mentioned in (your "Jira inbox" — what changed since you last looked).

By default looks back 2 days. Sorted by most-recently-updated first.

Examples:
  jira inbox                   # last 2 days
  jira inbox --since 1d        # last 24 hours
  jira inbox --since 7d        # last week
  jira inbox --mine            # only issues assigned to me
  jira inbox --json | jq '.[].key'`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Normalize "2d", "1h", "30m" etc → JQL relative format
		since := strings.TrimSpace(inboxSince)
		if since == "" {
			since = "-2d"
		}
		if !strings.HasPrefix(since, "-") {
			since = "-" + since
		}

		var clauses []string
		if inboxMine {
			clauses = append(clauses, "assignee = currentUser()")
		} else {
			clauses = append(clauses,
				"(watcher = currentUser() OR assignee = currentUser() OR reporter = currentUser())",
			)
		}
		clauses = append(clauses, fmt.Sprintf("updated >= \"%s\"", since))
		jql := strings.Join(clauses, " AND ") + " ORDER BY updated DESC"

		result, err := client.Search(jql, inboxLimit)
		if err != nil {
			return fmt.Errorf("search failed: %w", err)
		}

		if inboxJSON {
			return printJSON(issuesToJSON(result.Issues))
		}

		if len(result.Issues) == 0 {
			if isTTY() && !inboxPlain {
				fmt.Println(ui.Dimmed.Render(fmt.Sprintf("  No issues updated in the last %s.", strings.TrimPrefix(since, "-"))))
			}
			return nil
		}

		if !isTTY() || inboxPlain {
			return printIssuesTSV(result.Issues)
		}

		// Reuse the rich table from ls but with a custom title
		fmt.Println(ui.Title.Render(fmt.Sprintf(" inbox · last %s · %d updated", strings.TrimPrefix(since, "-"), len(result.Issues))))
		return printIssuesTable(result)
	},
}

func init() {
	inboxCmd.Flags().StringVar(&inboxSince, "since", "2d", "Relative time window (e.g. 1d, 12h, 7d)")
	inboxCmd.Flags().IntVarP(&inboxLimit, "limit", "n", 50, "Maximum number of issues")
	inboxCmd.Flags().BoolVar(&inboxJSON, "json", false, "Output as JSON")
	inboxCmd.Flags().BoolVar(&inboxPlain, "plain", false, "Force TSV output")
	inboxCmd.Flags().BoolVar(&inboxMine, "mine", false, "Only issues assigned to me")
	rootCmd.AddCommand(inboxCmd)
}
