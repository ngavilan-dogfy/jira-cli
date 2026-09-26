package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/ngavilan-dogfy/jira-cli/jira"

	"github.com/spf13/cobra"
)

var (
	exportJQL      string
	exportLimit    int
	exportFormat   string
	exportComments bool
)

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Bulk-export issues (full detail) as JSONL or markdown",
	Long: `Export many issues at once with full detail — for offline analysis,
backups, or feeding a whole backlog/epic to an AI agent in one shot.

Paginates automatically past the API's 100-per-page limit.

Formats:
  jsonl (default)  One JSON object per line (same shape as 'jira show --json')
  md               Markdown document per issue, separated by '---'

Use --comments to also fetch each issue's comments (one extra API call
per issue — slower on large exports).

Examples:
  jira export -n 500 > backlog.jsonl                 # whole project
  jira export --jql 'parent = PROJ-10' --format md   # an epic, in markdown
  jira export --jql 'status = Done AND updated >= -30d' --comments
  jira export --format md | claude -p "summarize this backlog"`,
	RunE: func(cmd *cobra.Command, args []string) error {
		jql := exportJQL
		if jql == "" {
			jql = fmt.Sprintf("project = %s ORDER BY updated DESC", cfg.Project)
		}

		result, err := client.Search(jql, exportLimit)
		if err != nil {
			return err
		}
		if len(result.Issues) == 0 {
			return fmt.Errorf("no issues matched")
		}
		if !result.IsLast {
			fmt.Fprintf(os.Stderr, "note: more issues available than the -n %d limit\n", exportLimit)
		}

		for i := range result.Issues {
			issue := &result.Issues[i]

			var comments *jira.CommentsResult
			if exportComments {
				if c, err := client.GetComments(issue.Key, 100); err == nil {
					comments = c
				}
			}
			detail := buildIssueDetailJSON(issue, comments)

			switch exportFormat {
			case "jsonl":
				line, err := json.Marshal(detail)
				if err != nil {
					return err
				}
				fmt.Println(string(line))
			case "md", "markdown":
				fmt.Println(renderExportMarkdown(detail))
				if i < len(result.Issues)-1 {
					fmt.Println("---")
				}
			default:
				return fmt.Errorf("unknown format %q — supported: jsonl, md", exportFormat)
			}
		}
		return nil
	},
}

func renderExportMarkdown(d issueDetailJSON) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s — %s\n\n", d.Key, d.Summary)
	fmt.Fprintf(&b, "- **Status:** %s · **Type:** %s", d.Status, d.Type)
	if d.Priority != "" {
		fmt.Fprintf(&b, " · **Priority:** %s", d.Priority)
	}
	b.WriteString("\n")
	if d.Assignee != "" {
		fmt.Fprintf(&b, "- **Assignee:** %s\n", d.Assignee)
	}
	if d.Parent != "" {
		fmt.Fprintf(&b, "- **Parent:** %s\n", d.Parent)
	}
	if len(d.Labels) > 0 {
		fmt.Fprintf(&b, "- **Labels:** %s\n", strings.Join(d.Labels, ", "))
	}
	if d.DueDate != "" {
		fmt.Fprintf(&b, "- **Due:** %s\n", d.DueDate)
	}
	fmt.Fprintf(&b, "- **Updated:** %s · **URL:** %s\n", d.Updated, d.URL)

	if desc := strings.TrimSpace(d.Description); desc != "" {
		fmt.Fprintf(&b, "\n%s\n", desc)
	}
	if len(d.Links) > 0 {
		b.WriteString("\n**Links:**\n")
		for _, l := range d.Links {
			fmt.Fprintf(&b, "- %s %s — %s (%s)\n", l.Relation, l.Key, l.Summary, l.Status)
		}
	}
	if len(d.Subtasks) > 0 {
		b.WriteString("\n**Subtasks:**\n")
		for _, s := range d.Subtasks {
			fmt.Fprintf(&b, "- %s [%s] %s\n", s.Key, s.Status, s.Summary)
		}
	}
	if len(d.Comments) > 0 {
		fmt.Fprintf(&b, "\n**Comments (%d):**\n", len(d.Comments))
		for _, c := range d.Comments {
			fmt.Fprintf(&b, "- %s (%s): %s\n", c.Author, c.Created, strings.TrimSpace(c.Body))
		}
	}
	return b.String()
}

func init() {
	exportCmd.Flags().StringVar(&exportJQL, "jql", "", "JQL query (default: whole project, most recently updated first)")
	exportCmd.Flags().IntVarP(&exportLimit, "limit", "n", 200, "Maximum issues to export")
	exportCmd.Flags().StringVar(&exportFormat, "format", "jsonl", "Output format: jsonl or md")
	exportCmd.Flags().BoolVar(&exportComments, "comments", false, "Include comments (one extra API call per issue)")
	rootCmd.AddCommand(exportCmd)
}
