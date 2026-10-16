package registry

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestDirPrefersCCYStateDir(t *testing.T) {
	got, err := Dir(env(map[string]string{"CCY_STATE_DIR": "/s", "XDG_STATE_HOME": "/x", "HOME": "/h"}))
	if err != nil || got != "/s/sessions" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestDirFallsBackToXDGThenHome(t *testing.T) {
	got, err := Dir(env(map[string]string{"XDG_STATE_HOME": "/x", "HOME": "/h"}))
	if err != nil || got != "/x/ccy/sessions" {
		t.Fatalf("xdg: got %q, %v", got, err)
	}
	got, err = Dir(env(map[string]string{"HOME": "/h"}))
	if err != nil || got != "/h/.local/state/ccy/sessions" {
		t.Fatalf("home: got %q, %v", got, err)
	}
}

func TestDirRefusesRelativeBases(t *testing.T) {
	cases := []map[string]string{
		{"XDG_STATE_HOME": "rel", "HOME": "/h"},
		{"CCY_STATE_DIR": "rel"},
		{"HOME": "rel"},
		{},
	}
	for _, c := range cases {
		if got, err := Dir(env(c)); err == nil {
			t.Fatalf("%v: want error, got %q", c, got)
		}
	}
	_, relBase := Dir(env(map[string]string{"CCY_STATE_DIR": "rel"}))
	if wantBase := "session registry base 'rel' is not an absolute path."; relBase == nil || relBase.Error() != wantBase {
		t.Fatalf("got %v", relBase)
	}
	_, err := Dir(env(map[string]string{"XDG_STATE_HOME": "rel"}))
	want := "XDG_STATE_HOME='rel' is not absolute, so the session registry's location would depend on the current directory."
	if err == nil || err.Error() != want {
		t.Fatalf("got %v", err)
	}
}

func sample() Record {
	return Record{
		Name: "ccy-proj", Dir: "/srv/proj", Launcher: "/usr/local/bin/ccy", Prefix: "ccy",
		Restore: true, Compose: "started", Args: []string{"--teams", "1@alpha", "", "a=b"},
	}
}

func newStore(t *testing.T) *Store {
	t.Helper()
	return New(filepath.Join(t.TempDir(), "sessions"))
}

func TestWriteReadRoundTripAndFormat(t *testing.T) {
	s := newStore(t)
	rec := sample()
	if err := s.Write(rec); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(s.Dir(), rec.Name))
	if err != nil {
		t.Fatal(err)
	}
	want := "ccy-session-record 1\nname=ccy-proj\ndir=/srv/proj\nlauncher=/usr/local/bin/ccy\nprefix=ccy\nrestore=yes\ncompose=started\narg=--teams\narg=1@alpha\narg=\narg=a=b\n"
	if string(raw) != want {
		t.Fatalf("file:\n%q\nwant:\n%q", raw, want)
	}
	info, err := os.Stat(filepath.Join(s.Dir(), rec.Name))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v, %v", info.Mode(), err)
	}
	dirInfo, err := os.Stat(s.Dir())
	if err != nil || dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode %v, %v", dirInfo.Mode(), err)
	}
	got, err := s.Get(rec.Name)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != rec.Name || got.Restore != true || got.Compose != "started" || !slices.Equal(got.Args, rec.Args) {
		t.Fatalf("got %+v", got)
	}
}

func TestWriteOmitsEmptyComposeAndLeavesNoTemp(t *testing.T) {
	s := newStore(t)
	rec := sample()
	rec.Compose = ""
	rec.Restore = false
	if err := s.Write(rec); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(s.Dir(), rec.Name))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "compose=") || !strings.Contains(string(raw), "restore=no\n") {
		t.Fatalf("file %q", raw)
	}
	entries, err := os.ReadDir(s.Dir())
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries %v, %v", entries, err)
	}
}

