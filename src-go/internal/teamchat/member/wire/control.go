package wire

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// MaxMessage bounds one control message (P2 note 8: a launch carries two paths of up to
// PATH_MAX each).
const MaxMessage = 64 << 10

// maxFDs is the most descriptors Recv makes room for; more is a message cut short.
const maxFDs = 4

// The kinds of control message (B 2.4.2, 2.5, 2.10; n3c-D6). The keeper sends launch, add,
// remove, owed, pid, claude, token, desired, end and refused; the link sends ready, started,
// state, removed, left, claude and token.
const (
	KindLaunch  = "launch"
	KindAdd     = "add"
	KindRemove  = "remove"
	KindOwed    = "owed"
	KindPID     = "pid"
	KindClaude  = "claude"
	KindToken   = "token"
	KindDesired = "desired"
	KindEnd     = "end"
	KindReady   = "ready"
	KindStarted = "started"
	KindState   = "state"
	KindRemoved = "removed"
	KindLeft    = "left"
	KindRefused = "refused"
)

// How a session ended, in an end message (KP3).
const (
	EndLaunchFailed = "launch-failed"
	EndStop         = "stop"
	EndRestart      = "restart"
	EndExit         = "exit"
	EndKilled       = "killed"
)

// ErrProtocol is a control message the protocol refuses: on Recv the link (or keeper) closes
// the socket. Its text names the rule broken, never a value of the message.
var ErrProtocol = errors.New("wire: a control message the protocol refuses")

// ErrNotControl is a descriptor that is not an AF_UNIX SOCK_SEQPACKET socket.
var ErrNotControl = errors.New("wire: not an AF_UNIX SOCK_SEQPACKET socket")

func refuse(rule string) error { return fmt.Errorf("%w: %s", ErrProtocol, rule) }

// Msg is one control message, {"<kind>":{body}}: Kind and the one body of that kind are set.
type Msg struct {
	Kind    string               `json:"-"`
	Launch  *Launch              `json:"launch,omitempty"`
	Add     *Add                 `json:"add,omitempty"`
	Remove  *Remove              `json:"remove,omitempty"`
	Owed    *Owed                `json:"owed,omitempty"`
	PID     *state.Process       `json:"pid,omitempty"`
	Claude  *state.Process       `json:"claude,omitempty"`
	Token   *Token               `json:"token,omitempty"`
	Desired *state.ReportDesired `json:"desired,omitempty"`
	End     *End                 `json:"end,omitempty"`
	Ready   *Ready               `json:"ready,omitempty"`
	Started *Started             `json:"started,omitempty"`
	State   *State               `json:"state,omitempty"`
	Removed *Removed             `json:"removed,omitempty"`
	Left    *Left                `json:"left,omitempty"`
	Refused *Refused             `json:"refused,omitempty"`
}

// Launch is the keeper's first message, sent once: the session's facts (B 2.4.1).
type Launch struct {
	Run             paths.RunID `json:"run"`
	Kind            string      `json:"kind"`
	Launch          string      `json:"launch"`
	Inbound         string      `json:"inbound"`
	Generation      int64       `json:"generation"`
	ClaudeConfigDir *string     `json:"claude_config_dir"`
	Cwd             string      `json:"cwd"`
	// Seats is how many add messages follow the launch; the link is ready only once it has
	// handled that many, a failed add included (n3c-D24).
	Seats int `json:"seats"`
}

// Add hands the link a seat, with the seat lock's descriptor; RemovedUserIDs are the team's
// user ids the keeper saw removed, for SY1 (c) (n3c-D13).
type Add struct {
	Team           paths.TeamID `json:"team"`
	Seat           string       `json:"seat"`
	RemovedUserIDs []string     `json:"removed_user_ids"`
}

// Remove takes a seat out: Farewell is why (nil for none), Txn the keeper's (LN8, n3c-D14).
type Remove struct {
	Team     paths.TeamID `json:"team"`
	Farewell *string      `json:"farewell"`
	Txn      string       `json:"txn"`
}

// Owed is a leave notice still owed, given to a restarted link.
type Owed struct {
	Team paths.TeamID `json:"team"`
	Txn  string       `json:"txn"`
}

// Token is the inbox socket token, a secret: no String, GoString or error prints it.
type Token struct {
	Token string `json:"token"`
}

