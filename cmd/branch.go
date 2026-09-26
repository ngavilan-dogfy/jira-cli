package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"unicode"

	"github.com/ngavilan-dogfy/jira-cli/ui"

	"github.com/spf13/cobra"
)

var (
	branchPrefix   string
	branchCheckout bool
	branchPush     bool
	branchPrint    bool
)

var branchCmd = &cobra.Command{
	Use:   "branch [<issue-key>]",
	Short: "Create (or print) a git branch name from an issue",
	Long: `Generate a git branch name from an issue summary and (by default) check it out.

The branch type prefix is inferred from the issue type:
  Task         → feat
  Story        → feat
  Bug          → fix
  Sub-task     → feat
  Spike/Chore  → chore

Override with --type. Use --print to just print the name without creating
the branch (useful for piping into other commands).

Examples:
  jira branch PROJ-251              # checkout feat/PROJ-251-spikes-5xx-...
  jira branch 251 --type hotfix     # hotfix/PROJ-251-...
  jira branch 251 --print           # just echo the name
  jira branch 251 --push            # checkout AND push -u
  jira branch                       # fzf picker`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		key, err := resolveKeyOrPick(args, "")
		if err != nil {
			return err
		}
		issue, err := client.GetIssue(key)
		if err != nil {
			return fmt.Errorf("failed to fetch issue: %w", err)
		}

		prefix := branchPrefix
		if prefix == "" {
			prefix = inferBranchPrefix(issue.Fields.IssueType.Name)
		}

		name := fmt.Sprintf("%s/%s-%s", prefix, key, slugify(issue.Fields.Summary))
		// Trim very long branch names — git supports long names but cap to ~70 chars total.
		if len(name) > 80 {
			name = name[:80]
			name = strings.TrimRight(name, "-")
		}

		if branchPrint {
			fmt.Println(name)
			return nil
		}

		// Verify we're in a git repo
		if _, err := exec.LookPath("git"); err != nil {
			return fmt.Errorf("git not found on PATH")
		}
		if err := runQuiet("git", "rev-parse", "--git-dir"); err != nil {
			return fmt.Errorf("not inside a git repository (cd to your repo first)")
		}

		if branchCheckout {
			gitCmd := exec.Command("git", "checkout", "-b", name)
			gitCmd.Stdout = os.Stdout
			gitCmd.Stderr = os.Stderr
			if err := gitCmd.Run(); err != nil {
				return fmt.Errorf("git checkout -b failed: %w", err)
			}
		}

		if branchPush {
			gitCmd := exec.Command("git", "push", "-u", "origin", name)
			gitCmd.Stdout = os.Stdout
			gitCmd.Stderr = os.Stderr
			if err := gitCmd.Run(); err != nil {
				return fmt.Errorf("git push failed: %w", err)
			}
		}

		if !isTTY() {
			fmt.Println(name)
			return nil
		}
		fmt.Println(ui.SuccessStyle.Render(fmt.Sprintf("  OK  branch %s", name)))
		fmt.Println(ui.Dimmed.Render(fmt.Sprintf("  %s · %s", key, issue.Fields.Summary)))
		return nil
	},
}

func inferBranchPrefix(issueType string) string {
	switch strings.ToLower(issueType) {
	case "bug":
		return "fix"
	case "spike", "chore", "task chore":
		return "chore"
	case "epic":
		return "epic"
	default:
		return "feat"
	}
}

// slugify converts a free-form string to a lowercase, hyphenated slug.
var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// asciiFold maps the accented letters common in Spanish/Latin summaries to
// their base letter, so "migración" slugs to "migracion", not "migraci-n".
func asciiFold(r rune) rune {
	switch r {
	case 'á', 'à', 'ä', 'â', 'ã':
		return 'a'
	case 'é', 'è', 'ë', 'ê':
		return 'e'
	case 'í', 'ì', 'ï', 'î':
		return 'i'
	case 'ó', 'ò', 'ö', 'ô', 'õ':
		return 'o'
	case 'ú', 'ù', 'ü', 'û':
		return 'u'
	case 'ñ':
		return 'n'
	case 'ç':
		return 'c'
	}
	return r
}

func slugify(s string) string {
	// Lowercase, strip diacritics best-effort, replace non-alphanum with -.
	var out strings.Builder
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			out.WriteRune(asciiFold(unicode.ToLower(r)))
		case unicode.IsSpace(r):
			out.WriteRune('-')
		default:
			// drop punctuation; let the regex collapse separators
			out.WriteRune('-')
		}
	}
	slug := slugRe.ReplaceAllString(out.String(), "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		slug = "task"
	}
	// Cap slug length to ~50 chars, cutting at a word boundary.
	if len(slug) > 50 {
		cut := slug[:50]
		if i := strings.LastIndexByte(cut, '-'); i > 20 {
			cut = cut[:i]
		}
		slug = strings.TrimRight(cut, "-")
	}
	return slug
}

func runQuiet(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run()
}

func init() {
	branchCmd.Flags().StringVar(&branchPrefix, "type", "", "Branch type prefix (overrides inferred prefix: feat/fix/chore/hotfix/...)")
	branchCmd.Flags().BoolVar(&branchCheckout, "checkout", true, "Run git checkout -b (use --checkout=false to skip)")
	branchCmd.Flags().BoolVar(&branchPush, "push", false, "Push -u to origin after checkout")
	branchCmd.Flags().BoolVar(&branchPrint, "print", false, "Print the branch name and exit without touching git")
	rootCmd.AddCommand(branchCmd)
}
