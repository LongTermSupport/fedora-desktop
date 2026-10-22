package link

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

const (
	sessA = "aaaaaaaa-0000-4000-8000-000000000001"
	sessB = "bbbbbbbb-0000-4000-8000-000000000002"
	// The process chain: the session (CMD, 100) runs Claude Code (101), whose hook shell
	// (102) runs the CLI (103), the peer.
	pidSession, pidClaude, pidHook, pidPeer = 100, 101, 102, 103
)

// factsWorld is a home, a process table and a session's facts over them.
type factsWorld struct {
	*home

	proc    *fakeProc
	s       *Session
	claudes []state.Process
	starts  int
	cfgDir  string
	runtime string
}

func newFactsWorld(t *testing.T) *factsWorld {
	t.Helper()
	w := &factsWorld{home: newHome(t), proc: newFakeProc()}
	w.cfgDir = filepath.Join(w.dir, ".claude")
	w.runtime = w.dir + "/xdg"
	w.proc.add(pidSession, 50, 1000)
	w.proc.add(pidClaude, pidSession, 1001)
	w.proc.add(pidHook, pidClaude, 1002)
	w.proc.add(pidPeer, pidHook, 1003)
	w.s = w.open(t)
	return w
}

func (w *factsWorld) open(t *testing.T) *Session {
	t.Helper()
	s, err := newSession(sessionDeps{
		runRoot: w.root, stateRoot: w.root, layout: w.layout, run: w.run, proc: w.proc, clock: w.clk,
		runtimeDir: w.runtime, claudeDir: w.cfgDir, cwd: "/work/my.repo_x",
		onClaude:  func(p state.Process) { w.claudes = append(w.claudes, p) },
		onStarted: func() { w.starts++ },
		log:       w.log,
	})
	check(t, err)
	return s
}

func (w *factsWorld) facts(op, id, source string) Facts {
	sock, err := paths.InboxSocket(w.runtime, pidClaude)
	if err != nil {
		panic(err)
	}
	return Facts{
		Op: op, Peer: pidPeer, SessionID: id, Source: source, Socket: sock,
		Transcript: w.cfgDir + "/projects/-work-my-repo-x/" + id + ".jsonl",
	}
}

func (w *factsWorld) sessionFile(t *testing.T) state.Session {
	t.Helper()
	s, err := state.Read(w.root, w.layout.Run(w.run).Session(), state.MaxSession, state.DecodeSession)
	check(t, err)
	return s
}

func (w *factsWorld) conversation(t *testing.T) state.Conversation {
	t.Helper()
	c, err := state.Read(w.root, w.layout.ConversationFile(), state.MaxConversation, state.DecodeConversation)
	check(t, err)
	return c
}

func refusedWith(t *testing.T, err error, code, words string) {
	t.Helper()
	xe, ok := errors.AsType[*exits.Error](err)
	if !ok {
		t.Fatalf("err %v, want an exits error %s", err, code)
	}
	if xe.Code != code || !strings.Contains(xe.Message, words) {
		t.Fatalf("refused %s %q, want %s with %q", xe.Code, xe.Message, code, words)
	}
}

func TestFactsWaitForTheSessionPID(t *testing.T) {
	w := newFactsWorld(t)
	refusedWith(t, w.s.AcceptFacts(w.facts(wire.OpHookSessionStart, sessA, state.SourceStartup)), exits.CodeRuntime, "not started")
}

func TestFactsAreWrittenAndTheStartTold(t *testing.T) {
	w := newFactsWorld(t)
	w.s.SetSessionPID(state.Process{PID: pidSession, StartTicks: 1000})
	check(t, w.s.AcceptFacts(w.facts(wire.OpHookSessionStart, sessA, state.SourceStartup)))
	f := w.sessionFile(t)
	if *f.SessionID != sessA || *f.Source != state.SourceStartup || f.Claude.PID != pidClaude || f.Claude.StartTicks != 1001 {
		t.Fatalf("session.json %+v", f)
	}
	if c := w.conversation(t); c.SessionID != sessA || c.WrittenMS != startWall || c.Run != w.run {
		t.Fatalf("conversation %+v", c)
	}
	if len(w.claudes) != 1 || w.claudes[0].PID != pidClaude || w.starts != 1 {
		t.Fatalf("claude told %v, starts %d", w.claudes, w.starts)
	}
	// A second session-start of the same session tells nothing again.
	check(t, w.s.AcceptFacts(w.facts(wire.OpHookSessionStart, sessA, state.SourceCompact)))
	if len(w.claudes) != 1 || w.starts != 1 || *w.sessionFile(t).Source != state.SourceCompact {
		t.Fatalf("claude told %v, starts %d", w.claudes, w.starts)
	}
	// A new link reads the facts back: Claude Code's pid is known.
	again := w.open(t)
	again.SetSessionPID(state.Process{PID: pidSession, StartTicks: 1000})
	other := w.facts(wire.OpHookPrompt, sessA, "")
	other.Socket = w.runtime + "/cc-socks/102.sock"
	refusedWith(t, again.AcceptFacts(other), exits.CodeClaude, "one Claude Code per run")
}

