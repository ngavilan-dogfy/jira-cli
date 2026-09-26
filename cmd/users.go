package cmd

import (
	"fmt"

	"github.com/ngavilan-dogfy/jira-cli/ui"

	"github.com/spf13/cobra"
)

var usersJSON bool

var usersCmd = &cobra.Command{
	Use:     "users",
	Aliases: []string{"team", "members"},
	Short:   "List assignable users in the project",
	Long: `List all users that can be assigned issues in the current project.
Shows display name and account ID (needed for 'jira assign').

Output:
  • TTY    → formatted name list
  • Piped  → TSV: ACCOUNT_ID<tab>DISPLAY_NAME
  • --json → [{"accountId":"...","displayName":"..."}]

Examples:
  jira users                                     # list team members
  jira users --json                              # JSON for scripting
  jira users --json | jq -r '.[] | .displayName' # just names
  jira users | grep "Ana"                        # find a user
  jira assign PROJ-100 $(jira users | grep Ana | awk '{print $1}')`,
	RunE: func(cmd *cobra.Command, args []string) error {
		users, err := client.SearchUsers(cfg.Project)
		if err != nil {
			return fmt.Errorf("failed to list users: %w", err)
		}

		if usersJSON {
			type userOut struct {
				AccountID   string `json:"accountId"`
				DisplayName string `json:"displayName"`
			}
			out := make([]userOut, len(users))
			for i, u := range users {
				out[i] = userOut{AccountID: u.AccountID, DisplayName: u.DisplayName}
			}
			return printJSON(out)
		}

		if !isTTY() {
			headers := []string{"ACCOUNT_ID", "DISPLAY_NAME"}
			var rows [][]string
			for _, u := range users {
				rows = append(rows, []string{u.AccountID, u.DisplayName})
			}
			printTSV(headers, rows)
			return nil
		}

		if len(users) == 0 {
			fmt.Println(ui.Dimmed.Render("  No assignable users found."))
			return nil
		}

		fmt.Println(ui.Title.Render(fmt.Sprintf(" %s team (%d)", cfg.Project, len(users))))
		for _, u := range users {
			name := ui.Key.Render(u.DisplayName)
			id := ui.Dimmed.Render(u.AccountID)
			fmt.Printf("  %s  %s\n", name, id)
		}
		return nil
	},
}

func init() {
	usersCmd.Flags().BoolVar(&usersJSON, "json", false, "Output as JSON")
	rootCmd.AddCommand(usersCmd)
}
