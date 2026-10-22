package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/shellword"
)

func args(t *testing.T, uidMap string) string {
	t.Helper()
	ranges, err := ParseUIDMap(uidMap)
	if err != nil {
		t.Fatalf("ParseUIDMap(%q): %v", uidMap, err)
	}
	return strings.Join(ForeignOwnerArgs(ranges), " ")
}

// The pure cases of scripts/test-ccy-relabel-preflight.bash (relabel_foreign_owner_args).
func TestForeignOwnerArgs(t *testing.T) {
	desktop := "         0       1000          1\n         1     524288      65536\n"
	cases := []struct {
		name, uidMap, want string
	}{
		{"a desktop's map: you, or your subordinate range", desktop, "! ( -uid 1000 -o ( -uid +524287 -uid -589824 ) )"},
		{"a range starting at uid 0 has no lower bound", "0 0 10", "! ( -uid -10 )"},
		{"a one-uid range at 0 is that uid", "0 0 1", "! ( -uid 0 )"},
		{"blank lines are not ranges", "\n0 0 1\n\n", "! ( -uid 0 )"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := args(t, c.uidMap); got != c.want {
				t.Fatalf("ForeignOwnerArgs = %q, want %q", got, c.want)
			}
		})
	}
}

func TestParseUIDMapRefusesWhatIsNotAMap(t *testing.T) {
	cases := []struct{ name, uidMap string }{
		{"an empty map is refused, not read as everyone is foreign", ""},
		{"whitespace only", " \n\t\n"},
		{"error text where the map should be is refused", "Error: cannot connect to podman"},
		{"a line short of its count is refused", "0 1000"},
		{"a line with a fourth field", "0 1000 1 7"},
		{"a negative number", "0 -1 1"},
		// The kernel never writes a range of no uids; such a line is not a map it wrote.
		{"a range of no uids", "0 1000 0"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseUIDMap(c.uidMap)
			if !errors.Is(err, ErrUnreadableUIDMap) {
				t.Fatalf("ParseUIDMap(%q) error = %v, want ErrUnreadableUIDMap", c.uidMap, err)
			}
		})
	}
}

func TestMapsSaysWhetherAUIDIsInsideTheNamespace(t *testing.T) {
	ranges, err := ParseUIDMap("0 1000 1\n1 524288 65536")
	if err != nil {
		t.Fatal(err)
	}
	cases := map[uint64]bool{
		1000: true, 524288: true, 589823: true,
		0: false, 999: false, 1001: false, 524287: false, 589824: false,
	}
	for uid, want := range cases {
		if got := Maps(ranges, uid); got != want {
			t.Errorf("Maps(%d) = %v, want %v", uid, got, want)
		}
	}
}

func TestRelabelBlocks(t *testing.T) {
	other := "unconfined_u:object_r:user_home_t:s0"
	cases := []struct {
		name    string
		top     string
		entries []string
		want    bool
	}{
		{"no foreign entries", other, nil, false},
		{"a foreign entry in a project not yet labelled", other, []string{other}, true},
		{"foreign entries already labelled, in a labelled project: podman skips them", SharedFileLabel, []string{SharedFileLabel, SharedFileLabel}, false},
		{"the same entries in a project not yet labelled: podman relabels every entry", other, []string{SharedFileLabel}, true},
		{"one foreign entry without the label in a labelled project", SharedFileLabel, []string{SharedFileLabel, other}, true},
		{"stat's error text is not the label", SharedFileLabel, []string{"stat: cannot statx"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := RelabelBlocks(c.top, c.entries); got != c.want {
				t.Fatalf("RelabelBlocks = %v, want %v", got, c.want)
			}
		})
	}
}

func TestNeedsRelabelCheck(t *testing.T) {
	cases := []struct {
		name    string
		relabel string
		kind    Kind
		want    bool
	}{
		{"podman with the shared relabel", "z", Podman, true},
		{"no relabel (SELinux off, or a forwarded agent): nothing to check", "", Podman, false},
		{"docker: no podman unshare to read a map from", "z", Docker, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := NeedsRelabelCheck(c.relabel, c.kind); got != c.want {
				t.Fatalf("NeedsRelabelCheck = %v, want %v", got, c.want)
			}
		})
	}
}

func TestLookAndFixCommands(t *testing.T) {
	ranges, err := ParseUIDMap("0 1007 1\n1 200000 100")
	if err != nil {
		t.Fatal(err)
	}
	preds := ForeignOwnerArgs(ranges)
	dir := "/home/user/a project"
	wantPreds := `'!' '(' -uid 1007 -o '(' -uid +199999 -uid -200100 ')' ')'`
	look := LookCommand(dir, preds)
	if want := "find '/home/user/a project' " + wantPreds + ` -printf '%U:%G  %p\n'`; look != want {
		t.Fatalf("LookCommand =\n %s\nwant\n %s", look, want)
	}
	fix := FixCommand(dir, preds, 1000, 1000)
	if want := "sudo find '/home/user/a project' " + wantPreds + " -exec chown -h 1000:1000 '{}' +"; fix != want {
		t.Fatalf("FixCommand =\n %s\nwant\n %s", fix, want)
	}
	argv := FixArgv(dir, preds, 1000, 1000)
	wantArgv := append(append([]string{"find", dir}, preds...), "-exec", "chown", "-h", "1000:1000", "{}", "+")
	if !slices.Equal(argv, wantArgv) {
		t.Fatalf("FixArgv =\n %q\nwant\n %q", argv, wantArgv)
	}
	if fix != "sudo "+quoteAll(argv) {
		t.Fatalf("the printed fix is not the argv it runs: %q vs %q", fix, argv)
	}
}

// The fix argv is run by find itself, with a chown on PATH that records its arguments: find
// must accept it (a missing `+` is "missing argument to -exec") and hand it every foreign entry.
func TestFixArgvRunsUnderFind(t *testing.T) {
	bin := t.TempDir()
	record := filepath.Join(t.TempDir(), "chown-args")
	fake := "#!/bin/sh\nprintf '%s\\n' \"$@\" >> " + shellword.Quote(record) + "\n"
	chown := filepath.Join(bin, "chown")
	if err := os.WriteFile(chown, []byte(fake), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Chmod(chown, 0o700); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "entry"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	// A map of one uid no account has, so every entry the test made is foreign.
	ranges := []Range{{Inside: 0, Outside: 4000000000, Count: 1}}
	argv := FixArgv(dir, ForeignOwnerArgs(ranges), 1000, 1000)
	if argv[0] != "find" {
		t.Fatalf("FixArgv runs %q, want find", argv[0])
	}
	res, err := execx.OS{}.Run(context.Background(), execx.Cmd{
		Name: "find", Args: argv[1:], Env: []string{"PATH=" + bin + ":" + os.Getenv("PATH")},
	})
	if err != nil {
		t.Fatalf("find %q: %v (stderr %q)", argv[1:], err, res.Stderr)
	}
	got, err := os.ReadFile(filepath.Clean(record))
	if err != nil {
		t.Fatalf("the fake chown never ran: %v", err)
	}
	want := "-h\n1000:1000\n" + dir + "\n" + filepath.Join(dir, "entry") + "\n"
	if string(got) != want {
		t.Fatalf("chown got\n%s\nwant\n%s", got, want)
	}
}
