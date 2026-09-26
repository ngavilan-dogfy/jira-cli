package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// How the work index shows up: a compact cell in the issue list, a "Work"
// block in the preview, and the rows of the detail's Work tab.

// agentDot is the agent's state at a glance: green waits for you, yellow
// is busy.
func agentDot(state string) string {
	switch state {
	case "waiting":
		return fg(th.green).Bold(true).Render("●")
	case "approval":
		return fg(th.accent).Bold(true).Render("●")
	case "working":
		return fg(th.yellow).Render("●")
	}
	return sMuted().Render("●")
}

// needsYou reports agents blocked on the human.
func needsYou(state string) bool { return state == "waiting" || state == "approval" }

func agentStateLabel(a agentSession) string {
	var s string
	switch a.state {
	case "waiting":
		s = fg(th.green).Bold(true).Render("Claude is waiting for you")
	case "approval":
		s = fg(th.accent).Bold(true).Render("Claude may need your approval")
	case "working":
		s = fg(th.yellow).Render("Claude is working")
	default:
		s = sMuted().Render("Claude is open")
	}
	if !a.lastAt.IsZero() {
		s += sMuted().Render(" · " + ago(a.lastAt))
	}
	return s
}

func prColor(state string) lipgloss.TerminalColor {
	switch state {
	case "open":
		return th.green
	case "merged":
		return th.magenta
	case "closed":
		return th.red
	}
	return th.muted // draft
}

// bestPR picks the PR that matters most: open, then draft, then the most
// recently updated.
func bestPR(prs []pullRequest) (pullRequest, bool) {
	rank := map[string]int{"open": 0, "draft": 1, "merged": 2, "closed": 3}
	var best pullRequest
	found := false
	for _, p := range prs {
		if !found || rank[p.state] < rank[best.state] || (rank[p.state] == rank[best.state] && p.updated.After(best.updated)) {
			best, found = p, true
		}
	}
	return best, found
}

// bestAgent prefers an agent waiting for you over a busy one.
func bestAgent(as []agentSession) (agentSession, bool) {
	rank := map[string]int{"approval": 0, "waiting": 0, "working": 1}
	var best agentSession
	found := false
	for _, a := range as {
		ra, okA := rank[a.state]
		rb, okB := rank[best.state]
		if !okA {
			ra = 2
		}
		if !okB {
			rb = 2
		}
		if !found || ra < rb {
			best, found = a, true
		}
	}
	return best, found
}

const workCellW = 7

// workCell is the list column: "● #412", "● wip", "  #412", "  wip".
func workCell(tw ticketWork) string {
	dot := "  "
	if a, ok := bestAgent(tw.agents); ok {
		dot = agentDot(a.state) + " "
	}
	rest := ""
	if p, ok := bestPR(tw.prs); ok {
		rest = fg(prColor(p.state)).Render(fmt.Sprintf("#%d", p.number))
	} else if liveBranches(tw.branches) > 0 || len(tw.agents) > 0 {
		rest = sMuted().Render("wip")
	}
	if dot == "  " && rest == "" {
		return ""
	}
	return fit(dot+rest, workCellW)
}

func prDetailLabel(d *prDetail) string {
	if d == nil {
		return ""
	}
	if d.loading {
		return sMuted().Render("checking…")
	}
	var parts []string
	switch d.checks {
	case "pass":
		parts = append(parts, fg(th.green).Render("checks pass"))
	case "fail":
		parts = append(parts, fg(th.red).Bold(true).Render("checks failing"))
	case "pending":
		parts = append(parts, fg(th.yellow).Render("checks running"))
	}
	switch d.review {
	case "APPROVED":
		parts = append(parts, fg(th.green).Render("approved"))
	case "CHANGES_REQUESTED":
		parts = append(parts, fg(th.red).Render("changes requested"))
	case "REVIEW_REQUIRED":
		parts = append(parts, sMuted().Render("review pending"))
	}
	return strings.Join(parts, sMuted().Render(" · "))
}

// branchFacts summarizes a branch: where it's checked out and how it
// compares with its upstream.
func liveBranches(bs []gitBranch) int {
	n := 0
	for _, b := range bs {
		if b.live() {
			n++
		}
	}
	return n
}

func branchFacts(b gitBranch) string {
	if !b.live() {
		why := "stale"
		switch {
		case b.prunable:
			why = "worktree folder deleted"
		case strings.Contains(b.track, "gone"):
			why = "upstream deleted (merged?)"
		}
		return sFaint().Render(why + " · " + ago(b.when))
	}
	var parts []string
	switch {
	case b.prunable:
		parts = append(parts, sFaint().Render("worktree gone"))
	case b.mainCheckout:
		parts = append(parts, "checked out in the repo")
	case b.worktree != "":
		parts = append(parts, "worktree")
	default:
		parts = append(parts, sMuted().Render("no checkout"))
	}
	if b.track != "" {
		parts = append(parts, fg(th.yellow).Render(b.track))
	} else if b.upstream == "" {
		parts = append(parts, sMuted().Render("not pushed"))
	}
	if !b.when.IsZero() {
		parts = append(parts, sMuted().Render(ago(b.when)))
	}
	return strings.Join(parts, sMuted().Render(" · "))
}

