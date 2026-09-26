package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/ngavilan-dogfy/jira-cli/ui"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var moveJSON bool

var moveCmd = &cobra.Command{
	Use:     "move <issue-key> [status]",
	Aliases: []string{"transition", "mv"},
	Short:   "Transition an issue to a new status",
	Long: `Move an issue to a new status. If status is provided, the transition
happens non-interactively (great for scripting). Otherwise, you'll be
prompted to select from available transitions.

Matches by transition name OR target status name (case-insensitive).
Use 'jira transitions <key>' to discover available transitions.

Output:
  • TTY    → success message
  • Piped  → issue key
  • --json → {"key":"PROJ-100","status":"In Progress"}

Examples:
  jira move PROJ-100                             # interactive
  jira move PROJ-100 "In Progress"               # non-interactive
  jira move PROJ-100 Done                        # non-interactive
  jira move 100 Done --json                      # JSON output
  jira move PROJ-100 "In Progress" --json | jq -r '.status'`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		key := normalizeKey(args[0])

		transitions, err := client.GetTransitions(key)
		if err != nil {
			return fmt.Errorf("failed to get transitions: %w", err)
		}

		if len(transitions) == 0 {
			return fmt.Errorf("no transitions available for %s", key)
		}

		// Non-interactive: match target status by name
		if len(args) == 2 {
			target := strings.ToLower(strings.TrimSpace(args[1]))
			for _, t := range transitions {
				if strings.ToLower(t.Name) == target || strings.ToLower(t.To.Name) == target {
					if err := client.DoTransition(key, t.ID); err != nil {
						return fmt.Errorf("failed to transition: %w", err)
					}
					if moveJSON {
						return printJSON(map[string]string{
							"key":    key,
							"status": t.To.Name,
						})
					}
					if !isTTY() {
						fmt.Println(key)
						return nil
					}
					fmt.Println(ui.SuccessStyle.Render(fmt.Sprintf("  OK  %s → %s", key, t.To.Name)))
					return nil
				}
			}
			// Idempotent: if already in the target status, succeed silently
			issue, err := client.GetIssue(key)
			if err == nil && strings.ToLower(issue.Fields.Status.Name) == target {
				if moveJSON {
					return printJSON(map[string]string{
						"key":    key,
						"status": issue.Fields.Status.Name,
					})
				}
				if !isTTY() {
					fmt.Println(key)
					return nil
				}
				fmt.Println(ui.Dimmed.Render(fmt.Sprintf("  %s already in %s", key, issue.Fields.Status.Name)))
				return nil
			}
			available := make([]string, len(transitions))
			for i, t := range transitions {
				available[i] = t.To.Name
			}
			return fmt.Errorf("no matching transition %q — available: %s", args[1], strings.Join(available, ", "))
		}

		// Interactive mode
		issue, err := client.GetIssue(key)
		if err != nil {
			return fmt.Errorf("failed to get issue: %w", err)
		}

		fmt.Printf("  %s  %s\n", ui.Key.Render(key), issue.Fields.Summary)
		fmt.Printf("  Current: %s\n\n", ui.StatusBadge(issue.Fields.Status.Name))

		fmt.Println(ui.Subtitle.Render("  Move to:"))
		for i, t := range transitions {
			num := lipgloss.NewStyle().
				Bold(true).
				Foreground(ui.Secondary).
				Width(4).
				Align(lipgloss.Right).
				Render(fmt.Sprintf("%d.", i+1))

			name := lipgloss.NewStyle().
				Foreground(ui.StatusColor(t.To.Name)).
				Bold(true).
				Render(t.Name)

			fmt.Printf("  %s %s\n", num, name)
		}

		fmt.Println()
		fmt.Print(ui.Dimmed.Render("  Select (q to cancel): "))

		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		if input == "q" || input == "" {
			fmt.Println(ui.Dimmed.Render("  Cancelled."))
			return nil
		}

		idx, err := strconv.Atoi(input)
		if err != nil || idx < 1 || idx > len(transitions) {
			return fmt.Errorf("invalid selection: %s", input)
		}

		selected := transitions[idx-1]

		if err := client.DoTransition(key, selected.ID); err != nil {
			return fmt.Errorf("failed to transition: %w", err)
		}

		fmt.Println(ui.SuccessStyle.Render(fmt.Sprintf("  OK  %s → %s", key, selected.Name)))
		return nil
	},
}

func init() {
	moveCmd.Flags().BoolVar(&moveJSON, "json", false, "Output as JSON")
	rootCmd.AddCommand(moveCmd)
}
