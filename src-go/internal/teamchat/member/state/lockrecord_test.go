package state

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/homefs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/lock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

func TestDecodeLockRecord(t *testing.T) {
	rec, err := DecodeLockRecord([]byte(`{"v":1,"run":"0123456789abcdef","started_ms":5}`))
	if err != nil || rec.Run.String() != "0123456789abcdef" || rec.StartedMS != 5 || rec.LaunchFailed() {
		t.Fatalf("DecodeLockRecord = %+v, %v", rec, err)
	}
	rec, err = DecodeLockRecord([]byte(`{"v":1,"run":"0123456789abcdef","started_ms":5,"launch_failed_ms":9}`))
	if err != nil || !rec.LaunchFailed() || *rec.LaunchFailedMS != 9 {
		t.Fatalf("a launch failure = %+v, %v", rec, err)
	}
	for _, bad := range []string{
		``, `{}`, `[]`, `{"v":2,"run":"0123456789abcdef","started_ms":5}`,
		`{"v":1,"run":"0123456789ABCDEF","started_ms":5}`,
		`{"v":1,"run":"0123456789abcdef","started_ms":5,"x":1}`, `{"v":1,"run":"0123456789abcdef"}`,
		`{"v":1,"run":"0123456789abcdef","started_ms":null}`,
		`{"v":1,"run":"0123456789abcdef","started_ms":5,"launch_failed_ms":null}`,
		`{"v":1,"run":"0123456789abcdef","started_ms":-1}`,
		`{"v":1,"run":"0123456789abcdef","started_ms":5,"launch_failed_ms":-1}`,
		`{"v":1,"run":"0123456789abcdef","started_ms":5.5}`,
		`{"v":1,"run":"0123456789abcdef","started_ms":true}`,
		`{"v":1,"run":"0123456789abcdef","STARTED_MS":5}`,
		`{"v":1,"run":"0123456789abcdef","started_ms":5,"Started_ms":6}`,
		`{"v":1,"run":"0123456789abcdef","started_ms":5,"started_ms":6}`,
		`{"v":1,"run":"0123456789abcdef","started_ms":5} {}`,
		`{"v":1,"run":"0123456789abcdef","started_ms":5,"pad":"` + strings.Repeat("x", MaxLockRecord) + `"}`,
	} {
		if _, err := DecodeLockRecord([]byte(bad)); !errors.Is(err, ErrInvalid) {
			t.Errorf("DecodeLockRecord(%q) = %v, want ErrInvalid", bad, err)
		}
	}
}

func TestLockRecordRoundTrip(t *testing.T) {
	failed := int64(7)
	for _, rec := range []LockRecord{
		{Run: runID(t), StartedMS: 3},
		{Run: runID(t), StartedMS: 3, LaunchFailedMS: &failed},
	} {
		data, err := rec.Encode()
		check(t, err)
		got, err := DecodeLockRecord(data)
		check(t, err)
		if got.Run != rec.Run || got.StartedMS != rec.StartedMS || got.LaunchFailed() != rec.LaunchFailed() {
			t.Fatalf("round trip of %+v = %+v", rec, got)
		}
	}
	if _, err := (LockRecord{StartedMS: 3}).Encode(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a record with no run encoded: %v", err)
	}
}

// TestInvalidNamesNoValue holds B10: a refusal names the file and the rule, never a value.
func TestInvalidNamesNoValue(t *testing.T) {
	for _, bad := range []string{
		`{"v":1,"run":"SECRETSECRETSECR","started_ms":5}`,
		`{"v":1,"run":"0123456789abcdef","started_ms":"SECRET"}`,
		`{"v":1,"run":"0123456789abcdef","started_ms":5,"SECRET":1}`,
		`{"v":1,"run":"0123456789abcdef","started_ms":5,"launch_failed_ms":-424242}`,
		`{"v":"SECRET"}`, `"SECRET`,
	} {
		_, err := DecodeLockRecord([]byte(bad))
		var inv *InvalidError
		if !errors.As(err, &inv) || inv.File != "seat.lock" {
			t.Fatalf("DecodeLockRecord(%q) = %v, want an InvalidError for seat.lock", bad, err)
		}
		if strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), "424242") {
			t.Errorf("the error quotes the file: %v", err)
		}
	}
}

func TestInvalidCodes(t *testing.T) {
	for _, c := range []struct{ data, code string }{
		{`{"v":1,`, CodeJSON},
		{`{"v":2,"run":"0123456789abcdef","started_ms":5}`, CodeVersion},
		{`{"run":"0123456789abcdef","started_ms":5}`, CodeVersion},
		{`[1]`, CodeSchema},
		{`{"v":1,"run":"0123456789abcdef"}`, CodeSchema},
	} {
		_, err := DecodeLockRecord([]byte(c.data))
		var inv *InvalidError
		if !errors.As(err, &inv) || inv.Code != c.code {
			t.Errorf("DecodeLockRecord(%q) = %v, want code %s", c.data, err, c.code)
		}
	}
}

func TestWriteLockRecord(t *testing.T) {
	w := newWorld(t)
	k, err := lock.TakeKeeper(t.Context(), noSleep{}, w.root, w.run)
	check(t, err)
	t.Cleanup(func() { check(t, k.Release()) })
	seat, err := k.TakeSeat(t.Context(), w.root, w.seat)
	check(t, err)
	t.Cleanup(func() { check(t, seat.Close()) })
	failed := int64(9)
	check(t, WriteLockRecord(seat, LockRecord{Run: runID(t), StartedMS: 5}))
	check(t, WriteLockRecord(seat, LockRecord{Run: runID(t), StartedMS: 5, LaunchFailedMS: &failed}))
	data, err := os.ReadFile(w.seat.Lock())
	check(t, err)
	rec, err := DecodeLockRecord(data)
	if err != nil || !rec.LaunchFailed() {
		t.Fatalf("the lock's record = %q, %v", data, err)
	}
	if err := WriteLockRecord(seat, LockRecord{StartedMS: 5}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("an invalid record was written: %v", err)
	}
}

// world is a member home with a run and one seat directory.
type world struct {
	root *homefs.Root
	run  paths.Run
	seat paths.Seat
	team paths.TeamID
}

func newWorld(t *testing.T) *world {
	t.Helper()
	home := filepath.Join(t.TempDir(), "home")
	check(t, os.Mkdir(home, 0o700))
	root, err := homefs.Open(home, homefs.Private)
	check(t, err)
	t.Cleanup(func() { check(t, root.Close()) })
	layout, err := paths.HomeLayout(home, home+"/runtime")
	check(t, err)
	team := teamID(t)
	w := &world{root: root, run: layout.Run(runID(t)), seat: layout.Seat(team), team: team}
	check(t, root.MkdirAll(w.run.Dir()))
	check(t, root.MkdirAll(w.seat.Dir()))
	return w
}

func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func runID(t *testing.T) paths.RunID {
	t.Helper()
	r, err := paths.ParseRunID("0123456789abcdef")
	check(t, err)
	return r
}

func teamID(t *testing.T) paths.TeamID {
	t.Helper()
	team, err := paths.ParseTeamID("alpha")
	check(t, err)
	return team
}