func TestWriteRefusesBadRecords(t *testing.T) {
	cases := map[string]func(*Record){
		"empty name":      func(r *Record) { r.Name = "" },
		"slash in name":   func(r *Record) { r.Name = "a/b" },
		"empty dir":       func(r *Record) { r.Dir = "" },
		"empty launcher":  func(r *Record) { r.Launcher = "" },
		"empty prefix":    func(r *Record) { r.Prefix = "" },
		"newline in dir":  func(r *Record) { r.Dir = "a\nb" },
		"newline in arg":  func(r *Record) { r.Args = []string{"x\ny"} },
		"bad compose":     func(r *Record) { r.Compose = "maybe" },
		"newline in name": func(r *Record) { r.Name = "a\nb" },
	}
	for name, mutate := range cases {
		s := newStore(t)
		rec := sample()
		mutate(&rec)
		if err := s.Write(rec); err == nil {
			t.Fatalf("%s: want error", name)
		}
		if _, err := os.Stat(s.Dir()); err == nil {
			entries, readErr := os.ReadDir(s.Dir())
			if readErr != nil || len(entries) != 0 {
				t.Fatalf("%s: left %v", name, entries)
			}
		}
	}
}

func TestReadRejectsMalformedRecords(t *testing.T) {
	good := "ccy-session-record 1\nname=n\ndir=/d\nlauncher=/l\nprefix=ccy\nrestore=yes\n"
	cases := map[string]string{
		"empty":           "",
		"wrong header":    "ccy-session-record 2\nname=n\n",
		"no equals":       good + "junk\n",
		"unknown key":     good + "color=red\n",
		"bad compose":     good + "compose=maybe\n",
		"bad restore":     strings.Replace(good, "restore=yes", "restore=maybe", 1),
		"missing name":    strings.Replace(good, "name=n\n", "", 1),
		"missing dir":     strings.Replace(good, "dir=/d\n", "", 1),
		"missing prefix":  strings.Replace(good, "prefix=ccy\n", "", 1),
		"empty launcher":  strings.Replace(good, "launcher=/l", "launcher=", 1),
		"missing restore": strings.Replace(good, "restore=yes\n", "", 1),
	}
	for name, body := range cases {
		path := filepath.Join(t.TempDir(), "rec")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := ReadFile(path)
		if err == nil {
			t.Fatalf("%s: want error", name)
		}
		if !errors.Is(err, ErrUnreadable) {
			t.Fatalf("%s: not ErrUnreadable: %v", name, err)
		}
	}
}

func TestReadNamesTheFirstMissingFieldInRecordOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rec")
	body := "ccy-session-record 1\nprefix=ccy\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	for range 50 {
		_, err := ReadFile(path)
		if err == nil || !strings.Contains(err.Error(), "is missing its name field") {
			t.Fatalf("got %v", err)
		}
	}
	if err := os.WriteFile(path, []byte("ccy-session-record 1\nname=n\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for range 50 {
		_, err := ReadFile(path)
		if err == nil || !strings.Contains(err.Error(), "is missing its dir field") {
			t.Fatalf("got %v", err)
		}
	}
}

func TestValidateNamesTheFirstMissingFieldInOrder(t *testing.T) {
	for range 50 {
		err := validate(Record{Name: "n"})
		if err == nil || !strings.Contains(err.Error(), "needs a directory") {
			t.Fatalf("got %v", err)
		}
	}
}

func TestReadAcceptsBlankLinesAndMissingFinalNewline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rec")
	body := "ccy-session-record 1\n\nname=n\ndir=/d\nlauncher=/l\nprefix=cc\nrestore=no\narg=x"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	rec, err := ReadFile(path)
	if err != nil || rec.Prefix != "cc" || rec.Restore || !slices.Equal(rec.Args, []string{"x"}) {
		t.Fatalf("%+v, %v", rec, err)
	}
}

func TestGetReportsMissingRecord(t *testing.T) {
	s := newStore(t)
	if _, err := s.Get("nope"); !errors.Is(err, ErrNoRecord) {
		t.Fatalf("got %v", err)
	}
}

func TestSetRestoreKeepsEverythingElse(t *testing.T) {
	s := newStore(t)
	rec := sample()
	if err := s.Write(rec); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRestore(rec.Name, false); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(rec.Name)
	if err != nil {
		t.Fatal(err)
	}
	rec.Restore = false
	if got.Restore || got.Compose != rec.Compose || !slices.Equal(got.Args, rec.Args) || got.Dir != rec.Dir {
		t.Fatalf("got %+v", got)
	}
}

