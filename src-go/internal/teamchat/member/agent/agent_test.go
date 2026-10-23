package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

const (
	home = "/home/u"
	cwd  = "/work/my.repo_x"
	sid  = "0b5e3f6a-1c2d-4e5f-8a9b-0c1d2e3f4a5b"
)

func TestTheAgentsAreTheLaunchValues(t *testing.T) {
	if string(Claude) != state.LaunchClaude || string(Plain) != state.LaunchPlain {
		t.Fatalf("Claude %q, Plain %q", Claude, Plain)
	}
}

func TestForGivesEachAgentItsCapabilities(t *testing.T) {
	for a, want := range map[Agent]Capabilities{
		Claude: {Delivery: state.DeliverySocket, SessionStart: true},
		Plain:  {Delivery: state.DeliveryNone, SessionStart: false},
	} {
		got, err := For(a)
		if err != nil || got.Capabilities != want || got.Preparer == nil {
			t.Errorf("For(%q) = %+v, %v; want %+v", a, got, err, want)
		}
	}
}

func TestForRefusesAnyOtherValue(t *testing.T) {
	for _, a := range []Agent{"", "opencode", "Claude"} {
		if got, err := For(a); err == nil || got.Preparer != nil {
			t.Errorf("For(%q) = %+v, %v; want an error", a, got, err)
		}
	}
}

func TestMaxSettingsIsTheKitSettingsCap(t *testing.T) {
	if MaxSettings != 64<<10 {
		t.Fatalf("MaxSettings %d", MaxSettings)
	}
}

func TestThePlainPreparerReadsNothingAndAppendsNothing(t *testing.T) {
	a, err := For(Plain)
	if err != nil {
		t.Fatal(err)
	}
	// Every function is nil: a plain run that called one would panic.
	l, err := a.Preparer.Prepare(RunFacts{Inbound: state.InboundAccept, ResumeRecorded: true})
	if err != nil || len(l.Args) != 0 {
		t.Fatalf("Prepare = %+v, %v", l, err)
	}
}

// facts is a claude run's facts over fakes: the kit's settings text, the files written, the
// conversation record and the log.
type facts struct {
	t        *testing.T
	settings string
	readErr  error
	writeErr error
	conv     state.Conversation
	convErr  error
	written  map[string]string
	limit    int
	log      []string
	f        RunFacts
}

