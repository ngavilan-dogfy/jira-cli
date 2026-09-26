package cmd

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ngavilan-dogfy/jira-cli/config"
	"github.com/ngavilan-dogfy/jira-cli/internal/selfupdate"
	"github.com/ngavilan-dogfy/jira-cli/jira"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	cfg    *config.Profile
	client *jira.Client
)

var rootCmd = &cobra.Command{
	Use:     "jira",
	Short:   "Fast CLI for managing Jira tickets",
	Version: Version,
	// Runtime errors (API failures, bad keys...) shouldn't dump the full
	// usage text — it buries the actual error for both humans and scripts.
	SilenceUsage: true,
	Long: `Jira Cloud from your terminal: a fast UI for people, and commands that
speak JSON for scripts and AI agents.

Get started:
  jira setup                    Connect to your Jira site, step by step
  jira ui                       Your tickets, the board, and the work on each
  jira ui --demo                Look around a made-up team's project first

Find things:
  jira ls                       Your issues (-a everyone's, -s status, -t type,
                                -q text, --label, --since, --parent, --jql)
  jira show PROJ-100            One issue (--json: every field and comment)
  jira context PROJ-100         Everything about an issue in one call, as
                                markdown for LLMs (or --json)
  jira export --jql '...'       Many issues at once, as JSONL or markdown
  jira epics · users · statuses · fields · projects · transitions PROJ-100

Change things:
  jira create "Summary"         New issue (-t Bug, --parent PROJ-10, -d text)
  jira edit PROJ-100            Fields (--summary, --description, --priority,
                                --labels, --field customfield_10016=5)
  jira move PROJ-100 "Done"     Change the status
  jira assign PROJ-100 --me     Assign it (jira unassign PROJ-100 clears it)
  jira comment PROJ-100 "…"     Comment in markdown (or from stdin)
  jira link PROJ-100 PROJ-200   Link two issues (--type Blocks)
  printf '100\n101\n' | jira batch move "In Progress"

Every command adapts to where its output goes: colors in a terminal, TSV
when piped, --json on most commands. Keys can be short: "100" means
PROJ-100 with the project from your profile ('jira config').

Keep it working:
  jira doctor                   Check the site, your session and tools, with fixes
  jira update                   Update to the latest release
  jira skill install            Teach Claude Code this CLI (/jira)`,
	// Bare 'jira': help, or a welcome that offers setup on a fresh install.
	RunE: func(cmd *cobra.Command, args []string) error {
		if cfg != nil && cfg.IsAuthenticated() {
			return cmd.Help()
		}
		return welcome(cmd)
	},
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		cfg, _ = config.LoadActive()
		if cfg != nil && cfg.IsAuthenticated() {
			client = buildClient(cfg)
			return nil
		}
		if !needsAuth(cmd) {
			return nil
		}
		// Not set up yet: in a terminal, offer the guided setup and then run
		// the command that was asked for; elsewhere, say exactly what's missing.
		if !interactive() {
			return notSetUpError()
		}
		if err := offerSetup(cmd.CommandPath()); err != nil {
			return err
		}
		cfg, _ = config.LoadActive()
		if cfg == nil || !cfg.IsAuthenticated() {
			return notSetUpError()
		}
		client = buildClient(cfg)
		return nil
	},
}

// needsAuth reports whether a command talks to Jira.
func needsAuth(cmd *cobra.Command) bool {
	if !cmd.HasParent() {
		return false // bare 'jira' shows help or the welcome
	}
	if cmd == uiCmd && uiDemo {
		return false
	}
	path := cmd.CommandPath()
	for _, prefix := range []string{
		"jira setup", "jira logout", "jira doctor", "jira version", "jira update",
		"jira skill", "jira profile", "jira config", "jira help", "jira completion",
	} {
		if path == prefix || strings.HasPrefix(path, prefix+" ") {
			return false
		}
	}
	return true
}

