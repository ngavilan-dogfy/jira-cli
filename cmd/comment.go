package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ngavilan-dogfy/jira-cli/ui"

	"github.com/spf13/cobra"
)

var (
	commentJSON   bool
	commentDryRun bool
)

var commentCmd = &cobra.Command{
	Use:   "comment <issue-key> <message>",
	Short: "Add a comment to an issue",
	Long: `Add a comment to an issue, written in markdown — headings, lists, bold,
code and links become Jira's own formatting. A bare URL becomes a link.

Mention people with @[Name]: the name is looked up and they are notified.
It must match exactly one person, or the comment is not posted and the
candidates are listed. @[Name](accountId) skips the lookup.

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
  jira comment PROJ-100 "@[Ada Lovelace] can you review [the PR](https://example.com/pr/7)?"
  jira comment PROJ-100 "Done" --json
  jira comment PROJ-100 --dry-run < note.md   # print what would be sent; post nothing`,
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

		if commentDryRun {
			// Resolves mentions (read-only) and prints the document Jira
			// would receive, so formatting can be checked before posting.
			adf, err := client.MarkdownToADF(message)
			if err != nil {
				return err
			}
			return printJSON(map[string]interface{}{"key": key, "commented": false, "body": adf})
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
	commentCmd.Flags().BoolVar(&commentDryRun, "dry-run", false, "Print the document that would be sent, as JSON, and post nothing")
	rootCmd.AddCommand(commentCmd)
}
