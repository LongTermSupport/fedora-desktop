package wire

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

const (
	secretToken = "tok_SECRET_0123456789"
	txn         = "tc0123456789abcdef0123456789abcdef"
)

func runID(t *testing.T) paths.RunID {
	t.Helper()
	r, err := paths.ParseRunID("0123456789abcdef")
	check(t, err)
	return r
}

// agentID is seat 2's user id on tm's server.
func agentID(tm paths.TeamID) string {
	return "@repo.2+site.podman:" + paths.ServerName(tm)
}

// sample is one valid message of every kind.
func sample(t *testing.T) []Msg {
	t.Helper()
	ops := teamID(t, "ops")
	code := state.CodeSchema
	return []Msg{
		{Kind: KindLaunch, Launch: &Launch{
			Run: runID(t), Kind: state.KindCCY, Launch: state.LaunchClaude, Inbound: state.InboundAccept,
			Generation: 3, ClaudeConfigDir: new("/home/u/.claude"), Cwd: "/work/repo",
		}},
		{Kind: KindLaunch, Launch: &Launch{
			Run: runID(t), Kind: state.KindHost, Launch: state.LaunchPlain, Inbound: state.InboundRefuse, Cwd: "/",
		}},
		{Kind: KindAdd, Add: &Add{Team: ops, Seat: "2", RemovedUserIDs: []string{agentID(ops)}}},
		{Kind: KindAdd, Add: &Add{Team: ops, Seat: "2", RemovedUserIDs: []string{}}},
		{Kind: KindRemove, Remove: &Remove{Team: ops, Farewell: new(state.FarewellParked), Txn: txn}},
		{Kind: KindRemove, Remove: &Remove{Team: ops, Txn: txn}},
		{Kind: KindOwed, Owed: &Owed{Team: ops, Txn: txn}},
		{Kind: KindPID, PID: &state.Process{PID: 42, StartTicks: 9000}},
		{Kind: KindClaude, Claude: &state.Process{PID: 43, StartTicks: 9001}},
		{Kind: KindToken, Token: &Token{Token: secretToken}},
		{Kind: KindDesired, Desired: &state.ReportDesired{Generation: 4, State: state.DesiredApplied}},
		{Kind: KindDesired, Desired: &state.ReportDesired{Generation: 4, State: state.DesiredRefused, Code: &code}},
		{Kind: KindEnd, End: &End{How: EndRestart, Status: 0}},
		{Kind: KindEnd, End: &End{How: EndKilled, Status: 255}},
		{Kind: KindReady, Ready: &Ready{}},
		{Kind: KindStarted, Started: &Started{}},
		{Kind: KindState, State: &State{
			Team: ops, Handle: "repo.2+site.podman", LastSyncOKMS: new(int64(1_760_000_000_000)),
			Pending: 2, Delivery: state.DeliverySocket, Store: state.StoreOK,
		}},
		{Kind: KindState, State: &State{
			Team: ops, Handle: "repo.2+site.podman", Delivery: state.DeliveryNone, Store: state.StoreRefused,
		}},
		{Kind: KindRemoved, Removed: &Removed{Team: ops, UserID: agentID(ops)}},
		{Kind: KindLeft, Left: &Left{Team: ops}},
		{Kind: KindRefused, Refused: &Refused{Team: ops, HolderRun: runID(t)}},
	}
}

// conns is a connected keeper and link.
type conns struct{ keeper, link *Conn }

func pair(t *testing.T) conns {
	t.Helper()
	k, l, err := Pair()
	check(t, err)
	keeper, err := NewConn(k)
	check(t, err)
	link, err := NewConn(l)
	check(t, err)
	t.Cleanup(func() {
		check(t, keeper.Close())
		check(t, link.Close())
	})
	return conns{keeper: keeper, link: link}
}

// received is one Recv's result.
type received struct {
	msg Msg
	fd  *os.File
	err error
}

func recv(c *Conn) received {
	m, fd, err := c.Recv()
	return received{msg: m, fd: fd, err: err}
}

// refusal is NewConn's error, the Conn closed when there is one.
func refusal(t *testing.T, f *os.File) error {
	t.Helper()
	c, err := NewConn(f)
	if err == nil {
		check(t, c.Close())
	}
	return err
}

