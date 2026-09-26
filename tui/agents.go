package tui

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Claude Code sessions running on this machine. For each `claude` process
// we learn its terminal (to jump to its iTerm tab), its folder, and — from
// the tail of its transcript in ~/.claude/projects — its git branch, when
// it last did something and whether it's working or waiting for you.

type agentsScannedMsg struct {
	agents []agentSession
	err    error
}

// claudeHome is ~/.claude, swappable in tests.
var claudeHome = func() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}

func cmdScanAgents() tea.Cmd {
	return func() tea.Msg { return scanAgents() }
}

func scanAgents() agentsScannedMsg {
	out, err := sys.run(5*time.Second, "", "pgrep", "-x", "claude")
	if err != nil {
		// pgrep exits 1 when nothing matches: no agents, not an error.
		return agentsScannedMsg{}
	}
	var pids []string
	for _, f := range strings.Fields(string(out)) {
		if _, err := strconv.Atoi(f); err == nil && f != strconv.Itoa(os.Getpid()) {
			pids = append(pids, f)
		}
	}
	if len(pids) == 0 {
		return agentsScannedMsg{}
	}
	list := strings.Join(pids, ",")
	ttys := map[int]string{}
	if out, err := sys.run(5*time.Second, "", "ps", "-o", "pid=,tty=", "-p", list); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			f := strings.Fields(line)
			if len(f) == 2 && f[1] != "??" {
				if pid, err := strconv.Atoi(f[0]); err == nil {
					ttys[pid] = "/dev/" + f[1]
				}
			}
		}
	}
	// lsof exits non-zero when a pid vanished meanwhile; its output is
	// still good for the rest.
	cwdOut, _ := sys.run(5*time.Second, "", "lsof", "-a", "-d", "cwd", "-p", list, "-Fpn")
	var agents []agentSession
	for pid, cwd := range parseLsofCwd(string(cwdOut)) {
		a := agentSession{pid: pid, tty: ttys[pid], cwd: cwd, state: "unknown"}
		if f := latestTranscript(cwd); f != "" {
			readTranscriptTail(f, &a)
		}
		if a.branch == "" || a.branch == "HEAD" {
			if out, err := sys.run(3*time.Second, cwd, "git", "branch", "--show-current"); err == nil {
				a.branch = strings.TrimSpace(string(out))
			}
		}
		agents = append(agents, a)
	}
	sort.Slice(agents, func(i, j int) bool { return agents[i].pid < agents[j].pid })
	return agentsScannedMsg{agents: agents}
}

// parseLsofCwd reads `lsof -Fpn` output: "p<pid>" then "n<path>" lines.
func parseLsofCwd(out string) map[int]string {
	res := map[int]string{}
	pid := 0
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "p"):
			pid, _ = strconv.Atoi(line[1:])
		case strings.HasPrefix(line, "n") && pid != 0:
			res[pid] = line[1:]
		}
	}
	return res
}

// projectDir is where Claude Code keeps a folder's transcripts: the path
// with every non-alphanumeric character turned into '-'.
func projectDir(cwd string) string { return projectDirIn(claudeHome(), cwd) }

