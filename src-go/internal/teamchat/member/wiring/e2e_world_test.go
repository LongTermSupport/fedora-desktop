package wiring

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/fakehs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/rooms"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
)

const (
	teamName = "alpha"
	me       = "myrepo.2+desk.podman"
	peer     = "myrepo.3+desk.podman"
	coord    = "myrepo.1+desk.podman"
	alice    = "alice"
	// mySeat is me's seat, the handle's <seat>.
	mySeat = "2"
	// settle bounds a wait for a real process's work: a sync, an end, a notice.
	settle = 30 * time.Second
)

var accounts = []string{alice, coord, me, peer}

// teamOf is a team of the world: its name, its id and its homeserver's name.
type teamOf struct {
	name   string
	id     paths.TeamID
	server string
}

func mustTeam(name string) teamOf {
	id, err := paths.ParseTeamID(name)
	if err != nil {
		panic(err)
	}
	return teamOf{name: name, id: id, server: paths.ServerName(id)}
}

var (
	alpha   = mustTeam(teamName)
	alphaID = alpha.id
)

// user is local's user id on the world's team's homeserver.
func (w *world) user(local string) string { return proto.UserID(local, w.team.server) }

func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// homeserver is the team's server, fakehs or a real Tuwunel: its URL, its admin, and a token
// for any account; fake is the fakehs, nil for a Tuwunel.
type homeserver struct {
	base  string
	admin *matrix.Admin
	mint  func(userID string) (string, error)
	fake  *fakehs.Server
}

// fakeServer is team alpha's fakehs (fakeServerFor).
func fakeServer(t *testing.T) homeserver {
	t.Helper()
	return fakeServerFor(t, alpha)
}