func lockFile(t *testing.T) *os.File {
	t.Helper()
	f, err := os.Create(filepath.Join(t.TempDir(), "seat.lock"))
	check(t, err)
	t.Cleanup(func() { check(t, f.Close()) })
	return f
}

func sameFile(t *testing.T, a, b *os.File) bool {
	t.Helper()
	sa, err := a.Stat()
	check(t, err)
	sb, err := b.Stat()
	check(t, err)
	return os.SameFile(sa, sb)
}

// n3c-D3: every kind crosses the socket and back unchanged; an add's descriptor with it.
func TestEveryKindRoundTrips(t *testing.T) {
	p := pair(t)
	seatLock := lockFile(t)
	seen := map[string]bool{}
	for _, m := range sample(t) {
		var fd *os.File
		if m.Kind == KindAdd {
			fd = seatLock
		}
		check(t, p.keeper.Send(m, fd))
		got := recv(p.link)
		check(t, got.err)
		if !reflect.DeepEqual(got.msg, m) {
			t.Errorf("%s: sent %s, received %s", m.Kind, m, got.msg)
		}
		if (got.fd != nil) != (m.Kind == KindAdd) {
			t.Fatalf("%s: descriptor %v", m.Kind, got.fd)
		}
		if got.fd != nil {
			if !sameFile(t, got.fd, seatLock) {
				t.Errorf("the add's descriptor is another file")
			}
			check(t, got.fd.Close())
		}
		seen[m.Kind] = true
	}
	for _, k := range Kinds() {
		if !seen[k] {
			t.Errorf("kind %s has no sample", k)
		}
	}
}

// n3c-D3: an add without its descriptor, and any other kind with one, is refused before
// anything is written.
func TestSendRefusesTheWrongDescriptorCount(t *testing.T) {
	p := pair(t)
	ops := teamID(t, "ops")
	add := Msg{Kind: KindAdd, Add: &Add{Team: ops, Seat: "2", RemovedUserIDs: []string{}}}
	if err := p.keeper.Send(add, nil); !errors.Is(err, ErrProtocol) {
		t.Errorf("an add without a descriptor: %v", err)
	}
	token := Msg{Kind: KindToken, Token: &Token{Token: secretToken}}
	err := p.keeper.Send(token, lockFile(t))
	if !errors.Is(err, ErrProtocol) {
		t.Errorf("a token with a descriptor: %v", err)
	}
	if err != nil && strings.Contains(err.Error(), secretToken) {
		t.Errorf("the refusal holds the token: %v", err)
	}
}

