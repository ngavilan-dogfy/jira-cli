package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// A synthetic document exercising every node type the renderer handles.
const adfKitchenSink = `{"type":"doc","version":1,"content":[
 {"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"Contexto"}]},
 {"type":"paragraph","content":[
   {"type":"text","text":"La migración a "},
   {"type":"text","text":"acme-platform-stg","marks":[{"type":"code"}]},
   {"type":"text","text":" está "},
   {"type":"text","text":"casi completa","marks":[{"type":"strong"}]},
   {"type":"text","text":", ver TST-80 y "},
   {"type":"text","text":"el runbook","marks":[{"type":"link","attrs":{"href":"https://example.com/runbook"}}]},
   {"type":"text","text":". "},
   {"type":"mention","attrs":{"id":"x","text":"@Laura Vidal"}},
   {"type":"text","text":" "},
   {"type":"emoji","attrs":{"shortName":":smile:","text":"😄"}},
   {"type":"hardBreak"},
   {"type":"text","text":"Segunda línea tras un salto duro "},
   {"type":"status","attrs":{"text":"en curso","color":"yellow"}},
   {"type":"text","text":" hasta "},
   {"type":"date","attrs":{"timestamp":"1767225600000"}}
 ]},
 {"type":"bulletList","content":[
   {"type":"listItem","content":[
     {"type":"paragraph","content":[{"type":"text","text":"Primer punto con una frase lo bastante larga como para obligar a partir la línea en varias"}]},
     {"type":"bulletList","content":[
       {"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"anidado"}]}]}
     ]}
   ]},
   {"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"Segundo"}]}]}
 ]},
 {"type":"orderedList","attrs":{"order":9},"content":[
   {"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"nueve"}]}]},
   {"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"diez"}]}]}
 ]},
 {"type":"taskList","attrs":{"localId":"t"},"content":[
   {"type":"taskItem","attrs":{"localId":"a","state":"DONE"},"content":[{"type":"text","text":"hecho"}]},
   {"type":"taskItem","attrs":{"localId":"b","state":"TODO"},"content":[{"type":"text","text":"pendiente"}]}
 ]},
 {"type":"codeBlock","attrs":{"language":"sh"},"content":[{"type":"text","text":"gcloud run services list --project acme-platform-pro --region eu-west1 --format=json\n  ├── nested"}]},
 {"type":"blockquote","content":[{"type":"paragraph","content":[{"type":"text","text":"Branch is not allowed to deploy to production due to environment protection rules."}]}]},
 {"type":"panel","attrs":{"panelType":"warning"},"content":[{"type":"paragraph","content":[{"type":"text","text":"No borrar hasta TST-80."}]}]},
 {"type":"rule"},
 {"type":"table","content":[
   {"type":"tableRow","content":[
     {"type":"tableHeader","content":[{"type":"paragraph","content":[{"type":"text","text":"Recurso"}]}]},
     {"type":"tableHeader","content":[{"type":"paragraph","content":[{"type":"text","text":"Bloqueador"}]}]},
     {"type":"tableHeader","content":[{"type":"paragraph","content":[{"type":"text","text":"Acción"}]}]}
   ]},
   {"type":"tableRow","content":[
     {"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"LB "},{"type":"text","text":"custom-domains-lb","marks":[{"type":"code"}]}]}]},
     {"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"sirve el 301 de example.es/www (DNS externo)"}]}]},
     {"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"mover .es al LB de platform"}]}]}
   ]}
 ]},
 {"type":"mediaSingle","content":[{"type":"media","attrs":{"id":"55bb","type":"file","collection":""}}]},
 {"type":"expand","attrs":{"title":"Más detalles"},"content":[{"type":"paragraph","content":[{"type":"text","text":"oculto"}]}]},
 {"type":"paragraph","content":[{"type":"inlineCard","attrs":{"url":"https://registrar.example.com/whois/results.aspx?domain=example.org"}}]},
 {"type":"paragraph","content":[]},
 {"type":"somethingNew","content":[{"type":"text","text":"desconocido"}]}
]}`

