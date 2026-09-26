package ui

import "github.com/charmbracelet/lipgloss"

// NO_COLOR is automatically respected by lipgloss via the colorprofile library.

var (
	Primary   = lipgloss.Color("#7C3AED")
	Secondary = lipgloss.Color("#06B6D4")
	Success   = lipgloss.Color("#10B981")
	Warning   = lipgloss.Color("#F59E0B")
	Danger    = lipgloss.Color("#EF4444")
	Muted     = lipgloss.Color("#6B7280")
	Text      = lipgloss.Color("#E5E7EB")
	Subtle    = lipgloss.Color("#374151")

	Title = lipgloss.NewStyle().
		Bold(true).
		Foreground(Primary).
		MarginBottom(1)

	Subtitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(Secondary)

	Key = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FFFFFF"))

	Label = lipgloss.NewStyle().
		Foreground(Muted).
		Width(12)

	Value = lipgloss.NewStyle().
		Foreground(Text)

	SuccessStyle = lipgloss.NewStyle().
			Foreground(Success).
			Bold(true)

	ErrorStyle = lipgloss.NewStyle().
			Foreground(Danger).
			Bold(true)

	Dimmed = lipgloss.NewStyle().
		Foreground(Muted)

	SectionHeader = lipgloss.NewStyle().
			Bold(true).
			Foreground(Secondary).
			BorderBottom(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(Subtle).
			MarginTop(1).
			MarginBottom(1)
)

func StatusColor(status string) lipgloss.Color {
	switch status {
	case "Done", "Closed", "Resolved":
		return Success
	case "In Progress", "In Development":
		return Warning
	case "In Review", "Code Review", "Review":
		return lipgloss.Color("#3B82F6")
	case "To Do", "Open", "Backlog":
		return Muted
	case "Blocked":
		return Danger
	default:
		return lipgloss.Color("#A78BFA")
	}
}

func PriorityColor(priority string) lipgloss.Color {
	switch priority {
	case "Highest", "Critical", "Blocker":
		return Danger
	case "High":
		return lipgloss.Color("#F97316")
	case "Medium":
		return Warning
	case "Low":
		return Success
	case "Lowest":
		return Muted
	default:
		return Text
	}
}

func StatusBadge(status string) string {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("#000000")).
		Background(StatusColor(status)).
		Bold(true).
		Padding(0, 1).
		Render(status)
}

func PriorityBadge(priority string) string {
	return lipgloss.NewStyle().
		Foreground(PriorityColor(priority)).
		Bold(true).
		Render(priority)
}

func TypeIcon(issueType string) string {
	switch issueType {
	case "Bug":
		return lipgloss.NewStyle().Foreground(Danger).Render("●")
	case "Story":
		return lipgloss.NewStyle().Foreground(Success).Render("◆")
	case "Task":
		return lipgloss.NewStyle().Foreground(Secondary).Render("■")
	case "Sub-task":
		return lipgloss.NewStyle().Foreground(Secondary).Render("▪")
	case "Epic":
		return lipgloss.NewStyle().Foreground(Primary).Render("◎")
	default:
		return lipgloss.NewStyle().Foreground(Muted).Render("○")
	}
}
