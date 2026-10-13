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

const sep = "\x1f"

func joinArgs(args []string) string { return strings.Join(args, "|") }

func TestRestoreArgsGoldenCases(t *testing.T) {
	cases := []struct {
		name    string
		compose string
		prefix  string
		args    []string
		want    string
	}{
		{"ccy appends supervise and continue", "", "ccy", []string{"--token", "work"}, "--token|work|--supervise|--continue"},
		{"supervise not doubled", "", "ccy", []string{"--supervise", "--token", "work"}, "--supervise|--token|work|--continue"},
		{"no-supervise respected", "", "ccy", []string{"--no-supervise"}, "--no-supervise|--continue"},
		{"continue not doubled", "", "ccy", []string{"--continue"}, "--continue|--supervise"},
		{"short continue", "", "ccy", []string{"-c"}, "-c|--supervise"},
		{"resume names a conversation", "", "ccy", []string{"--resume", "abc"}, "--resume|abc|--supervise"},
		{"short resume", "", "ccy", []string{"-r", "abc"}, "-r|abc|--supervise"},
		{"cc only continue", "", "cc", []string{"--model", "opus"}, "--model|opus|--continue"},
		{"empty in", "", "ccy", nil, "--supervise|--continue"},
		{"supervise goes before a recorded dash dash", "", "ccy", []string{"--token", "t", "--", "--model", "opus"}, "--token|t|--supervise|--|--model|opus|--continue"},
		{"supervise after dash dash is claudes", "", "ccy", []string{"--", "--supervise"}, "--supervise|--|--supervise|--continue"},
		{"continue after dash dash counts", "", "ccy", []string{"--", "--continue"}, "--supervise|--|--continue"},
		{"compose started", "started", "ccy", []string{"--token", "t"}, "--token|t|--compose|start|--supervise|--continue"},
		{"compose running", "running", "ccy", nil, "--compose|start|--supervise|--continue"},
		{"compose declined", "declined", "ccy", []string{"--"}, "--compose|skip|--supervise|--|--continue"},
		{"compose is ccys flag only", "started", "cc", []string{"-x"}, "-x|--continue"},
	}
	for _, c := range cases {
		got, err := RestoreArgs(c.compose, c.prefix, c.args)
		if err != nil || joinArgs(got) != c.want {
			t.Errorf("%s: got %q, %v; want %q", c.name, joinArgs(got), err, c.want)
		}
	}
}

func TestRestoreArgsErrors(t *testing.T) {
	if _, err := RestoreArgs("maybe", "ccy", nil); err == nil || err.Error() != "compose outcome 'maybe' is not one of started declined running" {
		t.Fatalf("got %v", err)
	}
	if _, err := RestoreArgs("", "", nil); err == nil {
		t.Fatal("an empty prefix is an error")
	}
}

func TestRestoreArgsAgreesWithBash(t *testing.T) {
	inputs := []struct {
		compose string
		prefix  string
		args    []string
	}{
		{"", "ccy", []string{"--teams", "1@alpha", "", "two words"}},
		{"started", "ccy", []string{"--token", "x", "--", "--supervise", "-c"}},
		{"declined", "ccy", []string{"--no-supervise", "--resume", "id"}},
		{"", "cc", []string{"--", "a"}},
		{"running", "ccy", []string{"-r"}},
	}
	for _, in := range inputs {
		script := "ccy_registry_restore_args"
		if in.compose != "" {
			script += " --compose " + in.compose
		}
		script += ` "$@" | tr '\n' '\037'`
		args := append([]string{in.prefix}, in.args...)
		out := bashWithArgs(t, script, args...)
		got, err := RestoreArgs(in.compose, in.prefix, in.args)
		if err != nil {
			t.Fatal(err)
		}
		if want := strings.Join(got, sep) + sep; out != want {
			t.Errorf("%v: bash %q, go %q", in, out, want)
		}
	}
}

func TestFlagClassAgreesWithBash(t *testing.T) {
	words := []string{
		"--rebuild", "--rebuild=1", "--update-token=x", "--update-token", "--teams=1@a", "--teams", "--token", "--no-ssh",
		"--supervise", "--compose", "--no-restore", "-h", "--model", "plain", "--connect", "--engine", "--until",
	}
	for _, w := range words {
		out := strings.TrimSpace(bashWithArgs(t, `ccy_registry_flag_class "$1"`, w))
		if got := FlagClass(w); string(got) != out {
			t.Errorf("%s: bash %q, go %q", w, out, got)
		}
	}
}

func TestResumeIDAndSupervisedGolden(t *testing.T) {
	idCases := []struct {
		args []string
		want string
	}{
		{[]string{"--token", "work", "--supervise", "--continue"}, ""},
		{[]string{"--token", "work", "--", "--resume", "0a1b-2c"}, "0a1b-2c"},
		{[]string{"--resume=0a1b"}, "0a1b"},
		{[]string{"--supervise", "-r", "0a1b"}, "0a1b"},
		{[]string{"--token", "-r", "--continue"}, ""},
		{[]string{"--resume"}, ""},
		{[]string{"--resume", "a", "--resume=b"}, "b"},
		{[]string{"--", "--token", "-r", "x"}, "x"},
		{[]string{"--model", "-r", "x"}, "x"},
	}
	for _, c := range idCases {
		if got := ResumeID(c.args); got != c.want {
			t.Errorf("%v: got %q, want %q", c.args, got, c.want)
		}
		out := strings.TrimSpace(bashWithArgs(t, `ccy_registry_resume_id "$@"`, c.args...))
		if out != c.want {
			t.Errorf("%v: bash %q, want %q", c.args, out, c.want)
		}
	}
	supCases := []struct {
		prefix string
		args   []string
		want   bool
	}{
		{"ccy", []string{"--token", "work", "--supervise", "--continue"}, true},
		{"ccy", []string{"--no-supervise", "--continue"}, false},
		{"ccy", []string{"--supervise", "--", "--no-supervise"}, true},
		{"ccy", []string{"--no-supervise", "--supervise"}, true},
		{"cc", []string{"--model", "opus", "--continue"}, false},
	}
	for _, c := range supCases {
		if got := Supervised(c.prefix, c.args); got != c.want {
			t.Errorf("%s %v: got %v", c.prefix, c.args, got)
		}
		want := "no"
		if c.want {
			want = "yes"
		}
		out := strings.TrimSpace(bashWithArgs(t, `ccy_registry_supervised "$@"`, append([]string{c.prefix}, c.args...)...))
		if out != want {
			t.Errorf("%s %v: bash %q", c.prefix, c.args, out)
		}
	}
}

