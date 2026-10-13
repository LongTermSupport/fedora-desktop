package teams

import (
	"io"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestListPrintsTheHeadingBlankLineAndRowsWhenNotColoured(t *testing.T) {
	h := world(t)
	if code := h.app.Run([]string{"--list"}); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.errOut.String())
	}
	lines := strings.Split(h.out.String(), "\n")
	if lines[0] != headingText() || lines[1] != "" {
		t.Fatalf("heading and blank line: %q", lines[:2])
	}
	if !strings.HasPrefix(lines[2], "alpha ") || !strings.Contains(lines[2], "up to date") || !strings.Contains(lines[2], "port 6167") {
		t.Fatalf("team row %q", lines[2])
	}
	if strings.Contains(h.out.String(), "\x1b") {
		t.Fatal("no colour when piped")
	}
	if h.errOut.String() != "" {
		t.Fatalf("stderr %q", h.errOut.String())
	}
	if len(lines) != 2+8+1 {
		t.Fatalf("%d lines: %q", len(lines), h.out.String())
	}
}

func TestListInColourHasNoBlankLine(t *testing.T) {
	h := world(t)
	h.app.Term.IsTerminal = func(any) bool { return true }
	if code := h.app.Run([]string{"-l"}); code != term.ExitOK {
		t.Fatalf("exit %d", code)
	}
	lines := strings.Split(h.out.String(), "\n")
	if lines[0] != cBold+headingText()+cReset {
		t.Fatalf("heading %q", lines[0])
	}
	if lines[1] == "" || !strings.Contains(lines[1], cGreen+"up ") {
		t.Fatalf("first row %q", lines[1])
	}
	if got := ansi.ReplaceAllString(lines[1], ""); !strings.HasPrefix(got, "alpha") {
		t.Fatalf("row %q", got)
	}
}

func TestListNoColourWithNoColor(t *testing.T) {
	h := world(t)
	h.app.Term.IsTerminal = func(any) bool { return true }
	h.env["NO_COLOR"] = "1"
	if code := h.app.Run([]string{"--list"}); code != term.ExitOK {
		t.Fatalf("exit %d", code)
	}
	if strings.Contains(h.out.String(), "\x1b") {
		t.Fatalf("NO_COLOR is honoured: %q", h.out.String())
	}
}

func TestListWithNoTeams(t *testing.T) {
	h := newHarness(t)
	h.onDeclare("declared", "")
	h.on("systemctl list-units", "", 0)
	if code := h.app.Run([]string{"--list"}); code != term.ExitOK {
		t.Fatalf("exit %d", code)
	}
	if want := "No agent teams on this machine yet. ccy-teams create starts one.\n"; h.errOut.String() != want || h.out.String() != "" {
		t.Fatalf("stdout %q stderr %q", h.out.String(), h.errOut.String())
	}
}

func TestListNeedingSudoSaysSoAndFails(t *testing.T) {
	h := world(t)
	h.onErr(sudoBus+"list alpha", "sudo: a password is required", 1)
	if code := h.app.Run([]string{"--list"}); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	want := "The members of alpha are not shown: reading them needs root. Run sudo -v first, or run play-agent-bus.yml once, which installs a rule that lets ccy-teams read them without a password.\n"
	if h.errOut.String() != want {
		t.Fatalf("stderr %q", h.errOut.String())
	}
	if !strings.Contains(h.out.String(), "members: need sudo") {
		t.Fatalf("the table is still printed: %q", h.out.String())
	}
	for _, c := range h.fake.Calls() {
		if c.Line() == "sudo -v" {
			t.Fatal("--list never runs sudo -v")
		}
	}
}

func TestListFailsWhenMembersCannotBeRead(t *testing.T) {
	h := world(t)
	h.onErr(sudoBus+"list alpha", "agent-bus: no admin token", 78)
	if code := h.app.Run([]string{"--list"}); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(h.out.String(), "members: read failed (above)") {
		t.Fatalf("stdout %q", h.out.String())
	}
}

func TestListWithAnUnreadableSeatFails(t *testing.T) {
	h := world(t)
	h.onErr("agent-bus seat list", "boom", 1)
	if code := h.app.Run([]string{"--list"}); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(h.out.String(), "a seat is unreadable (above)") {
		t.Fatalf("stdout %q", h.out.String())
	}
}

func TestListDownTeamIsStateNotFailure(t *testing.T) {
	h := world(t)
	h.on("systemctl is-active agent-bus-hs@alpha.service", "inactive\n", 3)
	if code := h.app.Run([]string{"--list"}); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.errOut.String())
	}
	if !strings.Contains(h.out.String(), "down (inactive)") || !strings.Contains(h.out.String(), "port 6167, members: homeserver down") {
		t.Fatalf("stdout %q", h.out.String())
	}
}

func TestListCollectionFailureExitsOne(t *testing.T) {
	h := newHarness(t)
	h.onErr(h.declarePrefix("declared"), "declare: bad", 78)
	if code := h.app.Run([]string{"--list"}); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if h.out.String() != "" {
		t.Fatalf("stdout %q", h.out.String())
	}
}

func TestListWriteFailureIsAnError(t *testing.T) {
	h := world(t)
	h.app.Stdout = failingWriter{}
	if code := h.app.Run([]string{"--list"}); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
