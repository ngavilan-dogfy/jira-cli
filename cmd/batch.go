package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/ngavilan-dogfy/jira-cli/ui"

	"github.com/spf13/cobra"
)

var batchCmd = &cobra.Command{
	Use:   "batch <action> [args...]",
	Short: "Run an action on multiple issues from stdin",
	Long: `Read issue keys from stdin (one per line) and apply an action to each.

Actions:
  move <status>      Transition all issues to a status
  assign me          Assign all to yourself
  assign <id>        Assign all to account ID
  unassign           Unassign all
  label-add <l>      Add label(s) to all
  label-rm <l>       Remove label(s) from all
  comment <msg>      Add same comment to all

Reads keys from stdin — one key per line, ignores empty lines and lines
starting with #. Keys are auto-normalized (e.g. "100" → "PROJ-100").

Output:
  Prints "OK <key>" or "ERR <key>: <reason>" for each issue.
  Exit code is non-zero if any operation failed.

Examples:
  jira ls -s "In Refinement" --plain | awk '{print $1}' | jira batch move "In Progress"
  echo -e "PROJ-100\nPROJ-101\nPROJ-102" | jira batch assign me
  jira ls -a --plain | awk '$3=="Done" {print $1}' | jira batch label-add archived
  cat keys.txt | jira batch comment "Sprint review done"`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		action := args[0]

		scanner := bufio.NewScanner(os.Stdin)
		var keys []string
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			// Take first field (support TSV input where key is first column)
			fields := strings.Fields(line)
			if len(fields) > 0 {
				keys = append(keys, normalizeKey(fields[0]))
			}
		}

		if len(keys) == 0 {
			return fmt.Errorf("no issue keys received from stdin")
		}

		var errCount int

		for _, key := range keys {
			var err error

			switch action {
			case "move":
				if len(args) < 2 {
					return fmt.Errorf("usage: jira batch move <status>")
				}
				target := strings.Join(args[1:], " ")
				err = batchMove(key, target)

			case "assign":
				if len(args) >= 2 && (args[1] == "me" || args[1] == "--me") {
					err = batchAssignMe(key)
				} else if len(args) >= 2 {
					err = client.AssignIssue(key, args[1])
				} else {
					return fmt.Errorf("usage: jira batch assign --me OR jira batch assign <account-id>")
				}

			case "unassign":
				err = client.AssignIssue(key, "")

			case "label-add":
				if len(args) < 2 {
					return fmt.Errorf("usage: jira batch label-add <label>...")
				}
				err = batchLabelAdd(key, args[1:])

			case "label-rm":
				if len(args) < 2 {
					return fmt.Errorf("usage: jira batch label-rm <label>...")
				}
				err = batchLabelRemove(key, args[1:])

			case "comment":
				if len(args) < 2 {
					return fmt.Errorf("usage: jira batch comment <message>")
				}
				msg := strings.Join(args[1:], " ")
				err = client.AddComment(key, msg)

			default:
				return fmt.Errorf("unknown action %q — supported: move, assign, unassign, label-add, label-rm, comment", action)
			}

			if err != nil {
				errCount++
				fmt.Fprintf(os.Stderr, "ERR %s: %s\n", key, err)
			} else {
				fmt.Printf("OK %s\n", key)
			}
		}

		if errCount > 0 {
			return fmt.Errorf("%d of %d operations failed", errCount, len(keys))
		}

		if isTTY() {
			fmt.Fprintln(os.Stderr, ui.SuccessStyle.Render(fmt.Sprintf("  OK  %d issues processed", len(keys))))
		}
		return nil
	},
}

var batchMyself string

func batchAssignMe(key string) error {
	if batchMyself == "" {
		myself, err := client.GetMyself()
		if err != nil {
			return err
		}
		batchMyself = myself.AccountID
	}
	return client.AssignIssue(key, batchMyself)
}

func batchMove(key, target string) error {
	transitions, err := client.GetTransitions(key)
	if err != nil {
		return err
	}
	target = strings.ToLower(strings.TrimSpace(target))
	for _, t := range transitions {
		if strings.ToLower(t.Name) == target || strings.ToLower(t.To.Name) == target {
			return client.DoTransition(key, t.ID)
		}
	}
	return fmt.Errorf("no matching transition %q", target)
}

func batchLabelAdd(key string, labels []string) error {
	issue, err := client.GetIssue(key)
	if err != nil {
		return err
	}
	merged := issue.Fields.Labels
	for _, l := range labels {
		if !contains(merged, l) {
			merged = append(merged, l)
		}
	}
	return client.EditIssue(key, map[string]interface{}{"labels": merged})
}

func batchLabelRemove(key string, labels []string) error {
	issue, err := client.GetIssue(key)
	if err != nil {
		return err
	}
	var remaining []string
	for _, l := range issue.Fields.Labels {
		if !contains(labels, l) {
			remaining = append(remaining, l)
		}
	}
	return client.EditIssue(key, map[string]interface{}{"labels": remaining})
}

func init() {
	rootCmd.AddCommand(batchCmd)
}
