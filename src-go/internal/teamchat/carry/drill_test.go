package carry

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
)

// drillWorld is a live team with every source SX2 names, and host.Serve as a fake that starts
// both units, or fails with serveErr.
type drillWorld struct {
	*exportWorld

	serveErr error
}

func newDrillWorld(t *testing.T) *drillWorld {
	t.Helper()
	return &drillWorld{exportWorld: newSourcesWorld(t)}
}

func (w *drillWorld) serve(_ context.Context, team paths.TeamID) error {
	w.effect("serve")
	if team != w.team {
		return errors.New("served another team")
	}
	if w.serveErr != nil {
		return w.serveErr
	}
	w.unit(paths.ServerUnit(team)).active = "active"
	w.unit(paths.TLSUnit(team)).active = "active"
	return nil
}

func (w *drillWorld) drillDeps() Deps {
	d := w.exportDeps()
	d.Serve = w.serve
	return d
}

func (w *drillWorld) drill() ([]byte, error) {
	return Drill(context.Background(), w.drillDeps(), w.team)
}

// drillRecord is drill.json, found false when there is none.
func (w *drillWorld) drillRecord() (DrillRecord, bool) {
	w.t.Helper()
	if !w.has(paths.DrillRecord(w.team)) {
		return DrillRecord{}, false
	}
	raw, err := os.ReadFile(filepath.Clean(w.root + paths.DrillRecord(w.team)))
	if err != nil {
		w.t.Fatal(err)
	}
	r, err := DecodeDrillRecord(raw, w.team)
	if err != nil {
		w.t.Fatal(err)
	}
	return r, true
}

// drillManifest is the drill export's manifest, read back as the import would.
func (w *drillWorld) drillManifest() Manifest {
	w.t.Helper()
	raw := w.archiveAt(paths.CarryDrillExport(w.team))
	if raw == nil {
		w.t.Fatal("there is no drill export")
	}
	m, names, data, rd := readExport(w.t, raw, w.team)
	if len(names) != len(m.Members) || len(data) == 0 || rd.Size != int64(len(raw)) {
		w.t.Fatalf("the drill export reads back as %d members, %d files, %d bytes", len(names), len(data), rd.Size)
	}
	return m
}

// drillCopyCalls are SD3's calls from the stop on: the stop without disable, RS18's reads, the
// copy into the carry directory, the restart and the record removed.
func drillCopyCalls(team paths.TeamID) []string {
	tls, server := paths.TLSUnit(team), paths.ServerUnit(team)
	part := filepath.Base(paths.CarryPart(team, true))
	return []string{
		"systemd state " + tls, "stop " + tls, "systemd state " + server, "stop " + server,
		"systemd state " + tls, "systemd state " + server,
		"files mkdir", "files create " + part, "files rename " + part, "files syncdir",
		"serve", "remove drill.json",
	}
}

// drillCalls are a fresh drill's calls: SD1's reads, SC2's tail and SD2's record, then SD3's.
func drillCalls(team paths.TeamID) []string {
	return append([]string{
		"units state " + paths.RestoreUnit(team), "units state " + paths.RestoreCtlUnit(team), "units state " + paths.ServerUnit(team), "ready",
		"child tail", "write drill.json",
	}, drillCopyCalls(team)...)
}

// served asserts the team is as a drill leaves it: serving, never disabled, never sealed or
// blocked, still on the host's books, the record removed and no part left.
func (w *drillWorld) served(at string) {
	w.t.Helper()
	for _, unit := range []string{paths.TLSUnit(w.team), paths.ServerUnit(w.team)} {
		if u := w.unit(unit); u.active != "active" || !u.enabled {
			w.t.Errorf("%s: %s is %s, enabled %v", at, unit, u.active, u.enabled)
		}
	}
	if _, found := w.drillRecord(); found {
		w.t.Errorf("%s: drill.json is left", at)
	}
	w.untouched(at)
	if w.has(paths.CarryPart(w.team, true)) {
		w.t.Errorf("%s: the part is left", at)
	}
}

// untouched asserts what a drill never changes (SD3, SD5): no seal record, no start block, the
// stamp and the status record in place, no unit disabled, no sealed export.
func (w *drillWorld) untouched(at string) {
	w.t.Helper()
	for _, path := range []string{paths.SealRecord(w.team), paths.RestoreBlock(w.team), paths.CarryExport(w.team)} {
		if w.has(path) {
			w.t.Errorf("%s: %s was made", at, path)
		}
	}
	if !w.has(paths.StampFile(w.team)) || !w.has(paths.StatusFile(w.team)) {
		w.t.Errorf("%s: the team was taken off the host's books", at)
	}
	for _, unit := range []string{paths.TLSUnit(w.team), paths.ServerUnit(w.team)} {
		if !w.unit(unit).enabled {
			w.t.Errorf("%s: %s was disabled", at, unit)
		}
	}
}

