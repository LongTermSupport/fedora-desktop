package v1

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func (w *world) client() client {
	return client{http: w.deps.HTTP, port: w.port, token: leakMarker}
}

const alphaServer = "alpha.agent-bus.internal"

// E9: whoami must answer @admin:<server_name>.
func TestE9Whoami(t *testing.T) {
	w := newWorld(t)
	if err := w.client().whoami(t.Context(), alphaServer); err != nil {
		t.Fatal(err)
	}
	err := w.client().whoami(t.Context(), "beta.agent-bus.internal")
	if err == nil || !strings.Contains(err.Error(), `"@admin:alpha.agent-bus.internal"`) {
		t.Fatalf("another user: %v", err)
	}
	c := w.client()
	c.token = "wrong"
	err = c.whoami(t.Context(), alphaServer)
	if err == nil || !strings.Contains(err.Error(), "the admin token was rejected") {
		t.Fatalf("a 401: %v", err)
	}
	w.hs.answer(http.StatusForbidden, `{"errcode":"M_FORBIDDEN"}`)
	err = w.client().whoami(t.Context(), alphaServer)
	if err == nil || !strings.Contains(err.Error(), "the admin token was rejected") {
		t.Fatalf("a 403: %v", err)
	}
}

func TestWhoamiOtherFailures(t *testing.T) {
	for name, c := range map[string]struct {
		status int
		body   string
		want   string
	}{
		"500":       {http.StatusInternalServerError, `{"errcode":"M_UNKNOWN"}`, "HTTP 500 (M_UNKNOWN)"},
		"odd code":  {http.StatusInternalServerError, `{"errcode":"bad code\n` + leakMarker + `"}`, "HTTP 500"},
		"not json":  {http.StatusOK, `nope`, "malformed"},
		"no userid": {http.StatusOK, `{}`, "malformed"},
	} {
		w := newWorld(t)
		w.hs.answer(c.status, c.body)
		err := w.client().whoami(t.Context(), alphaServer)
		if err == nil || !strings.Contains(err.Error(), c.want) || strings.Contains(err.Error(), leakMarker) || strings.Contains(err.Error(), "\n") {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// E7, E8: no answer is a noAnswerError.
func TestE8NoAnswer(t *testing.T) {
	w := newWorld(t)
	w.srv.Close()
	err := w.client().whoami(t.Context(), alphaServer)
	if e, ok := errors.AsType[*noAnswerError](err); !ok || e == nil {
		t.Fatalf("%T %v", err, err)
	}
}

// E7: a redirect is not followed.
func TestE7NoRedirect(t *testing.T) {
	w := newWorld(t)
	w.hs.set(func(h *fakeHS) {
		h.override = func(rw http.ResponseWriter, r *http.Request) { http.Redirect(rw, r, "/elsewhere", http.StatusFound) }
	})
	err := w.client().whoami(t.Context(), alphaServer)
	if err == nil || !strings.Contains(err.Error(), "HTTP 302") {
		t.Fatalf("%v", err)
	}
	if seen := w.hs.seen(); !slices.Equal(seen, []string{"GET /_matrix/client/v3/account/whoami"}) {
		t.Fatalf("followed: %v", seen)
	}
}

// E10: the starting token comes from a sync with a filter that gives no timeline.
func TestE10SyncFilter(t *testing.T) {
	w := newWorld(t)
	filters := make(chan string, 1)
	w.hs.set(func(h *fakeHS) {
		h.hook = func(r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/sync") {
				filters <- r.URL.Query().Get("filter")
			}
		}
	})
	tok, err := w.client().startToken(t.Context(), roomID('m'))
	if err != nil || tok != "s1" {
		t.Fatalf("%q %v", tok, err)
	}
	filter := <-filters
	var f struct {
		Room struct {
			Rooms    []string `json:"rooms"`
			Timeline struct {
				Limit *int `json:"limit"`
			} `json:"timeline"`
		} `json:"room"`
	}
	if err := json.Unmarshal([]byte(filter), &f); err != nil {
		t.Fatal(err)
	}
	if f.Room.Timeline.Limit == nil || *f.Room.Timeline.Limit != 0 || !slices.Equal(f.Room.Rooms, []string{roomID('m')}) {
		t.Errorf("filter %s", filter)
	}
}

func TestSyncMalformed(t *testing.T) {
	for _, body := range []string{`{}`, `{"next_batch":""}`, `{"next_batch":"a b"}`, `[]`} {
		w := newWorld(t)
		w.hs.answer(http.StatusOK, body)
		if _, err := w.client().startToken(t.Context(), roomID('m')); err == nil {
			t.Errorf("%s accepted", body)
		}
	}
}

// E10: paging an empty room, one page, many pages, in chronological order, with no filter.
func TestE10Paging(t *testing.T) {
	for _, n := range []int{0, 1, 99, 100, 101, 250, 1234} {
		for _, noEnd := range []bool{false, true} {
			checkPaging(t, n, noEnd)
		}
	}
}

func checkPaging(t *testing.T, n int, noEnd bool) {
	t.Helper()
	w := newWorld(t)
	w.hs.set(func(h *fakeHS) { h.noEndAtStart = noEnd })
	want := textEvents(t, n)
	w.hs.addRoom(roomID('m'), want, `[]`)
	got, err := w.client().history(t.Context(), roomID('m'), "s1")
	if err != nil {
		t.Fatalf("%d: %v", n, err)
	}
	if len(got) != n {
		t.Fatalf("%d events: got %d", n, len(got))
	}
	for i := range got {
		if string(got[i]) != string(want[i]) {
			t.Fatalf("%d events: event %d is %s, want %s", n, i, got[i], want[i])
		}
	}
	pages := 0
	for _, r := range w.hs.seen() {
		if strings.HasSuffix(r, "/messages") {
			pages++
		}
	}
	// Each full or part page, then the empty page after the start; with no end on the page that
	// reaches the start, no page after it.
	wantPages := (n+99)/100 + 1
	if noEnd {
		wantPages = max((n+99)/100, 1)
	}
	if pages != wantPages {
		t.Errorf("%d events (no end %v): %d pages, want %d", n, noEnd, pages, wantPages)
	}
}

// E10: an end equal to the token asked from ends the paging.
func TestE10EndEqualsFrom(t *testing.T) {
	w := newWorld(t)
	w.hs.answer(http.StatusOK, `{"chunk":[{"type":"x"}],"end":"s1"}`)
	got, err := w.client().history(t.Context(), roomID('m'), "s1")
	if err != nil || len(got) != 1 || len(w.hs.seen()) != 1 {
		t.Fatalf("%s %v %v", got, err, w.hs.seen())
	}
}

// E10: a failure part-way fails the room.
func TestE10FailurePartWay(t *testing.T) {
	w := newWorld(t)
	w.hs.addRoom(roomID('m'), textEvents(t, 350), `[]`)
	w.hs.set(func(h *fakeHS) { h.failAfter = 2 })
	if _, err := w.client().history(t.Context(), roomID('m'), "s1"); err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("%v", err)
	}
}

// E10: a page that is not a chunk of objects is refused.
func TestMessagesMalformed(t *testing.T) {
	for _, body := range []string{`{}`, `{"chunk":[1]}`, `{"chunk":"x"}`, `nope`, `{"chunk":[{}],"end":"a\u0001"}`} {
		w := newWorld(t)
		w.hs.answer(http.StatusOK, body)
		if _, err := w.client().history(t.Context(), roomID('m'), "s1"); err == nil {
			t.Errorf("%s accepted", body)
		}
	}
}

// E11: the state as the server sent it.
func TestE11State(t *testing.T) {
	w := newWorld(t)
	state := `[ {"type":"m.room.create","state_key":"","content":{"room_version":"12"}} ]`
	w.hs.addRoom(roomID('m'), nil, state)
	got, err := w.client().state(t.Context(), roomID('m'))
	if err != nil || string(got) != state {
		t.Fatalf("%s %v", got, err)
	}
	for _, bad := range []string{`{}`, `[1]`, `nope`} {
		w.hs.addRoom(roomID('m'), nil, bad)
		if _, err := w.client().state(t.Context(), roomID('m')); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}

// E7: an answer over the cap is refused, not read whole.
func TestResponseCap(t *testing.T) {
	w := newWorld(t)
	c := w.client()
	c.limit = 10
	if err := c.whoami(t.Context(), alphaServer); err == nil || !strings.Contains(err.Error(), "more than 10 bytes") {
		t.Fatalf("%v", err)
	}
}

// E7: the request goes to 127.0.0.1 on the team's port, as a GET, with the token only in the
// Authorization header.
func TestE7RequestShape(t *testing.T) {
	w := newWorld(t)
	type shape struct{ method, host, query, auth string }
	got := make(chan shape, 1)
	w.hs.set(func(h *fakeHS) {
		h.hook = func(r *http.Request) { got <- shape{r.Method, r.Host, r.URL.RawQuery, r.Header.Get("Authorization")} }
	})
	if err := w.client().whoami(t.Context(), alphaServer); err != nil {
		t.Fatal(err)
	}
	s := <-got
	if s.method != http.MethodGet || s.host != "127.0.0.1:"+strconv.Itoa(w.port) || strings.Contains(s.query, leakMarker) || s.auth != "Bearer "+leakMarker {
		t.Errorf("%+v", s)
	}
}