// workPreview is the preview pane's Work block (nil when there's nothing).
func workPreview(ctx *appCtx, tw ticketWork, w int) []string {
	if tw.empty() {
		return nil
	}
	// Only merged PRs and old branches: history, not work in flight.
	if len(tw.agents) == 0 && liveBranches(tw.branches) == 0 {
		if _, ok := bestPR(tw.prs); !ok {
			return nil
		}
	}
	var rows []metaRow
	for _, a := range tw.agents {
		v := agentStateLabel(a)
		if a.lastMsg != "" {
			v += "\n" + sMuted().Italic(true).Render("“"+a.lastMsg+"”")
		}
		rows = append(rows, metaRow{"Agent", v})
	}
	for _, p := range tw.prs {
		v := fg(prColor(p.state)).Render(fmt.Sprintf("#%d %s", p.number, p.state))
		if d := prDetailLabel(ctx.store.work.details[p.url]); d != "" {
			v += sMuted().Render(" · ") + d
		}
		v += " " + sMuted().Render(shortRepo(p.repo))
		rows = append(rows, metaRow{"PR", v})
	}
	shown, old := 0, 0
	for _, b := range tw.branches {
		if !b.live() {
			old++
			continue
		}
		if shown == 3 {
			old++
			continue
		}
		shown++
		rows = append(rows, metaRow{"Branch", sBold().Render(b.repo) + " " + b.name + sMuted().Render(" · ") + branchFacts(b)})
	}
	if old > 0 {
		rows = append(rows, metaRow{"", sFaint().Render(fmt.Sprintf("+ %d older branch%s (the issue's Work tab lists them)", old, map[bool]string{true: "", false: "es"}[old == 1]))})
	}
	if len(rows) == 0 {
		return nil
	}
	out := []string{"", sectionRule("Work", "", w)}
	return append(out, renderMetaMulti(rows, w)...)
}

func shortRepo(slug string) string {
	if i := strings.IndexByte(slug, '/'); i >= 0 {
		return slug[i+1:]
	}
	return slug
}

// renderMetaMulti is renderMeta for values that may span several lines
// (a label followed by an agent's last message).
func renderMetaMulti(rows []metaRow, w int) []string {
	lw := 0
	for _, r := range rows {
		lw = max(lw, sw(r.label))
	}
	lw += 2
	var out []string
	for _, r := range rows {
		for i, part := range strings.Split(r.value, "\n") {
			label := strings.Repeat(" ", lw)
			if i == 0 {
				label = sMuted().Render(fit(r.label, lw))
			}
			for j, v := range wrapStyled(part, max(8, w-lw)) {
				if j > 0 {
					label = strings.Repeat(" ", lw)
				}
				out = append(out, label+v)
			}
		}
	}
	return out
}

// workSummary is one line for headers: "2 waiting for you · 1 working".
func workSummary(agents []agentSession) string {
	waiting, working := 0, 0
	for _, a := range agents {
		switch {
		case needsYou(a.state):
			waiting++
		case a.state == "working":
			working++
		}
	}
	var parts []string
	if waiting > 0 {
		parts = append(parts, fg(th.green).Bold(true).Render(fmt.Sprintf("%d waiting for you", waiting)))
	}
	if working > 0 {
		parts = append(parts, fg(th.yellow).Render(fmt.Sprintf("%d working", working)))
	}
	return strings.Join(parts, sMuted().Render(" · "))
}

// agentsNeedingYou counts Claude sessions (any folder) blocked on you.
func agentsNeedingYou(ctx *appCtx) int {
	n := 0
	for _, a := range ctx.store.work.agents {
		if needsYou(a.state) {
			n++
		}
	}
	return n
}

// workStamp changes whenever a ticket's work does (preview cache key).
func workStamp(ctx *appCtx, key string) string {
	tw := ctx.store.work.forKey(key, ctx.keyRe())
	var b strings.Builder
	for _, a := range tw.agents {
		fmt.Fprintf(&b, "%d%s%s%d|", a.pid, a.state, a.lastMsg, a.lastAt.Unix())
	}
	for _, p := range tw.prs {
		b.WriteString(p.url + p.state)
		if d := ctx.store.work.details[p.url]; d != nil {
			fmt.Fprintf(&b, "%s%s%v", d.checks, d.review, d.loading)
		}
	}
	for _, br := range tw.branches {
		fmt.Fprintf(&b, "%s%s%s%v%s|", br.name, br.sha, br.worktree, br.prunable, br.track)
	}
	return b.String()
}

// ensurePRDetails fetches CI/review state for a ticket's open PRs once.
func ensurePRDetails(ctx *appCtx, key string) tea.Cmd {
	var cmds []tea.Cmd
	for _, p := range ctx.store.work.forKey(key, ctx.keyRe()).prs {
		if p.state != "open" && p.state != "draft" {
			continue
		}
		if _, ok := ctx.store.work.details[p.url]; ok {
			continue
		}
		ctx.store.work.details[p.url] = &prDetail{loading: true}
		cmds = append(cmds, cmdPRDetail(p.url))
	}
	return tea.Batch(cmds...)
}

// workEmptyState explains an empty Work tab.
func workEmptyState(ctx *appCtx) []string {
	w := ctx.store.work
	root := ctx.reposRoot()
	switch {
	case root == "":
		return []string{"No repositories folder found.", sMuted().Render(": → Set repositories folder, or export JIRA_REPOS")}
	case w.gitAt.IsZero() || w.agentsAt.IsZero():
		return []string{sAccent().Render(ctx.spinner()) + " Looking for work in " + tildify(root) + "…"}
	}
	var gh []string
	if w.prsErr != nil {
		gh = []string{"", fg(th.yellow).Render("GitHub: " + firstLine(w.prsErr.Error()))}
	}
	return append([]string{
		"Nothing in flight.",
		sMuted().Render("Tickets appear here when a branch, worktree, pull request or running"),
		sMuted().Render("Claude session mentions their key. C on any ticket starts Claude on it."),
		"",
		sMuted().Render(fmt.Sprintf("Watching %s (%d repos)", tildify(root), len(w.repos))),
	}, gh...)
}
