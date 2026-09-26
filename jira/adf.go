package jira

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

type adfNode struct {
	Type    string                 `json:"type"`
	Text    string                 `json:"text"`
	Content []adfNode              `json:"content"`
	Attrs   map[string]interface{} `json:"attrs"`
}

// ADFToText converts an Atlassian Document Format JSON blob to plain text.
func ADFToText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}

	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}

	var node adfNode
	if err := json.Unmarshal(raw, &node); err != nil {
		return string(raw)
	}

	return strings.TrimRight(renderNode(node, 0), "\n")
}

func renderNode(node adfNode, indent int) string {
	var sb strings.Builder
	prefix := strings.Repeat("  ", indent)

	switch node.Type {
	case "doc":
		for i, child := range node.Content {
			if i > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString(renderNode(child, indent))
		}

	case "paragraph":
		sb.WriteString(prefix)
		for _, child := range node.Content {
			sb.WriteString(renderNode(child, 0))
		}
		sb.WriteString("\n")

	case "heading":
		level := 1
		if l, ok := node.Attrs["level"].(float64); ok {
			level = int(l)
		}
		sb.WriteString(prefix + strings.Repeat("#", level) + " ")
		for _, child := range node.Content {
			sb.WriteString(renderNode(child, 0))
		}
		sb.WriteString("\n")

	case "text":
		sb.WriteString(node.Text)

	case "hardBreak":
		sb.WriteString("\n" + prefix)

	case "bulletList":
		for _, child := range node.Content {
			sb.WriteString(renderNode(child, indent))
		}

	case "orderedList":
		for _, child := range node.Content {
			sb.WriteString(renderNode(child, indent))
		}

	case "listItem":
		sb.WriteString(prefix + "  * ")
		for i, child := range node.Content {
			if i == 0 {
				text := renderNode(child, 0)
				sb.WriteString(strings.TrimLeft(text, " "))
			} else {
				sb.WriteString(renderNode(child, indent+2))
			}
		}

	case "taskList":
		for _, child := range node.Content {
			sb.WriteString(renderNode(child, indent))
		}

	case "taskItem":
		mark := "[ ]"
		if state, _ := node.Attrs["state"].(string); state == "DONE" {
			mark = "[x]"
		}
		sb.WriteString(prefix + "  " + mark + " ")
		for _, child := range node.Content {
			sb.WriteString(renderNode(child, 0))
		}
		sb.WriteString("\n")

	case "codeBlock":
		sb.WriteString(prefix + "```\n")
		for _, child := range node.Content {
			sb.WriteString(prefix + renderNode(child, 0))
		}
		sb.WriteString("\n" + prefix + "```\n")

	case "blockquote":
		for _, child := range node.Content {
			text := renderNode(child, indent)
			for _, line := range strings.Split(text, "\n") {
				if line != "" {
					sb.WriteString(prefix + "> " + strings.TrimLeft(line, " ") + "\n")
				}
			}
		}

	case "mention":
		if text, ok := node.Attrs["text"].(string); ok {
			sb.WriteString(text)
		}

	case "inlineCard":
		if url, ok := node.Attrs["url"].(string); ok {
			sb.WriteString(url)
		}

	case "emoji":
		if text, ok := node.Attrs["text"].(string); ok {
			sb.WriteString(text)
		} else if shortName, ok := node.Attrs["shortName"].(string); ok {
			sb.WriteString(shortName)
		}

	case "panel":
		panelType, _ := node.Attrs["panelType"].(string)
		if panelType == "" {
			panelType = "info"
		}
		sb.WriteString(prefix + ":::" + panelType + "\n")
		for _, child := range node.Content {
			sb.WriteString(renderNode(child, indent))
		}
		sb.WriteString(prefix + ":::\n")

	case "rule":
		sb.WriteString(prefix + "---\n")

	case "table":
		for _, child := range node.Content {
			sb.WriteString(renderNode(child, indent))
		}

	case "tableRow":
		sb.WriteString(prefix)
		for i, child := range node.Content {
			if i > 0 {
				sb.WriteString(" | ")
			}
			sb.WriteString(strings.TrimSpace(renderNode(child, 0)))
		}
		sb.WriteString("\n")

	case "tableHeader", "tableCell":
		for _, child := range node.Content {
			sb.WriteString(renderNode(child, 0))
		}

	default:
		for _, child := range node.Content {
			sb.WriteString(renderNode(child, indent))
		}
	}

	return sb.String()
}

