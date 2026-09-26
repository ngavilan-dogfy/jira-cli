package cmd

import (
	"fmt"

	"github.com/ngavilan-dogfy/jira-cli/ui"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var prioritiesJSON bool

var prioritiesCmd = &cobra.Command{
	Use:   "priorities",
	Short: "List available priorities",
	Long: `Show all priority levels available in your Jira instance.
Useful for knowing valid values for 'jira edit --priority' and 'jira ls' filtering.

Output:
  • TTY    → colored list
  • Piped  → TSV: NAME<tab>ID
  • --json → [{"name":"High","id":"2"}, ...]

Examples:
  jira priorities                                # see available priorities
  jira priorities --json                         # JSON for scripting
  jira priorities --json | jq -r '.[].name'      # just priority names`,
	RunE: func(cmd *cobra.Command, args []string) error {
		priorities, err := client.GetPriorities()
		if err != nil {
			return fmt.Errorf("failed to list priorities: %w", err)
		}

		if prioritiesJSON {
			type prioOut struct {
				Name string `json:"name"`
				ID   string `json:"id"`
			}
			out := make([]prioOut, len(priorities))
			for i, p := range priorities {
				out[i] = prioOut{Name: p.Name, ID: p.ID}
			}
			return printJSON(out)
		}

		if !isTTY() {
			headers := []string{"NAME", "ID"}
			var rows [][]string
			for _, p := range priorities {
				rows = append(rows, []string{p.Name, p.ID})
			}
			printTSV(headers, rows)
			return nil
		}

		fmt.Println(ui.Title.Render(" Priorities"))
		for _, p := range priorities {
			clr := ui.PriorityColor(p.Name)
			fmt.Printf("  %s\n", lipgloss.NewStyle().Foreground(clr).Bold(true).Render(p.Name))
		}
		return nil
	},
}

func init() {
	prioritiesCmd.Flags().BoolVar(&prioritiesJSON, "json", false, "Output as JSON")
	rootCmd.AddCommand(prioritiesCmd)
}
