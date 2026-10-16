package membership

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// stamp is the fixed clock the tests write with: 1760000000000 ms.
var stamp = time.UnixMilli(1760000000000)

type rig struct {
	*teamtest.World

	init    *Init
	runtime string
}

func newRig(t *testing.T) rig {
	t.Helper()
	w := teamtest.New(t)
	runtime := filepath.Join(t.TempDir(), "run-user")
	if err := os.Mkdir(runtime, 0o700); err != nil {
		t.Fatal(err)
	}
	w.Env["XDG_RUNTIME_DIR"] = runtime
	m := New(w.Host)
	m.Now = func() time.Time { return stamp }
	return rig{World: w, init: m, runtime: runtime}
}

func (r rig) dir(session string) string {
	return filepath.Join(r.runtime, "ccy", "membership", session)
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func mode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode()
}

func TestInitWritesGenerationOneWithTheLaunchSeatsAndPrintsTheDirectory(t *testing.T) {
	r := newRig(t)
	code := r.init.Run(t.Context(), []string{"ccy-proj-1", "/home/u/code/proj", "dev1@team-a,2@team-b"})
	if code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	want := `{"v":1,"session":"ccy-proj-1","kind":"ccy","checkout":"/home/u/code/proj","generation":1,` +
		`"written_ms":1760000000000,"seats":[{"team":"team-a","seat":"dev1"},{"team":"team-b","seat":"2"}],` +
		`"farewell":[]}` + "\n"
	if got := readFile(t, filepath.Join(r.dir("ccy-proj-1"), "membership.json")); got != want {
		t.Fatalf("membership.json\n got %s\nwant %s", got, want)
	}
	if got := r.Out.String(); got != r.dir("ccy-proj-1")+"\n" {
		t.Fatalf("stdout %q", got)
	}
	if r.ErrOut.Len() != 0 {
		t.Fatalf("stderr %q", r.ErrOut.String())
	}
}

func TestInitModesAreTheOwnersAlone(t *testing.T) {
	r := newRig(t)
	if code := r.init.Run(t.Context(), []string{"s", "/c", ""}); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	for _, p := range []string{filepath.Join(r.runtime, "ccy"), filepath.Join(r.runtime, "ccy", "membership"), r.dir("s")} {
		if got := mode(t, p); got != os.ModeDir|0o700 {
			t.Fatalf("%s mode %v", p, got)
		}
	}
	if got := mode(t, filepath.Join(r.dir("s"), "membership.json")); got != 0o600 {
		t.Fatalf("file mode %v", got)
	}
}

func TestInitWithNoSeatsWritesAnEmptyMembership(t *testing.T) {
	r := newRig(t)
	if code := r.init.Run(t.Context(), []string{"ccy-proj-2", "/c", ""}); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	if got := readFile(t, filepath.Join(r.dir("ccy-proj-2"), "membership.json")); !strings.Contains(got, `"seats":[],"farewell":[]}`) {
		t.Fatalf("membership.json %s", got)
	}
}

