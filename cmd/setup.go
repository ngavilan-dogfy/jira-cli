package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/ngavilan-dogfy/jira-cli/config"
	"github.com/ngavilan-dogfy/jira-cli/jira"
	"github.com/ngavilan-dogfy/jira-cli/tui"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
)

var (
	setupProfile    string
	setupMethod     string
	setupSite       string
	setupEmail      string
	setupProject    string
	setupTokenStdin bool
)

var setupCmd = &cobra.Command{
	Use:     "setup",
	Aliases: []string{"login", "auth", "init"},
	Short:   "Connect jira to your Jira Cloud site, step by step",
	Long: `A guided setup that connects this CLI to your Jira Cloud site. It takes
about two minutes and you can re-run it any time: current values are the
defaults, so you only change what you want.

  Step 1  Your Jira site      paste any Jira link, or just the site name
  Step 2  Sign in             API token (recommended) or browser login (OAuth)
  Step 3  Default project     where 'jira ls' and 'jira ui' start
  Step 4  Extras (optional)   your repos folder, GitHub sign-in, the Claude Code skill

With an API token you don't have to paste: click Copy on the new token and
setup takes it from your clipboard (and clears it after saving). Your email
comes from git: the work one, when you keep several.

Settings are saved in ~/.config/jira-cli/ (only readable by you).

Without a terminal (scripts, CI, agents) pass everything as flags and the
token on stdin (or in $JIRA_TOKEN); nothing is asked:

  echo "$TOKEN" | jira setup --site acme --email me@acme.com --token-stdin --project OPS

Or skip setup entirely with environment variables:

  JIRA_DOMAIN=acme JIRA_EMAIL=me@acme.com JIRA_TOKEN=... jira ls

Output:
  Guided: questions and a summary. Non-interactive: one line saying what was
  configured; errors explain what's wrong and how to fix it.

Examples:
  jira setup                      # guided
  jira setup --profile work       # a second profile (switch: 'jira profile use work')
  jira setup --method oauth       # straight to the browser login`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		profile := setupProfile
		if profile == "" {
			profile = config.ActiveName()
		}
		if setupTokenStdin || !interactive() {
			existing, _ := config.Load(profile)
			return setupNonInteractive(profile, existing)
		}
		return runSetup(profile, "")
	},
}

// runSetup runs the guided setup. then is the command that triggered it on
// a first run: it carries on afterwards, so the wizard doesn't offer jira ui.
func runSetup(profile, then string) error {
	existing, _ := config.Load(profile)
	w := &wizard{profile: profile, existing: existing, then: then}
	err := w.run()
	if errors.Is(err, errCancelled) {
		fmt.Println()
		fmt.Println(wzMuted.Render("  Setup cancelled — nothing was changed. Run 'jira setup' whenever you're ready."))
		if then != "" {
			return fmt.Errorf("jira isn't set up yet — run 'jira setup'")
		}
		return nil
	}
	return err
}

// offerSetup is the first-run prompt for a command that needs Jira.
func offerSetup(then string) error {
	fmt.Println()
	fmt.Println(wzAccent.Render("  jira") + wzMuted.Render(" isn't connected to your Jira yet."))
	yes := true
	err := ask(huh.NewConfirm().
		Title("Set it up now? It takes about two minutes.").
		Description("Afterwards '" + then + "' runs as you asked.").
		Affirmative("Yes, set it up").Negative("Not now").Value(&yes))
	if err != nil || !yes {
		return fmt.Errorf("jira isn't set up yet — run 'jira setup' when you're ready")
	}
	return runSetup(config.ActiveName(), then)
}

// welcome is what a bare 'jira' shows before setup.
func welcome(cmd *cobra.Command) error {
	fmt.Println()
	fmt.Println(wzAccent.Render("  jira") + "  Jira Cloud from your terminal — for you and your AI agents")
	fmt.Println()
	fmt.Println("  You're not connected to Jira yet. Setup takes about two minutes:")
	fmt.Println("  paste a link to your Jira, sign in, pick a project.")
	fmt.Println()
	if !interactive() {
		fmt.Println("  Run " + cmdHint("jira setup") + " in a terminal, or set JIRA_DOMAIN, JIRA_EMAIL and JIRA_TOKEN.")
		fmt.Println("  " + cmdHint("jira --help") + " lists every command.")
		return nil
	}
	yes := true
	if err := ask(huh.NewConfirm().Title("Set it up now?").Affirmative("Yes").Negative("Not now").Value(&yes)); err != nil || !yes {
		fmt.Println(wzMuted.Render("  Run 'jira setup' when you're ready · 'jira --help' lists every command."))
		return nil
	}
	return runSetup(config.ActiveName(), "")
}

// ─── the guided flow ─────────────────────────────────────────────

const setupSteps = 4

type wizard struct {
	profile  string
	existing *config.Profile
	then     string

	site   siteRef
	p      *config.Profile
	client *jira.Client
	me     *jira.Myself
}

