package open

import (
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// labels is what the terminal side offers for mime with progs installed.
func labels(t *testing.T, w *world, mime string) ([]string, bool) {
	t.Helper()
	cands, known := w.app().newRun().terminalCandidates(mime, "T")
	out := make([]string, len(cands))
	for i, c := range cands {
		out[i] = c.label
	}
	return out, known
}

// E11: each type's handlers in order, only the installed ones, file always last.
func TestTerminalHandlersByType(t *testing.T) {
	for _, tc := range []struct {
		mime      string
		installed []string
		want      []string
	}{
		{"inode/directory", []string{"ranger", "ls", "less", "tree", "file"}, []string{"ranger (file manager)", "ls (listing)", "tree (recursive listing)", "file (identify only)"}},
		{"x-scheme-handler/https", []string{"lynx", "curl", "less"}, []string{"lynx (browser)", "curl (dump)"}},
		{"text/html", []string{"w3m", "less"}, []string{"w3m (render)", "less (page)"}},
		{"application/pdf", []string{"pdftotext", "pdfinfo", "less"}, []string{"pdftotext (text)", "pdfinfo (metadata)"}},
		{"image/webp", []string{"chafa", "exiftool", "less"}, []string{"chafa (terminal image)", "exiftool (metadata)"}},
		{"video/x-matroska", []string{"mpv", "ffprobe"}, []string{"mpv (play)", "ffprobe (metadata)"}},
		{"audio/flac", []string{"ffplay"}, []string{"ffplay (play)"}},
		{"application/json", []string{"jq", "bat", "less"}, []string{"jq (pretty print)", "bat (view, highlighted)", "less (page)"}},
		{"application/zstd", []string{"ouch", "bsdtar", "7z", "less"}, []string{"ouch (list contents)", "bsdtar (list contents)", "7z (list contents)"}},
		{"application/vnd.sqlite3", []string{"sqlite3"}, []string{"sqlite3 (shell)"}},
		{"application/x-pie-executable", []string{"readelf", "nm", "less"}, []string{"readelf (headers)", "nm (symbols)"}},
		{"application/x-shellscript", []string{"less"}, []string{"less (page)"}},
		{"message/rfc822", []string{"bat"}, []string{"bat (view, highlighted)"}},
	} {
		w := newWorld(t)
		w.install(tc.installed...)
		got, known := labels(t, w, tc.mime)
		if !known || strings.Join(got, "|") != strings.Join(tc.want, "|") {
			t.Fatalf("%s: got %q (known %v), want %q", tc.mime, got, known, tc.want)
		}
	}
}

// E11, B9: a handler whose pager is missing is not offered.
func TestHandlerWithoutItsPagerIsNotOffered(t *testing.T) {
	w := newWorld(t)
	w.install("pdftotext", "pdfinfo")
	got, known := labels(t, w, "application/pdf")
	if !known || strings.Join(got, "|") != "pdfinfo (metadata)" {
		t.Fatalf("got %q, %v", got, known)
	}
}

// E12: the editor is the first of VISUAL, EDITOR, nvim, vim, nano, vi whose first word is on
// PATH, run with its words.
func TestEditorResolution(t *testing.T) {
	w := newWorld(t)
	w.env["VISUAL"] = "missing-editor --wait"
	w.env["EDITOR"] = "code -w"
	w.install("code", "vim")
	cands, known := w.app().newRun().terminalCandidates("text/plain", "T")
	if !known || len(cands) == 0 || cands[0].label != "code -w (edit)" || strings.Join(cands[0].argv, " ") != "code -w T" {
		t.Fatalf("got %+v", cands)
	}
	w = newWorld(t)
	w.install("nano", "vi")
	cands, known = w.app().newRun().terminalCandidates("text/plain", "T")
	if !known || len(cands) == 0 || cands[0].label != "nano (edit)" {
		t.Fatalf("got %+v", cands)
	}
}

// E13: an unknown type, or a known one with nothing installed, gets the generic offers.
func TestUnknownTypeGetsTheGenericOffers(t *testing.T) {
	w := newWorld(t)
	w.install("xxd", "less", "file")
	got, known := labels(t, w, "application/x-weird")
	if known || strings.Join(got, "|") != "xxd (hex dump)|less (page)|file (identify only)" {
		t.Fatalf("got %q, known %v", got, known)
	}
	w = newWorld(t)
	w.install("hexdump", "less")
	got, known = labels(t, w, "video/mp4")
	if known || strings.Join(got, "|") != "hexdump (hex dump)|less (page)" {
		t.Fatalf("nothing installed for video: got %q, known %v", got, known)
	}
}

// E11, D4: a handler is an argv with the target as its own argument, and an optional pager.
func TestHandlerArgv(t *testing.T) {
	w := newWorld(t)
	w.install("ls", "less", "yazi")
	cands, known := w.app().newRun().terminalCandidates("inode/directory", "my dir; rm -rf ~")
	if !known || len(cands) < 2 {
		t.Fatalf("got %+v, %v", cands, known)
	}
	if strings.Join(cands[0].argv, "\x00") != "yazi\x00my dir; rm -rf ~" || cands[0].pager != nil {
		t.Fatalf("yazi: %+v", cands[0])
	}
	ls := cands[1]
	if strings.Join(ls.argv, "\x00") != "ls\x00-lAh\x00--color=always\x00--\x00my dir; rm -rf ~" || strings.Join(ls.pager, " ") != "less -R" {
		t.Fatalf("ls: %+v", ls)
	}
}

// E14: nothing at all to offer is exit 1; --list says so and succeeds.
func TestNoCandidateFails(t *testing.T) {
	w := newWorld(t)
	p := w.file("a.bin", "x")
	w.install("mimetype")
	w.runner.On([]string{"mimetype"}, execx.Result{Stdout: []byte("application/x-weird\n")})
	if code := w.run(p); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	w.wantStderr("open: no handler available for '" + p + "' (application/x-weird). Install a viewer, or run 'open --list'.\n")

	w = newWorld(t)
	w.install("mimetype")
	w.runner.On([]string{"mimetype"}, execx.Result{Stdout: []byte("application/x-weird\n")})
	p = w.file("a.bin", "x")
	if code := w.run("--list", p); code != term.ExitOK {
		t.Fatalf("list: exit %d", code)
	}
	if got := w.stdout.String(); got != p+" (application/x-weird)\n  (no handler available)\n" {
		t.Fatalf("list %q", got)
	}
}
