package v1

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// leakMarker is the admin token the fake homeserver accepts: distinctive, so a test can look for
// it in every file and every output.
const leakMarker = "syt_YWRtaW4_ZXhwb3J0LXRva2VuLW5ldmVyLXNob3du_0xV1x"

// fixed is the fake clock's time: the record's stamp is 20261124T101112Z.
var fixed = time.Date(2026, 11, 24, 10, 11, 12, 345_000_000, time.UTC)

const fixedStamp = "20261124T101112Z"

// roomID is a v12 room id of 43 copies of c.
func roomID(c byte) string { return "!" + strings.Repeat(string(c), 43) }

// eventID is an event id numbered n.
func eventID(n int) string {
	s := strconv.Itoa(n)
	return "$" + strings.Repeat("e", 43-len(s)) + s
}

// fakeHS is a homeserver that holds rooms' timelines (chronological) and state, and records
// every request it is sent.
type fakeHS struct {
	mu       sync.Mutex
	user     string
	timeline map[string][]json.RawMessage
	state    map[string]string
	requests []string
	// failAfter, when above zero, fails the messages request after that many succeeded.
	failAfter int
	served    int
	// noEndAtStart answers the page that reaches the room's start with no end token.
	noEndAtStart bool
	// hook, when set, runs before each request is answered.
	hook func(r *http.Request)
	// override, when set, answers every request in place of the homeserver.
	override http.HandlerFunc
	syncs    int
}

// set changes the homeserver's settings under its lock, so a request being served sees them whole.
func (h *fakeHS) set(change func(h *fakeHS)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	change(h)
}

func newFakeHS(user string) *fakeHS {
	return &fakeHS{user: user, timeline: map[string][]json.RawMessage{}, state: map[string]string{}}
}

func (h *fakeHS) addRoom(id string, events []json.RawMessage, state string) {
	h.set(func(h *fakeHS) {
		h.timeline[id] = events
		h.state[id] = state
	})
}

// answer makes every request answered with status and body.
func (h *fakeHS) answer(status int, body string) {
	h.set(func(h *fakeHS) {
		h.override = func(rw http.ResponseWriter, _ *http.Request) { writeJSON(rw, status, body) }
	})
}

func (h *fakeHS) seen() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.requests)
}

func (h *fakeHS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	h.requests = append(h.requests, r.Method+" "+r.URL.Path)
	hook, override := h.hook, h.override
	h.mu.Unlock()
	if hook != nil {
		hook(r)
	}
	if override != nil {
		override(w, r)
		return
	}
	if r.Header.Get("Authorization") != "Bearer "+leakMarker {
		writeJSON(w, http.StatusUnauthorized, `{"errcode":"M_UNKNOWN_TOKEN","error":"Unknown token"}`)
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	path := r.URL.Path
	switch {
	case path == "/_matrix/client/v3/account/whoami":
		writeJSON(w, http.StatusOK, `{"user_id":`+strconv.Quote(h.user)+`}`)
	case path == "/_matrix/client/v3/sync":
		h.syncs++
		writeJSON(w, http.StatusOK, `{"next_batch":"s`+strconv.Itoa(h.syncs)+`","rooms":{}}`)
	case strings.HasPrefix(path, "/_matrix/client/v3/rooms/") && strings.HasSuffix(path, "/messages"):
		h.messages(w, r, strings.TrimSuffix(strings.TrimPrefix(path, "/_matrix/client/v3/rooms/"), "/messages"))
	case strings.HasPrefix(path, "/_matrix/client/v3/rooms/") && strings.HasSuffix(path, "/state"):
		writeJSON(w, http.StatusOK, h.state[strings.TrimSuffix(strings.TrimPrefix(path, "/_matrix/client/v3/rooms/"), "/state")])
	default:
		writeJSON(w, http.StatusNotFound, `{"errcode":"M_UNRECOGNIZED"}`)
	}
}

// messages pages backwards: a sync token starts at the room's end, "p<n>" at index n.
func (h *fakeHS) messages(w http.ResponseWriter, r *http.Request, room string) {
	q := r.URL.Query()
	if q.Get("dir") != "b" || q.Get("limit") != "100" || q.Has("filter") || q.Has("to") {
		writeJSON(w, http.StatusBadRequest, `{"errcode":"M_INVALID_PARAM"}`)
		return
	}
	if h.failAfter > 0 && h.served >= h.failAfter {
		writeJSON(w, http.StatusInternalServerError, `{"errcode":"M_UNKNOWN"}`)
		return
	}
	h.served++
	events := h.timeline[room]
	from := q.Get("from")
	idx := len(events)
	if n, ok := strings.CutPrefix(from, "p"); ok {
		v, err := strconv.Atoi(n)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, `{"errcode":"M_INVALID_PARAM"}`)
			return
		}
		idx = v
	}
	low := max(idx-100, 0)
	chunk := append([]json.RawMessage{}, events[low:idx]...)
	slices.Reverse(chunk)
	page := map[string]any{"chunk": chunk, "start": from}
	if low > 0 || !h.noEndAtStart {
		page["end"] = "p" + strconv.Itoa(low)
	}
	body, err := json.Marshal(page)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, `{}`)
		return
	}
	writeJSON(w, http.StatusOK, string(body))
}

func writeJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write([]byte(body)); err != nil {
		panic(err)
	}
}

// world is a scratch host: a v1 team's tree under root, a fake homeserver on loopback, and the
// deps an export runs on.
type world struct {
	t      *testing.T
	root   string
	team   string
	hs     *fakeHS
	srv    *httptest.Server
	port   int
	deps   Deps
	stderr bytes.Buffer
	// users are the accounts the lookup knows.
	users map[string]Account
}

func newWorld(t *testing.T) *world {
	t.Helper()
	w := &world{t: t, root: t.TempDir(), team: "alpha"}
	w.hs = newFakeHS("@admin:alpha.agent-bus.internal")
	w.srv = httptest.NewServer(w.hs)
	t.Cleanup(w.srv.Close)
	u, err := url.Parse(w.srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if w.port, err = strconv.Atoi(u.Port()); err != nil {
		t.Fatal(err)
	}
	me := Account{UID: os.Getuid(), GID: os.Getgid()}
	w.users = map[string]Account{"agent-bus": me, "owner": me}
	w.deps = Deps{
		Root: w.root, RootUID: os.Getuid(),
		Lookup: func(name string) (Account, bool, error) {
			a, ok := w.users[name]
			return a, ok, nil
		},
		HTTP: matrix.LoopbackClient(RequestLimit),
		Now:  func() time.Time { return fixed },
	}
	w.mkdir(paths.AdminStateRoot, 0o700)
	w.mkdir("/var/lib/agent-bus", 0o700)
	w.mkdir(w.teamDir(), 0o700)
	w.mkdir(w.teamDir()+"/secrets", 0o700)
	w.write(w.teamDir()+"/team.json", fmt.Sprintf(`{"team":"alpha","port":%d,"state":"present"}`, w.port), 0o640)
	w.write(w.teamDir()+"/room_id", roomID('m'), 0o600)
	w.write(w.teamDir()+"/secrets/admin.token", leakMarker, 0o600)
	w.hs.addRoom(roomID('m'), nil, `[]`)
	return w
}

func (w *world) teamDir() string { return "/var/lib/agent-bus/" + w.team }

func (w *world) mkdir(rel string, mode os.FileMode) {
	w.t.Helper()
	if err := os.MkdirAll(w.root+rel, mode); err != nil {
		w.t.Fatal(err)
	}
	if err := os.Chmod(w.root+rel, mode); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) write(rel, content string, mode os.FileMode) {
	w.t.Helper()
	if err := os.WriteFile(w.root+rel, []byte(content), mode); err != nil {
		w.t.Fatal(err)
	}
	if err := os.Chmod(w.root+rel, mode); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) exporter() Exporter { return Exporter{Deps: w.deps} }

func (w *world) export(req Request) (Result, error) {
	w.t.Helper()
	if req.Team == "" {
		req.Team = w.team
	}
	req.Progress = &w.stderr
	return w.exporter().Export(w.t.Context(), req)
}

// recordDir is where the record of the fixed clock lands.
func (w *world) recordDir() string {
	return w.root + paths.AdminStateRoot + "/_v1-export/" + w.team + "/" + fixedStamp
}

// ev is an event's JSON from its fields.
func ev(t *testing.T, fields map[string]any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// msgEvent is an m.room.message numbered n at second n of the fixed day.
func msgEvent(t *testing.T, n int, sender string, content map[string]any) json.RawMessage {
	t.Helper()
	return ev(t, map[string]any{
		"event_id": eventID(n), "type": "m.room.message", "sender": sender,
		"origin_server_ts": fixed.UnixMilli() + int64(n)*1000, "content": content,
	})
}

// textEvents are n plain messages from one human.
func textEvents(t *testing.T, n int) []json.RawMessage {
	t.Helper()
	out := make([]json.RawMessage, 0, n)
	for i := range n {
		out = append(out, msgEvent(t, i, "@human:alpha.agent-bus.internal", map[string]any{"msgtype": "m.text", "body": "line " + strconv.Itoa(i)}))
	}
	return out
}
