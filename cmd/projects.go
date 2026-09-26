package cmd

import (
	"fmt"

	"github.com/ngavilan-dogfy/jira-cli/ui"

	"github.com/spf13/cobra"
)

var projectsJSON bool

var projectsCmd = &cobra.Command{
	Use:   "projects",
	Short: "List accessible Jira projects",
	Long: `Show all Jira projects you have access to.
Useful for discovering project keys and switching projects with 'jira config set project <KEY>'.

Output:
  • TTY    → formatted list with current project highlighted
  • Piped  → TSV: KEY<tab>NAME
  • --json → [{"key":"PROJ","name":"Platform","id":"10001"}, ...]

Examples:
  jira projects                                  # list all projects
  jira projects --json                           # JSON for scripting
  jira projects --json | jq -r '.[].key'         # just project keys`,
	RunE: func(cmd *cobra.Command, args []string) error {
		projects, err := client.GetProjects()
		if err != nil {
			return fmt.Errorf("failed to list projects: %w", err)
		}

		if projectsJSON {
			type projOut struct {
				Key  string `json:"key"`
				Name string `json:"name"`
				ID   string `json:"id"`
			}
			out := make([]projOut, len(projects))
			for i, p := range projects {
				out[i] = projOut{Key: p.Key, Name: p.Name, ID: p.ID}
			}
			return printJSON(out)
		}

		if !isTTY() {
			headers := []string{"KEY", "NAME"}
			var rows [][]string
			for _, p := range projects {
				rows = append(rows, []string{p.Key, p.Name})
			}
			printTSV(headers, rows)
			return nil
		}

		if len(projects) == 0 {
			fmt.Println(ui.Dimmed.Render("  No projects found."))
			return nil
		}

		fmt.Println(ui.Title.Render(" Jira projects"))
		for _, p := range projects {
			marker := "  "
			if cfg != nil && p.Key == cfg.Project {
				marker = ui.SuccessStyle.Render("→ ")
			}
			key := ui.Key.Render(p.Key)
			fmt.Printf("  %s%s  %s\n", marker, key, p.Name)
		}
		return nil
	},
}

func init() {
	projectsCmd.Flags().BoolVar(&projectsJSON, "json", false, "Output as JSON")
	rootCmd.AddCommand(projectsCmd)
}
