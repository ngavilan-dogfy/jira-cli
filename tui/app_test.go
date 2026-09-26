package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ngavilan-dogfy/jira-cli/jira"

	tea "github.com/charmbracelet/bubbletea"
)

func TestBrowseGroupsByStatusAndNestsSubtasks(t *testing.T) {
	h := newHarness(t, 120, 30)
	h.expect("TST", "Mine 7", "In Progress", "In Refinement", "Backlog", "Done",
		"◆ TST-1", "└ ■ TST-2", "└ ▪ TST-3", // epic → task → sub-task
		"↑TST-2", // TST-4 is in another group than its parent: tagged instead
	)
	// Done starts folded; other people's issues aren't in "Mine".
	h.reject("Update docs", "Someone else's task")
	if got := h.selected(); got != "TST-1" {
		t.Fatalf("cursor should start on the first issue, got %q", got)
	}
	// In-progress work comes first, then what's next, then the backlog.
	s := h.screen()
	if !(strings.Index(s, "In Progress") < strings.Index(s, "In Refinement") &&
		strings.Index(s, "In Refinement") < strings.Index(s, "Backlog")) {
		t.Fatalf("unexpected group order:\n%s", s)
	}
	// The preview shows the selected issue's description.
	h.expect("Description of Platform migration")
	h.keys("j")
	h.expect("Description of Migrate api to Cloud Run", "↑ High")
}

func TestLayoutInvariantsAtManySizes(t *testing.T) {
	for _, sz := range [][2]int{{50, 12}, {64, 20}, {80, 24}, {100, 30}, {140, 37}, {220, 60}} {
		h := newHarness(t, sz[0], sz[1])
		h.screen()
		h.keys("j<enter>") // detail
		h.screen()
		for i := 0; i < 6; i++ {
			h.keys("<tab>")
			h.screen()
		}
		h.keys("<esc>b") // board
		h.screen()
		h.keys("L")
		h.screen()
		h.keys(":")
		h.screen()
		h.keys("<esc>?")
		h.screen()
		h.keys("<esc>b/ta")
		h.screen()
		h.keys("<esc>n")
		h.screen()
		h.keys("<esc>")
	}
}

func TestTooSmall(t *testing.T) {
	h := newHarness(t, 40, 10)
	h.expect("Terminal too small")
}

func TestFilterIsAccentInsensitiveAndOpensSingleMatch(t *testing.T) {
	h := newHarness(t, 120, 30)
	h.keys("/safari")
	h.expect("TST-5", "1 of 7")
	h.reject("TST-1 ")
	h.keys("<enter>") // one match: open it
	h.expect("Login redirect loop on Safari", "Description")
	h.keys("<esc>")
	h.expect("TST-5") // filter kept after coming back
	h.keys("<esc>")   // clears it
	h.expect("TST-1")

	// Accents: "migracion" finds "Migración" (Team tab includes unassigned).
	h.keys("3")
	h.expect("Team 9")
	h.keys("/migracion")
	h.expect("TST-9", "1 of 9")
}

func TestMoveThroughTransitionPicker(t *testing.T) {
	h := newHarness(t, 120, 30)
	h.keys("m")
	h.expect("Move TST-1", "Backlog", "In Refinement", "Done", "current")
	h.keys("4") // Backlog, Refinement, In Progress (current), Done
	h.expectWrite("transition TST-1 Done")
	h.expect("TST-1 → Done", "In Progress  2", "Done  2")
}

func TestComment(t *testing.T) {
	h := newHarness(t, 120, 30)
	h.keys("jc")
	h.expect("Comment on TST-2", "Migrate api to Cloud Run")
	h.keys("hola **mundo**<ctrl+s>")
	h.expectWrite("comment TST-2 hola mundo")
	h.expect("Comment added to TST-2")
}

