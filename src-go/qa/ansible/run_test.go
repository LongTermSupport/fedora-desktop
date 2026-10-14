package ansible

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// outcome is what one Main run printed and wrote.
type outcome struct {
	out, errOut, report string
	code                ExitCode
}

// runIn runs Main over the real file system with the report in a fresh temporary directory.
func runIn(t *testing.T, cwd string, args ...string) outcome {
	t.Helper()
	jsonPath := filepath.Join(t.TempDir(), "out.json")
	var stdout, stderr bytes.Buffer
	env := Env{
		Getenv: func(k string) string {
			if k == "QA_JSON_OUT" {
				return jsonPath
			}
			return ""
		},
		Stdout:    &stdout,
		Stderr:    &stderr,
		Cwd:       cwd,
		Open:      os.DirFS,
		WriteFile: func(path string, data []byte) error { return os.WriteFile(path, data, 0o600) },
	}
	res := outcome{code: Main(args, env), out: stdout.String(), errOut: stderr.String()}
	body, err := os.ReadFile(jsonPath)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	res.report = string(body)
	return res
}

func cleanTree() map[string]string {
	return map[string]string{
		"playbooks/a.yml": playbookShebang + "\n---\n- hosts: all\n  vars:\n    scope: general\n",
		"tasks/t.yml":     "k: v\n",
	}
}

func chmodX(t *testing.T, root, rel string) {
	t.Helper()
	if err := os.Chmod(filepath.Join(root, rel), 0o700); err != nil {
		t.Fatal(err)
	}
}

func TestMainCleanTreePasses(t *testing.T) {
	root := writeTree(t, cleanTree())
	chmodX(t, root, "playbooks/a.yml")
	res := runIn(t, root)
	if res.code != ExitOK || res.errOut != "" || !strings.HasPrefix(res.out, "✓ ansible: ") || !strings.Contains(res.out, "1 playbook(s)") {
		t.Fatalf("%+v", res)
	}
	if !strings.Contains(res.report, `"status": "pass"`) {
		t.Fatalf("report %s", res.report)
	}
}

func TestMainReportsEveryCheckInOrder(t *testing.T) {
	files := cleanTree()
	files["tasks/ff.yml"] = "  failed_when: false\n"
	files["vars/sr.yml"] = "foo: \"{{ foo | default(1) }}\"\n"
	files["environment/e.yml"] = "x: ansible_env\n"
	files["roles/r/rd.yml"] = "p: /run/user/1000\n"
	files["playbooks/b.yml"] = playbookShebang + "\n- hosts: all\n"
	root := writeTree(t, files)
	chmodX(t, root, "playbooks/a.yml")
	res := runIn(t, root)
	if res.code != ExitFailed {
		t.Fatalf("%+v", res)
	}
	var order []string
	for line := range strings.SplitSeq(res.out, "\n") {
		if i := strings.Index(line, "):"); strings.HasPrefix(line, "  ERROR (") && i > 0 {
			order = append(order, line[len("  ERROR ("):i])
		}
	}
	want := "fail-fast hygiene self-ref var scope deprecated fact var guessed uid in a runtime path"
	if got := strings.Join(order, " "); got != want {
		t.Fatalf("order %q want %q\n%s", got, want, res.out)
	}
	if !strings.Contains(res.out, "✗ ansible: 6 violation(s) — 1 fail-fast, 1 hygiene, 1 self-ref var, 1 scope/guard, 2 deprecated fact var\n") {
		t.Fatalf("summary:\n%s", res.out)
	}
}

func TestMainWithoutSearchDirectoriesCannotRun(t *testing.T) {
	root := t.TempDir()
	res := runIn(t, root)
	want := "ERROR: none of the expected Ansible dirs (playbooks/ tasks/ vars/ environment/ roles/) exist under " + root + "\n"
	if res.code != ExitCannotRun || res.errOut != want || res.report != "" {
		t.Fatalf("%+v", res)
	}
}

func TestMainWithoutPlaybooksDirectoryCannotRun(t *testing.T) {
	root := writeTree(t, map[string]string{"tasks/t.yml": "k: v\n"})
	res := runIn(t, root)
	if res.code != ExitCannotRun || !strings.Contains(res.errOut, "playbooks") || res.report != "" {
		t.Fatalf("%+v", res)
	}
}

func TestMainUsage(t *testing.T) {
	root := writeTree(t, cleanTree())
	help := runIn(t, root, "--help")
	if help.code != ExitOK || !strings.HasPrefix(help.out, "usage: qa-ansible") || help.errOut != "" {
		t.Fatalf("help: %+v", help)
	}
	typo := runIn(t, root, "--typo")
	if typo.code != ExitCannotRun || typo.out != "" || !strings.Contains(typo.errOut, "unknown argument '--typo'") || typo.report != "" {
		t.Fatalf("typo: %+v", typo)
	}
	bare := runIn(t, root, "--root")
	if bare.code != ExitCannotRun || !strings.Contains(bare.errOut, "--root needs a directory") {
		t.Fatalf("--root: %+v", bare)
	}
}

