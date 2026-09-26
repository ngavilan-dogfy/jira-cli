package cmd

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ngavilan-dogfy/jira-cli/config"
	"github.com/spf13/cobra"
)

// fakeSite is a tiny Jira Cloud: serverInfo (anonymous), myself (basic
// auth ana@acme.com / good-token), projects and search.
func fakeSite(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/api/3/serverInfo" {
			io.WriteString(w, `{"baseUrl":"https://acme.atlassian.net","deploymentType":"Cloud"}`)
			return
		}
		if u, p, ok := r.BasicAuth(); !ok || u != "ana@acme.com" || p != "good-token" {
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, "Client must be authenticated to access this resource.")
			return
		}
		switch r.URL.Path {
		case "/rest/api/3/myself":
			io.WriteString(w, `{"accountId":"a1","displayName":"Ana García","emailAddress":"ana@acme.com","active":true}`)
		case "/rest/api/3/project":
			io.WriteString(w, `[{"id":"2","key":"WEB","name":"Website"},{"id":"1","key":"OPS","name":"Operations"}]`)
		case "/rest/api/3/search/jql":
			io.WriteString(w, `{"issues":[],"isLast":true}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// useFakeSite points setup/doctor at srv and gives the test its own home.
func useFakeSite(t *testing.T, srv *httptest.Server) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	for _, v := range []string{"JIRA_DOMAIN", "JIRA_EMAIL", "JIRA_TOKEN", "JIRA_PROJECT", "JIRA_REPOS"} {
		t.Setenv(v, "")
	}
	oldProbe, oldBase, oldOpen := siteProbeURL, apiBaseFor, openURL
	siteProbeURL = func(siteRef) string { return srv.URL + "/rest/api/3/serverInfo" }
	apiBaseFor = func(*config.Profile) string { return srv.URL }
	openURL = func(string) { t.Error("tests must not open a browser") }
	t.Cleanup(func() { siteProbeURL, apiBaseFor, openURL = oldProbe, oldBase, oldOpen })
}

func resetSetupFlags() {
	setupProfile, setupMethod, setupSite, setupEmail, setupProject = "", "", "", "", ""
	setupTokenStdin = false
}

func TestParseSite(t *testing.T) {
	cases := []struct {
		in, domain, project, err string
	}{
		{in: "acme", domain: "acme"},
		{in: "  ACME ", domain: "acme"},
		{in: "acme.atlassian.net", domain: "acme"},
		{in: "https://acme.atlassian.net/", domain: "acme"},
		{in: "acme.atlassian.net:443", domain: "acme"},
		{in: "https://acme.atlassian.net/browse/OPS-12", domain: "acme", project: "OPS"},
		{in: "https://acme.atlassian.net/jira/software/c/projects/web/boards/3", domain: "acme", project: "WEB"},
		{in: "https://acme.atlassian.net/jira/your-work?selectedIssue=OPS-7", domain: "acme", project: "OPS"},
		{in: "https://my-team.atlassian.net/jira/core/projects/HR2/list", domain: "my-team", project: "HR2"},
		{in: "", err: "type your site"},
		{in: "https://jira.acme.com/browse/OPS-1", err: "isn't a Jira Cloud site"},
		{in: "acme corp", err: "isn't a valid site name"},
	}
	for _, c := range cases {
		ref, err := parseSite(c.in)
		if c.err != "" {
			if err == nil || !strings.Contains(err.Error(), c.err) {
				t.Errorf("parseSite(%q) error = %v, want it to mention %q", c.in, err, c.err)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseSite(%q): %v", c.in, err)
			continue
		}
		if ref.domain != c.domain || ref.project != c.project || ref.url != "https://"+c.domain+".atlassian.net" {
			t.Errorf("parseSite(%q) = %+v, want domain %q project %q", c.in, ref, c.domain, c.project)
		}
	}
}

func TestProbeSite(t *testing.T) {
	status, body := 200, `{"baseUrl":"https://acme.atlassian.net"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	defer srv.Close()
	old := siteProbeURL
	defer func() { siteProbeURL = old }()
	siteProbeURL = func(siteRef) string { return srv.URL }
	ref := siteRef{domain: "acme"}

	if err := probeSite(ref); err != nil {
		t.Fatalf("real site: %v", err)
	}
	status = 404
	if err := probeSite(ref); err == nil || !strings.Contains(err.Error(), "no Jira site at acme.atlassian.net") {
		t.Errorf("404: %v", err)
	}
	status, body = 200, "<html>parked domain</html>"
	if err := probeSite(ref); err == nil || !strings.Contains(err.Error(), "doesn't look like Jira") {
		t.Errorf("not Jira: %v", err)
	}
	siteProbeURL = func(siteRef) string { return "http://127.0.0.1:1/serverInfo" }
	if err := probeSite(ref); err == nil || !strings.Contains(err.Error(), "can't reach acme.atlassian.net") {
		t.Errorf("unreachable: %v", err)
	}
}

func TestExplainAuthError(t *testing.T) {
	msg, fix := explainAuthError(errors.New("jira API error 401: Client must be authenticated"), "ana@acme.com")
	if !strings.Contains(msg, "didn't accept") || !strings.Contains(fix, "ana@acme.com") || !strings.Contains(fix, apiTokensPage) {
		t.Errorf("401 → %q / %q", msg, fix)
	}
	if msg, fix := explainAuthError(errors.New("jira API error 403: Forbidden"), ""); !strings.Contains(msg, "refused") || !strings.Contains(fix, "OAuth") {
		t.Errorf("403 → %q / %q", msg, fix)
	}
	if msg, _ := explainAuthError(errors.New(`Get "https://x.atlassian.net": dial tcp: lookup x.atlassian.net: no such host`), ""); msg != "Couldn't reach Jira: no such host" {
		t.Errorf("network → %q", msg)
	}
}

func TestSetupNonInteractive(t *testing.T) {
	srv := fakeSite(t)
	useFakeSite(t, srv)
	defer resetSetupFlags()

	// Nothing given: says exactly what's missing.
	resetSetupFlags()
	err := setupNonInteractive("default", nil)
	for _, want := range []string{"--site", "--email", "--token-stdin"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("missing flags: error %v should mention %s", err, want)
		}
	}

	// Wrong token: explained, nothing saved.
	setupSite, setupEmail = "https://acme.atlassian.net/browse/OPS-3", "ana@acme.com"
	t.Setenv("JIRA_TOKEN", "bad-token")
	if err := setupNonInteractive("default", nil); err == nil || !strings.Contains(err.Error(), "didn't accept") {
		t.Fatalf("bad token: %v", err)
	}
	if config.Exists("default") {
		t.Fatal("a failed setup must not save anything")
	}

	// Right token: saved privately, activated, project from the pasted link.
	t.Setenv("JIRA_TOKEN", "good-token")
	if err := setupNonInteractive("default", nil); err != nil {
		t.Fatal(err)
	}
	p, err := config.Load("default")
	if err != nil {
		t.Fatal(err)
	}
	if p.Domain != "acme" || p.Email != "ana@acme.com" || p.Token != "good-token" || p.Project != "OPS" || p.AuthMethod != "token" {
		t.Errorf("saved profile = %+v", p)
	}
	if config.ActiveName() != "default" {
		t.Errorf("active = %q", config.ActiveName())
	}
	fi, _ := os.Stat(filepath.Join(config.ProfileDir(), "default.yaml"))
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("profile mode = %v, want 0600", fi.Mode().Perm())
	}

	// No project anywhere: the first one alphabetically.
	setupSite = "acme"
	if err := setupNonInteractive("work", nil); err != nil {
		t.Fatal(err)
	}
	if p, _ := config.Load("work"); p.Project != "OPS" {
		t.Errorf("default project = %q, want OPS (first by key)", p.Project)
	}

	// OAuth can't work without a terminal.
	setupMethod = "oauth"
	if err := setupNonInteractive("x", nil); err == nil || !strings.Contains(err.Error(), "needs a terminal") {
		t.Errorf("oauth without terminal: %v", err)
	}
}

