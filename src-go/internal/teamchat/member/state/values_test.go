package state

import (
	"bytes"
	"errors"
	"io/fs"
	"strconv"
	"strings"
	"testing"
)

func TestProcessValid(t *testing.T) {
	for _, c := range []struct {
		p    Process
		want bool
	}{
		{Process{PID: 1, StartTicks: 0}, true},
		{Process{PID: maxPID, StartTicks: 1}, true},
		{Process{PID: 0, StartTicks: 1}, false},
		{Process{PID: maxPID + 1, StartTicks: 1}, false},
		{Process{PID: 1, StartTicks: -1}, false},
	} {
		if got := c.p.valid(); got != c.want {
			t.Errorf("%+v.valid() = %v", c.p, got)
		}
	}
}

func TestValidSessionID(t *testing.T) {
	if !validSessionID(sampleSessionID) {
		t.Fatal("a session id refused")
	}
	for _, bad := range []string{"", "01234567-89AB-cdef-0123-456789abcdef", "0123456789abcdef0123456789abcdef", sampleSessionID + "0", "../x"} {
		if validSessionID(bad) {
			t.Errorf("validSessionID(%q) = true", bad)
		}
	}
}

func TestValidTranscript(t *testing.T) {
	if !validTranscript(sampleTranscript) || !validTranscript("/"+strings.Repeat("a", maxTranscriptPath-7)+".jsonl") {
		t.Fatal("a transcript refused")
	}
	for _, bad := range []string{"", "x.jsonl", "/a/../b.jsonl", "/a//b.jsonl", "/a/b.json", "/" + strings.Repeat("a", maxTranscriptPath-6) + ".jsonl"} {
		if validTranscript(bad) {
			t.Errorf("validTranscript(%q) = true", bad)
		}
	}
}

func TestValidSocket(t *testing.T) {
	long := "/" + strings.Repeat("a", maxSocketPath-len("/cc-socks/4242.sock")-1) + "/cc-socks/4242.sock"
	if !validSocket(sampleSocket, 4242) || !validSocket(long, 4242) || len(long) != maxSocketPath {
		t.Fatal("a socket refused")
	}
	for _, bad := range []string{"", "cc-socks/4242.sock", "/r/cc-socks/4243.sock", "/r/socks/4242.sock", "/r/x/../cc-socks/4242.sock", "/a" + long} {
		if validSocket(bad, 4242) {
			t.Errorf("validSocket(%q) = true", bad)
		}
	}
}

func TestOptionalOf(t *testing.T) {
	set := []string{"a"}
	if !optionalOf(nil, set) || !optionalOf(new("a"), set) || optionalOf(new("b"), set) {
		t.Fatal("optionalOf is wrong")
	}
	if !optionalMS(nil) || !optionalMS(new(int64(5))) || optionalMS(new(int64(-1))) {
		t.Fatal("optionalMS is wrong")
	}
}

// The samples the member file tests share.
const (
	sampleRun        = "0123456789abcdef"
	sampleSessionID  = "01234567-89ab-cdef-0123-456789abcdef"
	sampleTranscript = "/tmp/x/projects/p/" + sampleSessionID + ".jsonl"
	sampleSocket     = "/tmp/x/runtime/cc-socks/4242.sock"
)

// edit is good with old replaced by repl once; the test fails if old is not in good.
func edit(t *testing.T, good, old, repl string) string {
	t.Helper()
	if !strings.Contains(good, old) {
		t.Fatalf("%s is not in the sample", old)
	}
	return strings.Replace(good, old, repl, 1)
}

// refusal is an edit of a good file and the code its refusal carries.
type refusal struct{ old, repl, code string }

