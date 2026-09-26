//go:build e2e

package cmd

import (
	"os"

	"github.com/ngavilan-dogfy/jira-cli/config"
	"github.com/ngavilan-dogfy/jira-cli/internal/selfupdate"
)

// Hooks for driving the real binary end to end (go build -tags e2e): the
// site probe and the API go to $JIRA_E2E_BASE (a fake Jira), URLs are
// logged to $JIRA_E2E_LOG instead of opening a browser, and 'jira update'
// asks $JIRA_E2E_RELEASES (a fake GitHub: API and web) for releases. Never
// in releases.
func init() {
	if u := os.Getenv("JIRA_E2E_RELEASES"); u != "" {
		selfupdate.APIBase, selfupdate.WebBase = u, u
	}
	base := os.Getenv("JIRA_E2E_BASE")
	if base == "" {
		return
	}
	hostname = func() (string, error) { return "ana-laptop", nil }
	siteProbeURL = func(siteRef) string { return base + "/rest/api/3/serverInfo" }
	apiBaseFor = func(*config.Profile) string { return base }
	openURL = func(u string) {
		if f, err := os.OpenFile(os.Getenv("JIRA_E2E_LOG"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
			f.WriteString("open " + u + "\n")
			f.Close()
		}
	}
}