// MarkdownToADF converts a markdown string to an ADF document map suitable
// for the Jira REST API v3 description/body field.
//
// Supported block constructs:
//   - Headings (# .. ######)
//   - Fenced code blocks (```lang ... ```)
//   - Bullet lists (-, *)
//   - Ordered lists (1. 2.)
//   - Task lists (- [ ], - [x])
//   - GitHub-flavored tables (| h | h |\n|---|---|\n| c | c |)
//   - Blockquotes (>)
//   - Horizontal rules (---)
//   - Panels (:::info / :::warning / :::note / :::success / :::error / :::tip ... :::)
//
// Supported inline marks (composable, e.g. **`code`** works):
//   - Inline code (`code`)
//   - Bold (**bold**)
//   - Italic (*italic*)
//   - Strikethrough (~~strike~~)
//   - Links ([text](url))
func MarkdownToADF(md string) map[string]interface{} {
	lines := strings.Split(md, "\n")
	var content []map[string]interface{}

	ctx := &mdContext{}

	i := 0
	for i < len(lines) {
		line := lines[i]

		// Panel: :::info / :::warning / :::note / :::success / :::error / :::tip
		if m := rePanelOpen.FindStringSubmatch(line); m != nil {
			panelType := strings.ToLower(m[1])
			if _, ok := validPanelTypes[panelType]; ok {
				var inner []string
				i++
				for i < len(lines) && strings.TrimSpace(lines[i]) != ":::" {
					inner = append(inner, lines[i])
					i++
				}
				if i < len(lines) {
					i++ // skip closing :::
				}
				innerDoc := MarkdownToADF(strings.Join(inner, "\n"))
				innerContent, _ := innerDoc["content"].([]map[string]interface{})
				content = append(content, map[string]interface{}{
					"type":    "panel",
					"attrs":   map[string]interface{}{"panelType": panelType},
					"content": innerContent,
				})
				continue
			}
		}

		// Fenced code block
		if strings.HasPrefix(line, "```") {
			lang := strings.TrimSpace(strings.TrimPrefix(line, "```"))
			var codeLines []string
			i++
			for i < len(lines) && !strings.HasPrefix(lines[i], "```") {
				codeLines = append(codeLines, lines[i])
				i++
			}
			if i < len(lines) {
				i++ // skip closing ```
			}
			block := map[string]interface{}{
				"type": "codeBlock",
				"content": []map[string]interface{}{
					{"type": "text", "text": strings.Join(codeLines, "\n")},
				},
			}
			if lang != "" {
				block["attrs"] = map[string]interface{}{"language": lang}
			}
			content = append(content, block)
			continue
		}

		// Heading
		if m := reHeading.FindStringSubmatch(line); m != nil {
			level := len(m[1])
			content = append(content, map[string]interface{}{
				"type":    "heading",
				"attrs":   map[string]interface{}{"level": level},
				"content": parseInline(m[2]),
			})
			i++
			continue
		}

		// Horizontal rule
		if reHRule.MatchString(strings.TrimSpace(line)) {
			content = append(content, map[string]interface{}{"type": "rule"})
			i++
			continue
		}

		// Table — header line followed by separator line
		if isTableRow(line) && i+1 < len(lines) && isTableSeparator(lines[i+1]) {
			block, consumed := parseTable(lines[i:])
			content = append(content, block)
			i += consumed
			continue
		}

		// Blockquote
		if strings.HasPrefix(strings.TrimLeft(line, " "), "> ") || strings.TrimSpace(line) == ">" {
			var quoted []string
			for i < len(lines) {
				l := strings.TrimLeft(lines[i], " ")
				if !strings.HasPrefix(l, ">") {
					break
				}
				quoted = append(quoted, strings.TrimPrefix(strings.TrimPrefix(l, ">"), " "))
				i++
			}
			// Recursively convert the quoted block so nested blocks are preserved
			inner := MarkdownToADF(strings.Join(quoted, "\n"))
			innerContent, _ := inner["content"].([]map[string]interface{})
			content = append(content, map[string]interface{}{
				"type":    "blockquote",
				"content": innerContent,
			})
			continue
		}

		// Task list (- [ ] / - [x]). Detect before bullet so `[ ]` isn't lost.
		if reTaskItem.MatchString(line) {
			var items []map[string]interface{}
			for i < len(lines) {
				m := reTaskItem.FindStringSubmatch(lines[i])
				if m == nil {
					break
				}
				state := "TODO"
				if strings.EqualFold(m[1], "x") {
					state = "DONE"
				}
				items = append(items, map[string]interface{}{
					"type": "taskItem",
					"attrs": map[string]interface{}{
						"localId": ctx.nextLocalID(),
						"state":   state,
					},
					"content": parseInline(m[2]),
				})
				i++
			}
			content = append(content, map[string]interface{}{
				"type":    "taskList",
				"attrs":   map[string]interface{}{"localId": ctx.nextLocalID()},
				"content": items,
			})
			continue
		}

		// Ordered list
		if reOrdered.MatchString(line) {
			var items []map[string]interface{}
			for i < len(lines) && reOrdered.MatchString(lines[i]) {
				text := reOrdered.ReplaceAllString(lines[i], "")
				items = append(items, map[string]interface{}{
					"type": "listItem",
					"content": []map[string]interface{}{
						{"type": "paragraph", "content": parseInline(text)},
					},
				})
				i++
			}
			content = append(content, map[string]interface{}{
				"type":    "orderedList",
				"content": items,
			})
			continue
		}

		// Bullet list
		if reBullet.MatchString(line) {
			var items []map[string]interface{}
			for i < len(lines) && reBullet.MatchString(lines[i]) && !reTaskItem.MatchString(lines[i]) {
				text := reBullet.ReplaceAllString(lines[i], "")
				items = append(items, map[string]interface{}{
					"type": "listItem",
					"content": []map[string]interface{}{
						{"type": "paragraph", "content": parseInline(text)},
					},
				})
				i++
			}
			content = append(content, map[string]interface{}{
				"type":    "bulletList",
				"content": items,
			})
			continue
		}

		// Empty line — skip
		if strings.TrimSpace(line) == "" {
			i++
			continue
		}

		// Regular paragraph
		content = append(content, map[string]interface{}{
			"type":    "paragraph",
			"content": parseInline(line),
		})
		i++
	}

	if len(content) == 0 {
		content = []map[string]interface{}{
			{"type": "paragraph", "content": []map[string]interface{}{{"type": "text", "text": ""}}},
		}
	}

	return map[string]interface{}{
		"version": 1,
		"type":    "doc",
		"content": content,
	}
}

