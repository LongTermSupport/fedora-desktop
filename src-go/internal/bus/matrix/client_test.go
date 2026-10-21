package matrix

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/bus/matrix/fakehs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const (
	serverName = "example.org"
	loopback   = "127.0.0.1"
	whoamiPath = "/account/whoami"
)

func fixedNow() time.Time { return time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC) }

// fakeClock records the waits it is asked for and returns at once.
type fakeClock struct {
	mu     sync.Mutex
	sleeps []time.Duration
	cancel context.CancelFunc
}

func (c *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	c.mu.Lock()
	c.sleeps = append(c.sleeps, d)
	cancel := c.cancel
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return ctx.Err()
}

func (c *fakeClock) waits() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Duration(nil), c.sleeps...)
}

// member is a member client with the clock it waits on and the user it is signed in as.
type member struct {
	*Client

	clk *fakeClock
	uid string
}

func newServer(t *testing.T) *fakehs.Server {
	t.Helper()
	srv := fakehs.New(fakehs.Options{ServerName: serverName, Now: fixedNow, Quirks: fakehs.TuwunelQuirks()})
	if err := srv.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() {
		if err := srv.CheckModelled(); err != nil {
			t.Errorf("fakehs: %v", err)
		}
		if err := srv.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	return srv
}

func newMember(t *testing.T, base, token string) member {
	t.Helper()
	clk := &fakeClock{}
	c, err := NewMember(Config{BaseURL: base, PlainHTTPHosts: []string{loopback}, Token: token, Clock: clk})
	if err != nil {
		t.Fatalf("NewMember: %v", err)
	}
	t.Cleanup(c.Close)
	return member{Client: c, clk: clk}
}

// user seeds an account and gives a member client signed in as it.
func user(t *testing.T, srv *fakehs.Server, localpart string) member {
	t.Helper()
	uid := srv.AddUser(localpart, "secret-"+localpart, false)
	token, err := srv.MintToken(uid)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	m := newMember(t, srv.URL(), token)
	m.uid = uid
	return m
}

// wantErr checks err is a *matrix.Error of the kind, with the detail when one is given, and
// gives a copy of it (a value, which is not an error, so nothing need consume it).
func wantErr(t *testing.T, err error, kind error, detail string) Error {
	t.Helper()
	e, ok := errors.AsType[*Error](err)
	if !ok {
		t.Fatalf("err = %v, want a *matrix.Error", err)
	}
	if !errors.Is(err, kind) {
		t.Fatalf("err = %v, want kind %v", err, kind)
	}
	if detail != "" && e.Detail != detail {
		t.Fatalf("detail = %q, want %q", e.Detail, detail)
	}
	return *e
}

func lastRequest(t *testing.T, srv *fakehs.Server) fakehs.Request {
	t.Helper()
	reqs := srv.Requests()
	if len(reqs) == 0 {
		t.Fatal("no request reached the server")
	}
	return reqs[len(reqs)-1]
}

func inject(srv *fakehs.Server, method, path string, status int, body string) {
	srv.Inject(fakehs.Fault{Method: method, Path: path, Status: status, Body: []byte(body)})
}

// blockingServer answers nothing until the test ends, and reports each request it holds.
func blockingServer(t *testing.T) (*httptest.Server, <-chan struct{}) {
	t.Helper()
	release := make(chan struct{})
	arrived := make(chan struct{}, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		arrived <- struct{}{}
		<-release
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })
	return srv, arrived
}

func TestTokenGrammar(t *testing.T) {
	srv := newServer(t)
	for _, tok := range []string{"", "a b", "tok\n", "tok\x7f", "café", strings.Repeat("t", 4097)} {
		if _, err := NewMember(Config{BaseURL: srv.URL(), PlainHTTPHosts: []string{loopback}, Token: tok}); err == nil {
			t.Errorf("token %q accepted", tok)
		}
	}
	if _, err := NewAdmin(Config{BaseURL: srv.URL(), Token: "a b"}); err == nil {
		t.Error("admin token with a space accepted")
	}
	m := newMember(t, srv.URL(), strings.Repeat("t", 4096))
	if _, err := m.WithToken("bad token"); err == nil {
		t.Error("WithToken accepted a bad token")
	}
}

