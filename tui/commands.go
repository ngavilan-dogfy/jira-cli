package tui

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"github.com/ngavilan-dogfy/jira-cli/jira"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
)

// Async commands. Each runs on its own goroutine and reports back with a
// message; none of them touch the store.

func cmdFetchScope(c *appCtx, id string) tea.Cmd {
	jql := c.scopeJQLFor(id)
	limit := scopeByID(id).limit
	client := c.client
	return func() tea.Msg {
		if jql == "" {
			return scopeLoadedMsg{id: id}
		}
		res, err := client.Search(jql, limit)
		if err != nil {
			return scopeLoadedMsg{id: id, jql: jql, err: err}
		}
		return scopeLoadedMsg{id: id, jql: jql, issues: res.Issues, truncated: !res.IsLast}
	}
}

func cmdFetchIssue(c *appCtx, key string) tea.Cmd {
	client := c.client
	return func() tea.Msg {
		is, err := client.GetIssue(key)
		return issueLoadedMsg{key: key, issue: is, err: err}
	}
}

func cmdFetchComments(c *appCtx, key string) tea.Cmd {
	client := c.client
	return func() tea.Msg {
		res, err := client.GetComments(key, 100)
		if err != nil {
			return commentsLoadedMsg{key: key, err: err}
		}
		return commentsLoadedMsg{key: key, comments: res.Comments}
	}
}

// cmdFetchHistory gets the most recent changelog page: the API pages oldest
// first, so peek at the total and jump to the tail.
func cmdFetchHistory(c *appCtx, key string) tea.Cmd {
	client := c.client
	const page = 100
	return func() tea.Msg {
		vals, total, err := client.GetChangelogPage(key, 0, page)
		if err != nil {
			return historyLoadedMsg{key: key, err: err}
		}
		if total > page {
			if tail, _, err := client.GetChangelogPage(key, total-page, page); err == nil {
				vals = tail
			}
		}
		// Newest first.
		for i, j := 0, len(vals)-1; i < j; i, j = i+1, j-1 {
			vals[i], vals[j] = vals[j], vals[i]
		}
		return historyLoadedMsg{key: key, entries: vals}
	}
}

func cmdFetchChildren(c *appCtx, key string) tea.Cmd {
	client := c.client
	return func() tea.Msg {
		res, err := client.Search(fmt.Sprintf("parent = %s ORDER BY key ASC", key), 300)
		if err != nil {
			return childrenLoadedMsg{key: key, err: err}
		}
		return childrenLoadedMsg{key: key, issues: res.Issues}
	}
}

func cmdFetchWatch(c *appCtx, key string) tea.Cmd {
	client := c.client
	return func() tea.Msg {
		res, err := client.GetWatchers(key)
		if err != nil {
			return watchLoadedMsg{key: key, err: err}
		}
		return watchLoadedMsg{key: key, info: watchInfo{watching: res.IsWatching, count: res.WatchCount}}
	}
}

func cmdFetchTransitions(c *appCtx, key string) tea.Cmd {
	client := c.client
	return func() tea.Msg {
		ts, err := client.GetTransitions(key)
		return transitionsLoadedMsg{key: key, transitions: ts, err: err}
	}
}

func cmdFetchMyself(c *appCtx) tea.Cmd {
	client := c.client
	return func() tea.Msg {
		me, err := client.GetMyself()
		return myselfLoadedMsg{me: me, err: err}
	}
}

func cmdFetchUsers(c *appCtx) tea.Cmd {
	client, p := c.client, c.project()
	return func() tea.Msg {
		us, err := client.SearchUsers(p)
		return usersLoadedMsg{users: us, err: err}
	}
}

func cmdFetchPriorities(c *appCtx) tea.Cmd {
	client := c.client
	return func() tea.Msg {
		ps, err := client.GetPriorities()
		return prioritiesLoadedMsg{priorities: ps, err: err}
	}
}

func cmdFetchTypes(c *appCtx) tea.Cmd {
	client, p := c.client, c.project()
	return func() tea.Msg {
		ts, err := client.GetIssueTypes(p)
		return typesLoadedMsg{types: ts, err: err}
	}
}

func cmdFetchStatuses(c *appCtx) tea.Cmd {
	client, p := c.client, c.project()
	return func() tea.Msg {
		ss, err := client.GetProjectStatuses(p)
		return statusesLoadedMsg{statuses: ss, err: err}
	}
}

// ─── writes ──────────────────────────────────────────────────────

// mutate runs an optimistic local edit (if any) and then the API call. The
// app refetches the issue afterwards, which confirms or reverts the edit.
func mutate(key string, local func(*jira.Issue), call func() error, ok string) tea.Cmd {
	api := func() tea.Msg {
		if err := call(); err != nil {
			return mutationDoneMsg{key: key, err: err}
		}
		return mutationDoneMsg{key: key, ok: ok}
	}
	if local == nil {
		return api
	}
	return tea.Sequence(func() tea.Msg { return localEditMsg{key: key, apply: local} }, api)
}

func cmdTransition(c *appCtx, key string, t jira.Transition) tea.Cmd {
	client := c.client
	to := t.To
	return mutate(key,
		func(is *jira.Issue) { is.Fields.Status = to },
		func() error { return client.DoTransition(key, t.ID) },
		key+" "+gArrow+" "+to.Name)
}