func TestSetupTokenFromStdin(t *testing.T) {
	srv := fakeSite(t)
	useFakeSite(t, srv)
	defer resetSetupFlags()
	resetSetupFlags()

	r, w, _ := os.Pipe()
	io.WriteString(w, "good-\ntoken\n") // pasted with a line break
	w.Close()
	old := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = old }()

	setupSite, setupEmail, setupProject, setupTokenStdin = "acme", "ana@acme.com", "web", true
	if err := setupNonInteractive("default", nil); err != nil {
		t.Fatal(err)
	}
	if p, _ := config.Load("default"); p.Token != "good-token" || p.Project != "WEB" {
		t.Errorf("profile = %+v", p)
	}
}

func TestNeedsAuth(t *testing.T) {
	find := func(args ...string) *cobra.Command {
		c, _, err := rootCmd.Find(args)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		return c
	}
	for _, args := range [][]string{{}, {"setup"}, {"login"}, {"doctor"}, {"version"}, {"update"}, {"profile", "list"}} {
		if needsAuth(find(args...)) {
			t.Errorf("jira %s shouldn't need a Jira sign-in", strings.Join(args, " "))
		}
	}
	for _, args := range [][]string{{"ls"}, {"ui"}, {"show"}} {
		if !needsAuth(find(args...)) {
			t.Errorf("jira %s needs a Jira sign-in", strings.Join(args, " "))
		}
	}
}

