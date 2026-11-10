package kit_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/kit"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// imageKitFiles is where the repository keeps the ccy image's kit, all of it but the binary
// (Plan 00209 Task 4.1, D 3.6): tasks/teamchat-ccy-kit.yml copies these files into the image's
// build context, and the Dockerfile's builder stage builds bin/teamchat beside them.
const imageKitFiles = "../../../../../files/opt/claude-yolo/optional/" + paths.Product

// tree is every regular file below root, by its path relative to root, with its bytes.
func tree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	fsys := os.DirFS(root)
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !d.Type().IsRegular() {
			t.Errorf("%s/%s is not a regular file", root, p)
			return nil
		}
		data, err := fs.ReadFile(fsys, p)
		out[p] = string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// The repository's copy of the image kit is exactly what Assemble writes for the image's kit
// path, the binary aside: a change to the kit's text that is not carried into the files fails
// here, and so does a file the kit no longer has.
func TestTheImageKitFilesAreTheKitAssembledForTheImage(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "kit")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := kit.Assemble(dir, paths.CCYKitDir(), strings.NewReader("bin")); err != nil {
		t.Fatal(err)
	}
	want := tree(t, dir)
	binary := "bin/" + paths.MemberBinary
	if _, ok := want[binary]; !ok {
		t.Fatalf("the assembled kit has no %s", binary)
	}
	delete(want, binary)
	got := tree(t, imageKitFiles)
	for name, text := range want {
		if got[name] != text {
			t.Errorf("%s/%s differs from the assembled kit's:\n%s", imageKitFiles, name, text)
		}
	}
	for name := range got {
		if _, ok := want[name]; !ok {
			t.Errorf("%s/%s is not a file of the assembled kit", imageKitFiles, name)
		}
	}
}
