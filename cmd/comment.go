package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ngavilan-dogfy/jira-cli/ui"

	"github.com/spf13/cobra"
)

var commentJSON bool

var commentCmd = &cobra.Command{
	Use:   "comment <issue-key> <message>",
	Short: "Add a comment to an issue",
	Long: `Add a comment to an issue, written in markdown — headings, lists, bold,
code and links become Jira's own formatting.

The message can be passed as arguments or piped via stdin.
When piped, only the issue key is required as an argument.

Output:
  • TTY    → success message
  • Piped  → just the issue key (for chaining)
  • --json → {"key":"PROJ-100","commented":true}

Examples:
  jira comment PROJ-100 "Deployed to staging"
  jira comment 100 "Fixed in latest commit"
  echo "Automated comment" | jira comment PROJ-100
  git log -1 --format='%s' | jira comment PROJ-100
  jira comment PROJ-100 "Done" --json`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		key := normalizeKey(args[0])

		var message string
		if len(args) >= 2 {
			message = strings.Join(args[1:], " ")
		} else {
			// Read from stdin
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				return fmt.Errorf("failed to read from stdin: %w", err)
			}
			message = strings.TrimSpace(string(data))
		}

		if message == "" {
			return fmt.Errorf("comment message cannot be empty")
		}

		if err := client.AddComment(key, message); err != nil {
			return fmt.Errorf("failed to add comment: %w", err)
		}

		if commentJSON {
			return printJSON(map[string]interface{}{
				"key":       key,
				"commented": true,
			})
		}

		if !isTTY() {
			fmt.Println(key)
			return nil
		}

		fmt.Println(ui.SuccessStyle.Render(fmt.Sprintf("  OK  Comment added to %s", key)))
		return nil
	},
}

func init() {
	commentCmd.Flags().BoolVar(&commentJSON, "json", false, "Output as JSON")
	rootCmd.AddCommand(commentCmd)
}
