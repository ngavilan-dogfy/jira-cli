package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ngavilan-dogfy/jira-cli/jira"
)

func mdToRaw(t *testing.T, md string) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(jira.MarkdownToADF(md))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Anything MarkdownToADF wrote (what `jira create -d` and agents produce)
// must be editable and come back as the same markdown.
func TestEditableMarkdownRoundTrips(t *testing.T) {
	md := strings.Join([]string{
		"## Contexto",
		"La migración a `acme-platform-stg` está **casi completa**, ver [el runbook](https://example.com/r) y ~~lo viejo~~ *pronto*.",
		"- Repo: `infrastructure`",
		"- Backends **acme-api** en los LBs",
		"1. primero",
		"2. segundo",
		"- [ ] pendiente",
		"- [x] hecho",
		"```sh\ngcloud run services list\n  ├── nested\n```",
		"> cita con `code`",
		":::warning\nNo borrar hasta TST-80.\n:::",
		"---",
		"| Recurso | Acción |\n| --- | --- |\n| LB `x` | mover **ya** |",
	}, "\n\n")
	raw := mdToRaw(t, md)
	got, ok := editableMarkdown(raw)
	if !ok {
		t.Fatalf("round trip failed; serialized as:\n%s", got)
	}
	if !adfEquivalent(raw, mdToRaw(t, got)) {
		t.Fatalf("serialized markdown doesn't reproduce the document:\n%s", got)
	}
}

func TestEditableMarkdownRefusesLossyDocuments(t *testing.T) {
	cases := map[string]string{
		"mention":         `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"mention","attrs":{"id":"1","text":"@Ana"}}]}]}`,
		"hard break":      `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"a"},{"type":"hardBreak"},{"type":"text","text":"b"}]}]}`,
		"nested list":     `{"type":"doc","content":[{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"a"}]},{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"b"}]}]}]}]}]}]}`,
		"underline":       `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"u","marks":[{"type":"underline"}]}]}]}`,
		"strong+code":     `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"x","marks":[{"type":"strong"},{"type":"code"}]}]}]}`,
		"list start":      `{"type":"doc","content":[{"type":"orderedList","attrs":{"order":3},"content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"c"}]}]}]}]}`,
		"md-looking text": `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"# not a heading"}]}]}`,
		"plain string":    `"legacy text"`,
	}
	for name, doc := range cases {
		if _, ok := editableMarkdown(json.RawMessage(doc)); ok {
			t.Errorf("%s: should not be editable", name)
		}
	}
	if md, ok := editableMarkdown(nil); !ok || md != "" {
		t.Error("an empty description is always editable")
	}
}

func TestADFEquivalenceIgnoresNoise(t *testing.T) {
	a := `{"version":1,"type":"doc","content":[{"type":"taskList","attrs":{"localId":"x"},"content":[{"type":"taskItem","attrs":{"localId":"y","state":"TODO"},"content":[{"type":"text","text":"a "},{"type":"text","text":"b"}]}]}]}`
	b := `{"type":"doc","content":[{"type":"taskList","attrs":{"localId":"md-1"},"content":[{"type":"taskItem","attrs":{"localId":"md-2","state":"TODO"},"content":[{"type":"text","text":"a b"}]}]}]}`
	if !adfEquivalent(json.RawMessage(a), json.RawMessage(b)) {
		t.Error("localIds, version and text splitting must not matter")
	}
	spaced := `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"a"}]},{"type":"paragraph","content":[]},{"type":"codeBlock","attrs":{"language":""},"content":[{"type":"text","text":"x"}]}]}`
	tight := `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"a"}]},{"type":"codeBlock","content":[{"type":"text","text":"x"}]}]}`
	if !adfEquivalent(json.RawMessage(spaced), json.RawMessage(tight)) {
		t.Error("empty paragraphs and empty attrs are spacing/noise")
	}
	c := strings.Replace(b, `"TODO"`, `"DONE"`, 1)
	if adfEquivalent(json.RawMessage(a), json.RawMessage(c)) {
		t.Error("a changed task state must matter")
	}
}
