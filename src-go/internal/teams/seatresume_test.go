package teams

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const otherConvID = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"

func seatResume(checkout string) (*SeatResume, *bytes.Buffer, *bytes.Buffer) {
	var out, errOut bytes.Buffer
	return &SeatResume{
		Stdout: &out, Stderr: &errOut,
		Getenv: func(k string) string {
			if k == "HOME" {
				return "/nonexistent"
			}
			return ""
		},
		Getwd: func() (string, error) { return checkout, nil },
	}, &out, &errOut
}

// resumed is a seat-resume run: its exit, claude's arguments it printed, the count of them
// before the separator, and stderr.
type resumed struct {
	code   term.ExitCode
	args   []string
	before int
	stderr string
}

// resumeRun runs seat-resume over claude's arguments words (all before any `--`) in a checkout
// where dev1@team-a's session recorded seatConvID (record false: no session at all); a first
// word --relaunch is seat-resume's own.
func resumeRun(t *testing.T, record bool, words ...string) resumed {
	t.Helper()
	checkout := t.TempDir()
	if record {
		plantV1Seat(t, checkout, "ccy-proj", "dev1", "team-a", seatConvID)
	}
	args := []string{"dev1@team-a"}
	if len(words) > 0 && words[0] == flagRelaunch {
		args, words = append(args, flagRelaunch), words[1:]
	}
	args = append(append(args, strconv.Itoa(len(words)), "--"), words...)
	r, out, errOut := seatResume(checkout)
	got := resumed{code: r.Run(args), stderr: errOut.String(), before: -1}
	if got.code != term.ExitOK {
		if out.Len() != 0 {
			t.Errorf("%q: exit %d with stdout %q", words, got.code, out.String())
		}
		return got
	}
	records := strings.Split(strings.TrimSuffix(out.String(), "\x00"), "\x00")
	n, err := strconv.Atoi(records[0])
	if err != nil || !strings.HasSuffix(out.String(), "\x00") {
		t.Fatalf("%q: stdout %q is not NUL records led by a count", words, out.String())
	}
	got.before, got.args = n, records[1:]
	return got
}

func wantSays(t *testing.T, stderr string, says ...string) {
	t.Helper()
	for _, s := range says {
		if !strings.Contains(stderr, s) {
			t.Errorf("stderr %q does not say %q", stderr, s)
		}
	}
}

func wantArgs(t *testing.T, got resumed, before int, args ...string) {
	t.Helper()
	if got.code != term.ExitOK || got.before != before || !slices.Equal(got.args, args) {
		t.Errorf("exit %d, before %d, args %q; want before %d, args %q; stderr %q", got.code, got.before, got.args, before, args, got.stderr)
	}
}

// E8 f: a plain launch resumes the seats' conversation: --resume <id> appended.
func TestSeatResumePlainLaunchResumesTheRecord(t *testing.T) {
	got := resumeRun(t, true, "--model", "x")
	wantArgs(t, got, 2, "--model", "x", "--resume", seatConvID)
	wantSays(t, got.stderr, "resuming the conversation recorded for dev1@team-a: "+seatConvID, "ccy --teams dev1@team-a --fresh")
}

// E8 f: no record: 64, the case and both ways on.
func TestSeatResumeWithNoRecordIsRefusedNamingBothWaysOn(t *testing.T) {
	got := resumeRun(t, false)
	if got.code != term.ExitUsage {
		t.Fatalf("exit %d", got.code)
	}
	wantSays(t, got.stderr, "no conversation is recorded for dev1@team-a",
		"ccy-teams seats conversation dev1@team-a --set <id>", "ccy --teams dev1@team-a --fresh")
}

// E8 a: --continue (or -c) anywhere is refused, naming the plain launch and --fresh.
func TestSeatResumeRefusesContinue(t *testing.T) {
	for _, words := range [][]string{{"--continue"}, {"-c"}, {"--model", "x", "-c"}} {
		got := resumeRun(t, true, words...)
		if got.code != term.ExitUsage {
			t.Fatalf("%q: exit %d", words, got.code)
		}
		wantSays(t, got.stderr, "refuses --continue", "    ccy --teams dev1@team-a\n", "ccy --teams dev1@team-a --fresh")
		if strings.Contains(got.stderr, "--resume") {
			t.Errorf("%q: the refusal names --resume: %q", words, got.stderr)
		}
	}
}

