package cmd

import (
	"fmt"
	"strings"

	"github.com/ngavilan-dogfy/jira-cli/jira"
	"github.com/ngavilan-dogfy/jira-cli/ui"

	"github.com/spf13/cobra"
)

var (
	historyJSON  bool
	historyLimit int
)

var historyCmd = &cobra.Command{
	Use:   "history <issue-key>",
	Short: "Show change history of an issue",
	Long: `Show the changelog of an issue: who changed which field, when, from what value to what.

Output:
  • TTY    → human-friendly list with field/old/new
  • Piped  → TSV (timestamp\tauthor\tfield\tfrom\tto)
  • --json → array of {created, author, items[]}

Examples:
  jira history PROJ-251
  jira history PROJ-251 --json
  jira history PROJ-251 --limit 10`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		key := normalizeKey(args[0])
		entries, err := client.GetChangelog(key, historyLimit)
		if err != nil {
			return fmt.Errorf("failed to fetch changelog: %w", err)
		}
		if historyJSON {
			return printJSON(entries)
		}
		if len(entries) == 0 {
			if isTTY() {
				fmt.Println(ui.Dimmed.Render("  No history yet."))
			}
			return nil
		}
		if !isTTY() {
			fmt.Println("TIMESTAMP\tAUTHOR\tFIELD\tFROM\tTO")
			for _, e := range entries {
				for _, it := range e.Items {
					fmt.Printf("%s\t%s\t%s\t%s\t%s\n",
						e.Created, e.Author.DisplayName, it.Field, it.FromString, it.ToString)
				}
			}
			return nil
		}
		fmt.Println(ui.Title.Render(fmt.Sprintf(" %s · history (%d events)", key, len(entries))))
		for _, e := range entries {
			fmt.Println()
			fmt.Println(ui.Dimmed.Render(fmt.Sprintf("  %s · %s", jira.RelativeTime(e.Created), e.Author.DisplayName)))
			for _, it := range e.Items {
				from := strings.TrimSpace(it.FromString)
				to := strings.TrimSpace(it.ToString)
				if from == "" {
					from = "∅"
				}
				if to == "" {
					to = "∅"
				}
				fmt.Printf("    %s: %s → %s\n", it.Field, from, to)
			}
		}
		return nil
	},
}

func init() {
	historyCmd.Flags().BoolVar(&historyJSON, "json", false, "Output as JSON")
	historyCmd.Flags().IntVarP(&historyLimit, "limit", "n", 50, "Max number of changelog entries")
	rootCmd.AddCommand(historyCmd)
}
