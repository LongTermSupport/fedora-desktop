package chooser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// S-E4: the Element consent sits inside the flow, for a host seat only, after the seats are
// chosen and before the list is printed, and the record is written as pingbus reads it.

const (
	hostChoose = "CHOICE\t1\tfree\tproj.1+local.host\tdesktop (host)\nDEFAULT\t1\n"
	acceptedAt = "2026-10-15T12:30:00Z"
)

// accepted is where the owner's acceptance lives under the test's home.
type accepted struct{ dir, record string }

func (r *rig) consentRig() accepted {
	r.c.Now = func() time.Time { return time.Date(2026, 10, 15, 12, 30, 0, 0, time.UTC) }
	dir := filepath.Join(r.Env["HOME"], ".config", "pingbus")
	return accepted{dir: dir, record: filepath.Join(dir, "element-accepted")}
}

func (a accepted) exists() bool {
	_, err := os.Lstat(a.record)
	return err == nil
}

func (r *rig) hostSeat() {
	r.On("agent-bus seat choose alpha --type=host", hostChoose, 0)
}

func TestAHostSeatAsksForTheElementConsentAndRecordsIt(t *testing.T) {
	r := newRig(t, alphaCovers)
	a := r.consentRig()
	r.hostSeat()
	r.Script.Gum = []string{"YES"}
	exit(t, r.run("--type=host", "1@alpha"), term.ExitOK, r)
	if r.payload.String() != "1@alpha\n" {
		t.Fatalf("payload %q", r.payload.String())
	}
	contains(t, "stderr", r.stderr(), "proj.1+local.host", "runs as you", "Element")
	// Security audit H2 and L1: the owner is told plainly that the rules are not a wall, that
	// ccy is the safe choice, and who can direct the agent.
	contains(t, "stderr", r.stderr(), "other routes", "ccy --teams is the safe choice",
		"cannot reach your Element sign-in",
		"Any human listed in a team can direct its agents as you can.")
	if len(r.Script.GumLog) != 1 || !strings.Contains(r.Script.GumLog[0], "--default=false") {
		t.Fatalf("the question must default to No: %v", r.Script.GumLog)
	}
	body, err := os.ReadFile(a.record)
	if err != nil || string(body) != "accepted "+acceptedAt+"\n" {
		t.Fatalf("record %q, %v", body, err)
	}
	for path, want := range map[string]os.FileMode{a.dir: 0o700, a.record: 0o600} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != want {
			t.Fatalf("%s mode %v (%v), want %v", path, info.Mode().Perm(), err, want)
		}
	}
	if r.Out.Len() != 0 {
		t.Fatalf("stdout %q", r.Out.String())
	}
}

func TestDecliningTheConsentStartsNothingAndRecordsNothing(t *testing.T) {
	r := newRig(t, alphaCovers)
	a := r.consentRig()
	r.hostSeat()
	r.Script.Gum = []string{"NO"}
	exit(t, r.run("--type=host", "1@alpha"), term.ExitFailure, r)
	contains(t, "stderr", r.stderr(), "Not started. Nothing was recorded; cc --teams asks again next time.")
	r.lacksOutput(t)
	if a.exists() {
		t.Fatal("a declined consent was recorded")
	}
}

func TestACancelledConsentIsADeclinedOne(t *testing.T) {
	r := newRig(t, alphaCovers)
	a := r.consentRig()
	r.hostSeat()
	r.Script.Gum = []string{"CANCEL"}
	exit(t, r.run("--type=host", "1@alpha"), term.ExitFailure, r)
	r.lacksOutput(t)
	if a.exists() {
		t.Fatal("a cancelled consent was recorded")
	}
}

