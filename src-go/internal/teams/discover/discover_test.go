package discover

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
)

func rec(name, dir, prefix string, restore bool, args ...string) registry.Record {
	return registry.Record{Name: name, Dir: dir, Prefix: prefix, Restore: restore, Args: args}
}

func TestRecordsNameTheirSeatsByCheckout(t *testing.T) {
	ix := New()
	ix.AddRecord(rec("ccy-a", "/a", "ccy", true, "--teams", "1@alpha,dev@beta"))
	ix.AddRecord(rec("ccy-b", "/b", "ccy", false, "--teams=1@alpha", "--", "--teams", "9@alpha"))
	ix.AddRecord(rec("cc-c", "/c", "cc", false, "--teams", "2@alpha"))
	ix.AddRecord(rec("other", "/o", "other", false, "--teams", "3@alpha"))
	if got := ix.SessionOf(Key{"/a", "alpha", "1"}); got != "ccy-a" {
		t.Fatalf("/a 1@alpha is %q", got)
	}
	if got := ix.SessionOf(Key{"/b", "alpha", "1"}); got != "ccy-b" {
		t.Fatalf("the same seat in another checkout is another seat: %q", got)
	}
	if ix.SessionOf(Key{"/b", "alpha", "9"}) != "" || ix.SessionOf(Key{"/o", "alpha", "3"}) != "" {
		t.Fatal("words after -- and records of other launchers name no seat")
	}
	if ix.SessionOf(Key{"/c", "alpha", "2"}) != "cc-c" {
		t.Fatal("a cc record names its seats")
	}
	restores, recorded := ix.Restore(Key{"/a", "beta", "dev"})
	if !restores || !recorded {
		t.Fatal("restore flag of a recorded seat")
	}
	if _, recorded := ix.Restore(Key{"/x", "alpha", "1"}); recorded {
		t.Fatal("an unnamed seat has no restore flag")
	}
	want := []Key{{"/a", "alpha", "1"}, {"/a", "beta", "dev"}}
	if got := ix.SeatsOf("ccy-a"); !reflect.DeepEqual(got, want) {
		t.Fatalf("seats of ccy-a %v", got)
	}
}

func TestDesiredFilesNameSeatsTooAndARecordWins(t *testing.T) {
	runtime := t.TempDir()
	write := func(session, body string) {
		teamtest.MustWrite(t, filepath.Join(membership.Dir(runtime, session), membership.File), body)
	}
	write("ccy-hot", `{"v":1,"session":"ccy-hot","kind":"ccy","checkout":"/h","generation":2,"written_ms":1,"seats":[{"team":"alpha","seat":"3"}],"farewell":[]}`)
	write("ccy-bad", `not json`)
	desired, unreadable, err := ReadDesired(runtime)
	if err != nil {
		t.Fatal(err)
	}
	if len(desired) != 1 || desired[0].Session != "ccy-hot" || len(unreadable) != 1 || !strings.Contains(unreadable[0], "ccy-bad") {
		t.Fatalf("desired %v unreadable %v", desired, unreadable)
	}
	ix := New()
	ix.AddRecord(rec("ccy-rec", "/h", "ccy", true, "--teams", "4@alpha"))
	for _, d := range desired {
		ix.AddDesired(d)
	}
	ix.AddDesired(membership.Desired{Session: "ccy-other", Checkout: "/h", Seats: []membership.Seat{{Team: "alpha", Seat: "4"}}})
	if ix.SessionOf(Key{"/h", "alpha", "3"}) != "ccy-hot" {
		t.Fatal("a desired file names its seat")
	}
	// Fable's M3 note N4: the live session that wants the seat is its session; the record that
	// would replay it elsewhere is kept, so the conflict can be named.
	if ix.SessionOf(Key{"/h", "alpha", "4"}) != "ccy-other" {
		t.Fatal("a live desired file wins over another session's record")
	}
	if got := ix.RecordedBy(Key{"/h", "alpha", "4"}); got != "ccy-rec" {
		t.Fatalf("recorded by %q", got)
	}
	if restores, recorded := ix.Restore(Key{"/h", "alpha", "4"}); recorded || restores {
		t.Fatal("the other record's restore is not the live session's")
	}
	if got := ix.RecordedBy(Key{"/h", "alpha", "3"}); got != "" {
		t.Fatalf("no record names seat 3: %q", got)
	}
	if got := ix.DesiredBy(Key{"/h", "alpha", "4"}); !reflect.DeepEqual(got, []string{"ccy-other"}) {
		t.Fatalf("desired by %v", got)
	}
}

// Plan 00203 M6 (H31): a cc session's desired membership lives under pingbus/membership/, and
// names its seats as a ccy session's does, so allocation and the board count it too.
func TestReadDesiredReadsCCSessionsToo(t *testing.T) {
	runtime := t.TempDir()
	teamtest.MustWrite(t, filepath.Join(membership.Dir(runtime, "ccy-a"), membership.File),
		`{"v":1,"session":"ccy-a","kind":"ccy","checkout":"/h","generation":1,"written_ms":1,"seats":[],"farewell":[]}`)
	teamtest.MustWrite(t, filepath.Join(membership.CCDir(runtime, "cc-b"), membership.File),
		`{"v":1,"session":"cc-b","kind":"cc","checkout":"/h","generation":3,"written_ms":1,"seats":[{"team":"alpha","seat":"2"}],"farewell":[]}`)
	teamtest.MustWrite(t, filepath.Join(membership.CCDir(runtime, "cc-c"), membership.File),
		`{"v":1,"session":"cc-c","kind":"ccy","checkout":"/h","generation":1,"written_ms":1,"seats":[],"farewell":[]}`)
	desired, unreadable, err := ReadDesired(runtime)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(desired))
	for _, d := range desired {
		names = append(names, d.Session+":"+d.Kind)
	}
	if !reflect.DeepEqual(names, []string{"ccy-a:ccy", "cc-b:cc"}) {
		t.Fatalf("desired %v", names)
	}
	if len(unreadable) != 1 || !strings.Contains(unreadable[0], "cc-c") {
		t.Fatalf("a ccy membership among cc's is unreadable there: %v", unreadable)
	}
	running, _, err := RunningCC(runtime, func(name string) bool { return name != "cc-gone" })
	if err != nil || len(running) != 1 || running[0].Session != "cc-b" {
		t.Fatalf("running cc %v %v", running, err)
	}
	if running, _, err = RunningCC(runtime, func(string) bool { return false }); err != nil || len(running) != 0 {
		t.Fatalf("a cc session not on ccy's tmux is not running: %v %v", running, err)
	}
}

func TestReadDesiredOfNoDirectoryIsNothing(t *testing.T) {
	desired, unreadable, err := ReadDesired(t.TempDir())
	if err != nil || len(desired) != 0 || len(unreadable) != 0 {
		t.Fatalf("%v %v %v", desired, unreadable, err)
	}
	if _, _, err := ReadDesired("relative"); err == nil {
		t.Fatal("a relative runtime dir is refused")
	}
}

func TestTeamsListIsTheLastBeforeDoubleDash(t *testing.T) {
	if got := TeamsList([]string{"--teams", "1@a", "--teams=2@a", "--", "--teams", "3@a"}); got != "2@a" {
		t.Fatalf("got %q", got)
	}
}
