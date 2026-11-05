package fakehs

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"testing"
	"time"
)

type web struct {
	t      *testing.T
	tm     *team
	client *http.Client
}

func serve(t *testing.T) *web {
	t.Helper()
	tm := newTeam(t)
	if err := tm.s.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := tm.s.Close(); err != nil {
			t.Error(err)
		}
	})
	client := &http.Client{
		Transport:     &http.Transport{Proxy: nil, DisableKeepAlives: true},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return &web{t: t, tm: tm, client: client}
}

type httpAnswer struct {
	status int
	header http.Header
	body   string
}

func (w *web) do(ctx context.Context, method, path, token, body string) (httpAnswer, error) {
	w.t.Helper()
	req, err := http.NewRequestWithContext(ctx, method, w.tm.s.URL()+path, strings.NewReader(body))
	if err != nil {
		w.t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := w.client.Do(req)
	if err != nil {
		return httpAnswer{}, err
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			w.t.Error(cerr)
		}
	}()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return httpAnswer{}, err
	}
	return httpAnswer{resp.StatusCode, resp.Header, string(raw)}, nil
}

func (w *web) must(method, path, token, body string) httpAnswer {
	w.t.Helper()
	got, err := w.do(context.Background(), method, path, token, body)
	if err != nil {
		w.t.Fatal(err)
	}
	return got
}

func TestServesOverHTTPAndRecordsEveryRequest(t *testing.T) {
	w := serve(t)
	agent := w.tm.tok[w.tm.agent]
	got := w.must("GET", "/_matrix/client/v3/account/whoami?x=1", agent, "")
	if got.status != 200 || !strings.Contains(got.body, w.tm.agent) || got.header.Get("Content-Type") != "application/json" {
		t.Fatalf("whoami %v", got)
	}
	w.must("PUT", "/_matrix/client/v3/rooms/"+q(w.tm.room)+"/send/m.room.message/h1", agent, `{"body":"secret words"}`)
	reqs := w.tm.s.Requests()
	if len(reqs) != 2 || reqs[0].Method != http.MethodGet || reqs[0].Query.Get("x") != "1" ||
		reqs[0].Header.Get("Authorization") != "Bearer "+agent {
		t.Fatalf("recorded %+v", reqs)
	}
	if reqs[1].Path != "/_matrix/client/v3/rooms/"+q(w.tm.room)+"/send/m.room.message/h1" || !strings.Contains(reqs[1].Path, "%21") {
		t.Fatalf("the path as on the wire was not kept: %q", reqs[1].Path)
	}
}

func TestAFaultCarriesItsHeaders(t *testing.T) {
	w := serve(t)
	w.tm.s.Inject(Fault{
		Method: "GET", Path: "whoami", Status: 429, Header: http.Header{"Retry-After": {"7"}},
		Body: []byte(`{"errcode":"M_LIMIT_EXCEEDED","error":"x"}`), Times: 2,
	})
	for range 2 {
		if got := w.must("GET", "/_matrix/client/v3/account/whoami", w.tm.tok[w.tm.agent], ""); got.status != 429 || got.header.Get("Retry-After") != "7" {
			t.Fatalf("fault %v", got)
		}
	}
	if got := w.must("GET", "/_matrix/client/v3/account/whoami", w.tm.tok[w.tm.agent], ""); got.status != 200 {
		t.Fatalf("after the fault %v", got)
	}
}

func (w *web) sendPath(txn string) string {
	return "/_matrix/client/v3/rooms/" + q(w.tm.room) + "/send/m.room.message/" + txn
}

func (w *web) messageCount() int {
	w.t.Helper()
	page := w.tm.ok(w.tm.call(w.tm.agent, "GET", w.tm.roomPath("messages"), url.Values{
		"dir": {"b"}, "filter": {`{"types":["m.room.message"]}`},
	}, nil))
	return len(list(w.t, page["chunk"]))
}

