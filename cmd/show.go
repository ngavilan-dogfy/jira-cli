package cmd

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"github.com/ngavilan-dogfy/jira-cli/jira"
	"github.com/ngavilan-dogfy/jira-cli/ui"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var (
	showJSON bool
	showWeb  bool
	showURL  bool
)

var showCmd = &cobra.Command{
	Use:   "show <issue-key>",
	Short: "Show full issue details, metadata, and comments",
	Long: `Display complete information about a Jira issue: summary, status, type,
priority, assignee, reporter, parent/epic, labels, description, and comments.

Output:
  • TTY    → rich colored display
  • Piped  → plain text (no ANSI codes)
  • --json → full structured JSON (all fields + comments)
  • --url  → just the browse URL (useful for piping)
  • --web  → open in browser instead of printing

Examples:
  jira show PROJ-100                         # full details
  jira show 100                              # shorthand
  jira show PROJ-100 --json                  # JSON for scripting
  jira show PROJ-100 --json | jq '.status'   # extract status
  jira show PROJ-100 --url                   # just the URL
  jira show PROJ-100 --web                   # open in browser`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		key := normalizeKey(args[0])

		if showURL {
			fmt.Println(client.BrowseURL(key))
			return nil
		}

		if showWeb {
			openBrowserCmd(client.BrowseURL(key))
			fmt.Println(client.BrowseURL(key))
			return nil
		}

		issue, err := client.GetIssue(key)
		if err != nil {
			return fmt.Errorf("failed to get issue: %w", err)
		}

		comments, err := client.GetComments(key, 100)
		if err != nil {
			return fmt.Errorf("failed to get comments: %w", err)
		}

		if showJSON {
			return printIssueDetailJSON(issue, comments)
		}

		renderIssue(issue, comments)
		return nil
	},
}

type issueDetailJSON struct {
	Key         string             `json:"key"`
	Summary     string             `json:"summary"`
	Status      string             `json:"status"`
	Type        string             `json:"type"`
	Priority    string             `json:"priority,omitempty"`
	Resolution  string             `json:"resolution,omitempty"`
	Assignee    string             `json:"assignee,omitempty"`
	AssigneeID  string             `json:"assigneeId,omitempty"`
	Reporter    string             `json:"reporter,omitempty"`
	ReporterID  string             `json:"reporterId,omitempty"`
	Parent      string             `json:"parent,omitempty"`
	Labels      []string           `json:"labels,omitempty"`
	Components  []string           `json:"components,omitempty"`
	DueDate     string             `json:"due,omitempty"`
	Description string             `json:"description,omitempty"`
	Created     string             `json:"created"`
	Updated     string             `json:"updated"`
	URL         string             `json:"url"`
	Links       []issueLinkJSON    `json:"links,omitempty"`
	Subtasks    []relatedIssueJSON `json:"subtasks,omitempty"`
	Attachments []attachmentJSON   `json:"attachments,omitempty"`
	Comments    []showCommentJSON  `json:"comments,omitempty"`
}

type issueLinkJSON struct {
	Relation string `json:"relation"` // e.g. "blocks", "is blocked by", "relates to"
	Key      string `json:"key"`
	Summary  string `json:"summary"`
	Status   string `json:"status"`
}

type relatedIssueJSON struct {
	Key     string `json:"key"`
	Type    string `json:"type,omitempty"`
	Status  string `json:"status"`
	Summary string `json:"summary"`
}

type attachmentJSON struct {
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	MimeType string `json:"mimeType"`
}

type showCommentJSON struct {
	ID      string `json:"id"`
	Author  string `json:"author"`
	Body    string `json:"body"`
	Created string `json:"created"`
}

