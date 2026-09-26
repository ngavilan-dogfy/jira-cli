package cmd

import (
	"fmt"

	"github.com/ngavilan-dogfy/jira-cli/ui"

	"github.com/spf13/cobra"
)

var (
	linkType string
	linkJSON bool
)

var linkCmd = &cobra.Command{
	Use:   "link <issue-key-1> <issue-key-2>",
	Short: "Link two issues together",
	Long: `Create a link between two issues.

Default link type is "Relates". Use --type to specify another
(e.g., "Blocks", "Cloners", "Duplicate").

Use "jira link-types" to see all available link types.

Output:
  • TTY    → success message
  • Piped  → just the first issue key
  • --json → {"from":"PROJ-100","to":"OPS-200","type":"Relates","linked":true}

Examples:
  jira link PROJ-149 OPS-2965                  # relates (default)
  jira link PROJ-100 PROJ-200 --type Blocks    # blocks
  jira link 100 200                            # shorthand keys`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		key1 := normalizeKey(args[0])
		key2 := normalizeKey(args[1])

		if err := client.LinkIssues(key1, key2, linkType); err != nil {
			return fmt.Errorf("failed to link issues: %w", err)
		}

		if linkJSON {
			return printJSON(map[string]interface{}{
				"from":   key1,
				"to":     key2,
				"type":   linkType,
				"linked": true,
			})
		}

		if !isTTY() {
			fmt.Println(key1)
			return nil
		}

		fmt.Println(ui.SuccessStyle.Render(fmt.Sprintf("  OK  %s → %s (%s)", key1, key2, linkType)))
		return nil
	},
}

var linkTypesCmd = &cobra.Command{
	Use:   "link-types",
	Short: "List available issue link types",
	Long: `Show all link types available in the Jira instance.

Each link type has an inward and outward description:
  e.g., "Blocks" → outward: "blocks", inward: "is blocked by"

Output:
  • TTY    → formatted table
  • Piped  → TSV: NAME, INWARD, OUTWARD
  • --json → array of link type objects

Examples:
  jira link-types
  jira link-types --json`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		types, err := client.GetLinkTypes()
		if err != nil {
			return fmt.Errorf("failed to get link types: %w", err)
		}

		if linkTypesJSON {
			return printJSON(types)
		}

		headers := []string{"NAME", "INWARD", "OUTWARD"}
		var rows [][]string
		for _, t := range types {
			rows = append(rows, []string{t.Name, t.Inward, t.Outward})
		}
		printTSV(headers, rows)
		return nil
	},
}

var linkTypesJSON bool

func init() {
	linkCmd.Flags().StringVar(&linkType, "type", "Relates", "Link type name (use 'jira link-types' to list)")
	linkCmd.Flags().BoolVar(&linkJSON, "json", false, "Output as JSON")
	rootCmd.AddCommand(linkCmd)

	linkTypesCmd.Flags().BoolVar(&linkTypesJSON, "json", false, "Output as JSON")
	rootCmd.AddCommand(linkTypesCmd)
}