func TestADroppedConnectionLosesTheRequestOrTheAnswer(t *testing.T) {
	w := serve(t)
	agent := w.tm.tok[w.tm.agent]
	w.tm.s.Inject(Fault{Method: "PUT", Path: "/send/", Drop: true})
	if _, err := w.do(context.Background(), "PUT", w.sendPath("d1"), agent, `{"body":"x"}`); err == nil {
		t.Fatal("a dropped connection answered")
	}
	if n := w.messageCount(); n != 0 {
		t.Fatalf("a request dropped before handling landed %d messages", n)
	}
	w.tm.s.Inject(Fault{Method: "PUT", Path: "/send/", DropAfter: true})
	if _, err := w.do(context.Background(), "PUT", w.sendPath("d2"), agent, `{"body":"x"}`); err == nil {
		t.Fatal("a dropped answer arrived")
	}
	if n := w.messageCount(); n != 1 {
		t.Fatalf("a request whose answer was lost landed %d messages", n)
	}
	w.must("PUT", w.sendPath("d2"), agent, `{"body":"x"}`)
	if n := w.messageCount(); n != 1 {
		t.Fatalf("the retry with the same transaction ID landed %d messages", n)
	}
}

func TestARedirectTargetReceivesNothing(t *testing.T) {
	w := serve(t)
	target := serve(t)
	w.tm.s.Inject(Fault{Method: "GET", Path: "whoami", Redirect: target.tm.s.URL() + "/_matrix/client/v3/account/whoami"})
	got := w.must("GET", "/_matrix/client/v3/account/whoami", w.tm.tok[w.tm.agent], "")
	if got.status != 307 || got.header.Get("Location") == "" {
		t.Fatalf("redirect %v", got)
	}
	if reqs := target.tm.s.Requests(); len(reqs) != 0 {
		t.Fatalf("the redirect target received %+v", reqs)
	}
}

func TestStopRefusesAndRestartKeepsTheState(t *testing.T) {
	w := serve(t)
	agent := w.tm.tok[w.tm.agent]
	before := w.tm.s.URL()
	if err := w.tm.s.Stop(); err != nil {
		t.Fatal(err)
	}
	if _, err := w.do(context.Background(), "GET", "/_matrix/client/v3/account/whoami", agent, ""); !errors.Is(err, syscall.ECONNREFUSED) {
		t.Fatalf("a stopped server answered: %v", err)
	}
	if err := w.tm.s.Restart(); err != nil {
		t.Fatal(err)
	}
	if w.tm.s.URL() != before {
		t.Fatalf("restarted at %s, was %s", w.tm.s.URL(), before)
	}
	if got := w.must("GET", "/_matrix/client/v3/account/whoami", agent, ""); got.status != 200 {
		t.Fatalf("after a restart %v", got)
	}
	if err := w.tm.s.Start(); err == nil {
		t.Fatal("a second Start on a serving server succeeded")
	}
}

func TestRestartEndsAHeldSync(t *testing.T) {
	w := serve(t)
	agent := w.tm.tok[w.tm.agent]
	since := text(t, w.tm.sync(w.tm.agent, "", "", 0)["next_batch"])
	done := make(chan error, 1)
	go func() {
		_, err := w.do(context.Background(), "GET", "/_matrix/client/v3/sync?timeout=30000&since="+since, agent, "")
		done <- err
	}()
	for len(w.tm.s.Requests()) == 0 {
		time.Sleep(time.Millisecond)
	}
	if err := w.tm.s.Restart(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a held sync survived a restart with an answer")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("a restart left a held sync running")
	}
}

func TestOversizedBodiesOverHTTP(t *testing.T) {
	w := serve(t)
	w.tm.s.SetQuirks(Quirks{MaxRequestBytes: 8})
	got := w.must("PUT", w.sendPath("big"), w.tm.tok[w.tm.agent], `{"body":"far too long"}`)
	if got.status != 413 || !strings.Contains(got.body, "M_TOO_LARGE") {
		t.Fatalf("oversized %v", got)
	}
	w.tm.s.SetQuirks(Quirks{})
	huge := bytes.Repeat([]byte("x"), maxHTTPBody+1)
	got = w.must("PUT", w.sendPath("huge"), w.tm.tok[w.tm.agent], string(huge))
	if got.status != 413 {
		t.Fatalf("a body past the transport cap answered %v", got)
	}
}
