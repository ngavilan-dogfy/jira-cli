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
	createType      string
	createParent    string
	createDesc      string
	createDue       string
	createJSON      bool
	createTemplate  string
	createLabels    string
	createPriority  string
	createAssignee  string
	createComponent string
)

var createCmd = &cobra.Command{
	Use:   "create <summary>",
	Short: "Create a new issue",
	Long: `Create a new issue in the current project.

Defaults to type "Task". Use -t to specify another type (Bug, Story, Epic, Sub-task).
Use --parent to create a child issue under an epic or parent issue.
Use --description or -d to set a description. Use "-d -" to read description from stdin.

Output:
  • TTY    → success message with key and URL
  • Piped  → just the issue key
  • --json → {"key":"PROJ-200","url":"..."}

Examples:
  jira create "Fix login timeout"                                 # create a Task
  jira create -t Bug "Login page crashes on Safari"               # create a Bug
  jira create -t Sub-task --parent PROJ-10 "Setup CI"             # sub-task
  jira create "My task" -d "Detailed description here"            # with description
  echo "Long description" | jira create "My task" -d -            # description from stdin
  jira create "My task" --assignee me                             # create + self-assign
  jira create "My task" --component api --labels backend          # with metadata
  jira create "My task" --json                                    # get key as JSON
  jira create "My task" --json | jq -r '.key'                     # just the key`,
	Args: cobra.MinimumNArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Effective values come from: template (defaults) ← CLI flags (overrides) ← positional summary
		tpl, err := loadTemplateIfRequested()
		if err != nil {
			return err
		}

		summary := ""
		if len(args) > 0 {
			summary = strings.Join(args, " ")
		} else if tpl != nil && tpl.Summary != "" {
			summary = tpl.Summary
		}
		if strings.TrimSpace(summary) == "" {
			return fmt.Errorf("a summary is required (positional arg, or `summary:` in the template frontmatter)")
		}

		// Type: CLI flag wins over template
		issueType := createType
		if !cmd.Flag("type").Changed && tpl != nil && tpl.Type != "" {
			issueType = tpl.Type
		}

		parentKey := ""
		if createParent != "" {
			parentKey = normalizeKey(createParent)
		}

		// Subtask validation: parent required.
		if isSubtaskType(issueType) && parentKey == "" {
			return fmt.Errorf("issue type %q requires --parent <issue-key>", issueType)
		}

		description := createDesc
		if description == "-" {
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				return fmt.Errorf("failed to read description from stdin: %w", err)
			}
			description = strings.TrimSpace(string(data))
		}
		// If no -d given, fall back to template body.
		if description == "" && tpl != nil {
			description = tpl.Body
		}

		issue, err := client.CreateIssue(cfg.Project, issueType, summary, parentKey, description, createDue)
		if err != nil {
			return fmt.Errorf("failed to create issue: %w", err)
		}

		// Post-creation: apply labels, priority, assignee, components
		// (fields the create endpoint doesn't take, or that come from templates)
		labels := pickLabels(createLabels, tpl)
		priority := pickPriority(createPriority, tpl)
		fields := map[string]interface{}{}
		if len(labels) > 0 {
			fields["labels"] = labels
		}
		if priority != "" {
			fields["priority"] = map[string]string{"name": priority}
		}
		if createComponent != "" {
			var comps []map[string]string
			for _, c := range strings.Split(createComponent, ",") {
				if c = strings.TrimSpace(c); c != "" {
					comps = append(comps, map[string]string{"name": c})
				}
			}
			fields["components"] = comps
		}
		if len(fields) > 0 {
			if err := client.EditIssue(issue.Key, fields); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: created %s but failed to apply labels/priority/components: %v\n", issue.Key, err)
			}
		}
		if createAssignee != "" {
			accountID := createAssignee
			if createAssignee == "me" {
				myself, err := client.GetMyself()
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: created %s but failed to resolve your account ID: %v\n", issue.Key, err)
					accountID = ""
				} else {
					accountID = myself.AccountID
				}
			}
			if accountID != "" {
				if err := client.AssignIssue(issue.Key, accountID); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: created %s but failed to assign: %v\n", issue.Key, err)
				}
			}
		}

		if createJSON {
			return printJSON(map[string]string{
				"key": issue.Key,
				"url": client.BrowseURL(issue.Key),
			})
		}

		if !isTTY() {
			fmt.Println(issue.Key)
			return nil
		}

		fmt.Println(ui.SuccessStyle.Render(fmt.Sprintf("  OK  Created %s", issue.Key)))
		fmt.Println(ui.Dimmed.Render("  " + client.BrowseURL(issue.Key)))
		fmt.Println()
		fmt.Println(ui.Dimmed.Render(fmt.Sprintf("  View:   jira show %s", issue.Key)))
		fmt.Println(ui.Dimmed.Render(fmt.Sprintf("  Move:   jira move %s \"In Progress\"", issue.Key)))
		fmt.Println(ui.Dimmed.Render(fmt.Sprintf("  Assign: jira assign %s --me", issue.Key)))
		return nil
	},
}

func init() {
	createCmd.Flags().StringVarP(&createType, "type", "t", "Task", "Issue type (Task, Bug, Story, Epic, Sub-task)")
	createCmd.Flags().StringVar(&createParent, "parent", "", "Parent issue key (for sub-tasks or epic children)")
	createCmd.Flags().StringVarP(&createDesc, "description", "d", "", "Issue description (use '-' to read from stdin)")
	createCmd.Flags().StringVar(&createDue, "due", "", "Due date (YYYY-MM-DD format)")
	createCmd.Flags().BoolVar(&createJSON, "json", false, "Output as JSON")
	createCmd.Flags().StringVar(&createTemplate, "template", "", "Template name (looks in ~/.config/jira-cli/templates/<name>.md)")
	createCmd.Flags().StringVar(&createLabels, "labels", "", "Comma-separated labels (overrides template labels)")
	createCmd.Flags().StringVar(&createPriority, "priority", "", "Priority (overrides template)")
	createCmd.Flags().StringVar(&createAssignee, "assignee", "", "Assign on creation: 'me' or an account ID")
	createCmd.Flags().StringVar(&createComponent, "component", "", "Comma-separated component names")
	rootCmd.AddCommand(createCmd)
}

func isSubtaskType(t string) bool {
	lt := strings.ToLower(t)
	return strings.Contains(lt, "subtask") || strings.Contains(lt, "sub-task") || strings.Contains(lt, "subtarea")
}

func pickLabels(cli string, tpl *issueTemplate) []string {
	if strings.TrimSpace(cli) != "" {
		var out []string
		for _, l := range strings.Split(cli, ",") {
			l = strings.TrimSpace(l)
			if l != "" {
				out = append(out, l)
			}
		}
		return out
	}
	if tpl != nil {
		return tpl.Labels
	}
	return nil
}

func pickPriority(cli string, tpl *issueTemplate) string {
	if strings.TrimSpace(cli) != "" {
		return cli
	}
	if tpl != nil {
		return tpl.Priority
	}
	return ""
}