// SD1-SD3: a drill of a live team records drill.json before any stop, stops both units without
// disabling them, reads SC5's facts, writes the archive of kind drill with drilled_ms to
// CarryDrillExport, serves the team again and removes the record. The answer is SX6's with kind
// drill.
func TestDrillExportsAndServes(t *testing.T) {
	w := newDrillWorld(t)
	out, err := w.drill()
	if err != nil {
		t.Fatal(err)
	}
	if want := drillCalls(w.team); !slices.Equal(w.calls, want) {
		t.Errorf("calls =\n%q\nwant\n%q", w.calls, want)
	}
	raw := w.archiveAt(paths.CarryDrillExport(w.team))
	if raw == nil {
		t.Fatal("there is no drill export")
	}
	got := w.answered(out, raw, KindDrill)
	m, names, data, rd := readExport(t, raw, w.team)
	if rd.SHA256 != got.sum || rd.Size != got.size {
		t.Errorf("read %+v, answered %+v", rd, got)
	}
	want := SealFacts{
		TimeMS: sealMS, ServerName: w.decl.ServerName, Port: mustPort(t, 8448), Tuwunel: mustVersion(t, "1.9.4"),
		Software: mustDigest(t, sealSoftware), Roster: RosterFacts{Seq: 20, Rev: 5, RestoreEpoch: 2},
		EpochFloor: EpochFloor{Epoch: 3, MinBackup: 8}, Tail: w.tail.Rooms,
	}
	if m.Kind != KindDrill || m.Format != ExportFormat || m.ServerName != w.decl.ServerName || !sealEqual(m.Seal, want) {
		t.Errorf("manifest %+v", m)
	}
	if !slices.Equal(names, w.exportMembers()) {
		t.Errorf("members\n%q\nwant\n%q", names, w.exportMembers())
	}
	w.carriedAsIs(data)
	w.served("the drill")
}

// SD1, SG3, SD5: each refusal is made before anything is written or stopped; a sealed team
// refuses the drill (70).
func TestDrillRefusals(t *testing.T) {
	team := sampleTeam(t)
	for name, c := range map[string]struct {
		plant func(w *drillWorld)
		is    error
		names string
	}{
		"sealed": {func(w *drillWorld) {
			if _, err := w.seal(); err != nil {
				w.t.Fatal(err)
			}
		}, ErrSealed, "sealed"},
		"an import":          {func(w *drillWorld) { w.put(paths.CarryImportRecord(team), "{}") }, ErrSealed, "--discard"},
		"a restore recorded": {func(w *drillWorld) { w.put(paths.RestoreRecord(team), "{}") }, restore.ErrPending, "--resume"},
		"not applied":        {func(w *drillWorld) { mustRemove(w.sealWorld, paths.StampFile(team)) }, restore.ErrRefused, "team apply"},
		"migration origin": {
			func(w *drillWorld) { w.put(paths.MigrationDir(team)+"/state.json", "{}") }, ErrRefused, "migration",
		},
		"a restore leftover": {
			func(w *drillWorld) { w.put(paths.HomeserverPrivateState(team)+"/db.previous/marker", "x") }, restore.ErrRefused, "db.previous",
		},
		"a pending roster": {func(w *drillWorld) {
			w.putRoster(func(r *apply.RosterFile) {
				r.Pending = &apply.Pending{Op: apply.OpMemberRetire, Step: "rooms", SinceMS: 1, Handle: "web.1+desk.podman"}
			})
		}, ErrRefused, "member retire --team ops --handle web.1+desk.podman"},
		"a stopped server":       {func(w *drillWorld) { w.unit(paths.ServerUnit(team)).active = "inactive" }, ErrRefused, "team apply"},
		"a server not answering": {func(w *drillWorld) { w.silent = true }, ErrRefused, "team apply"},
		"a tail that disagrees":  {func(w *drillWorld) { w.tail.Epoch.Rev = 4 }, ErrRefused, "run team apply first"},
		"a drill record that does not decode": {
			func(w *drillWorld) { w.put(paths.DrillRecord(team), `{"v":1,"team":"web","step":"stopping"}`) }, nil, "drill.json",
		},
	} {
		t.Run(name, func(t *testing.T) {
			w := newDrillWorld(t)
			c.plant(w)
			mark := len(w.calls)
			before := w.has(paths.DrillRecord(team))
			out, err := w.drill()
			if err == nil || !strings.Contains(err.Error(), c.names) {
				t.Fatalf("err = %v, want one naming %q", err, c.names)
			}
			if c.is != nil && !errors.Is(err, c.is) {
				t.Errorf("err = %v, want %v", err, c.is)
			}
			if m := w.mutations(mark); len(m) != 0 || out != nil || w.has(paths.DrillRecord(team)) != before {
				t.Errorf("the refused drill changed %q, answered %s", m, out)
			}
			if w.has(paths.CarryDrillExport(team)) || w.has(paths.CarryPart(team, true)) {
				t.Error("the refused drill wrote an archive")
			}
		})
	}
}

