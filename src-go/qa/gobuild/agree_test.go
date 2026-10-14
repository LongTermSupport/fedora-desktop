package gobuild

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// The digest is defined here (gobuild.SourceDigest) and baked into every deployed command at
// build (tasks/build-go-command.yml). These tests are the agreement with the go tool, for every
// command under src-go/cmd: the closure the digest hashes is the closure `go list -deps`
// reports for the play's build, and a binary built the play's way prints the digest.

// checkout walks up from the test's directory to the checkout.
func checkout(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "scripts", "go-binary-suites.tsv")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no scripts/go-binary-suites.tsv above the test directory")
		}
		dir = parent
	}
}

// deployedCommands is every command of the module, read from src-go/cmd.
func deployedCommands(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, "src-go", "cmd"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	if len(names) == 0 {
		t.Fatal("src-go/cmd holds no command")
	}
	return names
}

func runGo(t *testing.T, dir string, args ...string) string {
	t.Helper()
	res, err := proc.OS{}.Run(context.Background(), proc.Cmd{
		Argv: append([]string{"go"}, args...),
		Dir:  dir,
		Env:  []string{"GOFLAGS=-mod=readonly", "GOTOOLCHAIN=local"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Code != 0 {
		t.Fatalf("go %s: exit %d\n%s", strings.Join(args, " "), res.Code, res.Stderr)
	}
	return res.Stdout
}

func TestTheTableOfSuitesNamesEveryCommandOfTheModule(t *testing.T) {
	root := checkout(t)
	data, err := os.ReadFile(filepath.Join(root, "scripts", "go-binary-suites.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []string
	for line := range strings.Lines(string(data)) {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			rows = append(rows, strings.Fields(line)[0])
		}
	}
	slices.Sort(rows)
	if want := deployedCommands(t, root); !slices.Equal(rows, want) {
		t.Fatalf("scripts/go-binary-suites.tsv names %v, src-go/cmd holds %v", rows, want)
	}
}

func TestTheClosureIsTheOneTheGoToolReportsForEveryCommand(t *testing.T) {
	root := checkout(t)
	moduleDir := filepath.Join(root, "src-go")
	for _, command := range deployedCommands(t, root) {
		listed := runGo(t, moduleDir, "list", "-deps", "-f", "{{if not .Standard}}{{.Dir}}{{end}}", "./cmd/"+command)
		var want []string
		for dir := range strings.FieldsSeq(listed) {
			rel, err := filepath.Rel(moduleDir, dir)
			if err != nil {
				t.Fatal(err)
			}
			want = append(want, filepath.ToSlash(rel))
		}
		slices.Sort(want)
		got, err := Closure(root, command)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s: the digest hashes %v, the go tool builds from %v", command, got, want)
		}
	}
}

func TestEveryCommandsBuiltBinaryPrintsTheDigestThePlayBakesIn(t *testing.T) {
	root := checkout(t)
	moduleDir := filepath.Join(root, "src-go")
	for _, command := range deployedCommands(t, root) {
		digest := mustDigest(t, root, command)
		binary := filepath.Join(t.TempDir(), command)
		runGo(t, moduleDir, "build", "-trimpath", "-buildvcs=false", "-ldflags", "-X main.SourceDigest="+digest, "-o", binary, "./cmd/"+command)
		res, err := proc.OS{}.Run(context.Background(), proc.Cmd{Argv: []string{binary, "--version"}, Dir: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(res.Stdout); res.Code != 0 || got != digest {
			t.Errorf("%s --version: exit %d, prints %q (stderr %q), want %q", command, res.Code, got, res.Stderr, digest)
		}
	}
}

func TestTheDriftTableNamesEveryCommandOfTheModule(t *testing.T) {
	root := checkout(t)
	rows, err := ReadTable(filepath.Join(root, TablePath))
	if err != nil {
		t.Fatal(err)
	}
	got := slices.Sorted(maps.Keys(rows))
	if want := deployedCommands(t, root); !slices.Equal(got, want) {
		t.Fatalf("%s names %v, src-go/cmd holds %v", TablePath, got, want)
	}
	for command, row := range rows {
		play, err := os.ReadFile(filepath.Join(root, row.Play))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(play), "go_command_name: "+command) && !strings.Contains(string(play), "freeze_tool: "+command) {
			t.Errorf("%s does not build %s", row.Play, command)
		}
		if !filepath.IsAbs(row.Installed) && strings.SplitN(row.Installed, "/", 2)[0] != "~" {
			t.Errorf("%s: installed path %q is neither absolute nor home-relative", command, row.Installed)
		}
	}
}