func TestRenderADFNeverExceedsWidth(t *testing.T) {
	for _, w := range []int{10, 14, 20, 27, 33, 47, 60, 80, 120, 200} {
		lines := renderADF(json.RawMessage(adfKitchenSink), w)
		if len(lines) == 0 {
			t.Fatalf("w=%d: no output", w)
		}
		for i, l := range lines {
			if got := ansi.StringWidth(l); got > w {
				t.Errorf("w=%d line %d is %d wide: %q", w, i, got, ansi.Strip(l))
			}
		}
	}
}

func TestRenderADFContent(t *testing.T) {
	lines := renderADF(json.RawMessage(adfKitchenSink), 80)
	text := ansi.Strip(strings.Join(lines, "\n"))
	for _, want := range []string{
		"Contexto",
		"La migración a acme-platform-stg está casi completa, ver TST-80 y el runbook.",
		"@Laura Vidal",
		"Segunda línea tras un salto duro  EN CURSO  hasta 1 Jan 2026",
		"• Primer punto",
		"  – anidado",
		"• Segundo",
		" 9. nueve",
		"10. diez",
		"▣ hecho",
		"□ pendiente",
		"gcloud run services list",
		"▌ Branch is not allowed",
		"▌ WARNING",
		"┌", "Recurso", "Bloqueador", "┘",
		"▣ attachment",
		"▼ Más detalles",
		"registrar.example.com/whois/results.aspx",
		"desconocido",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	// A heading hugs its paragraph; other blocks are separated by a blank line.
	if !strings.HasPrefix(text, "Contexto\nLa migración") {
		t.Errorf("heading should hug its content:\n%s", text)
	}
	if strings.Contains(text, "\n\n\n") {
		t.Errorf("double blank lines:\n%s", text)
	}
}

func TestRenderADFWrapsListsWithHangingIndent(t *testing.T) {
	lines := renderADF(json.RawMessage(adfKitchenSink), 40)
	text := ansi.Strip(strings.Join(lines, "\n"))
	if !strings.Contains(text, "• Primer punto con una frase lo bastante\n  larga") {
		t.Errorf("list continuation lines should align under the text:\n%s", text)
	}
}

func TestRenderADFNarrowTableFallsBackToRecords(t *testing.T) {
	lines := renderADF(json.RawMessage(adfKitchenSink), 24)
	text := ansi.Strip(strings.Join(lines, "\n"))
	if strings.Contains(text, "┬") {
		t.Errorf("24 cells can't fit a 3-column grid; expected record layout:\n%s", text)
	}
	if !strings.Contains(text, "Recurso: LB") {
		t.Errorf("record layout should label values with headers:\n%s", text)
	}
}

func TestRenderADFPlainString(t *testing.T) {
	lines := renderADF(json.RawMessage(`"hola mundo\n\nsegundo párrafo"`), 20)
	if got := ansi.Strip(strings.Join(lines, "|")); got != "hola mundo||segundo párrafo" {
		t.Errorf("got %q", got)
	}
	if renderADF(nil, 20) != nil || renderADF(json.RawMessage("null"), 20) != nil {
		t.Error("empty bodies should render nothing")
	}
}

func TestWrapRunsHardBreaksLongWords(t *testing.T) {
	lines := wrapRuns([]run{{text: "a " + strings.Repeat("x", 25) + " b"}}, 10)
	got := strings.Join(lines, "|")
	if got != "a|xxxxxxxxxx|xxxxxxxxxx|xxxxx b" {
		t.Errorf("got %q", got)
	}
}

func TestAllocate(t *testing.T) {
	w := allocate([]int{10, 50, 5}, []int{4, 8, 5}, 40)
	sum := w[0] + w[1] + w[2]
	if sum > 40 {
		t.Errorf("allocated %v (%d) over budget", w, sum)
	}
	if w[2] != 5 || w[0] < 4 || w[1] < 8 {
		t.Errorf("unexpected allocation %v", w)
	}
	// Everything fits: natural widths.
	if got := allocate([]int{3, 4}, []int{3, 3}, 50); got[0] != 3 || got[1] != 4 {
		t.Errorf("got %v", got)
	}
}