func TestSetRestoreFailures(t *testing.T) {
	s := newStore(t)
	if err := s.SetRestore("ghost", true); !errors.Is(err, ErrNoRecord) {
		t.Fatalf("missing: %v", err)
	}
	if err := os.MkdirAll(s.Dir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir(), "bad"), []byte("junk\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRestore("bad", true); !errors.Is(err, ErrUnreadable) {
		t.Fatalf("unreadable: %v", err)
	}
}

func TestSetTeamsReplacesTheRecordedTeamsAndKeepsTheRest(t *testing.T) {
	s := newStore(t)
	rec := sample()
	rec.Args = []string{"--token", "t", "--teams", "old@x", "--no-ssh", "--teams=more@y"}
	if err := s.Write(rec); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTeams(rec.Name, "dev1@team-a"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(rec.Name)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--token", "t", "--no-ssh", "--teams", "dev1@team-a"}
	if !slices.Equal(got.Args, want) || got.Compose != rec.Compose || got.Dir != rec.Dir || got.Restore != rec.Restore {
		t.Fatalf("got %+v, want args %v", got, want)
	}
}

func TestSetTeamsToNoneDropsTheFlag(t *testing.T) {
	s := newStore(t)
	rec := sample()
	rec.Args = []string{"--no-ssh", "--teams", "old@x"}
	if err := s.Write(rec); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTeams(rec.Name, ""); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(rec.Name)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Args, []string{"--no-ssh"}) {
		t.Fatalf("a session on no team replays no --teams: %v", got.Args)
	}
}

func TestSetTeamsWithNoRecordIsAnError(t *testing.T) {
	if err := newStore(t).SetTeams("ghost", "a@b"); !errors.Is(err, ErrNoRecord) {
		t.Fatalf("missing: %v", err)
	}
}

