package watermark

import (
	"os"
	"path/filepath"
	"testing"
)

func TestM10TheDefaultOutputKeepsTheStemAndLowerCasesTheExtension(t *testing.T) {
	cases := map[string]string{
		"a/b/Photo.JPG":    "a/b/Photo.watermarked.jpg",
		"x.tar.jpeg":       "x.tar.watermarked.jpeg",
		"/pics/shot.Png":   "/pics/shot.watermarked.png",
		"./rel/dir/IMG.jp": "./rel/dir/IMG.watermarked.jp",
	}
	for in, want := range cases {
		if got := defaultOutput(in); got != want {
			t.Errorf("defaultOutput(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestM9TheMarkedSuffixIsJudgedInAnyCase(t *testing.T) {
	for name, want := range map[string]bool{
		"a.watermarked.jpg": true, "A.WATERMARKED.JPEG": true, "b.watermarked.png": true,
		"watermarked.jpg": false, "a.watermarked.gif": false, "a.jpg": false,
	} {
		if hasMarkedName(name) != want {
			t.Errorf("%s: want %v", name, want)
		}
	}
}

func TestM10ResolveFollowsLinksAndKeepsTheMissingTail(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "real")
	if err = os.Mkdir(target, 0o750); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(target, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	got, err := resolve(filepath.Join(dir, "link", "new", "out.jpg"))
	if err != nil || got != filepath.Join(target, "new", "out.jpg") {
		t.Fatalf("got %q, %v", got, err)
	}
	got, err = resolve(filepath.Join(dir, "link"))
	if err != nil || got != target {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestM8TheContentDecidesNotTheName(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, data []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	if err := checkImage(write("ok.jpg", jpegBytes)); err != nil {
		t.Fatal(err)
	}
	if err := checkImage(write("ok.PNG", pngBytes)); err != nil {
		t.Fatal(err)
	}
	if err := checkImage(write("short.png", []byte{0x89})); err == nil {
		t.Fatal("a file shorter than any signature is not an image")
	}
	if err := checkImage(dir); err == nil {
		t.Fatal("a directory is not an image")
	}
}