func TestTrampolineIsByteIdenticalToBashForPlainPaths(t *testing.T) {
	for _, path := range []string{"/home/u/.local/state/ccy/sessions/ccy-proj", "/x/y_z-1.2/ccy@a+b:c=e%f"} {
		got := Trampoline(path, "ccy")
		want := bashWithArgs(t, `ccy_registry_trampoline "$1" "$2"`, path, "ccy")
		if got != want {
			t.Errorf("go %q, bash %q", got, want)
		}
	}
}

func TestTrampolineBehaviour(t *testing.T) {
	dir := t.TempDir()
	run := func(rec string, cmd ...string) (string, int) {
		t.Helper()
		if err := os.WriteFile(rec, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		res, err := execx.OS{}.Run(context.Background(), execx.Cmd{
			Name: "bash", Args: append([]string{"-c", Trampoline(rec, "ccy"), "ccy-tmux"}, cmd...),
			Stdin: strings.NewReader("\n"),
		})
		code := 0
		if exitErr, ok := errors.AsType[*execx.ExitError](err); ok {
			code = exitErr.Code
		} else if err != nil {
			t.Fatal(err)
		}
		if _, statErr := os.Stat(rec); statErr == nil {
			t.Errorf("the record %s was not removed", rec)
		}
		return string(res.Stdout) + string(res.Stderr), code
	}
	if out, code := run(filepath.Join(dir, "ccy-clean"), "true"); out != "" || code != 0 {
		t.Fatalf("clean: %q %d", out, code)
	}
	out, code := run(filepath.Join(dir, "ccy-fail"), "sh", "-c", "exit 7")
	if code != 7 || !strings.HasSuffix(out, "ccy exited with status 7. Press Enter to close this session.\n") {
		t.Fatalf("fail: %q %d", out, code)
	}
	if _, code := run(filepath.Join(dir, "ccy odd's \"name\" $x"), "true"); code != 0 {
		t.Fatalf("odd name: %d", code)
	}
	if got := Trampoline("", "ccy"); !strings.Contains(got, "rm -f -- ''") {
		t.Fatalf("got %q", got)
	}
}

func TestReadAllReportsEachUnreadableRecord(t *testing.T) {
	s := newStore(t)
	if got, err := s.ReadAll(); err != nil || len(got.Records) != 0 || len(got.Problems) != 0 {
		t.Fatalf("absent directory: %+v, %v", got, err)
	}
	for _, name := range []string{"ccy-b", "ccy-a"} {
		rec := sample()
		rec.Name = name
		if err := s.Write(rec); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(s.Dir(), "ccy-bad"), []byte("garbage\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir(), "ccy-x.tmp.123"), []byte("half"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := s.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(got.Records))
	for _, r := range got.Records {
		names = append(names, r.Name)
	}
	if !slices.Equal(names, []string{"ccy-a", "ccy-b"}) {
		t.Fatalf("records %v", names)
	}
	if len(got.Problems) != 1 || !errors.Is(got.Problems[0].Err, ErrUnreadable) || got.Problems[0].File != filepath.Join(s.Dir(), "ccy-bad") {
		t.Fatalf("problems %+v", got.Problems)
	}
	if !strings.Contains(got.Problems[0].Err.Error(), "does not start with 'ccy-session-record 1'") {
		t.Fatalf("text %q", got.Problems[0].Err)
	}
}

func TestReadAllFailsWhenTheDirectoryCannotBeListed(t *testing.T) {
	file := filepath.Join(t.TempDir(), "sessions")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(file).ReadAll(); err == nil || !strings.Contains(err.Error(), "could not list the session registry") {
		t.Fatalf("got %v", err)
	}
}

// bashWithArgs runs a snippet with the registry library sourced and the given words as $1....
func bashWithArgs(t *testing.T, snippet string, args ...string) string {
	t.Helper()
	root := repoRoot(t)
	script := "set -euo pipefail\nprint_error() { printf 'ERROR: %s\\n' \"$*\" >&2; }\nsource \"$1\"\nshift\n" + snippet
	res, err := execx.OS{}.Run(context.Background(), execx.Cmd{
		Name: "bash", Args: append([]string{"-c", script, "bash", filepath.Join(root, "files/var/local/claude-yolo/lib/session-registry.bash")}, args...),
		Env: []string{"CCY_STATE_DIR=" + t.TempDir()},
	})
	if err != nil {
		t.Fatalf("bash: %v\nstderr: %s", err, res.Stderr)
	}
	return string(res.Stdout)
}
