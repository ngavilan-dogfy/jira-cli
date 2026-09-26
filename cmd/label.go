package cmd

import (
	"fmt"

	"github.com/ngavilan-dogfy/jira-cli/ui"

	"github.com/spf13/cobra"
)

var labelJSON bool

var labelAddCmd = &cobra.Command{
	Use:   "label-add <issue-key> <label>...",
	Short: "Add labels to an issue",
	Long: `Add one or more labels to an issue without removing existing ones.

Output:
  • TTY    → success message
  • Piped  → issue key
  • --json → {"key":"PROJ-100","labels":["added1","added2"]}

Examples:
  jira label-add PROJ-100 backend
  jira label-add PROJ-100 urgent backend
  jira label-add 100 deploy --json`,
	Args: cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		key := normalizeKey(args[0])
		labels := args[1:]

		issue, err := client.GetIssue(key)
		if err != nil {
			return fmt.Errorf("failed to get issue: %w", err)
		}

		merged := issue.Fields.Labels
		for _, l := range labels {
			if !contains(merged, l) {
				merged = append(merged, l)
			}
		}

		if err := client.EditIssue(key, map[string]interface{}{"labels": merged}); err != nil {
			return fmt.Errorf("failed to update labels: %w", err)
		}

		if labelJSON {
			return printJSON(map[string]interface{}{
				"key":    key,
				"labels": merged,
			})
		}

		if !isTTY() {
			fmt.Println(key)
			return nil
		}

		fmt.Println(ui.SuccessStyle.Render(fmt.Sprintf("  OK  Labels updated on %s", key)))
		return nil
	},
}

var labelRemoveCmd = &cobra.Command{
	Use:   "label-rm <issue-key> <label>...",
	Short: "Remove labels from an issue",
	Long: `Remove one or more labels from an issue.

Examples:
  jira label-rm PROJ-100 urgent
  jira label-rm PROJ-100 backend urgent --json`,
	Args: cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		key := normalizeKey(args[0])
		toRemove := args[1:]

		issue, err := client.GetIssue(key)
		if err != nil {
			return fmt.Errorf("failed to get issue: %w", err)
		}

		var remaining []string
		for _, l := range issue.Fields.Labels {
			if !contains(toRemove, l) {
				remaining = append(remaining, l)
			}
		}

		if err := client.EditIssue(key, map[string]interface{}{"labels": remaining}); err != nil {
			return fmt.Errorf("failed to update labels: %w", err)
		}

		if labelJSON {
			return printJSON(map[string]interface{}{
				"key":    key,
				"labels": remaining,
			})
		}

		if !isTTY() {
			fmt.Println(key)
			return nil
		}

		fmt.Println(ui.SuccessStyle.Render(fmt.Sprintf("  OK  Labels updated on %s", key)))
		return nil
	},
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func init() {
	labelAddCmd.Flags().BoolVar(&labelJSON, "json", false, "Output as JSON")
	labelRemoveCmd.Flags().BoolVar(&labelJSON, "json", false, "Output as JSON")
	rootCmd.AddCommand(labelAddCmd)
	rootCmd.AddCommand(labelRemoveCmd)
}
