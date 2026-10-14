package businstall

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// The installer's source digest is defined in helpers/agent_bus/installer_digest.py (what
// the play bakes in and declare recomputes). These tests are the Go side of the agreement:
// they compute the same digest from the closure the go tool reports and compare, so a change
// to the definition on one side only, or an import the Python reader misses, fails here.

// checkoutRoot walks up from the test's directory to the checkout.
func checkoutRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "helpers", "agent_bus", "installer_digest.py")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no helpers/agent_bus/installer_digest.py above the test directory")
		}
		dir = parent
	}
}

func runIn(t *testing.T, dir string, cmd execx.Cmd) string {
	t.Helper()
	cmd.Dir = dir
	res, err := execx.OS{}.Run(context.Background(), cmd)
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", cmd.Name, strings.Join(cmd.Args, " "), err, res.Stderr)
	}
	return string(res.Stdout)
}

// goClosure is the directories (relative to src-go) of every package of the module the
// installer binary is built from, as the go tool resolves them for the play's build.
func goClosure(t *testing.T, moduleDir string) []string {
	t.Helper()
	listed := runIn(t, moduleDir, execx.Cmd{
		Name: "go",
		Args: []string{"list", "-deps", "-f", "{{if not .Standard}}{{.Dir}}{{end}}", "./cmd/agent-bus-install"},
		Env:  []string{"GOFLAGS=-mod=readonly", "GOTOOLCHAIN=local"},
	})
	listedDirs := slices.Collect(strings.FieldsSeq(listed))
	dirs := make([]string, 0, len(listedDirs))
	for _, dir := range listedDirs {
		rel, err := filepath.Rel(moduleDir, dir)
		if err != nil {
			t.Fatal(err)
		}
		dirs = append(dirs, filepath.ToSlash(rel))
	}
	slices.Sort(dirs)
	return dirs
}

// goDigest is the definition, in Go: go.mod and every regular file directly in a closure
// directory but the tests, sorted by path, hashed as path NUL file-hash LF.
func goDigest(t *testing.T, moduleDir string, dirs []string) string {
	t.Helper()
	names := []string{"go.mod"}
	for _, dir := range dirs {
		entries, err := os.ReadDir(filepath.Join(moduleDir, dir))
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if entry.Type().IsRegular() && !strings.HasSuffix(entry.Name(), "_test.go") {
				names = append(names, dir+"/"+entry.Name())
			}
		}
	}
	slices.Sort(names)
	whole := sha256.New()
	for _, name := range names {
		data, err := os.ReadFile(filepath.Clean(filepath.Join(moduleDir, filepath.FromSlash(name))))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		if _, err := fmt.Fprintf(whole, "%s\x00%s\n", name, hex.EncodeToString(sum[:])); err != nil {
			t.Fatal(err)
		}
	}
	return hex.EncodeToString(whole.Sum(nil))
}

func pythonDigest(t *testing.T, root string) string {
	t.Helper()
	out := runIn(t, root, execx.Cmd{
		Name: "python3",
		Args: []string{"-P", "-m", "helpers.agent_bus.installer_digest", "--source", root},
		Env:  []string{"PYTHONPATH=" + root},
	})
	return strings.TrimSpace(out)
}

func TestSourceDigestIsTheSameInGoAndPython(t *testing.T) {
	root := checkoutRoot(t)
	moduleDir := filepath.Join(root, "src-go")
	dirs := goClosure(t, moduleDir)
	for _, want := range []string{"cmd/agent-bus-install", "internal/businstall", "internal/fsx"} {
		if !slices.Contains(dirs, want) {
			t.Fatalf("the go tool's closure lacks %s: %v", want, dirs)
		}
	}
	if slices.Contains(dirs, "internal/teams") {
		t.Fatalf("the installer is not built from internal/teams: %v", dirs)
	}
	fromGo, fromPython := goDigest(t, moduleDir, dirs), pythonDigest(t, root)
	if fromGo != fromPython {
		t.Fatalf("the digest differs: Go %s, Python %s (closure by the go tool: %v)", fromGo, fromPython, dirs)
	}
}

func TestTheBuiltBinaryCarriesTheDigestThePlayBakesIn(t *testing.T) {
	root := checkoutRoot(t)
	digest := pythonDigest(t, root)
	binary := filepath.Join(t.TempDir(), "agent-bus-install")
	runIn(t, filepath.Join(root, "src-go"), execx.Cmd{
		Name: "go",
		Args: []string{
			"build", "-trimpath", "-buildvcs=false", "-ldflags", "-X main.SourceDigest=" + digest,
			"-o", binary, "./cmd/agent-bus-install",
		},
		Env: []string{"GOFLAGS=-mod=readonly", "GOTOOLCHAIN=local"},
	})
	got := strings.TrimSpace(runIn(t, t.TempDir(), execx.Cmd{Name: binary, Args: []string{"--version"}}))
	if got != digest {
		t.Fatalf("--version prints %q, want the baked digest %q", got, digest)
	}
}
