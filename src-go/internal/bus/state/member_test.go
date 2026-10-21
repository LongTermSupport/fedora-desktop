package state

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/pyjson"
)

const (
	testServer = "bus.example.com"
	testUser   = "@alice.1+box.podman:" + testServer
	testIP     = "192.0.2.1"
)

func validMember() pyjson.Ordered {
	return pyjson.Ordered{
		{Key: "protocol", Value: 1},
		{Key: "team", Value: "alpha"},
		{Key: "user_id", Value: testUser},
		{Key: "server_name", Value: testServer},
		{Key: "base_url", Value: "http://" + testIP + ":8008"},
		{Key: "plain_http_hosts", Value: []any{testIP, "::1"}},
		{Key: "token_file", Value: "token"},
		{Key: "admin", Value: "@admin:" + testServer},
		{Key: "room", Value: testRoom},
	}
}

func with(o pyjson.Ordered, key string, v any) pyjson.Ordered {
	out := pyjson.Ordered{}
	for _, m := range o {
		if m.Key != key {
			out = append(out, m)
		}
	}
	if v != nil {
		out = append(out, pyjson.Member{Key: key, Value: v})
	}
	return out
}

func writeMember(t *testing.T, b Bundle, o pyjson.Ordered) {
	t.Helper()
	data, err := pyjson.Encode(o, pyjson.Compact)
	must(t, err)
	writeRaw(t, b.MemberPath(), string(data))
}

func newBundle(t *testing.T) Bundle {
	t.Helper()
	return Bundle{Dir: t.TempDir()}
}

// S18: v1's bundle is read, with its optional keys.
func TestReadMemberAcceptsV1sBundle(t *testing.T) {
	b := newBundle(t)
	writeMember(t, b, validMember())
	m, err := ReadMember(b, "alpha")
	expectf(t, err == nil && m.UserID == testUser && m.Handle == "alice.1+box.podman" && m.HumanText, "%+v %v", m, err)
	expectf(t, m.ServerName == testServer && m.BaseURL == "http://"+testIP+":8008" && len(m.PlainHTTPHosts) == 2 && m.Room == testRoom, "%+v", m)
	limits := pyjson.Ordered{{Key: "report_per_hour", Value: 60}, {Key: "send_burst", Value: 1}}
	writeMember(t, b, with(with(with(validMember(), "human_text", false), "limits", limits), "base_url", "https://Bus.Example.com"))
	m, err = ReadMember(b, "alpha")
	expectf(t, err == nil && !m.HumanText && m.Limits["report_per_hour"] == 60 && m.Limits["send_burst"] == 1, "%+v %v", m, err)
	_, err = ReadMember(newBundle(t), "alpha")
	expectf(t, errors.Is(err, fs.ErrNotExist), "an absent member.json: %v", err)
}

// S18, SB9: every refusal, in v1's order, classed.
func TestReadMemberRefusesWhatV1Refuses(t *testing.T) {
	cases := map[string]struct {
		member pyjson.Ordered
		want   Class
	}{
		"an unknown key":        {with(validMember(), "x", 1), ClassUnknownKey},
		"no room":               {with(validMember(), "room", nil), ClassMissingKey},
		"protocol 2":            {with(validMember(), "protocol", 2), ClassVersion},
		"protocol true":         {with(validMember(), "protocol", true), ClassType},
		"another team":          {with(validMember(), "team", "beta"), ClassGrammar},
		"a server in capitals":  {with(validMember(), "server_name", "Bus.example.com"), ClassGrammar},
		"a long server":         {with(validMember(), "server_name", strings.Repeat("a.", 127)+"aa"), ClassGrammar},
		"a human user":          {with(validMember(), "user_id", "@bob:"+testServer), ClassGrammar},
		"a user on another":     {with(validMember(), "user_id", "@alice.1+box.podman:other"), ClassGrammar},
		"a user of 1":           {with(validMember(), "user_id", 1), ClassType},
		"another admin":         {with(validMember(), "admin", "@root:"+testServer), ClassGrammar},
		"a bad room":            {with(validMember(), "room", "!x"), ClassGrammar},
		"a token path":          {with(validMember(), "token_file", "/etc/token"), ClassGrammar},
		"hosts an object":       {with(validMember(), "plain_http_hosts", pyjson.Ordered{}), ClassType},
		"a host name":           {with(validMember(), "plain_http_hosts", []any{"example.com"}), ClassGrammar},
		"a host of 1":           {with(validMember(), "plain_http_hosts", []any{1}), ClassType},
		"a long-form host":      {with(validMember(), "plain_http_hosts", []any{"::0:1"}), ClassGrammar},
		"a host twice":          {with(validMember(), "plain_http_hosts", []any{testIP, testIP}), ClassGrammar},
		"a scoped host":         {with(validMember(), "plain_http_hosts", []any{"fe80::1%eth0"}), ClassGrammar},
		"http to a name":        {with(validMember(), "base_url", "http://example.com"), ClassGrammar},
		"http to an unlisted":   {with(validMember(), "base_url", "http://192.0.2.2"), ClassGrammar},
		"a path":                {with(validMember(), "base_url", "https://example.com/x"), ClassGrammar},
		"credentials":           {with(validMember(), "base_url", "https://u@example.com"), ClassGrammar},
		"a query":               {with(validMember(), "base_url", "https://example.com?x"), ClassGrammar},
		"ftp":                   {with(validMember(), "base_url", "ftp://example.com"), ClassGrammar},
		"a bad port":            {with(validMember(), "base_url", "https://example.com:99999"), ClassGrammar},
		"no host":               {with(validMember(), "base_url", "https://"), ClassGrammar},
		"a bad bracket":         {with(validMember(), "base_url", "https://[x]"), ClassGrammar},
		"a url of 1":            {with(validMember(), "base_url", 1), ClassType},
		"human_text 1":          {with(validMember(), "human_text", 1), ClassType},
		"limits a list":         {with(validMember(), "limits", []any{}), ClassType},
		"limits null":           {with(validMember(), "limits", pyjson.Null{}), ClassType},
		"an unknown limit":      {with(validMember(), "limits", pyjson.Ordered{{Key: "x", Value: 1}}), ClassUnknownKey},
		"a limit out of bounds": {with(validMember(), "limits", pyjson.Ordered{{Key: "report_per_hour", Value: 61}}), ClassGrammar},
		"a limit true":          {with(validMember(), "limits", pyjson.Ordered{{Key: "send_burst", Value: true}}), ClassType},
	}
	b := newBundle(t)
	for name, c := range cases {
		writeMember(t, b, c.member)
		_, err := ReadMember(b, "alpha")
		wantClass(t, name, err, c.want)
	}
	writeRaw(t, b.MemberPath(), "[]")
	_, err := ReadMember(b, "alpha")
	wantClass(t, "a list", err, ClassType)
	must(t, os.Remove(b.MemberPath()))
	other := newBundle(t)
	writeMember(t, other, validMember())
	must(t, os.Symlink(other.MemberPath(), b.MemberPath()))
	_, err = ReadMember(b, "alpha")
	wantClass(t, "a linked member.json", err, ClassFile)
}

