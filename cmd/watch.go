package cmd

import (
	"fmt"

	"github.com/ngavilan-dogfy/jira-cli/ui"

	"github.com/spf13/cobra"
)

var (
	watchAccount string
	watchJSON    bool
)

var watchCmd = &cobra.Command{
	Use:   "watch [<issue-key>]",
	Short: "Subscribe to issue notifications",
	Long: `Add yourself (or another user) as a watcher of an issue.

When the key is omitted and fzf is installed, an interactive picker opens.

Examples:
  jira watch PROJ-251
  jira watch 251
  jira watch PROJ-251 --account 5d0c2...  # add someone else as watcher
  jira watch                              # fzf picker`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		key, err := resolveKeyOrPick(args, "")
		if err != nil {
			return err
		}
		if err := client.AddWatcher(key, watchAccount); err != nil {
			return fmt.Errorf("failed to add watcher: %w", err)
		}
		if watchJSON {
			return printJSON(map[string]interface{}{"key": key, "watching": true})
		}
		if !isTTY() {
			fmt.Println(key)
			return nil
		}
		who := "you"
		if watchAccount != "" {
			who = watchAccount
		}
		fmt.Println(ui.SuccessStyle.Render(fmt.Sprintf("  OK  %s now watching %s", who, key)))
		return nil
	},
}

var unwatchCmd = &cobra.Command{
	Use:   "unwatch [<issue-key>]",
	Short: "Unsubscribe from issue notifications",
	Long: `Remove yourself (or another user) from the watchers of an issue.

Examples:
  jira unwatch PROJ-251
  jira unwatch PROJ-251 --account 5d0c2...
  jira unwatch                              # fzf picker`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		key, err := resolveKeyOrPick(args, "")
		if err != nil {
			return err
		}
		acc := watchAccount
		if acc == "" {
			me, err := client.GetMyself()
			if err != nil {
				return fmt.Errorf("failed to identify current user: %w", err)
			}
			acc = me.AccountID
		}
		if err := client.RemoveWatcher(key, acc); err != nil {
			return fmt.Errorf("failed to remove watcher: %w", err)
		}
		if watchJSON {
			return printJSON(map[string]interface{}{"key": key, "watching": false})
		}
		if !isTTY() {
			fmt.Println(key)
			return nil
		}
		fmt.Println(ui.SuccessStyle.Render(fmt.Sprintf("  OK  Stopped watching %s", key)))
		return nil
	},
}

func init() {
	watchCmd.Flags().StringVar(&watchAccount, "account", "", "Account ID to add as watcher (defaults to current user)")
	watchCmd.Flags().BoolVar(&watchJSON, "json", false, "Output as JSON")
	unwatchCmd.Flags().StringVar(&watchAccount, "account", "", "Account ID to remove (defaults to current user)")
	unwatchCmd.Flags().BoolVar(&watchJSON, "json", false, "Output as JSON")
	rootCmd.AddCommand(watchCmd)
	rootCmd.AddCommand(unwatchCmd)
}
