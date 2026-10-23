package wire

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

const kit = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func teamID(t *testing.T, s string) paths.TeamID {
	t.Helper()
	id, err := paths.ParseTeamID(s)
	check(t, err)
	return id
}

// refused is err's exit and code when it is an *exits.Error.
func refused(t *testing.T, err error) (int, string) {
	t.Helper()
	e, ok := errors.AsType[*exits.Error](err)
	if !ok {
		t.Fatalf("not an error object: %v", err)
	}
	return int(e.Exit), e.Code
}

func TestARequestRoundTrips(t *testing.T) {
	stdin := "Done, pushed."
	for _, r := range []Request{
		{V: 1, Op: OpRecv, Kit: kit, DeadlineMS: 1_760_000_030_000, Args: json.RawMessage(`{"team":null}`)},
		{V: 1, Op: OpReply, Kit: kit, DeadlineMS: 5, Args: json.RawMessage(`{"id":"$x","verb":null}`), Stdin: &stdin},
	} {
		line, err := r.Encode()
		check(t, err)
		got, err := DecodeRequest(line)
		check(t, err)
		again, err := got.Encode()
		check(t, err)
		if string(again) != string(line) {
			t.Errorf("round trip: %s, then %s", line, again)
		}
	}
}

// B 2.4.4's own example request decodes.
func TestBsExampleRequestDecodes(t *testing.T) {
	line := `{"v":1,"op":"reply","kit":"` + kit + `","deadline_ms":1760000030000,"args":{"id":"$<43>","verb":null},"stdin":"Done, pushed as <sha>."}`
	r, err := DecodeRequest([]byte(line))
	check(t, err)
	if r.Op != OpReply || r.DeadlineMS != 1760000030000 || r.Stdin == nil || *r.Stdin != "Done, pushed as <sha>." {
		t.Fatalf("%+v", r)
	}
}

// LN2: a request over 64 KiB is exit 10 code size, whatever it holds.
func TestAnOversizedRequestIsTenSize(t *testing.T) {
	line := []byte(strings.Repeat(" ", MaxRequest+1))
	if exit, code := refused(t, func() error { _, err := DecodeRequest(line); return err }()); exit != 10 || code != exits.CodeSize {
		t.Fatalf("exit %d code %s", exit, code)
	}
	at := []byte(`{"v":1,"op":"recv","kit":"` + kit + `","deadline_ms":1,"args":{},"stdin":null}`)
	at = append(at, []byte(strings.Repeat(" ", MaxRequest-len(at)))...)
	if _, err := DecodeRequest(at); err != nil {
		t.Fatalf("a request of exactly 64 KiB: %v", err)
	}
}

func TestARequestIsRefusedByTheStrictRules(t *testing.T) {
	ok := map[string]string{"v": `1`, "op": `"recv"`, "kit": `"` + kit + `"`, "deadline_ms": `1`, "args": `{}`, "stdin": `null`}
	build := func(change map[string]string, drop string) []byte {
		var b strings.Builder
		b.WriteString("{")
		first := true
		for _, k := range []string{"v", "op", "kit", "deadline_ms", "args", "stdin", "extra", "Op"} {
			v, has := ok[k]
			if c, changed := change[k]; changed {
				v, has = c, true
			}
			if !has || k == drop {
				continue
			}
			if !first {
				b.WriteString(",")
			}
			first = false
			b.WriteString(`"` + k + `":` + v)
		}
		b.WriteString("}")
		return []byte(b.String())
	}
	for name, c := range map[string]struct {
		line []byte
		code string
	}{
		"not json":         {[]byte(`{"v":1`), exits.CodeSchema},
		"repeated key":     {[]byte(`{"v":1,"v":1}`), exits.CodeSchema},
		"not an object":    {[]byte(`[1]`), exits.CodeSchema},
		"v 2":              {build(map[string]string{"v": `2`}, ""), exits.CodeSchema},
		"unknown op":       {build(map[string]string{"op": `"nope"`}, ""), exits.CodeVerb},
		"kit not a digest": {build(map[string]string{"kit": `"abc"`}, ""), exits.CodeSchema},
		"deadline zero":    {build(map[string]string{"deadline_ms": `0`}, ""), exits.CodeSchema},
		"args not object":  {build(map[string]string{"args": `[]`}, ""), exits.CodeSchema},
		"args null":        {build(map[string]string{"args": `null`}, ""), exits.CodeSchema},
		"stdin a number":   {build(map[string]string{"stdin": `1`}, ""), exits.CodeSchema},
		"a key missing":    {build(nil, "stdin"), exits.CodeSchema},
		"an unknown key":   {build(map[string]string{"extra": `1`}, ""), exits.CodeSchema},
		"a key in a case":  {build(map[string]string{"Op": `"recv"`}, "op"), exits.CodeSchema},
		"a float":          {build(map[string]string{"deadline_ms": `1.5`}, ""), exits.CodeSchema},
	} {
		_, err := DecodeRequest(c.line)
		if err == nil {
			t.Errorf("%s: decoded", name)
			continue
		}
		if exit, code := refused(t, err); exit != 10 || code != c.code {
			t.Errorf("%s: exit %d code %s, want 10 %s", name, exit, code, c.code)
		}
		if strings.Contains(err.Error(), "nope") {
			t.Errorf("%s: the refusal quotes the request", name)
		}
	}
}

