package teams

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const seatConvID = "0f1e2d3c-4b5a-4968-8776-655443322110"

// plantV1Seat writes a v1 session of checkout that held seat@team, with its conversation
// recorded (sessionID "" records none) and that conversation's transcript.
func plantV1Seat(t *testing.T, checkout, session, seat, team, sessionID string) {
	t.Helper()
	dir := filepath.Join(checkout, ".claude/ccy/pingbus/sessions", session)
	files := map[string]string{
		filepath.Join(dir, "applied.json"): `{"v":1,"generation":1,"seats":[{"team":"` + team + `","seat":"` + seat +
			`","handle":"proj.` + seat + `+local.podman","state":"held"}],"refused":[]}`,
	}
	if sessionID != "" {
		files[filepath.Join(dir, "conversation.json")] = `{"v":1,"session":"` + session + `","session_id":"` + sessionID +
			`","source":"startup","written_ms":1791000000000}`
		files[filepath.Join(checkout, ".claude/ccy/projects/-workspace", sessionID+".jsonl")] = "{}\n"
	}
	for path, body := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func seatConversation(checkout string) (*SeatConversation, *bytes.Buffer, *bytes.Buffer) {
	var out, errOut bytes.Buffer
	return &SeatConversation{
		Stdout: &out, Stderr: &errOut,
		Getenv: func(k string) string {
			if k == "HOME" {
				return "/nonexistent"
			}
			return ""
		},
		Getwd: func() (string, error) { return checkout, nil },
		NowMS: func() int64 { return 1795000000000 },
	}, &out, &errOut
}

// Issue 417 E7: --set points the seats at a conversation of this checkout, which the next lookup
// gives, over the session's older record; the id is printed.
func TestSeatConversationSetRepairsThePointer(t *testing.T) {
	checkout := t.TempDir()
	plantV1Seat(t, checkout, "ccy-proj-2", "dev1", "team-a", seatConvID)
	const other = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	writeFile(t, filepath.Join(checkout, ".claude/ccy/projects/-workspace", other+".jsonl"), "{}\n")
	c, out, errOut := seatConversation(checkout)
	if code := c.Run([]string{"dev1@team-a,qa@team-b", "--set", other}); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if out.String() != other+"\n" || !strings.Contains(errOut.String(), "dev1@team-a,qa@team-b now resume "+other) {
		t.Fatalf("stdout %q stderr %q", out.String(), errOut.String())
	}
	c, out, errOut = seatConversation(checkout)
	if code := c.Run([]string{"qa@team-b"}); code != term.ExitOK || out.String() != other+"\n" {
		t.Fatalf("after --set: exit %d, stdout %q stderr %q", code, out.String(), errOut.String())
	}
}

// E7: an id with no transcript here is refused, exit 1, and nothing is written.
func TestSeatConversationSetRefusesAConversationNotHere(t *testing.T) {
	checkout := t.TempDir()
	c, out, errOut := seatConversation(checkout)
	if code := c.Run([]string{"dev1@team-a", "--set", seatConvID}); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if out.Len() != 0 || !strings.Contains(errOut.String(), "no transcript in this checkout") {
		t.Fatalf("stdout %q stderr %q", out.String(), errOut.String())
	}
	if _, err := os.Stat(filepath.Join(checkout, ".claude/ccy/seat-conversations")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a refused --set wrote: %v", err)
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Issue 409: the recorded id of the seat's session, alone on stdout, for the launcher's refusal.
func TestSeatConversationPrintsTheRecordedID(t *testing.T) {
	checkout := t.TempDir()
	plantV1Seat(t, checkout, "ccy-proj", "dev1", "team-a", seatConvID)
	c, out, errOut := seatConversation(checkout)
	if code := c.Run([]string{"dev1@team-a"}); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if out.String() != seatConvID+"\n" || errOut.Len() != 0 {
		t.Fatalf("stdout %q stderr %q", out.String(), errOut.String())
	}
}

func TestSeatConversationRefusesASeatWithNoRecord(t *testing.T) {
	checkout := t.TempDir()
	plantV1Seat(t, checkout, "ccy-proj", "dev1", "team-a", "")
	c, out, errOut := seatConversation(checkout)
	if code := c.Run([]string{"dev1@team-a"}); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if out.Len() != 0 || !strings.HasPrefix(errOut.String(), "ccy-teams seats conversation: no recorded conversation") ||
		!strings.Contains(errOut.String(), "ccy-proj") {
		t.Fatalf("stdout %q stderr %q", out.String(), errOut.String())
	}
}

func TestSeatConversationUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"a@b", "c@d"}, {"a@b", "--set"}, {"a@b", "--set", seatConvID, "x"}, {"a@b", "--set", ""}} {
		c, out, errOut := seatConversation(t.TempDir())
		if code := c.Run(args); code != term.ExitUsage || out.Len() != 0 || !strings.Contains(errOut.String(), "seats conversation <seat>@<team>") {
			t.Fatalf("%q: exit %d, stdout %q stderr %q", args, code, out.String(), errOut.String())
		}
	}
}

func TestSeatConversationNeedsTheWorkingDirectory(t *testing.T) {
	c, out, errOut := seatConversation("")
	c.Getwd = func() (string, error) { return "", errors.New("gone") }
	if code := c.Run([]string{"dev1@team-a"}); code != term.ExitFailure || out.Len() != 0 || !strings.Contains(errOut.String(), "gone") {
		t.Fatalf("exit %d, stdout %q stderr %q", code, out.String(), errOut.String())
	}
}

// `ccy-teams seats conversation` is dispatched to it, its id on the payload stream.
func TestDispatchSeatsConversation(t *testing.T) {
	h := newHarness(t)
	checkout := t.TempDir()
	plantV1Seat(t, checkout, "ccy-proj", "dev1", "team-a", seatConvID)
	h.app.SeatConversation.Getwd = func() (string, error) { return checkout, nil }
	if code := h.app.Run([]string{"seats", "conversation", "dev1@team-a"}); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	if h.Out.String() != seatConvID+"\n" {
		t.Fatalf("stdout %q", h.Out.String())
	}
}