func TestMemberBaseURL(t *testing.T) {
	plain := []string{loopback, "::1", "198.51.100.1"}
	good := map[string]string{
		"http://127.0.0.1:8008":      "http://127.0.0.1:8008",
		"http://[::1]:8008":          "http://[::1]:8008",
		"https://Matrix.Example.ORG": "https://matrix.example.org",
		"https://192.0.2.7:8448":     "https://192.0.2.7:8448",
		"HTTPS://example.org":        "https://example.org",
	}
	for raw, want := range good {
		got, err := memberBase(raw, plain)
		if err != nil || got != want {
			t.Errorf("memberBase(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}
	bad := []string{
		"", "ftp://example.org", "http://example.org", "http://192.0.2.7", "https://example.org/",
		"https://example.org/_matrix", "https://example.org?x=1", "https://example.org#f",
		"https://u:p@example.org", "https://example.org:0", "https://example.org:65536",
		"https://example.org:080", "https://example.org:", "https://", "https://exa mple.org",
		"https://[fe80::1%25eth0]", "http://[::ffff:127.0.0.1]:80", "https://-bad.example.org",
		"https://" + strings.Repeat("a.", 127) + "org", "https://ex_ample.org", "https://example.org?",
		"https://éxample.org", "https://[::1", "mailto:someone",
	}
	for _, raw := range bad {
		if got, err := memberBase(raw, plain); err == nil {
			t.Errorf("memberBase(%q) = %q, want a refusal", raw, got)
		}
	}
}

func TestAdminBaseURL(t *testing.T) {
	for _, raw := range []string{"http://127.0.0.1:1", "http://127.0.0.1:65535", "http://127.0.0.1:8008"} {
		if got, err := adminBase(raw); err != nil || got != raw {
			t.Errorf("adminBase(%q) = %q, %v", raw, got, err)
		}
	}
	bad := []string{
		"http://127.0.0.1", "http://127.0.0.1:0", "http://127.0.0.1:080", "http://127.0.0.1:65536",
		"http://127.0.0.1:8008/", "https://127.0.0.1:8008", "http://localhost:8008", "http://127.0.0.2:8008",
		"http://127.0.0.1:99999999999999999999",
	}
	for _, raw := range bad {
		if _, err := adminBase(raw); err == nil {
			t.Errorf("adminBase(%q) accepted", raw)
		}
	}
	if _, err := NewAdmin(Config{BaseURL: "http://127.0.0.1:8008", PlainHTTPHosts: []string{loopback}}); err == nil {
		t.Error("admin config with PlainHTTPHosts accepted")
	}
	if _, err := NewAdmin(Config{BaseURL: "http://127.0.0.1:0"}); err == nil {
		t.Error("admin port 0 accepted")
	}
	if _, err := NewMember(Config{BaseURL: "http://127.0.0.1:8008", Token: "t"}); err == nil {
		t.Error("plain http member without PlainHTTPHosts accepted")
	}
}

func TestRequestHeaders(t *testing.T) {
	srv := newServer(t)
	m := user(t, srv, "alice")
	if _, err := m.Versions(t.Context()); err != nil {
		t.Fatalf("versions: %v", err)
	}
	if got := lastRequest(t, srv).Header.Get("Authorization"); got != "" {
		t.Errorf("versions sent Authorization %q", got)
	}
	who, err := m.Whoami(t.Context())
	if err != nil || who.UserID != m.uid || who.DeviceID == "" {
		t.Fatalf("whoami = %+v, %v", who, err)
	}
	req := lastRequest(t, srv)
	if !strings.HasPrefix(req.Header.Get("Authorization"), "Bearer ") || len(req.Query) != 0 {
		t.Errorf("whoami header %q query %v", req.Header.Get("Authorization"), req.Query)
	}
	for key, want := range map[string]string{"Accept": "application/json", "User-Agent": "pingbus", "Accept-Encoding": "", "Content-Type": ""} {
		if got := req.Header.Get(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if _, err := m.CreateRoom(t.Context(), CreateRoomRequest{Preset: "private_chat"}); err != nil {
		t.Fatalf("createRoom: %v", err)
	}
	if got := lastRequest(t, srv).Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
}

func TestUserAgentOverride(t *testing.T) {
	srv := newServer(t)
	c, err := NewMember(Config{BaseURL: srv.URL(), PlainHTTPHosts: []string{loopback}, Token: "t", UserAgent: "bus-member/2"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Versions(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := lastRequest(t, srv).Header.Get("User-Agent"); got != "bus-member/2" {
		t.Errorf("User-Agent = %q", got)
	}
	if _, err := NewMember(Config{BaseURL: srv.URL(), PlainHTTPHosts: []string{loopback}, Token: "t", UserAgent: "bad\nagent"}); err == nil {
		t.Error("a User-Agent with a newline accepted")
	}
}

func TestProxyIgnored(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://192.0.2.1:9")
	t.Setenv("http_proxy", "http://192.0.2.1:9")
	srv := newServer(t)
	m := user(t, srv, "alice")
	if _, err := m.Whoami(t.Context()); err != nil {
		t.Fatalf("whoami through the environment's proxy: %v", err)
	}
}

func TestRedirectRefused(t *testing.T) {
	srv := newServer(t)
	hits := 0
	var mu sync.Mutex
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
	}))
	defer target.Close()
	m := user(t, srv, "alice")
	for _, status := range []int{http.StatusTemporaryRedirect, http.StatusFound, http.StatusPermanentRedirect} {
		srv.Inject(fakehs.Fault{Method: http.MethodGet, Path: whoamiPath, Redirect: target.URL + "/steal", Status: status})
		_, err := m.Whoami(t.Context())
		e := wantErr(t, err, ErrRefused, "redirect refused")
		if e.Status != status || e.Transient() {
			t.Errorf("status %d transient %v", e.Status, e.Transient())
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if hits != 0 {
		t.Errorf("the redirect target saw %d requests", hits)
	}
}

func TestTokenNeverPrinted(t *testing.T) {
	srv := newServer(t)
	m := user(t, srv, "alice")
	token, err := srv.MintToken(m.uid)
	if err != nil {
		t.Fatal(err)
	}
	c, err := m.WithToken(token)
	if err != nil {
		t.Fatal(err)
	}
	inject(srv, http.MethodGet, whoamiPath, http.StatusUnauthorized, `{"errcode":"M_UNKNOWN_TOKEN","error":"`+token+`"}`)
	_, callErr := c.Whoami(t.Context())
	sess := Session{UserID: m.uid, AccessToken: Token(token), DeviceID: "D"}
	for _, v := range []any{c, *c, callErr, sess, &sess, sess.AccessToken} {
		for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x"} {
			if out := fmt.Sprintf(verb, v); strings.Contains(out, token) || strings.Contains(out, fmt.Sprintf("%x", token)) {
				t.Errorf("%s of %T shows the token: %s", verb, v, out)
			}
		}
	}
	if sess.AccessToken.Reveal() != token {
		t.Error("Reveal does not give the token")
	}
	if _, err := sess.AccessToken.MarshalText(); err == nil {
		t.Error("a token marshals")
	}
	var wire struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal([]byte(`{"access_token":"t"}`), &wire); err != nil || wire.AccessToken != "t" {
		t.Fatal(err)
	}
}

func TestStatusClasses(t *testing.T) {
	cases := []struct {
		status    int
		body      string
		kind      error
		exit      term.ExitCode
		transient bool
		errcode   string
	}{
		{http.StatusUnauthorized, `{"errcode":"M_UNKNOWN_TOKEN","error":"server words"}`, ErrAuthRefused, 8, false, "M_UNKNOWN_TOKEN"},
		{http.StatusForbidden, `{"errcode":"M_FORBIDDEN"}`, ErrForbidden, 10, false, "M_FORBIDDEN"},
		{http.StatusNotFound, `{"errcode":"m_lower"}`, ErrNotFound, 7, false, ""},
		{http.StatusInternalServerError, `nope`, ErrRefused, 7, true, ""},
		{http.StatusBadGateway, `{"errcode":"M_` + strings.Repeat("X", 65) + `"}`, ErrRefused, 7, true, ""},
		{http.StatusBadRequest, `{"errcode":"M_BAD_JSON"}`, ErrRefused, 7, false, "M_BAD_JSON"},
		{http.StatusTeapot, `[]`, ErrRefused, 7, false, ""},
		{http.StatusCreated, `{}`, ErrRefused, 7, false, ""},
		{http.StatusNoContent, ``, ErrRefused, 7, false, ""},
	}
	srv := newServer(t)
	m := user(t, srv, "alice")
	for _, tc := range cases {
		inject(srv, http.MethodGet, whoamiPath, tc.status, tc.body)
		_, err := m.Whoami(t.Context())
		e := wantErr(t, err, tc.kind, "")
		if e.Status != tc.status || e.ExitCode() != tc.exit || e.Transient() != tc.transient || e.Errcode != tc.errcode {
			t.Errorf("%d: %+v exit %d transient %v", tc.status, e, e.ExitCode(), e.Transient())
		}
		if e.Method != http.MethodGet || e.Path != "/_matrix/client/v3/account/whoami" {
			t.Errorf("%d: method %q path %q", tc.status, e.Method, e.Path)
		}
		if strings.Contains(e.Error(), "server words") {
			t.Errorf("the server's error text reached the message: %s", e.Error())
		}
	}
}

func TestExitCodes(t *testing.T) {
	for kind, want := range map[error]term.ExitCode{
		ErrAuthRefused: 8, ErrForbidden: 10, ErrNotFound: 7, ErrRateLimited: 9,
		ErrUnreachable: 7, ErrRefused: 7, ErrInvalid: 7,
	} {
		e := &Error{kind: kind}
		if got := e.ExitCode(); got != want {
			t.Errorf("%v: exit %d, want %d", kind, got, want)
		}
		if kind.Error() == "" {
			t.Errorf("kind %#v has no text", kind)
		}
	}
	e := &Error{Method: http.MethodGet, Path: "/p", Status: 404, Errcode: "M_NOT_FOUND", Detail: "not found", kind: ErrNotFound}
	if got := e.Error(); got != "matrix: GET /p: 404 M_NOT_FOUND: not found" {
		t.Errorf("Error() = %q", got)
	}
	e = &Error{Method: http.MethodGet, Path: "/p", Detail: "no answer", kind: ErrUnreachable}
	if got := e.Error(); got != "matrix: GET /p: no answer" {
		t.Errorf("Error() = %q", got)
	}
	if got := (&Error{Detail: "bad", kind: ErrInvalid}).Error(); got != "matrix: bad" {
		t.Errorf("Error() = %q", got)
	}
	if got := (&Error{}).ExitCode(); got != 7 {
		t.Errorf("a zero Error exits %d", got)
	}
}

func TestAnswerShape(t *testing.T) {
	srv := newServer(t)
	m := user(t, srv, "alice")
	for _, body := range []string{`[]`, `nope`, ``, `{"user_id":`, `"x"`, `{"user_id":5}`, `{} {}`, `null`} {
		inject(srv, http.MethodGet, whoamiPath, http.StatusOK, body)
		_, err := m.Whoami(t.Context())
		if e := wantErr(t, err, ErrUnreachable, ""); e.Transient() {
			t.Errorf("%q: a malformed answer is transient", body)
		}
	}
}

func TestAnswerCap(t *testing.T) {
	srv := newServer(t)
	m := user(t, srv, "alice")
	exact := `{"a":"` + strings.Repeat("x", maxAnswerBytes-8) + `"}`
	inject(srv, http.MethodGet, whoamiPath, http.StatusOK, exact)
	var out map[string]any
	if err := m.object(t.Context(), call{method: http.MethodGet, path: "/_matrix/client/v3/account/whoami"}, &out); err != nil {
		t.Fatalf("an answer of exactly the cap: %v", err)
	}
	inject(srv, http.MethodGet, whoamiPath, http.StatusOK, exact+" ")
	_, err := m.Whoami(t.Context())
	if e := wantErr(t, err, ErrUnreachable, "answer over 33554432 bytes"); e.Transient() {
		t.Error("an answer over the cap is transient")
	}
}

func TestHeaderCap(t *testing.T) {
	big := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Big", strings.Repeat("h", 70<<10))
		w.WriteHeader(http.StatusOK)
	}))
	defer big.Close()
	m := newMember(t, big.URL, "tok")
	_, err := m.Whoami(t.Context())
	wantErr(t, err, ErrUnreachable, "no answer")
}

func TestCutShortAnswer(t *testing.T) {
	short := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(`{"user_id":`)); err != nil {
			return
		}
	}))
	defer short.Close()
	m := newMember(t, short.URL, "tok")
	_, err := m.Whoami(t.Context())
	if e := wantErr(t, err, ErrUnreachable, "answer cut short"); !e.Transient() {
		t.Error("a cut answer is not transient")
	}
}

