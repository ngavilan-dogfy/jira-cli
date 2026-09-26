package cmd

import (
	"fmt"

	"github.com/ngavilan-dogfy/jira-cli/ui"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var statusesJSON bool

var statusesCmd = &cobra.Command{
	Use:   "statuses",
	Short: "List available statuses in the project",
	Long: `Show all statuses used in the current project with issue counts.

Useful for discovering valid --status filter values for 'jira ls'.

Output:
  • TTY    → colored table
  • Piped  → TSV: NAME<tab>COUNT
  • --json → [{"name":"Done","count":17}, ...]

Examples:
  jira statuses                              # see available statuses
  jira statuses --json                       # JSON for scripting
  jira statuses --json | jq -r '.[].name'    # just status names`,
	RunE: func(cmd *cobra.Command, args []string) error {
		jql := fmt.Sprintf("project = %s", cfg.Project)
		result, err := client.Search(jql, 100)
		if err != nil {
			return err
		}

		counts := map[string]int{}
		order := []string{}
		for _, issue := range result.Issues {
			s := issue.Fields.Status.Name
			if counts[s] == 0 {
				order = append(order, s)
			}
			counts[s]++
		}

		if statusesJSON {
			type statusOut struct {
				Name  string `json:"name"`
				Count int    `json:"count"`
			}
			out := make([]statusOut, len(order))
			for i, name := range order {
				out[i] = statusOut{Name: name, Count: counts[name]}
			}
			return printJSON(out)
		}

		if !isTTY() {
			headers := []string{"STATUS", "COUNT"}
			var rows [][]string
			for _, name := range order {
				rows = append(rows, []string{name, fmt.Sprintf("%d", counts[name])})
			}
			printTSV(headers, rows)
			return nil
		}

		fmt.Println(ui.Title.Render(fmt.Sprintf(" %s statuses", cfg.Project)))
		for _, name := range order {
			line := fmt.Sprintf("  ● %-20s %d issues", name, counts[name])
			fmt.Println(lipgloss.NewStyle().Foreground(ui.StatusColor(name)).Render(line))
		}
		return nil
	},
}

func init() {
	statusesCmd.Flags().BoolVar(&statusesJSON, "json", false, "Output as JSON")
	rootCmd.AddCommand(statusesCmd)
}
