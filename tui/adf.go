package tui

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// A dedicated ADF → terminal renderer. jira.ADFToText flattens documents to
// plain markdown-ish text and drops inline marks (code, bold, links), which
// is fine for LLM prompts but loses most of what makes a description
// readable. This walks the ADF tree directly and wraps styled text to the
// exact pane width, so nothing is ever cut at the right edge.

type adfNode struct {
	Type    string         `json:"type"`
	Text    string         `json:"text"`
	Content []adfNode      `json:"content"`
	Attrs   map[string]any `json:"attrs"`
	Marks   []adfMark      `json:"marks"`
}

type adfMark struct {
	Type  string         `json:"type"`
	Attrs map[string]any `json:"attrs"`
}

// renderADF renders a description/comment body to lines of at most width
// cells. Plain-string bodies (legacy API) are wrapped as paragraphs.
func renderADF(raw json.RawMessage, width int) []string {
	if width < 10 {
		width = 10
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return renderPlainText(s, width)
	}
	var doc adfNode
	if err := json.Unmarshal(raw, &doc); err != nil {
		return renderPlainText(string(raw), width)
	}
	r := adfRenderer{}
	lines := r.blocks(doc.Content, width, 0)
	// Trim trailing blank lines.
	for len(lines) > 0 && strings.TrimSpace(ansi.Strip(lines[len(lines)-1])) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func renderPlainText(s string, width int) []string {
	return wrapPlain(strings.TrimRight(s, "\n"), width)
}

// runsBlank reports whether runs would render as nothing but whitespace.
func runsBlank(runs []run) bool {
	for _, r := range runs {
		if r.keep || (!r.brk && strings.TrimSpace(r.text) != "") {
			return false
		}
	}
	return true
}

// adfIsEmpty reports whether a body has no visible content.
func adfIsEmpty(raw json.RawMessage) bool {
	return len(renderADF(raw, 80)) == 0
}

type adfRenderer struct{}

// ─── inline model ────────────────────────────────────────────────

// run is a span of text sharing one style. brk marks a hard line break.
type run struct {
	text  string
	style lipgloss.Style
	link  string
	brk   bool
	keep  bool // atomic: never split across lines (pills)
}

var issueKeyRe = regexp.MustCompile(`\b[A-Z][A-Z0-9]{1,9}-\d+\b`)

func (r *adfRenderer) inline(nodes []adfNode) []run {
	var runs []run
	for _, n := range nodes {
		switch n.Type {
		case "text":
			runs = append(runs, textRuns(n)...)
		case "hardBreak":
			runs = append(runs, run{brk: true})
		case "mention":
			name := attrStr(n.Attrs, "text")
			if name == "" {
				name = "@someone"
			}
			if !strings.HasPrefix(name, "@") {
				name = "@" + name
			}
			runs = append(runs, run{text: name, style: fg(th.accent).Bold(true)})
		case "emoji":
			t := attrStr(n.Attrs, "text")
			if t == "" {
				t = attrStr(n.Attrs, "shortName")
			}
			runs = append(runs, run{text: t})
		case "inlineCard":
			u := attrStr(n.Attrs, "url")
			runs = append(runs, run{text: shortURL(u), style: linkStyle(), link: u})
		case "status":
			runs = append(runs, run{text: statusLozenge(n.Attrs), keep: true})
		case "date":
			runs = append(runs, run{text: adfDate(attrStr(n.Attrs, "timestamp")), style: fg(th.cyan)})
		case "placeholder":
			runs = append(runs, run{text: attrStr(n.Attrs, "text"), style: sMuted().Italic(true)})
		case "mediaInline":
			runs = append(runs, run{text: "[attachment]", style: sMuted()})
		case "inlineExtension":
			runs = append(runs, run{text: "[" + attrStr(n.Attrs, "extensionKey") + "]", style: sMuted()})
		default:
			runs = append(runs, r.inline(n.Content)...)
		}
	}
	return runs
}

// textRuns applies marks to a text node. Issue keys in unmarked text get a
// subtle highlight so references stand out when scanning.
func textRuns(n adfNode) []run {
	st := lipgloss.NewStyle()
	link := ""
	code := false
	for _, m := range n.Marks {
		switch m.Type {
		case "strong":
			st = st.Bold(true)
		case "em":
			st = st.Italic(true)
		case "underline":
			st = st.Underline(true)
		case "strike":
			st = st.Strikethrough(true).Foreground(th.muted)
		case "code":
			code = true
			st = st.Foreground(th.cyan).Background(th.surface)
		case "link":
			link = attrStr(m.Attrs, "href")
			st = st.Inherit(linkStyle())
		}
	}
	if code || link != "" {
		return []run{{text: n.Text, style: st, link: link}}
	}
	var runs []run
	last := 0
	for _, loc := range issueKeyRe.FindAllStringIndex(n.Text, -1) {
		if loc[0] > last {
			runs = append(runs, run{text: n.Text[last:loc[0]], style: st})
		}
		runs = append(runs, run{text: n.Text[loc[0]:loc[1]], style: st.Foreground(th.accent)})
		last = loc[1]
	}
	if last < len(n.Text) {
		runs = append(runs, run{text: n.Text[last:], style: st})
	}
	return runs
}

func linkStyle() lipgloss.Style { return lipgloss.NewStyle().Foreground(th.blue).Underline(true) }

// hyperlink wraps s in an OSC 8 sequence: cmd-click opens it in iTerm2,
// kitty, WezTerm, Ghostty... Terminals without support ignore it.
func hyperlink(s, target string) string {
	if target == "" || strings.ContainsAny(target, "\x1b\x07") {
		return s
	}
	return "\x1b]8;;" + target + "\x1b\\" + s + "\x1b]8;;\x1b\\"
}

func shortURL(u string) string {
	p, err := url.Parse(u)
	if err != nil || p.Host == "" {
		return u
	}
	s := strings.TrimPrefix(p.Host, "www.") + p.EscapedPath()
	if len(s) > 1 && strings.HasSuffix(s, "/") {
		s = strings.TrimSuffix(s, "/")
	}
	return trunc(s, 48)
}

func statusLozenge(attrs map[string]any) string {
	text := strings.ToUpper(attrStr(attrs, "text"))
	c := th.muted
	switch attrStr(attrs, "color") {
	case "green":
		c = th.green
	case "yellow":
		c = th.yellow
	case "red":
		c = th.red
	case "blue":
		c = th.blue
	case "purple":
		c = th.magenta
	}
	return pill(text, c)
}

func adfDate(ts string) string {
	ms, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return ts
	}
	return time.UnixMilli(ms).UTC().Format("2 Jan 2006")
}

