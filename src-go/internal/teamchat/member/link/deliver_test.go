package link

import (
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/engine"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

const heldToken = "inbox-" + "secret"

// engineConfig makes the link's engine as usual and hands the test the config it was made with.
func (w *linkWorld) engineConfig() <-chan engine.Config {
	made := make(chan engine.Config, 1)
	w.cfg.Engine = func(c engine.Config) (*engine.Engine, error) {
		made <- c
		return engine.New(c)
	}
	return made
}

// startedSession is session.json of a session already started, as a restarted link finds it.
func (w *linkWorld) startedSession(u state.Unusable) state.Session {
	w.t.Helper()
	id, source := sessA, state.SourceStartup
	sock, err := paths.InboxSocket(w.dir+"/xdg", 101)
	check(w.t, err)
	transcript := w.dir + "/.claude/projects/-work/" + sessA + ".jsonl"
	s := state.Session{
		Run: w.run, SessionID: &id, Source: &source, Transcript: &transcript, Socket: &sock,
		Claude: &state.Process{PID: 101, StartTicks: 1001}, Told: state.Told{Roles: map[paths.TeamID]string{}},
		Unusable: u,
	}
	data, err := s.Encode()
	check(w.t, err)
	check(w.t, w.root.Create(w.layout.Run(w.run).Session(), data, 0o600))
	return s
}

func (w *linkWorld) sessionFile() state.Session {
	w.t.Helper()
	s, err := state.Read(w.root, w.layout.Run(w.run).Session(), state.MaxSession, state.DecodeSession)
	check(w.t, err)
	return s
}

// sync is a message the link only logs: once its line is in link.log, every message sent
// before it has been handled.
func (w *linkWorld) sync(n int) {
	w.t.Helper()
	w.send(wire.Msg{Kind: wire.KindReady, Ready: &wire.Ready{}}, nil)
	until(w.t, func() bool { return strings.Count(w.log.String(), "not the keeper's to send") == n })
}

func TestARestartedLinkDeliversOnTheToken(t *testing.T) {
	w := newLinkWorld(t)
	made := w.engineConfig()
	w.startedSession(state.Unusable{HookStdin: 1, Render: 3})
	w.ready()
	c := <-made
	if c.Inbox == nil || c.OnUnusable == nil || c.Unusable != (state.Unusable{HookStdin: 1, Render: 3}) {
		t.Fatalf("engine config inbox %v, on unusable set %t, unusable %+v", c.Inbox, c.OnUnusable != nil, c.Unusable)
	}
	l := w.link()
	if l.eng.Live() {
		t.Fatal("live before the token")
	}
	w.send(wire.Msg{Kind: wire.KindToken, Token: &wire.Token{Token: heldToken}}, nil)
	until(t, l.eng.Live)
	if strings.Contains(w.log.String(), heldToken) {
		t.Fatalf("the token is in link.log: %q", w.log.String())
	}
}

func TestATokenBeforeAnyHookIsNotLive(t *testing.T) {
	w := newLinkWorld(t)
	w.ready()
	l := w.link()
	w.send(wire.Msg{Kind: wire.KindToken, Token: &wire.Token{Token: heldToken}}, nil)
	w.sync(1)
	if l.eng.Live() {
		t.Fatal("live with no socket or transcript told")
	}
}

func TestAWaitLaunchHasNoDialer(t *testing.T) {
	w := newLinkWorld(t)
	made := w.engineConfig()
	w.startedSession(state.Unusable{})
	w.start()
	w.send(wire.Msg{Kind: wire.KindLaunch, Launch: &wire.Launch{
		Run: w.run, Kind: state.KindCCY, Launch: state.LaunchClaude, Inbound: state.InboundRefuse, Cwd: "/work",
	}}, nil)
	w.next(wire.KindReady)
	if c := <-made; c.Inbox != nil {
		t.Fatal("a wait launch has a dialer")
	}
	l := w.link()
	w.send(wire.Msg{Kind: wire.KindToken, Token: &wire.Token{Token: heldToken}}, nil)
	w.sync(1)
	if l.eng.Live() {
		t.Fatal("a wait launch is live")
	}
}

func TestTheRenderCountReachesSessionJSON(t *testing.T) {
	w := newLinkWorld(t)
	made := w.engineConfig()
	before := w.startedSession(state.Unusable{HookStdin: 4, Render: 1})
	w.ready()
	c := <-made
	c.OnUnusable(state.Unusable{HookStdin: 4, Render: 2})
	c.OnUnusable(state.Unusable{HookStdin: 4, Render: 3})
	until(t, func() bool { return w.sessionFile().Unusable.Render == 3 })
	got := w.sessionFile()
	if got.Unusable.HookStdin != 4 || *got.SessionID != *before.SessionID || *got.Socket != *before.Socket {
		t.Fatalf("session.json %+v, want the facts kept beside the count", got)
	}
}

func TestTheEndWritesTheRender(t *testing.T) {
	w := newLinkWorld(t)
	made := w.engineConfig()
	w.ready()
	c := <-made
	c.OnUnusable(state.Unusable{Render: 7})
	w.send(wire.Msg{Kind: wire.KindEnd, End: &wire.End{How: wire.EndStop}}, nil)
	if code := w.exited(); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if got := w.sessionFile(); got.Unusable.Render != 7 || got.Started() {
		t.Fatalf("session.json %+v", got)
	}
}

func TestAnUnwrittenRenderIsLogged(t *testing.T) {
	w := newLinkWorld(t)
	made := w.engineConfig()
	w.ready()
	c := <-made
	// A directory in session.json's place: the replace cannot land.
	check(t, w.root.MkdirAll(w.layout.Run(w.run).Session()+"/x"))
	c.OnUnusable(state.Unusable{Render: 1})
	until(t, func() bool { return strings.Contains(w.log.String(), "link: record the render count:") })
}
