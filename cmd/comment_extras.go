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
	commentEditJSON bool
	commentRmJSON   bool
)

var commentEditCmd = &cobra.Command{
	Use:   "comment-edit <issue-key> <comment-id> [message]",
	Short: "Edit an existing comment",
	Long: `Replace the body of an existing comment.

The new message can be passed as the third argument or piped via stdin.
Find the comment ID with: jira show <key> --json | jq '.comments[].id'

Examples:
  jira comment-edit PROJ-251 12345 "Updated context"
  echo "New body" | jira comment-edit PROJ-251 12345`,
	Args: cobra.RangeArgs(2, 999),
	RunE: func(cmd *cobra.Command, args []string) error {
		key := normalizeKey(args[0])
		commentID := args[1]

		var message string
		if len(args) >= 3 {
			message = strings.Join(args[2:], " ")
		} else {
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				return fmt.Errorf("failed to read from stdin: %w", err)
			}
			message = strings.TrimSpace(string(data))
		}
		if message == "" {
			return fmt.Errorf("comment message cannot be empty")
		}

		if err := client.EditComment(key, commentID, message); err != nil {
			return fmt.Errorf("failed to edit comment: %w", err)
		}
		if commentEditJSON {
			return printJSON(map[string]interface{}{"key": key, "commentId": commentID, "edited": true})
		}
		if !isTTY() {
			fmt.Println(key)
			return nil
		}
		fmt.Println(ui.SuccessStyle.Render(fmt.Sprintf("  OK  Comment %s on %s edited", commentID, key)))
		return nil
	},
}

var commentRmCmd = &cobra.Command{
	Use:     "comment-rm <issue-key> <comment-id>",
	Aliases: []string{"comment-delete"},
	Short:   "Delete a comment",
	Long: `Delete a comment by ID. Find IDs with: jira show <key> --json | jq '.comments[].id'

Examples:
  jira comment-rm PROJ-251 12345`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		key := normalizeKey(args[0])
		commentID := args[1]
		if err := client.DeleteComment(key, commentID); err != nil {
			return fmt.Errorf("failed to delete comment: %w", err)
		}
		if commentRmJSON {
			return printJSON(map[string]interface{}{"key": key, "commentId": commentID, "deleted": true})
		}
		if !isTTY() {
			fmt.Println(key)
			return nil
		}
		fmt.Println(ui.SuccessStyle.Render(fmt.Sprintf("  OK  Comment %s on %s deleted", commentID, key)))
		return nil
	},
}

func init() {
	commentEditCmd.Flags().BoolVar(&commentEditJSON, "json", false, "Output as JSON")
	commentRmCmd.Flags().BoolVar(&commentRmJSON, "json", false, "Output as JSON")
	rootCmd.AddCommand(commentEditCmd)
	rootCmd.AddCommand(commentRmCmd)
}