func attrStr(a map[string]any, k string) string {
	if a == nil {
		return ""
	}
	switch v := a[k].(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return ""
}

func attrInt(a map[string]any, k string, def int) int {
	if v, ok := a[k].(float64); ok {
		return int(v)
	}
	return def
}

// ─── wrapping ────────────────────────────────────────────────────

type token struct {
	text  string
	style lipgloss.Style
	link  string
	space bool // a single separating space
	brk   bool
	keep  bool
}

func tokenize(runs []run) []token {
	var toks []token
	for _, rn := range runs {
		if rn.brk {
			toks = append(toks, token{brk: true})
			continue
		}
		if rn.keep {
			toks = append(toks, token{text: rn.text, keep: true})
			continue
		}
		parts := strings.Split(rn.text, "\n")
		for pi, part := range parts {
			if pi > 0 {
				toks = append(toks, token{brk: true})
			}
			word := strings.Builder{}
			flush := func() {
				if word.Len() > 0 {
					toks = append(toks, token{text: word.String(), style: rn.style, link: rn.link})
					word.Reset()
				}
			}
			for _, ch := range part {
				if ch == ' ' || ch == '\t' {
					flush()
					toks = append(toks, token{space: true, style: rn.style, link: rn.link})
					continue
				}
				word.WriteRune(ch)
			}
			flush()
		}
	}
	return toks
}

// wrapRuns word-wraps styled runs to w cells. Each output line is
// self-contained (every styled segment closes its own escape sequence), so
// lines can be prefixed, padded and composited freely.
func wrapRuns(runs []run, w int) []string {
	if w < 1 {
		w = 1
	}
	toks := tokenize(runs)
	var lines []string
	var cur []token
	curW := 0

	emit := func() {
		// Drop trailing spaces.
		for len(cur) > 0 && cur[len(cur)-1].space {
			cur = cur[:len(cur)-1]
		}
		lines = append(lines, renderTokens(cur))
		cur = nil
		curW = 0
	}

	pendingSpace := token{}
	hasPending := false
	for _, t := range toks {
		switch {
		case t.brk:
			emit()
			hasPending = false
			continue
		case t.space:
			if curW > 0 {
				pendingSpace = t
				hasPending = true
			}
			continue
		}
		tw := sw(t.text)
		need := tw
		if hasPending {
			need++
		}
		if curW > 0 && curW+need > w {
			emit()
			hasPending = false
			need = tw
		}
		if hasPending {
			cur = append(cur, pendingSpace)
			curW++
			hasPending = false
		}
		// Hard-break words longer than a whole line.
		for tw > w-curW && !t.keep {
			room := w - curW
			if room <= 0 {
				emit()
				continue
			}
			head := ansi.Truncate(t.text, room, "")
			if head == "" {
				if curW > 0 {
					emit()
					continue
				}
				head = string([]rune(t.text)[:1])
			}
			part := t
			part.text = head
			cur = append(cur, part)
			emit()
			t.text = strings.TrimPrefix(t.text, head)
			tw = sw(t.text)
		}
		if t.text == "" {
			continue
		}
		cur = append(cur, t)
		curW += tw
	}
	if len(cur) > 0 || len(lines) == 0 {
		emit()
	}
	// Safety net: atomic tokens (pills) wider than the pane.
	for i, l := range lines {
		if sw(l) > w {
			lines[i] = ansi.Truncate(l, w, "")
		}
	}
	return lines
}

// renderTokens merges adjacent tokens sharing a style into single styled
// segments (fewer escape sequences, cheaper rendering).
func renderTokens(toks []token) string {
	var b strings.Builder
	i := 0
	for i < len(toks) {
		t := toks[i]
		if t.keep {
			b.WriteString(t.text)
			i++
			continue
		}
		var seg strings.Builder
		j := i
		for j < len(toks) && !toks[j].keep && sameStyle(toks[j], t) {
			if toks[j].space {
				seg.WriteByte(' ')
			} else {
				seg.WriteString(toks[j].text)
			}
			j++
		}
		s := t.style.Render(seg.String())
		if t.link != "" {
			s = hyperlink(s, t.link)
		}
		b.WriteString(s)
		i = j
	}
	return b.String()
}

func sameStyle(a, b token) bool {
	return a.link == b.link && a.style.GetBold() == b.style.GetBold() &&
		a.style.GetItalic() == b.style.GetItalic() &&
		a.style.GetUnderline() == b.style.GetUnderline() &&
		a.style.GetStrikethrough() == b.style.GetStrikethrough() &&
		a.style.GetForeground() == b.style.GetForeground() &&
		a.style.GetBackground() == b.style.GetBackground()
}

// ─── blocks ──────────────────────────────────────────────────────

// blocks renders a sequence of block nodes, separated by blank lines
// (except right after a heading, which hugs its content).
func (r *adfRenderer) blocks(nodes []adfNode, w, depth int) []string {
	var out []string
	prevHeading := false
	for _, n := range nodes {
		lines := r.block(n, w, depth)
		if len(lines) == 0 {
			continue
		}
		if len(out) > 0 && !prevHeading {
			out = append(out, "")
		}
		out = append(out, lines...)
		prevHeading = n.Type == "heading"
	}
	return out
}

func (r *adfRenderer) block(n adfNode, w, depth int) []string {
	switch n.Type {
	case "paragraph":
		runs := r.inline(n.Content)
		if runsBlank(runs) {
			return nil
		}
		return wrapRuns(runs, w)

	case "heading":
		level := attrInt(n.Attrs, "level", 1)
		runs := r.inline(n.Content)
		var c lipgloss.TerminalColor
		switch {
		case level <= 2:
			c = th.yellow
		case level == 3:
			c = th.green
		}
		for i := range runs {
			st := runs[i].style.Bold(true)
			// Inline code and links keep their own color inside headings.
			_, plainBg := st.GetBackground().(lipgloss.NoColor)
			if c != nil && plainBg && runs[i].link == "" {
				st = st.Foreground(c)
			}
			runs[i].style = st
		}
		return wrapRuns(runs, w)

	case "bulletList", "orderedList":
		return r.list(n, w, depth)

	case "taskList", "decisionList":
		return r.taskList(n, w, depth)

	case "codeBlock":
		return r.codeBlock(n, w)

	case "blockquote":
		bar := fg(th.faint).Render("▌") + " "
		return prefixLines(r.blocks(n.Content, w-2, depth), bar, bar)

	case "panel":
		kind := attrStr(n.Attrs, "panelType")
		if kind == "" {
			kind = "info"
		}
		c := panelColor(kind)
		bar := fg(c).Render("▌") + " "
		label := fg(c).Bold(true).Render(strings.ToUpper(kind))
		body := r.blocks(n.Content, w-2, depth)
		return prefixLines(append([]string{label}, body...), bar, bar)

	case "rule":
		return []string{sFaint().Render(strings.Repeat(gRule, w))}

	case "table":
		return r.table(n, w)

	case "mediaSingle", "mediaGroup":
		var out []string
		for _, c := range n.Content {
			out = append(out, r.block(c, w, depth)...)
		}
		return out

	case "media":
		label := "attachment"
		if alt := attrStr(n.Attrs, "alt"); alt != "" {
			label = alt
		}
		if u := attrStr(n.Attrs, "url"); u != "" {
			return []string{trunc(hyperlink(linkStyle().Render(shortURL(u)), u), w)}
		}
		return []string{trunc(sMuted().Render("▣ "+label+" (open in browser)"), w)}

	case "expand", "nestedExpand":
		title := attrStr(n.Attrs, "title")
		if title == "" {
			title = "Details"
		}
		head := sMuted().Render(gExpanded) + " " + sBold().Render(trunc(title, w-2))
		return append([]string{head}, prefixLines(r.blocks(n.Content, w-2, depth), "  ", "  ")...)

	case "blockCard", "embedCard":
		u := attrStr(n.Attrs, "url")
		return []string{trunc(hyperlink(linkStyle().Render(shortURL(u)), u), w)}

	case "layoutSection", "layoutColumn", "bodiedExtension", "doc":
		return r.blocks(n.Content, w, depth)

	case "extension":
		return []string{sMuted().Render(trunc("["+attrStr(n.Attrs, "extensionKey")+"]", w))}
	}

	// Unknown node: render children as blocks when they look like blocks,
	// otherwise as an inline paragraph.
	if len(n.Content) > 0 && isBlockType(n.Content[0].Type) {
		return r.blocks(n.Content, w, depth)
	}
	if runs := r.inline([]adfNode{n}); len(runs) > 0 {
		return wrapRuns(runs, w)
	}
	return nil
}

func isBlockType(t string) bool {
	switch t {
	case "paragraph", "heading", "bulletList", "orderedList", "taskList", "decisionList",
		"codeBlock", "blockquote", "panel", "rule", "table", "mediaSingle", "mediaGroup",
		"expand", "nestedExpand", "blockCard", "embedCard", "layoutSection", "layoutColumn":
		return true
	}
	return false
}

func panelColor(kind string) lipgloss.TerminalColor {
	switch strings.ToLower(kind) {
	case "warning":
		return th.yellow
	case "error":
		return th.red
	case "success":
		return th.green
	case "note":
		return th.magenta
	case "tip":
		return th.cyan
	}
	return th.blue
}

// prefixLines prefixes the first line with first and the rest with rest.
func prefixLines(lines []string, first, rest string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		if i == 0 {
			out[i] = first + l
		} else {
			out[i] = rest + l
		}
	}
	return out
}

