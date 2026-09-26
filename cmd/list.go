package cmd

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ngavilan-dogfy/jira-cli/jira"
	"github.com/ngavilan-dogfy/jira-cli/ui"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/spf13/cobra"
)

var (
	listStatus   string
	listType     string
	listAll      bool
	listLimit    int
	listJSON     bool
	listPlain    bool
	listJQL      string
	listLabel    string
	listAssignee string
	listReporter string
	listParent   string
	listQuery    string
	listSince    string
	listProject  string
)

var listCmd = &cobra.Command{
	Use:     "ls",
	Aliases: []string{"list"},
	Short:   "List project issues with full metadata",
	Long: `List Jira issues. Shows KEY, TYPE, STATUS, PRIORITY, ASSIGNEE, PARENT, SUMMARY.

By default shows issues assigned to you, ordered by workflow status
(In Refinement → In Progress → Done).

Output adapts automatically:
  • TTY (terminal)  → colored table with borders
  • Piped (| or >)  → tab-separated plain text (TSV), easy to parse
  • --json          → structured JSON array
  • --plain         → force TSV even in terminal

Filtering:
  -s, --status    Filter by exact status name
  -t, --type      Filter by issue type (Task, Epic, Bug, Sub-task, Story)
  -a, --all       Show all project issues, not just yours
  --label         Filter by label (comma-separated = any of them)
  --assignee      Filter by assignee: me, none, or an account ID
  --reporter      Filter by reporter: me or an account ID
  --parent        Children of an epic/issue (e.g. --parent PROJ-10)
  --query         Free-text search in summary/description/comments
  --since         Updated since: 30m, 4h, 7d, 2w, or YYYY-MM-DD
  --jql           Raw JQL override (ignores all other filters)

Examples:
  jira ls                                    # my issues, colored table
  jira ls -a                                 # all project issues
  jira ls -s "In Progress"                   # filter by status
  jira ls -t Epic                            # only epics
  jira ls -a --query "timeout"               # free-text search
  jira ls -a --since 7d                      # updated in the last week
  jira ls --parent PROJ-10                   # children of an epic
  jira ls -a --assignee none                 # unassigned issues
  jira ls --json                             # JSON for scripting
  jira ls --json | jq '.[].key'              # extract keys with jq
  jira ls --plain | grep "In Progress"       # TSV + grep
  jira ls | awk -F'\t' '$3=="Done"'          # piped = TSV auto
  jira ls -s "In Refinement" --json | jq -r '.[].key' | xargs -I{} jira move {} "In Progress"
  jira ls --jql 'project=PROJ AND labels=urgent ORDER BY priority'`,
	RunE: func(cmd *cobra.Command, args []string) error {
		jql := buildListJQL()

		result, err := client.Search(jql, listLimit)
		if err != nil {
			return err
		}

		if listJSON {
			return printJSON(issuesToJSON(result.Issues))
		}

		if len(result.Issues) == 0 {
			if isTTY() && !listPlain {
				fmt.Println(ui.Dimmed.Render("  No issues found."))
			}
			return nil
		}

		// TSV mode: when piped or --plain
		if !isTTY() || listPlain {
			return printIssuesTSV(result.Issues)
		}

		// Rich table mode
		return printIssuesTable(result)
	},
}

func buildListJQL() string {
	if listJQL != "" {
		return listJQL
	}

	project := cfg.Project
	if listProject != "" {
		project = strings.ToUpper(listProject)
	}

	var parts []string
	parts = append(parts, fmt.Sprintf("project = %s", project))

	switch {
	case listAssignee == "me":
		parts = append(parts, "assignee = currentUser()")
	case listAssignee == "none" || listAssignee == "unassigned":
		parts = append(parts, "assignee IS EMPTY")
	case listAssignee != "":
		parts = append(parts, fmt.Sprintf("assignee = %q", listAssignee))
	case !listAll && listParent == "" && listQuery == "":
		// Default scope: my issues — unless a broader filter was given.
		parts = append(parts, "assignee = currentUser()")
	}

	if listReporter == "me" {
		parts = append(parts, "reporter = currentUser()")
	} else if listReporter != "" {
		parts = append(parts, fmt.Sprintf("reporter = %q", listReporter))
	}
	if listStatus != "" {
		parts = append(parts, fmt.Sprintf("status = %q", listStatus))
	}
	if listType != "" {
		parts = append(parts, fmt.Sprintf("issuetype = %q", listType))
	}
	if listLabel != "" {
		var labels []string
		for _, l := range strings.Split(listLabel, ",") {
			if l = strings.TrimSpace(l); l != "" {
				labels = append(labels, fmt.Sprintf("%q", l))
			}
		}
		if len(labels) == 1 {
			parts = append(parts, "labels = "+labels[0])
		} else if len(labels) > 1 {
			parts = append(parts, fmt.Sprintf("labels IN (%s)", strings.Join(labels, ", ")))
		}
	}
	if listParent != "" {
		parts = append(parts, fmt.Sprintf("parent = %s", normalizeKey(listParent)))
	}
	if listQuery != "" {
		parts = append(parts, fmt.Sprintf("text ~ %q", listQuery))
	}
	if since := sinceToJQL(listSince); since != "" {
		parts = append(parts, since)
	}

	return strings.Join(parts, " AND ") + " ORDER BY statusCategory ASC, updated DESC"
}

