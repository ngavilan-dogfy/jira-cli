package tui

import (
	"github.com/ngavilan-dogfy/jira-cli/jira"

	tea "github.com/charmbracelet/bubbletea"
)

// issueAction runs the per-issue shortcuts shared by every screen. It
// reports whether the key was one of them.
func issueAction(ctx *appCtx, key string, is *jira.Issue) (tea.Cmd, bool) {
	if is == nil {
		return nil, false
	}
	switch key {
	case "m":
		return openModal(newMoveModal(ctx, is.Key)), true
	case "c":
		return openModal(newCommentModal(ctx, is.Key)), true
	case "e":
		return openModal(newEditModal(ctx, is.Key)), true
	case "E":
		return tea.Batch(toastNote("Loading "+is.Key+"…"), cmdStartDescEdit(ctx, is.Key)), true
	case "C":
		return openModal(newLaunchPicker(ctx, is.Key)), true
	case "i":
		me := ctx.myself()
		if me == nil {
			return toastNote("Still loading your account…"), true
		}
		if ctx.isMe(is.Fields.Assignee) {
			return toastNote(is.Key + " is already yours"), true
		}
		return cmdAssign(ctx, is.Key, &jira.UserField{AccountID: me.AccountID, DisplayName: me.DisplayName}), true
	case "u":
		if is.Fields.Assignee == nil {
			return toastNote(is.Key + " is already unassigned"), true
		}
		return cmdAssign(ctx, is.Key, nil), true
	case "A":
		return openModal(newAssignModal(ctx, is.Key)), true
	case "N":
		return openModal(newCreateModal(ctx, parentFor(is))), true
	case "w":
		return cmdToggleWatch(ctx, is.Key), true
	case "B":
		return cmdCreateBranch(is), true
	case "y":
		return copyText(is.Key, is.Key), true
	case "Y":
		return copyText(ctx.client.BrowseURL(is.Key), "link to "+is.Key), true
	case "o":
		return openBrowser(ctx.client.BrowseURL(is.Key)), true
	case "D":
		return openModal(newDeleteModal(ctx, is.Key)), true
	}
	return nil, false
}

// parentFor picks the parent for a new child of is: sub-tasks can't nest,
// so "new sub-task" on a sub-task creates a sibling.
func parentFor(is *jira.Issue) string {
	if isSubtask(is.Fields.IssueType) && is.Fields.Parent != nil {
		return is.Fields.Parent.Key
	}
	return is.Key
}

// scopeKey handles tab switching keys shared by the browser and the board.
func scopeKey(ctx *appCtx, key string) (tea.Cmd, bool) {
	scopes := ctx.visibleScopes()
	cur := 0
	for i, s := range scopes {
		if s.id == ctx.state.Scope {
			cur = i
		}
	}
	switchTo := func(i int) tea.Cmd {
		id := scopes[(i+len(scopes))%len(scopes)].id
		return func() tea.Msg { return switchScopeMsg{id} }
	}
	switch key {
	case "tab":
		return switchTo(cur + 1), true
	case "shift+tab":
		return switchTo(cur - 1), true
	case "a":
		if ctx.state.Scope == scopeMine {
			return func() tea.Msg { return switchScopeMsg{scopeTeam} }, true
		}
		return func() tea.Msg { return switchScopeMsg{scopeMine} }, true
	case "S":
		return openModal(newSearchModal(ctx)), true
	case "n":
		return openModal(newCreateModal(ctx, "")), true
	}
	if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
		i := int(key[0] - '1')
		if i < len(scopes) {
			return switchTo(i), true
		}
		return nil, true
	}
	return nil, false
}