// fakeServerFor is fakehs for tm over HTTP on the wall clock, its accounts made, for the member
// processes.
func fakeServerFor(t *testing.T, tm teamOf) homeserver {
	t.Helper()
	srv := fakehs.New(fakehs.Options{ServerName: tm.server, Quirks: fakehs.TuwunelQuirks()})
	check(t, srv.Start())
	t.Cleanup(func() {
		if err := srv.CheckModelled(); err != nil {
			t.Errorf("fakehs: %v", err)
		}
		if err := srv.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	srv.AddUser(proto.AdminLocalpart, "pw", true)
	for _, local := range accounts {
		srv.AddUser(local, "pw", false)
	}
	token, err := srv.MintToken(proto.AdminID(tm.server))
	check(t, err)
	adm, err := matrix.NewAdmin(matrix.Config{BaseURL: srv.URL(), Token: token, Clock: clock.System()})
	check(t, err)
	t.Cleanup(adm.Close)
	return homeserver{base: srv.URL(), admin: adm, mint: srv.MintToken, fake: srv}
}

type recorder struct{ ids proto.RoomIDs }

func (*recorder) Intent(string) error { return nil }

func (r *recorder) Record(purpose, id string) error {
	switch purpose {
	case proto.PurposeMain:
		r.ids.Main = id
	case proto.PurposeReports:
		r.ids.Reports = id
	case proto.PurposeQuestions:
		r.ids.Questions = id
	}
	return nil
}

// world is team alpha on a homeserver, me's pair in a seat directory of its own state root,
// and a kit whose bin/teamchat is this test binary. Its directories are short: a unix socket's
// path is at most 107 bytes.
type world struct {
	t       *testing.T
	team    teamOf
	hs      homeserver
	main    string
	tokens  map[string]string
	dir     string
	home    string
	rt      string
	state   string
	desired string
	kit     string
	agent   string
	// arm is the failpoints the next processes arm, once each (armOnce).
	arm string
}

func newWorld(t *testing.T, hs homeserver) *world {
	t.Helper()
	dir := t.TempDir()
	w := &world{
		t: t, team: alpha, hs: hs, tokens: map[string]string{}, dir: dir,
		home: dir + "/h", rt: dir + "/r", state: dir + "/s", desired: dir + "/d", kit: dir + "/k/" + testDigest,
	}
	for _, d := range []string{w.home, w.rt, w.state, w.desired, w.kit + "/bin"} {
		check(t, os.MkdirAll(d, 0o700))
	}
	w.agent = dir + "/" + agentName
	w.copySelf(w.kit + "/bin/teamchat")
	check(t, os.Link(w.kit+"/bin/teamchat", w.agent))
	w.populate()
	return w
}

// join is the world's second team, tm on hs: the same directories and kit, its own rooms,
// tokens and seat directory in the one state root.
func (w *world) join(tm teamOf, hs homeserver) *world {
	w.t.Helper()
	other := *w
	other.team, other.hs, other.main, other.tokens = tm, hs, "", map[string]string{}
	other.populate()
	return &other
}

// copySelf copies the test binary to path, executable.
func (w *world) copySelf(path string) {
	w.t.Helper()
	self, err := os.Executable()
	check(w.t, err)
	in, err := os.Open(self)
	check(w.t, err)
	defer func() { check(w.t, in.Close()) }()
	out, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
	check(w.t, err)
	if _, err := io.Copy(out, in); err != nil {
		w.t.Fatal(errors.Join(err, out.Close()))
	}
	check(w.t, out.Close())
}

// populate makes team alpha's rooms as the keeper does, gives each account a token and joins it
// to main, and places me's pair in its seat directory.
func (w *world) populate() {
	t := w.t
	roster := proto.Roster{
		Team: w.team.name, Name: w.team.name, Humans: []string{w.user(alice)},
		Members: map[string]proto.RosterMember{
			w.user(coord): {Role: proto.RoleCoordinator, HumanText: true},
			w.user(me):    {Role: proto.RoleWorker, HumanText: true},
			w.user(peer):  {Role: proto.RoleWorker, HumanText: true},
		},
		Forge: proto.Forge{Kind: proto.ForgeGitHub, Org: "example-org"}, Limits: proto.DefaultLimits(),
	}
	rec := &recorder{}
	if _, err := rooms.Converge(t.Context(), w.hs.admin, rec, rooms.Team{Server: w.team.server, Team: w.team.name, Roster: roster}, proto.RoomIDs{}); err != nil {
		t.Fatalf("Converge: %v", err)
	}
	w.main = rec.ids.Main
	for _, local := range accounts {
		token, err := w.hs.mint(w.user(local))
		check(t, err)
		w.tokens[local] = token
		if _, err := w.client(local).Join(t.Context(), w.main); err != nil {
			t.Fatalf("%s joins main: %v", local, err)
		}
	}
	seatDir := w.state + "/" + w.team.name
	check(t, os.Mkdir(seatDir, 0o700))
	issuer, err := paths.ParseDigest("0123456789abcdef")
	check(t, err)
	b := seat.Bundle{
		V: 1, Team: w.team.id, ServerName: w.team.server, UserID: w.user(me), AdminID: proto.AdminID(w.team.server),
		MainRoom: w.main, BaseURL: w.hs.base, PlainHTTPHosts: []string{"127.0.0.1"},
		DeviceID: paths.SeatDevice, IssuedMS: clock.System().WallMS() - 1000, Issuer: issuer,
	}
	data, err := b.Encode()
	check(t, err)
	w.replace(seatDir+"/token", []byte(w.tokens[me]))
	w.replace(seatDir+"/seat.json", data)
}

func (w *world) replace(path string, data []byte) {
	w.t.Helper()
	tmp := path + ".tmp"
	check(w.t, os.WriteFile(tmp, data, 0o600))
	check(w.t, os.Rename(tmp, path))
}

// desire writes the desired membership generation gen, inbound accept, by rename.
func (w *world) desire(gen int64, seats []state.DesiredSeat, farewell []state.Farewell) {
	w.t.Helper()
	session, err := paths.ParseSessionName("e2e")
	check(w.t, err)
	data, err := state.Desired{
		Session: session, Generation: gen, WrittenMS: clock.System().WallMS(),
		Seats: append([]state.DesiredSeat{}, seats...), Farewell: append([]state.Farewell{}, farewell...),
		Inbound: state.InboundAccept,
	}.Encode()
	check(w.t, err)
	w.replace(w.desired+"/desired.json", data)
}

// client is local's own client.
func (w *world) client(local string) *matrix.Client {
	w.t.Helper()
	c, err := matrix.NewMember(matrix.Config{BaseURL: w.hs.base, PlainHTTPHosts: []string{"127.0.0.1"}, Token: w.tokens[local], Clock: clock.System()})
	check(w.t, err)
	w.t.Cleanup(c.Close)
	return c
}

// say posts alice's message to main, mentioning me.
func (w *world) say(text string) string {
	w.t.Helper()
	data, err := json.Marshal(map[string]any{"msgtype": "m.text", "body": text, "m.mentions": map[string]any{"user_ids": []string{w.user(me)}}})
	check(w.t, err)
	id, err := w.client(alice).Send(w.t.Context(), w.main, proto.MessageType, "", data)
	check(w.t, err)
	return id
}

// peerText posts peer's teamchat.text to me through A's composer.
func (w *world) peerText(text string) string {
	w.t.Helper()
	raw, err := w.hs.admin.GetState(w.t.Context(), w.main, proto.RosterType, "")
	check(w.t, err)
	v, err := proto.Parse(raw)
	check(w.t, err)
	r, err := proto.DecodeRoster(v, w.team.server, w.team.name, w.main)
	check(w.t, err)
	out, err := proto.Composer{Server: w.team.server, Self: w.user(peer), Token: w.tokens[peer], Roster: r}.Text(proto.Text{To: []string{w.user(me)}, Text: text}, "", text)
	check(w.t, err)
	id, err := w.client(peer).Send(w.t.Context(), w.main, proto.MessageType, "", out.Content.Compact())
	check(w.t, err)
	return id
}

// lifecycle is me's lifecycle notices in main, newest first, each "event reason".
func (w *world) lifecycle() []string {
	w.t.Helper()
	page, err := w.client(alice).Messages(w.t.Context(), w.main, matrix.MessagesRequest{Dir: matrix.Backward, Limit: 100})
	check(w.t, err)
	var out []string
	for _, raw := range page.Chunk {
		v, err := proto.Parse(raw)
		check(w.t, err)
		if str(v, "sender") != w.user(me) {
			continue
		}
		content, ok := v.Get("content")
		if !ok {
			continue
		}
		if n, ok := content.Get(proto.KeyNotice); ok {
			out = append(out, strings.TrimSpace(str(n, "event")+" "+str(n, "reason")))
		}
	}
	return out
}

// str is v's string member name, "" when it has none.
func str(v proto.Value, name string) string {
	if f, ok := v.Get(name); ok && f.Kind() == proto.KindString {
		return f.Str()
	}
	return ""
}

// awaitNotice waits for me's latest notice to be want.
func (w *world) awaitNotice(want string) {
	w.t.Helper()
	eventually(w.t, func() bool {
		got := w.lifecycle()
		return len(got) > 0 && got[0] == want
	}, func() string { return "me's notices: " + strings.Join(w.lifecycle(), ", ") })
}

// eventually polls cond every 20 ms until it holds, or fails the test at settle with what().
func eventually(t *testing.T, cond func() bool, what func() string) {
	t.Helper()
	within(t, settle, cond, what)
}

// within is eventually with its own bound.
func within(t *testing.T, limit time.Duration, cond func() bool, what func() string) {
	t.Helper()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	bound := time.NewTimer(limit)
	defer bound.Stop()
	for !cond() {
		select {
		case <-tick.C:
		case <-bound.C:
			t.Fatalf("not within %v: %s", limit, what())
		}
	}
}

// env is a member process's environment: the world's home and runtime directory, and a race
// detector that does not sleep at exit.
func (w *world) env(extra ...string) []string {
	env := []string{"HOME=" + w.home, "XDG_RUNTIME_DIR=" + w.rt, "GORACE=atexit_sleep_ms=0"}
	if w.arm != "" {
		env = append(env, envArm+"="+w.arm, envArmDir+"="+w.dir)
	}
	return append(env, extra...)
}

// session is one `teamchat run` and its CMD.
type session struct {
	w       *world
	keeper  execx.Started
	stdin   *os.File
	out     *lines
	errOut  *lines
	agent   int
	runtime string
	ended   chan execx.ExitStatus
}

// lines is a process's output, kept by line.
type lines struct {
	mu  sync.Mutex
	all []string
}

func (l *lines) read(r io.Reader) {
	s := bufio.NewScanner(r)
	for s.Scan() {
		l.mu.Lock()
		l.all = append(l.all, s.Text())
		l.mu.Unlock()
	}
}

func (l *lines) text() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.all, "\n")
}