func TestCommentEscAsksBeforeDiscarding(t *testing.T) {
	h := newHarness(t, 120, 30)
	h.keys("cdraft<esc>")
	h.expect("Discard this comment?")
	h.keys("<esc>")
	h.reject("Comment on")
	if len(h.fj.writes()) != 0 {
		t.Fatalf("nothing should have been written: %q", h.fj.writes())
	}
}

func TestAssignShortcutsAndPicker(t *testing.T) {
	h := newHarness(t, 120, 30)
	h.keys("u")
	h.expectWrite("assign TST-1 none")
	h.keys("i")
	h.expectWrite("assign TST-1 acc-me")
	h.keys("A")
	h.expect("Assign TST-1", "Leo Martín", "Ana Díaz", "Unassigned")
	h.keys("ana<enter>")
	h.expectWrite("assign TST-1 acc-ana")
}

func TestCreateIssueSelectsIt(t *testing.T) {
	h := newHarness(t, 120, 30)
	h.keys("n")
	h.expect("New issue in TST", "Task")
	h.reject("Test Plan") // Xray types are hidden
	h.keys("Brand new thing<ctrl+s>")
	h.expectWrite(`create TST-10 Task "Brand new thing" parent=`)
	h.expectWrite("assign TST-10 acc-me")
	h.expect("Created TST-10", "Brand new thing")
	if got := h.selected(); got != "TST-10" {
		t.Fatalf("the new issue should be selected, got %q", got)
	}
}

func TestCreateSubtaskFromParent(t *testing.T) {
	h := newHarness(t, 120, 30)
	h.keys("jN") // on TST-2
	h.expect("TST-2", "Sub-task (tech)")
	h.keys("child work<ctrl+s>")
	h.expectWrite(`create TST-10 Sub-task (tech) "child work" parent=TST-2`)
}

func TestCreateValidates(t *testing.T) {
	h := newHarness(t, 120, 30)
	h.keys("n<ctrl+s>")
	h.expect("A summary is required")
	if len(h.fj.writes()) != 0 {
		t.Fatalf("unexpected writes %q", h.fj.writes())
	}
}

func TestEdit(t *testing.T) {
	h := newHarness(t, 120, 30)
	h.keys("e")
	h.expect("Edit TST-1", "Platform migration")
	for range "Platform migration" {
		h.keys("<backspace>")
	}
	h.keys("Renamed epic<tab><right><tab>infra, q3<ctrl+s>")
	h.expectWrite("edit TST-1 labels,priority,summary")
	h.expect("Renamed epic")
}

func TestDeleteNeedsTheKey(t *testing.T) {
	h := newHarness(t, 120, 30)
	h.selectKey("TST-5")
	h.keys("D")
	h.expect("Delete issue", "Type TST-5 to confirm")
	h.keys("TST-4<enter>")
	if len(h.fj.writes()) != 0 {
		t.Fatalf("a wrong key must not delete: %q", h.fj.writes())
	}
	for range "TST-4" {
		h.keys("<backspace>")
	}
	h.keys("5<enter>") // the number alone also confirms
	h.expectWrite("delete TST-5")
	h.reject("Login redirect loop")
}

func TestBoardMovesCardsBetweenColumns(t *testing.T) {
	h := newHarness(t, 140, 37)
	h.keys("b")
	h.expect("Backlog 2", "In Refinement 1", "In Progress 3", "Done 1", "Terraform module")
	h.keys("L") // first Backlog card (most recently updated: TST-4) → In Refinement
	h.expectWrite("transition TST-4 In Refinement")
	h.expect("In Refinement 2", "Backlog 1")
	h.keys("b")
	h.expect("In Progress") // back to the list
}

func TestPaletteJumpsToIssueAndRunsActions(t *testing.T) {
	h := newHarness(t, 120, 30)
	h.keys(":6")
	h.expect("Open TST-6", "Rotate credentials")
	h.keys("<enter>")
	h.expect("Rotate credentials", "Description of Rotate credentials", "Comments")
	h.keys("<esc>:comment")
	h.expect("Comment")
	h.keys("<enter>")
	h.expect("Comment on TST-1")
}

