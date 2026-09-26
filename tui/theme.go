package tui

import (
	"os"
	"strings"

	"github.com/ngavilan-dogfy/jira-cli/jira"

	"github.com/charmbracelet/lipgloss"
	colorful "github.com/lucasb-eyer/go-colorful"
	"github.com/muesli/termenv"
)

// The TUI paints with the terminal's own ANSI palette (colors 1-8) so it
// inherits whatever theme the user runs — gruvbox, solarized, catppuccin...
// Only the neutral surfaces (bars, selection, rules) need shades the ANSI
// palette doesn't have; those are blended from the terminal's real background
// and foreground, queried once at startup via OSC 10/11.

type palette struct {
	dark bool

	bg      lipgloss.TerminalColor // terminal background (pill text)
	surface lipgloss.TerminalColor // header/footer bars, code blocks
	sel     lipgloss.TerminalColor // selected row
	faint   lipgloss.TerminalColor // rules, borders, tree guides
	muted   lipgloss.TerminalColor // secondary text
	text    lipgloss.TerminalColor // body text (terminal default)

	accent  lipgloss.TerminalColor
	red     lipgloss.TerminalColor
	green   lipgloss.TerminalColor
	yellow  lipgloss.TerminalColor
	blue    lipgloss.TerminalColor
	magenta lipgloss.TerminalColor
	cyan    lipgloss.TerminalColor
}

// th is the active palette. Tests get the deterministic fallback.
var th = fallbackPalette()

func fallbackPalette() palette {
	return palette{
		dark:    true,
		bg:      lipgloss.Color("#282828"),
		surface: lipgloss.Color("#32302f"),
		sel:     lipgloss.Color("#3c3836"),
		faint:   lipgloss.Color("#595048"),
		muted:   lipgloss.Color("8"),
		text:    lipgloss.NoColor{},
		accent:  lipgloss.Color("5"),
		red:     lipgloss.Color("1"),
		green:   lipgloss.Color("2"),
		yellow:  lipgloss.Color("3"),
		blue:    lipgloss.Color("4"),
		magenta: lipgloss.Color("5"),
		cyan:    lipgloss.Color("6"),
	}
}

// initTheme queries the terminal colors. Must run before Bubble Tea takes
// over stdin, otherwise the OSC replies are swallowed as key presses.
func initTheme() {
	if os.Getenv("NO_COLOR") != "" {
		return
	}
	out := termenv.NewOutput(os.Stdout)
	bg, okBg := rgbOf(out.BackgroundColor())
	if !okBg {
		return
	}
	fg, okFg := rgbOf(out.ForegroundColor())
	_, _, l := bg.Hcl()
	dark := l < 0.5
	if !okFg {
		if dark {
			fg, _ = colorful.Hex("#d4d4d4")
		} else {
			fg, _ = colorful.Hex("#303030")
		}
	}
	mix := func(t float64) lipgloss.Color {
		return lipgloss.Color(bg.BlendLab(fg, t).Clamped().Hex())
	}
	p := fallbackPalette()
	p.dark = dark
	p.bg = lipgloss.Color(bg.Hex())
	p.surface = mix(0.07)
	p.sel = mix(0.13)
	p.faint = mix(0.30)
	p.muted = mix(0.55)
	th = p
}

func rgbOf(c termenv.Color) (colorful.Color, bool) {
	rgb, ok := c.(termenv.RGBColor)
	if !ok {
		return colorful.Color{}, false
	}
	col, err := colorful.Hex(string(rgb))
	if err != nil {
		return colorful.Color{}, false
	}
	return col, true
}

// ─── style shorthands ────────────────────────────────────────────

func fg(c lipgloss.TerminalColor) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }

var noStyle = lipgloss.NewStyle()

func sMuted() lipgloss.Style  { return fg(th.muted) }
func sFaint() lipgloss.Style  { return fg(th.faint) }
func sAccent() lipgloss.Style { return fg(th.accent) }
func sBold() lipgloss.Style   { return lipgloss.NewStyle().Bold(true) }

// pill renders a compact colored label: dark text on a colored background.
func pill(text string, bg lipgloss.TerminalColor) string {
	return lipgloss.NewStyle().Foreground(th.bg).Background(bg).Bold(true).Padding(0, 1).Render(text)
}

// ─── glyphs ──────────────────────────────────────────────────────
//
// Every glyph below exists in common programming fonts (checked against Fira
// Mono): missing glyphs fall back to other fonts and break the column grid.

const (
	gSel       = "▎"
	gBullet    = "•"
	gDot       = "·"
	gEllipsis  = "…"
	gArrow     = "→"
	gUp        = "↑"
	gDown      = "↓"
	gExpanded  = "▼"
	gCollapsed = "►"
	gTreeMid   = "├"
	gTreeEnd   = "└"
	gTreeBar   = "│"
	gRule      = "─"
	gEnter     = "⏎"
)

var spinnerFrames = []string{"◐", "◓", "◑", "◒"}

// ─── status semantics ────────────────────────────────────────────

// statusKind buckets a status into a visual family. Jira's category covers
// most of it; names refine it (refinement vs backlog, review, blocked).
type statusKind int

const (
	kindTodo statusKind = iota
	kindReady
	kindProgress
	kindReview
	kindBlocked
	kindDone
)

