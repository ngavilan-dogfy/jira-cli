package cmd

import (
	"fmt"

	"github.com/ngavilan-dogfy/jira-cli/ui"

	"github.com/spf13/cobra"
)

var typesJSON bool

var typesCmd = &cobra.Command{
	Use:   "types",
	Short: "List available issue types in the project",
	Long: `Show all issue types you can use when creating issues with 'jira create -t'.

Output:
  • TTY    → formatted list
  • Piped  → TSV: NAME<tab>ID<tab>SUBTASK
  • --json → [{"name":"Task","id":"10001","subtask":false}, ...]

Examples:
  jira types                                     # see available types
  jira types --json                              # JSON for scripting
  jira types --json | jq -r '.[].name'           # just type names`,
	RunE: func(cmd *cobra.Command, args []string) error {
		types, err := client.GetIssueTypes(cfg.Project)
		if err != nil {
			return fmt.Errorf("failed to list issue types: %w", err)
		}

		if typesJSON {
			type typeOut struct {
				Name    string `json:"name"`
				ID      string `json:"id"`
				Subtask bool   `json:"subtask"`
			}
			out := make([]typeOut, len(types))
			for i, t := range types {
				out[i] = typeOut{Name: t.Name, ID: t.ID, Subtask: t.Subtask}
			}
			return printJSON(out)
		}

		if !isTTY() {
			headers := []string{"NAME", "ID", "SUBTASK"}
			var rows [][]string
			for _, t := range types {
				sub := "false"
				if t.Subtask {
					sub = "true"
				}
				rows = append(rows, []string{t.Name, t.ID, sub})
			}
			printTSV(headers, rows)
			return nil
		}

		if len(types) == 0 {
			fmt.Println(ui.Dimmed.Render("  No issue types found."))
			return nil
		}

		fmt.Println(ui.Title.Render(fmt.Sprintf(" %s issue types", cfg.Project)))
		for _, t := range types {
			icon := ui.TypeIcon(t.Name)
			name := t.Name
			extra := ""
			if t.Subtask {
				extra = ui.Dimmed.Render("  (subtask)")
			}
			fmt.Printf("  %s %s%s\n", icon, name, extra)
		}
		return nil
	},
}

func init() {
	typesCmd.Flags().BoolVar(&typesJSON, "json", false, "Output as JSON")
	rootCmd.AddCommand(typesCmd)
}
