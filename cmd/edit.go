package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ngavilan-dogfy/jira-cli/ui"

	"github.com/spf13/cobra"
)

var (
	editSummary     string
	editPriority    string
	editLabels      string
	editType        string
	editDescription string
	editDue         string
	editFields      []string
	editJSON        bool
)

var editCmd = &cobra.Command{
	Use:   "edit <issue-key>",
	Short: "Edit issue fields inline",
	Long: `Update one or more fields of an existing issue without leaving the terminal.

Supported fields:
  --summary       Change the issue title/summary
  --description   Replace the description (supports markdown)
  --priority      Change priority (Highest, High, Medium, Low, Lowest)
  --labels        Replace all labels (comma-separated)
  --type          Change issue type (Task, Bug, Story, etc.)
  --field         Set ANY field by ID, including custom fields (repeatable).
                  Value is parsed as JSON if valid, else used as a string.
                  Discover field IDs with 'jira fields'.

Output:
  • TTY    → success message
  • Piped  → just the issue key
  • --json → {"key":"PROJ-100","updated":true}

Examples:
  jira edit PROJ-100 --summary "New title"
  jira edit PROJ-100 --description "## Context\nNew description with **markdown**"
  jira edit PROJ-100 --priority High
  jira edit PROJ-100 --labels "backend,urgent"
  jira edit PROJ-100 --summary "Fix it" --priority High --json
  jira edit 100 --type Bug
  jira edit PROJ-100 --field customfield_10016=5           # story points
  jira edit PROJ-100 --field 'customfield_10020={"value":"Platform"}'`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		key := normalizeKey(args[0])

		fields := map[string]interface{}{}

		if editSummary != "" {
			fields["summary"] = editSummary
		}
		if editPriority != "" {
			fields["priority"] = map[string]string{"name": editPriority}
		}
		if editLabels != "" {
			labels := strings.Split(editLabels, ",")
			for i := range labels {
				labels[i] = strings.TrimSpace(labels[i])
			}
			fields["labels"] = labels
		}
		if editType != "" {
			fields["issuetype"] = map[string]string{"name": editType}
		}
		if editDescription != "" {
			adf, err := client.MarkdownToADF(editDescription)
			if err != nil {
				return err
			}
			fields["description"] = adf
		}
		if editDue != "" {
			fields["duedate"] = editDue
		}
		for _, kv := range editFields {
			id, raw, ok := strings.Cut(kv, "=")
			if !ok || strings.TrimSpace(id) == "" {
				return fmt.Errorf("invalid --field %q — expected id=value (e.g. customfield_10016=5)", kv)
			}
			// JSON values pass through typed (numbers, objects, arrays,
			// booleans, null); anything else is a plain string.
			var val interface{}
			if err := json.Unmarshal([]byte(raw), &val); err != nil {
				val = raw
			}
			fields[strings.TrimSpace(id)] = val
		}

		if len(fields) == 0 {
			return fmt.Errorf("no fields to update — use --summary, --description, --priority, --labels, --type, --due, or --field")
		}

		if err := client.EditIssue(key, fields); err != nil {
			return fmt.Errorf("failed to update issue: %w", err)
		}

		if editJSON {
			return printJSON(map[string]interface{}{
				"key":     key,
				"updated": true,
			})
		}

		if !isTTY() {
			fmt.Println(key)
			return nil
		}

		fmt.Println(ui.SuccessStyle.Render(fmt.Sprintf("  OK  %s updated", key)))
		return nil
	},
}

func init() {
	editCmd.Flags().StringVar(&editSummary, "summary", "", "New summary/title")
	editCmd.Flags().StringVar(&editDescription, "description", "", "Replace description (supports markdown)")
	editCmd.Flags().StringVar(&editPriority, "priority", "", "New priority (Highest, High, Medium, Low, Lowest)")
	editCmd.Flags().StringVar(&editLabels, "labels", "", "Replace labels (comma-separated)")
	editCmd.Flags().StringVar(&editType, "type", "", "Change issue type")
	editCmd.Flags().StringVar(&editDue, "due", "", "Due date (YYYY-MM-DD format)")
	editCmd.Flags().StringArrayVar(&editFields, "field", nil, "Set any field by ID: id=value (repeatable, value parsed as JSON when valid)")
	editCmd.Flags().BoolVar(&editJSON, "json", false, "Output as JSON")
	rootCmd.AddCommand(editCmd)
}