// mdContext carries state across the conversion (e.g. unique IDs for taskList).
type mdContext struct {
	idSeq int
}

func (c *mdContext) nextLocalID() string {
	c.idSeq++
	return fmt.Sprintf("md-%d", c.idSeq)
}

var (
	reHeading   = regexp.MustCompile(`^(#{1,6})\s+(.+)$`)
	reHRule     = regexp.MustCompile(`^---+$`)
	reOrdered   = regexp.MustCompile(`^\s*\d+\.\s+`)
	reBullet    = regexp.MustCompile(`^\s*[-*]\s+`)
	reTaskItem  = regexp.MustCompile(`^\s*[-*]\s+\[( |x|X)\]\s+(.*)$`)
	rePanelOpen = regexp.MustCompile(`^:::\s*([a-zA-Z]+)\s*$`)
)

// validPanelTypes are the panelType values accepted by Jira ADF.
var validPanelTypes = map[string]struct{}{
	"info":    {},
	"note":    {},
	"warning": {},
	"success": {},
	"error":   {},
	"tip":     {},
}

// isTableRow returns true if a line looks like a markdown table row (`| ... |`).
func isTableRow(line string) bool {
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, "|") && strings.Count(t, "|") >= 2
}

// isTableSeparator matches the `|---|---|` separator (with optional alignment colons).
var reTableSep = regexp.MustCompile(`^\s*\|?\s*:?-{3,}:?\s*(\|\s*:?-{3,}:?\s*)+\|?\s*$`)

func isTableSeparator(line string) bool {
	return reTableSep.MatchString(line)
}

// splitTableRow splits a table row into cell strings, trimming the surrounding pipes.
func splitTableRow(line string) []string {
	t := strings.TrimSpace(line)
	t = strings.TrimPrefix(t, "|")
	t = strings.TrimSuffix(t, "|")
	parts := strings.Split(t, "|")
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = strings.TrimSpace(p)
	}
	return out
}

// parseTable consumes table lines starting at lines[0] (header) and returns the
// ADF block plus the number of lines consumed.
func parseTable(lines []string) (map[string]interface{}, int) {
	headers := splitTableRow(lines[0])
	// lines[1] is the separator — skip it
	var rows []map[string]interface{}

	headerCells := make([]map[string]interface{}, 0, len(headers))
	for _, h := range headers {
		headerCells = append(headerCells, map[string]interface{}{
			"type":  "tableHeader",
			"attrs": map[string]interface{}{},
			"content": []map[string]interface{}{
				{"type": "paragraph", "content": parseInline(h)},
			},
		})
	}
	rows = append(rows, map[string]interface{}{
		"type":    "tableRow",
		"content": headerCells,
	})

	consumed := 2
	for consumed < len(lines) && isTableRow(lines[consumed]) {
		cells := splitTableRow(lines[consumed])
		// Pad/truncate to header width so cell count is stable
		if len(cells) < len(headers) {
			for len(cells) < len(headers) {
				cells = append(cells, "")
			}
		} else if len(cells) > len(headers) {
			cells = cells[:len(headers)]
		}
		rowCells := make([]map[string]interface{}, 0, len(cells))
		for _, c := range cells {
			rowCells = append(rowCells, map[string]interface{}{
				"type":  "tableCell",
				"attrs": map[string]interface{}{},
				"content": []map[string]interface{}{
					{"type": "paragraph", "content": parseInline(c)},
				},
			})
		}
		rows = append(rows, map[string]interface{}{
			"type":    "tableRow",
			"content": rowCells,
		})
		consumed++
	}

	return map[string]interface{}{
		"type": "table",
		"attrs": map[string]interface{}{
			"isNumberColumnEnabled": false,
			"layout":                "default",
		},
		"content": rows,
	}, consumed
}