// sinceToJQL converts "30m" / "4h" / "7d" / "2w" / "2026-01-31" into an
// `updated >= ...` JQL clause. Returns "" for empty input.
func sinceToJQL(since string) string {
	since = strings.TrimSpace(since)
	if since == "" {
		return ""
	}
	if len(since) >= 2 {
		unit := since[len(since)-1]
		if _, err := strconv.Atoi(since[:len(since)-1]); err == nil &&
			(unit == 'm' || unit == 'h' || unit == 'd' || unit == 'w') {
			return fmt.Sprintf("updated >= -%s", since)
		}
	}
	// Assume a date literal (YYYY-MM-DD)
	return fmt.Sprintf("updated >= %q", since)
}

// --- JSON output ---

type issueJSONOut struct {
	Key      string   `json:"key"`
	Type     string   `json:"type"`
	Status   string   `json:"status"`
	Priority string   `json:"priority"`
	Assignee string   `json:"assignee"`
	Parent   string   `json:"parent"`
	Summary  string   `json:"summary"`
	Labels   []string `json:"labels,omitempty"`
	Due      string   `json:"due,omitempty"`
	Updated  string   `json:"updated"`
	Created  string   `json:"created"`
	URL      string   `json:"url"`
}

func issuesToJSON(issues []jira.Issue) []issueJSONOut {
	out := make([]issueJSONOut, len(issues))
	for i, issue := range issues {
		o := issueJSONOut{
			Key:     issue.Key,
			Type:    issue.Fields.IssueType.Name,
			Status:  issue.Fields.Status.Name,
			Summary: issue.Fields.Summary,
			Labels:  issue.Fields.Labels,
			Due:     issue.Fields.DueDate,
			Updated: issue.Fields.Updated,
			Created: issue.Fields.Created,
			URL:     client.BrowseURL(issue.Key),
		}
		if issue.Fields.Priority != nil {
			o.Priority = issue.Fields.Priority.Name
		}
		if issue.Fields.Assignee != nil {
			o.Assignee = issue.Fields.Assignee.DisplayName
		}
		if issue.Fields.Parent != nil {
			o.Parent = issue.Fields.Parent.Key
		}
		out[i] = o
	}
	return out
}

// --- TSV output (piped) ---

func printIssuesTSV(issues []jira.Issue) error {
	headers := []string{"KEY", "TYPE", "STATUS", "PRIORITY", "ASSIGNEE", "PARENT", "SUMMARY"}
	var rows [][]string
	for _, issue := range issues {
		priority := ""
		if issue.Fields.Priority != nil {
			priority = issue.Fields.Priority.Name
		}
		assignee := ""
		if issue.Fields.Assignee != nil {
			assignee = issue.Fields.Assignee.DisplayName
		}
		parent := ""
		if issue.Fields.Parent != nil {
			parent = issue.Fields.Parent.Key
		}
		rows = append(rows, []string{
			issue.Key,
			issue.Fields.IssueType.Name,
			issue.Fields.Status.Name,
			priority,
			assignee,
			parent,
			issue.Fields.Summary,
		})
	}
	printTSV(headers, rows)
	return nil
}

// --- Rich table output (TTY) ---

