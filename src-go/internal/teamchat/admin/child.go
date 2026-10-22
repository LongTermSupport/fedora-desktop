package admin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/health"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// The pipe's limits (CC3, CC6).
const (
	maxRequest = 64 << 10
	maxAnswer  = 1 << 20
	maxChanged = 4096
	maxLine    = 4096
	maxOp      = 64
	maxNotes   = 64
	maxSecret  = 4096
	wireMajor  = 1
)

// childPath is the child's whole environment's one variable (CC1).
const childPath = "/usr/bin:/bin"

var opRE = regexp.MustCompile(`^[a-z][a-z-]*(\.[a-z][a-z-]*)*$`)

// ChildSpec is how root starts the `teamchat-ctl` child (CC1): the binary, its argument, the
// account, no supplementary groups, `/`, and PATH alone.
type ChildSpec struct {
	Path     string
	Args     []string
	UID, GID uint32
	Groups   []uint32
	Dir      string
	Env      []string
}

// NewChildSpec is the start of the child from self, the running binary's absolute path, as
// the account u, which may not be root's.
func NewChildSpec(self string, u User) (ChildSpec, error) {
	if !filepath.IsAbs(self) || filepath.Clean(self) != self {
		return ChildSpec{}, fmt.Errorf("the binary path %q is not absolute and clean", self)
	}
	uid, uerr := accountID(u.UID)
	gid, gerr := accountID(u.GID)
	if err := errors.Join(uerr, gerr); err != nil {
		return ChildSpec{}, fmt.Errorf("the %s account: %w", paths.CtlUser, err)
	}
	return ChildSpec{
		Path: self, Args: []string{"ctl-child"}, UID: uid, GID: gid, Groups: []uint32{}, Dir: "/",
		Env: []string{"PATH=" + childPath},
	}, nil
}

func accountID(id int) (uint32, error) {
	if id <= 0 || id >= math.MaxUint32 {
		return 0, fmt.Errorf("id %d is root's or out of range", id)
	}
	return uint32(id), nil
}

// checkStarted is the child's check that root started it by the spec (CC2).
func checkStarted(p Process) error {
	if env := p.Environ(); !slices.Equal(env, []string{"PATH=" + childPath}) {
		return fmt.Errorf("its environment holds %d variables, not PATH=%s alone", len(env), childPath)
	}
	wd, err := p.Getwd()
	if err != nil {
		return fmt.Errorf("its working directory: %w", err)
	}
	if wd != "/" {
		return errors.New("its working directory is not /")
	}
	groups, err := p.Getgroups()
	if err != nil {
		return fmt.Errorf("its groups: %w", err)
	}
	if len(groups) > 0 {
		return fmt.Errorf("it has %d supplementary groups", len(groups))
	}
	return nil
}

// ctlChild is the child's side of the pipe: one request in, one answer out (CC2-CC5), served
// through the Opener of e.Sys; without one it serves no operation.
func ctlChild(ctx context.Context, e Env, args []string) term.ExitCode {
	if len(args) > 0 {
		return fail(e.Stderr, term.ExitUsage, "ctl-child takes no arguments")
	}
	if err := checkStarted(e.Proc); err != nil {
		return fail(e.Stderr, ExitConfig, "ctl-child was not started by root's child spec: "+err.Error())
	}
	raw, err := io.ReadAll(io.LimitReader(e.Stdin, maxRequest+1))
	if err != nil {
		return childFailed(e, "cannot read the request: "+err.Error())
	}
	req, err := DecodeRequest(raw)
	if err != nil {
		return childFailed(e, "bad request: "+err.Error())
	}
	sys, err := e.wired()
	if err != nil && !errors.Is(err, errUnwired) {
		return childFailed(e, err.Error())
	}
	if sys == nil || sys.Open == nil {
		return childFailed(e, "op "+req.Op+" is not served by this build")
	}
	a := Serve(ctx, sys.Open, req)
	raw, err = EncodeAnswer(a)
	if err != nil {
		return childFailed(e, "cannot encode the answer: "+err.Error())
	}
	if code := answer(e, string(raw)+"\n"); code != term.ExitOK {
		return code
	}
	if !a.OK {
		return term.ExitFailure
	}
	return term.ExitOK
}