func (l *lines) find(prefix string) (string, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	i := slices.IndexFunc(l.all, func(s string) bool { return strings.HasPrefix(s, prefix) })
	if i < 0 {
		return "", false
	}
	return l.all[i], true
}

// start is `teamchat run --plain --kind other <flags> --kit <kit> -- agent`, started.
func (w *world) start(flags ...string) *session {
	w.t.Helper()
	args := append([]string{"run", "--plain", "--kind", "other"}, flags...)
	args = append(args, "--kit", w.kit, "--", w.agent)
	inR, inW, err := os.Pipe()
	check(w.t, err)
	outR, outW, err := os.Pipe()
	check(w.t, err)
	errR, errW, err := os.Pipe()
	check(w.t, err)
	s := &session{w: w, stdin: inW, out: &lines{}, errOut: &lines{}, ended: make(chan execx.ExitStatus, 1)}
	k, err := execx.OS{}.Start(execx.Cmd{
		Name: w.kit + "/bin/teamchat", Args: args, Env: w.env(), Dir: w.dir,
		Stdin: inR, Stdout: outW, Stderr: errW,
	})
	check(w.t, errors.Join(err, inR.Close(), outW.Close(), errW.Close()))
	s.keeper = k
	go s.out.read(outR)
	go s.errOut.read(errR)
	go func() {
		st, werr := k.Wait()
		if werr != nil {
			st = execx.ExitStatus{Code: -1}
		}
		s.ended <- st
	}()
	w.t.Cleanup(s.cleanup)
	return s
}