func TestMainRootFromScriptsDirectory(t *testing.T) {
	root := writeTree(t, cleanTree())
	chmodX(t, root, "playbooks/a.yml")
	res := runIn(t, t.TempDir(), "--scripts", filepath.Join(root, "scripts"))
	if res.code != ExitOK || !strings.HasPrefix(res.out, "✓ ansible: ") {
		t.Fatalf("%+v", res)
	}
}

func TestReportPath(t *testing.T) {
	if got := reportPath(func(string) string { return "" }); got != "/tmp/qa-ansible-results.json" {
		t.Fatalf("default %q", got)
	}
	if got := reportPath(func(string) string { return "/x/y.json" }); got != "/x/y.json" {
		t.Fatalf("override %q", got)
	}
}

func TestMainUnwritableReportCannotRun(t *testing.T) {
	root := writeTree(t, cleanTree())
	chmodX(t, root, "playbooks/a.yml")
	var stdout, stderr bytes.Buffer
	env := Env{
		Getenv:    func(string) string { return "/x/out.json" },
		Stdout:    &stdout,
		Stderr:    &stderr,
		Cwd:       root,
		Open:      os.DirFS,
		WriteFile: func(string, []byte) error { return errors.New("read-only") },
	}
	if code := Main([]string{"--root", root}, env); code != ExitCannotRun {
		t.Fatalf("code %d", code)
	}
	if !strings.Contains(stderr.String(), "ERROR: cannot write the report /x/out.json: read-only") {
		t.Fatalf("stderr %q", stderr.String())
	}
}

// failingFS reads every file but the named one.
type failingFS struct {
	fs.FS

	bad string
}

func (f failingFS) Open(name string) (fs.File, error) {
	if name == f.bad {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrPermission}
	}
	return f.FS.Open(name)
}

func TestScanUnreadableFileCannotRunInEveryCheck(t *testing.T) {
	mem := fstest.MapFS{
		"playbooks/a.yml": &fstest.MapFile{Data: []byte("x"), Mode: 0o755},
		"tasks/t.yml":     &fstest.MapFile{Data: []byte("x")},
	}
	for _, bad := range []string{"tasks/t.yml", "playbooks/a.yml"} {
		_, err := Scan(failingFS{FS: mem, bad: bad}, "/r")
		if err == nil || !strings.Contains(err.Error(), bad) {
			t.Errorf("%s: err %v", bad, err)
		}
	}
}

func TestScanBinaryFileIsText(t *testing.T) {
	mem := fstest.MapFS{
		"playbooks/a.yml": &fstest.MapFile{Data: []byte("- hosts: all\n"), Mode: 0o755},
		"tasks/b.yml":     &fstest.MapFile{Data: []byte("  failed_when: false\x00\n")},
	}
	f, err := Scan(mem, "/r")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.FailFast) != 1 || !strings.HasPrefix(f.FailFast[0], "tasks/b.yml:1:") {
		t.Fatalf("got %q", f.FailFast)
	}
}

func TestScanOrdersFilesByName(t *testing.T) {
	mem := fstest.MapFS{
		"playbooks/a.yml": &fstest.MapFile{Data: []byte("- hosts: all\n"), Mode: 0o755},
		"tasks/z.yml":     &fstest.MapFile{Data: []byte("  failed_when: no\n")},
		"tasks/a.yml":     &fstest.MapFile{Data: []byte("  failed_when: no\n")},
		"tasks/m/b.yml":   &fstest.MapFile{Data: []byte("  failed_when: no\n")},
	}
	f, err := Scan(mem, "/r")
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(f.FailFast))
	for _, v := range f.FailFast {
		name, _, hasColon := strings.Cut(v, ":")
		if !hasColon {
			t.Fatalf("no colon in %q", v)
		}
		got = append(got, name)
	}
	if strings.Join(got, " ") != "tasks/a.yml tasks/m/b.yml tasks/z.yml" {
		t.Fatalf("got %q", got)
	}
}

func TestScanWithoutAnyPlaybookCannotRun(t *testing.T) {
	mem := fstest.MapFS{
		"playbooks/notes.yml": &fstest.MapFile{Data: []byte("k: v\n")},
		"tasks/t.yml":         &fstest.MapFile{Data: []byte("k: v\n")},
	}
	_, err := Scan(mem, "/r")
	if err == nil || !strings.Contains(err.Error(), "no playbook") {
		t.Fatalf("err %v", err)
	}
}
