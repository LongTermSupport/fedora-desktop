package open

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// E4, B15: a URL is a scheme by RFC 3986's grammar and ://, or mailto:; the scheme lower-cased.
func TestURLSchemeIsTheTypeOfAURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://example.com":        "https",
		"HTTP://example.com":         "http",
		"git+ssh://example.com/repo": "git+ssh",
		"mailto:someone@example.com": "mailto",
		"MailTo:someone@example.com": "mailto",
	} {
		if got, ok := urlScheme(in); !ok || got != want {
			t.Fatalf("%q: %q %v, want %q", in, got, ok, want)
		}
	}
	for _, in := range []string{"dir/x://y", "./a://b", "1http://x", "://x", "plain.txt", "c:foo", "mailtox"} {
		if got, ok := urlScheme(in); ok {
			t.Fatalf("%q is a path, got scheme %q", in, got)
		}
	}
}

// E4: a URL's type is x-scheme-handler/<scheme>, with no detector asked and no file looked for.
func TestURLTargetIsTypedWithoutDetectors(t *testing.T) {
	w := newWorld(t)
	w.install("w3m")
	w.runner.On([]string{"w3m"}, execx.Result{})
	if code := w.run("--list", "HTTPS://example.com/x"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.stderr.String())
	}
	if !strings.HasPrefix(w.stdout.String(), "HTTPS://example.com/x (x-scheme-handler/https)\n") {
		t.Fatalf("stdout %q", w.stdout.String())
	}
}

// E5: a missing path and an unreadable one are named, exit 1.
func TestMissingAndUnreadablePathsFail(t *testing.T) {
	w := newWorld(t)
	missing := filepath.Join(w.home, "nope.txt")
	if code := w.run(missing); code != term.ExitFailure {
		t.Fatalf("missing: exit %d", code)
	}
	w.wantStderr("open: no such file or directory: '" + missing + "'\n")

	w = newWorld(t)
	p := w.file("secret.txt", "x")
	w.unreadable[p] = true
	if code := w.run(p); code != term.ExitFailure {
		t.Fatalf("unreadable: exit %d", code)
	}
	w.wantStderr("open: not readable: '" + p + "'\n")
}

// E5, B16: a path that begins with a dash reaches every program as ./<path>.
func TestDashPathIsPassedWithADotSlash(t *testing.T) {
	w := newWorld(t)
	w.file("-rf.json", "{}")
	t.Chdir(w.home)
	w.types("application/json")
	w.install("jq", "less")
	if code := w.run("--dry-run", "--", "-rf.json"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.stderr.String())
	}
	if got := w.stdout.String(); got != "jq . ./-rf.json | less -R\n" {
		t.Fatalf("stdout %q", got)
	}
	if !w.called("file --brief --mime-type -- ./-rf.json") {
		t.Fatalf("the detector was not given ./-rf.json: %q", w.ran())
	}
}

// E6: a directory is inode/directory without asking a detector.
func TestDirectoryIsInodeDirectory(t *testing.T) {
	w := newWorld(t)
	got, err := w.app().newRun().typeOf(context.Background(), w.home)
	if err != nil || got != "inode/directory" {
		t.Fatalf("got %q, %v", got, err)
	}
	if len(w.ran()) != 0 {
		t.Fatalf("ran %q", w.ran())
	}
}

// E6: the detectors in order, the first installed one that answers one type/subtype word.
func TestDetectorsAreAskedInOrder(t *testing.T) {
	w := newWorld(t)
	p := w.file("a.txt", "x")
	w.install("mimetype", "xdg-mime", "file")
	w.runner.On([]string{"mimetype"}, execx.Result{Stdout: []byte("text/plain\n")})
	got, err := w.app().newRun().typeOf(context.Background(), p)
	if err != nil || got != "text/plain" {
		t.Fatalf("got %q, %v", got, err)
	}
	if want := []string{"mimetype --brief -- " + p}; !equalLines(w.ran(), want) {
		t.Fatalf("ran %q", w.ran())
	}

	// mimetype missing: xdg-mime is next.
	w = newWorld(t)
	p = w.file("a.txt", "x")
	w.install("xdg-mime", "file")
	w.runner.On([]string{"xdg-mime"}, execx.Result{Stdout: []byte("text/x-c\n")})
	got, err = w.app().newRun().typeOf(context.Background(), p)
	if err != nil || got != "text/x-c" || !equalLines(w.ran(), []string{"xdg-mime query filetype " + p}) {
		t.Fatalf("got %q, %v, ran %q", got, err, w.ran())
	}
}

// E6, B11: a detector that fails, or answers something that is not a type, is named and the
// next one asked.
func TestFailingDetectorIsNamedAndTheNextAsked(t *testing.T) {
	w := newWorld(t)
	p := w.file("a.bin", "x")
	w.install("mimetype", "xdg-mime", "file")
	w.runner.On([]string{"mimetype"}, execx.Result{ExitCode: 2, Stderr: []byte("boom")})
	w.runner.On([]string{"xdg-mime"}, execx.Result{Stdout: []byte("no idea at all\n")})
	w.runner.On([]string{"file"}, execx.Result{Stdout: []byte("application/octet-stream\n")})
	got, err := w.app().newRun().typeOf(context.Background(), p)
	if err != nil || got != "application/octet-stream" {
		t.Fatalf("got %q, %v", got, err)
	}
	w.wantStderr("open: mimetype could not type '" + p + "': mimetype exited 2: boom; trying the next detector\n")
	w.wantStderr("open: xdg-mime could not type '" + p + "': it answered \"no idea at all\"; trying the next detector\n")
}

// E6: none answering is exit 1 naming each detector's reason and the play.
func TestNoDetectorIsAFailureNamingThePlay(t *testing.T) {
	w := newWorld(t)
	p := w.file("a.bin", "x")
	w.install("file")
	w.runner.On([]string{"file"}, execx.Result{ExitCode: 1, Stderr: []byte("cannot open")})
	if code := w.run(p); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	w.wantStderr("open: could not determine the file type of '" + p + "'.\n")
	w.wantStderr("mimetype: not installed")
	w.wantStderr("file: file exited 1: cannot open")
	w.wantStderr("playbooks/imports/optional/common/play-cli-tools.yml")
}

func equalLines(got, want []string) bool { return slices.Equal(got, want) }

func mustSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}