// A Msg is one kind, its body set and named, and the body by its own rules.
func TestSendRefusesAMessageNotOfItsKind(t *testing.T) {
	p := pair(t)
	ops := teamID(t, "ops")
	for name, m := range map[string]Msg{
		"no body":          {Kind: KindLeft},
		"two bodies":       {Kind: KindLeft, Left: &Left{Team: ops}, Ready: &Ready{}},
		"another kind":     {Kind: KindReady, Left: &Left{Team: ops}},
		"an unknown kind":  {Kind: "nope", Left: &Left{Team: ops}},
		"a zero team":      {Kind: KindLeft, Left: &Left{}},
		"a body it breaks": {Kind: KindEnd, End: &End{How: "vanished"}},
	} {
		if err := p.keeper.Send(m, nil); !errors.Is(err, ErrProtocol) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func launchWith(t *testing.T, change func(l *Launch)) Msg {
	t.Helper()
	l := Launch{Run: runID(t), Kind: state.KindLXC, Launch: state.LaunchPlain, Inbound: state.InboundAccept, Cwd: "/"}
	change(&l)
	return Msg{Kind: KindLaunch, Launch: &l}
}

func stateWith(t *testing.T, change func(s *State)) Msg {
	t.Helper()
	s := State{Team: teamID(t, "ops"), Handle: "repo.2+site.podman", Delivery: state.DeliveryWait, Store: state.StoreOK}
	change(&s)
	return Msg{Kind: KindState, State: &s}
}

// Each body's own rules, as Send and Recv both apply them.
func TestBodyRules(t *testing.T) {
	ops, dev := teamID(t, "ops"), teamID(t, "dev")
	addOf := func(ids ...string) Msg {
		return Msg{Kind: KindAdd, Add: &Add{Team: ops, Seat: "2", RemovedUserIDs: ids}}
	}
	desired := func(gen int64, st string, code *string) Msg {
		return Msg{Kind: KindDesired, Desired: &state.ReportDesired{Generation: gen, State: st, Code: code}}
	}
	bad := map[string]Msg{
		"launch kind":          launchWith(t, func(l *Launch) { l.Kind = "vm" }),
		"launch launch":        launchWith(t, func(l *Launch) { l.Launch = "both" }),
		"launch inbound":       launchWith(t, func(l *Launch) { l.Inbound = "maybe" }),
		"launch generation":    launchWith(t, func(l *Launch) { l.Generation = -1 }),
		"launch relative cwd":  launchWith(t, func(l *Launch) { l.Cwd = "work" }),
		"launch unclean cwd":   launchWith(t, func(l *Launch) { l.Cwd = "/work/../etc" }),
		"launch config dir":    launchWith(t, func(l *Launch) { l.ClaudeConfigDir = new("rel") }),
		"add seat":             {Kind: KindAdd, Add: &Add{Team: ops, RemovedUserIDs: []string{}}},
		"add null ids":         {Kind: KindAdd, Add: &Add{Team: ops, Seat: "2"}},
		"add another server":   addOf(agentID(dev)),
		"add a human":          addOf("@alice:" + paths.ServerName(ops)),
		"add a long id":        addOf("@" + strings.Repeat("r", 120) + ".2+site.podman:" + paths.ServerName(ops)),
		"remove farewell":      {Kind: KindRemove, Remove: &Remove{Team: ops, Farewell: new("bye"), Txn: txn}},
		"remove txn":           {Kind: KindRemove, Remove: &Remove{Team: ops, Txn: "tc-disc-0123"}},
		"owed txn":             {Kind: KindOwed, Owed: &Owed{Team: ops, Txn: strings.ToUpper(txn)}},
		"pid zero":             {Kind: KindPID, PID: &state.Process{StartTicks: 1}},
		"claude ticks":         {Kind: KindClaude, Claude: &state.Process{PID: 1, StartTicks: -1}},
		"token empty":          {Kind: KindToken, Token: &Token{}},
		"token space":          {Kind: KindToken, Token: &Token{Token: "a b"}},
		"token long":           {Kind: KindToken, Token: &Token{Token: strings.Repeat("a", 1025)}},
		"desired code applied": desired(0, state.DesiredApplied, new(state.CodeJSON)),
		"desired no code":      desired(0, state.DesiredRefused, nil),
		"desired code":         desired(0, state.DesiredRefused, new("other")),
		"desired state":        desired(0, "pending", nil),
		"desired generation":   desired(-1, state.DesiredApplied, nil),
		"end how":              {Kind: KindEnd, End: &End{How: "keeper-died"}},
		"end status":           {Kind: KindEnd, End: &End{How: EndExit, Status: 256}},
		"end negative":         {Kind: KindEnd, End: &End{How: EndExit, Status: -1}},
		"state handle":         stateWith(t, func(s *State) { s.Handle = "repo" }),
		"state sync":           stateWith(t, func(s *State) { s.LastSyncOKMS = new(int64(0)) }),
		"state pending":        stateWith(t, func(s *State) { s.Pending = -1 }),
		"state delivery":       stateWith(t, func(s *State) { s.Delivery = "pigeon" }),
		"state store":          stateWith(t, func(s *State) { s.Store = "gone" }),
		"removed user":         {Kind: KindRemoved, Removed: &Removed{Team: ops, UserID: agentID(dev)}},
		"launch run":           launchWith(t, func(l *Launch) { l.Run = paths.RunID{} }),
		"add team":             {Kind: KindAdd, Add: &Add{Seat: "2", RemovedUserIDs: []string{}}},
		"remove team":          {Kind: KindRemove, Remove: &Remove{Txn: txn}},
		"owed team":            {Kind: KindOwed, Owed: &Owed{Txn: txn}},
		"state team":           stateWith(t, func(s *State) { s.Team = paths.TeamID{} }),
		"removed team":         {Kind: KindRemoved, Removed: &Removed{UserID: agentID(ops)}},
		"refused team":         {Kind: KindRefused, Refused: &Refused{HolderRun: runID(t)}},
		"refused run":          {Kind: KindRefused, Refused: &Refused{Team: ops}},
	}
	for name, m := range bad {
		if err := m.validate(); !errors.Is(err, ErrProtocol) {
			t.Errorf("%s: %v", name, err)
		}
	}
	for _, m := range sample(t) {
		if err := m.validate(); err != nil {
			t.Errorf("%s: %v", m, err)
		}
	}
}

// B 2.4.2: the token is a secret; no formatting of a message or its body prints it.
func TestATokenIsNeverFormatted(t *testing.T) {
	tok := Token{Token: secretToken}
	m := Msg{Kind: KindToken, Token: &tok}
	for _, s := range []string{
		m.String(), fmt.Sprint(m), fmt.Sprintf("%v %+v %s", m, m, m),
		tok.String(), tok.GoString(), fmt.Sprintf("%v %+v %#v %s", tok, tok, tok, tok),
		fmt.Sprintf("%v %+v %#v", &tok, &tok, &tok),
	} {
		if strings.Contains(s, secretToken) {
			t.Errorf("the token is printed: %s", s)
		}
	}
	left := Msg{Kind: KindLeft, Left: &Left{Team: teamID(t, "ops")}}
	if s := left.String(); !strings.Contains(s, "left") || !strings.Contains(s, "ops") {
		t.Errorf("a left message prints %q", s)
	}
	for _, m := range []Msg{{Kind: KindLeft}, {Kind: KindLeft, Left: &Left{}}} {
		if s := m.String(); !strings.HasPrefix(s, "left ") {
			t.Errorf("a message with no body or a zero team prints %q", s)
		}
	}
}

// B 2.2.1: fd 3 must be the keeper's control socket; a stream socket or a file is refused,
// and the descriptor is closed either way.
func TestNewConnRefusesWhatIsNotASeqpacketSocket(t *testing.T) {
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	check(t, err)
	a, b := os.NewFile(uintptr(fds[0]), "a"), os.NewFile(uintptr(fds[1]), "b")
	t.Cleanup(func() { check(t, b.Close()) })
	if got := refusal(t, a); !errors.Is(got, ErrNotControl) {
		t.Errorf("a stream socket: %v", got)
	}
	if got := a.Close(); !errors.Is(got, os.ErrClosed) {
		t.Errorf("the refused socket was left open: %v", got)
	}
	f, err := os.Create(filepath.Join(t.TempDir(), "plain"))
	check(t, err)
	if got := refusal(t, f); !errors.Is(got, ErrNotControl) {
		t.Errorf("a regular file: %v", got)
	}
	if got := f.Close(); !errors.Is(got, os.ErrClosed) {
		t.Errorf("the refused file was left open: %v", got)
	}
}

// LN1: the link checks its peer is its parent; in one process the peer is this process.
func TestPeerPIDIsThePeers(t *testing.T) {
	p := pair(t)
	for _, c := range []*Conn{p.keeper, p.link} {
		pid, err := c.PeerPID()
		check(t, err)
		if pid != os.Getpid() {
			t.Errorf("peer pid %d, want %d", pid, os.Getpid())
		}
	}
}

// LN1: the keeper's death is the link's EOF.
func TestThePeersCloseIsEOF(t *testing.T) {
	k, l, err := Pair()
	check(t, err)
	keeper, err := NewConn(k)
	check(t, err)
	link, err := NewConn(l)
	check(t, err)
	t.Cleanup(func() { check(t, link.Close()) })
	check(t, keeper.Close())
	if got := recv(link); !errors.Is(got.err, io.EOF) {
		t.Errorf("after the peer's close: %v", got.err)
	}
}

// rawEnds is a keeper end to write raw messages into, and the link's Conn reading them.
type rawEnds struct {
	keeper int
	link   *Conn
}

func rawPair(t *testing.T) rawEnds {
	t.Helper()
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	check(t, err)
	t.Cleanup(func() { check(t, unix.Close(fds[0])) })
	link, err := NewConn(os.NewFile(uintptr(fds[1]), "link"))
	check(t, err)
	t.Cleanup(func() { check(t, link.Close()) })
	return rawEnds{keeper: fds[0], link: link}
}

// openFDs is how many descriptors this process has open.
func openFDs(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	check(t, err)
	return len(entries)
}

// n3c-D3 and P2 note 8: a message Recv refuses is ErrProtocol, and every descriptor it carried
// is closed.
func TestRecvRefusesAndClosesTheDescriptors(t *testing.T) {
	addJSON := []byte(`{"add":{"team":"ops","seat":"2","removed_user_ids":[]}}`)
	leftJSON := []byte(`{"left":{"team":"ops"}}`)
	lockFD := int(lockFile(t).Fd())
	for name, c := range map[string]struct {
		data []byte
		fds  []int
	}{
		"not json":              {[]byte(`{`), nil},
		"no kind":               {[]byte(`{}`), nil},
		"not an object":         {[]byte(`[]`), nil},
		"two kinds":             {[]byte(`{"left":{"team":"ops"},"ready":{}}`), nil},
		"an unknown kind":       {[]byte(`{"gone":{}}`), nil},
		"a null body":           {[]byte(`{"ready":null}`), nil},
		"an unknown key":        {[]byte(`{"left":{"team":"ops","x":1}}`), nil},
		"a key in another case": {[]byte(`{"left":{"Team":"ops"}}`), nil},
		"a broken body":         {[]byte(`{"end":{"how":"stop","status":300}}`), nil},
		"an add with no fd":     {addJSON, nil},
		"an add with two fds":   {addJSON, []int{lockFD, lockFD}},
		"a left with a fd":      {leftJSON, []int{lockFD}},
		"a left with fds":       {leftJSON, []int{lockFD, lockFD, lockFD}},
		"over the cap":          {[]byte(`{"left":{"team":"ops"}}` + strings.Repeat(" ", MaxMessage)), nil},
		"truncated":             {[]byte(strings.Repeat(" ", MaxMessage+100)), nil},
		"too many fds":          {addJSON, []int{lockFD, lockFD, lockFD, lockFD, lockFD, lockFD, lockFD, lockFD}},
	} {
		t.Run(name, func(t *testing.T) {
			raw := rawPair(t)
			before := openFDs(t)
			var oob []byte
			if c.fds != nil {
				oob = unix.UnixRights(c.fds...)
			}
			check(t, unix.Sendmsg(raw.keeper, c.data, oob, nil, 0))
			got := recv(raw.link)
			if !errors.Is(got.err, ErrProtocol) || got.fd != nil {
				t.Errorf("received %v, %v", got.fd, got.err)
			}
			if after := openFDs(t); after != before {
				t.Errorf("%d descriptors open after the refusal, %d before", after, before)
			}
		})
	}
}

// A refusal of Recv leaves the socket usable: the next message is read.
func TestRecvGoesOnAfterARefusal(t *testing.T) {
	raw := rawPair(t)
	check(t, unix.Sendmsg(raw.keeper, []byte(`{"nope":{}}`), nil, nil, 0))
	check(t, unix.Sendmsg(raw.keeper, []byte(`{"ready":{}}`), nil, nil, 0))
	if got := recv(raw.link); !errors.Is(got.err, ErrProtocol) {
		t.Fatalf("first: %v", got.err)
	}
	got := recv(raw.link)
	check(t, got.err)
	if got.msg.Kind != KindReady {
		t.Errorf("second: %s", got.msg)
	}
}

// Send never writes a message over the cap.
func TestSendRefusesOverTheCap(t *testing.T) {
	p := pair(t)
	m := launchWith(t, func(l *Launch) { l.Cwd = "/" + strings.Repeat("d", MaxMessage) })
	if err := p.keeper.Send(m, nil); !errors.Is(err, ErrProtocol) {
		t.Errorf("an oversized launch: %v", err)
	}
}

// After Close, Send, Recv and PeerPID fail rather than reach another descriptor.
func TestAClosedConnFails(t *testing.T) {
	k, l, err := Pair()
	check(t, err)
	keeper, err := NewConn(k)
	check(t, err)
	check(t, l.Close())
	check(t, keeper.Close())
	if err := keeper.Send(Msg{Kind: KindReady, Ready: &Ready{}}, nil); err == nil {
		t.Error("send on a closed conn")
	}
	if got := recv(keeper); got.err == nil || errors.Is(got.err, io.EOF) {
		t.Errorf("recv on a closed conn: %v", got.err)
	}
	if pid, err := keeper.PeerPID(); err == nil {
		t.Errorf("peer pid %d of a closed conn", pid)
	}
	if err := keeper.Send(Msg{Kind: KindAdd, Add: &Add{Team: teamID(t, "ops"), Seat: "2", RemovedUserIDs: []string{}}}, lockFile(t)); err == nil {
		t.Error("send of an add on a closed conn")
	}
}
