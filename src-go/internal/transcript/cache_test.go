package transcript

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	assistantLine = `{"type":"assistant","message":{"model":"m","usage":{"input_tokens":%d,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}` + "\n"
	userLine      = `{"type":"user","timestamp":"%s"}` + "\n"
)

func assistant(tokens int) string { return fmt.Sprintf(assistantLine, tokens) }

// appendTo adds text to the end of the file, as Claude appends to a transcript.
func appendTo(t *testing.T, path, text string) {
	t.Helper()
	f, err := os.OpenFile(filepath.Clean(path), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.WriteString(text); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
}

func tokensOf(t *testing.T, c *Cache, path string) int64 {
	t.Helper()
	got, err := c.ContextTokens(path)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestCacheReadsOnlyWhatWasAppended(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.jsonl")
	first := assistant(10) + assistant(20)
	write(t, path, first, fixedMtime)
	c := NewCache()
	if got := tokensOf(t, c, path); got != 20 {
		t.Fatalf("got %d", got)
	}
	if c.bytesRead != int64(len(first)) {
		t.Fatalf("first read took %d bytes, want %d", c.bytesRead, len(first))
	}
	if got := tokensOf(t, c, path); got != 20 || c.bytesRead != int64(len(first)) {
		t.Fatalf("an unchanged file was read again: %d bytes read, answer %d", c.bytesRead, got)
	}
	more := assistant(30)
	appendTo(t, path, more)
	if got := tokensOf(t, c, path); got != 30 {
		t.Fatalf("got %d", got)
	}
	if c.bytesRead != int64(len(first)+len(more)) {
		t.Fatalf("only the appended %d bytes should be read, %d read in all", len(more), c.bytesRead)
	}
}

func TestCacheHoldsAPartialLineUntilItIsComplete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.jsonl")
	full := assistant(40)
	cut := len(full) / 2
	write(t, path, assistant(10)+full[:cut], fixedMtime)
	c := NewCache()
	if got := tokensOf(t, c, path); got != 10 {
		t.Fatalf("a half-written line must not count: got %d", got)
	}
	appendTo(t, path, full[cut:])
	if got := tokensOf(t, c, path); got != 40 {
		t.Fatalf("the completed line must count: got %d", got)
	}
	if got := tokensOf(t, c, path); got != 40 {
		t.Fatalf("got %d", got)
	}
}

func TestCacheCountsAnUnterminatedLineThatIsWholeJSONOnceOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.jsonl")
	whole := strings.TrimSuffix(assistant(50), "\n")
	write(t, path, assistant(10)+whole, fixedMtime)
	c := NewCache()
	if got := tokensOf(t, c, path); got != 50 {
		t.Fatalf("a final line that is whole JSON counts, as a full read counts it: got %d", got)
	}
	appendTo(t, path, "\n"+assistant(60))
	if got := tokensOf(t, c, path); got != 60 {
		t.Fatalf("got %d", got)
	}
}

func TestCacheReadsAgainWhenTheFileShrank(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.jsonl")
	write(t, path, assistant(10)+assistant(20)+assistant(30), fixedMtime)
	c := NewCache()
	tokensOf(t, c, path)
	shorter := assistant(7)
	write(t, path, shorter, fixedMtime)
	before := c.bytesRead
	if got := tokensOf(t, c, path); got != 7 {
		t.Fatalf("got %d", got)
	}
	if c.bytesRead-before != int64(len(shorter)) {
		t.Fatalf("a truncated file is read whole again: read %d, want %d", c.bytesRead-before, len(shorter))
	}
}

func TestCacheReadsAgainWhenTheFileWasReplaced(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t.jsonl")
	write(t, path, assistant(10), fixedMtime)
	c := NewCache()
	tokensOf(t, c, path)
	// A longer replacement that begins with the same bytes: only the inode tells them apart.
	next := filepath.Join(dir, "next.jsonl")
	replacement := assistant(10) + assistant(99)
	write(t, next, replacement, fixedMtime)
	if err := os.Rename(next, path); err != nil {
		t.Fatal(err)
	}
	before := c.bytesRead
	if got := tokensOf(t, c, path); got != 99 {
		t.Fatalf("got %d", got)
	}
	if c.bytesRead-before != int64(len(replacement)) {
		t.Fatalf("a replaced file is read whole again: read %d, want %d", c.bytesRead-before, len(replacement))
	}
}

func TestCacheReadsAgainWhenTheFileWasRewrittenInPlace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.jsonl")
	write(t, path, assistant(11), fixedMtime)
	c := NewCache()
	tokensOf(t, c, path)
	// Same inode, same length, different bytes.
	write(t, path, assistant(22), fixedMtime)
	if got := tokensOf(t, c, path); got != 22 {
		t.Fatalf("a rewrite in place went unseen: got %d", got)
	}
	// Same inode, longer, different bytes from the start.
	write(t, path, assistant(33)+assistant(44), fixedMtime)
	if got := tokensOf(t, c, path); got != 44 {
		t.Fatalf("got %d", got)
	}
}

// answers is what the cache says of a file: its context size, and whether input was taken and a
// compaction finished at the moments the tests use.
type answers struct {
	tokens    int64
	tokensErr bool
	took      bool
	compacted bool
}

