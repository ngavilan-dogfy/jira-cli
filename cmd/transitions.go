package cmd

import (
	"fmt"

	"github.com/ngavilan-dogfy/jira-cli/ui"

	"github.com/spf13/cobra"
)

var transitionsJSON bool

var transitionsCmd = &cobra.Command{
	Use:   "transitions <issue-key>",
	Short: "List available transitions for an issue",
	Long: `Show what status transitions are available for a given issue.

This is a discovery command — it tells you what values you can pass
to 'jira move <key> <status>'.

Output:
  • TTY    → colored list with current status
  • Piped  → TSV: TRANSITION_NAME<tab>TARGET_STATUS<tab>TRANSITION_ID
  • --json → [{"name":"Start","to":"In Progress","id":"2"}, ...]

Examples:
  jira transitions PROJ-100                  # see available moves
  jira transitions 100                       # shorthand (auto-prefixes project)
  jira transitions PROJ-100 --json           # JSON for scripting
  jira transitions PROJ-100 --json | jq -r '.[].to'  # just target statuses`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		key := normalizeKey(args[0])

		issue, err := client.GetIssue(key)
		if err != nil {
			return fmt.Errorf("failed to get issue: %w", err)
		}

		transitions, err := client.GetTransitions(key)
		if err != nil {
			return fmt.Errorf("failed to get transitions: %w", err)
		}

		if transitionsJSON {
			type transOut struct {
				Name string `json:"name"`
				To   string `json:"to"`
				ID   string `json:"id"`
			}
			out := make([]transOut, len(transitions))
			for i, t := range transitions {
				out[i] = transOut{Name: t.Name, To: t.To.Name, ID: t.ID}
			}
			return printJSON(out)
		}

		if !isTTY() {
			headers := []string{"TRANSITION", "TARGET_STATUS", "ID"}
			var rows [][]string
			for _, t := range transitions {
				rows = append(rows, []string{t.Name, t.To.Name, t.ID})
			}
			printTSV(headers, rows)
			return nil
		}

		fmt.Printf("  %s  %s\n", ui.Key.Render(key), issue.Fields.Summary)
		fmt.Printf("  Current: %s\n\n", ui.StatusBadge(issue.Fields.Status.Name))

		if len(transitions) == 0 {
			fmt.Println(ui.Dimmed.Render("  No transitions available."))
			return nil
		}

		fmt.Println(ui.Subtitle.Render("  Available transitions:"))
		for _, t := range transitions {
			fmt.Printf("    → %-20s  (jira move %s \"%s\")\n", t.To.Name, key, t.To.Name)
		}
		return nil
	},
}

func init() {
	transitionsCmd.Flags().BoolVar(&transitionsJSON, "json", false, "Output as JSON")
	rootCmd.AddCommand(transitionsCmd)
}
