package cmd

import (
	"fmt"

	"github.com/ngavilan-dogfy/jira-cli/jira"
	"github.com/ngavilan-dogfy/jira-cli/ui"

	"github.com/spf13/cobra"
)

var epicsJSON bool

var epicsCmd = &cobra.Command{
	Use:   "epics",
	Short: "List epics in the project",
	Long: `List all epics in the current project with their status and child issue counts.

Output:
  • TTY    → colored summary
  • Piped  → TSV: KEY<tab>STATUS<tab>SUMMARY
  • --json → structured JSON

Examples:
  jira epics                                 # list project epics
  jira epics --json                          # JSON for scripting
  jira ls -t Epic                            # alternative via ls filter
  jira ls --jql 'parent = PROJ-10'           # list children of an epic`,
	RunE: func(cmd *cobra.Command, args []string) error {
		jql := fmt.Sprintf("project = %s AND issuetype = Epic ORDER BY statusCategory ASC, updated DESC", cfg.Project)
		result, err := client.Search(jql, 50)
		if err != nil {
			return err
		}

		if epicsJSON {
			return printJSON(issuesToJSONEpics(result.Issues))
		}

		if len(result.Issues) == 0 {
			if isTTY() {
				fmt.Println(ui.Dimmed.Render("  No epics found."))
			}
			return nil
		}

		if !isTTY() {
			headers := []string{"KEY", "STATUS", "SUMMARY"}
			var rows [][]string
			for _, issue := range result.Issues {
				rows = append(rows, []string{issue.Key, issue.Fields.Status.Name, issue.Fields.Summary})
			}
			printTSV(headers, rows)
			return nil
		}

		fmt.Println(ui.Title.Render(fmt.Sprintf(" %s epics", cfg.Project)))
		for _, issue := range result.Issues {
			key := ui.Key.Render(issue.Key)
			status := ui.StatusBadge(issue.Fields.Status.Name)
			fmt.Printf("  %s  %s  %s\n", key, status, issue.Fields.Summary)
		}
		return nil
	},
}

type epicJSONOut struct {
	Key     string `json:"key"`
	Status  string `json:"status"`
	Summary string `json:"summary"`
	URL     string `json:"url"`
}

func issuesToJSONEpics(issues []jira.Issue) []epicJSONOut {
	out := make([]epicJSONOut, len(issues))
	for i, issue := range issues {
		out[i] = epicJSONOut{
			Key:     issue.Key,
			Status:  issue.Fields.Status.Name,
			Summary: issue.Fields.Summary,
			URL:     client.BrowseURL(issue.Key),
		}
	}
	return out
}

func init() {
	epicsCmd.Flags().BoolVar(&epicsJSON, "json", false, "Output as JSON")
	rootCmd.AddCommand(epicsCmd)
}
