package tui

import (
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// editorCommand resolves the user's editor: $VISUAL, $EDITOR, then the
// first of nvim/vim/nano found on PATH.
func editorCommand() []string {
	for _, env := range []string{"VISUAL", "EDITOR"} {
		if v := strings.Fields(os.Getenv(env)); len(v) > 0 {
			return v
		}
	}
	for _, e := range []string{"nvim", "vim", "nano", "vi"} {
		if _, err := exec.LookPath(e); err == nil {
			return []string{e}
		}
	}
	return []string{"vi"}
}

// openEditor suspends the TUI, edits text in $EDITOR (as markdown) and
// reports back with an editorDoneMsg tagged with id. name ends up in the
// temp file's name, so the editor's title says what is being edited.
// Swappable in tests.
var openEditor = func(id, name, text string) tea.Cmd {
	f, err := os.CreateTemp("", "jira-"+name+"-*.md")
	if err != nil {
		return func() tea.Msg { return editorDoneMsg{id: id, err: err} }
	}
	path := f.Name()
	_, _ = f.WriteString(text)
	_ = f.Close()

	argv := append(editorCommand(), path)
	c := exec.Command(argv[0], argv[1:]...)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		defer os.Remove(path)
		if err != nil {
			return editorDoneMsg{id: id, err: err}
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return editorDoneMsg{id: id, err: rerr}
		}
		return editorDoneMsg{id: id, text: strings.TrimRight(string(data), "\n")}
	})
}
