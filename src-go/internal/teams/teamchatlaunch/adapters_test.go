package teamchatlaunch

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/cli"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestAnnouncerJudgesTheVerbsOutcome(t *testing.T) {
	run, err := paths.ParseRunID("0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	n := seat.Notice{Bundle: "/s/held", SinceMS: 5, Run: run}
	for name, c := range map[string]struct {
		code   term.ExitCode
		stderr string
		want   seat.Outcome
		fails  bool
	}{
		"posted":    {code: term.ExitOK, want: seat.Outcome{Posted: true}},
		"never":     {code: term.ExitOK, stderr: "no notice: launch failed\n", want: seat.Outcome{Why: "no notice: launch failed"}},
		"transient": {code: 20, stderr: "did not answer\n", fails: true},
	} {
		a := Announcer{Verb: func(e cli.Env, _ cli.Invocation) term.ExitCode {
			if _, werr := e.Stderr.Write([]byte(c.stderr)); werr != nil {
				t.Fatal(werr)
			}
			return c.code
		}}
		out, err := a.Disconnected(context.Background(), n)
		if (err != nil) != c.fails || out != c.want {
			t.Fatalf("%s: %+v %v", name, out, err)
		}
	}
}

func TestRegistrySessionsIsWhetherTheRecordExists(t *testing.T) {
	dir := t.TempDir()
	store := registry.New(dir)
	if err := store.Write(registry.Record{Name: "kept", Dir: "/work", Launcher: "ccy", Prefix: "p"}); err != nil {
		t.Fatal(err)
	}
	s := RegistrySessions{Open: func() (*registry.Store, error) { return store, nil }}
	if ok, err := s.Restorable(mustSession(t, "kept")); err != nil || !ok {
		t.Fatalf("kept: %v %v", ok, err)
	}
	if ok, err := s.Restorable(mustSession(t, "gone")); err != nil || ok {
		t.Fatalf("gone: %v %v", ok, err)
	}
	broken := RegistrySessions{Open: func() (*registry.Store, error) { return nil, errors.New("no home") }}
	if _, err := broken.Restorable(mustSession(t, "kept")); err == nil {
		t.Fatal("an unopened registry answered")
	}
}

func TestAnnouncerFailsTransientlyOnABundleItCannotRead(t *testing.T) {
	run, err := paths.ParseRunID("0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	a := Announcer{}
	_, err = a.Disconnected(context.Background(), seat.Notice{Bundle: filepath.Join(t.TempDir(), "held"), SinceMS: 1, Run: run})
	if err == nil {
		t.Fatal("a missing bundle was announced")
	}
}
