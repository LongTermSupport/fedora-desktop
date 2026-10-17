package stt

import (
	"bufio"
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const binDir = "/opt/stt-bin"

var fixedNow = time.Date(2026, 10, 16, 19, 40, 0, 0, time.UTC)

type seg struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
}

// fakeServer is the warm server's socket. FILE_START records the request and names a new job.
// FILE_PROGRESS must name that job; finish, called with the job's paths, is the job's end, and
// a "done" job's segments are handed out one a poll from the `since` asked for, as "running",
// so a client that does not keep its place gets the text twice or never finishes.
type fakeServer struct {
	mu     sync.Mutex
	starts []map[string]any
	paths  []string
	job    string
	polls  int
	finish func(paths []string) map[string]any
	start  func() map[string]any
}

const maxPolls = 200

func (s *fakeServer) progress(t *testing.T, params map[string]any) map[string]any {
	t.Helper()
	s.polls++
	since, ok := params["since"].(float64)
	switch {
	case params["job"] != s.job:
		return map[string]any{"status": "error", "message": fmt.Sprintf("no such file job: %v", params["job"])}
	case !ok || s.polls > maxPolls:
		t.Errorf("FILE_PROGRESS since %v after %d polls", params["since"], s.polls)
		return map[string]any{"status": "error", "message": "bad since"}
	}
	end := s.finish(s.paths)
	segs, isDone := end["segments"].([]seg)
	if !isDone || end["status"] != "done" {
		return end
	}
	from := int(since)
	if from < len(segs) {
		return map[string]any{"status": "running", "segments": segs[from : from+1], "count": from + 1}
	}
	reply := maps.Clone(end)
	reply["segments"] = segs[from:]
	return reply
}

func (s *fakeServer) serve(t *testing.T, l net.Listener) {
	t.Helper()
	for {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		s.answer(t, conn)
	}
}

func (s *fakeServer) answer(t *testing.T, conn net.Conn) {
	t.Helper()
	defer func() {
		if err := conn.Close(); err != nil {
			t.Error(err)
		}
	}()
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		t.Error(err)
		return
	}
	var req struct {
		Command string         `json:"command"`
		Params  map[string]any `json:"params"`
	}
	if err = json.Unmarshal(line, &req); err != nil {
		t.Error(err)
		return
	}
	s.mu.Lock()
	var reply map[string]any
	switch req.Command {
	case "FILE_START":
		s.starts = append(s.starts, req.Params)
		s.paths = stringsOf(t, req.Params["paths"])
		s.job = fmt.Sprintf("job-%d", len(s.starts))
		reply = map[string]any{"status": "running", "job": s.job}
		if s.start != nil {
			reply = s.start()
		}
	case "FILE_PROGRESS":
		reply = s.progress(t, req.Params)
	default:
		reply = map[string]any{"status": "error", "message": "unknown"}
	}
	s.mu.Unlock()
	data, err := json.Marshal(reply)
	if err != nil {
		t.Error(err)
		return
	}
	if _, err := conn.Write(append(data, '\n')); err != nil {
		t.Error(err)
	}
}

// stringsOf is a decoded JSON array of strings; anything else fails the test.
func stringsOf(t *testing.T, v any) []string {
	t.Helper()
	items, ok := v.([]any)
	if !ok {
		t.Errorf("not a JSON array: %v", v)
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		s, ok := item.(string)
		if !ok {
			t.Errorf("not a string: %v", item)
			return nil
		}
		out = append(out, s)
	}
	return out
}

func done(segments ...seg) map[string]any {
	end := 0.0
	for _, s := range segments {
		end = s.End
	}
	if segments == nil {
		segments = []seg{}
	}
	return map[string]any{
		"status": "done", "segments": segments, "count": len(segments),
		"duration": end + 1, "language": "en", "model": "distil-large-v3.5",
	}
}

type world struct {
	t      *testing.T
	root   string
	runner *execx.Fake
	server *fakeServer
	socket string
	lang   string
	stdout bytes.Buffer
	stderr bytes.Buffer
}