func TestAFirstOpThatIsNotAStartIsAStartup(t *testing.T) {
	w := newFactsWorld(t)
	w.s.SetSessionPID(state.Process{PID: pidSession, StartTicks: 1000})
	check(t, w.s.AcceptFacts(w.facts(wire.OpHookPrompt, sessA, "")))
	if f := w.sessionFile(t); *f.Source != state.SourceStartup {
		t.Fatalf("source %s", *f.Source)
	}
	if w.starts != 0 {
		t.Fatal("a prompt told the start")
	}
	check(t, w.s.AcceptFacts(w.facts(wire.OpHookStop, sessA, "")))
	if f := w.sessionFile(t); *f.Source != state.SourceStartup {
		t.Fatalf("source %s", *f.Source)
	}
}

func TestTheChainIsWalkedAtMost64Steps(t *testing.T) {
	for _, c := range []struct {
		steps int
		ok    bool
	}{{64, true}, {65, false}} {
		w := newFactsWorld(t)
		w.s.SetSessionPID(state.Process{PID: pidSession, StartTicks: 1000})
		// The peer is steps parents below the session: 103 is the hook, then 1000 on.
		parent := pidHook
		peer := pidHook
		for i := range c.steps - 2 {
			peer = 1000 + i
			w.proc.add(peer, parent, 1)
			parent = peer
		}
		f := w.facts(wire.OpHookSessionStart, sessA, state.SourceStartup)
		f.Peer = peer
		err := w.s.AcceptFacts(f)
		if c.ok {
			check(t, err)
			continue
		}
		refusedWith(t, err, exits.CodeRuntime, "session's process")
	}
}

func TestTheChainMustReachTheSessionItself(t *testing.T) {
	w := newFactsWorld(t)
	// The session pid reused: its start ticks differ.
	w.s.SetSessionPID(state.Process{PID: pidSession, StartTicks: 999})
	refusedWith(t, w.s.AcceptFacts(w.facts(wire.OpHookSessionStart, sessA, state.SourceStartup)), exits.CodeRuntime, "session's process")
	w.s.SetSessionPID(state.Process{PID: pidSession, StartTicks: 1000})
	gone := w.facts(wire.OpHookSessionStart, sessA, state.SourceStartup)
	gone.Peer = 7777
	refusedWith(t, w.s.AcceptFacts(gone), exits.CodeRuntime, "session's process")
}

func TestTheSocketIsOnlyAChainPIDsBelowTheSession(t *testing.T) {
	w := newFactsWorld(t)
	w.s.SetSessionPID(state.Process{PID: pidSession, StartTicks: 1000})
	for name, sock := range map[string]string{
		"above the session": w.runtime + "/cc-socks/50.sock",
		"off the chain":     w.runtime + "/cc-socks/999.sock",
		"another runtime":   w.dir + "/other/cc-socks/101.sock",
		"not a socket name": w.runtime + "/cc-socks/101.sockx",
		"not a pid":         w.runtime + "/cc-socks/abc.sock",
	} {
		f := w.facts(wire.OpHookSessionStart, sessA, state.SourceStartup)
		f.Socket = sock
		if err := w.s.AcceptFacts(f); err == nil {
			t.Fatalf("%s: a socket was accepted", name)
		}
	}
	// The session process's own socket is on the chain, at the session.
	f := w.facts(wire.OpHookSessionStart, sessA, state.SourceStartup)
	f.Socket = w.runtime + "/cc-socks/100.sock"
	check(t, w.s.AcceptFacts(f))
	if w.claudes[0].PID != pidSession {
		t.Fatalf("claude %v", w.claudes)
	}
}