func TestEncodeRefusesWhatDecodeRefuses(t *testing.T) {
	if _, err := (Request{V: 1, Op: "nope", Kit: kit, DeadlineMS: 1, Args: json.RawMessage(`{}`)}).Encode(); err == nil {
		t.Fatal("encoded an unknown op")
	}
	if _, err := (Request{V: 1, Op: OpRecv, Kit: kit, DeadlineMS: 1, Args: json.RawMessage(`{`)}).Encode(); err == nil {
		t.Fatal("encoded args that are not JSON")
	}
}

func TestDoneRoundTrips(t *testing.T) {
	team := teamID(t, "alpha")
	e := exits.New(exits.CodeNotFound, &team, "no event {} in the team", exits.Name("$x"))
	for _, c := range []struct {
		done Done
		line string
	}{
		{Done{Exit: 0}, `{"done":{"exit":0}}`},
		{Done{Exit: 0, Ack: "0a1b2c3d"}, `{"done":{"exit":0,"ack":"0a1b2c3d"}}`},
		{Done{Exit: 23, Error: e}, `{"done":{"exit":23,"error":` + string(e.Object().Compact()) + `}}`},
	} {
		if got := string(c.done.Encode()); got != c.line {
			t.Errorf("encode: %s, want %s", got, c.line)
		}
		got, err := DecodeDone([]byte(c.line))
		check(t, err)
		if got.Exit != c.done.Exit || got.Ack != c.done.Ack || (got.Error == nil) != (c.done.Error == nil) {
			t.Errorf("decode %s: %+v", c.line, got)
		}
		if got.Error != nil && (got.Error.Code != e.Code || got.Error.Message != e.Message || *got.Error.Team != team) {
			t.Errorf("decode %s: error %+v", c.line, got.Error)
		}
	}
}

// Encode takes a failing done's exit from its error object, so the two cannot differ.
func TestDoneEncodesItsErrorsExit(t *testing.T) {
	e := exits.New(exits.CodeSize, nil, "too big")
	got, err := DecodeDone(Done{Exit: 0, Error: e}.Encode())
	check(t, err)
	if got.Exit != int(exits.ExitRefused) {
		t.Fatalf("exit %d", got.Exit)
	}
}

func TestDecodeDoneIsStrict(t *testing.T) {
	errObj := string(exits.New(exits.CodeSize, nil, "too big").Object().Compact())
	for _, line := range []string{
		`{"done":{}}`,
		`{"done":{"exit":0},"x":1}`,
		`{"done":{"exit":0,"extra":1}}`,
		`{"done":{"exit":10}}`,
		`{"done":{"exit":0,"error":` + errObj + `}}`,
		`{"done":{"exit":23,"error":` + errObj + `}}`,
		`{"done":{"exit":10,"error":{"kind":"error"}}}`,
		`{"done":{"exit":10,"error":` + errObj + `,"ack":"0a1b2c3d"}}`,
		`{"done":{"exit":0,"ack":"0A1B2C3D"}}`,
		`{"done":{"exit":0,"ack":"0a1b2c3"}}`,
		`{"done":{"exit":"0"}}`,
		`{"done":1}`,
		`{"ack":"0a1b2c3d"}`,
		`not json`,
	} {
		if _, err := DecodeDone([]byte(line)); !errors.Is(err, ErrMalformed) {
			t.Errorf("%s: %v", line, err)
		}
	}
}

