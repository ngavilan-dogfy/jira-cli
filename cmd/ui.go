package cmd

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ngavilan-dogfy/jira-cli/internal/demo"
	"github.com/ngavilan-dogfy/jira-cli/tui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

var uiDemo bool

var uiCmd = &cobra.Command{
	Use:   "ui [issue-key]",
	Short: "Launch interactive TUI",
	Long: `Interactive terminal UI for browsing and working on issues.

Tabs (1-6, tab/shift+tab): Mine · Work · Team · Watching · Recent · Epics,
plus a Search tab after S. Issues are grouped by status with sub-tasks nested
under their parents; wide terminals show a live preview of the selected issue.

Work with agents: tickets are linked by key to local branches and worktrees
(repos folder: auto-detected, $JIRA_REPOS or the palette), to GitHub PRs
(via gh, with CI and review state) and to running Claude Code sessions,
showing whether each one is working or waiting for you. The Work tab lists
what's in flight. C on a ticket creates a worktree + branch
(<type>/<KEY>-<slug> off origin's default branch) and starts claude in a new
iTerm2 tab with the ticket as its first prompt; the TUI keeps focus.

Keys (press ? inside for the full list, : for the command palette):
  enter open · C Claude · m move · c comment · e edit · E edit description
  i assign me · A assign to… · n new issue · N new sub-task · / filter
  f status filter · s sort · b board (H/L move cards) · o browser · q quit

The last tab, layout and folded groups are remembered, and each tab's last
result is cached so the UI paints instantly and refreshes in the background.

Examples:
  jira ui            # open on your last tab
  jira ui 306        # jump straight into PROJ-306 (esc goes back to the list)
  jira ui --demo     # a made-up team's project, to look around without a Jira`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if uiDemo {
			return runDemoUI(args)
		}
		if client == nil || cfg == nil {
			return fmt.Errorf("not signed in — run 'jira setup' to get started")
		}
		open := ""
		if len(args) == 1 {
			open = normalizeKey(args[0])
		}
		m := tui.New(cfg, client, tui.Options{Open: open})
		p := tea.NewProgram(m, tea.WithAltScreen())
		_, err := p.Run()
		return err
	},
}

// runDemoUI opens the TUI on the made-up Acme Shop site: nothing reads or
// writes your Jira, your settings or your repositories.
func runDemoUI(args []string) error {
	cleanup, err := tui.EnableDemo()
	if err != nil {
		return err
	}
	defer cleanup()
	open := ""
	if len(args) == 1 {
		open = strings.ToUpper(strings.TrimSpace(args[0]))
		if _, err := strconv.Atoi(open); err == nil {
			open = demo.Project + "-" + open
		}
	}
	m := tui.New(demo.Profile(), demo.Client(), tui.Options{Open: open})
	_, err = tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

func init() {
	uiCmd.Flags().BoolVar(&uiDemo, "demo", false, "Explore a made-up team's Jira (no account needed, nothing leaves your machine)")
	rootCmd.AddCommand(uiCmd)
}