func projectDirIn(claude, cwd string) string {
	var b strings.Builder
	for _, r := range cwd {
		if r < 128 && isAlnum(byte(r)) {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return filepath.Join(claude, "projects", b.String())
}

// latestTranscript returns the most recently written session of a folder.
func latestTranscript(cwd string) string {
	files, _ := filepath.Glob(filepath.Join(projectDir(cwd), "*.jsonl"))
	best, bestAt := "", time.Time{}
	for _, f := range files {
		if fi, err := os.Stat(f); err == nil && fi.ModTime().After(bestAt) {
			best, bestAt = f, fi.ModTime()
		}
	}
	return best
}

type transcriptEntry struct {
	Type           string `json:"type"`
	Timestamp      string `json:"timestamp"`
	GitBranch      string `json:"gitBranch"`
	PermissionMode string `json:"permissionMode"`
	Message        struct {
		Role       string          `json:"role"`
		StopReason string          `json:"stop_reason"`
		Content    json.RawMessage `json:"content"`
	} `json:"message"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
	Name string `json:"name"` // tool_use
}

// Tools that stop and wait for the human by design.
var askTools = map[string]bool{"AskUserQuestion": true, "ExitPlanMode": true}

// approvalAfter: a tool call without a result for this long, outside
// bypass mode, is most likely a permission prompt.
const approvalAfter = 60 * time.Second

// readTranscriptTail fills branch, last activity, last message and state
// from the end of a transcript. The last conversational entry decides:
//   - an assistant turn that ended, or a question/plan put to you: waiting;
//   - a tool call with no result for a while, when Claude isn't running in
//     bypass mode: probably a permission prompt (approval);
//   - anything else (tool running, tool result, fresh prompt): working.
func readTranscriptTail(path string, a *agentSession) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	const tail = 512 << 10
	if fi, err := f.Stat(); err == nil && fi.Size() > tail {
		_, _ = f.Seek(fi.Size()-tail, io.SeekStart)
	}
	data, _ := io.ReadAll(f)
	lines := bytes.Split(data, []byte("\n"))

	mode := ""
	stateSet := false
	pendingTool := false
	for i := len(lines) - 1; i >= 0; i-- {
		var e transcriptEntry
		if json.Unmarshal(lines[i], &e) != nil {
			continue
		}
		if mode == "" && e.Type == "permission-mode" {
			mode = e.PermissionMode
		}
		if a.branch == "" && e.GitBranch != "" {
			a.branch = e.GitBranch
		}
		if a.lastAt.IsZero() && e.Timestamp != "" {
			a.lastAt, _ = time.Parse(time.RFC3339Nano, e.Timestamp)
		}
		if e.Type != "assistant" && e.Type != "user" {
			continue
		}
		var blocks []contentBlock
		_ = json.Unmarshal(e.Message.Content, &blocks)
		if !stateSet {
			stateSet = true
			a.state = "working"
			if e.Type == "assistant" {
				last := contentBlock{}
				if len(blocks) > 0 {
					last = blocks[len(blocks)-1]
				}
				switch {
				case e.Message.StopReason == "end_turn":
					a.state = "waiting"
				case last.Type == "tool_use" && askTools[last.Name]:
					a.state = "waiting"
				case last.Type == "tool_use" || e.Message.StopReason == "tool_use":
					pendingTool = true
				}
			}
		}
		if a.lastMsg == "" && e.Type == "assistant" {
			for j := len(blocks) - 1; j >= 0; j-- {
				if blocks[j].Type == "text" && strings.TrimSpace(blocks[j].Text) != "" {
					a.lastMsg = oneLine(blocks[j].Text)
					break
				}
			}
		}
		if a.lastMsg != "" && a.branch != "" && !a.lastAt.IsZero() && mode != "" {
			break
		}
	}
	if pendingTool && mode != "bypassPermissions" && !a.lastAt.IsZero() && now().Sub(a.lastAt) > approvalAfter {
		a.state = "approval"
	}
}

// oneLine collapses whitespace so a message fits a single display line.
func oneLine(s string) string {
	sc := bufio.NewScanner(strings.NewReader(s))
	var parts []string
	for sc.Scan() {
		if t := strings.TrimSpace(sc.Text()); t != "" {
			parts = append(parts, t)
		}
	}
	return strings.Join(strings.Fields(strings.Join(parts, " ")), " ")
}

// lastSessionIn reports the most recent Claude activity in a folder, even
// when no process runs there anymore.
func lastSessionIn(dir string) (time.Time, string) {
	f := latestTranscript(dir)
	if f == "" {
		return time.Time{}, ""
	}
	var a agentSession
	readTranscriptTail(f, &a)
	return a.lastAt, a.lastMsg
}
