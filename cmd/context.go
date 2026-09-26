package cmd

import (
	"fmt"
	"strings"

	"github.com/ngavilan-dogfy/jira-cli/jira"

	"github.com/spf13/cobra"
)

var (
	contextJSONFlag    bool
	contextHistoryMax  int
	contextCommentsMax int
	contextFast        bool
)

// contextExtras holds the best-effort enrichment data: anything here failing
// to load must not break the command.
type contextExtras struct {
	remoteLinks []jira.RemoteLink
	watchers    *jira.WatchersResult
	worklogs    []jira.Worklog
	dev         *jira.DevStatus
}

var contextCmd = &cobra.Command{
	Use:     "context <issue-key>",
	Aliases: []string{"ctx"},
	Short:   "Full issue context in one call (optimized for AI agents)",
	Long: `Aggregate everything about an issue in a single command: fields,
description, all comments (chronological), linked issues, subtasks,
attachments, change history, available transitions, remote links
(Confluence/web), watchers, worklogs, linked development info (GitHub
branches/PRs/commits) — and, for epics, their child issues.

Designed so an AI agent (or a human) can load the complete context of a
ticket with one call instead of chaining show + history + transitions.

Output:
  • default → a markdown document (ideal to paste into an LLM prompt)
  • --json  → one structured JSON object with everything

Examples:
  jira context PROJ-100               # markdown context dump
  jira context PROJ-100 --json        # same, structured
  jira context PROJ-100 --history 0   # skip change history
  jira context PROJ-100 --fast        # core data only (fewer API calls)
  jira ctx 100                        # shorthand key + alias`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		key := normalizeKey(args[0])

		issue, err := client.GetIssue(key)
		if err != nil {
			return fmt.Errorf("failed to get issue: %w", err)
		}

		comments := &jira.CommentsResult{}
		if contextCommentsMax != 0 {
			if comments, err = client.GetComments(key, contextCommentsMax); err != nil {
				return fmt.Errorf("failed to get comments: %w", err)
			}
		}

		var history []jira.Changelog
		if contextHistoryMax > 0 {
			if history, err = client.GetChangelog(key, contextHistoryMax); err != nil {
				return fmt.Errorf("failed to get history: %w", err)
			}
		}

		transitions, err := client.GetTransitions(key)
		if err != nil {
			return fmt.Errorf("failed to get transitions: %w", err)
		}

		var children []jira.Issue
		if strings.EqualFold(issue.Fields.IssueType.Name, "Epic") {
			result, err := client.Search(fmt.Sprintf("parent = %s ORDER BY statusCategory ASC, updated DESC", key), 100)
			if err == nil {
				children = result.Issues
			}
		}

		// Best-effort enrichment: any of these failing (permissions, no
		// integration installed...) must not break the context dump.
		var extras contextExtras
		if !contextFast {
			if links, err := client.GetRemoteLinks(key); err == nil {
				extras.remoteLinks = links
			}
			if w, err := client.GetWatchers(key); err == nil {
				extras.watchers = w
			}
			if wl, err := client.GetWorklogs(key, 20); err == nil {
				extras.worklogs = wl
			}
			if issue.ID != "" {
				if dev, err := client.GetDevStatus(issue.ID); err == nil {
					extras.dev = dev
				}
			}
		}

		if contextJSONFlag {
			return printJSON(buildContextJSON(issue, comments, history, transitions, children, extras))
		}

		fmt.Print(renderContextMarkdown(issue, comments, history, transitions, children, extras))
		return nil
	},
}

// --- JSON output ---

type contextOutJSON struct {
	issueDetailJSON
	Children             []relatedIssueJSON `json:"children,omitempty"`
	History              []historyEventJSON `json:"history,omitempty"`
	AvailableTransitions []transitionJSON   `json:"availableTransitions,omitempty"`
	RemoteLinks          []remoteLinkJSON   `json:"remoteLinks,omitempty"`
	Watchers             []string           `json:"watchers,omitempty"`
	Worklogs             []worklogJSON      `json:"worklogs,omitempty"`
	Development          *jira.DevStatus    `json:"development,omitempty"`
}

type remoteLinkJSON struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Application string `json:"application,omitempty"`
}

type worklogJSON struct {
	Author    string `json:"author"`
	TimeSpent string `json:"timeSpent"`
	Started   string `json:"started"`
	Comment   string `json:"comment,omitempty"`
}

type historyEventJSON struct {
	Created string `json:"created"`
	Author  string `json:"author"`
	Field   string `json:"field"`
	From    string `json:"from,omitempty"`
	To      string `json:"to,omitempty"`
}

type transitionJSON struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	To   string `json:"to"`
}

