package tui

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ngavilan-dogfy/jira-cli/internal/demo"

	"github.com/charmbracelet/bubbles/cursor"
	tea "github.com/charmbracelet/bubbletea"
)

// newDemoHarness runs the app the way 'jira ui --demo' does.
func newDemoHarness(t *testing.T, width, height int) *harness {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM_PROGRAM", "")
	t.Setenv("JIRA_REPOS", "")
	origSys, origLook, origClaude := sys, lookPath, claudeHome
	t.Cleanup(func() {
		sys, lookPath, claudeHome = origSys, origLook, origClaude
		demoMode, demoDir = false, ""
	})
	cleanup, err := EnableDemo()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	origTick, origCursor, origClip := tickFn, cursorMode, writeClipboard
	tickFn = func(time.Duration, func(time.Time) tea.Msg) tea.Cmd { return nil }
	cursorMode = cursor.CursorStatic
	t.Cleanup(func() { tickFn, cursorMode, writeClipboard = origTick, origCursor, origClip })

	h := &harness{t: t, app: New(demo.Profile(), demo.Client(), Options{}), w: width, h: height}
	writeClipboard = func(text string) error { h.clipboard = text; return nil }
	h.run(h.app.Init())
	h.dispatch(tea.WindowSizeMsg{Width: width, Height: height})
	return h
}

func TestDemoWalkthrough(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {140, 40}, {200, 55}} {
		h := newDemoHarness(t, size[0], size[1])
		h.expect("SHOP", "DEMO", "SHOP-150")
		// Work: branches, worktrees, PRs and the two agents.
		h.keys("2")
		h.expect("SHOP-111", "SHOP-110", "#128")
		// Team, Watching, Recent, Epics.
		for _, tab := range []string{"3", "4", "5", "6"} {
			h.keys(tab)
			h.screen()
		}
		h.keys("6")
		h.expect("Checkout v2")
		h.keys("1")
		h.selectKey("SHOP-110")
		h.keys("<enter>")
		h.expect("One-page checkout layout", "Acceptance criteria")
		h.keys("o")
		if h.app.toast == nil || !strings.Contains(h.app.toast.text, "Demo data") {
			t.Fatalf("o should say the demo has nothing to open, toast: %+v", h.app.toast)
		}
		h.keys("<esc>")
		h.screen()
	}
}

// The demo writes nothing outside its temporary folder.
func TestDemoLeavesNoTrace(t *testing.T) {
	h := newDemoHarness(t, 120, 36)
	h.keys("2")
	h.screen()
	home := os.Getenv("HOME")
	entries, _ := os.ReadDir(home)
	for _, e := range entries {
		t.Errorf("the demo wrote %s in HOME", e.Name())
	}
}