// E8 b: --fresh starts a new conversation and says so; with a resume or --rebind it is refused.
func TestSeatResumeFresh(t *testing.T) {
	got := resumeRun(t, true, "--model", "x", "--fresh")
	wantArgs(t, got, 2, "--model", "x")
	wantSays(t, got.stderr, "--fresh", "new conversation", "dev1@team-a")
	for _, words := range [][]string{
		{"--fresh", "--resume", seatConvID},
		{"--fresh", "--resume"},
		{"--fresh", "-r"},
		{"--fresh", "--session-id", seatConvID},
		{"--fresh", "--rebind"},
	} {
		got := resumeRun(t, true, words...)
		if got.code != term.ExitUsage || !strings.Contains(got.stderr, "--fresh") {
			t.Errorf("%q: exit %d, stderr %q", words, got.code, got.stderr)
		}
	}
}

// --fresh and --rebind are ccy's only before claude's separator; after it they are claude's words.
func TestSeatResumeTakesCcyWordsBeforeTheSeparatorOnly(t *testing.T) {
	checkout := t.TempDir()
	plantV1Seat(t, checkout, "ccy-proj", "dev1", "team-a", seatConvID)
	r, out, errOut := seatResume(checkout)
	if code := r.Run([]string{"dev1@team-a", "1", "--", "--fresh", "--x", "--fresh"}); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if out.String() != "0\x00--x\x00--fresh\x00" {
		t.Fatalf("stdout %q", out.String())
	}
}

// E8 c: a bare --resume or -r (the picker, Q70) is left as it is.
func TestSeatResumePickerIsUnchanged(t *testing.T) {
	for _, words := range [][]string{{"--resume"}, {"-r"}, {"--resume", "--model", "x"}, {"--resume", "a search"}} {
		got := resumeRun(t, true, words...)
		wantArgs(t, got, len(words), words...)
	}
	got := resumeRun(t, true, "--rebind", "--resume")
	if got.code != term.ExitUsage || !strings.Contains(got.stderr, "--rebind goes with --resume <id>") {
		t.Errorf("--rebind with the picker: exit %d, %q", got.code, got.stderr)
	}
}

// E8 d: --resume <id> works as before with the recorded id or no record; another id is refused
// unless --rebind (or a relaunch of the session's own conversation).
func TestSeatResumeExplicitID(t *testing.T) {
	for _, words := range [][]string{{"--resume", seatConvID}, {"--resume=" + seatConvID}, {"-r", seatConvID}} {
		wantArgs(t, resumeRun(t, true, words...), len(words), words...)
	}
	wantArgs(t, resumeRun(t, false, "--resume", otherConvID), 2, "--resume", otherConvID)
	got := resumeRun(t, true, "--resume", otherConvID)
	if got.code != term.ExitUsage {
		t.Fatalf("another id: exit %d", got.code)
	}
	wantSays(t, got.stderr, otherConvID, "dev1@team-a: conversation "+seatConvID, "    ccy --teams dev1@team-a\n", "--rebind")
	got = resumeRun(t, true, "--rebind", "--resume", otherConvID)
	wantArgs(t, got, 2, "--resume", otherConvID)
	wantSays(t, got.stderr, "rebinding dev1@team-a")
	got = resumeRun(t, true, flagRelaunch, "--resume", otherConvID)
	wantArgs(t, got, 2, "--resume", otherConvID)
	wantSays(t, got.stderr, "rebinding dev1@team-a")
}

// 4b: a repair wins over a stale relaunch: a relaunch of the session's own conversation is
// refused when a seat it holds was pointed elsewhere by --set after the session's record.
func TestSeatResumeRelaunchYieldsToANewerRepair(t *testing.T) {
	checkout := t.TempDir()
	plantV1Seat(t, checkout, "ccy-proj", "dev1", "team-a", seatConvID)
	plantV1Seat(t, checkout, "ccy-other", "ops", "team-a", otherConvID)
	set, _, setErr := seatConversation(checkout)
	if code := set.Run([]string{"dev1@team-a", "--set", otherConvID}); code != term.ExitOK {
		t.Fatalf("--set: exit %d: %s", code, setErr.String())
	}
	r, out, errOut := seatResume(checkout)
	code := r.Run([]string{"dev1@team-a", flagRelaunch, "2", "--", "--resume", seatConvID})
	if code != term.ExitUsage || out.Len() != 0 {
		t.Fatalf("relaunch: exit %d, stdout %q, stderr %q", code, out.String(), errOut.String())
	}
	wantSays(t, errOut.String(), "dev1@team-a", otherConvID, "    ccy --teams dev1@team-a\n")
	// A relaunch whose own record is newer than the repair resumes as before.
	record := filepath.Join(checkout, ".claude/ccy/pingbus/sessions/ccy-proj/conversation.json")
	if err := os.WriteFile(record, []byte(`{"v":1,"session":"ccy-proj","session_id":"`+seatConvID+
		`","source":"resume","written_ms":1799000000000}`), 0o600); err != nil {
		t.Fatal(err)
	}
	r, _, errOut = seatResume(checkout)
	if code := r.Run([]string{"dev1@team-a", flagRelaunch, "2", "--", "--resume", seatConvID}); code != term.ExitOK {
		t.Errorf("a newer relaunch: exit %d: %s", code, errOut.String())
	}
}