func cmdAssign(c *appCtx, key string, user *jira.UserField) tea.Cmd {
	client := c.client
	id, msg := "", key+" unassigned"
	if user != nil {
		id = user.AccountID
		msg = key + " " + gArrow + " " + user.DisplayName
	}
	return mutate(key,
		func(is *jira.Issue) {
			if user == nil {
				is.Fields.Assignee = nil
			} else {
				u := *user
				is.Fields.Assignee = &u
			}
		},
		func() error { return client.AssignIssue(key, id) },
		msg)
}

func cmdComment(c *appCtx, key, body string) tea.Cmd {
	client := c.client
	return func() tea.Msg {
		if err := client.AddComment(key, body); err != nil {
			return mutationDoneMsg{key: key, err: err}
		}
		return mutationDoneMsg{key: key, ok: "Comment added to " + key}
	}
}

func cmdEdit(c *appCtx, key string, fields map[string]any, local func(*jira.Issue)) tea.Cmd {
	client := c.client
	return mutate(key, local,
		func() error { return client.EditIssue(key, fields) },
		key+" updated")
}

func cmdDelete(c *appCtx, key string) tea.Cmd {
	client := c.client
	return func() tea.Msg {
		if err := client.DeleteIssue(key); err != nil {
			return mutationDoneMsg{key: key, err: err}
		}
		return mutationDoneMsg{key: key, ok: key + " deleted", gone: true}
	}
}

func cmdToggleWatch(c *appCtx, key string) tea.Cmd {
	client := c.client
	me := c.myself()
	return func() tea.Msg {
		if me == nil {
			return mutationDoneMsg{key: key, err: fmt.Errorf("still loading your account, try again")}
		}
		res, err := client.GetWatchers(key)
		if err != nil {
			return mutationDoneMsg{key: key, err: err}
		}
		if res.IsWatching {
			err = client.RemoveWatcher(key, me.AccountID)
		} else {
			err = client.AddWatcher(key, me.AccountID)
		}
		if err != nil {
			return mutationDoneMsg{key: key, err: err}
		}
		if res.IsWatching {
			return mutationDoneMsg{key: key, ok: "Stopped watching " + key}
		}
		return mutationDoneMsg{key: key, ok: "Watching " + key}
	}
}

// cmdStartDescEdit refetches the issue so the editor starts from what Jira
// has right now, not from a list loaded minutes ago.
func cmdStartDescEdit(c *appCtx, key string) tea.Cmd {
	client := c.client
	return func() tea.Msg {
		is, err := client.GetIssue(key)
		return descEditReadyMsg{key: key, issue: is, err: err}
	}
}

// cmdSaveDescription writes an edited description, unless someone changed
// it in Jira while the editor was open: then nothing is overwritten and
// the edited text goes to the clipboard instead.
func cmdSaveDescription(c *appCtx, key string, original []byte, text string) tea.Cmd {
	client := c.client
	return func() tea.Msg {
		fresh, err := client.GetIssue(key)
		if err != nil {
			return mutationDoneMsg{key: key, err: err}
		}
		if !adfEquivalent(original, fresh.Fields.Description) && !(adfIsEmpty(original) && adfIsEmpty(fresh.Fields.Description)) {
			_ = writeClipboard(text)
			return mutationDoneMsg{key: key, err: fmt.Errorf("%s's description changed in Jira while you were editing, so it wasn't saved — your text is in the clipboard", key)}
		}
		if err := client.EditIssue(key, map[string]any{"description": jira.MarkdownToADF(text)}); err != nil {
			return mutationDoneMsg{key: key, err: err}
		}
		return mutationDoneMsg{key: key, ok: "Description of " + key + " updated"}
	}
}

type newIssue struct {
	project, issueType, summary, parent, description, priority string
	assignee                                                   *jira.UserField
}

func cmdCreate(c *appCtx, n newIssue) tea.Cmd {
	client := c.client
	return func() tea.Msg {
		is, err := client.CreateIssue(n.project, n.issueType, n.summary, n.parent, n.description, "")
		if err != nil {
			return mutationDoneMsg{err: err}
		}
		// Same as `jira create`: fields the create screen may not accept are
		// applied afterwards, and failures there only warn.
		var warn []string
		if n.priority != "" {
			if err := client.EditIssue(is.Key, map[string]any{"priority": map[string]string{"name": n.priority}}); err != nil {
				warn = append(warn, "priority")
			}
		}
		if n.assignee != nil {
			if err := client.AssignIssue(is.Key, n.assignee.AccountID); err != nil {
				warn = append(warn, "assignee")
			}
		}
		msg := "Created " + is.Key
		if len(warn) > 0 {
			msg += " (couldn't set " + strings.Join(warn, ", ") + ")"
		}
		return mutationDoneMsg{key: is.Key, ok: msg, refresh: true, created: is.Key}
	}
}

// ─── local side effects ──────────────────────────────────────────

func openBrowser(url string) tea.Cmd {
	return func() tea.Msg {
		if demoMode {
			return toastMsg{text: "Demo data: there's nothing to open", kind: toastInfo}
		}
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			cmd = exec.Command("open", url)
		case "windows":
			cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
		default:
			cmd = exec.Command("xdg-open", url)
		}
		if err := cmd.Start(); err != nil {
			return toastMsg{text: "couldn't open browser: " + err.Error(), kind: toastErr}
		}
		return toastMsg{text: "Opened in browser", kind: toastInfo}
	}
}

// writeClipboard is swappable so tests never touch the real clipboard.
var writeClipboard = clipboard.WriteAll

func copyText(text, label string) tea.Cmd {
	return func() tea.Msg {
		if err := writeClipboard(text); err != nil {
			return toastMsg{text: "clipboard: " + err.Error(), kind: toastErr}
		}
		return toastMsg{text: "Copied " + label, kind: toastOK}
	}
}