const redacted = "[redacted]"

func (Token) String() string { return "token " + redacted }

// GoString keeps %#v from printing the token.
func (Token) GoString() string { return "wire.Token{" + redacted + "}" }

// End is how the session ended, with CMD's or Claude Code's status (KP3, B 2.4.2).
type End struct {
	How    string `json:"how"`
	Status int    `json:"status"`
}

// Ready is the link's every store loaded and link.sock listening (LN1).
type Ready struct{}

// Started is the link's first accepted SessionStart, sent once (n3c-D6).
type Started struct{}

// State is the link's report of one team, for report.json (B 2.3.3).
type State struct {
	Team         paths.TeamID `json:"team"`
	Handle       string       `json:"handle"`
	LastSyncOKMS *int64       `json:"last_sync_ok_ms"`
	Pending      int64        `json:"pending"`
	Delivery     string       `json:"delivery"`
	Store        string       `json:"store"`
}

// Removed is the answer to remove (LN8). With no user id the link could not remove the team
// (it did not hold it, or never knew its user id), and the keeper deletes the store itself.
type Removed struct {
	Team   paths.TeamID `json:"team"`
	UserID string       `json:"user_id,omitempty"`
}

// Left is an owed notice posted, or its 30 s passed: the keeper closes the seat's fd.
type Left struct {
	Team paths.TeamID `json:"team"`
}

// Refused is an add refused because another run holds the seat (RC1).
type Refused struct {
	Team      paths.TeamID `json:"team"`
	HolderRun paths.RunID  `json:"holder_run"`
}

// String is the message for a log line; a token's is redacted.
func (m Msg) String() string {
	shown := m
	if shown.Token != nil {
		shown.Token = &Token{Token: redacted}
	}
	data, err := json.Marshal(shown)
	if err != nil {
		return m.Kind + " (a body that does not encode)"
	}
	return m.Kind + " " + string(data)
}

// body is one kind's place in a Msg.
type body struct {
	kind  string
	set   bool
	check func() string
}

func none() string { return "" }

func (m Msg) bodies() []body {
	return []body{
		{KindLaunch, m.Launch != nil, func() string { return m.Launch.check() }},
		{KindAdd, m.Add != nil, func() string { return m.Add.check() }},
		{KindRemove, m.Remove != nil, func() string { return m.Remove.check() }},
		{KindOwed, m.Owed != nil, func() string { return teamTxn(m.Owed.Team, m.Owed.Txn) }},
		{KindPID, m.PID != nil, func() string { return checkProcess(m.PID) }},
		{KindClaude, m.Claude != nil, func() string { return checkProcess(m.Claude) }},
		{KindToken, m.Token != nil, func() string { return m.Token.check() }},
		{KindDesired, m.Desired != nil, func() string { return checkDesired(m.Desired) }},
		{KindEnd, m.End != nil, func() string { return m.End.check() }},
		{KindReady, m.Ready != nil, none},
		{KindStarted, m.Started != nil, none},
		{KindState, m.State != nil, func() string { return m.State.check() }},
		{KindRemoved, m.Removed != nil, func() string { return m.Removed.check() }},
		{KindLeft, m.Left != nil, func() string { return checkTeam(m.Left.Team) }},
		{KindRefused, m.Refused != nil, func() string { return m.Refused.check() }},
	}
}

// Kinds is every kind of control message.
func Kinds() []string {
	bodies := (Msg{}).bodies()
	kinds := make([]string, 0, len(bodies))
	for _, b := range bodies {
		kinds = append(kinds, b.kind)
	}
	return kinds
}

// validate refuses a Msg that is not one body of its Kind by the body's rules.
func (m Msg) validate() error {
	var kind, rule string
	n := 0
	for _, b := range m.bodies() {
		if b.set {
			n++
			kind, rule = b.kind, b.check()
		}
	}
	switch {
	case n != 1:
		return refuse("a message is not one kind")
	case kind != m.Kind:
		return refuse("the kind is not its body's")
	case rule != "":
		return refuse(rule)
	}
	return nil
}