// buildClient makes the API client for a profile. OAuth tokens refresh under
// a cross-process lock (several agents may run jira at once) and are saved
// onto the latest copy of the profile on disk.
func buildClient(p *config.Profile) *jira.Client {
	if p.AuthMethod != "oauth" {
		return jira.NewBasicClient(apiBaseFor(p), p.Email, p.Token)
	}
	expiry, _ := time.Parse(time.RFC3339, p.TokenExpiry)
	name := p.Name
	c := jira.NewOAuthClient(p.CloudID, p.BrowseBaseURL(), p.AccessToken, p.RefreshToken, p.ClientID,
		config.GlobalClientSecret(), expiry,
		func(at, rt string, exp time.Time) {
			p.AccessToken, p.RefreshToken, p.TokenExpiry = at, rt, exp.Format(time.RFC3339)
			// Another process may have changed other settings meanwhile.
			disk, err := config.Load(name)
			if err != nil {
				disk = p
			}
			disk.AccessToken, disk.RefreshToken, disk.TokenExpiry = at, rt, p.TokenExpiry
			config.Save(disk)
		},
	)
	// Reuse tokens another process just got: Atlassian refresh tokens rotate,
	// and refreshing with a stale one ends the session.
	c.SetTokenSync(&jira.TokenSync{
		Lock: func() (func(), error) { return config.LockProfile(name, 15*time.Second) },
		Reload: func() (string, string, time.Time, error) {
			disk, err := config.Load(name)
			if err != nil {
				return "", "", time.Time{}, err
			}
			exp, _ := time.Parse(time.RFC3339, disk.TokenExpiry)
			return disk.AccessToken, disk.RefreshToken, exp, nil
		},
	})
	return c
}

// notSetUpError says what's missing, for scripts and agents that can't
// answer questions.
func notSetUpError() error {
	var set, unset []string
	for _, v := range []string{"JIRA_DOMAIN", "JIRA_EMAIL", "JIRA_TOKEN"} {
		if os.Getenv(v) != "" {
			set = append(set, v)
		} else {
			unset = append(unset, v)
		}
	}
	if len(set) > 0 && len(unset) > 0 {
		return fmt.Errorf("%s set but %s missing — all three of JIRA_DOMAIN, JIRA_EMAIL and JIRA_TOKEN are needed",
			strings.Join(set, ", "), strings.Join(unset, ", "))
	}
	if cfg != nil {
		return fmt.Errorf("profile %q isn't signed in — run 'jira setup'", cfg.Name)
	}
	return fmt.Errorf("jira isn't connected to Jira yet — run 'jira setup' in a terminal, " +
		"or set JIRA_DOMAIN, JIRA_EMAIL and JIRA_TOKEN (see 'jira setup --help')")
}

// quietError fails the command (exit 1) without printing it again: the
// command already explained the problem.
type quietError struct{ error }

func Execute() {
	rootCmd.SilenceErrors = true
	notifier := startNotifier()
	err := rootCmd.Execute()
	if err != nil {
		var quiet quietError
		if !errors.As(err, &quiet) {
			fmt.Fprintln(os.Stderr, "Error:", err)
		}
	}
	notifier.Print(os.Stderr)
	if err != nil {
		// `jira wait` returns errTimeout to signal a distinct exit code so
		// scripts can tell "no/match within timeout" apart from real failures.
		if err == errTimeout {
			os.Exit(2)
		}
		os.Exit(1)
	}
}

// startNotifier checks for a newer release in the background (cached, at
// most daily) when a person is at a terminal; the notice prints after the
// command so it never gets in the way.
func startNotifier() *selfupdate.Notifier {
	if !isTTY() || !term.IsTerminal(int(os.Stderr.Fd())) || wantsJSON() {
		return nil
	}
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "update", "upgrade", "version", "completion", "__complete", "__completeNoDesc", "ui":
			return nil
		}
	}
	return selfupdate.StartNotifier(updateTool(), cacheDir())
}

// wantsJSON reports whether --json appeared anywhere on the command line.
func wantsJSON() bool {
	for _, a := range os.Args[1:] {
		if a == "--json" {
			return true
		}
	}
	return false
}

func normalizeKey(key string) string {
	key = strings.ToUpper(strings.TrimSpace(key))
	if _, err := strconv.Atoi(key); err == nil {
		if cfg != nil && cfg.Project != "" {
			return cfg.Project + "-" + key
		}
		return key
	}
	return key
}
