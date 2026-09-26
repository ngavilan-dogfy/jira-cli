package cmd

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// A made-up token with the shape of Atlassian's.
var fakeToken = "ATATT3xFfGF0" + strings.Repeat("aB3_x-Q9", 20) + "=1A2B3C4D"

func newTokenModel(skip map[string]bool) *keyModel {
	in := textinput.New()
	in.Focus()
	return &keyModel{p: keyPrompt{Title: "Your API token", Label: "API token", Clean: cleanToken,
		Match: reAtlassianToken.MatchString, Skip: skip}, input: in}
}

func TestTokenPromptTakesACopiedToken(t *testing.T) {
	m := newTokenModel(nil)
	m.Update(clipMsg{text: "https://example.com/some/link"})
	if m.value != "" {
		t.Fatal("a link isn't a token")
	}
	if !strings.Contains(m.View(), "click Copy on the API token") {
		t.Errorf("should say it watches the clipboard:\n%s", m.View())
	}
	m.Update(clipMsg{text: fakeToken[:60] + "\n" + fakeToken[60:]}) // wrapped by some terminal
	if m.value != fakeToken || !m.fromClip {
		t.Fatalf("should take the copied token, got %q", m.value)
	}
}

func TestTokenPromptSkipsRefusedTokens(t *testing.T) {
	m := newTokenModel(map[string]bool{fakeToken: true})
	m.Update(clipMsg{text: fakeToken})
	if m.offered != "" {
		t.Error("a token Atlassian refused isn't offered again")
	}
	m.Update(clipMsg{text: "other"})
	m.Update(clipMsg{text: fakeToken})
	if m.value != "" {
		t.Error("…nor taken again")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.err != errCancelled {
		t.Error("esc cancels")
	}
}

func TestAtlassianTokenShape(t *testing.T) {
	for tok, want := range map[string]bool{
		fakeToken:                        true,
		"ATATT3xFfGF0short":              false,
		"0123456789abcdef0123456789abcd": false,
		"my password":                    false,
	} {
		if got := reAtlassianToken.MatchString(cleanToken(tok)); got != want {
			t.Errorf("%.20q… matches = %v, want %v", tok, got, want)
		}
	}
	if maskKey(fakeToken) != "****3C4D" {
		t.Errorf("maskKey = %q", maskKey(fakeToken))
	}
}

func TestEmailForPicksTheWorkIdentity(t *testing.T) {
	orig := gitEmails
	defer func() { gitEmails = orig }()
	gitEmails = func() []string { return []string{"me@gmail.com", "ana@acme.com", "ana@other.org"} }
	if got := emailFor("acme.atlassian.net"); got != "ana@acme.com" {
		t.Errorf("acme site → %q, want the acme email", got)
	}
	if got := emailFor("unrelated.atlassian.net"); got != "me@gmail.com" {
		t.Errorf("no match → the global email, got %q", got)
	}
	gitEmails = func() []string { return nil }
	if got := emailFor("acme.atlassian.net"); got != "" {
		t.Errorf("no git → empty, got %q", got)
	}
}
