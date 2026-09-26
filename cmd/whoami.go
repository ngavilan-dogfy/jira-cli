package cmd

import (
	"fmt"

	"github.com/ngavilan-dogfy/jira-cli/ui"

	"github.com/spf13/cobra"
)

var whoamiJSON bool

var whoamiCmd = &cobra.Command{
	Use:   "whoami",
	Short: "Show current authenticated user",
	Long: `Show the authenticated user's display name, email, account ID,
site URL, and active project.

Output:
  • TTY    → formatted display
  • Piped  → account ID only (for use with jira assign)
  • --json → full user object

Examples:
  jira whoami                                    # who am I?
  jira whoami --json                             # JSON for scripting
  jira whoami --json | jq -r '.accountId'        # just account ID
  jira assign PROJ-100 $(jira whoami)            # assign to self (piped)`,
	RunE: func(cmd *cobra.Command, args []string) error {
		myself, err := client.GetMyself()
		if err != nil {
			return fmt.Errorf("failed to get user info: %w", err)
		}

		if whoamiJSON {
			return printJSON(map[string]string{
				"displayName": myself.DisplayName,
				"email":       myself.EmailAddress,
				"accountId":   myself.AccountID,
				"site":        cfg.BrowseBaseURL(),
				"project":     cfg.Project,
			})
		}

		if !isTTY() {
			fmt.Println(myself.AccountID)
			return nil
		}

		fmt.Println()
		fmt.Println(ui.Key.Render("  " + myself.DisplayName))
		fmt.Println(ui.Dimmed.Render("  " + myself.EmailAddress))
		fmt.Println(ui.Dimmed.Render("  " + myself.AccountID))
		fmt.Println(ui.Dimmed.Render("  " + cfg.BrowseBaseURL()))
		fmt.Println(ui.Dimmed.Render("  Project: " + cfg.Project))
		fmt.Println()

		return nil
	},
}

func init() {
	whoamiCmd.Flags().BoolVar(&whoamiJSON, "json", false, "Output as JSON")
	rootCmd.AddCommand(whoamiCmd)
}
