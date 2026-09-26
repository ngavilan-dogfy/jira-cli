package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ngavilan-dogfy/jira-cli/ui"

	"github.com/spf13/cobra"
)

var (
	fieldsJSON   bool
	fieldsCustom bool
	fieldsQuery  string
)

var fieldsCmd = &cobra.Command{
	Use:   "fields",
	Short: "List all Jira fields (system + custom) with their IDs",
	Long: `List every field in the Jira instance, including custom fields.

Use this to discover custom field IDs (story points, team, sprint...)
which can then be set with 'jira edit KEY --field customfield_XXXXX=value'.

Output:
  • TTY    → readable list
  • Piped  → TSV: ID<tab>NAME<tab>TYPE<tab>CUSTOM
  • --json → array of {id, name, custom, type}

Examples:
  jira fields                        # all fields
  jira fields --custom               # only custom fields
  jira fields -q "story"             # filter by name (case-insensitive)
  jira fields -q points --json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fields, err := client.GetFields()
		if err != nil {
			return fmt.Errorf("failed to get fields: %w", err)
		}

		query := strings.ToLower(strings.TrimSpace(fieldsQuery))
		var filtered []struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Custom bool   `json:"custom"`
			Type   string `json:"type,omitempty"`
		}
		for _, f := range fields {
			if fieldsCustom && !f.Custom {
				continue
			}
			if query != "" && !strings.Contains(strings.ToLower(f.Name), query) && !strings.Contains(strings.ToLower(f.ID), query) {
				continue
			}
			filtered = append(filtered, struct {
				ID     string `json:"id"`
				Name   string `json:"name"`
				Custom bool   `json:"custom"`
				Type   string `json:"type,omitempty"`
			}{f.ID, f.Name, f.Custom, f.Schema.Type})
		}
		sort.Slice(filtered, func(i, j int) bool { return filtered[i].Name < filtered[j].Name })

		if fieldsJSON {
			return printJSON(filtered)
		}

		if !isTTY() {
			for _, f := range filtered {
				fmt.Printf("%s\t%s\t%s\t%t\n", f.ID, f.Name, f.Type, f.Custom)
			}
			return nil
		}

		fmt.Println(ui.Title.Render(fmt.Sprintf(" fields (%d)", len(filtered))))
		for _, f := range filtered {
			tag := ""
			if f.Custom {
				tag = " · custom"
			}
			fmt.Printf("  %s  %s (%s%s)\n", ui.Key.Render(f.ID), f.Name, f.Type, tag)
		}
		return nil
	},
}

func init() {
	fieldsCmd.Flags().BoolVar(&fieldsJSON, "json", false, "Output as JSON")
	fieldsCmd.Flags().BoolVar(&fieldsCustom, "custom", false, "Only custom fields")
	fieldsCmd.Flags().StringVarP(&fieldsQuery, "query", "q", "", "Filter by name or ID substring")
	rootCmd.AddCommand(fieldsCmd)
}