// SD4: with drill.json present the drill skips SD1's serving clause (the team may be down),
// removes a part left, keeps the record's tail, copies, serves and removes the record.
func TestDrillResumesFromItsRecord(t *testing.T) {
	w := newDrillWorld(t)
	tail := []TailEntry{{Room: "main", EventID: "$old", TsMS: 7}}
	w.put(paths.DrillRecord(w.team), string(EncodeDrillRecord(DrillRecord{Team: w.team, Step: StepStopping, StartedMS: 1, Tail: tail})))
	w.unit(paths.ServerUnit(w.team)).active = "inactive"
	w.unit(paths.TLSUnit(w.team)).active = "failed"
	w.put(paths.CarryPart(w.team, true), "half an archive")
	if _, err := w.drill(); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(w.calls, "ready") || slices.Contains(w.calls, "child tail") || slices.Contains(w.calls, "write drill.json") {
		t.Errorf("the resume read the live server or wrote the record again: %q", w.calls)
	}
	if m := w.drillManifest(); !slices.Equal(m.Seal.Tail, tail) {
		t.Errorf("tail %+v, want the record's %+v", m.Seal.Tail, tail)
	}
	w.served("the resume")
}

// SD4: a failure of Serve leaves the record and answers "the team is down" naming the way on
// (1, not a refusal); the next drill serves and removes the record.
func TestDrillServeFailureLeavesTheRecord(t *testing.T) {
	w := newDrillWorld(t)
	w.serveErr = errors.New("port 8448 is taken")
	out, err := w.drill()
	if err == nil || errors.Is(err, ErrRefused) || out != nil ||
		!strings.Contains(err.Error(), "the team is down: team export --team ops --drill again after fixing") ||
		!strings.Contains(err.Error(), "port 8448 is taken") {
		t.Fatalf("out %s, err %v", out, err)
	}
	if rec, found := w.drillRecord(); !found || rec.Step != StepStopping || rec.StartedMS != sealMS || !slices.Equal(rec.Tail, w.tail.Rooms) {
		t.Fatalf("the record with the team down: %+v, found %v", rec, found)
	}
	w.untouched("the failed serve")
	w.serveErr = nil
	if _, err = w.drill(); err != nil {
		t.Fatal(err)
	}
	w.served("the next drill")
}

// SD3: a copy that fails after the stop (a source SX2 refuses, found only once the team is cold)
// still serves the team again and removes the record; the failure is the copy's, and no archive
// or part is left.
func TestDrillCopyFailureServesAgain(t *testing.T) {
	w := newDrillWorld(t)
	w.put(paths.CtlState(w.team)+"/notes.txt", "x")
	_, err := w.drill()
	if !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "ctl/notes.txt") {
		t.Fatalf("err = %v", err)
	}
	if !slices.Contains(w.calls, "serve") {
		t.Errorf("the team was not served again: %q", w.calls)
	}
	if w.has(paths.CarryDrillExport(w.team)) {
		t.Error("an archive was made")
	}
	w.served("the failed copy")
}

// SD3: a drill export is replaced by the next drill: the live team's state and the drill's time
// change between drills, so SX7's equal-digest rule is the sealed export's only.
func TestDrillReplacesAnEarlierDrill(t *testing.T) {
	w := newDrillWorld(t)
	if _, err := w.drill(); err != nil {
		t.Fatal(err)
	}
	first := w.archiveAt(paths.CarryDrillExport(w.team))
	w.clock.now = w.clock.now.Add(time.Hour)
	w.put(paths.ServerConfig(w.team), "server_name = \"changed\"\n")
	if _, err := w.drill(); err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first, w.archiveAt(paths.CarryDrillExport(w.team))) || w.drillManifest().Seal.TimeMS != sealMS+time.Hour.Milliseconds() {
		t.Fatal("the second drill did not replace the first")
	}
	w.served("the second drill")
}