func newFacts(t *testing.T) *facts {
	t.Helper()
	kit, err := paths.KitAt("/home/u/.local/share/teamchat/kits/k1")
	if err != nil {
		t.Fatal(err)
	}
	layout, err := paths.HomeLayout(home, "/run/user/1")
	if err != nil {
		t.Fatal(err)
	}
	id, err := paths.ParseRunID("0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	x := &facts{t: t, settings: `{"theme":"dark"}`, written: map[string]string{}, conv: state.Conversation{SessionID: sid}}
	x.f = RunFacts{
		Inbound: state.InboundAccept, Kit: kit, Run: layout.Run(id), Cwd: cwd, Home: home,
		ReadTrusted: func(path string, limit int) ([]byte, error) {
			if path != kit.Settings() {
				t.Errorf("read %s, not the kit's settings", path)
			}
			x.limit = limit
			return []byte(x.settings), x.readErr
		},
		WriteRunFile: func(path string, data []byte) error {
			if x.writeErr != nil {
				return x.writeErr
			}
			x.written[path] = string(data)
			return nil
		},
		Conversation: func() (state.Conversation, error) { return x.conv, x.convErr },
		Logf:         func(format string, args ...any) { x.log = append(x.log, fmt.Sprintf(format, args...)) },
	}
	return x
}

func (x *facts) prepare() (Launch, error) {
	x.t.Helper()
	a, err := For(Claude)
	if err != nil {
		x.t.Fatal(err)
	}
	return a.Preparer.Prepare(x.f)
}

func (x *facts) tail() []string {
	return []string{"--plugin-dir", x.f.Kit.PluginDir(), "--settings", x.f.Run.Settings()}
}

func TestAClaudeRunWritesItsSettingsAndAppendsThePluginAndThem(t *testing.T) {
	for _, inbound := range []string{state.InboundAccept, state.InboundRefuse} {
		x := newFacts(t)
		x.f.Inbound = inbound
		l, err := x.prepare()
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(l.Args, x.tail()) {
			t.Errorf("args %q", l.Args)
		}
		want := `{"crossSessionInbound":"` + inbound + `","theme":"dark"}`
		if got := x.written[x.f.Run.Settings()]; got != want || len(x.written) != 1 {
			t.Errorf("written %q, want %s", x.written, want)
		}
		if x.limit != MaxSettings || len(x.log) != 0 {
			t.Errorf("limit %d, log %q", x.limit, x.log)
		}
	}
}

func TestKitSettingsThatAreNotAnObjectWithoutTheKeyAreRefused(t *testing.T) {
	for name, tc := range map[string]struct {
		text string
		err  error
		log  string
	}{
		"unreadable":  {err: errors.New("not regular"), log: "not regular"},
		"not JSON":    {text: "{", log: "unexpected end of JSON input"},
		"null":        {text: "null", log: "<nil>"},
		"has the key": {text: `{"crossSessionInbound":"accept"}`, log: "<nil>"},
	} {
		t.Run(name, func(t *testing.T) {
			x := newFacts(t)
			x.settings, x.readErr = tc.text, tc.err
			l, err := x.prepare()
			var e *exits.Error
			if !errors.As(err, &e) || e.Code != exits.CodeRuntime || len(l.Args) != 0 {
				t.Fatalf("Prepare = %+v, %v", l, err)
			}
			want := "the kit's settings " + x.f.Kit.Settings() + " are not a JSON object without the inbound key"
			if e.Message != want {
				t.Errorf("message %q", e.Message)
			}
			if len(x.log) != 1 || !strings.HasPrefix(x.log[0], "read "+x.f.Kit.Settings()+": ") || !strings.HasSuffix(x.log[0], tc.log) {
				t.Errorf("log %q", x.log)
			}
			if len(x.written) != 0 {
				t.Errorf("written %q", x.written)
			}
		})
	}
}

func TestAFailedSettingsWriteIsReturnedUnchanged(t *testing.T) {
	x := newFacts(t)
	x.f.ResumeRecorded = true
	x.writeErr = errors.New("the keeper's own")
	l, err := x.prepare()
	if !errors.Is(err, x.writeErr) || len(l.Args) != 0 || len(x.log) != 0 {
		t.Fatalf("Prepare = %+v, %v, log %q", l, err, x.log)
	}
}

// transcript makes the session's transcript under the Claude directory dir.
func transcript(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "projects", "-work-my-repo-x", sid+".jsonl")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestARecordedSessionWhoseTranscriptIsThereIsResumed(t *testing.T) {
	dir := t.TempDir()
	transcript(t, dir)
	x := newFacts(t)
	x.f.ResumeRecorded, x.f.ClaudeConfigDir = true, &dir
	l, err := x.prepare()
	if err != nil {
		t.Fatal(err)
	}
	if want := append([]string{"--resume", sid}, x.tail()...); !slices.Equal(l.Args, want) {
		t.Errorf("args %q", l.Args)
	}
	if len(x.log) != 0 {
		t.Errorf("log %q", x.log)
	}
}

func TestNoResumeWithoutResumeRecorded(t *testing.T) {
	x := newFacts(t)
	x.f.Conversation = func() (state.Conversation, error) {
		t.Error("the record was read")
		return state.Conversation{}, nil
	}
	l, err := x.prepare()
	if err != nil || !slices.Equal(l.Args, x.tail()) {
		t.Fatalf("Prepare = %+v, %v", l, err)
	}
}

func TestAResumeRefusedSaysWhyAndAppendsTheRest(t *testing.T) {
	dir := t.TempDir()
	notFile := filepath.Join(dir, "projects", "-work-my-repo-x", sid+".jsonl")
	for name, tc := range map[string]struct {
		set func(x *facts)
		log string
	}{
		"no record": {
			set: func(x *facts) { x.convErr = errors.New("the conversation record: missing") },
			log: "no resume: the conversation record: missing",
		},
		"no session id": {set: func(x *facts) { x.conv.SessionID = "" }, log: "no resume: the conversation record names no session id"},
		"a path":        {set: func(x *facts) { x.conv.SessionID = "a/b" }, log: "no resume: the conversation record names no session id"},
		"a dot name":    {set: func(x *facts) { x.conv.SessionID = ".x" }, log: "no resume: the conversation record names no session id"},
		"no transcript under HOME": {
			set: func(*facts) {},
			log: "no resume: the transcript /home/u/.claude/projects/-work-my-repo-x/" + sid + ".jsonl is not a regular file (no such file or directory)",
		},
		"a directory": {
			set: func(x *facts) {
				if err := os.MkdirAll(notFile, 0o700); err != nil {
					t.Fatal(err)
				}
				x.f.ClaudeConfigDir = &dir
			},
			log: "no resume: the transcript " + notFile + " is not a regular file (<nil>)",
		},
	} {
		t.Run(name, func(t *testing.T) {
			x := newFacts(t)
			x.f.ResumeRecorded = true
			tc.set(x)
			l, err := x.prepare()
			if err != nil || !slices.Equal(l.Args, x.tail()) {
				t.Fatalf("Prepare = %+v, %v", l, err)
			}
			if len(x.log) != 1 || x.log[0] != tc.log {
				t.Errorf("log %q, want %q", x.log, tc.log)
			}
			if len(x.written) != 1 {
				t.Errorf("the settings were not written before the resume rule: %q", x.written)
			}
		})
	}
}

func TestProjectDirIsEveryByteButLettersAndDigitsAsADash(t *testing.T) {
	if got := projectDir("/a/B9.c_d-é"); got != "-a-B9-c-d---" {
		t.Fatalf("projectDir %q", got)
	}
}