func newWorld(t *testing.T) *world {
	t.Helper()
	w := &world{
		t: t, root: t.TempDir(), runner: &execx.Fake{}, socket: filepath.Join(t.TempDir(), "s.sock"),
		server: &fakeServer{finish: func([]string) map[string]any {
			return done(seg{0, 2.5, "Hello there."}, seg{3, 5, "Same paragraph."}, seg{9, 11, "After a pause."})
		}},
	}
	w.runner.On([]string{binDir + "/wsi-setting", "language"}, execx.Result{Stdout: []byte("en\n")})
	w.runner.On([]string{binDir + "/wsi-stream", "--ensure-server"}, execx.Result{})
	l, err := net.Listen("unix", w.socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := l.Close(); err != nil {
			t.Error(err)
		}
	})
	go w.server.serve(t, l)
	return w
}

func (w *world) env() Env {
	return Env{
		Runner: w.runner, Stdout: &w.stdout, Stderr: &w.stderr, BinDir: binDir, Socket: w.socket,
		Now: func() time.Time { return fixedNow }, Getenv: func(k string) string {
			if k == "LANG" {
				return w.lang
			}
			return ""
		}, Sleep: func(context.Context, time.Duration) error { return nil },
	}
}

func (w *world) run(args ...string) term.ExitCode {
	w.t.Helper()
	return App{Env: w.env()}.Run(context.Background(), args)
}

func (w *world) write(rel, content string) string {
	w.t.Helper()
	path := filepath.Join(w.root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		w.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		w.t.Fatal(err)
	}
	// Written long before the run, so a loose file has settled (S18) unless a test says not.
	w.touch(path, fixedNow.Add(-time.Hour))
	return path
}

func (w *world) touch(path string, mtime time.Time) {
	w.t.Helper()
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		w.t.Fatal(err)
	}
}

// recording writes a phone recording folder: its parts and a manifest that describes them.
// Each part is 5 s long, so a recording is never short unless a test says so.
func (w *world) recording(name, state string, parts ...string) string {
	w.t.Helper()
	return w.timedRecording(name, state, 5000, parts...)
}

func (w *world) timedRecording(name, state string, partMillis int, parts ...string) string {
	w.t.Helper()
	segs := make([]map[string]any, 0, len(parts))
	for i, audio := range parts {
		part := fmt.Sprintf("%s-%03d.ogg", name, i+1)
		w.write(filepath.Join(name, part), audio)
		sum := md5.Sum([]byte(audio))
		segs = append(segs, map[string]any{
			"name": part, "bytes": len(audio), "md5": hex.EncodeToString(sum[:]),
			"durationMillis": partMillis,
		})
	}
	manifest, err := json.Marshal(map[string]any{"id": name, "format": "ogg-opus", "state": state, "segments": segs})
	if err != nil {
		w.t.Fatal(err)
	}
	w.write(filepath.Join(name, "manifest.json"), string(manifest))
	return filepath.Join(w.root, name)
}

func (w *world) starts() []map[string]any {
	w.server.mu.Lock()
	defer w.server.mu.Unlock()
	return append([]map[string]any(nil), w.server.starts...)
}

func (w *world) read(path string) string {
	w.t.Helper()
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		w.t.Fatal(err)
	}
	return string(data)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func wantCode(t *testing.T, got, want term.ExitCode, w *world) {
	t.Helper()
	if got != want {
		t.Fatalf("exit %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, w.stdout.String(), w.stderr.String())
	}
}

func (w *world) pathsOf(start map[string]any) []string {
	w.t.Helper()
	return stringsOf(w.t, start["paths"])
}

// S1. A recording folder's parts go to the warm server in order, as one recording, after the
// server is ensured; its sidecar is written beside the folder; stdout is the sidecar's path.
func TestARecordingFolderIsTranscribedIntoASidecarBesideIt(t *testing.T) {
	w := newWorld(t)
	rec := w.recording("dictation-1", "COMPLETE", "aaa", "bbb", "ccc")
	wantCode(t, w.run(w.root), term.ExitOK, w)
	starts := w.starts()
	if len(starts) != 1 {
		t.Fatalf("FILE_STARTs: %d, want 1", len(starts))
	}
	want := []string{rec + "/dictation-1-001.ogg", rec + "/dictation-1-002.ogg", rec + "/dictation-1-003.ogg"}
	if got := w.pathsOf(starts[0]); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("paths:\n got %q\nwant %q", got, want)
	}
	if starts[0]["language"] != "en" {
		t.Fatalf("language %v", starts[0]["language"])
	}
	if !w.runner.Called(binDir + "/wsi-stream --ensure-server") {
		t.Fatal("the server was not ensured")
	}
	if got := w.stdout.String(); got != rec+".stt.md\n" {
		t.Fatalf("stdout %q, want the sidecar path", got)
	}
}

