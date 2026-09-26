package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ngavilan-dogfy/jira-cli/jira"

	"github.com/charmbracelet/lipgloss"
)

// Renderers shared by the preview pane and the detail screen.

func updatedAt(is *jira.Issue) time.Time { return jira.ParseTime(is.Fields.Updated) }
func createdAt(is *jira.Issue) time.Time { return jira.ParseTime(is.Fields.Created) }

// sectionRule is a titled divider: "Subtasks 3/12 ──────────".
func sectionRule(title, count string, w int) string {
	head := sBold().Render(title)
	if count != "" {
		head += " " + sMuted().Render(count)
	}
	fill := w - sw(head) - 1
	if fill < 1 {
		return trunc(head, w)
	}
	return head + " " + sFaint().Render(strings.Repeat(gRule, fill))
}

type metaRow struct{ label, value string }

// metaRows lists an issue's fields as label/value pairs (values styled).
func metaRows(ctx *appCtx, is *jira.Issue, withStatus bool) []metaRow {
	f := is.Fields
	var rows []metaRow
	if withStatus {
		rows = append(rows, metaRow{"Status", statusLabel(f.Status)})
		rows = append(rows, metaRow{"Type", typeIcon(f.IssueType) + " " + f.IssueType.Name})
	}
	rows = append(rows, metaRow{"Assignee", personLabel(ctx, f.Assignee, "Unassigned")})
	if f.Reporter != nil && (f.Assignee == nil || f.Reporter.AccountID != f.Assignee.AccountID) {
		rows = append(rows, metaRow{"Reporter", personLabel(ctx, f.Reporter, "—")})
	}
	if f.Priority != nil {
		rows = append(rows, metaRow{"Priority", priorityLabel(f.Priority)})
	}
	if p := f.Parent; p != nil {
		v := typeIcon(p.Fields.IssueType) + " " + sAccent().Render(p.Key) + " " + p.Fields.Summary
		rows = append(rows, metaRow{"Parent", v})
	}
	if len(f.Labels) > 0 {
		var ls []string
		for _, l := range f.Labels {
			ls = append(ls, fg(th.cyan).Render("#"+l))
		}
		rows = append(rows, metaRow{"Labels", strings.Join(ls, " ")})
	}
	if len(f.Components) > 0 {
		var cs []string
		for _, c := range f.Components {
			cs = append(cs, c.Name)
		}
		rows = append(rows, metaRow{"Components", strings.Join(cs, ", ")})
	}
	if f.DueDate != "" {
		due, err := time.Parse("2006-01-02", f.DueDate)
		v := f.DueDate
		if err == nil {
			v = due.Format("Mon Jan 2")
			if due.Before(now().Truncate(24*time.Hour)) && kindOf(f.Status) != kindDone {
				v = fg(th.red).Bold(true).Render(v + " · overdue")
			}
		}
		rows = append(rows, metaRow{"Due", v})
	}
	if f.Resolution != nil && f.Resolution.Name != "" {
		rows = append(rows, metaRow{"Resolution", f.Resolution.Name})
	}
	rows = append(rows, metaRow{"Updated", ago(updatedAt(is))})
	rows = append(rows, metaRow{"Created", createdAt(is).Local().Format("Jan 2, 2006")})
	return rows
}

func personLabel(ctx *appCtx, u *jira.UserField, none string) string {
	if u == nil {
		return sMuted().Render(none)
	}
	if ctx.isMe(u) {
		return u.DisplayName + sMuted().Render(" (you)")
	}
	return u.DisplayName
}

// renderMeta lays out label/value rows; values wrap under themselves.
func renderMeta(rows []metaRow, w int) []string {
	lw := 0
	for _, r := range rows {
		lw = max(lw, sw(r.label))
	}
	lw += 2
	var out []string
	for _, r := range rows {
		label := sMuted().Render(fit(r.label, lw))
		vw := max(8, w-lw)
		vals := wrapStyled(r.value, vw)
		for i, v := range vals {
			if i == 0 {
				out = append(out, label+v)
			} else {
				out = append(out, strings.Repeat(" ", lw)+v)
			}
		}
	}
	return out
}

// wrapStyled wraps an already-styled single-line string. Values are short;
// anything that needs more than two lines is truncated on the second.
func wrapStyled(s string, w int) []string {
	if sw(s) <= w {
		return []string{s}
	}
	lines := strings.Split(lipgloss.NewStyle().Width(w).Render(s), "\n")
	if len(lines) > 2 {
		lines = lines[:2]
		lines[1] = trunc(strings.TrimRight(lines[1], " ")+gEllipsis, w)
	}
	for i := range lines {
		// Close any style a wrap left open so it can't bleed into what the
		// caller puts next to this line.
		lines[i] = trunc(lines[i], w) + "\x1b[0m"
	}
	return lines
}