func answersOf(t *testing.T, c *Cache, path string) answers {
	t.Helper()
	var a answers
	var err error
	a.tokens, err = c.ContextTokens(path)
	a.tokensErr = err != nil
	if a.took, err = c.TookInputSince(path, sent+300); err != nil {
		t.Fatal(err)
	}
	if a.compacted, err = c.CompactedSince(path, sent+390); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestCacheAnswersTheInputAndCompactionQuestionsIncrementally(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.jsonl")
	write(t, path, assistant(1), fixedMtime)
	c := NewCache()
	if got := answersOf(t, c, path); got.took || got.compacted {
		t.Fatalf("nothing yet: %+v", got)
	}
	appendTo(t, path, fmt.Sprintf(userLine, "2026-10-08T10:05:00.120Z"))
	if got := answersOf(t, c, path); !got.took || got.compacted {
		t.Fatalf("the input line is dated 300 s on: %+v", got)
	}
	if later, err := c.TookInputSince(path, sent+301); err != nil || later {
		t.Fatalf("got %v, %v", later, err)
	}
	appendTo(t, path, `{"type":"system","subtype":"compact_boundary","timestamp":"2026-10-08T10:06:30.500Z"}`+"\n")
	if got := answersOf(t, c, path); !got.took || !got.compacted {
		t.Fatalf("the compaction line is dated 390 s on: %+v", got)
	}
	if later, err := c.CompactedSince(path, sent+391); err != nil || later {
		t.Fatalf("got %v, %v", later, err)
	}
}

func TestCacheKeepsAFailureUntilTheFileChangesUnderIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.jsonl")
	write(t, path, `{"type":"user","timestamp":"yesterday"}`+"\n", fixedMtime)
	c := NewCache()
	for range 2 {
		if _, err := c.TookInputSince(path, 0); err == nil || !strings.Contains(err.Error(), "could not be read") {
			t.Fatalf("got %v", err)
		}
	}
	write(t, path, `{"type":"user","timestamp":"2026-10-08T10:00:00Z"}`+"\n", fixedMtime)
	if got, err := c.TookInputSince(path, sent); err != nil || !got {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestCacheErrorsForAMissingFileAndForgetsIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.jsonl")
	write(t, path, assistant(5), fixedMtime)
	c := NewCache()
	tokensOf(t, c, path)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ContextTokens(path); err == nil || !strings.Contains(err.Error(), "could not be read") {
		t.Fatalf("got %v", err)
	}
	if len(c.files) != 0 {
		t.Fatalf("a vanished file is still held: %d", len(c.files))
	}
}

func TestCacheRefusesALineBeyondTheBound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.jsonl")
	write(t, path, assistant(5)+strings.Repeat("x", maxLine+1), fixedMtime)
	c := NewCache()
	if _, err := c.ContextTokens(path); err == nil || !strings.Contains(err.Error(), "line longer than") {
		t.Fatalf("got %v", err)
	}
}

func TestCacheHoldsAtMostMaxFiles(t *testing.T) {
	dir := t.TempDir()
	c := NewCache()
	for i := range maxFiles + 10 {
		path := filepath.Join(dir, fmt.Sprintf("%d.jsonl", i))
		write(t, path, assistant(i+1), fixedMtime)
		if got := tokensOf(t, c, path); got != int64(i+1) {
			t.Fatalf("got %d", got)
		}
	}
	if len(c.files) > maxFiles {
		t.Fatalf("%d files held, bound is %d", len(c.files), maxFiles)
	}
}

// However the file grows, in whatever pieces, the cache answers as a full read of the same bytes.
func TestCacheAgreesWithAFullReadAtEveryCut(t *testing.T) {
	whole := sample + "\n" + assistant(77) + fmt.Sprintf(userLine, "2026-10-08T10:05:00.120Z") +
		`{"type":"system","subtype":"compact_boundary","timestamp":"2026-10-08T10:06:30.500Z","compactMetadata":{"postTokens":9000}}` + "\n" + assistant(5)
	path := filepath.Join(t.TempDir(), "t.jsonl")
	for _, step := range []int{1, 3, 17, 40, 7, 101, 250} {
		write(t, path, "", fixedMtime)
		c := NewCache()
		for written := 0; written < len(whole); {
			end := min(written+step, len(whole))
			appendTo(t, path, whole[written:end])
			written = end
			if got, want := answersOf(t, c, path), answersOf(t, NewCache(), path); got != want {
				t.Fatalf("step %d, at %d bytes: cache %+v, full read %+v", step, written, got, want)
			}
		}
	}
}

// BenchmarkPollCached is a poll after the cache has seen the file: one line appended, and only
// that line read and decoded.
func BenchmarkPollCached(b *testing.B) {
	path := bigTranscript(b)
	c := NewCache()
	if _, err := c.ContextTokens(path); err != nil {
		b.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Clean(path), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := range b.N {
		if _, err = f.WriteString(syntheticLine(i + 1)); err != nil {
			b.Fatal(err)
		}
		if _, err = c.ContextTokens(path); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	if err = f.Close(); err != nil {
		b.Fatal(err)
	}
}

// BenchmarkPollCachedUnchanged is a poll of a file that did not grow.
func BenchmarkPollCachedUnchanged(b *testing.B) {
	path := bigTranscript(b)
	c := NewCache()
	if _, err := c.ContextTokens(path); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for range b.N {
		if _, err := c.ContextTokens(path); err != nil {
			b.Fatal(err)
		}
	}
}