// S2. The sidecar: front matter, then paragraphs split at a pause of 3 s or more, each opened
// by its time.
func TestTheSidecarHoldsFrontMatterAndTimedParagraphs(t *testing.T) {
	w := newWorld(t)
	rec := w.recording("dictation-1", "COMPLETE", "aaa", "bbb")
	wantCode(t, w.run(w.root), term.ExitOK, w)
	want := `---
source: "dictation-1"
parts: 2
duration: "00:00:12"
language: "en"
model: "distil-large-v3.5"
transcribed: "2026-10-16T19:40:00Z"
---

**[00:00:00]** Hello there. Same paragraph.

**[00:00:09]** After a pause.
`
	if got := w.read(rec + ".stt.md"); got != want {
		t.Fatalf("sidecar:\n%s\nwant:\n%s", got, want)
	}
}

// S3. A recording with no speech still gets a sidecar, so it is not transcribed again.
func TestNoSpeechIsSaidInTheSidecar(t *testing.T) {
	w := newWorld(t)
	w.server.finish = func([]string) map[string]any { return done() }
	rec := w.recording("dictation-1", "COMPLETE", "aaa")
	wantCode(t, w.run(w.root), term.ExitOK, w)
	if got := w.read(rec + ".stt.md"); !strings.HasSuffix(got, "---\n\n_No speech found._\n") {
		t.Fatalf("sidecar:\n%s", got)
	}
}

// earlierSidecar is a transcript an earlier run wrote.
const earlierSidecar = "---\nsource: \"dictation-1\"\n---\n\n**[00:00:00]** Earlier.\n"

// S4. A recording that has its sidecar is done: nothing is sent, and the server is not ensured.
func TestADoneRecordingIsNotTranscribedAgain(t *testing.T) {
	w := newWorld(t)
	rec := w.recording("dictation-1", "COMPLETE", "aaa")
	w.write("dictation-1.stt.md", earlierSidecar)
	wantCode(t, w.run(w.root), term.ExitOK, w)
	if len(w.starts()) != 0 || w.runner.Called(binDir+"/wsi-stream") {
		t.Fatal("a done recording was transcribed again")
	}
	if w.read(rec+".stt.md") != earlierSidecar {
		t.Fatal("the sidecar was overwritten")
	}
}

// S4. A sidecar that is empty, or not one stt wrote (no front matter), is not done: it is a
// failure, so a recording is never hidden by a file cut short or emptied by someone else.
func TestAnEmptyOrForeignSidecarIsAFailureNotDone(t *testing.T) {
	for name, body := range map[string]string{"empty": "", "no front matter": "notes\n"} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			rec := w.recording("dictation-1", "COMPLETE", "aaa")
			w.write("dictation-1.stt.md", body)
			wantCode(t, w.run(w.root), term.ExitFailure, w)
			if !strings.Contains(w.stderr.String(), "stt: "+rec+": its sidecar "+rec+".stt.md is not a transcript") {
				t.Fatalf("stderr:\n%s", w.stderr.String())
			}
			if len(w.starts()) != 0 || w.read(rec+".stt.md") != body {
				t.Fatal("a foreign sidecar was transcribed over")
			}
			w.stdout.Reset()
			wantCode(t, w.run("--list", w.root), term.ExitOK, w)
			if !strings.HasPrefix(w.stdout.String(), "corrupt\t"+rec+"\t") {
				t.Fatalf("--list: %q", w.stdout.String())
			}
		})
	}
}