var bulletGlyphs = []string{"•", "–", gDot}

func (r *adfRenderer) list(n adfNode, w, depth int) []string {
	ordered := n.Type == "orderedList"
	start := attrInt(n.Attrs, "order", 1)
	numW := len(strconv.Itoa(start + len(n.Content) - 1))
	var out []string
	for i, item := range n.Content {
		var marker string
		if ordered {
			marker = fg(th.accent).Render(fmt.Sprintf("%*d.", numW, start+i)) + " "
		} else {
			marker = fg(th.accent).Render(bulletGlyphs[depth%len(bulletGlyphs)]) + " "
		}
		mw := sw(marker)
		body := r.listItem(item, w-mw, depth+1)
		if len(body) == 0 {
			body = []string{""}
		}
		out = append(out, prefixLines(body, marker, strings.Repeat(" ", mw))...)
	}
	return out
}

// listItem renders an item's blocks without blank lines between them, so a
// paragraph and its nested list stay together.
func (r *adfRenderer) listItem(item adfNode, w, depth int) []string {
	var out []string
	for _, c := range item.Content {
		out = append(out, r.block(c, w, depth)...)
	}
	return out
}

func (r *adfRenderer) taskList(n adfNode, w, depth int) []string {
	var out []string
	for _, item := range n.Content {
		switch item.Type {
		case "taskList", "decisionList":
			out = append(out, prefixLines(r.taskList(item, w-2, depth+1), "  ", "  ")...)
			continue
		}
		state := attrStr(item.Attrs, "state")
		var marker string
		runs := r.inline(item.Content)
		switch {
		case item.Type == "decisionItem":
			marker = fg(th.magenta).Render("◆") + " "
		case state == "DONE":
			marker = fg(th.green).Render("▣") + " "
			for i := range runs {
				runs[i].style = runs[i].style.Foreground(th.muted)
			}
		default:
			marker = sMuted().Render("□") + " "
		}
		body := wrapRuns(runs, w-2)
		out = append(out, prefixLines(body, marker, "  ")...)
	}
	return out
}

