package containername

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

type rig struct {
	*teamtest.World

	claim   *Claim
	runtime string
	proc    string
}

func newRig(t *testing.T) rig {
	t.Helper()
	w := teamtest.New(t)
	runtime := filepath.Join(t.TempDir(), "run-user")
	if err := os.Mkdir(runtime, 0o700); err != nil {
		t.Fatal(err)
	}
	w.Env["XDG_RUNTIME_DIR"] = runtime
	proc := t.TempDir()
	c := New(w.Host)
	c.ProcRoot = proc
	r := rig{World: w, claim: c, runtime: runtime, proc: proc}
	r.containers("")
	return r
}

// sibling is another ccy-teams run on the same machine: a fresh host over r's runtime
// directory and /proc.
func (r rig) sibling(t *testing.T) rig {
	t.Helper()
	w := teamtest.New(t)
	w.Env["XDG_RUNTIME_DIR"] = r.runtime
	c := New(w.Host)
	c.ProcRoot = r.proc
	return rig{World: w, claim: c, runtime: r.runtime, proc: r.proc}
}

// alive makes process pid exist in the fake /proc, started at start.
func (r rig) alive(t *testing.T, pid int, start int64) {
	t.Helper()
	teamtest.MustWrite(t, filepath.Join(r.proc, strconv.Itoa(pid), "stat"),
		fmt.Sprintf("%d (ba sh) S %s%d 0 0\n", pid, strings.Repeat("0 ", 18), start))
}

func (r rig) dead(t *testing.T, pid int) {
	t.Helper()
	if err := os.RemoveAll(filepath.Join(r.proc, strconv.Itoa(pid))); err != nil {
		t.Fatal(err)
	}
}

// containers is what `podman ps -a --format {{.Names}}` lists.
func (r rig) containers(names string) {
	r.On("podman ps -a --format {{.Names}}", names, 0)
}

func (r rig) reservations() string { return filepath.Join(r.runtime, "ccy", "container-names") }

func (r rig) run(t *testing.T, args ...string) (term.ExitCode, string) {
	t.Helper()
	r.Out.Reset()
	r.ErrOut.Reset()
	code := r.claim.Run(t.Context(), args)
	return code, r.Out.String()
}

func mustClaim(t *testing.T, r rig, project, owner string) string {
	t.Helper()
	code, out := r.run(t, project, "yolo", owner)
	if code != term.ExitOK {
		t.Fatalf("claim for %s exit %d: %s", owner, code, r.ErrOut.String())
	}
	return strings.TrimSuffix(out, "\n")
}