func TestDroppedConnection(t *testing.T) {
	srv := newServer(t)
	m := user(t, srv, "alice")
	srv.Inject(fakehs.Fault{Method: http.MethodGet, Path: whoamiPath, Drop: true})
	_, err := m.Whoami(t.Context())
	if e := wantErr(t, err, ErrUnreachable, "no answer"); !e.Transient() || e.ExitCode() != 7 {
		t.Errorf("transient %v exit %d", e.Transient(), e.ExitCode())
	}
	if err = srv.Stop(); err != nil {
		t.Fatal(err)
	}
	_, err = m.Whoami(t.Context())
	wantErr(t, err, ErrUnreachable, "no answer")
}

func TestRequestTimeLimit(t *testing.T) {
	slow, arrived := blockingServer(t)
	m := newMember(t, slow.URL, "tok")
	m.limit = 20 * time.Millisecond
	_, err := m.Whoami(t.Context())
	e := wantErr(t, err, ErrUnreachable, "timed out")
	if !e.Transient() || errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("transient %v; the client's own limit is not the caller's deadline", e.Transient())
	}
	<-arrived
	if memberLimit != 15*time.Second || adminLimit != 30*time.Second || syncGrace != 15*time.Second {
		t.Error("the time limits are not 15 s, 30 s and the long-poll plus 15 s")
	}
}