func kindOf(s jira.StatusField) statusKind {
	name := strings.ToLower(s.Name)
	switch {
	case strings.Contains(name, "block"):
		return kindBlocked
	}
	switch s.CategoryKey() {
	case "done":
		return kindDone
	case "indeterminate":
		if strings.Contains(name, "review") || strings.Contains(name, "qa") || strings.Contains(name, "test") {
			return kindReview
		}
		return kindProgress
	case "new":
		if strings.Contains(name, "refine") || strings.Contains(name, "ready") || strings.Contains(name, "selected") {
			return kindReady
		}
		return kindTodo
	}
	// No category (shouldn't happen with the v3 API): guess by name.
	switch {
	case name == "done" || name == "closed" || name == "resolved":
		return kindDone
	case strings.Contains(name, "progress") || strings.Contains(name, "development"):
		return kindProgress
	case strings.Contains(name, "review"):
		return kindReview
	case strings.Contains(name, "refine"):
		return kindReady
	}
	return kindTodo
}

func statusColor(s jira.StatusField) lipgloss.TerminalColor {
	switch kindOf(s) {
	case kindDone:
		return th.green
	case kindProgress:
		return th.yellow
	case kindReview:
		return th.blue
	case kindBlocked:
		return th.red
	case kindReady:
		return th.cyan
	}
	return th.muted
}

func statusGlyph(s jira.StatusField) string {
	switch kindOf(s) {
	case kindDone:
		return "●"
	case kindProgress:
		return "◐"
	case kindReview:
		return "◑"
	case kindBlocked:
		return "◉"
	case kindReady:
		return "◎"
	}
	return "○"
}

// statusIcon is the colored glyph alone.
func statusIcon(s jira.StatusField) string {
	return fg(statusColor(s)).Render(statusGlyph(s))
}

// statusLabel is glyph + name, colored.
func statusLabel(s jira.StatusField) string {
	return fg(statusColor(s)).Render(statusGlyph(s) + " " + s.Name)
}

// workflowRank orders statuses left-to-right as work flows: used for board
// columns and for sorting status groups.
func workflowRank(s jira.StatusField) int {
	name := strings.ToLower(s.Name)
	switch kindOf(s) {
	case kindTodo:
		if strings.Contains(name, "backlog") {
			return 0
		}
		return 1
	case kindReady:
		return 2
	case kindProgress:
		return 10
	case kindReview:
		return 11
	case kindBlocked:
		return 12
	case kindDone:
		if name == "done" {
			return 20
		}
		return 21
	}
	return 5
}

// groupRank orders status groups in the list: what's being worked on first,
// then what's next (refinement before backlog), then what's finished.
func groupRank(s jira.StatusField) int {
	switch kindOf(s) {
	case kindProgress:
		return 0
	case kindReview:
		return 1
	case kindBlocked:
		return 2
	case kindReady:
		return 3
	case kindTodo:
		return 4
	case kindDone:
		return 6
	}
	return 5
}

// ─── issue type & priority ───────────────────────────────────────

func isEpic(t jira.IssueTypeField) bool {
	return t.HierarchyLevel >= 1 || strings.EqualFold(t.Name, "epic")
}

func isSubtask(t jira.IssueTypeField) bool {
	if t.Subtask || t.HierarchyLevel < 0 {
		return true
	}
	n := strings.ToLower(t.Name)
	return strings.Contains(n, "sub-task") || strings.Contains(n, "subtask") || strings.Contains(n, "subtarea")
}

func typeGlyph(t jira.IssueTypeField) (string, lipgloss.TerminalColor) {
	n := strings.ToLower(t.Name)
	switch {
	case isEpic(t):
		return "◆", th.magenta
	case isSubtask(t):
		return "▪", th.cyan
	case strings.Contains(n, "bug") || strings.Contains(n, "incident"):
		return "▣", th.red
	case strings.Contains(n, "story"):
		return "▮", th.green
	case strings.Contains(n, "task") || strings.Contains(n, "tarea"):
		return "■", th.blue
	}
	return "□", th.muted
}

func typeIcon(t jira.IssueTypeField) string {
	g, c := typeGlyph(t)
	return fg(c).Render(g)
}

// priorityLevel maps a priority name to -2..2 (0 = the default, Medium).
func priorityLevel(p *jira.NameField) int {
	if p == nil {
		return 0
	}
	switch strings.ToLower(p.Name) {
	case "highest", "blocker", "critical", "urgent":
		return 2
	case "high", "major":
		return 1
	case "low", "minor":
		return -1
	case "lowest", "trivial":
		return -2
	}
	return 0
}

// priorityMark is a 2-cell indicator, blank for the default priority so the
// eye only catches the exceptions.
func priorityMark(p *jira.NameField) string {
	switch priorityLevel(p) {
	case 2:
		return fg(th.red).Bold(true).Render(gUp + gUp)
	case 1:
		return fg(th.yellow).Render(" " + gUp)
	case -1:
		return fg(th.blue).Render(" " + gDown)
	case -2:
		return fg(th.muted).Render(gDown + gDown)
	}
	return "  "
}

func priorityLabel(p *jira.NameField) string {
	if p == nil {
		return sMuted().Render("—")
	}
	switch priorityLevel(p) {
	case 2:
		return fg(th.red).Bold(true).Render(gUp + gUp + " " + p.Name)
	case 1:
		return fg(th.yellow).Render(gUp + " " + p.Name)
	case -1:
		return fg(th.blue).Render(gDown + " " + p.Name)
	case -2:
		return fg(th.muted).Render(gDown + gDown + " " + p.Name)
	}
	return p.Name
}

// statusSample builds a status for legends and tests.
func statusSample(category, name string) jira.StatusField {
	return jira.StatusField{Name: name, StatusCategory: &jira.StatusCategory{Key: category}}
}

func namePtr(n string) *jira.NameField { return &jira.NameField{Name: n} }