func buildContextJSON(issue *jira.Issue, comments *jira.CommentsResult, history []jira.Changelog, transitions []jira.Transition, children []jira.Issue, extras contextExtras) contextOutJSON {
	out := contextOutJSON{issueDetailJSON: buildIssueDetailJSON(issue, comments)}

	for _, c := range children {
		out.Children = append(out.Children, relatedIssueJSON{
			Key:     c.Key,
			Type:    c.Fields.IssueType.Name,
			Status:  c.Fields.Status.Name,
			Summary: c.Fields.Summary,
		})
	}
	for _, e := range history {
		for _, it := range e.Items {
			out.History = append(out.History, historyEventJSON{
				Created: e.Created,
				Author:  e.Author.DisplayName,
				Field:   it.Field,
				From:    it.FromString,
				To:      it.ToString,
			})
		}
	}
	for _, t := range transitions {
		out.AvailableTransitions = append(out.AvailableTransitions, transitionJSON{
			ID:   t.ID,
			Name: t.Name,
			To:   t.To.Name,
		})
	}
	for _, rl := range extras.remoteLinks {
		out.RemoteLinks = append(out.RemoteLinks, remoteLinkJSON{
			Title:       rl.Object.Title,
			URL:         rl.Object.URL,
			Application: rl.Application.Name,
		})
	}
	if extras.watchers != nil {
		for _, w := range extras.watchers.Watchers {
			out.Watchers = append(out.Watchers, w.DisplayName)
		}
	}
	for _, wl := range extras.worklogs {
		out.Worklogs = append(out.Worklogs, worklogJSON{
			Author:    wl.Author.DisplayName,
			TimeSpent: wl.TimeSpent,
			Started:   wl.Started,
			Comment:   strings.TrimSpace(jira.ADFToText(wl.Comment)),
		})
	}
	if extras.dev != nil && (len(extras.dev.Branches) > 0 || len(extras.dev.PullRequests) > 0 || len(extras.dev.Commits) > 0) {
		out.Development = extras.dev
	}
	return out
}

// --- Markdown output ---

