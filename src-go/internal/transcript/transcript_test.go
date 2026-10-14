package transcript

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

func TestDir(t *testing.T) {
	cases := []struct{ prefix, dir, home, want string }{
		{"ccy", "/p/my proj", "/home/u/.claude", "/p/my proj/.claude/ccy/projects/-workspace"},
		{"cc", "/p/my proj/v1.2", "/home/u/.claude", "/home/u/.claude/projects/-p-my-proj-v1-2"},
		{"cc", "/p/é", "/h", "/h/projects/-p--"},
		{"cc", "/p/😀", "/h", "/h/projects/-p---"},
	}
	for _, c := range cases {
		got, err := Dir(c.prefix, c.dir, c.home)
		if err != nil || got != c.want {
			t.Errorf("Dir(%q,%q): got %q, %v; want %q", c.prefix, c.dir, got, err, c.want)
		}
	}
}

func TestDirUnknownLauncher(t *testing.T) {
	_, err := Dir("zz", "/p", "/h")
	if err == nil || err.Error() != "no transcript directory is known for launcher 'zz'" {
		t.Fatalf("got %v", err)
	}
}

// fixedMtime is the modification time the fixtures are written with: a fixed instant, so no
// test depends on the hour it runs in.
var fixedMtime = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

func write(t *testing.T, path, text string, mtime time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

func TestNewestFailures(t *testing.T) {
	td := filepath.Join(t.TempDir(), "transcripts")
	if err := os.MkdirAll(filepath.Join(td, "older-session-subagents"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Newest(td); err == nil || err.Error() != "there is no transcript in "+td {
		t.Fatalf("got %v", err)
	}
	if _, err := Newest(filepath.Join(td, "nope")); err == nil || err.Error() != "there is no transcript directory "+filepath.Join(td, "nope") {
		t.Fatalf("got %v", err)
	}
}

func TestNewestAndFor(t *testing.T) {
	td := filepath.Join(t.TempDir(), "transcripts")
	write(t, filepath.Join(td, "old.jsonl"), "{}\n", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	write(t, filepath.Join(td, "new.jsonl"), "{}\n", time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC))
	write(t, filepath.Join(td, "notes.txt"), "x", time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC))
	write(t, filepath.Join(td, "older-session-subagents", "agent.jsonl"), "{}\n", time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC))
	if got, err := Newest(td); err != nil || got != filepath.Join(td, "new.jsonl") {
		t.Fatalf("got %q, %v", got, err)
	}
	if got, err := For(td, ""); err != nil || got != filepath.Join(td, "new.jsonl") {
		t.Fatalf("got %q, %v", got, err)
	}
	if got, err := For(td, "old"); err != nil || got != filepath.Join(td, "old.jsonl") {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := For(td, "gone"); err == nil || err.Error() != "the resumed conversation "+filepath.Join(td, "gone.jsonl")+" is not there" {
		t.Fatalf("got %v", err)
	}
	if _, err := For(td, "../new"); err == nil || err.Error() != "the resumed conversation id '../new' is not a conversation id" {
		t.Fatalf("got %v", err)
	}
}

func TestNewestTieKeepsTheFirstByName(t *testing.T) {
	td := t.TempDir()
	same := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	write(t, filepath.Join(td, "b.jsonl"), "{}\n", same)
	write(t, filepath.Join(td, "a.jsonl"), "{}\n", same)
	if got, err := Newest(td); err != nil || got != filepath.Join(td, "a.jsonl") {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestNewestDirectoryUnreadable(t *testing.T) {
	file := filepath.Join(t.TempDir(), "f")
	write(t, file, "x", fixedMtime)
	if _, err := Newest(file); err == nil || !strings.Contains(err.Error(), "there is no transcript directory") {
		t.Fatalf("got %v", err)
	}
}

const sample = `{"type":"user","timestamp":"2026-10-08T10:00:00.000Z","message":{"content":"hi"}}
{"type":"assistant","message":{"model":"claude-opus-5-5","usage":{"input_tokens":2,"cache_creation_input_tokens":4543,"cache_read_input_tokens":203980,"output_tokens":866}}}
{"type":"assistant","isSidechain":true,"message":{"model":"claude-opus-5-5","usage":{"input_tokens":9,"cache_creation_input_tokens":0,"cache_read_input_tokens":40000}}}
{"type":"assistant","message":{"model":"<synthetic>","usage":{"input_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}
{"type":"assistant","message":{"usa`

func TestContextTokens(t *testing.T) {
	f := filepath.Join(t.TempDir(), "t.jsonl")
	write(t, f, sample, fixedMtime)
	if got, err := ContextTokens(f); err != nil || got != 208525 {
		t.Fatalf("got %d, %v", got, err)
	}
	write(t, f, sample+"\n"+`{"type":"system","subtype":"compact_boundary","compactMetadata":{"trigger":"manual","preTokens":432810,"postTokens":11753}}`+"\n", fixedMtime)
	if got, err := ContextTokens(f); err != nil || got != 11753 {
		t.Fatalf("got %d, %v", got, err)
	}
}

func TestContextTokensFailures(t *testing.T) {
	d := t.TempDir()
	none := filepath.Join(d, "none.jsonl")
	write(t, none, `{"type":"user","timestamp":"2026-10-08T10:00:00.000Z"}`+"\n", fixedMtime)
	if _, err := ContextTokens(none); err == nil || err.Error() != "the transcript "+none+" records no context size" {
		t.Fatalf("got %v", err)
	}
	if _, err := ContextTokens(filepath.Join(d, "missing.jsonl")); err == nil || !strings.Contains(err.Error(), "could not be read") {
		t.Fatalf("got %v", err)
	}
	frac := filepath.Join(d, "frac.jsonl")
	write(t, frac, `{"type":"assistant","message":{"usage":{"input_tokens":1.5}}}`+"\n", fixedMtime)
	if _, err := ContextTokens(frac); err == nil || !strings.Contains(err.Error(), "records no context size") {
		t.Fatalf("got %v", err)
	}
	neg := filepath.Join(d, "neg.jsonl")
	write(t, neg, `{"type":"system","subtype":"compact_boundary","compactMetadata":{"postTokens":"12"}}`+"\n", fixedMtime)
	if _, err := ContextTokens(neg); err == nil || !strings.Contains(err.Error(), "records no context size") {
		t.Fatalf("got %v", err)
	}
}

func TestContextTokensIgnoresNonObjectsAndNonObjectUsage(t *testing.T) {
	f := filepath.Join(t.TempDir(), "t.jsonl")
	write(t, f, "[1,2]\n42\n\"s\"\nnull\n\n"+
		`{"type":"assistant","message":{"usage":"nope"}}`+"\n"+
		`{"type":"assistant","isSidechain":false,"message":{"usage":{"input_tokens":7}}}`+"\n"+
		`{"type":"assistant","isSidechain":null,"message":{"usage":{"cache_read_input_tokens":3}}}`+"\n", fixedMtime)
	if got, err := ContextTokens(f); err != nil || got != 3 {
		t.Fatalf("got %d, %v", got, err)
	}
}

// 2026-10-08T10:00:00Z is 1791453600.
const sent = 1791453600

func TestTookInputSince(t *testing.T) {
	f := filepath.Join(t.TempDir(), "t.jsonl")
	body := sample + "\n"
	write(t, f, body, fixedMtime)
	check := func(want bool, since int64) {
		t.Helper()
		got, err := TookInputSince(f, since)
		if err != nil || got != want {
			t.Fatalf("since %d: got %v, %v; want %v", since, got, err, want)
		}
	}
	check(false, sent+1)
	body += `{"type":"user","isMeta":true,"timestamp":"2026-10-08T10:05:00.000Z","message":{"content":"Caveat"}}` + "\n" +
		`{"type":"user","isSidechain":true,"timestamp":"2026-10-08T10:05:00.000Z","message":{"content":"sub"}}` + "\n"
	write(t, f, body, fixedMtime)
	check(false, sent+1)
	body += `{"type":"user","timestamp":"2026-10-08T10:05:00.120Z","message":{"content":"continue"}}` + "\n"
	write(t, f, body, fixedMtime)
	check(true, sent+300)
	check(false, sent+301)
	check(true, sent)
}

func TestCompactedSince(t *testing.T) {
	f := filepath.Join(t.TempDir(), "t.jsonl")
	body := sample + "\n"
	write(t, f, body, fixedMtime)
	check := func(want bool, since int64) {
		t.Helper()
		got, err := CompactedSince(f, since)
		if err != nil || got != want {
			t.Fatalf("since %d: got %v, %v; want %v", since, got, err, want)
		}
	}
	check(false, sent+1)
	body += `{"type":"system","subtype":"compact_boundary","isSidechain":true,"timestamp":"2026-10-08T10:06:00.000Z"}` + "\n"
	write(t, f, body, fixedMtime)
	check(false, sent+1)
	body += `{"type":"system","subtype":"compact_boundary","timestamp":"2026-10-08T10:06:30.500Z","compactMetadata":{"postTokens":9000}}` + "\n"
	write(t, f, body, fixedMtime)
	check(true, sent+390)
	check(false, sent+391)
}

func TestTimestampProblems(t *testing.T) {
	d := t.TempDir()
	bad := filepath.Join(d, "bad.jsonl")
	write(t, bad, `{"type":"user","timestamp":"yesterday"}`+"\n", fixedMtime)
	if _, err := TookInputSince(bad, 0); err == nil || !strings.Contains(err.Error(), "could not be read") {
		t.Fatalf("got %v", err)
	}
	if _, err := CompactedSince(filepath.Join(d, "missing.jsonl"), 0); err == nil || !strings.Contains(err.Error(), "could not be read") {
		t.Fatalf("got %v", err)
	}
	notString := filepath.Join(d, "ns.jsonl")
	write(t, notString, `{"type":"user","timestamp":12}`+"\n"+`{"type":"user"}`+"\n"+"garbage\n", fixedMtime)
	if got, err := TookInputSince(notString, 0); err != nil || got {
		t.Fatalf("got %v, %v", got, err)
	}
	noFraction := filepath.Join(d, "nf.jsonl")
	write(t, noFraction, `{"type":"user","timestamp":"2026-10-08T10:00:00Z"}`+"\n", fixedMtime)
	if got, err := TookInputSince(noFraction, sent); err != nil || !got {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestInputIsDatedInUTCWhateverTheZone(t *testing.T) {
	t.Setenv("TZ", "America/New_York")
	f := filepath.Join(t.TempDir(), "t.jsonl")
	write(t, f, `{"type":"user","timestamp":"2026-10-08T10:05:00.120Z"}`+"\n", fixedMtime)
	if got, err := TookInputSince(f, sent+301); err != nil || got {
		t.Fatalf("got %v, %v", got, err)
	}
	if got, err := TookInputSince(f, sent+300); err != nil || !got {
		t.Fatalf("got %v, %v", got, err)
	}
}

// The Go functions answer what the bash library (with jq) answers on the same files.
func TestAgreesWithTheBashLibrary(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq is not installed, so there is no bash answer to compare with")
	}
	root := repoRoot(t)
	f := filepath.Join(t.TempDir(), "t.jsonl")
	write(t, f, sample+"\n"+
		`{"type":"user","timestamp":"2026-10-08T10:05:00.120Z","message":{"content":"continue"}}`+"\n"+
		`{"type":"system","subtype":"compact_boundary","timestamp":"2026-10-08T10:06:30.500Z","compactMetadata":{"postTokens":9000}}`+"\n", fixedMtime)
	lib := filepath.Join(root, "files/var/local/claude-yolo/lib/session-registry.bash")
	res, err := execx.OS{}.Run(context.Background(), execx.Cmd{
		Name: "bash",
		Args: []string{"-c", `set -euo pipefail; print_error() { printf 'ERROR: %s\n' "$*" >&2; }; source "$1"
ccy_transcript_context_tokens "$2"; ccy_transcript_took_input_since "$2" 1791453900; ccy_transcript_compacted_since "$2" 1791453990
ccy_transcript_took_input_since "$2" 1791453901; ccy_transcript_compacted_since "$2" 1791453991
ccy_transcript_dir cc "/p/my proj/v1.2" /h`, "bash", lib, f},
	})
	if err != nil {
		t.Fatalf("bash: %v\n%s", err, res.Stderr)
	}
	want := "9000\nyes\nyes\nno\nno\n/h/projects/-p-my-proj-v1-2\n"
	if string(res.Stdout) != want {
		t.Fatalf("bash said %q", res.Stdout)
	}
	if got, err := ContextTokens(f); err != nil || got != 9000 {
		t.Fatalf("got %d, %v", got, err)
	}
	for _, c := range []struct {
		fn    func(string, int64) (bool, error)
		since int64
		want  bool
	}{{TookInputSince, 1791453900, true}, {CompactedSince, 1791453990, true}, {TookInputSince, 1791453901, false}, {CompactedSince, 1791453991, false}} {
		if got, err := c.fn(f, c.since); err != nil || got != c.want {
			t.Fatalf("since %d: got %v, %v", c.since, got, err)
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for dir := wd; dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		if _, statErr := os.Stat(filepath.Join(dir, "files/var/local/claude-yolo/lib/session-registry.bash")); statErr == nil {
			return dir
		}
	}
	t.Fatalf("repo root not found above %s", wd)
	return ""
}