func TestCancelled(t *testing.T) {
	srv := newServer(t)
	m := user(t, srv, "alice")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	before := len(srv.Requests())
	_, err := m.Whoami(ctx)
	wantErr(t, err, ErrUnreachable, "cancelled")
	if !errors.Is(err, context.Canceled) || len(srv.Requests()) != before {
		t.Errorf("err %v; requests %d -> %d", err, before, len(srv.Requests()))
	}

	slow, arrived := blockingServer(t)
	sm := newMember(t, slow.URL, "tok")
	ctx, cancel = context.WithCancel(t.Context())
	go func() {
		<-arrived
		cancel()
	}()
	_, err = sm.Whoami(ctx)
	wantErr(t, err, ErrUnreachable, "cancelled")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("in flight: %v", err)
	}

	ctx, cancel = context.WithTimeout(t.Context(), time.Millisecond)
	defer cancel()
	<-ctx.Done()
	_, err = m.Whoami(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("the caller's deadline: %v", err)
	}
}

func TestRateLimitWaits(t *testing.T) {
	cases := []struct {
		name   string
		header http.Header
		body   string
		want   time.Duration
	}{
		{"retry-after", http.Header{"Retry-After": {" 7 "}}, `{"retry_after_ms":1}`, 7 * time.Second},
		{"body ms", nil, `{"errcode":"M_LIMIT_EXCEEDED","retry_after_ms":1500}`, 1500 * time.Millisecond},
		{"default", nil, `{}`, 5 * time.Second},
		{"bad header", http.Header{"Retry-After": {"Wed, 21 Oct 2026 07:28:00 GMT"}}, `{"retry_after_ms":2000}`, 2 * time.Second},
		{"negative ms", nil, `{"retry_after_ms":-1}`, 5 * time.Second},
		{"string ms", nil, `{"retry_after_ms":"100"}`, 5 * time.Second},
		{"fraction ms", nil, `{"retry_after_ms":1.5}`, 5 * time.Second},
		{"not json", nil, `slow down`, 5 * time.Second},
		{"exactly 60 s", http.Header{"Retry-After": {"60"}}, ``, 60 * time.Second},
		{"zero", http.Header{"Retry-After": {"0"}}, ``, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newServer(t)
			m := user(t, srv, "alice")
			srv.Inject(fakehs.Fault{Method: http.MethodGet, Path: whoamiPath, Status: http.StatusTooManyRequests, Header: tc.header, Body: []byte(tc.body)})
			if _, err := m.Whoami(t.Context()); err != nil {
				t.Fatalf("whoami after one 429: %v", err)
			}
			if got := m.clk.waits(); len(got) != 1 || got[0] != tc.want {
				t.Errorf("waits %v, want [%v]", got, tc.want)
			}
		})
	}
}