// A launch saves each held seat's pointer (the plain lookup does not), so the seat keeps its
// conversation when the launch reuses its session's folder.
func TestSeatResumeLaunchSavesThePointer(t *testing.T) {
	checkout := t.TempDir()
	plantV1Seat(t, checkout, "ccy-proj", "dev1", "team-a", seatConvID)
	pointer := filepath.Join(checkout, ".claude/ccy/seat-conversations", "dev1@team-a"+".json")
	look, _, lookErr := seatConversation(checkout)
	if code := look.Run([]string{"dev1@team-a"}); code != term.ExitOK {
		t.Fatalf("lookup: exit %d: %s", code, lookErr.String())
	}
	if _, err := os.Lstat(pointer); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a plain lookup wrote the pointer: %v", err)
	}
	r, _, errOut := seatResume(checkout)
	if code := r.Run([]string{"qa@team-b", "0", "--"}); code != term.ExitUsage {
		t.Fatalf("qa@team-b: exit %d: %s", code, errOut.String())
	}
	if data, err := os.ReadFile(pointer); err != nil || !strings.Contains(string(data), seatConvID) {
		t.Errorf("the launch did not save dev1@team-a's pointer: %s, %v", data, err)
	}
}

// E8 e: --session-id is an explicit choice of conversation, left as it is.
func TestSeatResumeSessionIDIsLeftAlone(t *testing.T) {
	for _, words := range [][]string{{"--session-id", otherConvID}, {"--session-id=" + otherConvID}} {
		wantArgs(t, resumeRun(t, true, words...), len(words), words...)
	}
}

// E8 g: --rebind needs --resume <id>; a relaunch with no resume resumes the record.
func TestSeatResumeRebindAlone(t *testing.T) {
	got := resumeRun(t, true, "--rebind")
	if got.code != term.ExitUsage || !strings.Contains(got.stderr, "--rebind goes with --resume <id>") {
		t.Errorf("exit %d, stderr %q", got.code, got.stderr)
	}
	wantArgs(t, resumeRun(t, true, flagRelaunch), 0, "--resume", seatConvID)
}

func TestSeatResumeUsage(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"--"},
		{"dev1@team-a", "--"},
		{"dev1@team-a", "x", "--"},
		{"dev1@team-a", "-1", "--"},
		{"dev1@team-a", "2", "--", "a"},
		{"dev1@team-a", "--bogus", "0", "--"},
		{"dev1@team-a", "0", "x"},
	} {
		r, out, errOut := seatResume(t.TempDir())
		if code := r.Run(args); code != term.ExitUsage || out.Len() != 0 || !strings.Contains(errOut.String(), "internal seat-resume <seats> [--relaunch] <before> -- <claude arguments...>") {
			t.Errorf("%q: exit %d, stdout %q, stderr %q", args, code, out.String(), errOut.String())
		}
	}
	r, _, errOut := seatResume(t.TempDir())
	if code := r.Run([]string{"a@b@c", "0", "--"}); code != term.ExitUsage || !strings.Contains(errOut.String(), "a@b@c") {
		t.Errorf("a malformed list: exit %d, %q", code, errOut.String())
	}
}

// `ccy-teams internal seat-resume` is dispatched to it, its arguments on the payload stream.
func TestDispatchSeatResume(t *testing.T) {
	h := newHarness(t)
	checkout := t.TempDir()
	plantV1Seat(t, checkout, "ccy-proj", "qa", "team-b", seatConvID)
	h.app.SeatResume.Getwd = func() (string, error) { return checkout, nil }
	if code := h.app.Run([]string{"internal", "seat-resume", "qa@team-b", "0", "--"}); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	if h.Out.String() != "0\x00--resume\x00"+seatConvID+"\x00" {
		t.Fatalf("stdout %q", h.Out.String())
	}
}
