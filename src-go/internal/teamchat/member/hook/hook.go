// Package hook is `teamchat hook <event>`, the plugin's one hook command (Plan 00209 B 2.8,
// SPEC-n3 HK1's client half). It reads Claude Code's payload from stdin (at most 1 MiB, strict
// UTF-8, one JSON object), sends one hook.<event> request to the link with the payload fields
// the link uses and the inbox socket variables, and prints the link's answer line.
//
// It always exits 0 (OF1; n3e-D15): only an event outside kit.Hooks, which the parser refuses
// before this runs, is 64. With no TEAMCHAT_RUNTIME it answers {} without a dial. When the link
// does not answer (no socket, a refused connect, a close before done, a malformed answer, or
// nothing within the 300 ms deadline) it answers from its own template: the verbs' exit 69
// text for keeper.json and the keeper.lock probe (client.ReadDown) as additional context on
// SessionStart and UserPromptSubmit, {} on Stop and SessionEnd. It writes no file, makes no
// network call, and prints nothing to stderr.
package hook

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"time"
	"unicode/utf8"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/cli"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/client"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/hooktext"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/kit"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// The inbox socket variables Claude Code sets for a session (B 2.7.1); the token is a secret.
const (
	EnvInboxSocket = "CLAUDE_CODE_MESSAGING_SOCKET"
	EnvInboxToken  = "CLAUDE_CODE_MESSAGING_TOKEN"
)

const (
	// maxStdin bounds the payload (B 2.8).
	maxStdin = 1 << 20
	// maxAnswer bounds one line from the link.
	maxAnswer = 1 << 20
)

const msgEvent exits.Template = "teamchat hook takes one of session-start, user-prompt-submit, stop, session-end"

// event is one hook: its link op and Claude Code's name for it.
type event struct {
	op   string
	name string
}

// ops are the link ops by the kit's hook argument.
var ops = map[string]string{
	"session-start":      wire.OpHookSessionStart,
	"user-prompt-submit": wire.OpHookPrompt,
	"stop":               wire.OpHookStop,
	"session-end":        wire.OpHookSessionEnd,
}

func eventOf(arg string) (event, bool) {
	for _, h := range kit.Hooks {
		if h.Arg == arg {
			op, ok := ops[arg]
			return event{op: op, name: h.Event}, ok
		}
	}
	return event{}, false
}

// deps are what the hook waits on and connects through.
type deps struct {
	clock clock.Clock
	dial  func(path string) (*net.UnixConn, error)
}

func dialUnix(path string) (*net.UnixConn, error) {
	return net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
}

// Main is `teamchat hook <event>`; cmd/teamchat puts it in cli.Env.Verbs as "hook".
func Main(e cli.Env, inv cli.Invocation) term.ExitCode {
	return run(e, inv, deps{clock: clock.System(), dial: dialUnix})
}

func run(e cli.Env, inv cli.Invocation, d deps) term.ExitCode {
	var ev event
	ok := len(inv.Args) == 1
	if ok {
		ev, ok = eventOf(inv.Args[0])
	}
	if !ok {
		return exits.Write(e.Stderr, exits.New(exits.CodeUsage, nil, msgEvent))
	}
	r, err := client.Runtime(e.Getenv)
	if err != nil {
		return say(e, hooktext.Empty())
	}
	args := payload(e.Stdin, ev.op)
	args.Socket = variable(e.Getenv(EnvInboxSocket), wire.MaxHookSocket)
	args.Token = variable(e.Getenv(EnvInboxToken), wire.MaxHookToken)
	raw, err := json.Marshal(args)
	if err != nil {
		return say(e, hooktext.Empty())
	}
	deadline := d.clock.Boot() + wire.Deadline(ev.op, 0)
	line, err := wire.Request{V: wire.Version, Op: ev.op, Kit: e.Digest, DeadlineMS: deadline.Milliseconds(), Args: raw}.Encode()
	if err != nil {
		return say(e, hooktext.Empty())
	}
	rep, answered := d.exchange(r, line, deadline)
	switch {
	case answered && rep.refused == "":
		return say(e, hooktext.Answer{Line: rep.line})
	case answered:
		return say(e, hooktext.Context(ev.name, hooktext.Recovery(rep.refused)))
	}
	recovery := client.ReadDown(r).Error(nil, d.clock.Boot()).Message
	return say(e, hooktext.Context(ev.name, hooktext.Recovery(recovery)))
}

// say prints the answer and its newline; a failed write keeps exit 0, as every hook exit is.
func say(e cli.Env, a hooktext.Answer) term.ExitCode {
	if _, err := e.Stdout.Write(append(a.Line, '\n')); err != nil {
		return term.ExitOK
	}
	return term.ExitOK
}