func TestNotSetUpError(t *testing.T) {
	old := cfg
	defer func() { cfg = old }()
	cfg = nil
	t.Setenv("JIRA_DOMAIN", "acme")
	t.Setenv("JIRA_EMAIL", "")
	t.Setenv("JIRA_TOKEN", "x")
	if err := notSetUpError(); !strings.Contains(err.Error(), "JIRA_EMAIL missing") {
		t.Errorf("partial env: %v", err)
	}
	t.Setenv("JIRA_DOMAIN", "")
	t.Setenv("JIRA_TOKEN", "")
	if err := notSetUpError(); !strings.Contains(err.Error(), "jira setup") || !strings.Contains(err.Error(), "JIRA_TOKEN") {
		t.Errorf("nothing set: %v", err)
	}
}

func TestDoctorJSON(t *testing.T) {
	srv := fakeSite(t)
	useFakeSite(t, srv)
	oldLook, oldProbe := lookTool, probeTool
	defer func() { lookTool, probeTool = oldLook, oldProbe }()
	lookTool = func(name string) (string, error) {
		if name == "claude" {
			return "", errors.New("not found")
		}
		return "/usr/bin/" + name, nil
	}
	probeTool = func(_ time.Duration, name string, args ...string) (string, error) {
		switch name {
		case "git":
			return "git version 2.45.0", nil
		case "gh":
			return "github.com\n  ✓ Logged in to github.com account ana (keyring)", nil
		}
		return "", errors.New("unexpected " + name)
	}

	run := func(p *config.Profile) (map[string]check, error) {
		t.Helper()
		r, w, _ := os.Pipe()
		old := os.Stdout
		os.Stdout = w
		err := runDoctor(p, true)
		w.Close()
		os.Stdout = old
		var out struct {
			OK     bool    `json:"ok"`
			Checks []check `json:"checks"`
		}
		if jerr := json.NewDecoder(r).Decode(&out); jerr != nil {
			t.Fatalf("doctor --json isn't JSON: %v", jerr)
		}
		byName := map[string]check{}
		for _, c := range out.Checks {
			byName[c.Name] = c
		}
		if out.OK != (err == nil) {
			t.Errorf("ok=%v but err=%v", out.OK, err)
		}
		return byName, err
	}

	good := &config.Profile{Name: "default", AuthMethod: "token", Domain: "acme", Email: "ana@acme.com", Token: "good-token", Project: "OPS"}
	checks, err := run(good)
	if err != nil {
		t.Fatalf("healthy setup: %v", err)
	}
	for _, name := range []string{"site", "signin", "project", "git", "gh"} {
		if checks[name].Status != "ok" {
			t.Errorf("%s = %+v, want ok", name, checks[name])
		}
	}
	if !strings.Contains(checks["signin"].Detail, "Ana García") || !strings.Contains(checks["gh"].Detail, "as ana") {
		t.Errorf("details: %q / %q", checks["signin"].Detail, checks["gh"].Detail)
	}
	if checks["claude"].Status != "info" || !strings.Contains(checks["claude"].Fix, "claude.com") {
		t.Errorf("claude missing = %+v", checks["claude"])
	}

	bad := *good
	bad.Token, bad.Project = "bad-token", "NOPE"
	checks, err = run(&bad)
	var quiet quietError
	if !errors.As(err, &quiet) {
		t.Fatalf("failed checks should end quietly with exit 1, got %v", err)
	}
	if checks["signin"].Status != "fail" || !strings.Contains(checks["signin"].Fix, "API token, not your password") {
		t.Errorf("bad token = %+v", checks["signin"])
	}

	bad.Token = "good-token"
	checks, _ = run(&bad)
	if c := checks["project"]; c.Status != "fail" || !strings.Contains(c.Fix, "OPS, WEB") {
		t.Errorf("unknown project = %+v", c)
	}

	checks, _ = run(nil)
	if c := checks["profile"]; c.Status != "fail" || c.Fix != "Run: jira setup" {
		t.Errorf("no profile = %+v", c)
	}
}