var (
	txnRE      = regexp.MustCompile(`^tc[0-9a-f]{32}$`)
	kinds      = []string{state.KindCCY, state.KindLXC, state.KindHost, state.KindOther}
	farewells  = []string{state.FarewellReleased, state.FarewellRemoved, state.FarewellParked}
	desiredOf  = []string{state.CodeJSON, state.CodeSchema, state.CodeVersion, state.CodeSeat, state.CodeMissing}
	ends       = []string{EndLaunchFailed, EndStop, EndRestart, EndExit, EndKilled}
	deliveries = []string{state.DeliverySocket, state.DeliveryWait, state.DeliveryNone}
)

// maxUserID bounds a user id in a message, as the store does.
const maxUserID = 128

// maxToken bounds the inbox socket token.
const maxToken = 1024

func cleanAbs(p string) bool {
	return filepath.IsAbs(p) && filepath.Clean(p) == p && !strings.ContainsRune(p, 0)
}

func checkTeam(t paths.TeamID) string {
	if t.String() == "" {
		return "team is not a team id"
	}
	return ""
}

func checkRun(r paths.RunID) string {
	if r.String() == "" {
		return "run is not a run id"
	}
	return ""
}

func teamTxn(t paths.TeamID, txn string) string {
	if rule := checkTeam(t); rule != "" {
		return rule
	}
	if !txnRE.MatchString(txn) {
		return "txn is not tc and 32 hex"
	}
	return ""
}

// agentOf is whether id is an agent's user id on team's server, within maxUserID; the caller
// has checked the team.
func agentOf(t paths.TeamID, id string) bool {
	_, ok := proto.AgentHandle(id, paths.ServerName(t))
	return ok && len(id) <= maxUserID
}

func (l *Launch) check() string {
	switch {
	case checkRun(l.Run) != "":
		return checkRun(l.Run)
	case !slices.Contains(kinds, l.Kind):
		return "kind is not ccy, lxc, host or other"
	case l.Launch != state.LaunchClaude && l.Launch != state.LaunchPlain:
		return "launch is not claude or plain"
	case l.Inbound != state.InboundAccept && l.Inbound != state.InboundRefuse:
		return "inbound is not accept or refuse"
	case l.Generation < 0:
		return "generation is negative"
	case l.Seats < 0:
		return "seats is negative"
	case !cleanAbs(l.Cwd) || l.ClaudeConfigDir != nil && !cleanAbs(*l.ClaudeConfigDir):
		return "cwd or claude_config_dir is not an absolute clean path"
	}
	return ""
}

func (a *Add) check() string {
	if rule := checkTeam(a.Team); rule != "" {
		return rule
	}
	if !proto.ValidSeat(a.Seat) {
		return "seat is not a seat"
	}
	if a.RemovedUserIDs == nil {
		return "removed_user_ids is not a list"
	}
	for _, id := range a.RemovedUserIDs {
		if !agentOf(a.Team, id) {
			return "a removed user id is not an agent's of the team"
		}
	}
	return ""
}

func (r *Remove) check() string {
	if r.Farewell != nil && !slices.Contains(farewells, *r.Farewell) {
		return "farewell is not released, removed, parked or null"
	}
	return teamTxn(r.Team, r.Txn)
}

func checkProcess(p *state.Process) string {
	if p.PID <= 0 || p.StartTicks < 0 {
		return "a process is not a pid and its start ticks"
	}
	return ""
}

func (t Token) check() string {
	if len(t.Token) < 1 || len(t.Token) > maxToken {
		return "the token is not 1 to 1024 bytes"
	}
	for i := range len(t.Token) {
		if c := t.Token[i]; c < 0x21 || c > 0x7e {
			return "the token is not printable ascii without spaces"
		}
	}
	return ""
}

// checkDesired is report.json's rule for its desired object (state/report.go).
func checkDesired(d *state.ReportDesired) string {
	switch {
	case d.Generation < 0:
		return "desired generation is negative"
	case d.State != state.DesiredApplied && d.State != state.DesiredRefused:
		return "desired state is not applied or refused"
	case (d.Code == nil) != (d.State == state.DesiredApplied):
		return "desired code is not null exactly when applied"
	case d.Code != nil && !slices.Contains(desiredOf, *d.Code):
		return "desired code is not json, schema, version, seat or missing"
	}
	return ""
}

func (e *End) check() string {
	if !slices.Contains(ends, e.How) {
		return "how is not launch-failed, stop, restart, exit or killed"
	}
	if e.Status < 0 || e.Status > 255 {
		return "status is not 0 to 255"
	}
	return ""
}

