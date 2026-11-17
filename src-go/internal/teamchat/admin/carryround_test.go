package admin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/carry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// SPEC-carry section 13, item 15: a carry run end to end through the verbs, `team seal`,
// `team export`, `team import` and `team carry-check`, over a hand-built carry engine of the same
// file adapters as `wire`'s (the CarryChild and the ctl-child served in process), with a test
// homeserver. The round is run here over fakehs, and with `-tags tuwunel` over a real Tuwunel
// (carry_tuwunel_test.go), whose database is the one carried.

// carryServer is the homeserver of a carry round: the source's live server, stopped by the
// seal, and the server on the guest's imported database.
type carryServer interface {
	// stopLive ends the source's server when the seal stops its unit, so the export copies a
	// database at rest.
	stopLive() error
	// serveImported serves the guest's imported database on loopback and points the guest's
	// admin client at it.
	serveImported(ctx context.Context, guest *world) error
	// stopImported ends that server, when one runs.
	stopImported() error
}

// sharedServer is fakehs as a carry round's homeserver: one in-memory server stands for the
// source's and the guest's, so it carries the events by itself, and the round proves the
// plumbing rather than the database.
type sharedServer struct{ src *world }

func (sharedServer) stopLive() error { return nil }

func (s sharedServer) serveImported(_ context.Context, guest *world) error {
	guest.srv, guest.admin = s.src.srv, s.src.admin
	return nil
}

func (sharedServer) stopImported() error { return nil }

// carryHost is one host of a carry round: its world and the round's server.
type carryHost struct {
	w     *world
	hs    carryServer
	guest bool
}

// carrySystemd is the world's systemd with the homeserver unit's stop ending the round's server.
type carrySystemd struct {
	*fakeSystemd

	h *carryHost
}

func (s carrySystemd) Stop(ctx context.Context, unit string) error {
	if unit == paths.ServerUnit(alpha(s.w.t)) && s.unit(unit).Active == "active" {
		stop := s.h.hs.stopLive
		if s.h.guest {
			stop = s.h.hs.stopImported
		}
		if err := stop(); err != nil {
			return err
		}
	}
	return s.fakeSystemd.Stop(ctx, unit)
}

// carryUnits is carrySystemd as restore.Units: a unit never started is not loaded.
type carryUnits struct{ s carrySystemd }

func (u carryUnits) State(_ context.Context, unit string) (restore.UnitState, error) {
	st, known := u.s.w.units[unit]
	if !known {
		return restore.UnitState{Load: "not-found", Active: "inactive"}, nil
	}
	return restore.UnitState{Load: "loaded", Active: st.Active, Result: "success"}, nil
}

func (u carryUnits) Stop(ctx context.Context, unit string) error { return u.s.Stop(ctx, unit) }

// deps is a carry engine built by hand (admin's tests cannot import `wire`): the same file
// adapters as `wire`'s (carry.OS, carry.StageOS, restore.OS, CarryChild), the world's fakes for
// systemd, readiness and the status record, a fixed archive cap, no drill Serve, and the round's
// server as the loopback start. `wire`'s own assembly is pinned by its store_test.
func (h *carryHost) deps(t *testing.T) carry.Deps {
	t.Helper()
	w, uid := h.w, selfUID(t)
	software, err := paths.ParseDigest(digest)
	if err != nil {
		t.Fatal(err)
	}
	sd := carrySystemd{fakeSystemd: &fakeSystemd{w: w}, h: h}
	return carry.Deps{
		Root: w.root, LockDir: w.root + paths.AdminRunDir, CtlUID: uid, StateOwnerUID: uid, Clock: w, Software: software,
		Units: carryUnits{sd}, Systemd: sd, Ready: w, Child: CarryChild{Live: inProcessChild{w: w}},
		FS: restore.OS{Root: w.root, StateOwnerUID: uid}, Status: &fakeStatus{w: w},
		Files: carry.OS{Root: w.root, StateOwnerUID: uid},
		Stage: carry.StageOS{
			Root: w.root, StateOwnerUID: uid, CtlUID: os.Getuid(), CtlGID: os.Getgid(),
			OwnerGroup: func() (int, error) { return os.Getgid(), nil },
		},
		MaxArchive: func() (int64, error) { return 1 << 30, nil },
		Serve:      func(context.Context, paths.TeamID) error { return errors.New("a carry round runs no drill") },
		Loopback: func(ctx context.Context, team paths.TeamID) error {
			if err := h.hs.serveImported(ctx, w); err != nil {
				return err
			}
			sd.unit(paths.ServerUnit(team)).Active = "active"
			return nil
		},
	}
}

