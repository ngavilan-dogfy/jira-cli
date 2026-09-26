package tui

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/ngavilan-dogfy/jira-cli/jira"
)

// Editing a description in $EDITOR means ADF → markdown → (edit) → ADF.
// jira.MarkdownToADF only understands a subset of ADF, so a document can
// only be offered for editing if it survives the trip unchanged: we
// serialize it, parse it back and compare. Anything that doesn't come back
// identical (mentions, nested lists, hard breaks, media, web-UI table
// widths…) is refused rather than silently flattened.

// editableMarkdown returns the description as markdown, and whether it
// round-trips losslessly. Empty descriptions are always editable.
func editableMarkdown(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", true
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, false // legacy plain text: saving would change its format
	}
	var doc adfNode
	if err := json.Unmarshal(raw, &doc); err != nil || doc.Type != "doc" {
		return "", false
	}
	md, ok := blocksToMD(doc.Content)
	if !ok {
		return md, false
	}
	back, err := json.Marshal(jira.MarkdownToADF(md))
	if err != nil {
		return md, false
	}
	return md, adfEquivalent(raw, back)
}

// adfEquivalent compares two ADF documents ignoring what Jira regenerates
// or MarkdownToADF can't express but doesn't matter (random localIds, the
// version field, how text is split into nodes).
func adfEquivalent(a, b json.RawMessage) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	return reflect.DeepEqual(normADF(x), normADF(y))
}

func normADF(v any) any {
	switch n := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, val := range n {
			switch k {
			case "version":
				continue
			case "attrs":
				if a := normAttrs(val); len(a) > 0 {
					out[k] = a
				}
				continue
			case "marks":
				if ms := normMarks(val); len(ms) > 0 {
					out[k] = ms
				}
				continue
			case "content":
				if c := normContent(val); len(c) > 0 {
					out[k] = c
				}
				continue
			}
			out[k] = normADF(val)
		}
		return out
	case []any:
		out := make([]any, len(n))
		for i, e := range n {
			out[i] = normADF(e)
		}
		return out
	}
	return v
}

func normAttrs(v any) map[string]any {
	m, _ := v.(map[string]any)
	out := map[string]any{}
	for k, val := range m {
		if k == "localId" || val == nil || val == "" {
			continue
		}
		if k == "order" && val == float64(1) {
			continue
		}
		out[k] = normADF(val)
	}
	return out
}

func normMarks(v any) []any {
	list, _ := v.([]any)
	out := make([]any, 0, len(list))
	for _, m := range list {
		out = append(out, normADF(m))
	}
	sort.Slice(out, func(i, j int) bool {
		a, _ := json.Marshal(out[i])
		b, _ := json.Marshal(out[j])
		return string(a) < string(b)
	})
	return out
}

// normContent normalizes children and merges adjacent text nodes carrying
// the same marks ("a " + "b" ≡ "a b"); empty text nodes and empty
// paragraphs (pure spacing) vanish.
func normContent(v any) []any {
	list, _ := v.([]any)
	var out []any
	for _, c := range list {
		n := normADF(c)
		m, isMap := n.(map[string]any)
		if isMap && m["type"] == "paragraph" && m["content"] == nil {
			continue
		}
		if isMap && m["type"] == "text" {
			if t, _ := m["text"].(string); t == "" {
				continue
			}
			if len(out) > 0 {
				if prev, ok := out[len(out)-1].(map[string]any); ok && prev["type"] == "text" &&
					reflect.DeepEqual(prev["marks"], m["marks"]) {
					merged := map[string]any{}
					for k, val := range prev {
						merged[k] = val
					}
					merged["text"] = prev["text"].(string) + m["text"].(string)
					out[len(out)-1] = merged
					continue
				}
			}
		}
		out = append(out, n)
	}
	return out
}

// ─── serializer ──────────────────────────────────────────────────
// Emits exactly the markdown dialect jira.MarkdownToADF parses. ok=false
// means a node has no representation at all; subtler losses are caught by
// the round-trip comparison.