func (r *adfRenderer) codeBlock(n adfNode, w int) []string {
	var src strings.Builder
	for _, c := range n.Content {
		src.WriteString(c.Text)
	}
	text := strings.TrimRight(strings.ReplaceAll(src.String(), "\t", "    "), "\n")
	inner := w - 2
	st := lipgloss.NewStyle().Foreground(th.cyan)
	var out []string
	for _, line := range strings.Split(text, "\n") {
		chunks := []string{line}
		if sw(line) > inner {
			chunks = strings.Split(ansi.Hardwrap(line, inner, true), "\n")
		}
		for _, c := range chunks {
			out = append(out, paintBg(" "+st.Render(fit(c, inner))+" ", th.surface))
		}
	}
	return out
}

// ─── tables ──────────────────────────────────────────────────────

type tcell struct {
	lines  [][]run // logical lines (split at paragraphs / hard breaks)
	header bool
}

func (r *adfRenderer) cell(n adfNode) tcell {
	c := tcell{header: n.Type == "tableHeader"}
	var cur []run
	flush := func() {
		if len(cur) > 0 {
			c.lines = append(c.lines, cur)
			cur = nil
		}
	}
	var walk func(nodes []adfNode, prefix string)
	walk = func(nodes []adfNode, prefix string) {
		for _, b := range nodes {
			switch b.Type {
			case "paragraph", "heading":
				flush()
				if prefix != "" {
					cur = append(cur, run{text: prefix, style: fg(th.accent)})
				}
				for _, rn := range r.inline(b.Content) {
					if rn.brk {
						flush()
						continue
					}
					cur = append(cur, rn)
				}
				flush()
			case "bulletList", "orderedList", "taskList":
				for _, it := range b.Content {
					walk(it.Content, "• ")
				}
			case "listItem", "taskItem":
				walk(b.Content, "• ")
			default:
				if isBlockType(b.Type) {
					walk(b.Content, prefix)
				} else {
					cur = append(cur, r.inline([]adfNode{b})...)
				}
			}
		}
		flush()
	}
	walk(n.Content, "")
	if c.header {
		for _, l := range c.lines {
			for i := range l {
				l[i].style = l[i].style.Bold(true)
			}
		}
	}
	return c
}