// flattenLinks converts raw issuelinks into direction-resolved entries.
func flattenLinks(links []jira.IssueLink) []issueLinkJSON {
	var out []issueLinkJSON
	for _, l := range links {
		if l.InwardIssue != nil {
			out = append(out, issueLinkJSON{
				Relation: l.Type.Inward,
				Key:      l.InwardIssue.Key,
				Summary:  l.InwardIssue.Fields.Summary,
				Status:   l.InwardIssue.Fields.Status.Name,
			})
		}
		if l.OutwardIssue != nil {
			out = append(out, issueLinkJSON{
				Relation: l.Type.Outward,
				Key:      l.OutwardIssue.Key,
				Summary:  l.OutwardIssue.Fields.Summary,
				Status:   l.OutwardIssue.Fields.Status.Name,
			})
		}
	}
	return out
}

func flattenSubtasks(subtasks []jira.RelatedIssue) []relatedIssueJSON {
	var out []relatedIssueJSON
	for _, s := range subtasks {
		out = append(out, relatedIssueJSON{
			Key:     s.Key,
			Type:    s.Fields.IssueType.Name,
			Status:  s.Fields.Status.Name,
			Summary: s.Fields.Summary,
		})
	}
	return out
}

func buildIssueDetailJSON(issue *jira.Issue, comments *jira.CommentsResult) issueDetailJSON {
	out := issueDetailJSON{
		Key:         issue.Key,
		Summary:     issue.Fields.Summary,
		Status:      issue.Fields.Status.Name,
		Type:        issue.Fields.IssueType.Name,
		DueDate:     issue.Fields.DueDate,
		Description: jira.ADFToText(issue.Fields.Description),
		Created:     issue.Fields.Created,
		Updated:     issue.Fields.Updated,
		URL:         client.BrowseURL(issue.Key),
	}
	if issue.Fields.Priority != nil {
		out.Priority = issue.Fields.Priority.Name
	}
	if issue.Fields.Resolution != nil {
		out.Resolution = issue.Fields.Resolution.Name
	}
	if issue.Fields.Assignee != nil {
		out.Assignee = issue.Fields.Assignee.DisplayName
		out.AssigneeID = issue.Fields.Assignee.AccountID
	}
	if issue.Fields.Reporter != nil {
		out.Reporter = issue.Fields.Reporter.DisplayName
		out.ReporterID = issue.Fields.Reporter.AccountID
	}
	if issue.Fields.Parent != nil {
		out.Parent = issue.Fields.Parent.Key
	}
	if len(issue.Fields.Labels) > 0 {
		out.Labels = issue.Fields.Labels
	}
	for _, c := range issue.Fields.Components {
		out.Components = append(out.Components, c.Name)
	}
	out.Links = flattenLinks(issue.Fields.IssueLinks)
	out.Subtasks = flattenSubtasks(issue.Fields.Subtasks)
	for _, a := range issue.Fields.Attachments {
		out.Attachments = append(out.Attachments, attachmentJSON{
			Filename: a.Filename,
			Size:     a.Size,
			MimeType: a.MimeType,
		})
	}
	if comments != nil {
		for _, c := range comments.Comments {
			out.Comments = append(out.Comments, showCommentJSON{
				ID:      c.ID,
				Author:  c.Author.DisplayName,
				Body:    jira.ADFToText(c.Body),
				Created: c.Created,
			})
		}
	}
	return out
}