func blocksToMD(nodes []adfNode) (string, bool) {
	var parts []string
	for _, n := range nodes {
		s, ok := blockToMD(n)
		if !ok {
			return "", false
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, "\n\n"), true
}

func blockToMD(n adfNode) (string, bool) {
	switch n.Type {
	case "paragraph":
		return inlineToMD(n.Content)
	case "heading":
		s, ok := inlineToMD(n.Content)
		return strings.Repeat("#", attrInt(n.Attrs, "level", 1)) + " " + s, ok
	case "bulletList", "orderedList":
		var lines []string
		for i, it := range n.Content {
			if it.Type != "listItem" || len(it.Content) != 1 || it.Content[0].Type != "paragraph" {
				return "", false // nested lists / multi-block items
			}
			s, ok := inlineToMD(it.Content[0].Content)
			if !ok {
				return "", false
			}
			marker := "- "
			if n.Type == "orderedList" {
				marker = fmt.Sprintf("%d. ", i+1)
			}
			lines = append(lines, marker+s)
		}
		return strings.Join(lines, "\n"), true
	case "taskList":
		var lines []string
		for _, it := range n.Content {
			if it.Type != "taskItem" {
				return "", false
			}
			s, ok := inlineToMD(it.Content)
			if !ok {
				return "", false
			}
			box := "[ ]"
			if attrStr(it.Attrs, "state") == "DONE" {
				box = "[x]"
			}
			lines = append(lines, "- "+box+" "+s)
		}
		return strings.Join(lines, "\n"), true
	case "codeBlock":
		var b strings.Builder
		for _, c := range n.Content {
			b.WriteString(c.Text)
		}
		return "```" + attrStr(n.Attrs, "language") + "\n" + b.String() + "\n```", true
	case "blockquote":
		inner, ok := blocksToMD(n.Content)
		if !ok {
			return "", false
		}
		lines := strings.Split(inner, "\n")
		for i, l := range lines {
			if l == "" {
				lines[i] = ">"
			} else {
				lines[i] = "> " + l
			}
		}
		return strings.Join(lines, "\n"), true
	case "panel":
		inner, ok := blocksToMD(n.Content)
		return ":::" + attrStr(n.Attrs, "panelType") + "\n" + inner + "\n:::", ok
	case "rule":
		return "---", true
	case "table":
		var rows []string
		for ri, row := range n.Content {
			var cells []string
			for _, c := range row.Content {
				if len(c.Content) > 1 || (len(c.Content) == 1 && c.Content[0].Type != "paragraph") {
					return "", false
				}
				s := ""
				if len(c.Content) == 1 {
					var ok bool
					if s, ok = inlineToMD(c.Content[0].Content); !ok {
						return "", false
					}
				}
				cells = append(cells, s)
			}
			rows = append(rows, "| "+strings.Join(cells, " | ")+" |")
			if ri == 0 {
				seps := make([]string, len(cells))
				for i := range seps {
					seps[i] = "---"
				}
				rows = append(rows, "| "+strings.Join(seps, " | ")+" |")
			}
		}
		return strings.Join(rows, "\n"), true
	}
	return "", false
}

func inlineToMD(nodes []adfNode) (string, bool) {
	var b strings.Builder
	for _, n := range nodes {
		if n.Type != "text" {
			return "", false // mentions, hard breaks, cards, emoji, dates…
		}
		b.WriteString(markToMD(n))
	}
	return b.String(), true
}

func markToMD(n adfNode) string {
	s := n.Text
	var link string
	has := map[string]bool{}
	for _, m := range n.Marks {
		has[m.Type] = true
		if m.Type == "link" {
			link = attrStr(m.Attrs, "href")
		}
	}
	if has["code"] {
		return "`" + s + "`"
	}
	if has["em"] {
		s = "*" + s + "*"
	}
	if has["strike"] {
		s = "~~" + s + "~~"
	}
	if has["strong"] {
		s = "**" + s + "**"
	}
	if link != "" {
		s = "[" + s + "](" + link + ")"
	}
	return s
}