// launch is start, once its CMD has started.
func (w *world) launch(flags ...string) *session {
	w.t.Helper()
	s := w.start(flags...)
	eventually(w.t, func() bool {
		_, ok := s.out.find(agentName + " ")
		return ok || s.exited()
	}, s.describe)
	line, ok := s.out.find(agentName + " ")
	if !ok {
		w.t.Fatalf("the session ended before CMD started: %s", s.describe())
	}
	fields := strings.Fields(line)
	pid, err := strconv.Atoi(fields[1])
	check(w.t, err)
	s.agent = pid
	if len(fields) == 3 {
		s.runtime = fields[2]
	}
	return s
}

// seatedIn waits until the session's link answers status for team alpha: its seat is held
// and the link has it.
func (s *session) seatedIn() {
	s.w.t.Helper()
	eventually(s.w.t, func() bool { return s.exec("status", "--team", s.w.team.name).ExitCode == 0 }, s.describe)
}

func (s *session) exited() bool { return len(s.ended) > 0 }

// as is the session seen through tm's world: its team's verbs, messages and notices.
func (s *session) as(tm *world) *session {
	view := *s
	view.w = tm
	return &view
}

func (s *session) describe() string {
	return "stdout:\n" + s.out.text() + "\nstderr:\n" + s.errOut.text() + "\nkeeper.log:\n" + s.log("keeper.log") +
		"\nlink.log:\n" + s.log("link.log")
}

// log is a file of the session's runtime directory, or why it cannot be read.
func (s *session) log(name string) string {
	dir := s.runtime
	if dir == "" {
		// CMD never said it: the world's one runtime directory, when it has one.
		runs, err := filepath.Glob(s.w.rt + "/teamchat/run/*")
		if err != nil || len(runs) != 1 {
			return "(no runtime directory)"
		}
		dir = runs[0]
	}
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return err.Error()
	}
	return string(data)
}