func runsWidth(rs []run) int {
	n := 0
	for _, r := range rs {
		n += sw(r.text)
	}
	return n
}

func longestWord(rs []run) int {
	best := 0
	for _, t := range tokenize(rs) {
		if !t.space && !t.brk {
			best = max(best, sw(t.text))
		}
	}
	return best
}

func (r *adfRenderer) table(n adfNode, w int) []string {
	var rows [][]tcell
	cols := 0
	for _, row := range n.Content {
		var cells []tcell
		for _, c := range row.Content {
			cells = append(cells, r.cell(c))
		}
		if len(cells) > 0 {
			rows = append(rows, cells)
			cols = max(cols, len(cells))
		}
	}
	if len(rows) == 0 {
		return nil
	}
	for i := range rows {
		for len(rows[i]) < cols {
			rows[i] = append(rows[i], tcell{})
		}
	}

	// Natural and minimum widths per column.
	nat := make([]int, cols)
	minw := make([]int, cols)
	for _, row := range rows {
		for j, c := range row {
			for _, l := range c.lines {
				nat[j] = max(nat[j], runsWidth(l))
				minw[j] = max(minw[j], min(longestWord(l), 14))
			}
		}
	}
	for j := range nat {
		nat[j] = max(nat[j], 1)
		minw[j] = max(min(minw[j], nat[j]), 3)
	}

	avail := w - (cols + 1) - 2*cols
	sumMin := 0
	for _, m := range minw {
		sumMin += m
	}
	if avail < sumMin {
		return r.tableRecords(rows, w)
	}
	widths := allocate(nat, minw, avail)

	bs := sFaint()
	hline := func(l, m, rr string) string {
		var b strings.Builder
		b.WriteString(l)
		for j, cw := range widths {
			if j > 0 {
				b.WriteString(m)
			}
			b.WriteString(strings.Repeat("─", cw+2))
		}
		b.WriteString(rr)
		return bs.Render(b.String())
	}

	// Wrap every cell first; separators between body rows only pay off when
	// some row spans several lines.
	wrapped := make([][][]string, len(rows))
	multi := false
	for i, row := range rows {
		wrapped[i] = make([][]string, cols)
		for j, c := range row {
			var ls []string
			for _, l := range c.lines {
				ls = append(ls, wrapRuns(l, widths[j])...)
			}
			if len(ls) > 1 {
				multi = true
			}
			wrapped[i][j] = ls
		}
	}

	out := []string{hline("┌", "┬", "┐")}
	for i, row := range rows {
		h := 1
		for j := range row {
			h = max(h, len(wrapped[i][j]))
		}
		for k := 0; k < h; k++ {
			var b strings.Builder
			b.WriteString(bs.Render("│"))
			for j := range row {
				text := ""
				if k < len(wrapped[i][j]) {
					text = wrapped[i][j][k]
				}
				b.WriteString(" " + fit(text, widths[j]) + " " + bs.Render("│"))
			}
			out = append(out, b.String())
		}
		last := i == len(rows)-1
		isHeader := len(row) > 0 && row[0].header
		if !last && (isHeader || multi) {
			out = append(out, hline("├", "┼", "┤"))
		}
	}
	out = append(out, hline("└", "┴", "┘"))
	return out
}