// childFailed answers a `failed` error and exits 1 (CC5).
func childFailed(e Env, message string) term.ExitCode {
	raw, err := EncodeAnswer(Answer{Error: &AnswerError{Class: ClassFailed, Message: oneLine(message)}})
	if err != nil {
		return fail(e.Stderr, term.ExitFailure, "cannot encode the answer: "+err.Error())
	}
	if code := answer(e, string(raw)+"\n"); code != term.ExitOK {
		return code
	}
	return term.ExitFailure
}

// oneLine is s with every character that could break a line a space, cut to maxLine bytes, so
// any error's text is one line an answer carries.
func oneLine(s string) string {
	s = strings.Map(func(r rune) rune {
		if breaksLine(r) {
			return ' '
		}
		return r
	}, s)
	if len(s) <= maxLine {
		return s
	}
	cut := maxLine - len(cutMark)
	for !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + cutMark
}

const cutMark = "..."

// breaksLine is a character a reader of root's lines could take as a line end or an escape: a
// control (C0, DEL, C1), a format character, or the Unicode line or paragraph separator. It is
// stricter than A 13.3's membership line (proto), a protocol rule, by the format characters: no
// child-written text root prints needs one.
func breaksLine(r rune) bool {
	return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == 0x2028 || r == 0x2029
}

// Request is one operation asked of the child (CC3).
type Request struct {
	Op   string
	Team paths.TeamID
	Args proto.Value
}

// EncodeRequest is the request on the wire.
func EncodeRequest(r Request) ([]byte, error) {
	if !validOp(r.Op) || r.Team.String() == "" || r.Args.Kind() != proto.KindObject {
		return nil, errors.New("the request needs an op, a team and object args")
	}
	return proto.Obj(
		proto.M("v", proto.Int(wireMajor)), proto.M("op", proto.Str(r.Op)),
		proto.M("team", proto.Str(r.Team.String())), proto.M("args", r.Args),
	).Compact(), nil
}

func validOp(op string) bool { return len(op) <= maxOp && opRE.MatchString(op) }

// DecodeRequest reads a request strictly (CC3).
func DecodeRequest(raw []byte) (Request, error) {
	if len(raw) > maxRequest {
		return Request{}, fmt.Errorf("the request is over %d bytes", maxRequest)
	}
	obj, err := strictObject(raw, "v", "op", "team", "args")
	if err != nil {
		return Request{}, err
	}
	if v := obj["v"]; v.Kind() != proto.KindInt || v.Int() != wireMajor {
		return Request{}, errors.New("v is not 1")
	}
	op := obj["op"]
	if op.Kind() != proto.KindString || !validOp(op.Str()) {
		return Request{}, errors.New("op is not a dotted lower-case word")
	}
	teamValue := obj["team"]
	if teamValue.Kind() != proto.KindString {
		return Request{}, errors.New("team is not a string")
	}
	team, err := paths.ParseTeamID(teamValue.Str())
	if err != nil {
		return Request{}, fmt.Errorf("team: %w", err)
	}
	if obj["args"].Kind() != proto.KindObject {
		return Request{}, errors.New("args is not an object")
	}
	return Request{Op: op.Str(), Team: team, Args: obj["args"]}, nil
}

// strictObject is raw as one object whose keys are exactly want.
func strictObject(raw []byte, want ...string) (map[string]proto.Value, error) {
	v, err := proto.Parse(raw)
	if err != nil {
		return nil, err
	}
	return exactKeys(v, want...)
}

func exactKeys(v proto.Value, want ...string) (map[string]proto.Value, error) {
	return keys(v, want)
}

// keys is v as an object holding every key of want and no key outside want and optional.
func keys(v proto.Value, want []string, optional ...string) (map[string]proto.Value, error) {
	if v.Kind() != proto.KindObject {
		return nil, errors.New("not an object")
	}
	out := make(map[string]proto.Value, len(want))
	for _, m := range v.Members() {
		if !slices.Contains(want, m.Key) && !slices.Contains(optional, m.Key) {
			return nil, fmt.Errorf("unknown key %s", strconv.Quote(m.Key))
		}
		out[m.Key] = m.Val
	}
	for _, k := range want {
		if _, ok := out[k]; !ok {
			return nil, fmt.Errorf("no %s key", k)
		}
	}
	return out, nil
}

// ErrorClass is the kind of a failure the child answers, which root maps to its exit code
// (CC6).
type ErrorClass string

// The failure classes.
const (
	ClassFailed      ErrorClass = "failed"
	ClassRefused     ErrorClass = "refused"
	ClassUnreachable ErrorClass = "unreachable"
	ClassConfig      ErrorClass = "config"
)