func (s *State) check() string {
	if _, ok := proto.ParseHandle(s.Handle); !ok {
		return "handle is not a handle"
	}
	switch {
	case s.LastSyncOKMS != nil && *s.LastSyncOKMS <= 0:
		return "last_sync_ok_ms is not a time or null"
	case s.Pending < 0:
		return "pending is negative"
	case !slices.Contains(deliveries, s.Delivery):
		return "delivery is not socket, wait or none"
	case s.Store != state.StoreOK && s.Store != state.StoreRefused:
		return "store is not ok or refused"
	}
	return checkTeam(s.Team)
}

func (r *Removed) check() string {
	if rule := checkTeam(r.Team); rule != "" {
		return rule
	}
	if r.UserID != "" && !agentOf(r.Team, r.UserID) {
		return "user_id is not an agent's of the team"
	}
	return ""
}

func (r *Refused) check() string {
	if rule := checkTeam(r.Team); rule != "" {
		return rule
	}
	return checkRun(r.HolderRun)
}

// Pair is a new control socketpair, AF_UNIX SOCK_SEQPACKET, both ends close-on-exec: the
// keeper's end, then the link's. The keeper clears close-on-exec on the link's end only by
// passing it in execx.Cmd.ExtraFiles (fd 3).
func Pair() (*os.File, *os.File, error) {
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("wire: socketpair: %w", err)
	}
	return os.NewFile(uintptr(fds[0]), "keeper-control"), os.NewFile(uintptr(fds[1]), "link-control"), nil
}

// Conn is one end of the control socket.
type Conn struct {
	c *net.UnixConn
}

// NewConn makes a Conn of f, refusing (ErrNotControl) a descriptor that is not an AF_UNIX
// SOCK_SEQPACKET socket: the link's "fd 3 is the keeper's control socket" (B 2.2.1). It takes
// f: f is closed on every path, the Conn holding its own descriptor.
func NewConn(f *os.File) (*Conn, error) {
	var conn net.Conn
	ok, err := isControl(f)
	if err == nil && ok {
		conn, err = net.FileConn(f)
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil || !ok {
		return nil, closeRefused(conn, err)
	}
	uc, isUnix := conn.(*net.UnixConn)
	if !isUnix {
		return nil, closeRefused(conn, nil)
	}
	return &Conn{c: uc}, nil
}

// closeRefused closes conn, if made, and is the refusal: err, or ErrNotControl when none.
func closeRefused(conn net.Conn, err error) error {
	if conn != nil {
		if closeErr := conn.Close(); err == nil {
			err = closeErr
		}
	}
	if err == nil {
		return ErrNotControl
	}
	return fmt.Errorf("%w: %w", ErrNotControl, err)
}

// isControl is whether f is an AF_UNIX SOCK_SEQPACKET socket; a descriptor that is not a
// socket at all is not one.
func isControl(f *os.File) (bool, error) {
	raw, err := f.SyscallConn()
	if err != nil {
		return false, err
	}
	var ok bool
	err = raw.Control(func(fd uintptr) {
		typ, typErr := unix.GetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_TYPE)
		dom, domErr := unix.GetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_DOMAIN)
		ok = typErr == nil && domErr == nil && typ == unix.SOCK_SEQPACKET && dom == unix.AF_UNIX
	})
	return ok, err
}

// Send writes m, refusing (ErrProtocol) before anything is written a message not of its kind,
// over MaxMessage, or with the wrong descriptor: fd is the seat lock of an add, nil otherwise.
func (c *Conn) Send(m Msg, fd *os.File) error {
	if err := m.validate(); err != nil {
		return err
	}
	if (m.Kind == KindAdd) != (fd != nil) {
		return refuse("an add carries one descriptor and no other kind carries any")
	}
	data, err := json.Marshal(m)
	if err != nil {
		return refuse("a body that does not encode")
	}
	if len(data) > MaxMessage {
		return refuse("a message over 64 KiB")
	}
	if fd == nil {
		return c.write(data, nil)
	}
	raw, err := fd.SyscallConn()
	if err != nil {
		return fmt.Errorf("wire: send: %w", err)
	}
	var writeErr error
	if err := raw.Control(func(f uintptr) { writeErr = c.write(data, unix.UnixRights(int(f))) }); err != nil {
		return fmt.Errorf("wire: send: %w", err)
	}
	return writeErr
}