func (w *wizard) run() error {
	fmt.Println()
	fmt.Println(wzAccent.Render("  jira setup"))
	fmt.Println(wzMuted.Render("  Connect this CLI to your Jira Cloud site — about two minutes."))
	fmt.Println(wzMuted.Render("  Nothing is saved until the end. Ctrl+C cancels at any point."))

	if w.existing != nil && w.existing.IsAuthenticated() && w.then == "" {
		next, err := w.askReconfigure()
		if err != nil || next == "done" {
			return err
		}
		if next == "project" {
			w.useExisting()
			if err := w.stepProject(); err != nil {
				return err
			}
			return w.save()
		}
	}
	if err := w.stepSite(); err != nil {
		return err
	}
	if err := w.stepSignIn(); err != nil {
		return err
	}
	if err := w.stepProject(); err != nil {
		return err
	}
	if err := w.save(); err != nil {
		return err
	}
	if err := w.stepExtras(); err != nil && !errors.Is(err, errCancelled) {
		return err
	}
	return w.done()
}

// askReconfigure handles a profile that's already set up.
func (w *wizard) askReconfigure() (string, error) {
	e := w.existing
	fmt.Println()
	sayOK(fmt.Sprintf("Profile %q is set up: %s · project %s · %s",
		w.profile, e.BrowseBaseURL(), orNone(e.Project), authLabel(e)))
	fmt.Println()
	choice := "check"
	err := ask(huh.NewSelect[string]().
		Title("What would you like to do?").
		Options(
			huh.NewOption("Check that everything works", "check"),
			huh.NewOption("Change the default project", "project"),
			huh.NewOption("Set it up again (site, sign-in, project…)", "all"),
			huh.NewOption("Nothing, leave it as it is", "done"),
		).Value(&choice))
	if err != nil {
		return "", err
	}
	switch choice {
	case "check":
		return "done", runDoctor(e, false)
	case "done":
		return "done", nil
	}
	return choice, nil
}

// useExisting works with the saved profile as it is.
func (w *wizard) useExisting() {
	w.p = w.existing
	w.client = buildClient(w.p)
	w.site, _ = parseSite(w.p.BrowseBaseURL())
}

// ─── step 1: site ────────────────────────────────────────────────

type siteRef struct {
	domain  string // acme (for acme.atlassian.net)
	url     string // https://acme.atlassian.net
	project string // project key found in the pasted link, if any
}

var (
	reBrowseKey   = regexp.MustCompile(`/browse/([A-Za-z][A-Za-z0-9_]+)-\d+`)
	reProjectPath = regexp.MustCompile(`/projects/([A-Za-z][A-Za-z0-9_]+)`)
	reIssueParam  = regexp.MustCompile(`(?:selectedIssue|issueKey)=([A-Za-z][A-Za-z0-9_]+)-\d+`)
	reSiteName    = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
)

// parseSite understands "acme", "acme.atlassian.net" and any Jira link,
// picking up a project key from links like /browse/OPS-12.
func parseSite(input string) (siteRef, error) {
	in := strings.TrimSpace(input)
	if in == "" {
		return siteRef{}, fmt.Errorf("type your site, e.g. acme")
	}
	var ref siteRef
	for _, re := range []*regexp.Regexp{reBrowseKey, reProjectPath, reIssueParam} {
		if m := re.FindStringSubmatch(in); m != nil {
			ref.project = strings.ToUpper(m[1])
			break
		}
	}
	host := strings.ToLower(in)
	if strings.ContainsAny(host, "/.:") {
		if !strings.Contains(host, "://") {
			host = "https://" + host
		}
		u, err := url.Parse(host)
		if err != nil || u.Hostname() == "" {
			return siteRef{}, fmt.Errorf("that doesn't look like a Jira link")
		}
		host = u.Hostname()
		if !strings.HasSuffix(host, ".atlassian.net") {
			return siteRef{}, fmt.Errorf("%s isn't a Jira Cloud site — jira works with *.atlassian.net", host)
		}
		host = strings.TrimSuffix(host, ".atlassian.net")
	}
	if !reSiteName.MatchString(host) {
		return siteRef{}, fmt.Errorf("%q isn't a valid site name", host)
	}
	ref.domain = host
	ref.url = "https://" + host + ".atlassian.net"
	return ref, nil
}

// siteProbeURL is swappable in tests.
var siteProbeURL = func(ref siteRef) string { return ref.url + "/rest/api/3/serverInfo" }