func TestAnAcceptedConsentIsNotAskedAgainForAnySeat(t *testing.T) {
	r := newRig(t, alphaCovers)
	a := r.consentRig()
	r.hostSeat()
	if err := os.MkdirAll(a.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	teamtest.MustWrite(t, a.record, "accepted earlier\n")
	exit(t, r.run("--type=host", "1@alpha"), term.ExitOK, r)
	if len(r.Script.GumLog) != 0 {
		t.Fatalf("asked again: %v", r.Script.GumLog)
	}
	if r.payload.String() != "1@alpha\n" {
		t.Fatalf("payload %q", r.payload.String())
	}
}

func TestAPodmanSeatNeverAsksForTheElementConsent(t *testing.T) {
	r := newRig(t, alphaCovers)
	a := r.consentRig()
	exit(t, r.run("--type=podman", "1@alpha"), term.ExitOK, r)
	if len(r.Script.GumLog) != 0 {
		t.Fatalf("asked: %v", r.Script.GumLog)
	}
	if a.exists() {
		t.Fatal("a podman launch wrote the host record")
	}
}

func TestTheConsentNamesEveryHandleOfSeveralTeams(t *testing.T) {
	r := newRig(t, alphaCovers+betaWiden)
	r.consentRig()
	r.hostSeat()
	r.On("agent-bus seat choose beta --type=host", "CHOICE\t2\tfree\tproj.2+local.host\tdesktop (host)\nDEFAULT\t2\n", 0)
	r.Script.Gum = []string{"YES"}
	exit(t, r.run("--type=host", "1@alpha,2@beta"), term.ExitOK, r)
	contains(t, "stderr", r.stderr(), "proj.1+local.host", "proj.2+local.host")
	contains(t, "question", r.Script.GumLog[0], "proj.1+local.host", "proj.2+local.host")
	if r.payload.String() != "1@alpha,2@beta\n" {
		t.Fatalf("payload %q", r.payload.String())
	}
}

func TestTheConsentIsAskedAfterTheListIsChosen(t *testing.T) {
	r := newRig(t, alphaCovers)
	a := r.consentRig()
	r.hostSeat()
	r.Script.Gum = []string{"YES"}
	exit(t, r.run("--type=host"), term.ExitOK, r)
	if len(r.seatsAsked) != 1 || r.seatsAsked[0] != "alpha/host" {
		t.Fatalf("seats %v", r.seatsAsked)
	}
	if !a.exists() {
		t.Fatal("no record")
	}
	if r.payload.String() != "1@alpha\n" {
		t.Fatalf("payload %q", r.payload.String())
	}
}

func TestASeatThatAgentBusDoesNotOfferCannotBeNamedInTheConsent(t *testing.T) {
	r := newRig(t, alphaCovers)
	a := r.consentRig()
	r.hostSeat()
	exit(t, r.run("--type=host", "9@alpha"), 78, r)
	contains(t, "stderr", r.stderr(), "9@alpha", "not one of the seats")
	r.lacksOutput(t)
	if a.exists() {
		t.Fatal("recorded")
	}
}

func TestARecordThatIsASymlinkIsRefusedAndNothingIsWrittenThroughIt(t *testing.T) {
	r := newRig(t, alphaCovers)
	a := r.consentRig()
	r.hostSeat()
	elsewhere := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.MkdirAll(a.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, a.record); err != nil {
		t.Fatal(err)
	}
	exit(t, r.run("--type=host", "1@alpha"), 78, r)
	contains(t, "stderr", r.stderr(), a.record)
	if _, err := os.Lstat(elsewhere); err == nil {
		t.Fatal("written through the symlink")
	}
	r.lacksOutput(t)
}

func TestAFolderThatIsASymlinkIsRefused(t *testing.T) {
	r := newRig(t, alphaCovers)
	a := r.consentRig()
	r.hostSeat()
	target := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(a.dir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, a.dir); err != nil {
		t.Fatal(err)
	}
	exit(t, r.run("--type=host", "1@alpha"), 78, r)
	if entries, err := os.ReadDir(target); err != nil || len(entries) != 0 {
		t.Fatalf("written through the folder link: %v %v", entries, err)
	}
}

// recorded makes an accepted record with the given modes, as pingbus would find it.
func (a accepted) recorded(t *testing.T, dirMode, fileMode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(a.dir, dirMode); err != nil {
		t.Fatal(err)
	}
	teamtest.MustWrite(t, a.record, "accepted earlier\n")
	for path, mode := range map[string]os.FileMode{a.dir: dirMode, a.record: fileMode} {
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
	}
}

// pingbus (config.element_accepted) counts a record only if it is the owner's, owner-only, in a
// folder the owner has; the Go reader holds the same rule so no seat is made for a record
// pingbus would refuse.
func TestARecordOpenToOthersIsRefusedWithHowToFixIt(t *testing.T) {
	r := newRig(t, alphaCovers)
	a := r.consentRig()
	r.hostSeat()
	a.recorded(t, 0o700, 0o644)
	exit(t, r.run("--type=host", "1@alpha"), 78, r)
	contains(t, "stderr", r.stderr(), a.record, "chmod 600", "remove it")
	r.lacksOutput(t)
	if len(r.Script.GumLog) != 0 {
		t.Fatalf("asked: %v", r.Script.GumLog)
	}
}

func TestARecordOfAnotherOwnerIsRefused(t *testing.T) {
	r := newRig(t, alphaCovers)
	a := r.consentRig()
	r.hostSeat()
	a.recorded(t, 0o700, 0o600)
	r.c.UID = func() int { return os.Getuid() + 1 }
	exit(t, r.run("--type=host", "1@alpha"), 78, r)
	contains(t, "stderr", r.stderr(), a.dir, "owner")
	r.lacksOutput(t)
}

func TestAFolderOpenToOthersIsRefusedEvenWithAGoodRecord(t *testing.T) {
	r := newRig(t, alphaCovers)
	a := r.consentRig()
	r.hostSeat()
	a.recorded(t, 0o755, 0o600)
	exit(t, r.run("--type=host", "1@alpha"), 78, r)
	contains(t, "stderr", r.stderr(), a.dir, "chmod 700")
	r.lacksOutput(t)
}

// With no terminal nobody can be asked: Go refuses before any seat is made, whatever the list.
func TestNoRecordAndNoTerminalRefusesBeforeAnySeatIsTaken(t *testing.T) {
	r := newRig(t, alphaCovers)
	a := r.consentRig()
	r.hostSeat()
	r.Host.IsTerminal = func(any) bool { return false }
	exit(t, r.run("--type=host", "1@alpha"), 78, r)
	contains(t, "stderr", r.stderr(), "terminal", "cc --teams")
	r.lacksOutput(t)
	if a.exists() {
		t.Fatal("recorded")
	}
}

func TestARecordAnswersWithoutATerminalForAFullList(t *testing.T) {
	r := newRig(t, alphaCovers)
	a := r.consentRig()
	a.recorded(t, 0o700, 0o600)
	r.Host.IsTerminal = func(any) bool { return false }
	exit(t, r.run("--type=host", "1@alpha"), term.ExitOK, r)
	if r.payload.String() != "1@alpha\n" {
		t.Fatalf("payload %q", r.payload.String())
	}
}

func TestATeamAloneStillNeedsATerminal(t *testing.T) {
	r := newRig(t, alphaCovers)
	r.Host.IsTerminal = func(any) bool { return false }
	exit(t, r.run("--type=host", "alpha"), term.ExitUsage, r)
	r.lacksOutput(t)
}

func TestWithoutGumTheConsentCannotBeAsked(t *testing.T) {
	r := newRig(t, alphaCovers)
	a := r.consentRig()
	r.hostSeat()
	lookPath := r.Host.LookPath
	r.Host.LookPath = func(name string) (string, error) {
		if name == "gum" {
			return "", os.ErrNotExist
		}
		return lookPath(name)
	}
	exit(t, r.run("--type=host", "1@alpha"), 78, r)
	contains(t, "stderr", r.stderr(), "gum", "play-claude-yolo.yml")
	if a.exists() {
		t.Fatal("recorded")
	}
}