func TestRemoveIsIdempotent(t *testing.T) {
	s := newStore(t)
	if err := s.Remove("ghost"); err != nil {
		t.Fatalf("absent: %v", err)
	}
	if err := s.Write(sample()); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove("ccy-proj"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("ccy-proj"); !errors.Is(err, ErrNoRecord) {
		t.Fatalf("still there: %v", err)
	}
}

func TestListSeparatesReadableFromUnreadable(t *testing.T) {
	s := newStore(t)
	for _, n := range []string{"b", "a"} {
		rec := sample()
		rec.Name = n
		if err := s.Write(rec); err != nil {
			t.Fatal(err)
		}
	}
	for name, body := range map[string]string{"broken": "junk\n", "a.tmp.123": "ignored", "z.tmp.9": "ignored"} {
		if err := os.WriteFile(filepath.Join(s.Dir(), name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(list.Records))
	for _, r := range list.Records {
		names = append(names, r.Name)
	}
	if !slices.Equal(names, []string{"a", "b"}) || !slices.Equal(list.Unreadable, []string{"broken"}) {
		t.Fatalf("%v / %v", names, list.Unreadable)
	}
}

func TestListOfAbsentDirectoryIsEmpty(t *testing.T) {
	list, err := newStore(t).List()
	if err != nil || len(list.Records) != 0 || len(list.Unreadable) != 0 {
		t.Fatalf("%+v, %v", list, err)
	}
}

func TestLocateUsesEnvironment(t *testing.T) {
	s, err := Locate(env(map[string]string{"CCY_STATE_DIR": "/s"}))
	if err != nil || s.Dir() != "/s/sessions" {
		t.Fatalf("%v, %v", s, err)
	}
	if _, err := Locate(env(nil)); err == nil {
		t.Fatal("want error")
	}
}

// --- format compatibility with the bash library, both directions ---

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for dir := wd; dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		if _, statErr := os.Stat(filepath.Join(dir, "files/var/local/claude-yolo/lib/session-registry.bash")); statErr == nil {
			return dir
		}
	}
	t.Fatalf("repo root not found above %s", wd)
	return ""
}

// bash runs a snippet with the real registry library sourced and print_error stubbed.
func bash(t *testing.T, stateDir, snippet string) string {
	t.Helper()
	lib := filepath.Join(repoRoot(t), "files/var/local/claude-yolo/lib/session-registry.bash")
	script := "set -euo pipefail\nprint_error() { printf 'ERROR: %s\\n' \"$*\" >&2; }\nsource \"$1\"\n" + snippet
	res, err := execx.OS{}.Run(context.Background(), execx.Cmd{
		Name: "bash", Args: []string{"-c", script, "bash", lib},
		Env: []string{"CCY_STATE_DIR=" + stateDir},
	})
	if err != nil {
		t.Fatalf("bash: %v\nstderr: %s", err, res.Stderr)
	}
	return string(res.Stdout)
}

func TestBashWritesGoReads(t *testing.T) {
	state := t.TempDir()
	bash(t, state, `ccy_registry_write --compose running ccy-proj /srv/proj /usr/local/bin/ccy ccy no --teams 1@alpha "" "two words" --x=y`)
	s, err := Locate(env(map[string]string{"CCY_STATE_DIR": state}))
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Get("ccy-proj")
	if err != nil {
		t.Fatal(err)
	}
	wantArgs := []string{"--teams", "1@alpha", "", "two words", "--x=y"}
	if got.Dir != "/srv/proj" || got.Launcher != "/usr/local/bin/ccy" || got.Prefix != "ccy" ||
		got.Restore || got.Compose != "running" || !slices.Equal(got.Args, wantArgs) {
		t.Fatalf("got %+v", got)
	}
	bash(t, state, `ccy_registry_write cc-other /d /l cc yes`)
	list, err := s.List()
	if err != nil || len(list.Records) != 2 || len(list.Unreadable) != 0 {
		t.Fatalf("%+v, %v", list, err)
	}
}

func TestGoWritesBashReads(t *testing.T) {
	state := t.TempDir()
	s, err := Locate(env(map[string]string{"CCY_STATE_DIR": state}))
	if err != nil {
		t.Fatal(err)
	}
	rec := sample()
	if err := s.Write(rec); err != nil {
		t.Fatal(err)
	}
	out := bash(t, state, `ccy_registry_read "$(ccy_registry_dir)/ccy-proj"
printf '%s|%s|%s|%s|%s|%s|%s\n' "$REC_NAME" "$REC_DIR" "$REC_LAUNCHER" "$REC_PREFIX" "$REC_RESTORE" "$REC_COMPOSE" "${REC_ARGS[*]}"`)
	if out != "ccy-proj|/srv/proj|/usr/local/bin/ccy|ccy|yes|started|--teams 1@alpha  a=b\n" {
		t.Fatalf("bash saw %q", out)
	}
}

func TestBashSetRestoreOnGoRecordAndGoSetRestoreOnBashRecord(t *testing.T) {
	state := t.TempDir()
	s, err := Locate(env(map[string]string{"CCY_STATE_DIR": state}))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Write(sample()); err != nil {
		t.Fatal(err)
	}
	bash(t, state, `ccy_registry_set_restore ccy-proj no`)
	got, err := s.Get("ccy-proj")
	if err != nil || got.Restore || !slices.Equal(got.Args, sample().Args) {
		t.Fatalf("%+v, %v", got, err)
	}
	bash(t, state, `ccy_registry_write --compose declined other /d /l ccy no --teams 2@alpha`)
	if err = s.SetRestore("other", true); err != nil {
		t.Fatal(err)
	}
	out := bash(t, state, `ccy_registry_read "$(ccy_registry_dir)/other"; printf '%s %s %s\n' "$REC_RESTORE" "$REC_COMPOSE" "${REC_ARGS[*]}"`)
	if out != "yes declined --teams 2@alpha\n" {
		t.Fatalf("bash saw %q", out)
	}
	bash(t, state, `ccy_registry_remove other`)
	if _, err := s.Get("other"); !errors.Is(err, ErrNoRecord) {
		t.Fatalf("got %v", err)
	}
}