// payload is the args op's stdin gives: its fields when it is one JSON object within the
// bound, else unusable with the first reason that holds. A field that is not a string within
// its bound is absent, not unusable; a stdin that fails to read is json, as nothing whole
// was read.
func payload(stdin io.Reader, op string) wire.HookArgs {
	unusable := func(why string) wire.HookArgs {
		return wire.HookArgs{Payload: wire.PayloadUnusable, Unusable: &why}
	}
	if stdin == nil {
		return unusable(wire.UnusableJSON)
	}
	data, err := io.ReadAll(io.LimitReader(stdin, maxStdin+1))
	switch {
	case err != nil:
		return unusable(wire.UnusableJSON)
	case len(data) > maxStdin:
		return unusable(wire.UnusableSize)
	case !utf8.Valid(data):
		return unusable(wire.UnusableUTF8)
	}
	v, err := proto.Parse(data)
	if err != nil || v.Kind() != proto.KindObject {
		return unusable(wire.UnusableJSON)
	}
	a := wire.HookArgs{
		Payload:        wire.PayloadOK,
		SessionID:      field(v, "session_id", wire.MaxHookSessionID),
		TranscriptPath: field(v, "transcript_path", wire.MaxHookTranscript),
	}
	switch op {
	case wire.OpHookSessionStart:
		a.Source = field(v, "source", wire.MaxHookWord)
	case wire.OpHookStop:
		active, ok := v.Get("stop_hook_active")
		a.StopHookFalse = ok && active.Kind() == proto.KindBool && !active.Bool()
	case wire.OpHookSessionEnd:
		a.Reason = field(v, "reason", wire.MaxHookWord)
	}
	return a
}

// field is v's key when it is a string within the bound.
func field(v proto.Value, key string, most int) *string {
	f, ok := v.Get(key)
	if !ok || f.Kind() != proto.KindString || len(f.Str()) > most {
		return nil
	}
	return new(f.Str())
}

// variable is an environment value when it is set, UTF-8 and within its bound.
func variable(s string, most int) *string {
	if s == "" || len(s) > most || !utf8.ValidString(s) {
		return nil
	}
	return &s
}

// reply is what a link that answered said: its answer line, or the message of its refusal.
type reply struct {
	line    []byte
	refused string
}

// exchange sends the request line and reads the answer and its done before the deadline,
// which closes the socket; false is no answer.
func (d deps) exchange(r paths.Run, line []byte, deadline time.Duration) (reply, bool) {
	sock, err := d.dial(r.LinkSock())
	if err != nil {
		return reply{}, false
	}
	stop := d.clock.AfterFunc(deadline-d.clock.Boot(), func() { closeSock(sock) })
	defer stop()
	defer closeSock(sock)
	if _, err = sock.Write(append(line, '\n')); err != nil {
		return reply{}, false
	}
	return read(bufio.NewReaderSize(sock, maxAnswer))
}

// read is the link's answer: one JSON object line then a done of exit 0, or a refusing done
// alone; anything else is no answer.
func read(br *bufio.Reader) (reply, bool) {
	first, err := readLine(br)
	if err != nil {
		return reply{}, false
	}
	if bytes.HasPrefix(first, []byte(`{"done":`)) {
		refusal, derr := wire.DecodeDone(first)
		if derr != nil || refusal.Error == nil {
			return reply{}, false
		}
		return reply{refused: refusal.Error.Message}, true
	}
	if v, perr := proto.Parse(first); perr != nil || v.Kind() != proto.KindObject {
		return reply{}, false
	}
	last, err := readLine(br)
	if err != nil {
		return reply{}, false
	}
	if done, derr := wire.DecodeDone(last); derr != nil || done.Exit != 0 || done.Ack != "" {
		return reply{}, false
	}
	return reply{line: first}, true
}

// errLong is a line from the link past maxAnswer.
var errLong = errors.New("hook: a line from the link is too long")

// readLine is the next line without its newline, copied out of the reader's buffer.
func readLine(br *bufio.Reader) ([]byte, error) {
	line, err := br.ReadSlice('\n')
	if errors.Is(err, bufio.ErrBufferFull) {
		return nil, errLong
	}
	if err != nil {
		return nil, err
	}
	return bytes.Clone(line[:len(line)-1]), nil
}

// closeSock closes the connection, which the deadline's timer may have closed already; a
// close's own failure leaves nothing to undo.
func closeSock(sock *net.UnixConn) {
	if err := sock.Close(); err != nil {
		return
	}
}
