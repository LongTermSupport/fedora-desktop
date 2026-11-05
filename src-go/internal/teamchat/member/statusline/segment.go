package statusline

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/colour"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/client"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/homefs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/lock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

const (
	// label starts the segment; it takes 3 columns, and a continuation row is indented as many.
	label  = "👥 "
	indent = "   "
	// staleMS is how far behind the wall clock status.json may be while the link holds a team.
	staleMS = 90_000
)

// letters are the role letters (n3e-D3).
var letters = map[string]string{proto.RoleCoordinator: "c", proto.RoleWorker: "w", proto.RoleProbe: "p"}

// word is one unit the rows wrap between: its text (ASCII, one column a byte), its colour
// escape, and its span: consecutive words of one span share one escape.
type word struct {
	text string
	sgr  string
	span int
}

// segment is the team segment, or "" for none.
func segment(getenv func(string) string, clk clock.Clock) string {
	run, err := client.Runtime(getenv)
	if err != nil {
		return ""
	}
	facts := readRun(run)
	st, held, ok := facts.status, facts.held, facts.ok
	var ws words
	switch {
	case !held:
		ws.note(downText(client.ReadDown(run)))
	case ok:
		for i, t := range st.Teams {
			text := piece(t)
			if i < len(st.Teams)-1 {
				text += ","
			}
			ws.team(colour.SGR(t.Colour), text)
		}
		if len(st.Teams) > 0 && clk.WallMS()-st.WrittenMS > staleMS {
			ws.note("teamchat not answering")
		}
	}
	if ok && st.Desired == state.DesiredRefused {
		ws.note("teamchat membership refused")
	}
	if ok && st.UnusableHookStdin > 0 {
		ws.note("teamchat hook input unusable: " + strconv.FormatInt(st.UnusableHookStdin, 10))
	}
	if len(ws) == 0 {
		return ""
	}
	return wrapped(ws, columns(getenv("COLUMNS")))
}

// words are the segment's words in order.
type words []word

// team adds one team's piece as a span of its own.
func (ws *words) team(sgr, text string) {
	*ws = append(*ws, word{text: text, sgr: sgr, span: len(*ws) + 1})
}

// note adds a red note, one word a space, all in one span.
func (ws *words) note(text string) {
	span := len(*ws) + 1
	for f := range strings.FieldsSeq(text) {
		*ws = append(*ws, word{text: f, sgr: red, span: span})
	}
}

// runFacts are what one probe of link.lock and one read of status.json give: the file, whether
// the link holds its lock, and whether the file was this run's and read whole.
type runFacts struct {
	status state.Status
	held   bool
	ok     bool
}

// readRun is the run's facts. A run directory that cannot be opened is a link not held; a
// status.json that cannot be read, or is another run's, is not ok.
func readRun(run paths.Run) runFacts {
	root, err := homefs.Open(run.Dir(), homefs.Private)
	if err != nil {
		return runFacts{}
	}
	defer func() {
		if cerr := root.Close(); cerr != nil {
			return
		}
	}()
	held, probeErr := lock.Probe(root, run.LinkLock())
	st, readErr := state.Read(root, run.Status(), state.MaxStatus, state.DecodeStatus)
	return runFacts{
		status: st,
		held:   held && probeErr == nil,
		ok:     readErr == nil && st.Run.String() == filepath.Base(run.Dir()),
	}
}

// piece is one team's text: `<team>: <member> (<r>)` and its marks.
func piece(t state.StatusTeam) string {
	member := "member?"
	if t.Member != nil {
		member = *t.Member
	}
	role := "role?"
	if t.Role != nil {
		if l, ok := letters[*t.Role]; ok {
			role = l
		}
	}
	var b strings.Builder
	b.WriteString(t.Team.String() + ": " + member + " (" + role + ")")
	for _, m := range []struct {
		on   bool
		mark string
	}{{t.Down, "(down)"}, {t.Stalled, "(stalled)"}, {t.Missing > 0, "(missing)"}, {t.Failed != nil, "(failed)"}} {
		if m.on {
			b.WriteString(" " + m.mark)
		}
	}
	return b.String()
}

// downText is the red form of a link that does not answer (LN3's reading, n3e-D17).
func downText(d client.Down) string {
	switch d.State {
	case client.DownRestarting:
		return "teamchat restarting"
	case client.DownStarting:
		return "teamchat starting"
	}
	if (d.Kind == state.KindCCY || d.Kind == state.KindLXC) && d.Session != "" {
		return "teamchat stopped: ccy-teams restart " + d.Session
	}
	return "teamchat stopped: relaunch this session"
}

// columns is COLUMNS when it is 1 to 6 digits above zero, else 0: one row (S28).
func columns(v string) int {
	if v == "" || len(v) > 6 {
		return 0
	}
	for _, c := range v {
		if c < '0' || c > '9' {
			return 0
		}
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0
	}
	return n
}

// wrapped is the label and the words in rows no wider than cols (one row for 0), breaking only
// between words, a later row indented; each span is its escape, its words and a reset, and the
// row's label or indent is inside its first span.
func wrapped(words []word, cols int) string {
	var rows [][]word
	cur := make([]word, 0, len(words))
	width := len(indent)
	for _, w := range words {
		if len(cur) > 0 && cols > 0 && width+1+len(w.text) > cols {
			rows, cur, width = append(rows, cur), make([]word, 0, len(words)), len(indent)
		}
		if len(cur) > 0 {
			width++
		}
		width += len(w.text)
		cur = append(cur, w)
	}
	rows = append(rows, cur)
	var b strings.Builder
	for r, row := range rows {
		if r > 0 {
			b.WriteByte('\n')
		}
		prefix := label
		if r > 0 {
			prefix = indent
		}
		for i, w := range row {
			switch {
			case i == 0:
				b.WriteString(w.sgr + prefix + w.text)
			case w.span == row[i-1].span:
				b.WriteString(" " + w.text)
			default:
				b.WriteString(colour.Reset + " " + w.sgr + w.text)
			}
		}
		b.WriteString(colour.Reset)
	}
	return b.String()
}
