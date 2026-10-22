package board

import (
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestListPrintsTheHeadingBlankLineAndRowsWhenNotColoured(t *testing.T) {
	h := populated(t)
	if code := h.c.List(t.Context()); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	lines := strings.Split(h.Out.String(), "\n")
	if !strings.HasPrefix(lines[0], "TEAM ") || !strings.HasSuffix(lines[0], "WHERE") || lines[1] != "" {
		t.Fatalf("heading and blank line: %q", lines[:2])
	}
	if !strings.HasPrefix(lines[2], "alpha ") || !strings.Contains(lines[2], "up to date") || !strings.Contains(lines[2], "port 6167") {
		t.Fatalf("team row %q", lines[2])
	}
	if strings.Contains(h.Out.String(), "\x1b") {
		t.Fatal("no colour when piped")
	}
	if h.ErrOut.String() != "" {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
	if len(lines) != 2+8+1 {
		t.Fatalf("%d lines: %q", len(lines), h.Out.String())
	}
}

func TestListInColourHasNoBlankLine(t *testing.T) {
	h := populated(t)
	h.c.Term.IsTerminal = func(any) bool { return true }
	if code := h.c.List(t.Context()); code != term.ExitOK {
		t.Fatalf("exit %d", code)
	}
	lines := strings.Split(h.Out.String(), "\n")
	if !strings.HasPrefix(lines[0], term.Bold+"TEAM ") || !strings.HasSuffix(lines[0], "WHERE"+term.Reset) {
		t.Fatalf("heading %q", lines[0])
	}
	if lines[1] == "" || !strings.Contains(lines[1], cGreen+"up ") {
		t.Fatalf("first row %q", lines[1])
	}
	if got := ansi.ReplaceAllString(lines[1], ""); !strings.HasPrefix(got, "alpha") {
		t.Fatalf("row %q", got)
	}
}

func TestListOnANarrowTerminalPutsTheRestOfEachRowOnTheLineBelow(t *testing.T) {
	h := populated(t)
	h.c.Term.IsTerminal = func(any) bool { return true }
	h.Env["NO_COLOR"] = "1"
	h.c.Columns = func(any) int { return 70 }
	if code := h.c.List(t.Context()); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	lines := strings.Split(strings.TrimSuffix(h.Out.String(), "\n"), "\n")
	if strings.Contains(lines[0], "APPLIED") || !strings.HasSuffix(lines[0], "STATE") {
		t.Fatalf("heading %q", lines[0])
	}
	if len(lines) != 2+2*8 || lines[1] != "" {
		t.Fatalf("%d lines: %q", len(lines), h.Out.String())
	}
	for i := 2; i < len(lines); i += 2 {
		if !strings.HasPrefix(lines[i], "alpha ") || !strings.HasPrefix(lines[i+1], restIndent) || !strings.Contains(lines[i+1], "APPLIED ") {
			t.Fatalf("row %q, then %q", lines[i], lines[i+1])
		}
	}
	if !strings.Contains(h.Out.String(), "proj.1+local.podman") {
		t.Fatalf("the seat's handle is shown: %q", h.Out.String())
	}
}

func TestListNoColourWithNoColor(t *testing.T) {
	h := populated(t)
	h.c.Term.IsTerminal = func(any) bool { return true }
	h.Env["NO_COLOR"] = "1"
	if code := h.c.List(t.Context()); code != term.ExitOK {
		t.Fatalf("exit %d", code)
	}
	if strings.Contains(h.Out.String(), "\x1b") {
		t.Fatalf("NO_COLOR is honoured: %q", h.Out.String())
	}
}

func TestListWithNoTeams(t *testing.T) {
	h := newRig(t)
	h.WriteHostVars(t, "")
	h.OnDeclare("status", "")
	h.On("systemctl list-units", "", 0)
	if code := h.c.List(t.Context()); code != term.ExitOK {
		t.Fatalf("exit %d", code)
	}
	if want := "No agent teams on this machine yet. ccy-teams create starts one.\n"; h.ErrOut.String() != want || h.Out.String() != "" {
		t.Fatalf("stdout %q stderr %q", h.Out.String(), h.ErrOut.String())
	}
}

func TestListNeedingSudoSaysSoAndFails(t *testing.T) {
	h := populated(t)
	h.OnErr(teamtest.SudoBus+"list alpha", "sudo: a password is required", 1)
	if code := h.c.List(t.Context()); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	want := "The members of alpha are not shown: reading them needs root. Run sudo -v first, or run play-agent-bus.yml once, which installs a rule that lets ccy-teams read them without a password.\n"
	if h.ErrOut.String() != want {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
	if !strings.Contains(h.Out.String(), "members: need sudo") {
		t.Fatalf("the table is still printed: %q", h.Out.String())
	}
	for _, c := range h.Fake.Calls() {
		if c.Line() == "sudo -v" {
			t.Fatal("--list never runs sudo -v")
		}
	}
}

func TestListFailsWhenMembersCannotBeRead(t *testing.T) {
	h := populated(t)
	h.OnErr(teamtest.SudoBus+"list alpha", "agent-bus: no admin token", 78)
	if code := h.c.List(t.Context()); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(h.Out.String(), "members: read failed (above)") {
		t.Fatalf("stdout %q", h.Out.String())
	}
}

// The seats cannot be read at all: a member.json nested too deep for v1's decoder, on which
// its seat list crashed (SPEC-view B-S3).
func TestListWithAnUnreadableSeatFails(t *testing.T) {
	h := populated(t)
	teamtest.MustWrite(t, filepath.Join(teamtest.SeatDir(h.Checkout(), "alpha", "dev"), "member.json"), strings.Repeat("[", 30000))
	if code := h.c.List(t.Context()); code != term.ExitFailure {
		t.Fatalf("exit %d\n%s\n%s", code, h.Out.String(), h.ErrOut.String())
	}
	if !strings.Contains(h.Out.String(), "a seat is unreadable (above)") {
		t.Fatalf("stdout %q", h.Out.String())
	}
}

func TestListDownTeamIsStateNotFailure(t *testing.T) {
	h := populated(t)
	h.On("systemctl list-units", "agent-bus-hs@alpha.service loaded inactive dead x\n", 0)
	if code := h.c.List(t.Context()); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	if !strings.Contains(h.Out.String(), "down (inactive)") || !strings.Contains(h.Out.String(), "port 6167, members: homeserver down") {
		t.Fatalf("stdout %q", h.Out.String())
	}
}

func TestListCollectionFailureExitsOne(t *testing.T) {
	h := newRig(t)
	h.WriteHostVars(t, "agent_bus_teams:\n  - team: alpha\n    port: port\n")
	if code := h.c.List(t.Context()); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if h.Out.String() != "" {
		t.Fatalf("stdout %q", h.Out.String())
	}
}

func TestListWriteFailureIsAnError(t *testing.T) {
	h := populated(t)
	h.c.Stdout = failingWriter{}
	if code := h.c.List(t.Context()); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
