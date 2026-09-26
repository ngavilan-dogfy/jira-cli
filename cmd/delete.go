package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/ngavilan-dogfy/jira-cli/ui"

	"github.com/spf13/cobra"
)

var (
	deleteConfirm bool
	deleteJSON    bool
)

var deleteCmd = &cobra.Command{
	Use:   "delete <issue-key>",
	Short: "Delete an issue (requires confirmation)",
	Long: `Permanently delete a Jira issue. This action CANNOT be undone.

Safety:
  • By default, prompts you to type the issue key to confirm
  • Use --confirm to skip the prompt (for scripting — use with caution)
  • The issue summary is shown before confirmation so you can verify

Output:
  • TTY    → confirmation prompt, then success message
  • Piped  → requires --confirm flag (won't prompt in non-TTY)
  • --json → {"key":"PROJ-100","deleted":true}

Examples:
  jira delete PROJ-100                           # interactive confirmation
  jira delete PROJ-100 --confirm                 # skip prompt (scripting)
  jira delete PROJ-100 --confirm --json          # JSON output`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		key := normalizeKey(args[0])

		// Fetch the issue first to show what will be deleted
		issue, err := client.GetIssue(key)
		if err != nil {
			return fmt.Errorf("failed to get issue: %w", err)
		}

		if !deleteConfirm {
			if !isTTY() {
				return fmt.Errorf("refusing to delete without confirmation in non-interactive mode — use --confirm")
			}

			// Show what will be deleted
			fmt.Println()
			fmt.Println(ui.ErrorStyle.Render("  ⚠ DELETE ISSUE"))
			fmt.Println()
			fmt.Printf("  %s  %s\n", ui.Key.Render(issue.Key), issue.Fields.Summary)
			fmt.Printf("  Status: %s   Type: %s\n",
				ui.StatusBadge(issue.Fields.Status.Name),
				issue.Fields.IssueType.Name)
			fmt.Println()
			fmt.Println(ui.ErrorStyle.Render("  This action CANNOT be undone."))
			fmt.Println()
			fmt.Printf("  Type %s to confirm: ", ui.Key.Render(issue.Key))

			reader := bufio.NewReader(os.Stdin)
			input, _ := reader.ReadString('\n')
			input = strings.TrimSpace(input)

			if strings.ToUpper(input) != strings.ToUpper(issue.Key) {
				fmt.Println(ui.Dimmed.Render("  Cancelled."))
				return nil
			}
		}

		if err := client.DeleteIssue(key); err != nil {
			return fmt.Errorf("failed to delete issue: %w", err)
		}

		if deleteJSON {
			return printJSON(map[string]interface{}{
				"key":     key,
				"deleted": true,
			})
		}

		if !isTTY() {
			fmt.Println(key)
			return nil
		}

		fmt.Println(ui.SuccessStyle.Render(fmt.Sprintf("  OK  %s deleted", key)))
		return nil
	},
}

func init() {
	deleteCmd.Flags().BoolVar(&deleteConfirm, "confirm", false, "Skip confirmation prompt (use with caution)")
	deleteCmd.Flags().BoolVar(&deleteJSON, "json", false, "Output as JSON")
	rootCmd.AddCommand(deleteCmd)
}
