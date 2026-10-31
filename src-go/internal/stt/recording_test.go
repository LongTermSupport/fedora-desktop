package stt

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"
	"time"
)

func TestIsAudioByExtensionAnyCase(t *testing.T) {
	for path, want := range map[string]bool{
		"a.ogg": true, "a.OPUS": true, "a.m4a": true, "a.wav": true, "a.mp3": true, "a.flac": true,
		"a.txt": false, "a.ogg.stt.md": false, "manifest.json": false, "ogg": false,
	} {
		if got := isAudio(path); got != want {
			t.Errorf("isAudio(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestTheSidecarIsBesideTheRecording(t *testing.T) {
	if got := folder("/r/dictation-1").sidecar; got != "/r/dictation-1.stt.md" {
		t.Fatalf("folder sidecar %q", got)
	}
	if got := audioFile("/r/memo.m4a").sidecar; got != "/r/memo.m4a.stt.md" {
		t.Fatalf("file sidecar %q", got)
	}
}

func TestReadManifestNamesWhatIsWrong(t *testing.T) {
	for want, body := range map[string]string{
		"is not the phone app's JSON": "[1]",
		"has no state":                `{"segments": [{"name": "a.ogg", "bytes": 1, "md5": "00"}]}`,
		"names no parts":              `{"state": "COMPLETE"}`,
		"is not a file name":          `{"state": "COMPLETE", "segments": [{"name": "..", "bytes": 1, "md5": "00"}]}`,
		"has no size or no MD5":       `{"state": "COMPLETE", "segments": [{"name": "a.ogg", "md5": "00"}]}`,
	} {
		path := filepath.Join(t.TempDir(), manifestName)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, reason := readManifest(path); !strings.Contains(reason, want) {
			t.Errorf("%s: reason %q, want %q", body, reason, want)
		}
	}
}

// The front matter is read strictly: a read that fails, before or inside it, and an audio line
// that does not parse, are errors, never a sidecar quietly taken as done.
func TestReadFrontMatterReportsWhatItCannotRead(t *testing.T) {
	broken := errors.New("disk gone")
	for name, in := range map[string]io.Reader{
		"before":   iotest.ErrReader(broken),
		"inside":   io.MultiReader(strings.NewReader("---\nsource: \"x\"\n"), iotest.ErrReader(broken)),
		"bad line": strings.NewReader("---\naudio: [not json\n---\n"),
	} {
		recorded, opened, err := readFrontMatter(in)
		if err == nil {
			t.Errorf("%s: no error (recorded %v, opened %v)", name, recorded, opened)
		}
	}
}

// A sidecar that cannot be read (here a folder in its place) is corrupt, not done.
func TestAnUnreadableSidecarIsCorrupt(t *testing.T) {
	r := audioFile(filepath.Join(t.TempDir(), "memo.m4a"))
	if err := os.Mkdir(r.sidecar, 0o700); err != nil {
		t.Fatal(err)
	}
	recorded, st, reason, found := r.sidecarState()
	if st != stateCorrupt || !found || recorded != nil || !strings.Contains(reason, "cannot read its sidecar") {
		t.Fatalf("state %v, found %v, reason %q", st, found, reason)
	}
}

// audioChanged: a folder whose manifest is bad is left to the manifest's own checks; a loose
// file that is gone, or cannot be read for its MD5, cannot be judged; a fingerprint of other
// than one part is a change.
func TestAudioChangedSaysWhenItCannotJudge(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, manifestName), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	notAFile := audioFile(t.TempDir())
	for name, tc := range map[string]struct {
		r        recording
		recorded []audioPart
		changed  bool
		why      string
	}{
		"bad manifest":        {folder(dir), nil, true, ""},
		"gone":                {audioFile(filepath.Join(dir, "gone.m4a")), nil, false, "cannot be read"},
		"no parts recorded":   {notAFile, nil, true, ""},
		"cannot read its MD5": {notAFile, []audioPart{{Name: "x", Bytes: -1}}, false, "cannot be read"},
	} {
		changed, why := tc.r.audioChanged(tc.recorded)
		if changed != tc.changed || !strings.Contains(why, tc.why) || (tc.why == "") != (why == "") {
			t.Errorf("%s: changed %v, why %q", name, changed, why)
		}
	}
}

// A loose file that is gone, or cannot be read for its MD5, is corrupt.
func TestALooseFileThatCannotBeReadIsCorrupt(t *testing.T) {
	later := check{verify: true, now: fixedNow.Add(100 * 365 * 24 * time.Hour)}
	for name, r := range map[string]recording{
		"gone":       audioFile(filepath.Join(t.TempDir(), "gone.m4a")),
		"not a file": audioFile(t.TempDir()),
	} {
		if st, reason := r.looseFile(later); st != stateCorrupt || !strings.Contains(reason, "cannot be read") {
			t.Errorf("%s: %v %q", name, st, reason)
		}
	}
	if sum, err := md5File(filepath.Join(t.TempDir(), "gone")); err == nil {
		t.Errorf("md5File of a missing file: %q, no error", sum)
	}
}

// sleep ends with ctx, naming why, and otherwise when its time is up.
func TestSleepEndsWithItsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleep(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled: %v", err)
	}
	if err := sleep(context.Background(), 0); err != nil {
		t.Fatalf("elapsed: %v", err)
	}
}

func TestNewEnvIsTheProductionEnvironment(t *testing.T) {
	env, err := NewEnv()
	if err != nil {
		t.Fatal(err)
	}
	if env.Socket != DefaultSocket() || !strings.HasSuffix(env.Socket, "/wsi-stream.socket") || env.Sleep == nil {
		t.Fatalf("env %+v", env)
	}
}

func TestStateStrings(t *testing.T) {
	for s, want := range map[state]string{statePending: "pending", stateDone: "done", stateWaiting: "waiting"} {
		if s.String() != want {
			t.Errorf("%d: %q", s, s.String())
		}
	}
}