// relatedRow renders a compact line for a sub-task, child or linked issue:
// "◐ PROJ-316  Summary…". Width-exact.
func relatedRow(ctx *appCtx, key string, st jira.StatusField, summary string, assignee *jira.UserField, w int) string {
	right := ""
	if assignee != nil {
		right = " " + sMuted().Render(shortName(assignee.DisplayName))
	}
	left := statusIcon(st) + " " + sMuted().Render(key) + " " + summary
	if kindOf(st) == kindDone {
		left = statusIcon(st) + " " + sMuted().Render(key+" "+summary)
	}
	return spread(left, right, w)
}

// childKeys returns the issue's children: loaded ones (full issues) when
// available, else the embedded sub-task stubs.
func childKeys(ctx *appCtx, is *jira.Issue) []string {
	if l := ctx.store.children[is.Key]; l != nil && l.loaded {
		return l.val
	}
	var keys []string
	for _, s := range is.Fields.Subtasks {
		keys = append(keys, s.Key)
	}
	return keys
}

// childStub finds display data for a child key, from the store or the
// parent's embedded sub-task list.
func childStub(ctx *appCtx, parent *jira.Issue, key string) (jira.StatusField, string, *jira.UserField) {
	if is := ctx.store.issue(key); is != nil {
		return is.Fields.Status, is.Fields.Summary, is.Fields.Assignee
	}
	for _, s := range parent.Fields.Subtasks {
		if s.Key == key {
			return s.Fields.Status, s.Fields.Summary, nil
		}
	}
	return jira.StatusField{}, "", nil
}

func doneCount(ctx *appCtx, parent *jira.Issue, keys []string) int {
	n := 0
	for _, k := range keys {
		st, _, _ := childStub(ctx, parent, k)
		if kindOf(st) == kindDone {
			n++
		}
	}
	return n
}

// progressBar draws done/total as a thin bar: ████░░░░ 3/8.
func progressBar(done, total, w int) string {
	if total == 0 || w < 4 {
		return ""
	}
	filled := done * w / total
	return fg(th.green).Render(strings.Repeat("█", filled)) + sFaint().Render(strings.Repeat("░", w-filled))
}

// linkEntry is one issue link seen from this issue's side.
type linkEntry struct {
	relation string
	key      string
	status   jira.StatusField
	summary  string
}

func linksOf(is *jira.Issue) []linkEntry {
	var out []linkEntry
	for _, l := range is.Fields.IssueLinks {
		switch {
		case l.OutwardIssue != nil:
			o := l.OutwardIssue
			out = append(out, linkEntry{l.Type.Outward, o.Key, o.Fields.Status, o.Fields.Summary})
		case l.InwardIssue != nil:
			o := l.InwardIssue
			out = append(out, linkEntry{l.Type.Inward, o.Key, o.Fields.Status, o.Fields.Summary})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].relation < out[j].relation })
	return out
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n>>10)
	}
	return fmt.Sprintf("%d B", n)
}

// previewLines renders the whole preview document for an issue at width w.
func previewLines(ctx *appCtx, is *jira.Issue, w int) []string {
	f := is.Fields
	var out []string

	head := typeIcon(f.IssueType) + " " + sBold().Render(is.Key) + " " + sMuted().Render(f.IssueType.Name)
	out = append(out, spread(head, " "+statusLabel(f.Status), w))
	for _, l := range wrapPlain(f.Summary, w) {
		out = append(out, sBold().Render(l))
	}
	out = append(out, "")
	out = append(out, renderMeta(metaRows(ctx, is, false), w)...)
	out = append(out, workPreview(ctx, ctx.store.work.forKey(is.Key, ctx.keyRe()), w)...)

	out = append(out, "", sectionRule("Description", "", w))
	if desc := renderADF(f.Description, w); len(desc) > 0 {
		out = append(out, desc...)
	} else {
		out = append(out, sMuted().Italic(true).Render("No description"))
	}

	if kids := childKeys(ctx, is); len(kids) > 0 {
		label := "Sub-tasks"
		if isEpic(f.IssueType) {
			label = "Issues in epic"
		}
		done := doneCount(ctx, is, kids)
		out = append(out, "", sectionRule(label, fmt.Sprintf("%d/%d done", done, len(kids)), w))
		for i, k := range kids {
			if i == 12 {
				out = append(out, sMuted().Render(fmt.Sprintf("  … %d more — enter to open the issue", len(kids)-i)))
				break
			}
			st, sum, as := childStub(ctx, is, k)
			out = append(out, relatedRow(ctx, k, st, sum, as, w))
		}
	}

	if links := linksOf(is); len(links) > 0 {
		out = append(out, "", sectionRule("Links", fmt.Sprint(len(links)), w))
		for _, l := range links {
			rel := sMuted().Render(l.relation + " ")
			out = append(out, rel+relatedRow(ctx, l.key, l.status, l.summary, nil, w-sw(rel)))
		}
	}

	if n := len(f.Attachments); n > 0 {
		out = append(out, "", sectionRule("Attachments", fmt.Sprint(n), w))
		for i, a := range f.Attachments {
			if i == 5 {
				out = append(out, sMuted().Render(fmt.Sprintf("  … %d more", n-i)))
				break
			}
			out = append(out, spread("▣ "+a.Filename, " "+sMuted().Render(humanSize(a.Size)), w))
		}
	}
	return out
}