var classExit = map[ErrorClass]term.ExitCode{
	ClassFailed: term.ExitFailure, ClassRefused: ExitRefused, ClassUnreachable: ExitUnreachable, ClassConfig: ExitConfig,
}

// Answer is the child's one answer (CC5, CV2, CO1-CO3, BU1): what changed, and for a member
// verb its handle, the credential the caller asked for with the team's main room, and notes for
// the status record; for team.view the view, for member.disconnected the notice; or the
// failure, with the lines that changed before it.
type Answer struct {
	OK         bool
	Changed    []string
	Handle     string
	Credential *apply.Credential
	MainRoom   string
	Notes      []string
	View       *health.View
	Notice     *health.Notice
	Error      *AnswerError
}

// AnswerError is a failed answer's class and one-line message.
type AnswerError struct {
	Class   ErrorClass
	Message string
}

// ExitCode is root's exit code for the answer; the child's own exit code is never used (CC6).
func (a Answer) ExitCode() term.ExitCode {
	if a.OK {
		return term.ExitOK
	}
	return classExit[a.Error.Class]
}

func (a Answer) validate() error {
	if a.OK != (a.Error == nil) {
		return errors.New("an answer is ok or carries an error, not both or neither")
	}
	if err := validLines("changed", a.Changed, maxChanged); err != nil {
		return err
	}
	if !a.OK {
		return a.validateFailure()
	}
	if a.Handle != "" && !validHandle(a.Handle) {
		return errors.New("the handle is not a member handle")
	}
	if a.Credential != nil {
		if err := validCredential(*a.Credential); err != nil {
			return err
		}
	}
	if err := validLines("notes", a.Notes, maxNotes); err != nil {
		return err
	}
	return a.validateOps()
}

func (a Answer) validateFailure() error {
	if a.Handle != "" || a.Credential != nil || a.MainRoom != "" || len(a.Notes) > 0 || a.View != nil || a.Notice != nil {
		return errors.New("a failed answer carries no handle, credential, main room, notes, view or notice")
	}
	if _, ok := classExit[a.Error.Class]; !ok {
		return fmt.Errorf("error class %s", strconv.Quote(string(a.Error.Class)))
	}
	if !validLine(a.Error.Message) {
		return errors.New("the error message is not one line")
	}
	return nil
}

func validLines(name string, lines []string, most int) error {
	if len(lines) > most {
		return fmt.Errorf("more than %d %s lines", most, name)
	}
	for _, line := range lines {
		if !validLine(line) {
			return fmt.Errorf("a %s line is not one line", name)
		}
	}
	return nil
}

func validLine(s string) bool {
	return s != "" && len(s) <= maxLine && !strings.ContainsFunc(s, breaksLine)
}

func validHandle(s string) bool {
	h, ok := proto.ParseHandle(s)
	return ok && h.String() == s
}

// validCredential is CV2's rule: a kind the verbs issue and a secret of 1 to maxSecret printable
// ASCII bytes without a space. No error quotes the secret.
func validCredential(c apply.Credential) error {
	if c.Kind != apply.CredPassword && c.Kind != apply.CredToken {
		return errors.New("the credential kind is not password or token")
	}
	if c.Secret == "" || len(c.Secret) > maxSecret || strings.ContainsFunc(c.Secret, func(r rune) bool { return r <= ' ' || r > '~' }) {
		return fmt.Errorf("the credential's secret is not 1 to %d printable ASCII bytes without a space", maxSecret)
	}
	return nil
}

// EncodeAnswer is the answer on the wire.
func EncodeAnswer(a Answer) ([]byte, error) {
	if err := a.validate(); err != nil {
		return nil, err
	}
	members := []proto.Member{proto.M("v", proto.Int(wireMajor)), proto.M("ok", proto.Bool(a.OK))}
	if a.OK || len(a.Changed) > 0 {
		members = append(members, proto.M("changed", lineArray(a.Changed)))
	}
	if !a.OK {
		errValue := proto.Obj(proto.M("class", proto.Str(string(a.Error.Class))), proto.M("message", proto.Str(a.Error.Message)))
		return proto.Obj(append(members, proto.M("error", errValue))...).Compact(), nil
	}
	if a.Handle != "" {
		members = append(members, proto.M("handle", proto.Str(a.Handle)))
	}
	if c := a.Credential; c != nil {
		members = append(members, proto.M("credential", proto.Obj(proto.M("kind", proto.Str(c.Kind)), proto.M("secret", proto.Str(c.Secret)))))
	}
	if len(a.Notes) > 0 {
		members = append(members, proto.M("notes", lineArray(a.Notes)))
	}
	return proto.Obj(append(members, a.encodeOps()...)...).Compact(), nil
}