// S1. The parts go in the manifest's order, whatever their names sort as.
func TestPartsGoInManifestOrder(t *testing.T) {
	w := newWorld(t)
	w.write("rec/b.ogg", "bbb")
	w.write("rec/a.ogg", "aaa")
	sumA, sumB := md5.Sum([]byte("aaa")), md5.Sum([]byte("bbb"))
	w.write("rec/manifest.json", fmt.Sprintf(`{"state": "COMPLETE", "recordedMillis": 9000, "segments": [
		{"name": "b.ogg", "bytes": 3, "md5": %q}, {"name": "a.ogg", "bytes": 3, "md5": %q}]}`,
		hex.EncodeToString(sumB[:]), hex.EncodeToString(sumA[:])))
	wantCode(t, w.run(w.root), term.ExitOK, w)
	got := w.pathsOf(w.starts()[0])
	if want := []string{w.root + "/rec/b.ogg", w.root + "/rec/a.ogg"}; strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("paths %q, want manifest order %q", got, want)
	}
}

// S5. A recording still being made, or whose parts have not all synced, waits: no sidecar, not
// a failure.
func TestARecordingNotYetCompleteWaits(t *testing.T) {
	w := newWorld(t)
	w.recording("dictation-1", "RECORDING", "aaa")
	rec := w.recording("dictation-2", "COMPLETE", "aaa", "bbb")
	if err := os.Remove(rec + "/dictation-2-002.ogg"); err != nil {
		t.Fatal(err)
	}
	wantCode(t, w.run(w.root), term.ExitOK, w)
	if len(w.starts()) != 0 {
		t.Fatal("an incomplete recording was transcribed")
	}
	for _, want := range []string{
		"stt: waiting: " + w.root + "/dictation-1 (the manifest says RECORDING)",
		"stt: waiting: " + rec + " (part 2, dictation-2-002.ogg, has not synced yet)",
	} {
		if !strings.Contains(w.stderr.String(), want) {
			t.Fatalf("stderr lacks %q:\n%s", want, w.stderr.String())
		}
	}
}

// S6. A part whose checksum or size is not the manifest's is a corrupt recording: a failure.
func TestACorruptPartIsAFailure(t *testing.T) {
	for name, damage := range map[string]string{"checksum": "xyz", "size": "aaaa"} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			rec := w.recording("dictation-1", "COMPLETE", "aaa")
			w.write("dictation-1/dictation-1-001.ogg", damage)
			wantCode(t, w.run(w.root), term.ExitFailure, w)
			if !strings.Contains(w.stderr.String(), "stt: "+rec+": part 1, dictation-1-001.ogg, does not match the manifest") {
				t.Fatalf("stderr:\n%s", w.stderr.String())
			}
			if exists(rec + ".stt.md") {
				t.Fatal("a corrupt recording got a sidecar")
			}
		})
	}
}

