package cmd

import (
	"fmt"
	"os/exec"
	"runtime"

	"github.com/ngavilan-dogfy/jira-cli/ui"

	"github.com/spf13/cobra"
)

var openCmd = &cobra.Command{
	Use:   "open <issue-key>",
	Short: "Open issue in browser",
	Long: `Open a Jira issue in your default web browser.

Output:
  • TTY    → success message with URL
  • Piped  → just the URL

Examples:
  jira open PROJ-100                             # open in browser
  jira open 100                                  # shorthand
  jira ls -s "In Progress" --plain | awk '{print $1}' | head -1 | xargs jira open`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		key := normalizeKey(args[0])
		url := client.BrowseURL(key)

		var openBrowser *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			openBrowser = exec.Command("open", url)
		case "linux":
			openBrowser = exec.Command("xdg-open", url)
		default:
			openBrowser = exec.Command("open", url)
		}

		if err := openBrowser.Run(); err != nil {
			return fmt.Errorf("failed to open browser: %w", err)
		}

		if !isTTY() {
			fmt.Println(url)
			return nil
		}

		fmt.Println(ui.SuccessStyle.Render(fmt.Sprintf("  OK  Opened %s", key)))
		fmt.Println(ui.Dimmed.Render("  " + url))
		return nil
	},
}

func init() {
	rootCmd.AddCommand(openCmd)
}
