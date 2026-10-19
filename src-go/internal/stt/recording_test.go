package stt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestStateStrings(t *testing.T) {
	for s, want := range map[state]string{statePending: "pending", stateDone: "done", stateWaiting: "waiting"} {
		if s.String() != want {
			t.Errorf("%d: %q", s, s.String())
		}
	}
}
