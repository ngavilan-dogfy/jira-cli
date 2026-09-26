package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/ngavilan-dogfy/jira-cli/ui"

	"github.com/spf13/cobra"
)

var (
	waitStatuses []string
	waitTimeout  time.Duration
	waitPoll     time.Duration
	waitJSON     bool
)

var waitCmd = &cobra.Command{
	Use:           "wait <issue-key>",
	Short:         "Block until an issue reaches a target status",
	SilenceUsage:  true,
	SilenceErrors: true,
	Long: `Poll the issue and exit when its status matches any of --status.
Useful in CI / shell scripts where a step depends on a manual transition
(e.g. wait for a ticket to be approved before deploying).

Exit codes:
  0  status matched within timeout
  1  any other error
  2  timeout (status not reached)

Examples:
  jira wait PROJ-251 --status Done
  jira wait PROJ-251 --status "In Review,Done" --timeout 30m
  jira wait PROJ-251 --status Done --poll 30s --timeout 1h`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		key := normalizeKey(args[0])
		if len(waitStatuses) == 0 {
			return fmt.Errorf("--status is required (one or more status names)")
		}

		// Allow comma-separated list on a single --status flag
		var targets []string
		for _, s := range waitStatuses {
			for _, p := range strings.Split(s, ",") {
				p = strings.TrimSpace(p)
				if p != "" {
					targets = append(targets, p)
				}
			}
		}

		started := time.Now()
		if isTTY() && !waitJSON {
			fmt.Println(ui.Dimmed.Render(fmt.Sprintf("  waiting %s for %s to reach: %s", waitTimeout, key, strings.Join(targets, ", "))))
		}

		final, err := client.PollStatus(key, targets, waitTimeout, waitPoll)
		elapsed := time.Since(started).Round(time.Second)

		if err != nil {
			// Timeout — exit code 2 via cobra returning a sentinel
			if waitJSON {
				_ = printJSON(map[string]interface{}{
					"key":     key,
					"matched": false,
					"status":  final,
					"elapsed": elapsed.String(),
					"error":   err.Error(),
				})
			} else if isTTY() {
				fmt.Println(ui.ErrorStyle.Render(fmt.Sprintf("  TIMEOUT  %s is %q after %s", key, final, elapsed)))
			} else {
				fmt.Fprintln(cmd.ErrOrStderr(), err)
			}
			return errTimeout
		}

		if waitJSON {
			return printJSON(map[string]interface{}{
				"key":     key,
				"matched": true,
				"status":  final,
				"elapsed": elapsed.String(),
			})
		}
		if !isTTY() {
			fmt.Println(key)
			return nil
		}
		fmt.Println(ui.SuccessStyle.Render(fmt.Sprintf("  OK  %s reached %q in %s", key, final, elapsed)))
		return nil
	},
}

// errTimeout is recognized in main to set exit code 2.
var errTimeout = fmt.Errorf("timeout")

func init() {
	waitCmd.Flags().StringSliceVarP(&waitStatuses, "status", "s", nil, "Target status name(s). Repeat or comma-separate for multiple.")
	waitCmd.Flags().DurationVar(&waitTimeout, "timeout", 10*time.Minute, "Maximum time to wait")
	waitCmd.Flags().DurationVar(&waitPoll, "poll", 10*time.Second, "Poll interval")
	waitCmd.Flags().BoolVar(&waitJSON, "json", false, "Output as JSON")
	rootCmd.AddCommand(waitCmd)
}
