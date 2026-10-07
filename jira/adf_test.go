package jira

import (
	"encoding/json"
	"testing"
)

func toJSON(v interface{}) string {
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}

func TestMarkdownToADF_Headings(t *testing.T) {
	tests := []struct {
		name  string
		input string
		check func(t *testing.T, doc map[string]interface{})
	}{
		{
			name:  "h1",
			input: "# Title",
			check: func(t *testing.T, doc map[string]interface{}) {
				content := doc["content"].([]map[string]interface{})
				if len(content) != 1 {
					t.Fatalf("expected 1 block, got %d", len(content))
				}
				if content[0]["type"] != "heading" {
					t.Fatalf("expected heading, got %s", content[0]["type"])
				}
				attrs := content[0]["attrs"].(map[string]interface{})
				if attrs["level"] != 1 {
					t.Fatalf("expected level 1, got %v", attrs["level"])
				}
			},
		},
		{
			name:  "h2",
			input: "## Section",
			check: func(t *testing.T, doc map[string]interface{}) {
				content := doc["content"].([]map[string]interface{})
				attrs := content[0]["attrs"].(map[string]interface{})
				if attrs["level"] != 2 {
					t.Fatalf("expected level 2, got %v", attrs["level"])
				}
			},
		},
		{
			name:  "h3 with inline bold",
			input: "### Section with **bold**",
			check: func(t *testing.T, doc map[string]interface{}) {
				content := doc["content"].([]map[string]interface{})
				heading := content[0]
				inlines := heading["content"].([]map[string]interface{})
				if len(inlines) < 2 {
					t.Fatalf("expected at least 2 inline nodes, got %d: %s", len(inlines), toJSON(inlines))
				}
				// Check the bold part exists
				found := false
				for _, n := range inlines {
					if marks, ok := n["marks"]; ok {
						marksSlice := marks.([]map[string]interface{})
						if len(marksSlice) > 0 && marksSlice[0]["type"] == "strong" {
							found = true
						}
					}
				}
				if !found {
					t.Fatalf("expected strong mark in heading, got: %s", toJSON(inlines))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := MarkdownToADF(tt.input)
			tt.check(t, doc)
		})
	}
}

func TestMarkdownToADF_BulletLists(t *testing.T) {
	tests := []struct {
		name  string
		input string
		check func(t *testing.T, doc map[string]interface{})
	}{
		{
			name:  "simple bullets with dash",
			input: "- item one\n- item two\n- item three",
			check: func(t *testing.T, doc map[string]interface{}) {
				content := doc["content"].([]map[string]interface{})
				if len(content) != 1 {
					t.Fatalf("expected 1 block (bulletList), got %d: %s", len(content), toJSON(content))
				}
				bl := content[0]
				if bl["type"] != "bulletList" {
					t.Fatalf("expected bulletList, got %s", bl["type"])
				}
				items := bl["content"].([]map[string]interface{})
				if len(items) != 3 {
					t.Fatalf("expected 3 items, got %d", len(items))
				}
			},
		},
		{
			name:  "simple bullets with asterisk",
			input: "* item one\n* item two",
			check: func(t *testing.T, doc map[string]interface{}) {
				content := doc["content"].([]map[string]interface{})
				bl := content[0]
				if bl["type"] != "bulletList" {
					t.Fatalf("expected bulletList, got %s", bl["type"])
				}
				items := bl["content"].([]map[string]interface{})
				if len(items) != 2 {
					t.Fatalf("expected 2 items, got %d", len(items))
				}
			},
		},
		{
			name:  "bullets with inline formatting",
			input: "- **Billing**: Verified active\n- **Role**: `editor` assigned",
			check: func(t *testing.T, doc map[string]interface{}) {
				content := doc["content"].([]map[string]interface{})
				bl := content[0]
				items := bl["content"].([]map[string]interface{})
				// First item should have bold "Billing"
				item0 := items[0]
				para := item0["content"].([]map[string]interface{})[0]
				inlines := para["content"].([]map[string]interface{})
				foundStrong := false
				for _, n := range inlines {
					if marks, ok := n["marks"]; ok {
						marksSlice := marks.([]map[string]interface{})
						for _, m := range marksSlice {
							if m["type"] == "strong" {
								foundStrong = true
							}
						}
					}
				}
				if !foundStrong {
					t.Fatalf("expected strong mark in first bullet, got: %s", toJSON(inlines))
				}
			},
		},
		{
			name:  "bullets separated by blank line should be two lists",
			input: "- a\n- b\n\n- c\n- d",
			check: func(t *testing.T, doc map[string]interface{}) {
				content := doc["content"].([]map[string]interface{})
				if len(content) != 2 {
					t.Fatalf("expected 2 bulletLists separated by blank line, got %d: %s", len(content), toJSON(content))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := MarkdownToADF(tt.input)
			tt.check(t, doc)
		})
	}
}

func TestMarkdownToADF_InlineFormatting(t *testing.T) {
	tests := []struct {
		name  string
		input string
		check func(t *testing.T, doc map[string]interface{})
	}{
		{
			name:  "bold text",
			input: "This is **bold** text",
			check: func(t *testing.T, doc map[string]interface{}) {
				content := doc["content"].([]map[string]interface{})
				para := content[0]
				inlines := para["content"].([]map[string]interface{})
				if len(inlines) != 3 {
					t.Fatalf("expected 3 nodes (text, strong, text), got %d: %s", len(inlines), toJSON(inlines))
				}
				boldNode := inlines[1]
				if boldNode["text"] != "bold" {
					t.Fatalf("expected text 'bold', got '%s'", boldNode["text"])
				}
				marks := boldNode["marks"].([]map[string]interface{})
				if marks[0]["type"] != "strong" {
					t.Fatalf("expected strong mark, got %s", marks[0]["type"])
				}
			},
		},
		{
			name:  "italic text",
			input: "This is *italic* text",
			check: func(t *testing.T, doc map[string]interface{}) {
				content := doc["content"].([]map[string]interface{})
				para := content[0]
				inlines := para["content"].([]map[string]interface{})
				if len(inlines) != 3 {
					t.Fatalf("expected 3 nodes, got %d: %s", len(inlines), toJSON(inlines))
				}
				italicNode := inlines[1]
				marks := italicNode["marks"].([]map[string]interface{})
				if marks[0]["type"] != "em" {
					t.Fatalf("expected em mark, got %s", marks[0]["type"])
				}
			},
		},
		{
			name:  "inline code",
			input: "Use `kubectl get pods` to check",
			check: func(t *testing.T, doc map[string]interface{}) {
				content := doc["content"].([]map[string]interface{})
				para := content[0]
				inlines := para["content"].([]map[string]interface{})
				if len(inlines) != 3 {
					t.Fatalf("expected 3 nodes, got %d: %s", len(inlines), toJSON(inlines))
				}
				codeNode := inlines[1]
				if codeNode["text"] != "kubectl get pods" {
					t.Fatalf("expected 'kubectl get pods', got '%s'", codeNode["text"])
				}
				marks := codeNode["marks"].([]map[string]interface{})
				if marks[0]["type"] != "code" {
					t.Fatalf("expected code mark, got %s", marks[0]["type"])
				}
			},
		},
		{
			name:  "link",
			input: "See [Jira](https://jira.example.com) for details",
			check: func(t *testing.T, doc map[string]interface{}) {
				content := doc["content"].([]map[string]interface{})
				para := content[0]
				inlines := para["content"].([]map[string]interface{})
				if len(inlines) != 3 {
					t.Fatalf("expected 3 nodes, got %d: %s", len(inlines), toJSON(inlines))
				}
				linkNode := inlines[1]
				if linkNode["text"] != "Jira" {
					t.Fatalf("expected text 'Jira', got '%s'", linkNode["text"])
				}
				marks := linkNode["marks"].([]map[string]interface{})
				if marks[0]["type"] != "link" {
					t.Fatalf("expected link mark, got %s", marks[0]["type"])
				}
				attrs := marks[0]["attrs"].(map[string]interface{})
				if attrs["href"] != "https://jira.example.com" {
					t.Fatalf("expected href 'https://jira.example.com', got '%s'", attrs["href"])
				}
			},
		},
		{
			name:  "multiple inline marks in one line",
			input: "**bold** then `code` then *italic*",
			check: func(t *testing.T, doc map[string]interface{}) {
				content := doc["content"].([]map[string]interface{})
				para := content[0]
				inlines := para["content"].([]map[string]interface{})
				// Should be: bold, " then ", code, " then ", italic
				if len(inlines) < 5 {
					t.Fatalf("expected at least 5 nodes, got %d: %s", len(inlines), toJSON(inlines))
				}
			},
		},
		{
			name:  "bold at start of line",
			input: "**Project**: acme-internal-apps-prod",
			check: func(t *testing.T, doc map[string]interface{}) {
				content := doc["content"].([]map[string]interface{})
				para := content[0]
				inlines := para["content"].([]map[string]interface{})
				// First node should be bold "Project"
				if len(inlines) < 2 {
					t.Fatalf("expected at least 2 nodes, got %d: %s", len(inlines), toJSON(inlines))
				}
				boldNode := inlines[0]
				if boldNode["text"] != "Project" {
					t.Fatalf("expected text 'Project', got '%s': %s", boldNode["text"], toJSON(inlines))
				}
				marks := boldNode["marks"].([]map[string]interface{})
				if marks[0]["type"] != "strong" {
					t.Fatalf("expected strong mark, got %s", marks[0]["type"])
				}
			},
		},
		{
			name:  "bold at end of line",
			input: "This is **important**",
			check: func(t *testing.T, doc map[string]interface{}) {
				content := doc["content"].([]map[string]interface{})
				para := content[0]
				inlines := para["content"].([]map[string]interface{})
				if len(inlines) != 2 {
					t.Fatalf("expected 2 nodes, got %d: %s", len(inlines), toJSON(inlines))
				}
			},
		},
		{
			name:  "asterisk in text should not be confused with bold",
			input: "Use 2 * 3 = 6 formula",
			check: func(t *testing.T, doc map[string]interface{}) {
				content := doc["content"].([]map[string]interface{})
				para := content[0]
				inlines := para["content"].([]map[string]interface{})
				// "2 * 3 = 6" has isolated asterisks — should NOT be parsed as italic
				// since there's no matching close *
				for _, n := range inlines {
					if marks, ok := n["marks"]; ok {
						marksSlice := marks.([]map[string]interface{})
						for _, m := range marksSlice {
							if m["type"] == "em" {
								t.Fatalf("isolated asterisks should not be parsed as italic: %s", toJSON(inlines))
							}
						}
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := MarkdownToADF(tt.input)
			tt.check(t, doc)
		})
	}
}

func TestMarkdownToADF_CodeBlocks(t *testing.T) {
	tests := []struct {
		name  string
		input string
		check func(t *testing.T, doc map[string]interface{})
	}{
		{
			name:  "simple code block",
			input: "```\nfoo\nbar\n```",
			check: func(t *testing.T, doc map[string]interface{}) {
				content := doc["content"].([]map[string]interface{})
				if len(content) != 1 {
					t.Fatalf("expected 1 block, got %d", len(content))
				}
				if content[0]["type"] != "codeBlock" {
					t.Fatalf("expected codeBlock, got %s", content[0]["type"])
				}
				inner := content[0]["content"].([]map[string]interface{})
				if inner[0]["text"] != "foo\nbar" {
					t.Fatalf("expected 'foo\\nbar', got '%s'", inner[0]["text"])
				}
			},
		},
		{
			name:  "code block with language",
			input: "```bash\ngcloud compute firewall-rules list\n```",
			check: func(t *testing.T, doc map[string]interface{}) {
				content := doc["content"].([]map[string]interface{})
				if content[0]["type"] != "codeBlock" {
					t.Fatalf("expected codeBlock, got %s", content[0]["type"])
				}
				inner := content[0]["content"].([]map[string]interface{})
				if inner[0]["text"] != "gcloud compute firewall-rules list" {
					t.Fatalf("expected command text, got '%s'", inner[0]["text"])
				}
			},
		},
		{
			name:  "code block with empty lines",
			input: "```\nline1\n\nline3\n```",
			check: func(t *testing.T, doc map[string]interface{}) {
				content := doc["content"].([]map[string]interface{})
				inner := content[0]["content"].([]map[string]interface{})
				if inner[0]["text"] != "line1\n\nline3" {
					t.Fatalf("expected preserved empty line, got '%s'", inner[0]["text"])
				}
			},
		},
		{
			name:  "unclosed code block",
			input: "```\nfoo\nbar",
			check: func(t *testing.T, doc map[string]interface{}) {
				content := doc["content"].([]map[string]interface{})
				if content[0]["type"] != "codeBlock" {
					t.Fatalf("expected codeBlock even unclosed, got %s", content[0]["type"])
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := MarkdownToADF(tt.input)
			tt.check(t, doc)
		})
	}
}

func TestMarkdownToADF_MixedContent(t *testing.T) {
	tests := []struct {
		name  string
		input string
		check func(t *testing.T, doc map[string]interface{})
	}{
		{
			name: "heading then paragraph then list",
			input: `## Contexto

Se realizó la configuración de OAuth en el proyecto GCP.

## Acciones realizadas

- **Billing**: Verificado que está activo
- **Rol OAuth**: Se otorgó ` + "`roles/editor`" + ` a user@example.com`,
			check: func(t *testing.T, doc map[string]interface{}) {
				content := doc["content"].([]map[string]interface{})
				// Expected: heading, paragraph, heading, bulletList
				if len(content) != 4 {
					t.Fatalf("expected 4 blocks, got %d: %s", len(content), toJSON(content))
				}
				if content[0]["type"] != "heading" {
					t.Fatalf("block 0: expected heading, got %s", content[0]["type"])
				}
				if content[1]["type"] != "paragraph" {
					t.Fatalf("block 1: expected paragraph, got %s", content[1]["type"])
				}
				if content[2]["type"] != "heading" {
					t.Fatalf("block 2: expected heading, got %s", content[2]["type"])
				}
				if content[3]["type"] != "bulletList" {
					t.Fatalf("block 3: expected bulletList, got %s", content[3]["type"])
				}
			},
		},
		{
			name:  "horizontal rule between sections",
			input: "## Section 1\n\nContent\n\n---\n\n## Section 2\n\nMore content",
			check: func(t *testing.T, doc map[string]interface{}) {
				content := doc["content"].([]map[string]interface{})
				// heading, paragraph, rule, heading, paragraph
				if len(content) != 5 {
					t.Fatalf("expected 5 blocks, got %d: %s", len(content), toJSON(content))
				}
				if content[2]["type"] != "rule" {
					t.Fatalf("block 2: expected rule, got %s", content[2]["type"])
				}
			},
		},
		{
			name:  "empty input",
			input: "",
			check: func(t *testing.T, doc map[string]interface{}) {
				content := doc["content"].([]map[string]interface{})
				if len(content) != 1 {
					t.Fatalf("expected 1 empty paragraph, got %d", len(content))
				}
				if content[0]["type"] != "paragraph" {
					t.Fatalf("expected paragraph, got %s", content[0]["type"])
				}
			},
		},
		{
			name:  "only blank lines",
			input: "\n\n\n",
			check: func(t *testing.T, doc map[string]interface{}) {
				content := doc["content"].([]map[string]interface{})
				if len(content) != 1 {
					t.Fatalf("expected 1 empty paragraph fallback, got %d", len(content))
				}
			},
		},
		{
			name:  "paragraph with trailing newlines",
			input: "Hello world\n\n",
			check: func(t *testing.T, doc map[string]interface{}) {
				content := doc["content"].([]map[string]interface{})
				if len(content) != 1 {
					t.Fatalf("expected 1 paragraph, got %d", len(content))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := MarkdownToADF(tt.input)
			tt.check(t, doc)
		})
	}
}

func TestMarkdownToADF_EdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		input string
		check func(t *testing.T, doc map[string]interface{})
	}{
		{
			name:  "line starting with dash but not a bullet (no space after dash)",
			input: "-not-a-bullet",
			check: func(t *testing.T, doc map[string]interface{}) {
				content := doc["content"].([]map[string]interface{})
				// Should be paragraph, not bulletList
				if content[0]["type"] == "bulletList" {
					t.Fatalf("should not parse '-not-a-bullet' as bullet list")
				}
			},
		},
		{
			name:  "ordered list",
			input: "1. First\n2. Second\n3. Third",
			check: func(t *testing.T, doc map[string]interface{}) {
				content := doc["content"].([]map[string]interface{})
				if len(content) != 1 {
					t.Fatalf("expected 1 orderedList, got %d: %s", len(content), toJSON(content))
				}
				if content[0]["type"] != "orderedList" {
					t.Fatalf("expected orderedList, got %s", content[0]["type"])
				}
				items := content[0]["content"].([]map[string]interface{})
				if len(items) != 3 {
					t.Fatalf("expected 3 items, got %d", len(items))
				}
			},
		},
		{
			name:  "isolated asterisks should not become italic",
			input: "2 * 3 * 4 = 24",
			check: func(t *testing.T, doc map[string]interface{}) {
				content := doc["content"].([]map[string]interface{})
				para := content[0]
				inlines := para["content"].([]map[string]interface{})
				// Should be a single text node, no italic marks
				for _, n := range inlines {
					if marks, ok := n["marks"]; ok {
						marksSlice := marks.([]map[string]interface{})
						for _, m := range marksSlice {
							if m["type"] == "em" {
								t.Fatalf("'2 * 3 * 4' should not produce italic: %s", toJSON(inlines))
							}
						}
					}
				}
			},
		},
		{
			name:  "mixed isolated and real italic asterisks",
			input: "allow * → todo el tráfico *pasa*",
			check: func(t *testing.T, doc map[string]interface{}) {
				content := doc["content"].([]map[string]interface{})
				para := content[0]
				inlines := para["content"].([]map[string]interface{})
				// "pasa" should be italic, but the isolated * should be plain text
				foundItalicPasa := false
				for _, n := range inlines {
					if marks, ok := n["marks"]; ok {
						marksSlice := marks.([]map[string]interface{})
						for _, m := range marksSlice {
							if m["type"] == "em" && n["text"] == "pasa" {
								foundItalicPasa = true
							}
						}
					}
				}
				if !foundItalicPasa {
					t.Fatalf("expected italic 'pasa', got: %s", toJSON(inlines))
				}
			},
		},
		{
			name:  "code block with language attribute",
			input: "```bash\necho hello\n```",
			check: func(t *testing.T, doc map[string]interface{}) {
				content := doc["content"].([]map[string]interface{})
				cb := content[0]
				attrs, ok := cb["attrs"].(map[string]interface{})
				if !ok {
					t.Fatalf("expected attrs on codeBlock, got none: %s", toJSON(cb))
				}
				if attrs["language"] != "bash" {
					t.Fatalf("expected language 'bash', got '%s'", attrs["language"])
				}
			},
		},
		{
			name:  "code block without language has no attrs",
			input: "```\nplain\n```",
			check: func(t *testing.T, doc map[string]interface{}) {
				content := doc["content"].([]map[string]interface{})
				cb := content[0]
				if _, ok := cb["attrs"]; ok {
					t.Fatalf("expected no attrs on plain codeBlock, got: %s", toJSON(cb))
				}
			},
		},
		{
			name:  "bold with asterisks inside text",
			input: "- **Cloud Armor policy:** `internal-apps-policy` asignada como edge security",
			check: func(t *testing.T, doc map[string]interface{}) {
				content := doc["content"].([]map[string]interface{})
				bl := content[0]
				items := bl["content"].([]map[string]interface{})
				item := items[0]
				para := item["content"].([]map[string]interface{})[0]
				inlines := para["content"].([]map[string]interface{})
				// Should have: bold("Cloud Armor policy:"), text(" "), code("internal-apps-policy"), text(" asignada...")
				foundStrong := false
				foundCode := false
				for _, n := range inlines {
					if marks, ok := n["marks"]; ok {
						marksSlice := marks.([]map[string]interface{})
						for _, m := range marksSlice {
							if m["type"] == "strong" {
								foundStrong = true
							}
							if m["type"] == "code" {
								foundCode = true
							}
						}
					}
				}
				if !foundStrong {
					t.Fatalf("expected strong mark, got: %s", toJSON(inlines))
				}
				if !foundCode {
					t.Fatalf("expected code mark, got: %s", toJSON(inlines))
				}
			},
		},
		{
			name:  "URL with parens in link should work",
			input: "See [docs](https://example.com/path_(section))",
			check: func(t *testing.T, doc map[string]interface{}) {
				// This is a known limitation - just make sure it doesn't crash
				content := doc["content"].([]map[string]interface{})
				if len(content) == 0 {
					t.Fatal("expected content")
				}
			},
		},
		{
			name:  "heading with no space after hash",
			input: "#NoSpace",
			check: func(t *testing.T, doc map[string]interface{}) {
				content := doc["content"].([]map[string]interface{})
				// Should be a paragraph, not a heading (markdown requires space after #)
				if content[0]["type"] == "heading" {
					t.Fatalf("#NoSpace should not be a heading (no space after #)")
				}
			},
		},
		{
			name:  "line that looks like heading but is bold list item",
			input: "- **GCP Project:** `acme-internal-apps-prod`",
			check: func(t *testing.T, doc map[string]interface{}) {
				content := doc["content"].([]map[string]interface{})
				if content[0]["type"] != "bulletList" {
					t.Fatalf("expected bulletList, got %s", content[0]["type"])
				}
			},
		},
		{
			name:  "arrow unicode in text",
			input: "- Regla default: allow * → todo el tráfico pasa",
			check: func(t *testing.T, doc map[string]interface{}) {
				content := doc["content"].([]map[string]interface{})
				if content[0]["type"] != "bulletList" {
					t.Fatalf("expected bulletList, got %s", content[0]["type"])
				}
				// The * in "allow *" should not trigger italic
				bl := content[0]
				items := bl["content"].([]map[string]interface{})
				para := items[0]["content"].([]map[string]interface{})[0]
				inlines := para["content"].([]map[string]interface{})
				for _, n := range inlines {
					if marks, ok := n["marks"]; ok {
						marksSlice := marks.([]map[string]interface{})
						for _, m := range marksSlice {
							if m["type"] == "em" {
								t.Fatalf("isolated * should not trigger italic: %s", toJSON(inlines))
							}
						}
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := MarkdownToADF(tt.input)
			tt.check(t, doc)
		})
	}
}

func TestMarkdownToADF_RealWorldDescription(t *testing.T) {
	// This is the kind of description Claude would generate for a Jira ticket
	input := `## Context

Related to [OPS-2965](https://acme.atlassian.net/browse/OPS-2965): the checkout page sometimes shows a stale price after a coupon is applied, and ` + "`cart/summary`" + ` takes 2 s to answer.

On the platform side, we want to cache price lookups in Redis.

## What runs today

- **Service:** ` + "`pricing-api`" + ` on Cloud Run, two instances
- **Cache:** none; every request reads Postgres (` + "`prices`" + ` table)
- **Load:** ` + "`p95 1.8 s`" + ` at peak
- **Alerting:** the ` + "`pricing-latency`" + ` monitor, threshold 1 s
- **No feature flag for the cache yet**

## Cache key

` + "```" + `
price:{sku}:{currency}
` + "```" + `

---

Add the cache behind a flag.`

	doc := MarkdownToADF(input)
	content := doc["content"].([]map[string]interface{})

	// Verify structure
	expectedTypes := []string{
		"heading",    // ## Context
		"paragraph",  // Related to...
		"paragraph",  // On the platform side...
		"heading",    // ## What runs today
		"bulletList", // the 5 bullets
		"heading",    // ## Cache key
		"codeBlock",  // the key
		"rule",       // ---
		"paragraph",  // Add the cache
	}

	if len(content) != len(expectedTypes) {
		t.Fatalf("expected %d blocks, got %d:\n%s", len(expectedTypes), len(content), toJSON(content))
	}

	for i, expected := range expectedTypes {
		actual := content[i]["type"].(string)
		if actual != expected {
			t.Errorf("block %d: expected %s, got %s", i, expected, actual)
		}
	}

	// Verify the bullet list has 5 items
	bl := content[4]
	items := bl["content"].([]map[string]interface{})
	if len(items) != 5 {
		t.Fatalf("expected 5 bullet items, got %d", len(items))
	}

	// Verify first bullet has bold + code formatting
	item0 := items[0]
	para := item0["content"].([]map[string]interface{})[0]
	inlines := para["content"].([]map[string]interface{})
	foundStrong := false
	foundCode := false
	for _, n := range inlines {
		if marks, ok := n["marks"]; ok {
			marksSlice := marks.([]map[string]interface{})
			for _, m := range marksSlice {
				if m["type"] == "strong" {
					foundStrong = true
				}
				if m["type"] == "code" {
					foundCode = true
				}
			}
		}
	}
	if !foundStrong {
		t.Fatalf("first bullet should have bold: %s", toJSON(inlines))
	}
	if !foundCode {
		t.Fatalf("first bullet should have code: %s", toJSON(inlines))
	}
}

func TestMarkdownToADF_Tables(t *testing.T) {
	input := "| Hora | Errores | Notas |\n|---|---|---|\n| 02:49 | 333 | Spike principal |\n| 06:00 | 304 | Spike actual |"
	doc := MarkdownToADF(input)
	content := doc["content"].([]map[string]interface{})
	if len(content) != 1 {
		t.Fatalf("expected 1 table block, got %d:\n%s", len(content), toJSON(content))
	}
	table := content[0]
	if table["type"] != "table" {
		t.Fatalf("expected table, got %s", table["type"])
	}
	rows := table["content"].([]map[string]interface{})
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows (1 header + 2 body), got %d", len(rows))
	}
	// Header row should be tableHeader cells
	headerCells := rows[0]["content"].([]map[string]interface{})
	if len(headerCells) != 3 {
		t.Fatalf("expected 3 header cells, got %d", len(headerCells))
	}
	if headerCells[0]["type"] != "tableHeader" {
		t.Fatalf("expected tableHeader, got %s", headerCells[0]["type"])
	}
	// Body row should be tableCell
	bodyCells := rows[1]["content"].([]map[string]interface{})
	if bodyCells[0]["type"] != "tableCell" {
		t.Fatalf("expected tableCell, got %s", bodyCells[0]["type"])
	}
	// Cell content should be wrapped in a paragraph
	cellContent := bodyCells[0]["content"].([]map[string]interface{})
	if cellContent[0]["type"] != "paragraph" {
		t.Fatalf("expected cell paragraph wrapper, got %s", cellContent[0]["type"])
	}
}

func TestMarkdownToADF_TableWithUnevenCells(t *testing.T) {
	// Body row missing a cell should be padded to header width
	input := "| A | B | C |\n|---|---|---|\n| 1 | 2 |"
	doc := MarkdownToADF(input)
	table := doc["content"].([]map[string]interface{})[0]
	rows := table["content"].([]map[string]interface{})
	body := rows[1]["content"].([]map[string]interface{})
	if len(body) != 3 {
		t.Fatalf("expected body padded to 3 cells, got %d", len(body))
	}
}

func TestMarkdownToADF_TaskList(t *testing.T) {
	input := "- [ ] Subir minScale\n- [x] Habilitar cpu-boost\n- [ ] Auditar headers"
	doc := MarkdownToADF(input)
	content := doc["content"].([]map[string]interface{})
	if len(content) != 1 {
		t.Fatalf("expected 1 block, got %d", len(content))
	}
	tl := content[0]
	if tl["type"] != "taskList" {
		t.Fatalf("expected taskList, got %s", tl["type"])
	}
	if _, ok := tl["attrs"].(map[string]interface{})["localId"]; !ok {
		t.Fatalf("taskList missing localId attr")
	}
	items := tl["content"].([]map[string]interface{})
	if len(items) != 3 {
		t.Fatalf("expected 3 task items, got %d", len(items))
	}
	states := []string{"TODO", "DONE", "TODO"}
	for i, item := range items {
		if item["type"] != "taskItem" {
			t.Fatalf("item %d: expected taskItem, got %s", i, item["type"])
		}
		attrs := item["attrs"].(map[string]interface{})
		if attrs["state"] != states[i] {
			t.Fatalf("item %d: expected state %s, got %v", i, states[i], attrs["state"])
		}
		if _, ok := attrs["localId"]; !ok {
			t.Fatalf("item %d: missing localId", i)
		}
	}
}

func TestMarkdownToADF_TaskListNotConfusedWithBullet(t *testing.T) {
	// Plain bullets must NOT become a taskList
	input := "- one\n- two"
	doc := MarkdownToADF(input)
	block := doc["content"].([]map[string]interface{})[0]
	if block["type"] != "bulletList" {
		t.Fatalf("expected bulletList for plain bullets, got %s", block["type"])
	}
}

func TestMarkdownToADF_Blockquote(t *testing.T) {
	input := "> Citado en una línea\n> y otra línea"
	doc := MarkdownToADF(input)
	content := doc["content"].([]map[string]interface{})
	if len(content) != 1 {
		t.Fatalf("expected 1 block, got %d", len(content))
	}
	bq := content[0]
	if bq["type"] != "blockquote" {
		t.Fatalf("expected blockquote, got %s", bq["type"])
	}
	inner := bq["content"].([]map[string]interface{})
	if len(inner) == 0 || inner[0]["type"] != "paragraph" {
		t.Fatalf("expected paragraph inside blockquote, got %s", toJSON(inner))
	}
}

func TestMarkdownToADF_Strikethrough(t *testing.T) {
	input := "this is ~~strike~~ text"
	doc := MarkdownToADF(input)
	para := doc["content"].([]map[string]interface{})[0]
	inlines := para["content"].([]map[string]interface{})
	found := false
	for _, n := range inlines {
		if marks, ok := n["marks"]; ok {
			for _, m := range marks.([]map[string]interface{}) {
				if m["type"] == "strike" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatalf("expected strike mark, got: %s", toJSON(inlines))
	}
}

func TestMarkdownToADF_BoldContainingCode(t *testing.T) {
	// Jira's ADF spec forbids combining `code` with strong/em/link/strike/
	// underline. When **`code`** is parsed, code wins and strong is dropped on
	// that specific text node (otherwise Jira rejects the document).
	input := "**`minScale`** is the field"
	doc := MarkdownToADF(input)
	para := doc["content"].([]map[string]interface{})[0]
	inlines := para["content"].([]map[string]interface{})
	var combined map[string]interface{}
	for _, n := range inlines {
		if n["text"] == "minScale" {
			combined = n
			break
		}
	}
	if combined == nil {
		t.Fatalf("expected a 'minScale' text node, got: %s", toJSON(inlines))
	}
	marks, ok := combined["marks"].([]map[string]interface{})
	if !ok {
		t.Fatalf("expected marks on combined node, got: %s", toJSON(combined))
	}
	if len(marks) != 1 || marks[0]["type"] != "code" {
		t.Fatalf("expected only code mark (strong dropped due to ADF spec), got: %s", toJSON(marks))
	}
}

func TestMarkdownToADF_LinkWithInlineCode(t *testing.T) {
	// [`api`](url): code is exclusive in ADF, so the link mark is dropped on
	// the code-marked text node. The URL is lost — users should write
	// `[api](url)` instead if they want the link to be active.
	input := "see [`api`](https://example.com) for details"
	doc := MarkdownToADF(input)
	para := doc["content"].([]map[string]interface{})[0]
	inlines := para["content"].([]map[string]interface{})
	for _, n := range inlines {
		if n["text"] == "api" {
			marks := n["marks"].([]map[string]interface{})
			if len(marks) != 1 || marks[0]["type"] != "code" {
				t.Fatalf("expected only code mark (link dropped), got: %s", toJSON(marks))
			}
			return
		}
	}
	t.Fatalf("expected 'api' text node, got: %s", toJSON(inlines))
}

func TestMarkdownToADF_MentionsAndBareURLs(t *testing.T) {
	inline := func(md string) []interface{} {
		t.Helper()
		raw, _ := json.Marshal(MarkdownToADF(md))
		var doc map[string]interface{}
		json.Unmarshal(raw, &doc)
		para := doc["content"].([]interface{})[0].(map[string]interface{})
		return para["content"].([]interface{})
	}
	href := func(n interface{}) string {
		for _, m := range n.(map[string]interface{})["marks"].([]interface{}) {
			mark := m.(map[string]interface{})
			if mark["type"] == "link" {
				return mark["attrs"].(map[string]interface{})["href"].(string)
			}
		}
		return ""
	}

	t.Run("mention with an account id becomes a mention node", func(t *testing.T) {
		nodes := inline("Thanks @[Ada Lovelace](5b10a2844c20165700ede21g), merged.")
		if len(nodes) != 3 {
			t.Fatalf("want text, mention, text; got %s", toJSON(nodes))
		}
		m := nodes[1].(map[string]interface{})
		attrs := m["attrs"].(map[string]interface{})
		if m["type"] != "mention" || attrs["id"] != "5b10a2844c20165700ede21g" || attrs["text"] != "@Ada Lovelace" {
			t.Fatalf("bad mention: %s", toJSON(m))
		}
		if _, hasMarks := m["marks"]; hasMarks {
			t.Fatalf("a mention carries no marks: %s", toJSON(m))
		}
	})

	t.Run("mention without an id stays text", func(t *testing.T) {
		nodes := inline("Ask @[Ada Lovelace] about it")
		if len(nodes) != 1 || nodes[0].(map[string]interface{})["type"] != "text" {
			t.Fatalf("want one text node, got %s", toJSON(nodes))
		}
	})

	t.Run("bare URL is linked, sentence punctuation is not", func(t *testing.T) {
		nodes := inline("See https://example.com/pull/12 (PROJ-7), or https://example.com/docs.")
		if got := href(nodes[1]); got != "https://example.com/pull/12" {
			t.Fatalf("first link href = %q in %s", got, toJSON(nodes))
		}
		last := nodes[len(nodes)-1].(map[string]interface{})
		link := nodes[len(nodes)-2]
		if href(link) != "https://example.com/docs" || last["text"] != "." {
			t.Fatalf("trailing dot must stay outside the link: %s", toJSON(nodes))
		}
	})

	t.Run("balanced parenthesis stays in the URL", func(t *testing.T) {
		nodes := inline("(see https://en.wikipedia.org/wiki/Go_(game))")
		if got := href(nodes[1]); got != "https://en.wikipedia.org/wiki/Go_(game)" {
			t.Fatalf("href = %q", got)
		}
	})

	t.Run("URL as link text is not linked twice", func(t *testing.T) {
		nodes := inline("[https://example.com](https://example.com)")
		if len(nodes) != 1 || len(nodes[0].(map[string]interface{})["marks"].([]interface{})) != 1 {
			t.Fatalf("want one node with one link mark, got %s", toJSON(nodes))
		}
	})

	t.Run("URL inside code is left alone", func(t *testing.T) {
		nodes := inline("run `curl https://example.com`")
		code := nodes[1].(map[string]interface{})
		marks := code["marks"].([]interface{})
		if len(marks) != 1 || marks[0].(map[string]interface{})["type"] != "code" {
			t.Fatalf("want only a code mark, got %s", toJSON(code))
		}
	})
}