func TestSearchPrompt(t *testing.T) {
	h := newHarness(t, 120, 30)
	h.keys("Ssafari<enter>")
	h.expect("Search 1", "TST-5")
	h.reject("TST-1 ")
	// A key in the search box opens the issue instead.
	h.keys("S2<enter>")
	h.expect("Migrate api to Cloud Run", "Description of Migrate api")
}

func TestDetailSectionsAndRelatedNavigation(t *testing.T) {
	h := newHarness(t, 140, 37)
	h.keys("j<enter>") // TST-2
	h.expect("TST-1 › TST-2", "Migrate api to Cloud Run", "Comments 2", "Sub-tasks 2")
	h.keys("3")
	h.expect("Done in infra PR #12.", "First look: needs a VPC connector.")
	s := h.screen()
	if strings.Index(s, "Done in infra") > strings.Index(s, "First look") {
		t.Fatalf("newest comment should come first:\n%s", s)
	}
	h.keys("4")
	h.expect("Dockerfile for api", "Terraform module", "0 of 2 done")
	h.keys("j<enter>") // TST-4
	h.expect("TST-2 › TST-4", "Terraform module")
	h.keys("<esc>P") // back to TST-2, then its parent
	h.expect("TST-1", "Platform migration")
	h.keys("<esc>6") // back on TST-2: history
	h.expect("History", "status  Backlog → In Progress")
}

func TestWatchToggle(t *testing.T) {
	h := newHarness(t, 120, 30)
	h.keys("w")
	h.expectWrite("watch TST-1")
	h.keys("w")
	h.expectWrite("unwatch TST-1")
}

func TestFoldingAndGroupJumps(t *testing.T) {
	h := newHarness(t, 120, 30)
	h.keys("}") // In Refinement header
	h.keys("<space>")
	h.reject("Rotate credentials")
	h.keys("<space>")
	h.expect("Rotate credentials")
	h.keys("Z") // fold everything
	h.reject("TST-1", "TST-6", "TST-5")
	h.keys("Z")
	h.expect("TST-1", "TST-6", "TST-5")
}

func TestStatusFilterAndFlatSort(t *testing.T) {
	h := newHarness(t, 120, 30)
	h.keys("f")
	h.expect("Show statuses", "Backlog", "In Progress")
	h.keys("<down><space><enter>") // Backlog, In Refinement, ... (workflow order)
	h.expect("TST-6", "status In Refinement")
	h.reject("TST-5")
	h.keys("<esc>v") // flat list: status moves into each row
	h.reject("▼")
	h.expect("TST-6    ◎ In Refinement", "TST-7    ● Done")
	h.keys("s")
	h.expect("Sort by", "Priority")
	h.keys("3") // priority
	s := h.screen()
	if strings.Index(s, "TST-5") > strings.Index(s, "TST-2") {
		t.Fatalf("highest priority first:\n%s", s)
	}
}

func TestStateIsRemembered(t *testing.T) {
	h := newHarness(t, 120, 30)
	h.keys("3q")
	if !h.quit {
		t.Fatal("q should quit")
	}
	data, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".config", "jira-cli", "tui-test.json"))
	if err != nil {
		t.Fatal(err)
	}
	var st uiState
	if err := json.Unmarshal(data, &st); err != nil || st.Scope != scopeTeam {
		t.Fatalf("scope not saved: %s", data)
	}
}

func TestHelpListsScreenKeys(t *testing.T) {
	h := newHarness(t, 140, 37)
	h.keys("?")
	h.expect("Keyboard shortcuts", "Issue list", "Filter by status", "Actions on the selected issue", "Legend")
	h.keys("<esc>b?")
	h.expect("Board", "Move card to previous / next status")
}

