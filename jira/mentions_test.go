package jira

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func mentionServer(t *testing.T, calls *int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/user/search" {
			t.Errorf("unexpected request: %s", r.URL)
		}
		*calls++
		switch strings.ToLower(r.URL.Query().Get("query")) {
		case "ada lovelace":
			// The exact name wins over a longer one and over a bot.
			fmt.Fprint(w, `[
				{"accountId":"a1","displayName":"Ada Lovelace","accountType":"atlassian","active":true},
				{"accountId":"a2","displayName":"Ada Lovelace Jr","accountType":"atlassian","active":true},
				{"accountId":"b1","displayName":"Ada Lovelace","accountType":"app","active":true}]`)
		case "grace":
			fmt.Fprint(w, `[{"accountId":"g1","displayName":"Grace Hopper","accountType":"atlassian","active":true},
				{"accountId":"g0","displayName":"Grace Old","accountType":"atlassian","active":false}]`)
		case "alan":
			fmt.Fprint(w, `[{"accountId":"t1","displayName":"Alan Turing","accountType":"atlassian","active":true},
				{"accountId":"k1","displayName":"Alan Kay","accountType":"atlassian","active":true}]`)
		default:
			fmt.Fprint(w, `[]`)
		}
	}))
}

func TestResolveMentions(t *testing.T) {
	calls := 0
	server := mentionServer(t, &calls)
	defer server.Close()
	c := NewBasicClient(server.URL, "test@example.com", "token")

	md := "Hi @[Ada Lovelace] and @[Grace], see `@[Alan]`.\n" +
		"```\n@[Alan]\n```\n" +
		"Again @[ada lovelace]; kept: @[Someone](x9)."
	got, err := c.ResolveMentions(md)
	if err != nil {
		t.Fatal(err)
	}
	want := "Hi @[Ada Lovelace](a1) and @[Grace Hopper](g1), see `@[Alan]`.\n" +
		"```\n@[Alan]\n```\n" +
		"Again @[Ada Lovelace](a1); kept: @[Someone](x9)."
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if calls != 2 {
		t.Fatalf("want one lookup per distinct name (2), got %d", calls)
	}
}

func TestResolveMentionsErrors(t *testing.T) {
	calls := 0
	server := mentionServer(t, &calls)
	defer server.Close()
	c := NewBasicClient(server.URL, "test@example.com", "token")

	if _, err := c.ResolveMentions("ping @[Alan]"); err == nil ||
		!strings.Contains(err.Error(), "Alan Turing (t1)") || !strings.Contains(err.Error(), "Alan Kay (k1)") {
		t.Fatalf("ambiguous name must list the candidates, got %v", err)
	}
	if _, err := c.ResolveMentions("ping @[Nobody Here]"); err == nil || !strings.Contains(err.Error(), "matches nobody") {
		t.Fatalf("unknown name must fail, got %v", err)
	}
}

func TestResolveMentionsWithoutMentionsMakesNoRequest(t *testing.T) {
	calls := 0
	server := mentionServer(t, &calls)
	defer server.Close()
	c := NewBasicClient(server.URL, "test@example.com", "token")

	if got, err := c.ResolveMentions("mail me @ home [x](y)"); err != nil || got != "mail me @ home [x](y)" || calls != 0 {
		t.Fatalf("got %q, err %v, calls %d", got, err, calls)
	}
}