// CN5, SL1: the archive cap is read first, then the lock waited for host.PlayWait; either
// failing reads and changes nothing.
func TestDrillNeedsTheCapAndTheLock(t *testing.T) {
	w := newDrillWorld(t)
	w.maxErr = errors.New("the setting is absent")
	if _, err := w.drill(); !errors.Is(err, w.maxErr) {
		t.Fatalf("got %v", err)
	}
	w.maxErr = nil
	held, err := teamlock.Take(context.Background(), w.root+paths.AdminRunDir, w.team, "apply", 0, w.clock, teamlock.Unguarded)
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.drill()
	if rerr := held.Release(); rerr != nil {
		t.Fatal(rerr)
	}
	if !errors.Is(err, teamlock.ErrBusy) || w.clock.slept < host.PlayWait {
		t.Fatalf("err = %v after waiting %v", err, w.clock.slept)
	}
	if len(w.calls) != 0 || len(w.opens) != 0 {
		t.Errorf("a drill without the cap or the lock did %q, opened %v", w.calls, w.opens)
	}
}

// killDrill runs the drill until the kill and asserts it was killed.
func killDrill(t *testing.T, w *drillWorld) {
	t.Helper()
	done := make(chan bool, 1)
	go func() {
		returned := false
		defer func() { done <- returned }()
		if _, err := w.drill(); err != nil {
			t.Errorf("the run failed before its kill: %v", err)
		}
		returned = true
	}()
	if <-done {
		t.Fatal("the drill was not killed")
	}
}

// drillInvariants are SM5's after a kill: nothing a drill never does (untouched); with no record,
// nothing stopped and no archive or part.
func (w *drillWorld) drillInvariants(k int) {
	w.t.Helper()
	at := "kill " + strconv.Itoa(k)
	w.untouched(at)
	if _, found := w.drillRecord(); found {
		return
	}
	for _, unit := range []string{paths.TLSUnit(w.team), paths.ServerUnit(w.team)} {
		if w.unit(unit).active != "active" {
			w.t.Errorf("%s: %s was stopped with no record", at, unit)
		}
	}
	if w.has(paths.CarryDrillExport(w.team)) || w.has(paths.CarryPart(w.team, true)) {
		w.t.Errorf("%s: an archive or a part with no record", at)
	}
}

// SM5, SD4: a failure answered by any write or removal of the drill is its failure, never a
// refusal; the invariants hold as after a kill, and the next drill ends served.
func TestDrillFailureAtEveryWrite(t *testing.T) {
	for k, call := range drillCalls(sampleTeam(t)) {
		if !strings.HasPrefix(call, "write ") && !strings.HasPrefix(call, "remove ") {
			continue
		}
		at := "failure " + strconv.Itoa(k+1) + " (" + call + ")"
		w := newDrillWorld(t)
		w.fail = k + 1
		if _, err := w.drill(); !errors.Is(err, errInjected) || errors.Is(err, ErrRefused) {
			t.Fatalf("%s: err = %v", at, err)
		}
		w.drillInvariants(k + 1)
		w.fail = 0
		if _, err := w.drill(); err != nil {
			t.Fatalf("%s: the next drill: %v", at, err)
		}
		w.served(at)
	}
}

// SM5, SD4: a kill at every call of the drill leaves the invariants, and the next drill (its
// resume when the record is there) ends served with the archive; one more drill succeeds too.
func TestDrillKillAtEveryCall(t *testing.T) {
	calls := drillCalls(sampleTeam(t))
	for k := 1; k <= len(calls); k++ {
		t.Run(strconv.Itoa(k)+" "+calls[k-1], func(t *testing.T) {
			t.Parallel()
			w := newDrillWorld(t)
			w.kill = k
			killDrill(t, w)
			w.drillInvariants(k)
			w.kill = 0
			if _, err := w.drill(); err != nil {
				t.Fatalf("kill %d: the next drill: %v", k, err)
			}
			w.served("kill " + strconv.Itoa(k))
			if m := w.drillManifest(); m.Kind != KindDrill {
				t.Fatalf("kill %d: the drill export is of kind %s", k, m.Kind)
			}
			if _, err := w.drill(); err != nil {
				t.Fatalf("kill %d: one more drill: %v", k, err)
			}
			w.served("kill " + strconv.Itoa(k) + ", once more")
		})
	}
}