// Inline parsing.
//
// We use a recursive approach so marks compose (e.g. `**`code`**` produces a
// strong text node carrying both the strong and code marks).

// inlineRe matches inline markdown tokens. Order in the alternation matters
// because the regex engine takes the leftmost-longest match within a single
// alternation slot — but here we expose each token as a distinct alternation
// so the leftmost match wins. We handle priority manually after matching.
var inlineRe = regexp.MustCompile(
	"`[^`]+`" + // code (highest priority — atomic)
		`|\*\*(?:[^*]|\*(?:[^*]))+?\*\*` + // bold **...** (non-greedy, allow lone *)
		`|~~[^~]+~~` + // strikethrough
		`|\*[^\s*][^*]*?\*` + // italic *...* (must not start with space)
		`|\[[^\]]+\]\([^)]+\)`, // link [text](url)
)

func parseInline(text string) []map[string]interface{} {
	return parseInlineWithMarks(text, nil)
}

// parseInlineWithMarks parses text and applies the given marks to every emitted
// text node, in addition to whatever marks the matched tokens add themselves.
func parseInlineWithMarks(text string, inherited []map[string]interface{}) []map[string]interface{} {
	if text == "" {
		return nil
	}

	matches := inlineRe.FindAllStringIndex(text, -1)
	if len(matches) == 0 {
		return []map[string]interface{}{textNode(text, inherited)}
	}

	var nodes []map[string]interface{}
	cursor := 0
	for _, m := range matches {
		start, end := m[0], m[1]
		if start > cursor {
			nodes = append(nodes, textNode(text[cursor:start], inherited))
		}
		tok := text[start:end]
		nodes = append(nodes, parseInlineToken(tok, inherited)...)
		cursor = end
	}
	if cursor < len(text) {
		nodes = append(nodes, textNode(text[cursor:], inherited))
	}
	return nodes
}

func parseInlineToken(tok string, inherited []map[string]interface{}) []map[string]interface{} {
	switch {
	case strings.HasPrefix(tok, "`") && strings.HasSuffix(tok, "`"):
		// Code is atomic — no further parsing inside.
		// The Jira ADF spec forbids combining code with strong/em/link/strike/
		// underline/textColor on the same text node, so we drop those and keep
		// only the code mark.
		return []map[string]interface{}{
			textNode(tok[1:len(tok)-1], []map[string]interface{}{{"type": "code"}}),
		}
	case strings.HasPrefix(tok, "**") && strings.HasSuffix(tok, "**"):
		return parseInlineWithMarks(tok[2:len(tok)-2], appendMark(inherited, "strong"))
	case strings.HasPrefix(tok, "~~") && strings.HasSuffix(tok, "~~"):
		return parseInlineWithMarks(tok[2:len(tok)-2], appendMark(inherited, "strike"))
	case strings.HasPrefix(tok, "*") && strings.HasSuffix(tok, "*"):
		return parseInlineWithMarks(tok[1:len(tok)-1], appendMark(inherited, "em"))
	case strings.HasPrefix(tok, "["):
		linkRe := regexp.MustCompile(`^\[([^\]]+)\]\(([^)]+)\)$`)
		lm := linkRe.FindStringSubmatch(tok)
		if lm != nil {
			linkMark := map[string]interface{}{
				"type":  "link",
				"attrs": map[string]interface{}{"href": lm[2]},
			}
			marks := append([]map[string]interface{}{}, inherited...)
			marks = append(marks, linkMark)
			return parseInlineWithMarks(lm[1], marks)
		}
	}
	// Fallback: emit as plain text with inherited marks.
	return []map[string]interface{}{textNode(tok, inherited)}
}

func textNode(text string, marks []map[string]interface{}) map[string]interface{} {
	n := map[string]interface{}{"type": "text", "text": text}
	if len(marks) > 0 {
		// Copy so callers can keep appending without aliasing.
		out := make([]map[string]interface{}, len(marks))
		copy(out, marks)
		n["marks"] = out
	}
	return n
}

func appendMark(existing []map[string]interface{}, markType string) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(existing)+1)
	out = append(out, existing...)
	out = append(out, map[string]interface{}{"type": markType})
	return out
}
