package admin

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const goodRequest = `{"v":1,"op":"member.add","team":"myteam","args":{"seat":"3"}}`

func TestChildSpec(t *testing.T) {
	spec, err := NewChildSpec("/usr/local/sbin/teamchat-admin", User{UID: ctlUID, GID: ctlGID})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Path != "/usr/local/sbin/teamchat-admin" || !slices.Equal(spec.Args, []string{"ctl-child"}) ||
		spec.UID != ctlUID || spec.GID != ctlGID || spec.Groups == nil || len(spec.Groups) != 0 || spec.Dir != "/" ||
		!slices.Equal(spec.Env, []string{"PATH=/usr/bin:/bin"}) {
		t.Fatalf("spec = %+v", spec)
	}
	for name, c := range map[string]struct {
		self string
		u    User
	}{
		"relative":   {"teamchat-admin", User{UID: ctlUID, GID: ctlGID}},
		"unclean":    {"/usr/local/sbin/../sbin/teamchat-admin", User{UID: ctlUID, GID: ctlGID}},
		"root uid":   {"/x", User{UID: 0, GID: ctlGID}},
		"root gid":   {"/x", User{UID: ctlUID, GID: 0}},
		"negative":   {"/x", User{UID: -1, GID: ctlGID}},
		"too large":  {"/x", User{UID: 1 << 33, GID: ctlGID}},
		"gid huge":   {"/x", User{UID: ctlUID, GID: 1 << 33}},
		"empty self": {"", User{UID: ctlUID, GID: ctlGID}},
	} {
		if _, err := NewChildSpec(c.self, c.u); err == nil {
			t.Errorf("%s: spec accepted", name)
		}
	}
}

