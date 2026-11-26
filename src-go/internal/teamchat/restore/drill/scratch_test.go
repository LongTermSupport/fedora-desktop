package drill

import (
	"bytes"
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/carry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
)

const digest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// host is a scratch root with the admin state root made, and the uid its files have.
func host(t *testing.T) (string, uint32) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(root+paths.AdminStateRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatal("no owner in a stat")
	}
	return root, st.Uid
}

func marker(t *testing.T) Marker {
	t.Helper()
	d, err := paths.ParseDigest(digest)
	if err != nil {
		t.Fatal(err)
	}
	return Marker{Digest: d, StartedMS: 1700000000000}
}

func team(t *testing.T, s string) paths.TeamID {
	t.Helper()
	id, err := paths.ParseTeamID(s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// applied gives root a team's state directory and declaration, as an apply leaves them.
func applied(t *testing.T, root string, id paths.TeamID) {
	t.Helper()
	if err := os.MkdirAll(root+paths.AdminState(id), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root+paths.TeamDeclaration(id), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// A scratch team id is the drill prefix and 12 hex from the random source, inside the team id
// grammar; the declare gate refuses every id of that prefix, so none is a declared team's.
func TestNewTeam(t *testing.T) {
	id, err := NewTeam(bytes.NewReader([]byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xab}))
	if err != nil || id.String() != "rd-0123456789ab" || !IsScratch(id) {
		t.Fatalf("NewTeam = %v, %v", id, err)
	}
	if !strings.HasPrefix(id.String(), declare.DrillPrefix) {
		t.Fatalf("%s does not start with the prefix declare refuses", id)
	}
	if _, err := NewTeam(bytes.NewReader([]byte{1, 2})); err == nil {
		t.Fatal("a short random read made a team id")
	}
	for _, s := range []string{"ops", "rd-ops", "rd-0123456789a", "rd-0123456789abc", "rd-0123456789ag"} {
		if id, err := paths.ParseTeamID(s); err == nil && IsScratch(id) {
			t.Errorf("%s is taken for a scratch team", s)
		}
	}
}

// The marker is strict {"v":1,"digest","started_ms"}, written 0600 before anything else of the
// team, in a state directory the drill makes; a second marker for the same id is refused.
func TestMarkerRoundTrips(t *testing.T) {
	root, uid := host(t)
	id := team(t, "rd-0123456789ab")
	if err := WriteMarker(root, id, marker(t)); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(root + paths.RestoreDrillMarker(id))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("stat %v, %v", info, err)
	}
	m, found, err := ReadMarker(root, uid, id)
	if err != nil || !found || m != marker(t) {
		t.Fatalf("ReadMarker = %+v, %v, %v", m, found, err)
	}
	if err := WriteMarker(root, id, marker(t)); err == nil {
		t.Fatal("a second drill was marked on a team that exists")
	}
	if err := WriteMarker(root, team(t, "ops"), marker(t)); err == nil {
		t.Fatal("a team that is not a scratch team was marked")
	}
	if _, found, err := ReadMarker(root, uid, team(t, "rd-aaaaaaaaaaaa")); found || err != nil {
		t.Fatalf("no marker: %v, %v", found, err)
	}
	if _, _, err := ReadMarker(root, uid+1, id); err == nil {
		t.Fatal("a marker of another owner was read")
	}
}

// The marker is not a carry record: the carry's guards, which every lock-taking verb and team
// remove run under the lock, pass a marked scratch team, so the drill's own apply, backup,
// restore and --clean's purge are not refused as "a drill export is under way" (issue #324).
func TestTheMarkerPassesTheCarryGuards(t *testing.T) {
	root, uid := host(t)
	id := team(t, "rd-0123456789ab")
	if err := WriteMarker(root, id, marker(t)); err != nil {
		t.Fatal(err)
	}
	if recorded, err := carry.Recorded(root, id); err != nil || len(recorded) != 0 {
		t.Fatalf("the marker reads as carry records %v, %v", recorded, err)
	}
	for name, guard := range map[string]teamlock.Guard{
		"Guard": carry.Guard(root), "RemoveGuard": carry.RemoveGuard(root), "DrillImportGuard": carry.DrillImportGuard(root, uid),
	} {
		if err := guard(id); err != nil {
			t.Errorf("carry.%s refuses the drill's team: %v", name, err)
		}
	}
}

func TestDecodeMarkerRefuses(t *testing.T) {
	good := string(EncodeMarker(marker(t)))
	if good != `{"v":1,"digest":"`+digest+`","started_ms":1700000000000}` {
		t.Fatalf("encoded %s", good)
	}
	for name, s := range map[string]string{
		"not json":   "{",
		"a version":  strings.Replace(good, `"v":1`, `"v":2`, 1),
		"a digest":   strings.Replace(good, digest, "xyz", 1),
		"a negative": strings.Replace(good, "1700000000000", "-1", 1),
		"an extra":   strings.Replace(good, `"v":1,`, `"v":1,"x":1,`, 1),
		"a missing":  `{"v":1,"digest":"` + digest + `"}`,
		"too large":  good + strings.Repeat(" ", maxMarker),
	} {
		if _, err := DecodeMarker([]byte(s)); err == nil {
			t.Errorf("%s: decoded", name)
		}
	}
}

// A marker no drill could read back (no digest) is never written: a scratch team with an
// unreadable marker would block every drill and be beyond --clean.
func TestWriteMarkerRefusesAnUnreadableMarker(t *testing.T) {
	root, uid := host(t)
	id := team(t, "rd-0123456789ab")
	if err := WriteMarker(root, id, Marker{StartedMS: 1}); err == nil || !strings.Contains(err.Error(), "read back") {
		t.Fatalf("WriteMarker = %v", err)
	}
	if err := CheckStart(root, uid); err != nil {
		t.Fatalf("the refused marker left a team behind: %v", err)
	}
}

// Q1: the drill starts only on a host that holds no team but marked scratch teams, and no marked
// scratch team either: a leftover is named with --clean, any other team (an applied team, or a
// scratch id with no marker) refuses the drill.
func TestCheckStart(t *testing.T) {
	root, uid := host(t)
	if err := os.MkdirAll(root+paths.AdminLXCDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root+paths.RestoreProofFile, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CheckStart(root, uid); err != nil {
		t.Fatalf("an empty host: %v", err)
	}
	left := team(t, "rd-0123456789ab")
	if err := WriteMarker(root, left, marker(t)); err != nil {
		t.Fatal(err)
	}
	err := CheckStart(root, uid)
	if !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "rd-0123456789ab") || !strings.Contains(err.Error(), "--clean") {
		t.Fatalf("a leftover: %v", err)
	}

	root, uid = host(t)
	applied(t, root, team(t, "ops"))
	err = CheckStart(root, uid)
	if !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "ops") || !strings.Contains(err.Error(), "never runs beside a team") {
		t.Fatalf("an applied team: %v", err)
	}

	root, uid = host(t)
	if err := os.MkdirAll(root+paths.AdminState(team(t, "rd-bbbbbbbbbbbb")), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := CheckStart(root, uid); !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "rd-bbbbbbbbbbbb") {
		t.Fatalf("an unmarked scratch directory: %v", err)
	}
}

// A carry import under way is a team arriving before its state directory exists (SI8), so it
// refuses the drill; an archive alone in the carry inbox is no team, and an import record that
// names no team cannot be judged.
func TestCheckStartRefusesACarryImport(t *testing.T) {
	root, uid := host(t)
	if err := os.MkdirAll(root+paths.CarryDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root+paths.CarryExport(team(t, "web")), []byte("tar"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CheckStart(root, uid); err != nil {
		t.Fatalf("an archive in the carry inbox is no team: %v", err)
	}
	if err := os.WriteFile(root+paths.CarryImportRecord(team(t, "web")), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CheckStart(root, uid); !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "web") || !strings.Contains(err.Error(), "import") {
		t.Fatalf("an import under way: %v", err)
	}
	root, uid = host(t)
	if err := os.MkdirAll(root+paths.CarryDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root+paths.CarryDir+"/Not.A.Team.import.json", []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CheckStart(root, uid); err == nil || errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "no team's import record") {
		t.Fatalf("an import record of no team: %v", err)
	}
}

// fakeCleaner records each clean-up step.
type fakeCleaner struct {
	recorded map[string]bool
	fail     string
	calls    []string
}

func (f *fakeCleaner) cleaner() Cleaner {
	step := func(what string, id paths.TeamID) error {
		f.calls = append(f.calls, what+" "+id.String())
		if f.fail == what {
			return errors.New("injected " + what)
		}
		return nil
	}
	return Cleaner{
		Recorded: func(id paths.TeamID) (bool, error) { return f.recorded[id.String()], nil },
		Discard:  func(_ context.Context, id paths.TeamID) error { return step("discard", id) },
		Remove:   func(_ context.Context, id paths.TeamID) error { return step("remove", id) },
	}
}

// --clean removes only marked scratch teams: a recorded restore is discarded first, then the
// team is removed with purge; a declared team and an unmarked scratch directory are left alone.
func TestClean(t *testing.T) {
	root, uid := host(t)
	a, b := team(t, "rd-aaaaaaaaaaaa"), team(t, "rd-bbbbbbbbbbbb")
	for _, id := range []paths.TeamID{a, b} {
		if err := WriteMarker(root, id, marker(t)); err != nil {
			t.Fatal(err)
		}
	}
	applied(t, root, team(t, "ops"))
	if err := os.MkdirAll(root+paths.AdminState(team(t, "rd-cccccccccccc")), 0o700); err != nil {
		t.Fatal(err)
	}
	f := &fakeCleaner{recorded: map[string]bool{"rd-bbbbbbbbbbbb": true}}
	cleaned, err := Clean(t.Context(), root, uid, f.cleaner())
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"remove rd-aaaaaaaaaaaa", "discard rd-bbbbbbbbbbbb", "remove rd-bbbbbbbbbbbb"}; !slices.Equal(f.calls, want) {
		t.Fatalf("calls = %q, want %q", f.calls, want)
	}
	if !slices.Equal(cleaned, []paths.TeamID{a, b}) {
		t.Fatalf("cleaned = %v", cleaned)
	}
}

// A failed step stops the clean-up at once: nothing after it runs.
func TestCleanStopsAtAFailure(t *testing.T) {
	root, uid := host(t)
	for _, s := range []string{"rd-aaaaaaaaaaaa", "rd-bbbbbbbbbbbb"} {
		if err := WriteMarker(root, team(t, s), marker(t)); err != nil {
			t.Fatal(err)
		}
	}
	f := &fakeCleaner{recorded: map[string]bool{"rd-aaaaaaaaaaaa": true}, fail: "discard"}
	cleaned, err := Clean(t.Context(), root, uid, f.cleaner())
	if err == nil || !strings.Contains(err.Error(), "injected discard") || !strings.Contains(err.Error(), "rd-aaaaaaaaaaaa") || len(cleaned) != 0 {
		t.Fatalf("Clean = %v, %v", cleaned, err)
	}
	if !slices.Equal(f.calls, []string{"discard rd-aaaaaaaaaaaa"}) {
		t.Fatalf("calls = %q", f.calls)
	}
}
