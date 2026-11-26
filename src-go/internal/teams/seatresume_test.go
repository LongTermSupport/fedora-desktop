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

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/conversation"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const (
	otherConvID = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	freshConvID = "11111111-2222-4333-8444-555555555555"
)

// plantTranscript writes the ccy transcript of sessionID in checkout.
func plantTranscript(t *testing.T, checkout, sessionID string) {
	t.Helper()
	path := filepath.Join(checkout, ".claude/ccy/projects/-workspace", sessionID+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func plantSeatRecord(t *testing.T, checkout, list, sessionID string) {
	t.Helper()
	if err := conversation.RecordSeats(checkout, list, sessionID, conversation.SourceLaunch, 1); err != nil {
		t.Fatal(err)
	}
}

func noHome(k string) string {
	if k == "HOME" {
		return "/nonexistent"
	}
	return ""
}

func seatResume(checkout string) (*SeatResume, *bytes.Buffer, *bytes.Buffer) {
	var out, errOut bytes.Buffer
	return &SeatResume{
		Stdout: &out, Stderr: &errOut, Getenv: noHome,
		Getwd: func() (string, error) { return checkout, nil },
		NowMS: func() int64 { return 1793000000000 },
		NewID: func() (string, error) { return freshConvID, nil },
	}, &out, &errOut
}

// recordedID is the id the seat records of list name in checkout (ForSeats').
func recordedID(t *testing.T, checkout, list string) string {
	t.Helper()
	got, err := conversation.ForSeats(checkout, list, conversation.Here(noHome))
	if err != nil {
		t.Fatalf("the seats' record: %v", err)
	}
	return got.ID
}

func words(out *bytes.Buffer) string { return strings.ReplaceAll(out.String(), "\x00", "|") }

// launched is what one run of the verb printed: the claude arguments it added after the ones it
// was given (less --fresh and --rebind, which it takes out), each with "|", and its stderr.
type launched struct{ out, err string }

// run is the verb over args, failing the test unless it exits want. A printed answer must be the
// count and the given claude arguments, less --fresh and --rebind, before what it adds.
func run(t *testing.T, checkout string, want term.ExitCode, args ...string) launched {
	t.Helper()
	r, out, errOut := seatResume(checkout)
	if code := r.Run(args); code != want {
		t.Fatalf("%q: exit %d, want %d: %s", args, code, want, errOut.String())
	}
	if out.Len() == 0 {
		return launched{err: errOut.String()}
	}
	var kept []string
	if sep := slices.Index(args, "--"); sep >= 0 {
		kept = slices.DeleteFunc(slices.Clone(args[sep+1:]), func(a string) bool { return a == "--fresh" || a == "--rebind" })
	}
	printed := strings.Split(strings.TrimSuffix(out.String(), "\x00"), "\x00")
	head := append([]string{strconv.Itoa(len(kept))}, kept...)
	if len(printed) < len(head) || !slices.Equal(printed[:len(head)], head) {
		t.Fatalf("%q printed %q, want it to start %q", args, printed, head)
	}
	added := ""
	for _, w := range printed[len(head):] {
		added += w + "|"
	}
	return launched{out: added, err: errOut.String()}
}

// ccy's --fresh and --rebind reach claude's arguments through the launcher's parse: they are
// taken out of those before its `--` only, and the count of those is printed first.
func TestSeatResumeTakesCcysFlagsOutOfClaudesArguments(t *testing.T) {
	checkout := t.TempDir()
	r, out, errOut := seatResume(checkout)
	if code := r.Run([]string{"--before=2", "1@team-a", "--", "--model", "--fresh", "--rebind", "x"}); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if words(out) != "1|--model|--rebind|x|--session-id|"+freshConvID+"|" {
		t.Fatalf("stdout %q", words(out))
	}
	for _, before := range []string{"--before=9", "--before=-1", "--before=x"} {
		r, out, errOut = seatResume(checkout)
		if code := r.Run([]string{before, "1@team-a", "--", "a"}); code != term.ExitUsage || out.Len() != 0 {
			t.Errorf("%s: exit %d, stdout %q: %s", before, code, out.String(), errOut.String())
		}
	}
}

// Issue 417 point 1: a plain seated launch resumes the seats' recorded conversation.
func TestSeatResumeAPlainLaunchResumesTheSeatsConversation(t *testing.T) {
	checkout := t.TempDir()
	plantTranscript(t, checkout, seatConvID)
	plantSeatRecord(t, checkout, "1@team-a", seatConvID)
	got := run(t, checkout, term.ExitOK, "--relaunch=", "1@team-a,2@team-b", "--", "--model", "opus")
	if got.out != "--resume|"+seatConvID+"|" {
		t.Fatalf("stdout %q", got.out)
	}
	if !strings.Contains(got.err, seatConvID) {
		t.Errorf("stderr %q does not say what is resumed", got.err)
	}
	if id := recordedID(t, checkout, "2@team-b"); id != seatConvID {
		t.Errorf("the unrecorded seat was recorded with %q", id)
	}
}

// The records written before #417 (the session's) still answer, and the seats are then recorded.
func TestSeatResumeCarriesASessionRecordIntoTheSeats(t *testing.T) {
	checkout := t.TempDir()
	plantV1Seat(t, checkout, "ccy-proj", "dev1", "team-a", seatConvID)
	if got := run(t, checkout, term.ExitOK, "dev1@team-a", "--"); got.out != "--resume|"+seatConvID+"|" {
		t.Fatalf("stdout %q", got.out)
	}
	if _, err := os.Stat(filepath.Join(checkout, ".claude/ccy/seat-conversations/team-a/dev1.json")); err != nil {
		t.Fatalf("the seat was not recorded: %v", err)
	}
}

// Issue 417 point 2: with none to name, exit 64 naming the case, the repair and --fresh.
func TestSeatResumeRefusesWithNoRecordNamingRepairAndFresh(t *testing.T) {
	cases := []struct {
		name, list, says string
		plant            func(t *testing.T, checkout string)
	}{
		{"no session", "dev1@team-a", "no session record", func(*testing.T, string) {}},
		{"no conversation", "dev3@team-c", "ccy-proj-2", func(t *testing.T, c string) {
			t.Helper()
			plantV1Seat(t, c, "ccy-proj-2", "dev3", "team-c", "")
		}},
		{"ambiguous", "dev1@team-a,dev2@team-b", otherConvID, func(t *testing.T, c string) {
			t.Helper()
			plantTranscript(t, c, seatConvID)
			plantTranscript(t, c, otherConvID)
			plantSeatRecord(t, c, "dev1@team-a", seatConvID)
			plantSeatRecord(t, c, "dev2@team-b", otherConvID)
		}},
		{"no transcript", "dev1@team-a", seatConvID + ".jsonl", func(t *testing.T, c string) {
			t.Helper()
			plantSeatRecord(t, c, "dev1@team-a", seatConvID)
		}},
	}
	for _, c := range cases {
		checkout := t.TempDir()
		c.plant(t, checkout)
		got := run(t, checkout, term.ExitUsage, c.list, "--")
		launch := membership.Launch("ccy", c.list)
		for _, want := range []string{c.says, launch + " --resume <id> --rebind", launch + " --fresh"} {
			if !strings.Contains(got.err, want) {
				t.Errorf("%s: stderr %q does not say %q", c.name, got.err, want)
			}
		}
		if got.out != "" {
			t.Errorf("%s: stdout %q", c.name, got.out)
		}
	}
}

// Issue 423 with 417: a seat the person picked in the chooser (not in the list as given, --given)
// that no session ever used starts fresh, said, and is recorded; a picked seat with a record
// resumes it; a seat named in the given list keeps the refusal; and a picked seat whose record is
// in trouble (not never-used) is refused as before.
func TestSeatResumeAChosenNeverUsedSeatStartsFresh(t *testing.T) {
	checkout := t.TempDir()
	got := run(t, checkout, term.ExitOK, "--given=team-a", "3@team-a", "--")
	if got.out != "--session-id|"+freshConvID+"|" {
		t.Fatalf("a chosen never-used seat: stdout %q", got.out)
	}
	data, err := os.ReadFile(filepath.Join(checkout, ".claude/ccy/seat-conversations/team-a/3.json"))
	if !strings.Contains(got.err, "never used") || err != nil || !strings.Contains(string(data), freshConvID) {
		t.Fatalf("not said or not recorded: %q, record %s, %v", got.err, data, err)
	}
	if got := run(t, t.TempDir(), term.ExitOK, "--given=", "3@team-a", "--"); got.out != "--session-id|"+freshConvID+"|" {
		t.Fatalf("a bare --teams: stdout %q", got.out)
	}

	checkout = t.TempDir()
	plantTranscript(t, checkout, seatConvID)
	plantSeatRecord(t, checkout, "2@team-a", seatConvID)
	if got := run(t, checkout, term.ExitOK, "--given=team-a", "2@team-a", "--"); got.out != "--resume|"+seatConvID+"|" {
		t.Fatalf("a chosen seat with a record: stdout %q", got.out)
	}

	for _, given := range []string{"--given=3@team-a", "--given=3@team-a,team-b"} {
		got := run(t, t.TempDir(), term.ExitUsage, given, "3@team-a,4@team-b", "--")
		if !strings.Contains(got.err, "--fresh") {
			t.Errorf("%s: a named seat with no record: %q", given, got.err)
		}
	}
	run(t, t.TempDir(), term.ExitUsage, "3@team-a", "--")

	checkout = t.TempDir()
	plantSeatRecord(t, checkout, "3@team-a", seatConvID)
	if got := run(t, checkout, term.ExitUsage, "--given=team-a", "3@team-a", "--"); !strings.Contains(got.err, seatConvID+".jsonl") {
		t.Errorf("a chosen seat whose record has no transcript: %q", got.err)
	}
}

// --fresh starts a new conversation with an id ccy chooses and records, so the next plain launch
// resumes it.
func TestSeatResumeFreshStartsANewConversationAndRecordsIt(t *testing.T) {
	checkout := t.TempDir()
	plantTranscript(t, checkout, seatConvID)
	plantSeatRecord(t, checkout, "1@team-a", seatConvID)
	got := run(t, checkout, term.ExitOK, "--fresh", "1@team-a", "--")
	if got.out != "--session-id|"+freshConvID+"|" {
		t.Fatalf("stdout %q", got.out)
	}
	data, err := os.ReadFile(filepath.Join(checkout, ".claude/ccy/seat-conversations/team-a/1.json"))
	if err != nil || !strings.Contains(string(data), freshConvID) || !strings.Contains(string(data), `"source":"fresh"`) {
		t.Fatalf("record %s, %v", data, err)
	}
	if !strings.Contains(got.err, "--fresh") {
		t.Errorf("stderr %q does not name --fresh", got.err)
	}
}

// Issue 417 point 3: --resume <id> other than the record is refused without --rebind.
func TestSeatResumeRefusesAnotherIDWithoutRebind(t *testing.T) {
	checkout := t.TempDir()
	plantTranscript(t, checkout, seatConvID)
	plantSeatRecord(t, checkout, "1@team-a", seatConvID)
	got := run(t, checkout, term.ExitUsage, "1@team-a", "--", "--resume", otherConvID)
	for _, want := range []string{seatConvID, otherConvID, "--rebind", "ccy --teams 1@team-a"} {
		if !strings.Contains(got.err, want) {
			t.Errorf("stderr %q does not say %q", got.err, want)
		}
	}
	if got.out != "" || recordedID(t, checkout, "1@team-a") != seatConvID {
		t.Fatalf("stdout %q, or the record moved", got.out)
	}
	if got = run(t, checkout, term.ExitOK, "--rebind", "1@team-a", "--", "-r", otherConvID); got.out != "" {
		t.Fatalf("--rebind: stdout %q: %s", got.out, got.err)
	}
	plantTranscript(t, checkout, otherConvID)
	if id := recordedID(t, checkout, "1@team-a"); id != otherConvID {
		t.Fatalf("--rebind left the record at %s", id)
	}
}

// The same id, or no record at all, works as before and is recorded; a relaunch (a restore, a team
// restart, the supervisor's) resumes the session's own conversation and moves the record to it.
func TestSeatResumeAcceptsTheSameIDNoRecordAndARelaunch(t *testing.T) {
	checkout := t.TempDir()
	plantTranscript(t, checkout, seatConvID)
	if got := run(t, checkout, term.ExitOK, "1@team-a", "--", "--resume="+seatConvID); got.out != "" {
		t.Fatalf("no record: stdout %q: %s", got.out, got.err)
	}
	if recordedID(t, checkout, "1@team-a") != seatConvID {
		t.Fatal("a session joining a team did not carry its conversation into the seat")
	}
	run(t, checkout, term.ExitOK, "1@team-a", "--", "--resume", seatConvID)
	plantTranscript(t, checkout, otherConvID)
	run(t, checkout, term.ExitOK, "--relaunch=1", "1@team-a", "--", "--resume", otherConvID)
	if recordedID(t, checkout, "1@team-a") != otherConvID {
		t.Fatal("a relaunch did not move the record to the session's own conversation")
	}
}

// Register R4: a session that existed before it joined a team comes into its seat by the
// supervisor's relaunch with --resume <its id>, and no seat has a record yet. The seat records it.
func TestSeatResumeARelaunchIntoAnUnrecordedSeatRecordsIt(t *testing.T) {
	checkout := t.TempDir()
	plantTranscript(t, checkout, seatConvID)
	if got := run(t, checkout, term.ExitOK, "--relaunch=ccy-proj", "4@team-a", "--", "--resume", seatConvID); got.out != "" {
		t.Fatalf("stdout %q: %s", got.out, got.err)
	}
	data, err := os.ReadFile(filepath.Join(checkout, ".claude/ccy/seat-conversations/team-a/4.json"))
	if err != nil || !strings.Contains(string(data), seatConvID) || !strings.Contains(string(data), `"source":"relaunch"`) {
		t.Fatalf("record %s, %v", data, err)
	}
	if got := run(t, checkout, term.ExitOK, "4@team-a", "--"); got.out != "--resume|"+seatConvID+"|" {
		t.Fatalf("the next plain launch: %q", got.out)
	}
}

// Issue 417 point 4: the --continue refusal names the plain form (and --fresh), never an id.
func TestSeatResumeRefusesContinueNamingThePlainForm(t *testing.T) {
	for _, flag := range []string{"--continue", "-c"} {
		got := run(t, t.TempDir(), term.ExitUsage, "1@team-a", "--", flag)
		if got.out != "" {
			t.Fatalf("%s: stdout %q", flag, got.out)
		}
		for _, want := range []string{"refuses --continue", "ccy --teams team-a\n", "ccy --teams team-a --fresh"} {
			if !strings.Contains(got.err, want) {
				t.Errorf("%s: stderr %q does not say %q", flag, got.err, want)
			}
		}
		if strings.Contains(got.err, "--resume ") {
			t.Errorf("%s: the refusal still names an id form: %q", flag, got.err)
		}
	}
}

// Issue 417 point 6: the bare --resume picker is left alone; nothing is printed or recorded.
func TestSeatResumeLeavesThePickerAlone(t *testing.T) {
	checkout := t.TempDir()
	if got := run(t, checkout, term.ExitOK, "1@team-a", "--", "--resume", "--model", "opus"); got.out != "" {
		t.Fatalf("stdout %q: %s", got.out, got.err)
	}
	if _, err := os.Stat(filepath.Join(checkout, ".claude/ccy/seat-conversations")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the picker recorded something: %v", err)
	}
}

func TestSeatResumeRefusesWhatCannotBeMeant(t *testing.T) {
	for name, args := range map[string][]string{
		"no separator":        {"1@team-a"},
		"no list":             {"--", "--model"},
		"unknown flag":        {"--again", "1@team-a", "--"},
		"fresh with a resume": {"--fresh", "1@team-a", "--", "--resume", seatConvID},
		"fresh with continue": {"--fresh", "1@team-a", "--", "-c"},
		"rebind with no id":   {"--rebind", "1@team-a", "--"},
		"rebind with fresh":   {"--rebind", "--fresh", "1@team-a", "--"},
		"resume of a non-id":  {"1@team-a", "--", "--resume", "my-chat"},
		"a malformed list":    {"alpha", "--"},
		"two resumes":         {"1@team-a", "--", "--resume", seatConvID, "--session-id", otherConvID},
	} {
		if got := run(t, t.TempDir(), term.ExitUsage, args...); got.out != "" || got.err == "" {
			t.Errorf("%s: stdout %q, stderr %q", name, got.out, got.err)
		}
	}
}

func TestSeatResumeFailsWhenNoIDCanBeMade(t *testing.T) {
	r, out, errOut := seatResume(t.TempDir())
	r.NewID = func() (string, error) { return "", errors.New("no entropy") }
	if code := r.Run([]string{"--fresh", "1@team-a", "--"}); code != term.ExitFailure || out.Len() != 0 || !strings.Contains(errOut.String(), "no entropy") {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out.String(), errOut.String())
	}
}

func TestNewConversationIDIsAUUID(t *testing.T) {
	a, err := NewConversationID()
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewConversationID()
	if err != nil || a == b || len(a) != 36 || a[14] != '4' {
		t.Fatalf("ids %q %q, %v", a, b, err)
	}
}

// Issue 417's repair: --set points the seats at an id whose transcript is here.
func TestSeatConversationSetRecordsTheSeats(t *testing.T) {
	checkout := t.TempDir()
	c, out, errOut := seatConversation(checkout)
	if code := c.Run([]string{"1@team-a,2@team-b", "--set", seatConvID}); code != term.ExitFailure {
		t.Fatalf("no transcript: exit %d", code)
	}
	if !strings.Contains(errOut.String(), seatConvID+".jsonl") || out.Len() != 0 {
		t.Fatalf("stdout %q stderr %q", out.String(), errOut.String())
	}
	plantTranscript(t, checkout, seatConvID)
	c, out, errOut = seatConversation(checkout)
	if code := c.Run([]string{"1@team-a,2@team-b", "--set", seatConvID}); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if out.String() != seatConvID+"\n" || recordedID(t, checkout, "2@team-b") != seatConvID {
		t.Fatalf("stdout %q", out.String())
	}
	for _, args := range [][]string{{"1@team-a", "--set"}, {"1@team-a", "--set", "abc"}, {"1@team-a", "--sat", seatConvID}} {
		c, out, errOut = seatConversation(checkout)
		if code := c.Run(args); code != term.ExitUsage || out.Len() != 0 || errOut.Len() == 0 {
			t.Errorf("%q: exit %d, stdout %q", args, code, out.String())
		}
	}
}

// The query's refusal names the two ways on too.
func TestSeatConversationRefusalNamesTheWaysOn(t *testing.T) {
	c, out, errOut := seatConversation(t.TempDir())
	if code := c.Run([]string{"1@team-a"}); code != term.ExitFailure || out.Len() != 0 {
		t.Fatalf("exit %d, stdout %q", code, out.String())
	}
	if !strings.Contains(errOut.String(), "--resume <id> --rebind") || !strings.Contains(errOut.String(), "--fresh") {
		t.Fatalf("stderr %q", errOut.String())
	}
}

// `ccy-teams internal seat-resume` is dispatched to it, its words on the payload stream.
func TestDispatchSeatResume(t *testing.T) {
	h := newHarness(t)
	checkout := t.TempDir()
	plantTranscript(t, checkout, seatConvID)
	plantSeatRecord(t, checkout, "dev1@team-a", seatConvID)
	h.app.SeatResume.Getwd = func() (string, error) { return checkout, nil }
	h.app.SeatResume.Getenv = noHome
	if code := h.app.Run([]string{"internal", "seat-resume", "dev1@team-a", "--"}); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	if words(h.Out) != "0|--resume|"+seatConvID+"|" {
		t.Fatalf("stdout %q", words(h.Out))
	}
}