// allocate distributes avail cells across columns: every column gets its
// minimum, then the remainder goes to columns that want more, in proportion
// to how much more they want, never beyond their natural width.
func allocate(nat, minw []int, avail int) []int {
	n := len(nat)
	widths := make([]int, n)
	total := 0
	for j := range nat {
		widths[j] = minw[j]
		total += minw[j]
	}
	for total < avail {
		want := 0
		for j := range nat {
			want += nat[j] - widths[j]
		}
		if want <= 0 {
			break
		}
		room := avail - total
		gave := 0
		for j := range nat {
			d := nat[j] - widths[j]
			if d <= 0 {
				continue
			}
			g := d * room / want
			if g == 0 {
				g = 1
			}
			g = min(g, d, avail-total-gave)
			widths[j] += g
			gave += g
			if total+gave >= avail {
				break
			}
		}
		if gave == 0 {
			break
		}
		total += gave
	}
	return widths
}

// tableRecords is the fallback for tables too wide for the pane: each row
// becomes a block of "Header: value" lines.
func (r *adfRenderer) tableRecords(rows [][]tcell, w int) []string {
	var headers []string
	body := rows
	if len(rows) > 0 && rows[0][0].header {
		for _, c := range rows[0] {
			var parts []string
			for _, l := range c.lines {
				for _, rn := range l {
					parts = append(parts, rn.text)
				}
			}
			headers = append(headers, strings.Join(parts, " "))
		}
		body = rows[1:]
	}
	var out []string
	for i, row := range body {
		if i > 0 {
			out = append(out, sFaint().Render(strings.Repeat("╌", min(w, 12))))
		}
		for j, c := range row {
			label := ""
			if j < len(headers) && headers[j] != "" {
				label = headers[j] + ": "
			}
			var runs []run
			if label != "" {
				runs = append(runs, run{text: label, style: sMuted().Bold(true)})
			}
			for k, l := range c.lines {
				if k > 0 {
					runs = append(runs, run{text: " "})
				}
				runs = append(runs, l...)
			}
			out = append(out, wrapRuns(runs, w)...)
		}
	}
	return out
}