func printIssueDetailJSON(issue *jira.Issue, comments *jira.CommentsResult) error {
	data, err := json.MarshalIndent(buildIssueDetailJSON(issue, comments), "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func renderIssue(issue *jira.Issue, comments *jira.CommentsResult) {
	title := fmt.Sprintf("%s  %s",
		ui.Key.Render(issue.Key),
		issue.Fields.Summary)
	fmt.Println(ui.Title.Render(title))
	fmt.Println()

	metaStyle := lipgloss.NewStyle().PaddingLeft(2)

	type row struct{ label, value string }
	rows := []row{
		{"Status", ui.StatusBadge(issue.Fields.Status.Name)},
		{"Type", ui.TypeIcon(issue.Fields.IssueType.Name) + " " + issue.Fields.IssueType.Name},
	}

	if issue.Fields.Priority != nil {
		rows = append(rows, row{"Priority", ui.PriorityBadge(issue.Fields.Priority.Name)})
	}
	if issue.Fields.Assignee != nil {
		rows = append(rows, row{"Assignee", issue.Fields.Assignee.DisplayName})
	}
	if issue.Fields.Reporter != nil {
		rows = append(rows, row{"Reporter", issue.Fields.Reporter.DisplayName})
	}
	if issue.Fields.Parent != nil {
		rows = append(rows, row{"Parent", issue.Fields.Parent.Key + " — " + issue.Fields.Parent.Fields.Summary})
	}
	if len(issue.Fields.Labels) > 0 {
		rows = append(rows, row{"Labels", strings.Join(issue.Fields.Labels, ", ")})
	}
	if len(issue.Fields.Components) > 0 {
		names := make([]string, len(issue.Fields.Components))
		for i, c := range issue.Fields.Components {
			names[i] = c.Name
		}
		rows = append(rows, row{"Components", strings.Join(names, ", ")})
	}
	if issue.Fields.DueDate != "" {
		rows = append(rows, row{"Due", issue.Fields.DueDate})
	}
	if issue.Fields.Resolution != nil {
		rows = append(rows, row{"Resolution", issue.Fields.Resolution.Name})
	}

	rows = append(rows,
		row{"Created", jira.ParseTime(issue.Fields.Created).Format("2006-01-02 15:04")},
		row{"Updated", jira.RelativeTime(issue.Fields.Updated)},
	)

	for _, r := range rows {
		line := ui.Label.Render(r.label+":") + " " + r.value
		fmt.Println(metaStyle.Render(line))
	}

	desc := jira.ADFToText(issue.Fields.Description)
	if desc != "" {
		fmt.Println()
		fmt.Println(ui.SectionHeader.Render("  Description"))
		descStyle := lipgloss.NewStyle().
			PaddingLeft(4).
			Foreground(ui.Text)
		fmt.Println(descStyle.Render(strings.TrimSpace(desc)))
	}

	if links := flattenLinks(issue.Fields.IssueLinks); len(links) > 0 {
		fmt.Println()
		fmt.Println(ui.SectionHeader.Render("  Linked issues"))
		for _, l := range links {
			fmt.Printf("    %s %s — %s (%s)\n", l.Relation, ui.Key.Render(l.Key), l.Summary, l.Status)
		}
	}

	if subtasks := flattenSubtasks(issue.Fields.Subtasks); len(subtasks) > 0 {
		fmt.Println()
		fmt.Println(ui.SectionHeader.Render(fmt.Sprintf("  Subtasks (%d)", len(subtasks))))
		for _, s := range subtasks {
			fmt.Printf("    %s [%s] %s\n", ui.Key.Render(s.Key), s.Status, s.Summary)
		}
	}

	if comments != nil && len(comments.Comments) > 0 {
		fmt.Println()
		fmt.Println(ui.SectionHeader.Render(fmt.Sprintf("  Comments (%d)", comments.Total)))

		for _, c := range comments.Comments {
			author := ui.Subtitle.Render(c.Author.DisplayName)
			ts := ui.Dimmed.Render(jira.RelativeTime(c.Created))
			fmt.Printf("    %s  %s\n", author, ts)

			body := jira.ADFToText(c.Body)
			bodyStyle := lipgloss.NewStyle().
				PaddingLeft(4).
				Foreground(ui.Text)
			fmt.Println(bodyStyle.Render(strings.TrimSpace(body)))
			fmt.Println()
		}
	}

	fmt.Println()
	fmt.Println(ui.Dimmed.Render("  " + client.BrowseURL(issue.Key)))
}

func openBrowserCmd(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		cmd = exec.Command("open", url)
	}
	_ = cmd.Run()
}

func init() {
	showCmd.Flags().BoolVar(&showJSON, "json", false, "Output as JSON")
	showCmd.Flags().BoolVar(&showWeb, "web", false, "Open in browser")
	showCmd.Flags().BoolVar(&showURL, "url", false, "Print browse URL only")
	rootCmd.AddCommand(showCmd)
}
