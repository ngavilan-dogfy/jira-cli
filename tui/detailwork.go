package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// The detail's Work tab: everything in flight for the ticket, one
// actionable row per agent, pull request and branch.

type workRow struct {
	kind   string // agent, pr, branch
	agent  agentSession
	pr     pullRequest
	branch gitBranch
}

func (d *detailScreen) workRows() []workRow {
	tw := d.ctx.store.work.forKey(d.key, d.ctx.keyRe())
	var rows []workRow
	for _, a := range tw.agents {
		rows = append(rows, workRow{kind: "agent", agent: a})
	}
	for _, p := range tw.prs {
		rows = append(rows, workRow{kind: "pr", pr: p})
	}
	for _, b := range tw.branches {
		rows = append(rows, workRow{kind: "branch", branch: b})
	}
	return rows
}

func (d *detailScreen) work(w int) []string {
	rows := d.workRows()
	wi := d.ctx.store.work
	var out []string
	head := workSummary(d.ctx.store.work.forKey(d.key, d.ctx.keyRe()).agents)
	if head == "" {
		head = sMuted().Render("C starts Claude Code on this ticket in its own worktree")
	}
	out = append(out, head, "")
	if len(rows) == 0 {
		switch {
		case d.ctx.reposRoot() == "":
			out = append(out, sMuted().Render("No repositories folder found — : → Set repositories folder"))
		case wi.gitAt.IsZero():
			out = append(out, sAccent().Render(d.ctx.spinner())+" Looking for branches, PRs and agents…")
		default:
			out = append(out,
				sMuted().Render("No branch, worktree, pull request or Claude session mentions "+d.key+" yet."),
				sMuted().Render(fmt.Sprintf("Watching %s (%d repos) · PRs in %s", tildify(wi.root), len(wi.repos), orDash(wi.owner))))
		}
		d.curLine = 0
		return out
	}
	d.cursor[tabWork] = min(d.cursor[tabWork], len(rows)-1)
	section := ""
	for i, r := range rows {
		title := map[string]string{"agent": "Claude sessions", "pr": "Pull requests", "branch": "Branches & worktrees"}[r.kind]
		if title != section {
			if section != "" {
				out = append(out, "")
			}
			out = append(out, sectionRule(title, "", w))
			section = title
		}
		if i == d.cursor[tabWork] {
			d.curLine = len(out)
		}
		var line, sub string
		switch r.kind {
		case "agent":
			a := r.agent
			line = agentDot(a.state) + " " + agentStateLabel(a)
			if room := w - 4 - sw(line) - 3; room > 10 {
				line += sMuted().Render(" · " + truncLeft(tildify(a.cwd), room))
			}
			if a.lastMsg != "" {
				sub = sMuted().Italic(true).Render("“" + a.lastMsg + "”")
			}
		case "pr":
			p := r.pr
			line = fg(prColor(p.state)).Render(fmt.Sprintf("#%d %s", p.number, p.state))
			if det := prDetailLabel(wi.details[p.url]); det != "" {
				line += sMuted().Render(" · ") + det
			}
			line += "  " + sMuted().Render(shortRepo(p.repo)) + "  " + p.title
		case "branch":
			b := r.branch
			line = sBold().Render(b.repo) + "  " + b.name + sMuted().Render("  ") + branchFacts(b)
			if b.worktree != "" && !b.mainCheckout {
				sub = sMuted().Render(truncLeft(tildify(b.worktree), w-40))
				if at, _ := lastSessionIn(b.worktree); !at.IsZero() {
					sub += sMuted().Render(" · last Claude session " + ago(at))
				}
			}
		}
		out = append(out, d.cursorRow(i, line, w))
		if sub != "" {
			out = append(out, fit("    "+sub, w))
		}
	}
	return out
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// workAction runs enter / C / X on the selected Work row.
func (d *detailScreen) workAction(k string) tea.Cmd {
	rows := d.workRows()
	c := d.cursor[tabWork]
	if c < 0 || c >= len(rows) {
		if k == "C" {
			return openModal(newLaunchPicker(d.ctx, d.key))
		}
		return nil
	}
	r := rows[c]
	switch k {
	case "enter":
		switch r.kind {
		case "agent":
			return cmdFocusAgent(r.agent.tty)
		case "pr":
			return openBrowser(r.pr.url)
		case "branch":
			if r.branch.worktree != "" && !r.branch.prunable {
				return cmdOpenShell(r.branch.worktree, d.key+" · "+r.branch.repo)
			}
			return toastNote("No checkout for " + r.branch.name + " — C starts Claude on it in a new worktree")
		}
	case "C":
		if r.kind == "branch" && r.branch.worktree != "" && !r.branch.prunable {
			return cmdResumeClaude(d.key, r.branch.worktree)
		}
		if r.kind == "agent" {
			return cmdFocusAgent(r.agent.tty)
		}
		return openModal(newLaunchPicker(d.ctx, d.key))
	case "X":
		if r.kind != "branch" {
			return toastNote("X removes a worktree — pick a branch row")
		}
		b := r.branch
		switch {
		case b.prunable:
			return cmdPruneWorktrees(b.repoPath)
		case b.worktree == "" || b.mainCheckout:
			return toastNote(b.name + " has no separate worktree to remove")
		}
		return openModal(newRemoveWorktreeModal(b))
	}
	return nil
}

// cmdPruneWorktrees drops bookkeeping for worktrees whose folder is gone.
func cmdPruneWorktrees(repo string) tea.Cmd {
	return func() tea.Msg {
		if _, err := sys.run(20*time.Second, repo, "git", "worktree", "prune"); err != nil {
			return worktreeRemovedMsg{err: err}
		}
		return worktreeRemovedMsg{path: repo + " (stale worktrees pruned)"}
	}
}
