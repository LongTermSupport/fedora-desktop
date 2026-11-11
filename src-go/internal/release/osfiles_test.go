package release_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/release"
)

func TestOSFilesReadWhatWasWrittenAndNotWhatIsAbsent(t *testing.T) {
	root := t.TempDir()
	files := release.OSFiles{Root: root}
	path := filepath.Join(root, "CHANGELOG.md")

	if text, present, err := files.Read(path); err != nil || present || text != "" {
		t.Fatalf("absent file: got %q, %v, %v", text, present, err)
	}
	if err := files.Write(path, "# Changelog\n"); err != nil {
		t.Fatal(err)
	}
	if text, present, err := files.Read(path); err != nil || !present || text != "# Changelog\n" {
		t.Fatalf("written file: got %q, %v, %v", text, present, err)
	}
}

// A path that is not under the root is refused, whatever it names: the changelog is the only
// file the app reads, and it is at the top of the checkout.
func TestOSFilesRefuseAPathOutsideTheRoot(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "checkout")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "secret"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	text, present, err := release.OSFiles{Root: root}.Read(filepath.Join(parent, "secret"))
	if err == nil || present || text != "" {
		t.Fatalf("got %q, %v, %v: a file outside the root must be an error", text, present, err)
	}
}