// S19: the token is a regular file of this uid, mode within 0600, printable ASCII.
func TestReadTokenChecksTheFileAndNeverQuotesIt(t *testing.T) {
	b := newBundle(t)
	uid := os.Getuid()
	_, err := ReadToken(b, uid)
	expectf(t, errors.Is(err, fs.ErrNotExist), "an absent token: %v", err)
	writeRaw(t, b.TokenPath(), "s3cr3t-value")
	token, err := ReadToken(b, uid)
	expectf(t, err == nil && token == "s3cr3t-value", "%v", err)
	_, err = ReadToken(b, uid+1)
	wantClass(t, "another owner", err, ClassFile)
	must(t, os.Chmod(b.TokenPath(), 0o640))
	_, err = ReadToken(b, uid)
	wantClass(t, "a group bit", err, ClassFile)
	must(t, os.Chmod(b.TokenPath(), 0o600))
	writeRaw(t, b.TokenPath(), "s3cr3t value\n")
	_, err = ReadToken(b, uid)
	wantClass(t, "a space", err, ClassSyntax)
	expectf(t, !strings.Contains(err.Error(), "s3cr3t"), "the refusal quotes the token: %v", err)
	writeRaw(t, b.TokenPath(), "")
	_, err = ReadToken(b, uid)
	wantClass(t, "an empty token", err, ClassSyntax)
	writeRaw(t, b.TokenPath(), strings.Repeat("x", maxTokenBytes+1))
	_, err = ReadToken(b, uid)
	wantClass(t, "a long token", err, ClassSize)
	must(t, os.Remove(b.TokenPath()))
	must(t, os.Mkdir(b.TokenPath(), 0o700))
	_, err = ReadToken(b, uid)
	wantClass(t, "a directory", err, ClassFile)
}

// S20: two bundles of one command never share a user ID or a token.
func TestCheckDistinctBundles(t *testing.T) {
	a := Member{Team: "alpha", UserID: testUser}
	b := Member{Team: "beta", UserID: "@bob.1+box.podman:" + testServer}
	must(t, CheckDistinctBundles([]Member{a, b}, []string{"t1", "t2"}))
	wantClass(t, "a shared user", CheckDistinctBundles([]Member{a, a}, []string{"t1", "t2"}), ClassGrammar)
	wantClass(t, "a shared token", CheckDistinctBundles([]Member{a, b}, []string{"t1", "t1"}), ClassGrammar)
}

// S21: a linked team entry points at a real directory of this uid, mode exactly 0700.
func TestCheckBundleLink(t *testing.T) {
	home := t.TempDir()
	uid := os.Getuid()
	target := filepath.Join(home, "seat")
	must(t, os.Mkdir(target, 0o700))
	entry := filepath.Join(home, "alpha")
	must(t, os.Symlink(target, entry))
	must(t, CheckBundleLink(entry, uid))
	plain := filepath.Join(home, "plain")
	must(t, os.Mkdir(plain, 0o750))
	must(t, CheckBundleLink(plain, uid))
	wantClass(t, "another owner", CheckBundleLink(entry, uid+1), ClassFile)
	must(t, os.Chmod(target, 0o750))
	wantClass(t, "a group bit", CheckBundleLink(entry, uid), ClassFile)
	chain := filepath.Join(home, "chain")
	must(t, os.Symlink(entry, chain))
	wantClass(t, "a link to a link", CheckBundleLink(chain, uid), ClassFile)
	dangling := filepath.Join(home, "dangling")
	must(t, os.Symlink(filepath.Join(home, "none"), dangling))
	expectf(t, CheckBundleLink(dangling, uid) != nil, "a dangling link")
	expectf(t, errors.Is(CheckBundleLink(filepath.Join(home, "absent"), uid), fs.ErrNotExist), "an absent entry")
}