func lineArray(lines []string) proto.Value {
	items := make([]proto.Value, 0, len(lines))
	for _, line := range lines {
		items = append(items, proto.Str(line))
	}
	return proto.Arr(items...)
}

// DecodeAnswer reads the child's answer strictly, the one thing root parses that a
// less-privileged process wrote (CC6).
func DecodeAnswer(raw []byte) (Answer, error) {
	if len(raw) > maxAnswer {
		return Answer{}, fmt.Errorf("the answer is over %d bytes", maxAnswer)
	}
	v, err := proto.Parse(raw)
	if err != nil {
		return Answer{}, err
	}
	ok, found := v.Get("ok")
	if !found || ok.Kind() != proto.KindBool {
		return Answer{}, errors.New("ok is not a boolean")
	}
	if ok.Bool() {
		return decodeDone(v)
	}
	return decodeFailed(v)
}

func decodeDone(v proto.Value) (Answer, error) {
	obj, err := keys(v, []string{"v", "ok", "changed"}, "handle", "credential", "notes", keyMainRoom, keyView, keyNotice)
	if err != nil {
		return Answer{}, err
	}
	if err = checkMajor(obj["v"]); err != nil {
		return Answer{}, err
	}
	a := Answer{OK: true}
	if a.Changed, err = stringList(obj["changed"], "changed"); err != nil {
		return Answer{}, err
	}
	if h, given := obj["handle"]; given {
		if h.Kind() != proto.KindString || h.Str() == "" {
			return Answer{}, errors.New("handle is not a member handle")
		}
		a.Handle = h.Str()
	}
	if c, given := obj["credential"]; given {
		if a.Credential, err = decodeCredential(c); err != nil {
			return Answer{}, err
		}
	}
	if n, given := obj["notes"]; given {
		if a.Notes, err = stringList(n, "notes"); err != nil {
			return Answer{}, err
		}
		if len(a.Notes) == 0 {
			return Answer{}, errors.New("notes is empty")
		}
	}
	if err = decodeOps(&a, obj); err != nil {
		return Answer{}, err
	}
	return a, a.validate()
}

func decodeCredential(v proto.Value) (*apply.Credential, error) {
	c, err := exactKeys(v, "kind", "secret")
	if err != nil {
		return nil, fmt.Errorf("credential: %w", err)
	}
	if c["kind"].Kind() != proto.KindString || c["secret"].Kind() != proto.KindString {
		return nil, errors.New("the credential's kind and secret are not strings")
	}
	return &apply.Credential{Kind: c["kind"].Str(), Secret: c["secret"].Str()}, nil
}

func stringList(v proto.Value, name string) ([]string, error) {
	if v.Kind() != proto.KindArray {
		return nil, fmt.Errorf("%s is not an array", name)
	}
	var out []string
	for _, item := range v.Items() {
		if item.Kind() != proto.KindString {
			return nil, fmt.Errorf("a %s line is not a string", name)
		}
		out = append(out, item.Str())
	}
	return out, nil
}

func decodeFailed(v proto.Value) (Answer, error) {
	obj, err := keys(v, []string{"v", "ok", "error"}, "changed")
	if err != nil {
		return Answer{}, err
	}
	if err = checkMajor(obj["v"]); err != nil {
		return Answer{}, err
	}
	e, err := exactKeys(obj["error"], "class", "message")
	if err != nil {
		return Answer{}, fmt.Errorf("error: %w", err)
	}
	if e["class"].Kind() != proto.KindString || e["message"].Kind() != proto.KindString {
		return Answer{}, errors.New("error class and message are not strings")
	}
	a := Answer{Error: &AnswerError{Class: ErrorClass(e["class"].Str()), Message: e["message"].Str()}}
	if lines, given := obj["changed"]; given {
		if a.Changed, err = stringList(lines, "changed"); err != nil {
			return Answer{}, err
		}
		if len(a.Changed) == 0 {
			return Answer{}, errors.New("a failed answer's changed is empty")
		}
	}
	return a, a.validate()
}

func checkMajor(v proto.Value) error {
	if v.Kind() != proto.KindInt || v.Int() != wireMajor {
		return errors.New("v is not 1")
	}
	return nil
}