func TestNoContainerAndNoClaimGivesTheBaseNameAndRecordsTheOwner(t *testing.T) {
	r := newRig(t)
	r.alive(t, 101, 5000)
	code, out := r.run(t, "proj", "yolo", "101")
	if code != term.ExitOK || out != "proj_yolo\n" {
		t.Fatalf("exit %d out %q err %q", code, out, r.ErrOut.String())
	}
	data, err := os.ReadFile(filepath.Join(r.reservations(), "proj_yolo"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "101 5000\n" {
		t.Fatalf("reservation %q", data)
	}
	info, err := os.Lstat(r.reservations())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 || !info.IsDir() {
		t.Fatalf("reservations dir mode %v", info.Mode())
	}
	if r.ErrOut.Len() != 0 {
		t.Fatalf("stderr %q", r.ErrOut.String())
	}
}

func TestExistingContainersAreSkippedToTheLowestFreeSuffix(t *testing.T) {
	r := newRig(t)
	r.alive(t, 101, 1)
	r.containers("proj_yolo\nproj_yolo_1\nproj_yolo_3\nother_yolo\nproj_yolo_x\n")
	if got := mustClaim(t, r, "proj", "101"); got != "proj_yolo_2" {
		t.Fatalf("got %q", got)
	}
}

func TestAnotherLiveLaunchersClaimIsSkipped(t *testing.T) {
	r := newRig(t)
	r.alive(t, 101, 1)
	r.alive(t, 102, 2)
	r.alive(t, 103, 3)
	got := []string{mustClaim(t, r, "proj", "101"), mustClaim(t, r, "proj", "102"), mustClaim(t, r, "proj", "103")}
	if want := []string{"proj_yolo", "proj_yolo_1", "proj_yolo_2"}; !slices.Equal(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestAClaimWhoseLauncherHasEndedIsFreeAgainAndItsFileReplaced(t *testing.T) {
	r := newRig(t)
	r.alive(t, 101, 1)
	r.alive(t, 102, 2)
	mustClaim(t, r, "proj", "101")
	r.dead(t, 101)
	if got := mustClaim(t, r, "proj", "102"); got != "proj_yolo" {
		t.Fatalf("got %q", got)
	}
	data, err := os.ReadFile(filepath.Join(r.reservations(), "proj_yolo"))
	if err != nil || string(data) != "102 2\n" {
		t.Fatalf("reservation %q %v", data, err)
	}
}

func TestAStaleClaimOfAnotherNameIsRemoved(t *testing.T) {
	r := newRig(t)
	r.alive(t, 101, 1)
	r.alive(t, 102, 2)
	r.containers("proj_yolo\n")
	mustClaim(t, r, "proj", "101")
	r.dead(t, 101)
	r.containers("")
	if got := mustClaim(t, r, "proj", "102"); got != "proj_yolo" {
		t.Fatalf("got %q", got)
	}
	if _, err := os.Lstat(filepath.Join(r.reservations(), "proj_yolo_1")); !os.IsNotExist(err) {
		t.Fatalf("the stale claim is still there: %v", err)
	}
}

func TestAReusedPidWithAnotherStartTimeDoesNotHoldTheClaim(t *testing.T) {
	r := newRig(t)
	r.alive(t, 101, 1)
	r.alive(t, 102, 2)
	mustClaim(t, r, "proj", "101")
	r.alive(t, 101, 999)
	if got := mustClaim(t, r, "proj", "102"); got != "proj_yolo" {
		t.Fatalf("got %q", got)
	}
}

func TestTheSameLauncherClaimingAgainGetsItsOwnNameBack(t *testing.T) {
	r := newRig(t)
	r.alive(t, 101, 1)
	r.alive(t, 102, 2)
	mustClaim(t, r, "other", "102")
	first := mustClaim(t, r, "proj", "101")
	second := mustClaim(t, r, "proj", "101")
	if first != "proj_yolo" || second != "proj_yolo" {
		t.Fatalf("got %q then %q", first, second)
	}
	if _, err := os.Lstat(filepath.Join(r.reservations(), "other_yolo")); err != nil {
		t.Fatalf("another launcher's claim went: %v", err)
	}
}

func TestConcurrentClaimsAllGetDistinctNames(t *testing.T) {
	r := newRig(t)
	const n = 8
	for i := range n {
		r.alive(t, 200+i, int64(i))
	}
	// Each launcher is its own ccy-teams run: its own host and streams, the same runtime
	// directory and /proc.
	launchers := make([]rig, n)
	for i := range launchers {
		launchers[i] = r.sibling(t)
		launchers[i].containers("proj_yolo\n")
	}
	codes := make([]term.ExitCode, n)
	var wg sync.WaitGroup
	for i, l := range launchers {
		wg.Go(func() {
			codes[i] = l.claim.Run(t.Context(), []string{"proj", "yolo", strconv.Itoa(200 + i)})
		})
	}
	wg.Wait()
	names := make([]string, 0, n)
	for i, code := range codes {
		if code != term.ExitOK {
			t.Fatalf("claim %d exit %d: %s", i, code, launchers[i].ErrOut.String())
		}
		names = append(names, strings.TrimSuffix(launchers[i].Out.String(), "\n"))
	}
	slices.Sort(names)
	want := []string{"proj_yolo_1", "proj_yolo_2", "proj_yolo_3", "proj_yolo_4", "proj_yolo_5", "proj_yolo_6", "proj_yolo_7", "proj_yolo_8"}
	if !slices.Equal(names, want) {
		t.Fatalf("got %v want %v", names, want)
	}
}

func TestTheEngineIsTheLaunchersContainerEngine(t *testing.T) {
	r := newRig(t)
	r.alive(t, 101, 1)
	r.Env["CONTAINER_ENGINE"] = "docker"
	r.On("docker ps -a --format {{.Names}}", "proj_yolo\n", 0)
	if got := mustClaim(t, r, "proj", "101"); got != "proj_yolo_1" {
		t.Fatalf("got %q", got)
	}
	if r.Fake.Called("podman") {
		t.Fatal("podman was asked")
	}
}

func TestAnEngineThatCannotListStopsTheClaim(t *testing.T) {
	r := newRig(t)
	r.alive(t, 101, 1)
	r.OnErr("podman ps", "Error: the engine is down", 125)
	code, out := r.run(t, "proj", "yolo", "101")
	if code != term.ExitFailure || out != "" {
		t.Fatalf("exit %d out %q", code, out)
	}
	if !strings.Contains(r.ErrOut.String(), "the engine is down") {
		t.Fatalf("stderr %q", r.ErrOut.String())
	}
	if _, err := os.Lstat(r.reservations()); !os.IsNotExist(err) {
		t.Fatalf("a claim was made: %v", err)
	}
}

func TestNoRuntimeDirectoryStopsTheClaim(t *testing.T) {
	for _, v := range []string{"", "run"} {
		r := newRig(t)
		r.alive(t, 101, 1)
		r.Env["XDG_RUNTIME_DIR"] = v
		code, out := r.run(t, "proj", "yolo", "101")
		if code != term.ExitFailure || out != "" || !strings.Contains(r.ErrOut.String(), "XDG_RUNTIME_DIR") {
			t.Fatalf("%q: exit %d out %q err %q", v, code, out, r.ErrOut.String())
		}
	}
}

func TestBadArgumentsAreUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{"proj", "yolo"},
		{"proj", "yolo", "101", "x"},
		{"../proj", "yolo", "101"},
		{".proj", "yolo", "101"},
		{"", "yolo", "101"},
		{"proj", "Yo/lo", "101"},
		{"proj", "yolo", "abc"},
		{"proj", "yolo", "0"},
		{"proj", "yolo", "-3"},
	} {
		r := newRig(t)
		code, out := r.run(t, args...)
		if code != term.ExitUsage || out != "" || !strings.Contains(r.ErrOut.String(), "claim-container-name") {
			t.Fatalf("%q: exit %d out %q err %q", args, code, out, r.ErrOut.String())
		}
	}
}

func TestAnOwnerThatIsNotRunningIsRefused(t *testing.T) {
	r := newRig(t)
	code, out := r.run(t, "proj", "yolo", "101")
	if code != term.ExitFailure || out != "" || !strings.Contains(r.ErrOut.String(), "process 101") {
		t.Fatalf("exit %d out %q err %q", code, out, r.ErrOut.String())
	}
}

func TestAnUnreadableClaimIsReportedNotIgnored(t *testing.T) {
	r := newRig(t)
	r.alive(t, 101, 1)
	teamtest.MustWrite(t, filepath.Join(r.reservations(), "proj_yolo"), "garbage")
	code, out := r.run(t, "proj", "yolo", "101")
	if code != term.ExitFailure || out != "" || !strings.Contains(r.ErrOut.String(), "proj_yolo") {
		t.Fatalf("exit %d out %q err %q", code, out, r.ErrOut.String())
	}
}

func TestASymlinkInPlaceOfTheClaimsDirectoryIsRefused(t *testing.T) {
	r := newRig(t)
	r.alive(t, 101, 1)
	if err := os.MkdirAll(filepath.Join(r.runtime, "ccy"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), r.reservations()); err != nil {
		t.Fatal(err)
	}
	code, out := r.run(t, "proj", "yolo", "101")
	if code != term.ExitFailure || out != "" || !strings.Contains(r.ErrOut.String(), "not a directory") {
		t.Fatalf("exit %d out %q err %q", code, out, r.ErrOut.String())
	}
}