func renderContextMarkdown(issue *jira.Issue, comments *jira.CommentsResult, history []jira.Changelog, transitions []jira.Transition, children []jira.Issue, extras contextExtras) string {
	f := issue.Fields
	var b strings.Builder

	fmt.Fprintf(&b, "# %s — %s\n\n", issue.Key, f.Summary)

	meta := func(label, value string) {
		if value != "" {
			fmt.Fprintf(&b, "- **%s:** %s\n", label, value)
		}
	}
	meta("Status", f.Status.Name)
	meta("Type", f.IssueType.Name)
	if f.Priority != nil {
		meta("Priority", f.Priority.Name)
	}
	if f.Resolution != nil {
		meta("Resolution", f.Resolution.Name)
	}
	if f.Assignee != nil {
		meta("Assignee", fmt.Sprintf("%s (%s)", f.Assignee.DisplayName, f.Assignee.AccountID))
	} else {
		meta("Assignee", "unassigned")
	}
	if f.Reporter != nil {
		meta("Reporter", f.Reporter.DisplayName)
	}
	if f.Parent != nil {
		meta("Parent", fmt.Sprintf("%s — %s", f.Parent.Key, f.Parent.Fields.Summary))
	}
	if len(f.Labels) > 0 {
		meta("Labels", strings.Join(f.Labels, ", "))
	}
	if len(f.Components) > 0 {
		names := make([]string, len(f.Components))
		for i, c := range f.Components {
			names[i] = c.Name
		}
		meta("Components", strings.Join(names, ", "))
	}
	meta("Due", f.DueDate)
	meta("Created", jira.ParseTime(f.Created).Format("2006-01-02 15:04"))
	meta("Updated", jira.ParseTime(f.Updated).Format("2006-01-02 15:04"))
	meta("URL", client.BrowseURL(issue.Key))

	if desc := strings.TrimSpace(jira.ADFToText(f.Description)); desc != "" {
		fmt.Fprintf(&b, "\n## Description\n\n%s\n", desc)
	}

	if links := flattenLinks(f.IssueLinks); len(links) > 0 {
		b.WriteString("\n## Linked issues\n\n")
		for _, l := range links {
			fmt.Fprintf(&b, "- %s **%s** — %s _(%s)_\n", l.Relation, l.Key, l.Summary, l.Status)
		}
	}

	if len(f.Subtasks) > 0 {
		fmt.Fprintf(&b, "\n## Subtasks (%d)\n\n", len(f.Subtasks))
		for _, s := range f.Subtasks {
			fmt.Fprintf(&b, "- **%s** [%s] %s\n", s.Key, s.Fields.Status.Name, s.Fields.Summary)
		}
	}

	if len(children) > 0 {
		fmt.Fprintf(&b, "\n## Epic children (%d)\n\n", len(children))
		for _, c := range children {
			fmt.Fprintf(&b, "- **%s** [%s] %s\n", c.Key, c.Fields.Status.Name, c.Fields.Summary)
		}
	}

	if len(f.Attachments) > 0 {
		fmt.Fprintf(&b, "\n## Attachments (%d)\n\n", len(f.Attachments))
		for _, a := range f.Attachments {
			fmt.Fprintf(&b, "- %s (%s, %d bytes) — download: `jira attachments %s --download`\n", a.Filename, a.MimeType, a.Size, issue.Key)
		}
	}

	if dev := extras.dev; dev != nil && (len(dev.PullRequests) > 0 || len(dev.Branches) > 0 || len(dev.Commits) > 0) {
		b.WriteString("\n## Development (linked code)\n")
		if len(dev.PullRequests) > 0 {
			fmt.Fprintf(&b, "\n### Pull requests (%d)\n\n", len(dev.PullRequests))
			for _, pr := range dev.PullRequests {
				fmt.Fprintf(&b, "- [%s] %s — %s\n", pr.Status, pr.Name, pr.URL)
			}
		}
		if len(dev.Branches) > 0 {
			fmt.Fprintf(&b, "\n### Branches (%d)\n\n", len(dev.Branches))
			for _, br := range dev.Branches {
				repo := ""
				if br.Repository != "" {
					repo = " (" + br.Repository + ")"
				}
				fmt.Fprintf(&b, "- %s%s — %s\n", br.Name, repo, br.URL)
			}
		}
		if len(dev.Commits) > 0 {
			fmt.Fprintf(&b, "\n### Commits (%d)\n\n", len(dev.Commits))
			for _, cm := range dev.Commits {
				msg := strings.SplitN(cm.Message, "\n", 2)[0]
				fmt.Fprintf(&b, "- %s — %s\n", msg, cm.URL)
			}
		}
	}

	if len(extras.remoteLinks) > 0 {
		b.WriteString("\n## Remote links\n\n")
		for _, rl := range extras.remoteLinks {
			app := ""
			if rl.Application.Name != "" {
				app = " _(" + rl.Application.Name + ")_"
			}
			fmt.Fprintf(&b, "- [%s](%s)%s\n", rl.Object.Title, rl.Object.URL, app)
		}
	}

	if len(extras.worklogs) > 0 {
		fmt.Fprintf(&b, "\n## Worklog (%d entries)\n\n", len(extras.worklogs))
		for _, wl := range extras.worklogs {
			line := fmt.Sprintf("- %s · %s — %s", jira.ParseTime(wl.Started).Format("2006-01-02"), wl.Author.DisplayName, wl.TimeSpent)
			if comment := strings.TrimSpace(jira.ADFToText(wl.Comment)); comment != "" {
				line += ": " + comment
			}
			b.WriteString(line + "\n")
		}
	}

	if extras.watchers != nil && len(extras.watchers.Watchers) > 0 {
		names := make([]string, len(extras.watchers.Watchers))
		for i, w := range extras.watchers.Watchers {
			names[i] = w.DisplayName
		}
		fmt.Fprintf(&b, "\n## Watchers (%d)\n\n%s\n", extras.watchers.WatchCount, strings.Join(names, ", "))
	}

	if comments != nil && len(comments.Comments) > 0 {
		fmt.Fprintf(&b, "\n## Comments (%d, oldest first)\n", len(comments.Comments))
		for _, c := range comments.Comments {
			fmt.Fprintf(&b, "\n### %s — %s\n\n%s\n",
				c.Author.DisplayName,
				jira.ParseTime(c.Created).Format("2006-01-02 15:04"),
				strings.TrimSpace(jira.ADFToText(c.Body)))
		}
	}

	if len(history) > 0 {
		fmt.Fprintf(&b, "\n## History (last %d events)\n\n", len(history))
		for _, e := range history {
			for _, it := range e.Items {
				from := it.FromString
				if from == "" {
					from = "∅"
				}
				to := it.ToString
				if to == "" {
					to = "∅"
				}
				fmt.Fprintf(&b, "- %s · %s — %s: %s → %s\n",
					jira.ParseTime(e.Created).Format("2006-01-02 15:04"),
					e.Author.DisplayName, it.Field, from, to)
			}
		}
	}

	if len(transitions) > 0 {
		b.WriteString("\n## Available transitions\n\n")
		for _, t := range transitions {
			fmt.Fprintf(&b, "- %s → **%s** (id %s)\n", t.Name, t.To.Name, t.ID)
		}
	}

	return b.String()
}

func init() {
	contextCmd.Flags().BoolVar(&contextJSONFlag, "json", false, "Output as one structured JSON object")
	contextCmd.Flags().IntVar(&contextHistoryMax, "history", 20, "Max change-history events to include (0 to skip)")
	contextCmd.Flags().IntVar(&contextCommentsMax, "comments", 100, "Max comments to include (0 to skip)")
	contextCmd.Flags().BoolVar(&contextFast, "fast", false, "Skip remote links, watchers, worklogs and dev info (fewer API calls)")
	rootCmd.AddCommand(contextCmd)
}
