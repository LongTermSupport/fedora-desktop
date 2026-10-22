// Package wire is the member's two local protocols (Plan 00209 B 2.4.2, 2.4.4; SPEC-n3 LN2,
// n3c-D3). The link socket: one strict JSON request line per connection, answered by the verb's
// record lines and one done line, with the acknowledgement recv and wait end with. The control
// socket between keeper and link: an AF_UNIX SOCK_SEQPACKET socketpair, so a message keeps its
// boundaries and a seat lock's descriptor travels with the add that names it; each message is
// one strict JSON object of at most 8 KiB, {"<kind>":{...}}.
//
// Every decoder is strict, as the member files are: proto's strict JSON (valid UTF-8, no
// repeated key, integers only), decoded into its typed struct with no unknown key, and the
// struct must encode back to the same JSON value, so a key missing, in another case, or null
// where a value is needed is refused; then the fields' own rules run. A refusal quotes nothing
// it was given. A refused request is an *exits.Error, exit 10, for the link to answer with.
package wire

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// MaxRequest bounds a request line (B 2.4.4); larger is exit 10 code size.
const MaxRequest = 64 << 10

// Version is a request's v.
const Version = 1

// The ops of the link socket. n3c serves recv, wait, inbox, status, verify and history; the
// rest are named so the table is whole.
const (
	OpRecv    = "recv"
	OpWait    = "wait"
	OpInbox   = "inbox"
	OpStatus  = "status"
	OpVerify  = "verify"
	OpHistory = "history"

	OpReply     = "reply"
	OpSay       = "say"
	OpPing      = "ping"
	OpQuestions = "questions"

	OpHookSessionStart = "hook.session-start"
	OpHookPrompt       = "hook.prompt"
	OpHookStop         = "hook.stop"
	OpHookSessionEnd   = "hook.session-end"
)

// deadlines is LN2's table, every op's; 0 is wait's, which is its own timeout.
var deadlines = map[string]time.Duration{
	OpHookSessionStart: 300 * time.Millisecond, OpHookPrompt: 300 * time.Millisecond,
	OpHookStop: 300 * time.Millisecond, OpHookSessionEnd: 300 * time.Millisecond,
	OpRecv: 2 * time.Second, OpInbox: 2 * time.Second, OpStatus: 2 * time.Second,
	OpReply: 30 * time.Second, OpSay: 30 * time.Second, OpPing: 30 * time.Second, OpVerify: 30 * time.Second,
	OpHistory: time.Minute, OpQuestions: time.Minute,
	OpWait: 0,
}

// Deadline is op's deadline by LN2's table: hooks 300 ms; recv, inbox and status 2 s; sends
// and verify 30 s (n3c-D4); history and questions 60 s; wait its waitTimeout. An op not in the
// table has none (0): the link refuses it.
func Deadline(op string, waitTimeout time.Duration) time.Duration {
	if op == OpWait {
		return waitTimeout
	}
	return deadlines[op]
}

// Request is one link socket request (B 2.4.4).
type Request struct {
	V  int    `json:"v"`
	Op string `json:"op"`
	// Kit is the client's source digest; the link refuses one that is not its own (78).
	Kit string `json:"kit"`
	// DeadlineMS is the client's deadline, absolute CLOCK_BOOTTIME milliseconds.
	DeadlineMS int64 `json:"deadline_ms"`
	// Args is one object, the op's own (DecodeArgs).
	Args  json.RawMessage `json:"args"`
	Stdin *string         `json:"stdin"`
}

const (
	msgSize   exits.Template = "the request is larger than 64 KiB"
	msgSchema exits.Template = "the request is not a link request: {}"
	msgOp     exits.Template = "the request names no op of the link"
	msgArgs   exits.Template = "the request's arguments are not its op's: {}"
)

func refuseRequest(rule string) error {
	return exits.New(exits.CodeSchema, nil, msgSchema, exits.Words(rule))
}

// DecodeRequest reads one request line strictly; a refusal is exit 10, code size over
// MaxRequest, verb for an op the table does not have, schema otherwise.
func DecodeRequest(line []byte) (Request, error) {
	if len(line) > MaxRequest {
		return Request{}, exits.New(exits.CodeSize, nil, msgSize)
	}
	var r Request
	v, err := decodeStrict(line, &r)
	if err != nil {
		return Request{}, refuseRequest(err.Error())
	}
	switch {
	case r.V != Version:
		return Request{}, refuseRequest("v is not one")
	case !knownOp(r.Op):
		return Request{}, exits.New(exits.CodeVerb, nil, msgOp)
	case !validDigest(r.Kit):
		return Request{}, refuseRequest("kit is not a source digest")
	case r.DeadlineMS <= 0:
		return Request{}, refuseRequest("deadline is not a time")
	}
	if args, ok := v.Get("args"); !ok || args.Kind() != proto.KindObject {
		return Request{}, refuseRequest("args is not an object")
	}
	return r, nil
}

// Encode is the request's line, without its newline; refused unless DecodeRequest reads it.
func (r Request) Encode() ([]byte, error) {
	line, err := json.Marshal(r)
	if err != nil {
		return nil, refuseRequest("args is not json")
	}
	if _, err := DecodeRequest(line); err != nil {
		return nil, err
	}
	return line, nil
}

func knownOp(op string) bool {
	_, ok := deadlines[op]
	return ok
}

func validDigest(s string) bool {
	_, err := paths.ParseDigest(s)
	return err == nil
}