func TestTheTranscriptIsOnlyTheDerivedPath(t *testing.T) {
	w := newFactsWorld(t)
	w.s.SetSessionPID(state.Process{PID: pidSession, StartTicks: 1000})
	for name, path := range map[string]string{
		"another project": w.cfgDir + "/projects/-work-other/" + sessA + ".jsonl",
		"another id":      w.cfgDir + "/projects/-work-my-repo-x/" + sessB + ".jsonl",
		"another dir":     w.dir + "/.claudex/projects/-work-my-repo-x/" + sessA + ".jsonl",
	} {
		f := w.facts(wire.OpHookSessionStart, sessA, state.SourceStartup)
		f.Transcript = path
		refusedWith(t, w.s.AcceptFacts(f), exits.CodeRuntime, "transcript")
		if name == "" {
			t.Fatal(name)
		}
	}
	// A link on the way is refused, though the path is the derived one.
	check(t, os.MkdirAll(w.cfgDir, 0o700))
	check(t, os.Symlink(w.dir, w.cfgDir+"/projects"))
	refusedWith(t, w.s.AcceptFacts(w.facts(wire.OpHookSessionStart, sessA, state.SourceStartup)), exits.CodeRuntime, "transcript")
}

func TestAnIDSwapIsOnlyAClearOrAResume(t *testing.T) {
	w := newFactsWorld(t)
	w.s.SetSessionPID(state.Process{PID: pidSession, StartTicks: 1000})
	check(t, w.s.AcceptFacts(w.facts(wire.OpHookSessionStart, sessA, state.SourceStartup)))
	refusedWith(t, w.s.AcceptFacts(w.facts(wire.OpHookPrompt, sessB, "")), exits.CodeRuntime, "session id")
	refusedWith(t, w.s.AcceptFacts(w.facts(wire.OpHookSessionStart, sessB, state.SourceCompact)), exits.CodeRuntime, "session id")
	check(t, w.s.AcceptFacts(w.facts(wire.OpHookSessionStart, sessB, state.SourceClear)))
	if c := w.conversation(t); c.SessionID != sessB || c.Source != state.SourceClear {
		t.Fatalf("conversation %+v", c)
	}
	check(t, w.s.AcceptFacts(w.facts(wire.OpHookSessionStart, sessA, state.SourceResume)))
	// A new link's first op may carry a new id: the session moved on while no link ran.
	again := w.open(t)
	again.SetSessionPID(state.Process{PID: pidSession, StartTicks: 1000})
	check(t, again.AcceptFacts(w.facts(wire.OpHookPrompt, sessB, "")))
	if f := w.sessionFile(t); *f.SessionID != sessB || *f.Source != state.SourceResume {
		t.Fatalf("session.json %+v", f)
	}
	refusedWith(t, again.AcceptFacts(w.facts(wire.OpHookPrompt, sessA, "")), exits.CodeRuntime, "session id")
}

func TestAKeeperNamedClaudeFixesThePID(t *testing.T) {
	w := newFactsWorld(t)
	w.s.SetSessionPID(state.Process{PID: pidSession, StartTicks: 1000})
	w.s.SetClaude(state.Process{PID: pidHook, StartTicks: 1002})
	refusedWith(t, w.s.AcceptFacts(w.facts(wire.OpHookSessionStart, sessA, state.SourceStartup)), exits.CodeClaude, "one Claude Code per run")
	if len(w.claudes) != 0 {
		t.Fatal("the keeper was told its own claude")
	}
}

func TestABadIDOrSourceIsRefused(t *testing.T) {
	w := newFactsWorld(t)
	w.s.SetSessionPID(state.Process{PID: pidSession, StartTicks: 1000})
	refusedWith(t, w.s.AcceptFacts(w.facts(wire.OpHookSessionStart, "not-an-id", state.SourceStartup)), exits.CodeRuntime, "session id")
	refusedWith(t, w.s.AcceptFacts(w.facts(wire.OpHookSessionStart, sessA, "rebooted")), exits.CodeRuntime, "source")
	// The claude's start ticks unreadable: it has gone.
	w.proc.mu.Lock()
	delete(w.proc.ticks, pidClaude)
	w.proc.mu.Unlock()
	refusedWith(t, w.s.AcceptFacts(w.facts(wire.OpHookSessionStart, sessA, state.SourceStartup)), exits.CodeRuntime, "session's process")
}

func TestACorruptSessionFileStartsAfresh(t *testing.T) {
	w := newFactsWorld(t)
	check(t, w.root.Replace(w.layout.Run(w.run).Session(), []byte("{"), 0o600))
	s := w.open(t)
	s.SetSessionPID(state.Process{PID: pidSession, StartTicks: 1000})
	check(t, s.AcceptFacts(w.facts(wire.OpHookSessionStart, sessA, state.SourceStartup)))
	if !strings.Contains(w.log.String(), "session.json") {
		t.Fatalf("log %q", w.log.String())
	}
	if s.EndReason() != nil {
		t.Fatal("an end reason from nowhere")
	}
}