// Recv reads one message and an add's descriptor. The peer's close is io.EOF (LN1: the
// keeper's death). A message cut short (MSG_TRUNC, MSG_CTRUNC), over MaxMessage, not strict,
// not of one kind by its rules, or with the wrong descriptor count is ErrProtocol, and every
// descriptor it carried is closed. Received descriptors are close-on-exec (MSG_CMSG_CLOEXEC,
// which Go sets on Linux).
func (c *Conn) Recv() (Msg, *os.File, error) {
	buf := make([]byte, MaxMessage+1)
	oob := make([]byte, unix.CmsgSpace(maxFDs*4))
	n, oobn, flags, _, err := c.c.ReadMsgUnix(buf, oob)
	if errors.Is(err, io.EOF) || err == nil && n == 0 && oobn == 0 {
		return Msg{}, nil, io.EOF
	}
	if err != nil {
		return Msg{}, nil, fmt.Errorf("wire: recv: %w", err)
	}
	fds, rightsErr := rights(oob[:oobn])
	m, err := judge(buf[:n], flags, len(fds))
	if err == nil {
		err = rightsErr
	}
	if err != nil {
		return Msg{}, nil, errors.Join(err, closeAll(fds))
	}
	if len(fds) == 0 {
		return m, nil, nil
	}
	return m, os.NewFile(uintptr(fds[0]), "seat-lock"), nil
}

// rights is every descriptor in a message's control data.
func rights(oob []byte) ([]int, error) {
	msgs, err := unix.ParseSocketControlMessage(oob)
	if err != nil {
		return nil, refuse("control data that does not parse")
	}
	var fds []int
	for i := range msgs {
		if msgs[i].Header.Level != unix.SOL_SOCKET || msgs[i].Header.Type != unix.SCM_RIGHTS {
			continue
		}
		got, err := unix.ParseUnixRights(&msgs[i])
		if err != nil {
			return fds, refuse("control data that does not parse")
		}
		fds = append(fds, got...)
	}
	return fds, nil
}

// closeAll closes the descriptors of a refused message.
func closeAll(fds []int) error {
	var errs []error
	for _, fd := range fds {
		if err := unix.Close(fd); err != nil {
			errs = append(errs, fmt.Errorf("wire: closing a refused descriptor: %w", err))
		}
	}
	return errors.Join(errs...)
}

// judge decodes a received message by the protocol's rules.
func judge(data []byte, flags, nfds int) (Msg, error) {
	if flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 {
		return Msg{}, refuse("a message cut short")
	}
	if len(data) > MaxMessage {
		return Msg{}, refuse("a message over 64 KiB")
	}
	var m Msg
	if _, err := decodeStrict(data, &m); err != nil {
		return Msg{}, refuse(err.Error())
	}
	for _, b := range m.bodies() {
		if b.set {
			m.Kind = b.kind
		}
	}
	if err := m.validate(); err != nil {
		return Msg{}, err
	}
	want := 0
	if m.Kind == KindAdd {
		want = 1
	}
	if nfds != want {
		return Msg{}, refuse("an add carries one descriptor and no other kind carries any")
	}
	return m, nil
}

// PeerPID is the peer's pid (SO_PEERCRED): the link checks it is its parent (LN1).
func (c *Conn) PeerPID() (int, error) {
	raw, err := c.c.SyscallConn()
	if err != nil {
		return 0, fmt.Errorf("wire: peer pid: %w", err)
	}
	var cred *unix.Ucred
	var credErr error
	if err := raw.Control(func(fd uintptr) {
		cred, credErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return 0, fmt.Errorf("wire: peer pid: %w", err)
	}
	if credErr != nil {
		return 0, fmt.Errorf("wire: peer pid: %w", credErr)
	}
	return int(cred.Pid), nil
}

// Close closes the socket; a Recv blocked on it returns.
func (c *Conn) Close() error { return c.c.Close() }

func (c *Conn) write(data, oob []byte) error {
	n, oobn, err := c.c.WriteMsgUnix(data, oob, nil)
	if err != nil {
		return fmt.Errorf("wire: send: %w", err)
	}
	if n != len(data) || oobn != len(oob) {
		return errors.New("wire: send: a short write")
	}
	return nil
}