// DecodeArgs decodes a request's args into out, one of the op's Args types, strictly; a
// refusal is exit 10 code schema. The values' own rules are the verb's.
func DecodeArgs(raw json.RawMessage, out any) error {
	if _, err := decodeStrict(raw, out); err != nil {
		return exits.New(exits.CodeSchema, nil, msgArgs, exits.Words(err.Error()))
	}
	return nil
}

// WaitArgs are wait's.
type WaitArgs struct {
	TimeoutS int64 `json:"timeout_s"`
}

// VerifyArgs are verify's: the event id.
type VerifyArgs struct {
	ID string `json:"id"`
}

// HistoryArgs are history's.
type HistoryArgs struct {
	Team  *paths.TeamID `json:"team"`
	Room  string        `json:"room"`
	Limit int           `json:"limit"`
}

// TeamsArgs are recv's, inbox's and status's: the --team, null for every active team.
type TeamsArgs struct {
	Team *paths.TeamID `json:"team"`
}

// ErrMalformed is a done or ack line that is not one: the link and its client disagree, which
// the client reports as internal.
var ErrMalformed = errors.New("wire: not a done or ack line")

var ackRE = regexp.MustCompile(`^[0-9a-f]{8}$`)

// Done is a request's final line: its exit, the error object of a failure, and recv's and
// wait's acknowledgement id.
type Done struct {
	Exit  int
	Error *exits.Error
	Ack   string
}

// Encode is the done line, without its newline. A failure's exit is its error object's.
func (d Done) Encode() []byte {
	exit := d.Exit
	if d.Error != nil {
		exit = int(d.Error.Exit)
	}
	members := []proto.Member{proto.M("exit", proto.Int(int64(exit)))}
	if d.Error != nil {
		members = append(members, proto.M("error", d.Error.Object()))
	}
	if d.Ack != "" {
		members = append(members, proto.M("ack", proto.Str(d.Ack)))
	}
	return proto.Obj(proto.M("done", proto.Obj(members...))).Compact()
}

// DecodeDone reads a done line strictly: an exit, an error object exactly when the exit is
// not 0 and with that exit, an ack only on exit 0.
func DecodeDone(line []byte) (Done, error) {
	body, ok := only(line, "done")
	if !ok || body.Kind() != proto.KindObject {
		return Done{}, ErrMalformed
	}
	var d Done
	for _, m := range body.Members() {
		if !readDoneMember(&d, m) {
			return Done{}, ErrMalformed
		}
	}
	_, hasExit := body.Get("exit")
	if !hasExit || (d.Exit == 0) != (d.Error == nil) || d.Error != nil && int(d.Error.Exit) != d.Exit ||
		d.Ack != "" && d.Exit != 0 {
		return Done{}, ErrMalformed
	}
	return d, nil
}

// readDoneMember reads one member of a done body into d, false for one it may not have.
func readDoneMember(d *Done, m proto.Member) bool {
	switch {
	case m.Key == "exit" && m.Val.Kind() == proto.KindInt:
		d.Exit = int(m.Val.Int())
		return true
	case m.Key == "error":
		e, err := exits.Decode(m.Val.Compact())
		d.Error = &e
		return err == nil
	case m.Key == "ack" && m.Val.Kind() == proto.KindString:
		d.Ack = m.Val.Str()
		return ackRE.MatchString(d.Ack)
	}
	return false
}

// Ack is the client's acknowledgement of recv's or wait's records, sent after it flushed them.
type Ack struct {
	Ack string
}

// NewAck is a fresh acknowledgement id, 8 hex from crypto/rand.
func NewAck() (Ack, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return Ack{}, err
	}
	return Ack{Ack: hex.EncodeToString(b[:])}, nil
}

// Encode is the ack line, without its newline.
func (a Ack) Encode() []byte { return proto.Obj(proto.M("ack", proto.Str(a.Ack))).Compact() }

// DecodeAck reads an ack line strictly.
func DecodeAck(line []byte) (Ack, error) {
	v, ok := only(line, "ack")
	if !ok || v.Kind() != proto.KindString || !ackRE.MatchString(v.Str()) {
		return Ack{}, ErrMalformed
	}
	return Ack{Ack: v.Str()}, nil
}

// only is the value of an object line whose one member is key.
func only(line []byte, key string) (proto.Value, bool) {
	v, err := proto.Parse(line)
	if err != nil || v.Kind() != proto.KindObject || len(v.Members()) != 1 || v.Members()[0].Key != key {
		return proto.Value{}, false
	}
	return v.Members()[0].Val, true
}

// strictError is a document the strict decoder refuses; its text is a rule, never the input.
type strictError string

func (e strictError) Error() string { return string(e) }

// decodeStrict decodes data into out by the package's rules and returns the parsed value.
func decodeStrict(data []byte, out any) (proto.Value, error) {
	v, err := proto.Parse(data)
	if err != nil {
		return proto.Value{}, strictError("not strict json")
	}
	if v.Kind() != proto.KindObject {
		return proto.Value{}, strictError("not an object")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if dec.Decode(out) != nil {
		return proto.Value{}, strictError("a key it does not have or a value not of its type")
	}
	again, err := json.Marshal(out)
	if err != nil {
		return proto.Value{}, strictError("a value not of its grammar")
	}
	back, err := proto.Parse(again)
	if err != nil || !back.Equal(v) {
		return proto.Value{}, strictError("a key missing or in another case or null where a value is needed")
	}
	return v, nil
}
