package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// overlay paints the block fgBlock over bg with its top-left corner at
// (x, y). bg must be a normalized frame (see frame). Unlike a naive line
// replacement, the background to the left and right of the block survives.
func overlay(bg, fgBlock string, x, y int) string {
	bgLines := strings.Split(bg, "\n")
	fgLines := strings.Split(fgBlock, "\n")
	for i, fl := range fgLines {
		row := y + i
		if row < 0 || row >= len(bgLines) {
			continue
		}
		bl := bgLines[row]
		if room := sw(bl) - x; sw(fl) > room {
			fl = ansi.Truncate(fl, max(0, room), "") // never widen the frame
		}
		fw := sw(fl)
		left := ansi.Truncate(bl, x, "")
		if n := sw(left); n < x {
			left += strings.Repeat(" ", x-n)
		}
		right := ansi.TruncateLeft(bl, x+fw, "")
		bgLines[row] = left + "\x1b[0m" + fl + "\x1b[0m" + right
	}
	return strings.Join(bgLines, "\n")
}

// center returns the top-left position that centers a w×h block in W×H,
// nudged slightly above the middle (reads better than dead center).
func center(W, H, w, h int) (int, int) {
	x := max(0, (W-w)/2)
	y := max(0, (H-h)/2-H/12)
	return x, y
}

// dim strips all styling from a frame and repaints it in the faint color,
// pushing it visually behind a modal.
func dim(s string) string {
	lines := strings.Split(ansi.Strip(s), "\n")
	st := fg(th.faint)
	for i, l := range lines {
		lines[i] = st.Render(l)
	}
	return strings.Join(lines, "\n")
}

// box draws a bordered panel with the title set into the top border:
//
//	┌─ Title ─────────┐
//	│ body            │
//	└─────────────────┘
//
// w is the total width including borders. Body lines are fitted to the inner
// width (w-4: border + one space of padding on each side).
func box(title, body string, w int, border lipgloss.TerminalColor) string {
	if w < 8 {
		w = 8
	}
	inner := w - 4
	bs := fg(border)
	var b strings.Builder

	if title != "" {
		t := trunc(title, inner-2)
		fill := w - 5 - sw(t)
		b.WriteString(bs.Render("┌─") + " " + sBold().Render(t) + " " +
			bs.Render(strings.Repeat("─", max(0, fill))+"┐"))
	} else {
		b.WriteString(bs.Render("┌" + strings.Repeat("─", w-2) + "┐"))
	}

	for _, l := range strings.Split(body, "\n") {
		b.WriteString("\n" + bs.Render("│") + " " + fit(l, inner) + " " + bs.Render("│"))
	}
	b.WriteString("\n" + bs.Render("└"+strings.Repeat("─", w-2)+"┘"))
	return b.String()
}

// hintBar renders "key label" pairs for footers and modal bottoms, dropping
// pairs from the end when they don't fit in w cells.
func hintBar(hints []hint, w int) string {
	var parts []string
	used := 0
	for _, h := range hints {
		p := sAccent().Bold(true).Render(h.key) + " " + sMuted().Render(h.label)
		pw := sw(p)
		sep := 0
		if len(parts) > 0 {
			sep = 2
		}
		if used+sep+pw > w {
			break
		}
		parts = append(parts, p)
		used += sep + pw
	}
	return strings.Join(parts, "  ")
}

type hint struct{ key, label string }