// refusesAll holds every edit of good refused by decode with the edit's code, as an
// InvalidError for file whose message quotes nothing the edit wrote (B10).
func refusesAll[T any](t *testing.T, file, good string, decode func([]byte) (T, error), cases []refusal) {
	t.Helper()
	for _, c := range cases {
		bad := edit(t, good, c.old, c.repl)
		_, err := decode([]byte(bad))
		inv, ok := errors.AsType[*InvalidError](err)
		if !ok || inv.File != file || inv.Code != c.code {
			t.Errorf("%s with %s for %s = %v, want code %s", file, c.repl, c.old, err, c.code)
			continue
		}
		if strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), "424242") {
			t.Errorf("the error quotes the file: %v", err)
		}
	}
}

// accepts holds every document decoded.
func accepts[T any](t *testing.T, decode func([]byte) (T, error), docs ...string) {
	t.Helper()
	for _, doc := range docs {
		if _, err := decode([]byte(doc)); err != nil {
			t.Errorf("%s refused: %v", doc, err)
		}
	}
}

// strictRefusals holds what every member file refuses, by the codec's general rules: not
// JSON, a value after it, a repeated key, a float, v that is not 1 or is missing, a key it does
// not have, a key in another case, and a file over limit; and a file of exactly limit bytes
// read. key is a key of good whose value is an integer.
func strictRefusals[T any](t *testing.T, file, good, key string, limit int, decode func([]byte) (T, error)) {
	t.Helper()
	upper := `"` + strings.ToUpper(key) + `":`
	refusesAll(t, file, good, decode, []refusal{
		{`{"v":1,`, `{"v":1,"v":1,`, CodeJSON},
		{`"` + key + `":`, `"` + key + `":1.5,"x` + key + `":`, CodeJSON},
		{`{"v":1,`, `{"v":2,`, CodeVersion},
		{`{"v":1,`, `{`, CodeVersion},
		{`{"v":1,`, `{"v":1,"SECRET":1,`, CodeSchema},
		{`"` + key + `":`, upper, CodeSchema},
	})
	for _, bad := range []string{good[:len(good)-1], good + "{}", "[1]"} {
		if _, err := decode([]byte(bad)); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %s read: %v", file, bad, err)
		}
	}
	padded := good + strings.Repeat(" ", limit-len(good))
	if _, err := decode([]byte(padded)); err != nil {
		t.Errorf("%s of exactly its cap refused: %v", file, err)
	}
	_, err := decode([]byte(padded + " "))
	if inv, ok := errors.AsType[*InvalidError](err); !ok || !strings.Contains(inv.Rule, "cap of "+strconv.Itoa(limit)) {
		t.Errorf("%s over its cap = %v", file, err)
	}
}

// roundTrips holds v written through a homefs root and read back to the same bytes, and the
// zero value refused by Encode.
func roundTrips[T Encoder](t *testing.T, v T, limit int, decode func([]byte) (T, error)) {
	t.Helper()
	w := newWorld(t)
	full := w.run.Dir() + "/file.json"
	check(t, Write(w.root, full, v))
	got, err := Read(w.root, full, limit, decode)
	check(t, err)
	want, err := v.Encode()
	check(t, err)
	again, err := got.Encode()
	check(t, err)
	if !bytes.Equal(want, again) {
		t.Fatalf("read back %s, want %s", again, want)
	}
	var zero T
	if _, err := zero.Encode(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("the zero value encoded: %v", err)
	}
	other := w.run.Dir() + "/other.json"
	if err := Write(w.root, other, zero); !errors.Is(err, ErrInvalid) {
		t.Fatalf("the zero value written: %v", err)
	}
	if _, err := Read(w.root, other, limit, decode); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a refused write left a file, or a missing file read: %v", err)
	}
}

// teamsJSON is n distinct team ids of the longest grammar, each as tmpl with %s replaced.
func teamsJSON(n int, tmpl string) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = strings.ReplaceAll(tmpl, "%s", longTeam(i))
	}
	return strings.Join(parts, ",")
}

// longTeam is the i-th of distinct 22-character team ids.
func longTeam(i int) string {
	s := "t" + strconv.Itoa(i)
	return s + strings.Repeat("a", 22-len(s))
}
