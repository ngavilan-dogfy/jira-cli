package tui

// One registry of shortcuts feeds the help screen and the command palette,
// so what the palette runs is exactly what the key does (the palette just
// replays the key).

type binding struct {
	keys    string // as shown to the user
	send    string // key to replay from the palette; "" = not runnable there
	desc    string
	ctx     string // where it applies: global, browse, board, detail, issue
	palette bool   // offered in the palette
}

const (
	ctxGlobal = "global"
	ctxBrowse = "browse"
	ctxBoard  = "board"
	ctxDetail = "detail"
	ctxIssue  = "issue" // any screen with a selected issue
)

var bindings = []binding{
	// Issue actions — same keys on every screen.
	{"enter", "enter", "Open issue", ctxIssue, false},
	{"m", "m", "Move to another status", ctxIssue, true},
	{"c", "c", "Comment", ctxIssue, true},
	{"e", "e", "Edit summary, priority, labels, due date", ctxIssue, true},
	{"E", "E", "Edit description in $EDITOR (markdown)", ctxIssue, true},
	{"i", "i", "Assign to me", ctxIssue, true},
	{"A", "A", "Assign to…", ctxIssue, true},
	{"u", "u", "Unassign", ctxIssue, true},
	{"N", "N", "New sub-task / child issue", ctxIssue, true},
	{"C", "C", "Start / resume Claude Code on it (own worktree, new iTerm tab)", ctxIssue, true},
	{"w", "w", "Watch / unwatch", ctxIssue, true},
	{"B", "B", "Create git branch (or copy its name)", ctxIssue, true},
	{"y", "y", "Copy key", ctxIssue, true},
	{"Y", "Y", "Copy link", ctxIssue, true},
	{"o", "o", "Open in browser", ctxIssue, true},
	{"D", "D", "Delete issue", ctxIssue, true},

	// Browser (list).
	{"j k ↑ ↓", "", "Move", ctxBrowse, false},
	{"g G", "", "Top / bottom", ctxBrowse, false},
	{"{ }", "", "Previous / next group", ctxBrowse, false},
	{"space", "", "Fold / unfold group", ctxBrowse, false},
	{"Z", "Z", "Fold / unfold all groups", ctxBrowse, true},
	{"/", "/", "Filter (accent-insensitive)", ctxBrowse, true},
	{"f", "f", "Filter by status", ctxBrowse, true},
	{"s", "s", "Sort by…", ctxBrowse, true},
	{"v", "v", "Grouped by status / flat list", ctxBrowse, true},
	{"p", "p", "Show / hide preview", ctxBrowse, true},
	{"J K", "", "Scroll preview", ctxBrowse, false},
	{"tab 1-6", "", "Switch tab: Mine · Work · Team · Watching · Recent · Epics", ctxBrowse, false},
	{"a", "a", "Toggle Mine / Team", ctxBrowse, true},
	{"S", "S", "Search: text or JQL", ctxBrowse, true},
	{"n", "n", "New issue", ctxBrowse, true},
	{"b", "b", "Board view", ctxBrowse, true},
	{"r", "r", "Refresh", ctxBrowse, true},

	// Board.
	{"h l ← →", "", "Previous / next column", ctxBoard, false},
	{"j k", "", "Move within column", ctxBoard, false},
	{"H L", "", "Move card to previous / next status", ctxBoard, false},
	{"tab 1-6", "", "Switch tab", ctxBoard, false},
	{"a", "a", "Toggle Mine / Team", ctxBoard, true},
	{"n", "n", "New issue", ctxBoard, true},
	{"b", "b", "List view", ctxBoard, true},
	{"r", "r", "Refresh", ctxBoard, true},

	// Detail.
	{"tab 1-6", "", "Switch section (2 = Work: agents, PRs, branches)", ctxDetail, false},
	{"⏎ in Work", "", "Jump to the agent's tab · open a shell in the worktree · open the PR", ctxDetail, false},
	{"X in Work", "", "Remove the selected worktree (the branch stays)", ctxDetail, false},
	{"j k", "", "Scroll / move in lists", ctxDetail, false},
	{"ctrl+d ctrl+u", "", "Half page down / up", ctxDetail, false},
	{"P", "P", "Open parent", ctxDetail, true},
	{"r", "r", "Reload issue", ctxDetail, true},
	{"esc q", "esc", "Back", ctxDetail, true},

	// Everywhere.
	{":", "", "Command palette · type a key to jump", ctxGlobal, false},
	{"?", "", "This help", ctxGlobal, false},
	{"q", "", "Quit (back, inside an issue)", ctxGlobal, false},
	{"ctrl+c", "", "Quit", ctxGlobal, false},
}

// screenContext names the binding context of a screen.
func screenContext(s screen) string {
	switch s.(type) {
	case *boardScreen:
		return ctxBoard
	case *detailScreen:
		return ctxDetail
	}
	return ctxBrowse
}