func TestAckRoundTripsAndIsStrict(t *testing.T) {
	a, err := NewAck()
	check(t, err)
	if len(a.Ack) != 8 {
		t.Fatalf("ack %q", a.Ack)
	}
	got, err := DecodeAck(a.Encode())
	check(t, err)
	if got != a {
		t.Fatalf("round trip %+v", got)
	}
	for _, line := range []string{`{"ack":"0a1b2c3"}`, `{"ack":"0a1b2c3d","x":1}`, `{"ack":1}`, `{}`, `{"done":{"exit":0}}`} {
		if _, err := DecodeAck([]byte(line)); !errors.Is(err, ErrMalformed) {
			t.Errorf("%s: %v", line, err)
		}
	}
}

// LN2's table: hooks 300 ms; recv, inbox and status 2 s; sends and verify 30 s; history and
// questions 60 s; wait its own timeout.
func TestDeadlineIsLN2sTable(t *testing.T) {
	for op, want := range map[string]time.Duration{
		OpHookSessionStart: 300 * time.Millisecond, OpHookPrompt: 300 * time.Millisecond,
		OpHookStop: 300 * time.Millisecond, OpHookSessionEnd: 300 * time.Millisecond,
		OpRecv: 2 * time.Second, OpInbox: 2 * time.Second, OpStatus: 2 * time.Second,
		OpReply: 30 * time.Second, OpSay: 30 * time.Second, OpPing: 30 * time.Second, OpVerify: 30 * time.Second,
		OpHistory: time.Minute, OpQuestions: time.Minute,
		OpWait: 110 * time.Second,
	} {
		if got := Deadline(op, 110*time.Second); got != want {
			t.Errorf("%s: %v, want %v", op, got, want)
		}
	}
	if got := Deadline("nope", time.Hour); got != 0 {
		t.Errorf("an unknown op has deadline %v", got)
	}
}

func TestArgsDecodeStrictly(t *testing.T) {
	var w WaitArgs
	check(t, DecodeArgs(json.RawMessage(`{"team":"alpha","timeout_s":110}`), &w))
	if w.Team == nil || w.Team.String() != "alpha" || w.TimeoutS != 110 {
		t.Fatalf("%+v", w)
	}
	if err := DecodeArgs(json.RawMessage(`{"timeout_s":110}`), &w); err == nil {
		t.Error("wait's args with no team key decoded")
	}
	var h HistoryArgs
	check(t, DecodeArgs(json.RawMessage(`{"team":"alpha","room":"main","limit":20}`), &h))
	if h.Team == nil || h.Team.String() != "alpha" || h.Room != "main" || h.Limit != 20 {
		t.Fatalf("%+v", h)
	}
	var tm TeamsArgs
	check(t, DecodeArgs(json.RawMessage(`{"team":null}`), &tm))
	if tm.Team != nil {
		t.Fatalf("%+v", tm)
	}
	if err := DecodeArgs(json.RawMessage(`{"team":"no such team!"}`), &tm); err == nil {
		t.Error("a team that is not a team id decoded")
	}
}

// P2 note 7: verify's args carry its --team; every refusal is 10 schema.
func TestVerifyArgsDecodeStrictly(t *testing.T) {
	var v VerifyArgs
	check(t, DecodeArgs(json.RawMessage(`{"team":null,"id":"$abc"}`), &v))
	if v.Team != nil || v.ID != "$abc" {
		t.Fatalf("%+v", v)
	}
	for name, raw := range map[string]string{
		"unknown key": `{"team":null,"id":"$abc","x":1}`,
		"missing key": `{"id":"$abc"}`,
		"wrong type":  `{"team":null,"id":1}`,
		"not json":    `{`,
		"null":        `null`,
		"repeated":    `{"team":null,"id":"a","id":"b"}`,
	} {
		err := DecodeArgs(json.RawMessage(raw), &v)
		if exit, code := refused(t, err); exit != 10 || code != exits.CodeSchema {
			t.Errorf("%s: exit %d code %s", name, exit, code)
		}
	}
}