// probeSite checks that the site exists and is Jira (serverInfo answers
// without signing in).
func probeSite(ref siteRef) error {
	hc := &http.Client{Timeout: 10 * time.Second}
	resp, err := hc.Get(siteProbeURL(ref))
	if err != nil {
		return fmt.Errorf("can't reach %s.atlassian.net: %s", ref.domain, rootCause(err))
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return fmt.Errorf("there's no Jira site at %s.atlassian.net", ref.domain)
	case resp.StatusCode >= 400:
		return fmt.Errorf("%s.atlassian.net answered %d %s", ref.domain, resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	var info struct {
		BaseURL string `json:"baseUrl"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&info) != nil || info.BaseURL == "" {
		return fmt.Errorf("%s.atlassian.net doesn't look like Jira", ref.domain)
	}
	return nil
}

// rootCause keeps the last part of Go's nested network errors.
func rootCause(err error) string {
	s := err.Error()
	if i := strings.LastIndex(s, ": "); i >= 0 {
		s = s[i+2:]
	}
	return s
}

func (w *wizard) stepSite() error {
	stepHeader(1, setupSteps, "Your Jira site",
		"Open Jira in your browser, copy any link from the address bar and paste it here —\nor just type the site name (acme for acme.atlassian.net).")
	input := setupSite
	if input == "" && w.existing != nil && w.existing.Domain != "" {
		input = w.existing.BrowseBaseURL()
	}
	for {
		err := ask(huh.NewInput().
			Title("Jira site").
			Placeholder("acme  ·  acme.atlassian.net  ·  https://acme.atlassian.net/browse/OPS-12").
			Value(&input).
			Validate(func(s string) error { _, err := parseSite(s); return err }))
		if err != nil {
			return err
		}
		ref, _ := parseSite(input)
		err = withSpinner("Looking for "+ref.domain+".atlassian.net", func() error { return probeSite(ref) })
		if err == nil {
			sayOK("Found " + wzBold.Render(ref.domain+".atlassian.net"))
			if ref.project != "" {
				sayInfo("Noted project " + ref.project + " from your link")
			}
			w.site = ref
			return nil
		}
		sayFail(err.Error(), "Check the spelling, or copy the address from Jira in your browser.\n"+
			"On a company VPN or proxy? Make sure it's connected.")
	}
}

// ─── step 2: sign in ─────────────────────────────────────────────

var errSwitchMethod = errors.New("switch sign-in method")

const apiTokensPage = "https://id.atlassian.com/manage-profile/security/api-tokens"

func (w *wizard) stepSignIn() error {
	stepHeader(2, setupSteps, "Sign in",
		"jira acts as you: it sees and changes exactly what you can in Jira.")
	keep := false
	if e := w.existing; e != nil && e.IsAuthenticated() {
		old, _ := parseSite(e.BrowseBaseURL())
		keep = old.domain == w.site.domain
	}
	method := setupMethod
	for {
		var opts []huh.Option[string]
		if keep {
			opts = append(opts, huh.NewOption("Keep the current sign-in ("+authLabel(w.existing)+")", "keep"))
		}
		opts = append(opts,
			huh.NewOption("API token — recommended, takes a minute", "token"),
			huh.NewOption("Browser login (OAuth) — if your company turned API tokens off", "oauth"),
		)
		if method == "" || (method == "keep" && !keep) {
			method = opts[0].Value
		}
		err := ask(huh.NewSelect[string]().
			Title("How do you want to sign in?").
			Description("Both work with Google or SSO accounts. The API token is simpler;\nOAuth needs a one-time app in Atlassian's developer console.").
			Options(opts...).Value(&method))
		if err != nil {
			return err
		}
		switch method {
		case "keep":
			w.useExisting()
			err = withSpinner("Checking your sign-in", func() error { var e error; w.me, e = w.client.GetMyself(); return e })
			if err == nil {
				sayOK("Signed in as " + wzBold.Render(w.me.DisplayName))
				return nil
			}
			sayFail("The saved sign-in doesn't work anymore: "+err.Error(), "Choose how to sign in again.")
			keep, method = false, "token"
			continue
		case "token":
			err = w.signInToken()
			method = "oauth"
		case "oauth":
			err = w.signInOAuth()
			method = "token"
		}
		if !errors.Is(err, errSwitchMethod) {
			return err
		}
	}
}

func (w *wizard) signInToken() error {
	email := setupEmail
	if email == "" && w.existing != nil {
		email = w.existing.Email
	}
	if email == "" {
		email = emailFor(w.site.domain)
	}
	validEmail := func(s string) error {
		if !strings.Contains(strings.TrimSpace(s), "@") {
			return fmt.Errorf("that doesn't look like an email")
		}
		return nil
	}
	emailField := func() huh.Field {
		return huh.NewInput().Title("Your Atlassian email").
			Description("The one you sign in to Jira with.").Value(&email).Validate(validEmail)
	}
	if err := ask(emailField()); err != nil {
		return err
	}
	sayOK("Atlassian account " + wzBold.Render(strings.TrimSpace(email)))
	fmt.Println()
	fmt.Println("  " + wzBold.Render("Now create an API token:"))
	printSteps(
		"Your browser opens "+wzBold.Render("id.atlassian.com → Security → API tokens"),
		"Click "+wzBold.Render("Create API token")+" and call it "+wzBold.Render(tokenName()),
		"Pick an expiry date and press "+wzBold.Render("Create"),
		"Click "+wzBold.Render("Copy")+": setup takes the token from your clipboard",
	)
	fmt.Println()
	open := true
	if err := ask(huh.NewConfirm().
		Title("Open that page now?").
		Affirmative("Yes, open it").Negative("I already have a token").
		Value(&open)); err != nil {
		return err
	}
	if open {
		openURL(apiTokensPage)
		sayInfo("Opened " + apiTokensPage)
	}
	fmt.Println()
	tried := map[string]bool{}
	token := ""
	for {
		email = strings.TrimSpace(email)
		if token == "" {
			if len(tried) > 0 {
				fmt.Println()
			}
			t, err := readKey(keyPrompt{Title: "Your API token", Label: "API token",
				Hint:  "Hidden while you type, saved only on this machine.",
				Clean: cleanToken, Match: reAtlassianToken.MatchString, Skip: tried})
			if err != nil {
				return err
			}
			token = t
			tried[token] = true
		}
		p := &config.Profile{Name: w.profile, AuthMethod: "token", Domain: w.site.domain, SiteURL: w.site.url,
			Email: email, Token: token, MaxResults: 20}
		w.carry(p)
		c := jira.NewBasicClient(apiBaseFor(p), email, token)
		var me *jira.Myself
		err := withSpinner("Checking your token", func() error { var e error; me, e = c.GetMyself(); return e })
		if err == nil {
			sayOK("Signed in as " + wzBold.Render(me.DisplayName) + wzMuted.Render(" ("+email+")"))
			w.p, w.client, w.me = p, c, me
			return nil
		}
		msg, fix := explainAuthError(err, email)
		sayFail(msg, fix)
		next := "retry"
		if err := ask(huh.NewSelect[string]().Title("What now?").Options(
			huh.NewOption("Try another token", "retry"),
			huh.NewOption("Fix the email (and try this token again)", "email"),
			huh.NewOption("Open the API tokens page again", "page"),
			huh.NewOption("Sign in with the browser (OAuth) instead", "oauth"),
		).Value(&next)); err != nil {
			return err
		}
		switch next {
		case "email":
			if err := ask(emailField()); err != nil {
				return err
			}
			continue // same token, new email
		case "page":
			openURL(apiTokensPage)
		case "oauth":
			return errSwitchMethod
		}
		token = ""
	}
}

// explainAuthError turns a failed sign-in into what probably went wrong.
func explainAuthError(err error, email string) (msg, fix string) {
	s := err.Error()
	switch {
	case strings.Contains(s, "error 401"):
		return "Jira didn't accept that email + token",
			"• Is " + email + " the email you sign in to Atlassian with?\n" +
				"• It has to be an API token, not your password\n" +
				"• Copy the whole token (Atlassian's Copy button does it right)\n" +
				"• Tokens expire: create a new one at " + apiTokensPage
	case strings.Contains(s, "error 403"):
		return "Jira refused access for this account",
			"If your company turned API tokens off, use the browser login (OAuth);\n" +
				"otherwise ask your Jira admin for access to this site."
	case strings.Contains(s, "no such host"), strings.Contains(s, "timeout"),
		strings.Contains(s, "connection refused"), strings.Contains(s, "dial tcp"):
		return "Couldn't reach Jira: " + rootCause(err), "Check your connection or VPN and try again."
	}
	return "Sign-in failed: " + s, ""
}

func (w *wizard) signInOAuth() error {
	clientID := ""
	if w.existing != nil {
		clientID = w.existing.ClientID
	}
	if clientID == "" {
		clientID = config.GlobalClientID()
	}
	if clientID == "" {
		var err error
		if clientID, err = w.oauthAppSetup(); err != nil {
			return err
		}
	}
	secret := config.GlobalClientSecret()

	authURL, resultCh, errCh, err := jira.OAuthLogin(clientID, secret)
	if err != nil {
		sayFail("Couldn't start the browser login: "+err.Error(),
			"Another 'jira setup' may be waiting on the same port — close it and try again.")
		return errSwitchMethod
	}
	openURL(authURL)
	sayInfo("Your browser opened Atlassian's login — approve access there.")
	sayInfo(wzMuted.Render("Didn't open? Copy this into your browser: ") + authURL)
	var result jira.OAuthResult
	err = withSpinner("Waiting for you to approve in the browser (up to 5 minutes)", func() error {
		select {
		case result = <-resultCh:
			return nil
		case err := <-errCh:
			return err
		case <-time.After(5 * time.Minute):
			return fmt.Errorf("timed out waiting for the browser")
		}
	})
	if err != nil {
		sayFail("Browser login failed: "+err.Error(), "You can try again, or use an API token instead.")
		return errSwitchMethod
	}
	var site *jira.CloudSite
	var names []string
	for i, s := range result.Sites {
		ref, _ := parseSite(s.URL)
		names = append(names, strings.TrimPrefix(s.URL, "https://"))
		if ref.domain == w.site.domain {
			site = &result.Sites[i]
		}
	}
	if site == nil {
		fix := "Sign in to Atlassian with the account that has access to that site."
		if len(names) > 0 {
			fix = "This account can open: " + strings.Join(names, ", ") + "\n" + fix
		}
		sayFail("That account can't access "+w.site.domain+".atlassian.net", fix)
		return errSwitchMethod
	}
	p := &config.Profile{Name: w.profile, AuthMethod: "oauth", ClientID: clientID, CloudID: site.ID,
		AccessToken: result.Tokens.AccessToken, RefreshToken: result.Tokens.RefreshToken,
		TokenExpiry: result.ExpiresAt.Format(time.RFC3339), Domain: w.site.domain, SiteURL: site.URL, MaxResults: 20}
	w.carry(p)
	// Fresh tokens: no refresh can happen before the profile is saved.
	c := jira.NewOAuthClient(p.CloudID, p.SiteURL, p.AccessToken, p.RefreshToken, p.ClientID, secret, result.ExpiresAt, nil)
	me, err := c.GetMyself()
	if err != nil {
		sayFail("Signed in, but Jira rejected the request: "+err.Error(),
			"Check the app's scopes: read:jira-work, write:jira-work, read:jira-user and read:me.")
		return errSwitchMethod
	}
	sayOK("Signed in as " + wzBold.Render(me.DisplayName) + wzMuted.Render(" (browser login)"))
	w.p, w.client, w.me = p, c, me
	return nil
}

// oauthAppSetup walks through creating the Atlassian OAuth app (once per
// machine) and stores its credentials.
func (w *wizard) oauthAppSetup() (string, error) {
	open := true
	err := ask(
		huh.NewNote().
			Title("One-time: create an OAuth app (about 3 minutes)").
			Description("Atlassian's browser login needs an \"app\" that stands for this CLI.\n\n"+
				"1. Create → OAuth 2.0 integration, name it jira-cli\n"+
				"2. Permissions → Jira API → Add → Configure → Edit scopes:\n"+
				"     read:jira-work, write:jira-work, read:jira-user\n"+
				"   Permissions → User identity API → Add (read:me)\n"+
				"3. Authorization → OAuth 2.0 (3LO) → Add, callback URL:\n"+
				"     "+jira.CallbackURL()+"\n"+
				"4. Settings → copy the Client ID and the Secret"),
		huh.NewConfirm().Title("Open Atlassian's developer console now?").Affirmative("Yes").Negative("No").Value(&open),
	)
	if err != nil {
		return "", err
	}
	if open {
		openURL("https://developer.atlassian.com/console/myapps/")
	}
	id, secret := "", ""
	required := func(s string) error {
		if strings.TrimSpace(s) == "" {
			return fmt.Errorf("required")
		}
		return nil
	}
	if err := ask(
		huh.NewInput().Title("Client ID").Value(&id).Validate(required),
		huh.NewInput().Title("Secret").EchoMode(huh.EchoModePassword).Value(&secret).Validate(required),
	); err != nil {
		return "", err
	}
	id, secret = strings.TrimSpace(id), strings.TrimSpace(secret)
	if err := config.SetGlobalClientID(id); err != nil {
		return "", err
	}
	if err := config.SetGlobalClientSecret(secret); err != nil {
		return "", err
	}
	sayOK("App saved — you won't need to do this again on this machine")
	return id, nil
}

// carry keeps settings from the previous profile that setup doesn't ask.
func (w *wizard) carry(p *config.Profile) {
	if w.existing == nil {
		return
	}
	if w.existing.MaxResults > 0 {
		p.MaxResults = w.existing.MaxResults
	}
	p.Project = w.existing.Project
}

// ─── step 3: project ─────────────────────────────────────────────

func (w *wizard) stepProject() error {
	stepHeader(3, setupSteps, "Default project",
		"Where 'jira ls' and 'jira ui' start, and what short keys expand to (306 → KEY-306).")
	var projects []jira.Project
	err := withSpinner("Loading your projects", func() error { var e error; projects, e = w.client.GetProjects(); return e })
	if err != nil || len(projects) == 0 {
		if err != nil {
			sayWarn("Couldn't list your projects: "+err.Error(), "")
		} else {
			sayWarn("Jira didn't list any project for you", "You may not have access to any yet; type the key if you know it.")
		}
		key, _ := w.suggestProject(nil)
		if err := ask(huh.NewInput().Title("Project key").
			Description("The letters before the number in issue keys, like OPS in OPS-12.").
			Value(&key).Validate(func(s string) error {
			if strings.TrimSpace(s) == "" {
				return fmt.Errorf("type a project key")
			}
			return nil
		})); err != nil {
			return err
		}
		w.p.Project = strings.ToUpper(strings.TrimSpace(key))
		return nil
	}
	sort.Slice(projects, func(i, j int) bool { return projects[i].Key < projects[j].Key })
	key, why := w.suggestProject(projects)
	// The suggestion goes first: huh scrolls a preselected option to the top
	// of the list, which would hide the projects above it.
	opts := make([]huh.Option[string], 0, len(projects))
	for _, p := range projects {
		if p.Key == key {
			opts = append(opts, huh.NewOption(p.Key+"  "+p.Name+wzMuted.Render("  ← "+why), p.Key))
		}
	}
	for _, p := range projects {
		if p.Key != key {
			opts = append(opts, huh.NewOption(p.Key+"  "+p.Name, p.Key))
		}
	}
	sel := huh.NewSelect[string]().
		Title(fmt.Sprintf("Pick your default project (%d available)", len(projects))).
		Description("Type / to search. Change it any time with 'jira setup'.").
		Options(opts...).Value(&key)
	if len(opts) > 12 {
		sel = sel.Height(14)
	}
	if err := ask(sel); err != nil {
		return err
	}
	w.p.Project = key
	sayOK("Default project: " + wzBold.Render(key))
	return nil
}

// suggestProject picks the preselected project and says why: --project,
// the key in the pasted link, the current default — whichever exists —
// else the first one.
func (w *wizard) suggestProject(projects []jira.Project) (key, why string) {
	candidates := [][2]string{{setupProject, "from --project"}, {w.site.project, "from your link"}}
	if w.p != nil {
		candidates = append(candidates, [2]string{w.p.Project, "current"})
	}
	for _, c := range candidates {
		k := strings.ToUpper(strings.TrimSpace(c[0]))
		if k == "" {
			continue
		}
		if projects == nil {
			return k, c[1]
		}
		for _, p := range projects {
			if p.Key == k {
				return k, c[1]
			}
		}
	}
	if len(projects) > 0 {
		return projects[0].Key, "first"
	}
	return "", ""
}

func (w *wizard) save() error {
	if err := config.Save(w.p); err != nil {
		return fmt.Errorf("couldn't save your settings: %w", err)
	}
	if err := config.SetActive(w.profile); err != nil {
		return err
	}
	w.client = buildClient(w.p) // saves refreshed OAuth tokens from now on
	sayOK("Saved to " + tildePath(config.ProfileDir()+"/"+w.profile+".yaml") + wzMuted.Render(" (only readable by you)"))
	clearTakenKeys()
	return nil
}

// hostname is os.Hostname; the e2e build fixes it for recordings.
var hostname = os.Hostname

// reAtlassianToken is what an Atlassian API token looks like: ATATT…,
// about 190 characters.
var reAtlassianToken = regexp.MustCompile(`^ATATT[A-Za-z0-9_\-=]{40,}$`)

// cleanToken drops the spaces and line breaks a paste can bring.
func cleanToken(s string) string { return strings.Join(strings.Fields(s), "") }

// tokenName is what to call the token at Atlassian, so it's clear later
// where it's used.
func tokenName() string {
	host, _ := hostname()
	host = strings.TrimSuffix(strings.TrimSuffix(host, ".local"), ".lan")
	if i := strings.IndexByte(host, '.'); i > 0 {
		host = host[:i]
	}
	if host == "" {
		return "jira-cli"
	}
	return "jira-cli · " + strings.ToLower(host)
}

// ─── step 4: extras ──────────────────────────────────────────────

func (w *wizard) stepExtras() error {
	stepHeader(4, setupSteps, "Extras for jira ui (optional)",
		"jira ui links tickets to your git branches, pull requests and Claude Code sessions.\n"+
			"Nothing here is needed for the rest of the CLI.")
	for _, c := range extraChecks() {
		printCheck(c)
	}
	fmt.Println()
	if err := w.askRepos(); err != nil {
		return err
	}
	if err := offerGitHub(); err != nil {
		return err
	}
	if _, err := lookTool("claude"); err == nil {
		return offerSkill()
	}
	return nil
}

func (w *wizard) askRepos() error {
	root, n := tui.ReposRoot(w.profile)
	if root != "" {
		use := true
		if err := ask(huh.NewConfirm().
			Title(fmt.Sprintf("Use %s as your repos folder? (%d git repos inside)", tildePath(root), n)).
			Description("Branches there named like feat/KEY-12-… show up on their tickets.").
			Affirmative("Yes").Negative("Choose another").Value(&use)); err != nil {
			return err
		}
		if use {
			if err := tui.SetReposRoot(w.profile, root); err != nil {
				return err
			}
			sayOK(fmt.Sprintf("Repos folder: %s (%d repos)", tildePath(root), n))
			return nil
		}
	}
	dir := ""
	if err := ask(huh.NewInput().
		Title("Folder that holds your git repos (optional)").
		Description("The folder with your checkouts one level down, e.g. ~/code. Leave empty to skip.").
		Placeholder("~/code").Value(&dir).
		Validate(func(s string) error {
			if strings.TrimSpace(s) == "" {
				return nil
			}
			if p, n := tui.ReposIn(s); n == 0 {
				return fmt.Errorf("no git repositories directly inside %s", tildePath(p))
			}
			return nil
		})); err != nil {
		return err
	}
	if strings.TrimSpace(dir) == "" {
		sayInfo("Skipped — set it later in jira ui (press : → \"Set repositories folder\")")
		return nil
	}
	if err := tui.SetReposRoot(w.profile, dir); err != nil {
		return err
	}
	p, n := tui.ReposIn(dir)
	sayOK(fmt.Sprintf("Repos folder: %s (%d repos)", tildePath(p), n))
	return nil
}

// runGHLogin runs 'gh auth login' on this terminal; swappable in tests.
var runGHLogin = func() error {
	c := exec.Command("gh", "auth", "login")
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}

// offerGitHub signs the GitHub CLI in when it's installed but signed out:
// that's what puts pull requests on tickets.
func offerGitHub() error {
	if _, err := lookTool("gh"); err != nil {
		return nil
	}
	if _, err := probeTool(10*time.Second, "gh", "auth", "status"); err == nil {
		return nil
	}
	fmt.Println()
	yes := true
	if err := ask(huh.NewConfirm().
		Title("Sign in to GitHub now? (gh auth login)").
		Description("So tickets show their pull requests. GitHub asks a few questions and\nopens your browser; pick HTTPS and 'Login with a web browser'.").
		Affirmative("Yes").Negative("Later").Value(&yes)); err != nil || !yes {
		if err == nil {
			sayInfo("Later: " + cmdHint("gh auth login"))
		}
		return err
	}
	fmt.Println()
	if err := runGHLogin(); err != nil {
		sayWarn("GitHub sign-in didn't finish", "Run it again any time: gh auth login")
		return nil
	}
	if status, err := probeTool(10*time.Second, "gh", "auth", "status"); err == nil {
		who := ""
		if m := reGHAccount.FindStringSubmatch(status); m != nil {
			who = " as " + m[1]
		}
		sayOK("GitHub CLI signed in" + who + " — pull requests show on tickets")
	}
	return nil
}

// offerSkill installs (or refreshes) the /jira skill for Claude Code, if
// the user wants it.
func offerSkill() error {
	dest, err := skillPath()
	if err != nil {
		return nil
	}
	current, _ := os.ReadFile(dest)
	if string(current) == string(skillMD) {
		sayOK("Claude Code knows this CLI " + wzMuted.Render("(/jira skill installed)"))
		return nil
	}
	yes := true
	title := "Teach Claude Code to use this CLI? (/jira skill)"
	if len(current) > 0 {
		title = "Refresh the /jira skill for Claude Code to this version?"
	}
	fmt.Println()
	if err := ask(huh.NewConfirm().Title(title).
		Description("Then ask Claude to find, create or move tickets, or to work on one,\nand it uses this CLI — asking before it changes anything.").
		Affirmative("Yes").Negative("No").Value(&yes)); err != nil || !yes {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(dest, skillMD, 0o644); err != nil {
		return err
	}
	sayOK("Claude Code skill installed " + wzMuted.Render(tildePath(dest)))
	return nil
}

func (w *wizard) done() error {
	fmt.Println()
	fmt.Println("  " + wzOK.Render("●") + " " + wzBold.Render("All set!"))
	fmt.Println()
	for _, r := range [][2]string{
		{"Site", w.p.BrowseBaseURL()},
		{"Signed in as", w.me.DisplayName + wzMuted.Render(" · "+authLabel(w.p))},
		{"Project", w.p.Project},
		{"Profile", w.profile},
	} {
		fmt.Printf("    %s %s\n", wzMuted.Render(fmt.Sprintf("%-13s", r[0])), r[1])
	}
	fmt.Println()
	if w.then != "" {
		fmt.Println(wzMuted.Render("  Now running '" + w.then + "'…"))
		fmt.Println()
		return nil
	}
	fmt.Println("  Try these:")
	tries := [][2]string{
		{"jira ls", "your open issues"},
		{"jira ui", "the interactive view (press ? inside for help)"},
		{"jira doctor", "check that everything works"},
		{"jira --help", "every command"},
	}
	if skillInstalled() {
		tries = append(tries, [2]string{"/jira", "in Claude Code: \"what's on my plate this sprint?\""})
	}
	for _, c := range tries {
		fmt.Printf("    %s %s\n", cmdHint(fmt.Sprintf("%-12s", c[0])), wzMuted.Render(c[1]))
	}
	fmt.Println()
	open := true
	if err := ask(huh.NewConfirm().Title("Open jira ui now?").Affirmative("Yes").Negative("Not now").Value(&open)); err != nil || !open {
		return nil
	}
	cfg, client = w.p, w.client
	return uiCmd.RunE(uiCmd, nil)
}

// ─── non-interactive ─────────────────────────────────────────────

// setupNonInteractive configures an API-token profile from flags, the
// token coming from stdin or $JIRA_TOKEN — for scripts, CI and agents.
func setupNonInteractive(profile string, existing *config.Profile) error {
	token := os.Getenv("JIRA_TOKEN")
	if setupTokenStdin {
		data, err := io.ReadAll(io.LimitReader(os.Stdin, 64<<10))
		if err != nil {
			return fmt.Errorf("reading the token from stdin: %w", err)
		}
		token = strings.Join(strings.Fields(string(data)), "")
	}
	if setupMethod == "oauth" {
		return fmt.Errorf("the browser login needs a terminal — use an API token for scripts")
	}
	var missing []string
	if setupSite == "" {
		missing = append(missing, "--site")
	}
	if setupEmail == "" {
		missing = append(missing, "--email")
	}
	if token == "" {
		missing = append(missing, "--token-stdin (or JIRA_TOKEN)")
	}
	if len(missing) > 0 {
		return fmt.Errorf("no terminal to ask questions in, so these are needed: %s\n"+
			"example: echo \"$TOKEN\" | jira setup --site acme --email me@acme.com --token-stdin --project OPS",
			strings.Join(missing, ", "))
	}
	ref, err := parseSite(setupSite)
	if err != nil {
		return err
	}
	if err := probeSite(ref); err != nil {
		return err
	}
	p := &config.Profile{Name: profile, AuthMethod: "token", Domain: ref.domain, SiteURL: ref.url,
		Email: strings.TrimSpace(setupEmail), Token: token, MaxResults: 20}
	if existing != nil && existing.MaxResults > 0 {
		p.MaxResults = existing.MaxResults
	}
	c := jira.NewBasicClient(apiBaseFor(p), p.Email, p.Token)
	me, err := c.GetMyself()
	if err != nil {
		msg, fix := explainAuthError(err, p.Email)
		if fix != "" {
			msg += "\n" + fix
		}
		return errors.New(msg)
	}
	p.Project = strings.ToUpper(strings.TrimSpace(setupProject))
	if p.Project == "" {
		p.Project = ref.project
	}
	if p.Project == "" && existing != nil {
		p.Project = existing.Project
	}
	if p.Project == "" {
		if projects, err := c.GetProjects(); err == nil && len(projects) > 0 {
			sort.Slice(projects, func(i, j int) bool { return projects[i].Key < projects[j].Key })
			p.Project = projects[0].Key
		}
	}
	if err := config.Save(p); err != nil {
		return err
	}
	if err := config.SetActive(profile); err != nil {
		return err
	}
	fmt.Printf("configured profile %q: %s as %s, project %s\n", profile, ref.url, me.DisplayName, orNone(p.Project))
	return nil
}

// ─── helpers ─────────────────────────────────────────────────────

// apiBaseFor is the REST base URL for a profile; swappable in tests.
var apiBaseFor = func(p *config.Profile) string { return p.APIBaseURL() }

func authLabel(p *config.Profile) string {
	switch {
	case p == nil:
		return ""
	case p.AuthMethod == "oauth":
		return "browser login"
	case p.Email != "":
		return "API token · " + p.Email
	}
	return "API token"
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

func tildePath(p string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, home+"/") {
		return "~" + p[len(home):]
	}
	return p
}

// emailFor guesses the Atlassian email for a site: among the emails git
// knows, the one whose domain looks like the site's name
// (acme.atlassian.net → ana@acme.com), else git's global one.
func emailFor(siteDomain string) string {
	emails := gitEmails()
	name := strings.ToLower(strings.SplitN(siteDomain, ".", 2)[0])
	for _, e := range emails {
		if at := strings.LastIndex(e, "@"); at > 0 && len(name) >= 3 && strings.Contains(strings.ToLower(e[at+1:]), name) {
			return e
		}
	}
	if len(emails) > 0 {
		return emails[0]
	}
	return ""
}

// gitEmails are the emails git knows: the global one, and those of the
// configs it includes for some folders (a work identity next to a personal
// one). Swappable in tests.
var gitEmails = func() []string {
	var out []string
	seen := map[string]bool{}
	add := func(e string) {
		if e = strings.TrimSpace(e); e != "" && !seen[e] {
			seen[e] = true
			out = append(out, e)
		}
	}
	if b, err := exec.Command("git", "config", "--global", "user.email").Output(); err == nil {
		add(string(b))
	}
	b, err := exec.Command("git", "config", "--global", "--get-regexp", `^includeif\..*\.path$`).Output()
	if err != nil {
		return out
	}
	home, _ := os.UserHomeDir()
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		path := f[len(f)-1]
		if strings.HasPrefix(path, "~/") {
			path = filepath.Join(home, path[2:])
		}
		if eb, err := exec.Command("git", "config", "--file", path, "user.email").Output(); err == nil {
			add(string(eb))
		}
	}
	return out
}

// openURL opens a page in the default browser; swappable in tests.
var openURL = func(u string) {
	switch runtime.GOOS {
	case "darwin":
		exec.Command("open", u).Start()
	case "linux":
		exec.Command("xdg-open", u).Start()
	case "windows":
		exec.Command("rundll32", "url.dll,FileProtocolHandler", u).Start()
	}
}

func init() {
	f := setupCmd.Flags()
	f.StringVarP(&setupProfile, "profile", "p", "", "Profile to create or update (default: the active one)")
	f.StringVarP(&setupMethod, "method", "m", "", "Sign-in method: token (recommended) or oauth")
	f.StringVar(&setupSite, "site", "", "Jira site: name, domain or any Jira link")
	f.StringVar(&setupEmail, "email", "", "Atlassian account email (API token sign-in)")
	f.StringVar(&setupProject, "project", "", "Default project key")
	f.BoolVar(&setupTokenStdin, "token-stdin", false, "Read the API token from stdin; never asks questions")
	rootCmd.AddCommand(setupCmd)
}
