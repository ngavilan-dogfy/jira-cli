package cmd

import (
	"fmt"

	"github.com/ngavilan-dogfy/jira-cli/ui"

	"github.com/spf13/cobra"
)

var (
	assignMe   bool
	assignJSON bool
)

var assignCmd = &cobra.Command{
	Use:   "assign <issue-key> [account-id]",
	Short: "Assign an issue to a user",
	Long: `Assign an issue to a user by account ID, or use --me to assign to yourself.

Use 'jira users' to discover team member account IDs.

Output:
  • TTY    → success message
  • Piped  → issue key
  • --json → {"key":"PROJ-100","assigned":true}

Examples:
  jira assign PROJ-100 --me
  jira assign 100 --me --json
  jira assign PROJ-100 $(jira users --json | jq -r '.[] | select(.displayName | contains("Leo")) | .accountId')`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		key := normalizeKey(args[0])

		var accountID string
		if assignMe {
			myself, err := client.GetMyself()
			if err != nil {
				return fmt.Errorf("failed to get current user: %w", err)
			}
			accountID = myself.AccountID
		} else if len(args) == 2 {
			accountID = args[1]
		} else {
			return fmt.Errorf("provide an account ID or use --me")
		}

		if err := client.AssignIssue(key, accountID); err != nil {
			return fmt.Errorf("failed to assign: %w", err)
		}

		if assignJSON {
			return printJSON(map[string]interface{}{
				"key":      key,
				"assigned": true,
			})
		}
		if !isTTY() {
			fmt.Println(key)
			return nil
		}
		fmt.Println(ui.SuccessStyle.Render(fmt.Sprintf("  OK  %s assigned", key)))
		return nil
	},
}

var unassignCmd = &cobra.Command{
	Use:   "unassign <issue-key>",
	Short: "Unassign an issue",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		key := normalizeKey(args[0])

		if err := client.AssignIssue(key, ""); err != nil {
			return fmt.Errorf("failed to unassign: %w", err)
		}

		if assignJSON {
			return printJSON(map[string]interface{}{
				"key":        key,
				"unassigned": true,
			})
		}
		if !isTTY() {
			fmt.Println(key)
			return nil
		}
		fmt.Println(ui.SuccessStyle.Render(fmt.Sprintf("  OK  %s unassigned", key)))
		return nil
	},
}

func init() {
	assignCmd.Flags().BoolVar(&assignMe, "me", false, "Assign to yourself")
	assignCmd.Flags().BoolVar(&assignJSON, "json", false, "Output as JSON")
	unassignCmd.Flags().BoolVar(&assignJSON, "json", false, "Output as JSON")
	rootCmd.AddCommand(assignCmd)
	rootCmd.AddCommand(unassignCmd)
}