func TestInitReplacesWhatAnEarlierLaunchLeft(t *testing.T) {
	r := newRig(t)
	teamtest.MustWrite(t, filepath.Join(r.dir("s"), "membership.json"), `{"v":1,"generation":9}`)
	if err := os.Chmod(r.dir("s"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := r.init.Run(t.Context(), []string{"s", "/c", "1@a"}); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	if got := readFile(t, filepath.Join(r.dir("s"), "membership.json")); !strings.Contains(got, `"generation":1,`) {
		t.Fatalf("membership.json %s", got)
	}
	if got := mode(t, r.dir("s")); got != os.ModeDir|0o700 {
		t.Fatalf("dir mode %v", got)
	}
}

func TestInitRefusesASessionDirectoryThatIsASymlink(t *testing.T) {
	r := newRig(t)
	elsewhere := t.TempDir()
	if err := os.MkdirAll(filepath.Join(r.runtime, "ccy", "membership"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, r.dir("s")); err != nil {
		t.Fatal(err)
	}
	if code := r.init.Run(t.Context(), []string{"s", "/c", ""}); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(r.ErrOut.String(), "not a directory") {
		t.Fatalf("stderr %q", r.ErrOut.String())
	}
	if _, err := os.Stat(filepath.Join(elsewhere, "membership.json")); err == nil {
		t.Fatal("wrote through the symlink")
	}
}

func TestInitNeedsAnAbsoluteRuntimeDirectory(t *testing.T) {
	for _, value := range []string{"", "run/user/1000"} {
		r := newRig(t)
		r.Env["XDG_RUNTIME_DIR"] = value
		if code := r.init.Run(t.Context(), []string{"s", "/c", ""}); code != term.ExitFailure {
			t.Fatalf("%q: exit %d", value, code)
		}
		if !strings.Contains(r.ErrOut.String(), "XDG_RUNTIME_DIR") || r.Out.Len() != 0 {
			t.Fatalf("%q: stderr %q stdout %q", value, r.ErrOut.String(), r.Out.String())
		}
	}
}

func TestInitRefusesWhatIsNotItsThreeArguments(t *testing.T) {
	cases := map[string][]string{
		"no arguments":             nil,
		"two arguments":            {"s", "/c"},
		"four arguments":           {"s", "/c", "", "x"},
		"an empty session":         {"", "/c", ""},
		"a session with a slash":   {"a/b", "/c", ""},
		"a session from a dot":     {".s", "/c", ""},
		"a session with a space":   {"a b", "/c", ""},
		"a control in the session": {"s\x01", "/c", ""},
		"a relative checkout":      {"s", "c", ""},
		"an unclean checkout":      {"s", "/c/../d", ""},
		"a team alone":             {"s", "/c", "team-a"},
		"two @":                    {"s", "/c", "1@a@b"},
		"an upper-case team":       {"s", "/c", "1@Team"},
		"a seat that is not one":   {"s", "/c", "0@a"},
		"an empty item":            {"s", "/c", "1@a,,2@b"},
		"a team twice":             {"s", "/c", "1@a,2@a"},
		"spaces around an item":    {"s", "/c", " 1@a"},
		"a trailing comma":         {"s", "/c", "1@a,"},
		"a seven-digit seat":       {"s", "/c", "1000000@a"},
		"a 25-character team":      {"s", "/c", "1@" + strings.Repeat("a", 25)},
		"a newline in the seat":    {"s", "/c", "1\n@a"},
	}
	for name, args := range cases {
		r := newRig(t)
		if code := r.init.Run(t.Context(), args); code != term.ExitUsage {
			t.Fatalf("%s: exit %d", name, code)
		}
		if r.Out.Len() != 0 || !strings.Contains(r.ErrOut.String(), "membership-init") {
			t.Fatalf("%s: stdout %q stderr %q", name, r.Out.String(), r.ErrOut.String())
		}
		if _, err := os.Stat(filepath.Join(r.runtime, "ccy")); err == nil {
			t.Fatalf("%s: wrote before refusing", name)
		}
	}
}

// The seat and team grammar is pingbus's (helpers/pingbus/protocol.py); this package holds a
// copy so the launcher's list is checked before anything is written. The copy is pinned here.
func TestTheGrammarIsPingbusOwn(t *testing.T) {
	src := readFile(t, filepath.Join(repoRoot(t), "helpers/pingbus/protocol.py"))
	for name, ours := range map[string]string{"HANDLE_SEAT_PATTERN": seatPattern, "TEAM_NAME_PATTERN": teamPattern} {
		m := regexp.MustCompile(name + ` = r"([^"]+)"`).FindStringSubmatch(src)
		if m == nil {
			t.Fatalf("%s not found in protocol.py", name)
		}
		if m[1] != ours {
			t.Fatalf("%s is %q in protocol.py, %q here", name, m[1], ours)
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for dir := wd; dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		if _, statErr := os.Stat(filepath.Join(dir, "helpers/pingbus/protocol.py")); statErr == nil {
			return dir
		}
	}
	t.Fatalf("repo root not found above %s", wd)
	return ""
}
