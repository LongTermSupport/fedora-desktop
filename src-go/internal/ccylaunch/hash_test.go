package ccylaunch

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

const launcherRel = "files/var/local/claude-yolo/claude-yolo"

// bashHashBlock is the launcher's own CCY_HASH=$( ... ) assignment, from the checkout: the
// oracle the Go hash is held to, so a change to the bash rule fails this test.
func bashHashBlock(t *testing.T) string {
	t.Helper()
	text := checkoutFile(t, launcherRel)
	start := strings.Index(text, "\nCCY_HASH=$(\n")
	if start < 0 {
		t.Fatal("the launcher has no CCY_HASH=$( line")
	}
	end := strings.Index(text[start:], "\n)\n")
	if end < 0 {
		t.Fatal("the launcher's CCY_HASH block has no closing line")
	}
	return text[start+1 : start+end+3]
}

// bashHash runs the launcher's hash block over an install: $0 the bash launcher, SCRIPT_DIR
// its directory, as the launcher sets them.
func bashHash(t *testing.T, bash string) string {
	t.Helper()
	script := `SCRIPT_DIR="$1"` + "\n" + bashHashBlock(t) + `printf '%s' "$CCY_HASH"` + "\n"
	res, err := execx.OS{}.Run(context.Background(), execx.Cmd{
		Name: "bash", Args: []string{"-c", script, bash, filepath.Dir(bash)},
	})
	if err != nil {
		t.Fatalf("the launcher's hash block: %v\n%s", err, res.Stderr)
	}
	return string(res.Stdout)
}

func writeTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// IntegrityHash is the bash launcher's CCY_HASH byte for byte: the launcher's text without its
// CCY_HASH= lines, then every regular *.bash and *.py file directly in lib/ in byte order;
// links, other names and subdirectories are not part of it.
func TestIntegrityHashIsTheLaunchersRule(t *testing.T) {
	dir := t.TempDir()
	bashText := "#!/bin/bash\nCCY_VERSION=\"1.2.3\"\nCCY_HASH=$(\n  echo kept\n)\nlast line, no newline"
	writeTree(t, dir, map[string]string{
		BashName:               bashText,
		"lib/b.bash":           "b\n",
		"lib/a.bash":           "a\n",
		"lib/C.bash":           "upper sorts first\n",
		"lib/.hidden.bash":     "hidden\n",
		"lib/agent.py":         "py\n",
		"lib/notes.txt":        "not part of it\n",
		"lib/sub/inner.bash":   "not part of it\n",
		"lib/common-pure.bash": "pure\n",
	})
	if err := os.Symlink("a.bash", filepath.Join(dir, "lib", "link.bash")); err != nil {
		t.Fatal(err)
	}
	got, libs, err := IntegrityHash([]byte(bashText), filepath.Join(dir, "lib"))
	if err != nil {
		t.Fatal(err)
	}
	if want := bashHash(t, filepath.Join(dir, BashName)); got != want {
		t.Fatalf("IntegrityHash = %q, the launcher's rule gives %q", got, want)
	}
	if libs != 5 {
		t.Fatalf("libs = %d, want the 5 regular .bash files", libs)
	}
}

// The checkout's own launcher and libraries hash as the launcher does.
func TestIntegrityHashOfTheCheckout(t *testing.T) {
	bash := filepath.Join(repoRoot(t), filepath.FromSlash(launcherRel))
	text, err := os.ReadFile(bash)
	if err != nil {
		t.Fatal(err)
	}
	got, libs, err := IntegrityHash(text, filepath.Join(filepath.Dir(bash), "lib"))
	if err != nil {
		t.Fatal(err)
	}
	if want := bashHash(t, bash); got != want || libs == 0 {
		t.Fatalf("IntegrityHash = %q (%d libs), the launcher's rule gives %q", got, libs, want)
	}
}

func TestIntegrityHashOfNoLibDirectoryIsAnError(t *testing.T) {
	if _, _, err := IntegrityHash([]byte("x\n"), filepath.Join(t.TempDir(), "lib")); err == nil {
		t.Fatal("a missing lib directory hashed")
	}
}