// ok runs teamchat-admin as root over the host with the round's carry engine, and wants exit 0.
func (h *carryHost) ok(t *testing.T, args ...string) string {
	t.Helper()
	sys := h.w.sys()
	d := h.deps(t)
	sys.Carry = func() (Carrier, error) { return CarryEngine{Deps: d}, nil }
	var stdout, stderr bytes.Buffer
	code := Run(t.Context(), Env{Args: args, Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr, Proc: rootProc(), Digest: digest, Sys: sys})
	if code != term.ExitOK {
		t.Fatalf("%v = %d\nstdout %s\nstderr %s", args, code, stdout.String(), stderr.String())
	}
	return stdout.String()
}

// writePlaced writes carry.place's files under ctl/<team>/, each 0600 in a directory 0700.
func (w *world) writePlaced(team paths.TeamID, files []PlacedFile) error {
	for _, f := range files {
		path := w.root + paths.CtlState(team) + "/" + f.Name
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(path, f.Data, 0o600); err != nil {
			return err
		}
	}
	return nil
}

// carryGuest is an empty guest the play has made ready: the base units (bareWorld's), the Tuwunel
// binary of version, and the export placed in its inbox by root, 0600. Its clock is an hour
// after the source's.
func carryGuest(t *testing.T, src *world, version paths.ServerVersion, archive []byte) *world {
	t.Helper()
	g := bareWorld(t)
	g.at = src.now().Add(time.Hour)
	g.placeOnDisk = true
	for _, dir := range []string{paths.CarryDir, paths.AdminRunDir, filepath.Dir(paths.ServerBinary(version))} {
		if err := os.MkdirAll(g.root+dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(g.root+paths.ServerBinary(version), []byte("tuwunel"), 0o600); err != nil {
		t.Fatal(err)
	}
	inbox, err := os.OpenFile(filepath.Clean(g.root+paths.CarryExport(alpha(t))), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, werr := inbox.Write(archive)
	if err := errors.Join(werr, inbox.Close()); err != nil {
		t.Fatal(err)
	}
	return g
}

// carryRound carries alpha from src, applied with its admin token written, to an empty guest:
// a message in each room, then `team seal`, `team export`, the archive into the guest's inbox,
// `team import` and `team carry-check`, which finds the seal's tail; then the imported database
// is served again and each room's newest event read back is the seal's tail, and the roster
// event the seal's epoch.
func carryRound(t *testing.T, src *world, hs carryServer) {
	t.Helper()
	team := alpha(t)
	ids := src.rooms(t)
	for _, room := range []string{ids.Main, ids.Reports, ids.Questions} {
		if room != "" {
			src.send(t, room, "before-the-seal-"+strings.TrimPrefix(room, "!"))
		}
	}
	want := src.wantTail(t)
	epoch := src.liveRoster(t)
	source := &carryHost{w: src, hs: hs}
	source.ok(t, "team", "seal", "--team", team.String())
	source.ok(t, "team", "export", "--team", team.String())
	archive, err := os.ReadFile(src.root + paths.CarryExport(team))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(archive)
	guest := carryGuest(t, src, src.pin().Version, archive)
	g := &carryHost{w: guest, hs: hs, guest: true}
	g.ok(t, "team", "import", "--team", team.String(), "--sha256", hex.EncodeToString(sum[:]))
	if got, sealed := treeOf(t, guest.root+paths.HomeserverPrivateState(team)), treeOf(t, src.root+paths.HomeserverPrivateState(team)); !maps.Equal(got, sealed) || len(got) == 0 {
		t.Fatalf("the guest's homeserver tree holds %d files, not the sealed source's %d byte for byte", len(got), len(sealed))
	}
	carried := guestRecord(t, guest)
	if !slices.Equal(carried.Tail, want) {
		t.Fatalf("carried.json's tail %+v, want each room's newest event %+v", carried.Tail, want)
	}
	checked := g.ok(t, "team", "carry-check", "--team", team.String())
	if strings.Contains(checked, `"found":false`) || strings.Count(checked, `"found":true`) != len(want) {
		t.Fatalf("carry-check answered %s, want each of %d tail events found", checked, len(want))
	}
	if u := guest.units[paths.ServerUnit(team)]; u == nil || u.Active != "inactive" {
		t.Fatalf("the guest's homeserver after the check: %+v, want stopped", u)
	}
	readBack(t, guest, hs, want, epoch)
}

// readBack serves the guest's imported database again and wants each room's newest event to be
// the seal's tail, and the roster event the seal's rev and restore epoch.
func readBack(t *testing.T, guest *world, hs carryServer, want []carry.TailEntry, epoch proto.Roster) {
	t.Helper()
	if err := hs.serveImported(t.Context(), guest); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := hs.stopImported(); err != nil {
			t.Error(err)
		}
	})
	if got := guest.wantTail(t); !slices.Equal(got, want) {
		t.Errorf("read back from the imported database: %+v, want the seal's tail %+v", got, want)
	}
	if got := guest.liveRoster(t); got.Rev != epoch.Rev || got.RestoreEpoch != epoch.RestoreEpoch {
		t.Errorf("the imported roster event rev %d epoch %d, want the seal's %d, %d", got.Rev, got.RestoreEpoch, epoch.Rev, epoch.RestoreEpoch)
	}
}

// treeOf is every regular file under dir by its path from dir, with its content.
func treeOf(t *testing.T, dir string) map[string]string {
	t.Helper()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	tree := root.FS()
	err = fs.WalkDir(tree, ".", func(path string, e fs.DirEntry, err error) error {
		if err != nil || !e.Type().IsRegular() {
			return err
		}
		raw, rerr := fs.ReadFile(tree, path)
		out[path] = string(raw)
		return rerr
	})
	if err = errors.Join(err, root.Close()); err != nil {
		t.Fatal(err)
	}
	return out
}

// guestRecord is the guest's carried.json.
func guestRecord(t *testing.T, guest *world) carry.CarriedRecord {
	t.Helper()
	raw, err := os.ReadFile(guest.root + paths.CarriedRecord(alpha(t)))
	if err != nil {
		t.Fatal(err)
	}
	rec, err := carry.DecodeCarriedRecord(raw, alpha(t))
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

// Item 15: alpha carried through the four verbs over fakehs: the seal's tail is the one each
// room's newest event was, carried in carried.json, found by the check and read back on the guest.
func TestCarryRoundThroughTheVerbs(t *testing.T) {
	src := newWorld(t)
	src.applied(t)
	if err := os.WriteFile(src.root+paths.CtlState(alpha(t))+"/"+AdminTokenName, []byte("syt_YWRtaW4_abcdefghijklmnop_0123"), 0o600); err != nil {
		t.Fatal(err)
	}
	// fakehs keeps no database: a stand-in tree, which the round wants carried byte for byte.
	db := src.root + paths.HomeserverPrivateDatabase(alpha(t))
	if err := os.MkdirAll(db, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(db+"/CURRENT", []byte("MANIFEST-000001\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	carryRound(t, src, sharedServer{src: src})
}
