package cmd

import (
	"fmt"
	"os"

	"github.com/ngavilan-dogfy/jira-cli/ui"

	"github.com/spf13/cobra"
)

var attachJSON bool

var attachCmd = &cobra.Command{
	Use:   "attach <issue-key> <file> [<file>...]",
	Short: "Upload one or more files as attachments to an issue",
	Long: `Upload files to an issue. Common for incident screenshots, logs, etc.

Output:
  • TTY    → list of uploaded filenames
  • Piped  → tab-separated id\tfilename per attachment
  • --json → array of {id, filename, size, mimeType}

Examples:
  jira attach PROJ-251 /tmp/spike.png
  jira attach PROJ-251 *.log
  jira attach 251 screenshot.png datadog-monitor.png`,
	Args: cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		key := normalizeKey(args[0])
		files := args[1:]

		// Validate files exist before uploading anything
		for _, f := range files {
			if _, err := os.Stat(f); err != nil {
				return fmt.Errorf("file not accessible: %s: %w", f, err)
			}
		}

		atts, err := client.AddAttachment(key, files)
		if err != nil {
			return fmt.Errorf("failed to upload attachments: %w", err)
		}

		if attachJSON {
			return printJSON(atts)
		}

		if !isTTY() {
			for _, a := range atts {
				fmt.Printf("%s\t%s\n", a.ID, a.Filename)
			}
			return nil
		}

		fmt.Println(ui.SuccessStyle.Render(fmt.Sprintf("  OK  Uploaded %d file(s) to %s", len(atts), key)))
		for _, a := range atts {
			fmt.Println(ui.Dimmed.Render(fmt.Sprintf("  · %s (%d bytes)", a.Filename, a.Size)))
		}
		return nil
	},
}

func init() {
	attachCmd.Flags().BoolVar(&attachJSON, "json", false, "Output as JSON")
	rootCmd.AddCommand(attachCmd)
}