func TestStartsOnRequestedIssue(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	h := newHarness(t, 120, 30)
	h.app = New(h.app.ctx.cfg, h.app.ctx.client, Options{Open: "TST-6"})
	h.run(h.app.Init())
	h.dispatch(tea.WindowSizeMsg{Width: 120, Height: 30})
	h.expect("TST-6", "Rotate credentials", "Description of Rotate credentials")
	h.keys("<esc>")
	h.expect("In Progress", "TST-1")
}

func TestEditDescriptionInEditor(t *testing.T) {
	h := newHarness(t, 120, 30)
	var got string
	h.editor = func(text string) string {
		got = text
		return "Nueva descripción **editada**\n\n- con lista"
	}
	h.keys("E")
	if got != "Description of Platform migration" {
		t.Fatalf("editor should open with the current description as markdown, got %q", got)
	}
	h.expectWrite("edit TST-1 description")
	h.expect("Description of TST-1 updated", "Nueva descripción editada", "• con lista")
}

func TestEditDescriptionUnchanged(t *testing.T) {
	h := newHarness(t, 120, 30)
	h.keys("E")
	h.expect("Description unchanged")
	if len(h.fj.writes()) != 0 {
		t.Fatalf("nothing should be written: %q", h.fj.writes())
	}
}

func TestEditDescriptionRefusesLossyFormatting(t *testing.T) {
	h := newHarness(t, 120, 30)
	h.fj.issues["TST-1"].Fields.Description = json.RawMessage(`{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"ping "},{"type":"mention","attrs":{"id":"acc-ana","text":"@Ana"}}]}]}`)
	opened := false
	h.editor = func(text string) string { opened = true; return text }
	h.keys("E")
	if opened {
		t.Fatal("the editor must not open for a description it can't round-trip")
	}
	h.expect("can't keep")
}

func TestEditDescriptionDoesNotOverwriteConcurrentChanges(t *testing.T) {
	h := newHarness(t, 120, 30)
	h.editor = func(text string) string {
		// Someone edits the issue in the browser meanwhile.
		h.fj.mu.Lock()
		h.fj.issues["TST-1"].Fields.Description = adfPara("Edited in the browser")
		h.fj.mu.Unlock()
		return "my version"
	}
	h.keys("E")
	for _, w := range h.fj.writes() {
		if strings.HasPrefix(w, "edit TST-1") {
			t.Fatalf("must not overwrite: %q", h.fj.writes())
		}
	}
	h.expect("changed in Jira")
	if h.clipboard != "my version" {
		t.Fatalf("the edited text should be saved to the clipboard, got %q", h.clipboard)
	}
}

func TestCopyKeyAndLink(t *testing.T) {
	h := newHarness(t, 120, 30)
	h.keys("y")
	if h.clipboard != "TST-1" {
		t.Fatalf("y copies the key, got %q", h.clipboard)
	}
	h.keys("Y")
	if !strings.HasSuffix(h.clipboard, "/browse/TST-1") {
		t.Fatalf("Y copies the link, got %q", h.clipboard)
	}
}

func TestBranchNameFoldsAccents(t *testing.T) {
	is := &jira.Issue{Key: "PROJ-9"}
	is.Fields.Summary = "Migración de la web pública (añadir caché)"
	is.Fields.IssueType.Name = "Bug"
	if got := branchName(is); got != "fix/PROJ-9-migracion-de-la-web-publica-anadir-cache" {
		t.Fatalf("got %q", got)
	}
}

func TestBranchNameCutsAtWordBoundary(t *testing.T) {
	is := &jira.Issue{Key: "PROJ-324"}
	is.Fields.Summary = "Observabilidad: monitores Tier 1/2, SLOs y alertas burn rate hacia PagerDuty"
	is.Fields.IssueType.Name = "Task"
	if got := branchName(is); got != "feat/PROJ-324-observabilidad-monitores-tier-1-2-slos-y-alertas" {
		t.Fatalf("got %q", got)
	}
}