// S7. A manifest that is not the phone app's JSON, names no parts, names a path, or lacks a
// checksum or a state, is a failure naming the manifest.
func TestABadManifestIsAFailure(t *testing.T) {
	for name, manifest := range map[string]string{
		"not JSON":    "{",
		"no parts":    `{"state": "COMPLETE", "segments": []}`,
		"a path name": `{"state": "COMPLETE", "segments": [{"name": "../x.ogg", "bytes": 1, "md5": "00"}]}`,
		"no checksum": `{"state": "COMPLETE", "segments": [{"name": "x.ogg", "bytes": 1}]}`,
		"no state":    `{"segments": [{"name": "x.ogg", "bytes": 1, "md5": "00"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			w.write("dictation-1/manifest.json", manifest)
			w.write("dictation-1/x.ogg", "a")
			wantCode(t, w.run(w.root), term.ExitFailure, w)
			if !strings.Contains(w.stderr.String(), "manifest.json") {
				t.Fatalf("stderr does not name the manifest:\n%s", w.stderr.String())
			}
		})
	}
}

// S8. A loose audio file is a recording of one part; its sidecar is the file's name plus
// .stt.md. Other files, and folders with no manifest, are not recordings.
func TestALooseAudioFileIsARecordingOfOnePart(t *testing.T) {
	w := newWorld(t)
	memo := w.write("memo.m4a", "m4a audio")
	w.write("notes.txt", "not audio")
	w.write("CLAUDE.md", "about this folder")
	w.write("other/thing.ogg", "inside a folder with no manifest")
	wantCode(t, w.run(w.root), term.ExitOK, w)
	starts := w.starts()
	if len(starts) != 1 || w.pathsOf(starts[0])[0] != memo {
		t.Fatalf("FILE_STARTs: %v", starts)
	}
	if !strings.Contains(w.read(memo+".stt.md"), "parts: 1\n") {
		t.Fatal("the loose file's sidecar is missing")
	}
}

// S18. A loose audio file changed less than a minute ago may still be being written (a copy
// into a watched folder): it waits, a line on stderr, and is sent on a later run. A recording
// folder needs no such wait: its manifest's sizes and MD5s say when it is whole.
func TestALooseFileStillBeingWrittenWaits(t *testing.T) {
	w := newWorld(t)
	memo := w.write("memo.m4a", "half an m4a")
	w.touch(memo, fixedNow.Add(-10*time.Second))
	rec := w.recording("dictation-1", "COMPLETE", "aaa")
	w.touch(filepath.Join(rec, "dictation-1-001.ogg"), fixedNow.Add(-time.Second))
	wantCode(t, w.run(w.root), term.ExitOK, w)
	if starts := w.starts(); len(starts) != 1 || w.pathsOf(starts[0])[0] == memo {
		t.Fatalf("FILE_STARTs: %v (only the folder may be sent)", starts)
	}
	if exists(memo + ".stt.md") {
		t.Fatal("a file still being written got a sidecar")
	}
	if !strings.Contains(w.stderr.String(), "waiting: "+memo+" (changed 10 s ago; it may still be being written)") {
		t.Fatalf("stderr:\n%s", w.stderr.String())
	}
	w.stdout.Reset()
	wantCode(t, w.run("--list", memo), term.ExitOK, w)
	if got := w.stdout.String(); !strings.HasPrefix(got, "waiting\t"+memo+"\t") {
		t.Fatalf("--list: %q", got)
	}
}

// S9. A path can name a recording folder or an audio file directly; a relative path is made
// absolute before it reaches the server.
func TestAPathCanNameOneRecording(t *testing.T) {
	w := newWorld(t)
	rec := w.recording("dictation-1", "COMPLETE", "aaa")
	w.recording("dictation-2", "COMPLETE", "bbb")
	memo := w.write("memo.wav", "wav")
	t.Chdir(w.root)
	wantCode(t, w.run("dictation-1", memo), term.ExitOK, w)
	if got := w.stdout.String(); got != rec+".stt.md\n"+memo+".stt.md\n" {
		t.Fatalf("stdout %q", got)
	}
}

// S10. Recordings run in name order, and the first failure stops the run: nothing after it is
// attempted, and no sidecar is left for the failed one.
func TestTheFirstFailureStopsTheRun(t *testing.T) {
	w := newWorld(t)
	first := w.recording("dictation-1", "COMPLETE", "aaa")
	second := w.recording("dictation-2", "COMPLETE", "bbb")
	w.recording("dictation-3", "COMPLETE", "ccc")
	w.server.finish = func(paths []string) map[string]any {
		if strings.Contains(paths[0], "dictation-2") {
			return map[string]any{
				"status": "failed", "segments": []seg{}, "count": 0,
				"error": "RuntimeError: CUDA failed with error out of memory",
			}
		}
		return done(seg{0, 1, "Fine."})
	}
	wantCode(t, w.run(w.root), term.ExitFailure, w)
	if len(w.starts()) != 2 {
		t.Fatalf("FILE_STARTs: %d, want 2 (the third must not start)", len(w.starts()))
	}
	if !exists(first+".stt.md") || exists(second+".stt.md") {
		t.Fatal("wrong sidecars after the failure")
	}
	want := "stt: " + second + ": the speech-to-text server failed: RuntimeError: CUDA failed with error out of memory"
	if !strings.Contains(w.stderr.String(), want) {
		t.Fatalf("stderr:\n%s", w.stderr.String())
	}
}

// S11. A server that is busy with another file, refuses, cannot be reached, or cannot be
// started, is a failure, and no sidecar is written.
func TestAServerThatCannotTakeTheRecordingIsAFailure(t *testing.T) {
	cases := map[string]struct {
		setup func(w *world)
		want  string
	}{
		"busy": {func(w *world) {
			w.server.start = func() map[string]any {
				return map[string]any{"status": "error", "busy": "file", "message": "A file is already being transcribed"}
			}
		}, "the speech-to-text server refused the recording: A file is already being transcribed"},
		"unreachable": {func(w *world) { w.socket = filepath.Join(w.root, "no.sock") }, "cannot reach the speech-to-text server"},
		"not started": {func(w *world) {
			w.runner.On([]string{binDir + "/wsi-stream", "--ensure-server"}, execx.Result{ExitCode: 1})
		}, "the speech-to-text server could not be started"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			tc.setup(w)
			rec := w.recording("dictation-1", "COMPLETE", "aaa")
			wantCode(t, w.run(w.root), term.ExitFailure, w)
			if !strings.Contains(w.stderr.String(), tc.want) {
				t.Fatalf("stderr lacks %q:\n%s", tc.want, w.stderr.String())
			}
			if exists(rec + ".stt.md") {
				t.Fatal("a failed recording got a sidecar")
			}
		})
	}
}

// S12. The language: --language, else the panel's setting ("system" is the locale's); "auto"
// is detection, sent as null.
func TestTheLanguage(t *testing.T) {
	cases := []struct {
		name, setting, lang string
		args                []string
		want                any
	}{
		{"setting", "de", "", nil, "de"},
		{"system locale", "system", "fr_FR.UTF-8", nil, "fr"},
		{"flag wins", "de", "", []string{"--language", "en"}, "en"},
		{"auto", "en", "", []string{"--language", "auto"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			w.lang = tc.lang
			w.runner.On([]string{binDir + "/wsi-setting", "language"}, execx.Result{Stdout: []byte(tc.setting + "\n")})
			w.recording("dictation-1", "COMPLETE", "aaa")
			wantCode(t, w.run(append(tc.args, w.root)...), term.ExitOK, w)
			if got := w.starts()[0]["language"]; got != tc.want {
				t.Fatalf("language %v, want %v", got, tc.want)
			}
		})
	}
}

// S13. Progress: each new segment's time and text go to stderr as they arrive.
func TestProgressGoesToStderr(t *testing.T) {
	w := newWorld(t)
	w.recording("dictation-1", "COMPLETE", "aaa")
	wantCode(t, w.run(w.root), term.ExitOK, w)
	if !strings.Contains(w.stderr.String(), "stt:   [00:00:09] After a pause.\n") {
		t.Fatalf("stderr:\n%s", w.stderr.String())
	}
}

// S14. --list prints each recording's state and transcribes nothing.
func TestListShowsEachRecordingAndRunsNothing(t *testing.T) {
	w := newWorld(t)
	doneRec := w.recording("dictation-1", "COMPLETE", "aaa")
	w.write("dictation-1.stt.md", earlierSidecar)
	pending := w.recording("dictation-2", "COMPLETE", "bbb")
	waiting := w.recording("dictation-3", "RECORDING", "ccc")
	wantCode(t, w.run("--list", w.root), term.ExitOK, w)
	want := "done\t" + doneRec + "\npending\t" + pending + "\nwaiting\t" + waiting + "\tthe manifest says RECORDING\n"
	if got := w.stdout.String(); got != want {
		t.Fatalf("--list:\n%s\nwant:\n%s", got, want)
	}
	if len(w.runner.Calls()) != 0 || len(w.starts()) != 0 {
		t.Fatal("--list ran something")
	}
}

// S15. Usage: no path, an unknown flag or a flag with no value is exit 64 and runs nothing; a
// path that does not exist or is not audio is exit 1 before anything runs; -h prints the usage.
func TestUsage(t *testing.T) {
	w := newWorld(t)
	wantCode(t, w.run("--help"), term.ExitOK, w)
	if !strings.HasPrefix(w.stdout.String(), "usage: stt") {
		t.Fatalf("--help: %q", w.stdout.String())
	}
	for _, args := range [][]string{nil, {"--frob", "x"}, {"--language"}, {"--list"}} {
		w := newWorld(t)
		wantCode(t, w.run(args...), term.ExitUsage, w)
		if len(w.runner.Calls()) != 0 {
			t.Fatalf("%q ran commands", args)
		}
	}
	for _, rel := range []string{"gone", "notes.txt"} {
		w := newWorld(t)
		w.write("notes.txt", "x")
		wantCode(t, w.run(filepath.Join(w.root, rel)), term.ExitFailure, w)
		if len(w.runner.Calls()) != 0 {
			t.Fatalf("%s: ran commands", rel)
		}
	}
}

// S17. A recording under --min-seconds (default 3) is short: not sent, no sidecar, counted;
// --list shows it and --short prints only those paths.
func TestAShortRecordingIsNotTranscribed(t *testing.T) {
	w := newWorld(t)
	tap := w.timedRecording("dictation-1", "COMPLETE", 1200, "a", "b")
	w.recording("dictation-2", "COMPLETE", "bbb")
	wantCode(t, w.run(w.root), term.ExitOK, w)
	if starts := w.starts(); len(starts) != 1 || strings.Contains(w.pathsOf(starts[0])[0], "dictation-1") {
		t.Fatalf("FILE_STARTs: %v", starts)
	}
	if exists(tap + ".stt.md") {
		t.Fatal("a short recording got a sidecar")
	}
	if !strings.Contains(w.stderr.String(), "too short 1 (stt --short lists them)") {
		t.Fatalf("stderr:\n%s", w.stderr.String())
	}

	w = newWorld(t)
	tap = w.timedRecording("dictation-1", "COMPLETE", 1200, "a", "b")
	w.recording("dictation-2", "COMPLETE", "bbb")
	wantCode(t, w.run("--short", w.root), term.ExitOK, w)
	if got := w.stdout.String(); got != tap+"\n" {
		t.Fatalf("--short: %q", got)
	}

	w = newWorld(t)
	tap = w.timedRecording("dictation-1", "COMPLETE", 1200, "a", "b")
	wantCode(t, w.run("--list", w.root), term.ExitOK, w)
	if got := w.stdout.String(); got != "short\t"+tap+"\t2.4 s\n" {
		t.Fatalf("--list: %q", got)
	}

	w = newWorld(t)
	w.timedRecording("dictation-1", "COMPLETE", 1200, "a", "b")
	wantCode(t, w.run("--min-seconds", "2", w.root), term.ExitOK, w)
	if len(w.starts()) != 1 {
		t.Fatal("--min-seconds 2 must send a 2.4 s recording")
	}
}

// S17. --min-seconds must be a number of seconds; --list and --short are not given together.
func TestMinSecondsAndReportUsage(t *testing.T) {
	for _, args := range [][]string{
		{"--min-seconds", "x", "p"},
		{"--min-seconds", "-1", "p"},
		{"--min-seconds"},
		{"--list", "--short", "p"},
	} {
		w := newWorld(t)
		wantCode(t, w.run(args...), term.ExitUsage, w)
	}
}

// S16. The summary line on stderr counts what the run did.
func TestTheSummary(t *testing.T) {
	w := newWorld(t)
	w.recording("dictation-1", "COMPLETE", "aaa")
	w.write("dictation-1.stt.md", earlierSidecar)
	w.recording("dictation-2", "COMPLETE", "bbb")
	w.recording("dictation-3", "RECORDING", "ccc")
	wantCode(t, w.run(w.root), term.ExitOK, w)
	if !strings.Contains(w.stderr.String(), "stt: transcribed 1, already done 1, waiting 1\n") {
		t.Fatalf("stderr:\n%s", w.stderr.String())
	}
}
