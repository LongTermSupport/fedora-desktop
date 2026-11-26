package sessions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/conversation"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/seatcheckouts"
)

// seatWorld is a checkout with teamchat pool seats, listed in seat-checkouts.
type seatWorld struct {
	h        *harness
	checkout string
	roles    map[string]string // handle -> role, the team's status record
}

func newSeatWorld(t *testing.T, h *harness) *seatWorld {
	t.Helper()
	w := &seatWorld{h: h, checkout: realDir(t, h, "repo"), roles: map[string]string{}}
	regdir, err := registry.Dir(h.app.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	if err := seatcheckouts.Of(registry.New(regdir)).Add(w.checkout); err != nil {
		t.Fatal(err)
	}
	h.app.TeamStatus = func(paths.TeamID) (status.Record, bool, error) {
		var rec status.Record
		for handle, role := range w.roles {
			rec.Members = append(rec.Members, status.Member{Handle: handle, Role: role})
		}
		return rec, len(w.roles) > 0, nil
	}
	return w
}

// seat places pool seat handle of team alpha in the checkout.
func (w *seatWorld) seat(t *testing.T, handle string) {
	t.Helper()
	state, err := paths.StateHome(w.h.app.Getenv("HOME"), w.h.app.Getenv("XDG_STATE_HOME"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := paths.SeatStore(state)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(w.checkout)
	if err != nil {
		t.Fatal(err)
	}
	key, err := paths.CheckoutKeyOf(resolved)
	if err != nil {
		t.Fatal(err)
	}
	id, err := paths.ParseTeamID("alpha")
	if err != nil {
		t.Fatal(err)
	}
	name, err := paths.ParseSeatName(handle)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(store.PoolSeat(key, id, name).Dir(), 0o700); err != nil {
		t.Fatal(err)
	}
}

// ran records seat@team's conversation id here; with prompt, its transcript too.
func (w *seatWorld) ran(t *testing.T, seat, id, prompt string) {
	t.Helper()
	if err := conversation.RecordSeats(w.checkout, seat, id, conversation.SourceExit, w.h.clock.UnixMilli()-2*60*60*1000); err != nil {
		t.Fatal(err)
	}
	if prompt == "" {
		return
	}
	line := `{"type":"user","message":{"role":"user","content":"` + prompt + `"}}` + "\n"
	w.h.write(strings.TrimPrefix(filepath.Join(w.checkout, ".claude/ccy/projects/-workspace", id+".jsonl"), w.h.root+"/"), line)
}

const (
	seatConvA = "aaaaaaaa-0000-4000-8000-000000000000"
	seatConvB = "bbbbbbbb-0000-4000-8000-000000000000"
)

// Plan 00265 R89 (design section 3): after the records, the restore starts each pool seat with
// a conversation that no running session holds and no record covers, and says it in words; a
// seat whose transcript is gone, or a host seat, is named as not started with the way on. The
// dry run says the same and starts nothing.
func TestRestoreStartsEverySeatNoRecordCovers(t *testing.T) {
	h := newHarness(t)
	restoreHost(h)
	w := newSeatWorld(t, h)
	w.seat(t, "repo.1+site.podman")
	w.seat(t, "repo.2+site.podman")
	w.seat(t, "repo.3+site.podman")
	w.seat(t, "repo.4+site.host")
	w.roles["repo.1+site.podman"] = "coordinator"
	w.roles["repo.2+site.podman"] = "worker"
	w.ran(t, "1@alpha", seatConvA, "Plan the release")
	w.ran(t, "2@alpha", seatConvB, "")
	w.ran(t, "4@alpha", seatConvB, "")

	exitCodeIs(t, h.run("restore", "--dry-run"), 0, h)
	contains(t, "dry run", h.stdout(),
		`would start the coordinator of alpha in `+w.checkout+`, resuming "Plan the release" (last ran 2 hours ago)`,
		"would not start a worker of alpha in "+w.checkout+": its conversation's transcript is gone; run ccy --teams alpha there to choose",
		"would not start a host seat of alpha in "+w.checkout+": the restore starts ccy seats only; run cc --teams alpha there")
	lacks(t, "dry run", h.stdout()+h.stderr(), "1@alpha", "seat 1", "a seat of alpha in") // seat 3 never ran
	if n := h.count("systemd-run"); n != 0 {
		t.Fatalf("the dry run started %d sessions", n)
	}

	h.out.Reset()
	h.errOut.Reset()
	exitCodeIs(t, h.run("restore"), 0, h)
	if n := h.count("systemd-run"); n != 1 {
		t.Fatalf("started %d sessions, want the coordinator's alone:\n%s", n, strings.Join(h.ran(), "\n"))
	}
	regdir, err := registry.Dir(h.app.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	name := "ccy-alpha-" + projectName(w.checkout) + "-coordinator"
	rec, err := registry.New(regdir).Get(name)
	if err != nil {
		t.Fatalf("the seat's session has no record: %v", err)
	}
	if rec.Dir != w.checkout || rec.Prefix != "ccy" || !rec.Restore || !strings.Contains(strings.Join(rec.Args, " "), "--teams 1@alpha") {
		t.Fatalf("record %+v", rec)
	}
	contains(t, "restore", h.stderr(), "starting the coordinator of alpha in "+w.checkout+`, resuming "Plan the release"`,
		"restored "+name+" in "+w.checkout)

	// The record now covers the seat: a second restore, with that session running, starts nothing new.
	h.sessions(name + " 0 " + w.checkout)
	before := h.count("systemd-run")
	exitCodeIs(t, h.run("restore"), 0, h)
	if h.count("systemd-run") != before {
		t.Fatal("a seat a record covers was started again")
	}
}

// A restore of named sessions is about those records alone: no seat is started.
func TestRestoreOfNamedSessionsStartsNoSeat(t *testing.T) {
	h := newHarness(t)
	restoreHost(h)
	w := newSeatWorld(t, h)
	w.seat(t, "repo.1+site.podman")
	w.ran(t, "1@alpha", seatConvA, "Plan the release")
	a := realDir(t, h, "a")
	record(t, h, registry.Record{Name: "ccy-a", Dir: a, Launcher: "/launch/ccy", Prefix: "ccy", Restore: true})
	exitCodeIs(t, h.run("restore", "--dry-run", "ccy-a"), 0, h)
	lacks(t, "named restore", h.stdout(), "coordinator", "of alpha")
}
