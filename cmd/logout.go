package cmd

import (
	"fmt"

	"github.com/ngavilan-dogfy/jira-cli/config"
	"github.com/ngavilan-dogfy/jira-cli/ui"

	"github.com/spf13/cobra"
)

var logoutProfile string

var logoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Clear credentials from profile",
	RunE: func(cmd *cobra.Command, args []string) error {
		name := logoutProfile
		if name == "" {
			name = config.ActiveName()
		}

		p, err := config.Load(name)
		if err != nil {
			return err
		}

		p.Token = ""
		p.AccessToken = ""
		p.RefreshToken = ""
		p.TokenExpiry = ""

		if err := config.Save(p); err != nil {
			return fmt.Errorf("failed to save: %w", err)
		}

		fmt.Println(ui.SuccessStyle.Render("  Logged out"))
		fmt.Println(ui.Dimmed.Render("  Credentials cleared from profile: " + name))

		return nil
	},
}

func init() {
	logoutCmd.Flags().StringVarP(&logoutProfile, "profile", "p", "", "Target profile (default: active)")
	rootCmd.AddCommand(logoutCmd)
}