func printIssuesTable(result *jira.SearchResult) error {
	header := fmt.Sprintf(" %s", cfg.Project)
	if !listAll {
		header += " · my issues"
	} else {
		header += " · all issues"
	}
	if listStatus != "" {
		header += " · " + listStatus
	}
	if listType != "" {
		header += " · " + listType
	}
	fmt.Println(ui.Title.Render(header))

	var rows [][]string
	for _, issue := range result.Issues {
		priority := ""
		if issue.Fields.Priority != nil {
			priority = issue.Fields.Priority.Name
		}
		assignee := "—"
		if issue.Fields.Assignee != nil {
			assignee = firstName(issue.Fields.Assignee.DisplayName)
		}
		parent := ""
		if issue.Fields.Parent != nil {
			parent = issue.Fields.Parent.Key
		}
		summary := issue.Fields.Summary
		if len(summary) > 50 {
			summary = summary[:47] + "..."
		}

		rows = append(rows, []string{
			issue.Key,
			ui.TypeIcon(issue.Fields.IssueType.Name),
			issue.Fields.Status.Name,
			priority,
			assignee,
			parent,
			summary,
			jira.RelativeTime(issue.Fields.Updated),
		})
	}

	t := table.New().
		Headers("KEY", "", "STATUS", "PRI", "ASSIGNEE", "EPIC", "SUMMARY", "UPDATED").
		Rows(rows...).
		Border(lipgloss.RoundedBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(ui.Subtle)).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return lipgloss.NewStyle().Bold(true).Foreground(ui.Secondary).Padding(0, 1)
			}
			s := lipgloss.NewStyle().Padding(0, 1)
			switch col {
			case 0: // KEY
				s = s.Bold(true).Foreground(lipgloss.Color("#FFFFFF"))
			case 1: // TYPE icon
				s = s.Width(2).Padding(0, 0)
			case 2: // STATUS
				if row >= 0 && row < len(result.Issues) {
					s = s.Foreground(ui.StatusColor(result.Issues[row].Fields.Status.Name)).Bold(true)
				}
			case 3: // PRIORITY
				if row >= 0 && row < len(result.Issues) {
					prio := ""
					if result.Issues[row].Fields.Priority != nil {
						prio = result.Issues[row].Fields.Priority.Name
					}
					s = s.Foreground(ui.PriorityColor(prio))
				}
			case 4: // ASSIGNEE
				s = s.Foreground(ui.Muted).Width(10)
			case 5: // EPIC
				s = s.Foreground(ui.Primary).Width(8)
			case 6: // SUMMARY
				s = s.Width(52).Foreground(ui.Text)
			case 7: // UPDATED
				s = s.Foreground(ui.Muted)
			}
			return s
		})

	fmt.Println(t)
	footer := fmt.Sprintf("  %d issues", len(result.Issues))
	if !result.IsLast {
		footer += fmt.Sprintf(" (more available — raise with -n %d)", listLimit*2)
	}
	fmt.Println(ui.Dimmed.Render(footer))
	return nil
}

func firstName(full string) string {
	if parts := strings.Fields(full); len(parts) > 0 {
		return parts[0]
	}
	return full
}

func init() {
	listCmd.Flags().StringVarP(&listStatus, "status", "s", "", "Filter by status name (e.g. 'In Progress', 'Done')")
	listCmd.Flags().StringVarP(&listType, "type", "t", "", "Filter by issue type (Task, Epic, Bug, Story, Sub-task)")
	listCmd.Flags().BoolVarP(&listAll, "all", "a", false, "Show all project issues (not just assigned to you)")
	listCmd.Flags().IntVarP(&listLimit, "limit", "n", 50, "Maximum number of results")
	listCmd.Flags().BoolVar(&listJSON, "json", false, "Output as JSON array (for jq, scripts)")
	listCmd.Flags().BoolVar(&listPlain, "plain", false, "Force plain TSV output (even in terminal)")
	listCmd.Flags().StringVar(&listJQL, "jql", "", "Raw JQL query (overrides all other filters)")
	listCmd.Flags().StringVar(&listLabel, "label", "", "Filter by label (comma-separated = any of them)")
	listCmd.Flags().StringVar(&listAssignee, "assignee", "", "Filter by assignee: me, none, or account ID")
	listCmd.Flags().StringVar(&listReporter, "reporter", "", "Filter by reporter: me or account ID")
	listCmd.Flags().StringVar(&listParent, "parent", "", "List children of an epic/issue key")
	listCmd.Flags().StringVarP(&listQuery, "query", "q", "", "Free-text search (summary, description, comments)")
	listCmd.Flags().StringVar(&listSince, "since", "", "Only issues updated since: 30m, 4h, 7d, 2w, or YYYY-MM-DD")
	listCmd.Flags().StringVarP(&listProject, "project", "p", "", "Query another project (default: configured project)")
	rootCmd.AddCommand(listCmd)
}