func TestRateLimitGivesUp(t *testing.T) {
	srv := newServer(t)
	m := user(t, srv, "alice")
	for _, wait := range []string{"61", "99999999999999999999999"} {
		srv.Inject(fakehs.Fault{Method: http.MethodGet, Path: whoamiPath, Status: http.StatusTooManyRequests, Header: http.Header{"Retry-After": {wait}}})
		before := len(srv.Requests())
		_, err := m.Whoami(t.Context())
		if e := wantErr(t, err, ErrRateLimited, ""); !e.Transient() || e.ExitCode() != 9 {
			t.Errorf("transient %v exit %d", e.Transient(), e.ExitCode())
		}
		if len(m.clk.waits()) != 0 || len(srv.Requests()) != before+1 {
			t.Errorf("a %s s wait was slept or retried", wait)
		}
	}
	inject(srv, http.MethodGet, whoamiPath, http.StatusTooManyRequests, `{"retry_after_ms":99999999999999}`)
	_, err := m.Whoami(t.Context())
	wantErr(t, err, ErrRateLimited, "")
	srv.Inject(fakehs.Fault{Method: http.MethodGet, Path: whoamiPath, Status: http.StatusTooManyRequests, Times: 3})
	before := len(srv.Requests())
	_, err = m.Whoami(t.Context())
	wantErr(t, err, ErrRateLimited, "rate limited")
	if got := len(srv.Requests()) - before; got != 3 || len(m.clk.waits()) != 2 {
		t.Errorf("tries %d waits %v, want 3 tries and 2 waits", got, m.clk.waits())
	}
}