// exec runs a client verb in the session, as its CMD would.
func (s *session) exec(args ...string) execx.Result {
	s.w.t.Helper()
	res, err := execx.OS{}.Run(s.w.t.Context(), execx.Cmd{
		Name: s.w.kit + "/bin/teamchat", Args: args, Env: s.w.env(paths.EnvRuntime + "=" + s.runtime),
	})
	exited, isExit := errors.AsType[*execx.ExitError](err)
	switch {
	case err != nil && !isExit:
		s.w.t.Fatal(err)
	case isExit && exited.Signal != 0:
		s.w.t.Fatalf("%v: %v", args, exited)
	}
	return res
}

// records is a client verb's records, each a JSON object; it must exit 0.
func (s *session) records(args ...string) []map[string]any {
	s.w.t.Helper()
	res := s.exec(args...)
	if res.ExitCode != 0 {
		s.w.t.Fatalf("%v: exit %d, stderr %s", args, res.ExitCode, res.Stderr)
	}
	var out []map[string]any
	for line := range strings.SplitSeq(strings.TrimSpace(string(res.Stdout)), "\n") {
		if line == "" {
			continue
		}
		var r map[string]any
		check(s.w.t, json.Unmarshal([]byte(line), &r))
		out = append(out, r)
	}
	return out
}

// recvUntil receives in the session until every id has come; each must come once.
func (s *session) recvUntil(ids ...string) []map[string]any {
	s.w.t.Helper()
	var got []map[string]any
	seen := map[string]int{}
	eventually(s.w.t, func() bool {
		for _, r := range s.records("recv", "--team", s.w.team.name) {
			if id, ok := r["id"].(string); ok {
				seen[id]++
			}
			got = append(got, r)
		}
		return !slices.ContainsFunc(ids, func(id string) bool { return seen[id] == 0 })
	}, s.describe)
	for _, id := range ids {
		if seen[id] != 1 {
			s.w.t.Fatalf("%s came %d times", id, seen[id])
		}
	}
	return got
}

// kill sends sig to pid: the keeper this test started, or the CMD it started.
func kill(t *testing.T, pid int, sig syscall.Signal) {
	t.Helper()
	if pid <= 0 {
		t.Fatalf("no process to signal: pid %d", pid)
	}
	check(t, syscall.Kill(pid, sig))
}

// wait waits for the keeper's exit, within settle.
func (s *session) wait() {
	s.w.t.Helper()
	bound := time.NewTimer(settle)
	defer bound.Stop()
	select {
	case st := <-s.ended:
		s.ended <- st
	case <-bound.C:
		s.w.t.Fatalf("the keeper did not exit: %s", s.describe())
	}
}

// cleanup ends what the session left running, before its directories are removed: CMD and the
// keeper by SIGKILL, then a wait for each link the keeper started, which ends by itself. A
// session whose CMD never started has no pid for it: a kill of pid 0 is the test's own group.
func (s *session) cleanup() {
	if s.agent > 0 {
		if err := syscall.Kill(s.agent, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			s.w.t.Error(err)
		}
	}
	if !s.exited() {
		if err := s.keeper.Signal(syscall.SIGKILL); err != nil {
			s.w.t.Error(err)
		}
		<-s.ended
	}
	if err := s.stdin.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		s.w.t.Error(err)
	}
	for _, pid := range s.links() {
		eventually(s.w.t, func() bool { return gone(pid) }, s.describe)
	}
}

// gone is whether pid has exited: no such process, or a zombie its new parent has not reaped.
func gone(pid int) bool {
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return true
	}
	i := bytes.LastIndexByte(stat, ')')
	return i < 0 || bytes.HasPrefix(stat[i+1:], []byte(" Z"))
}

// links are the pids of the links keeper.log says were started.
func (s *session) links() []int {
	var out []int
	for line := range strings.SplitSeq(s.log("keeper.log"), "\n") {
		f := strings.Fields(line)
		if len(f) == 3 && f[0] == "link" && f[2] == "started" {
			if pid, err := strconv.Atoi(f[1]); err == nil {
				out = append(out, pid)
			}
		}
	}
	return out
}
