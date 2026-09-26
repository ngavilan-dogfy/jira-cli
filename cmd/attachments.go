package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ngavilan-dogfy/jira-cli/ui"

	"github.com/spf13/cobra"
)

var (
	attachmentsJSON     bool
	attachmentsDownload bool
	attachmentsOut      string
)

var attachmentsCmd = &cobra.Command{
	Use:   "attachments <issue-key>",
	Short: "List or download an issue's attachments",
	Long: `List the attachments of an issue, or download them to disk with
--download (so logs, screenshots and files attached to a ticket can be
inspected locally — e.g. by an AI agent).

Output:
  • TTY    → list of attachments
  • Piped  → TSV: ID<tab>FILENAME<tab>SIZE<tab>MIMETYPE
  • --json → array of {id, filename, size, mimeType}

Examples:
  jira attachments PROJ-100                     # list
  jira attachments PROJ-100 --json              # structured list
  jira attachments PROJ-100 --download          # download all to cwd
  jira attachments PROJ-100 --download -o /tmp  # download to a directory`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		key := normalizeKey(args[0])

		issue, err := client.GetIssue(key)
		if err != nil {
			return fmt.Errorf("failed to get issue: %w", err)
		}
		atts := issue.Fields.Attachments

		if attachmentsDownload {
			if len(atts) == 0 {
				return fmt.Errorf("%s has no attachments", key)
			}
			dir := attachmentsOut
			if dir == "" {
				dir = "."
			}
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
			for _, a := range atts {
				dest := filepath.Join(dir, a.Filename)
				// Avoid clobbering distinct files that share a name.
				if _, err := os.Stat(dest); err == nil {
					dest = filepath.Join(dir, a.ID+"_"+a.Filename)
				}
				if err := client.DownloadAttachment(a.ID, dest); err != nil {
					return fmt.Errorf("failed to download %s: %w", a.Filename, err)
				}
				fmt.Println(dest)
			}
			if isTTY() {
				fmt.Fprintln(os.Stderr, ui.SuccessStyle.Render(fmt.Sprintf("  OK  %d file(s) downloaded", len(atts))))
			}
			return nil
		}

		if attachmentsJSON {
			out := make([]attachmentJSON, 0, len(atts))
			for _, a := range atts {
				out = append(out, attachmentJSON{Filename: a.Filename, Size: a.Size, MimeType: a.MimeType})
			}
			return printJSON(out)
		}

		if len(atts) == 0 {
			if isTTY() {
				fmt.Println(ui.Dimmed.Render("  No attachments."))
			}
			return nil
		}

		if !isTTY() {
			for _, a := range atts {
				fmt.Printf("%s\t%s\t%d\t%s\n", a.ID, a.Filename, a.Size, a.MimeType)
			}
			return nil
		}

		fmt.Println(ui.Title.Render(fmt.Sprintf(" %s · attachments (%d)", key, len(atts))))
		for _, a := range atts {
			fmt.Printf("  · %s (%s, %d bytes)\n", a.Filename, a.MimeType, a.Size)
		}
		return nil
	},
}

func init() {
	attachmentsCmd.Flags().BoolVar(&attachmentsJSON, "json", false, "Output as JSON")
	attachmentsCmd.Flags().BoolVar(&attachmentsDownload, "download", false, "Download all attachments")
	attachmentsCmd.Flags().StringVarP(&attachmentsOut, "out", "o", "", "Directory to download into (default: current dir)")
	rootCmd.AddCommand(attachmentsCmd)
}