func TestRateLimitWaitCancelled(t *testing.T) {
	srv := newServer(t)
	m := user(t, srv, "alice")
	ctx, cancel := context.WithCancel(t.Context())
	m.clk.cancel = cancel
	srv.Inject(fakehs.Fault{Method: http.MethodGet, Path: whoamiPath, Status: http.StatusTooManyRequests, Times: 3})
	before := len(srv.Requests())
	_, err := m.Whoami(ctx)
	wantErr(t, err, ErrUnreachable, "cancelled")
	if !errors.Is(err, context.Canceled) || len(srv.Requests()) != before+1 {
		t.Errorf("err %v, %d requests", err, len(srv.Requests())-before)
	}
}

func TestSystemClock(t *testing.T) {
	if err := SystemClock().Sleep(t.Context(), time.Millisecond); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := SystemClock().Sleep(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled wait: %v", err)
	}
}

func TestPathSegments(t *testing.T) {
	if got := escape("@a:b/c d~e_f.g-h%"); got != "%40a%3Ab%2Fc%20d~e_f.g-h%25" {
		t.Errorf("escape = %q", got)
	}
	srv := newServer(t)
	m := user(t, srv, "alice")
	before := len(srv.Requests())
	for _, room := range []string{"", "!a\x00b", "!café", "!" + strings.Repeat("a", 255)} {
		_, err := m.GetState(t.Context(), room, "m.room.name", "")
		wantErr(t, err, ErrInvalid, "")
	}
	_, err := m.GetState(t.Context(), "!room", "", "")
	wantErr(t, err, ErrInvalid, "")
	if len(srv.Requests()) != before {
		t.Error("a refused segment reached the server")
	}
	_, err = m.GetState(t.Context(), "!nope:example.org", "m.room.member", "@a:b/c")
	wantErr(t, err, ErrNotFound, "")
	if got := lastRequest(t, srv).Path; got != "/_matrix/client/v3/rooms/%21nope%3Aexample.org/state/m.room.member/%40a%3Ab%2Fc" {
		t.Errorf("wire path %q", got)
	}
}

func TestNoTokenRefused(t *testing.T) {
	srv := newServer(t)
	a, err := NewAdmin(Config{BaseURL: srv.URL(), Clock: &fakeClock{}})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	before := len(srv.Requests())
	_, err = a.Whoami(t.Context())
	wantErr(t, err, ErrInvalid, "no access token")
	if len(srv.Requests()) != before {
		t.Error("a call needing a token was sent without one")
	}
}

func TestCloseLeavesNoGoroutine(t *testing.T) {
	srv := newServer(t)
	settle := func(limit int) int {
		n := runtime.NumGoroutine()
		for i := 0; i < 400 && n > limit; i++ {
			time.Sleep(5 * time.Millisecond)
			n = runtime.NumGoroutine()
		}
		return n
	}
	before := settle(0)
	uid := srv.AddUser("alice", "pw", false)
	token, err := srv.MintToken(uid)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewMember(Config{BaseURL: srv.URL(), PlainHTTPHosts: []string{loopback}, Token: token})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Whoami(t.Context()); err != nil {
		t.Fatal(err)
	}
	c.Close()
	if after := settle(before); after > before {
		t.Errorf("%d goroutines before, %d after Close", before, after)
	}
}
