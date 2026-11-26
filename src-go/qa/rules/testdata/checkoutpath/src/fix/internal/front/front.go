// Package front acts on paths of a checkout a container writes, outside the os.Root seam.
package front

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"fix/internal/fsx"
)

const (
	ccyDir       = ".claude/ccy"
	gitignoreRel = ccyDir + "/.gitignore"
)

// Front is a launcher run in a checkout.
type Front struct{ Dir string }

func (f Front) at(rel string) string { return filepath.Join(f.Dir, rel) }

// seats is the seats directory, joined from pieces.
func seats(dir string) string { return filepath.Join(dir, ".claude", "ccy", "pingbus", "seats") }

// Gitignore is the defect: the launcher wrote .claude/ccy/.gitignore through a link the
// container put in its place (Plan 00266 review finding 6).
func (f Front) Gitignore(text []byte) error {
	return os.WriteFile(f.at(gitignoreRel), text, 0o644) // want `os.WriteFile acts on .claude/ccy/.gitignore, a path in a checkout a container writes`
}

// Local follows the path through a local variable.
func (f Front) Local() ([]byte, error) {
	local := filepath.Join(f.Dir, ccyDir, "ccy.env.local")
	return os.ReadFile(local) // want `os.ReadFile acts on .claude/ccy/ccy.env.local, a path in a checkout a container writes`
}

// Seats follows the path through a function of the package.
func Seats(dir string) ([]os.DirEntry, error) {
	return os.ReadDir(seats(dir)) // want `os.ReadDir acts on .claude/ccy/pingbus/seats, a path in a checkout a container writes`
}

// Make makes the directory.
func Make(dir string) error {
	return os.MkdirAll(filepath.Join(dir, ".claude/ccy"), 0o700) // want `os.MkdirAll acts on .claude/ccy, a path in a checkout a container writes`
}

// Atomic writes it through the module's atomic writer, which opens by path.
func Atomic(dir string) error {
	return fsx.WriteAtomic(filepath.Join(dir, ".claude/ccy/state"), "the state", nil, 0o600) // want `fsx.WriteAtomic acts on .claude/ccy/state, a path in a checkout a container writes`
}

// Walk walks it.
func Walk(dir string) error {
	return filepath.WalkDir(seats(dir), func(string, fs.DirEntry, error) error { return nil }) // want `filepath.WalkDir acts on .claude/ccy/pingbus/seats, a path in a checkout a container writes`
}

// Rooted is the seam: every open goes through an os.Root of the checkout.
func Rooted(dir string) ([]byte, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.ReadFile(gitignoreRel)
}

// Home is the user's own ~/.claude, which no checkout holds.
func Home(home string) error {
	return os.MkdirAll(filepath.Join(home, ".claude"), 0o700)
}

// Message only names the path.
func Message() string { return "write " + gitignoreRel }

// PlantSeat is a test helper outside a _test.go file (a fake machine's package): it takes a
// *testing.T, so it is test scaffolding that plants a test's own world, not host code.
func PlantSeat(t *testing.T, dir string) {
	t.Helper()
	if err := os.RemoveAll(seats(dir)); err != nil {
		t.Fatal(err)
	}
}

// PlantWith takes a testing.TB, which is a test helper too.
func PlantWith(tb testing.TB, dir string) {
	if err := os.MkdirAll(seats(dir), 0o700); err != nil {
		tb.Fatal(err)
	}
}