func TestDecodeRequest(t *testing.T) {
	req, err := DecodeRequest([]byte(goodRequest))
	if err != nil {
		t.Fatal(err)
	}
	if req.Op != "member.add" || req.Team.String() != "myteam" || string(req.Args.Compact()) != `{"seat":"3"}` {
		t.Fatalf("request = %+v", req)
	}
	bad := map[string]string{
		"not json":       `{`,
		"array":          `[]`,
		"v 2":            `{"v":2,"op":"a","team":"myteam","args":{}}`,
		"v string":       `{"v":"1","op":"a","team":"myteam","args":{}}`,
		"no op":          `{"v":1,"team":"myteam","args":{}}`,
		"op upper":       `{"v":1,"op":"Member.add","team":"myteam","args":{}}`,
		"op dot end":     `{"v":1,"op":"member.","team":"myteam","args":{}}`,
		"op empty":       `{"v":1,"op":"","team":"myteam","args":{}}`,
		"team bad":       `{"v":1,"op":"a","team":"../x","args":{}}`,
		"team number":    `{"v":1,"op":"a","team":1,"args":{}}`,
		"args array":     `{"v":1,"op":"a","team":"myteam","args":[]}`,
		"no args":        `{"v":1,"op":"a","team":"myteam"}`,
		"unknown key":    `{"v":1,"op":"a","team":"myteam","args":{},"x":1}`,
		"repeated key":   `{"v":1,"op":"a","op":"b","team":"myteam","args":{}}`,
		"trailing":       goodRequest + `{}`,
		"too large":      `{"v":1,"op":"a","team":"myteam","args":{"x":"` + strings.Repeat("a", maxRequest) + `"}}`,
		"op too long":    `{"v":1,"op":"` + strings.Repeat("a", 65) + `","team":"myteam","args":{}}`,
		"op empty words": `{"v":1,"op":"a..b","team":"myteam","args":{}}`,
	}
	for name, raw := range bad {
		if _, err := DecodeRequest([]byte(raw)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestRequestRoundTrip(t *testing.T) {
	team, err := paths.ParseTeamID("myteam")
	if err != nil {
		t.Fatal(err)
	}
	args, err := proto.Parse([]byte(`{"prefix":"r+s.podman"}`))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := EncodeRequest(Request{Op: "member.add", Team: team, Args: args})
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"v":1,"op":"member.add","team":"myteam","args":{"prefix":"r+s.podman"}}` {
		t.Fatalf("encoded %s", raw)
	}
	if _, err := EncodeRequest(Request{Op: "Bad", Team: team, Args: args}); err == nil {
		t.Error("bad op encoded")
	}
	if _, err := EncodeRequest(Request{Op: "a", Team: team, Args: proto.Str("x")}); err == nil {
		t.Error("non-object args encoded")
	}
}

func TestAnswerRoundTrip(t *testing.T) {
	cases := []struct {
		answer Answer
		wire   string
		code   term.ExitCode
	}{
		{Answer{OK: true, Changed: []string{"member x added"}}, `{"v":1,"ok":true,"changed":["member x added"]}`, term.ExitOK},
		{Answer{OK: true}, `{"v":1,"ok":true,"changed":[]}`, term.ExitOK},
		{Answer{Error: &AnswerError{Class: ClassFailed, Message: "boom"}}, `{"v":1,"ok":false,"error":{"class":"failed","message":"boom"}}`, term.ExitFailure},
		{Answer{Error: &AnswerError{Class: ClassRefused, Message: "no"}}, `{"v":1,"ok":false,"error":{"class":"refused","message":"no"}}`, ExitRefused},
		{Answer{Error: &AnswerError{Class: ClassUnreachable, Message: "down"}}, `{"v":1,"ok":false,"error":{"class":"unreachable","message":"down"}}`, ExitUnreachable},
		{Answer{Error: &AnswerError{Class: ClassConfig, Message: "file"}}, `{"v":1,"ok":false,"error":{"class":"config","message":"file"}}`, ExitConfig},
	}
	for _, c := range cases {
		raw, err := EncodeAnswer(c.answer)
		if err != nil || string(raw) != c.wire {
			t.Errorf("encode %+v = %s, %v", c.answer, raw, err)
			continue
		}
		back, err := DecodeAnswer(raw)
		if err != nil || back.ExitCode() != c.code || back.OK != c.answer.OK {
			t.Errorf("decode %s = %+v, %v", raw, back, err)
		}
		if c.answer.OK && !slices.Equal(back.Changed, c.answer.Changed) && len(c.answer.Changed) > 0 {
			t.Errorf("changed %v", back.Changed)
		}
		if !c.answer.OK && *back.Error != *c.answer.Error {
			t.Errorf("error %+v", back.Error)
		}
	}
	for name, a := range map[string]Answer{
		"ok with error":     {OK: true, Error: &AnswerError{Class: ClassFailed, Message: "x"}},
		"failed no error":   {},
		"bad class":         {Error: &AnswerError{Class: "maybe", Message: "x"}},
		"two lines":         {Error: &AnswerError{Class: ClassFailed, Message: "a\nb"}},
		"empty message":     {Error: &AnswerError{Class: ClassFailed}},
		"changed two lines": {OK: true, Changed: []string{"a\rb"}},
		"changed empty":     {OK: true, Changed: []string{""}},
	} {
		if _, err := EncodeAnswer(a); err == nil {
			t.Errorf("%s: encoded", name)
		}
	}
}

const sampleHandle = "myrepo.3+desk.podman"

// TestAnswerCarriesAMemberVerbsFields is CV2: the handle, the credential and notes, and a
// failure's changed lines.
func TestAnswerCarriesAMemberVerbsFields(t *testing.T) {
	cases := []struct {
		answer Answer
		wire   string
	}{
		{
			Answer{OK: true, Changed: []string{"member x issued"}, Handle: sampleHandle, Credential: &apply.Credential{Kind: apply.CredToken, Secret: "syt_abc~1"}, Notes: []string{"a note"}},
			`{"v":1,"ok":true,"changed":["member x issued"],"handle":"` + sampleHandle + `","credential":{"kind":"token","secret":"syt_abc~1"},"notes":["a note"]}`,
		},
		{
			Answer{OK: true, Handle: sampleHandle},
			`{"v":1,"ok":true,"changed":[],"handle":"` + sampleHandle + `"}`,
		},
		{
			Answer{Changed: []string{"member x claimed"}, Error: &AnswerError{Class: ClassFailed, Message: "boom"}},
			`{"v":1,"ok":false,"changed":["member x claimed"],"error":{"class":"failed","message":"boom"}}`,
		},
	}
	for _, c := range cases {
		raw, err := EncodeAnswer(c.answer)
		if err != nil || string(raw) != c.wire {
			t.Errorf("encode = %s, %v\nwant %s", raw, err, c.wire)
			continue
		}
		back, err := DecodeAnswer(raw)
		if err != nil || back.Handle != c.answer.Handle || !slices.Equal(back.Changed, c.answer.Changed) || !slices.Equal(back.Notes, c.answer.Notes) {
			t.Errorf("decode %s = %+v, %v", raw, back, err)
		}
		if (back.Credential == nil) != (c.answer.Credential == nil) || back.Credential != nil && *back.Credential != *c.answer.Credential {
			t.Errorf("decode %s: credential %v", raw, back.Credential)
		}
	}
}

func TestAnswerRefusesABadMemberField(t *testing.T) {
	cred := func(kind, secret string) *apply.Credential { return &apply.Credential{Kind: kind, Secret: secret} }
	failure := &AnswerError{Class: ClassFailed, Message: "x"}
	for name, a := range map[string]Answer{
		"credential on a failure": {Credential: cred(apply.CredToken, "s"), Error: failure},
		"handle on a failure":     {Handle: sampleHandle, Error: failure},
		"credential kind":         {OK: true, Credential: cred("key", "s")},
		"secret with a space":     {OK: true, Credential: cred(apply.CredPassword, "a b")},
		"secret empty":            {OK: true, Credential: cred(apply.CredPassword, "")},
		"secret too long":         {OK: true, Credential: cred(apply.CredPassword, strings.Repeat("a", maxSecret+1))},
		"secret not ascii":        {OK: true, Credential: cred(apply.CredPassword, "é")},
		"handle":                  {OK: true, Handle: "not a handle"},
		"note two lines":          {OK: true, Notes: []string{"a\nb"}},
	} {
		if _, err := EncodeAnswer(a); err == nil {
			t.Errorf("%s: encoded", name)
		}
	}
	const secret = "hunter2 with a space"
	for name, raw := range map[string]string{
		"issued on a failure":  `{"v":1,"ok":false,"credential":{"kind":"token","secret":"s"},"error":{"class":"failed","message":"x"}}`,
		"failure no lines":     `{"v":1,"ok":false,"changed":[],"error":{"class":"failed","message":"x"}}`,
		"issued, an extra key": `{"v":1,"ok":true,"changed":[],"credential":{"kind":"token","secret":"s","x":1}}`,
		"issued as a string":   `{"v":1,"ok":true,"changed":[],"credential":"s"}`,
		"issued as a number":   `{"v":1,"ok":true,"changed":[],"credential":{"kind":"token","secret":1}}`,
		"issued with a space":  `{"v":1,"ok":true,"changed":[],"credential":{"kind":"token","secret":"` + secret + `"}}`,
		"handle empty":         `{"v":1,"ok":true,"changed":[],"handle":""}`,
		"handle number":        `{"v":1,"ok":true,"changed":[],"handle":3}`,
		"notes empty":          `{"v":1,"ok":true,"changed":[],"notes":[]}`,
		"notes number":         `{"v":1,"ok":true,"changed":[],"notes":[1]}`,
		"notes object":         `{"v":1,"ok":true,"changed":[],"notes":{}}`,
	} {
		_, err := DecodeAnswer([]byte(raw))
		if err == nil {
			t.Errorf("%s: accepted", name)
			continue
		}
		if strings.Contains(err.Error(), "hunter2") {
			t.Errorf("%s: the error quotes the secret: %v", name, err)
		}
	}
}

func TestEncodeAnswerRefusesLineBreakers(t *testing.T) {
	for _, r := range lineBreakers {
		for name, a := range map[string]Answer{
			"changed": {OK: true, Changed: []string{"a" + r + "b"}},
			"message": {Error: &AnswerError{Class: ClassFailed, Message: "a" + r + "b"}},
		} {
			if _, err := EncodeAnswer(a); err == nil {
				t.Errorf("%s holding %q: encoded", name, r)
			}
		}
	}
}

// lineBreakers are the characters a reader of root's lines could take as a line end or an
// escape: a C1 control (NEL, CSI), the Unicode line and paragraph separators, a format character.
var lineBreakers = []string{string(rune(0x85)), string(rune(0x9b)), string(rune(0x2028)), string(rune(0x2029)), string(rune(0x200e))}

func TestOneLineAnswersAnyMessage(t *testing.T) {
	for _, msg := range append([]string{strings.Repeat("é", maxLine), "a\nb\x7f"}, lineBreakers...) {
		got := oneLine(msg)
		if !validLine(got) || !utf8.ValidString(got) {
			t.Errorf("oneLine(%q) = %q, not one line", msg, got)
		}
	}
	if got := oneLine(strings.Repeat("a", maxLine+1)); !strings.HasSuffix(got, "...") || len(got) > maxLine {
		t.Errorf("a long message cut to %d bytes, %q at the end", len(got), got[len(got)-3:])
	}
}

func TestDecodeAnswerRefusesLineBreakers(t *testing.T) {
	for _, r := range lineBreakers {
		for _, raw := range []string{
			`{"v":1,"ok":true,"changed":["a` + r + `b"]}`,
			`{"v":1,"ok":false,"error":{"class":"failed","message":"a` + r + `b"}}`,
		} {
			if _, err := DecodeAnswer([]byte(raw)); err == nil {
				t.Errorf("%s: accepted", raw)
			}
		}
	}
}

func TestCtlChildAnswersALongBadRequest(t *testing.T) {
	got := run(t, childProc(), `{"v":1,"`+strings.Repeat("k", 10<<10)+`":1}`, "ctl-child")
	answer, err := DecodeAnswer([]byte(strings.TrimSuffix(got.stdout, "\n")))
	if got.code != term.ExitFailure || err != nil || answer.ExitCode() != term.ExitFailure {
		t.Fatalf("a 10 KiB unknown key = %+v, %v", got, err)
	}
}

func TestDecodeAnswerIsStrict(t *testing.T) {
	bad := map[string]string{
		"not json":        `nope`,
		"v 2":             `{"v":2,"ok":true,"changed":[]}`,
		"no v":            `{"ok":true,"changed":[]}`,
		"ok string":       `{"v":1,"ok":"true","changed":[]}`,
		"no ok":           `{"v":1,"changed":[]}`,
		"ok no changed":   `{"v":1,"ok":true}`,
		"ok with error":   `{"v":1,"ok":true,"changed":[],"error":{"class":"failed","message":"x"}}`,
		"fail no error":   `{"v":1,"ok":false}`,
		"fail changed":    `{"v":1,"ok":false,"changed":[],"error":{"class":"failed","message":"x"}}`,
		"bundle":          `{"v":1,"ok":true,"changed":[],"bundle":{}}`,
		"unknown":         `{"v":1,"ok":true,"changed":[],"x":1}`,
		"repeated":        `{"v":1,"ok":true,"ok":true,"changed":[]}`,
		"changed object":  `{"v":1,"ok":true,"changed":{}}`,
		"changed number":  `{"v":1,"ok":true,"changed":[1]}`,
		"changed newline": `{"v":1,"ok":true,"changed":["a\nb"]}`,
		"error extra":     `{"v":1,"ok":false,"error":{"class":"failed","message":"x","exit":70}}`,
		"error no class":  `{"v":1,"ok":false,"error":{"message":"x"}}`,
		"error bad class": `{"v":1,"ok":false,"error":{"class":"exit-70","message":"x"}}`,
		"error string":    `{"v":1,"ok":false,"error":"x"}`,
		"message newline": `{"v":1,"ok":false,"error":{"class":"failed","message":"a\nb"}}`,
		"message number":  `{"v":1,"ok":false,"error":{"class":"failed","message":1}}`,
		"trailing":        `{"v":1,"ok":true,"changed":[]} {}`,
		"too large":       `{"v":1,"ok":true,"changed":["` + strings.Repeat("a", maxAnswer) + `"]}`,
		"too many":        `{"v":1,"ok":true,"changed":[` + strings.TrimSuffix(strings.Repeat(`"a",`, maxChanged+1), ",") + `]}`,
	}
	for name, raw := range bad {
		if _, err := DecodeAnswer([]byte(raw)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestCtlChildAnswersEveryRequest(t *testing.T) {
	got := run(t, childProc(), goodRequest, "ctl-child")
	if got.code != term.ExitFailure || got.stderr != "" ||
		got.stdout != `{"v":1,"ok":false,"error":{"class":"failed","message":"op member.add is not served by this build"}}`+"\n" {
		t.Fatalf("unserved op = %+v", got)
	}
	got = run(t, childProc(), `{"v":1}`, "ctl-child")
	if got.code != term.ExitFailure || !strings.HasPrefix(got.stdout, `{"v":1,"ok":false,"error":{"class":"failed","message":"bad request: `) {
		t.Fatalf("bad request = %+v", got)
	}
	answer, err := DecodeAnswer([]byte(strings.TrimSuffix(got.stdout, "\n")))
	if err != nil || answer.ExitCode() != term.ExitFailure {
		t.Fatalf("the child's own answer does not decode: %v", err)
	}
	got = run(t, childProc(), strings.Repeat(" ", maxRequest+1), "ctl-child")
	if got.code != term.ExitFailure || !strings.Contains(got.stdout, "the request is over 65536 bytes") {
		t.Fatalf("oversized request = %+v", got)
	}
}

// errReader fails the read.
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("eio") }

func TestCtlChildUnreadableStdin(t *testing.T) {
	var stdout, stderr strings.Builder
	code := Run(t.Context(), Env{Args: []string{"ctl-child"}, Stdin: errReader{}, Stdout: &stdout, Stderr: &stderr, Proc: childProc(), Digest: digest})
	if code != term.ExitFailure || !strings.Contains(stdout.String(), "cannot read the request: eio") {
		t.Fatalf("unreadable stdin: %d %q %q", code, stdout.String(), stderr.String())
	}
}

func TestCtlChildRefusesArguments(t *testing.T) {
	got := run(t, childProc(), goodRequest, "ctl-child", "--op", "x")
	if got.code != term.ExitUsage || got.stdout != "" || got.stderr != "teamchat-admin: ctl-child takes no arguments\n" {
		t.Fatalf("ctl-child with arguments = %+v", got)
	}
}

func TestCtlChildChecksHowItWasStarted(t *testing.T) {
	cases := map[string]func(p *fakeProc){
		"extra variable": func(p *fakeProc) { p.env = append(p.env, "HOME=/root") },
		"no PATH":        func(p *fakeProc) { p.env = nil },
		"other PATH":     func(p *fakeProc) { p.env = []string{"PATH=/tmp"} },
		"cwd":            func(p *fakeProc) { p.wd = "/root" },
		"cwd error":      func(p *fakeProc) { p.wdErr = errors.New("gone") },
		"groups":         func(p *fakeProc) { p.groups = []int{10} },
		"groups error":   func(p *fakeProc) { p.groupsErr = errors.New("eperm") },
	}
	for name, change := range cases {
		p := childProc()
		change(p)
		got := run(t, p, goodRequest, "ctl-child")
		if got.code != ExitConfig || got.stdout != "" || !strings.HasPrefix(got.stderr, "teamchat-admin: ctl-child was not started by root's child spec: ") {
			t.Errorf("%s: %+v", name, got)
		}
	}
}

func TestCtlChildUnwritableStdout(t *testing.T) {
	var stderr strings.Builder
	code := Run(t.Context(), Env{Args: []string{"ctl-child"}, Stdin: strings.NewReader(goodRequest), Stdout: failWriter{}, Stderr: &stderr, Proc: childProc(), Digest: digest})
	if code != term.ExitFailure || stderr.String() != "teamchat-admin: cannot write the answer: closed\n" {
		t.Fatalf("unwritable stdout: %d %q", code, stderr.String())
	}
}
