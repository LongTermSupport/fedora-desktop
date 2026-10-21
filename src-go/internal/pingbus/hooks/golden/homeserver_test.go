package golden

import (
	"bytes"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// The fake homeserver the goodbye talks to: what syncer.verify_room reads (the create event,
// the team record event, the power levels; the topic and the rooms index are absent) and the
// send. It lives here until src-go/internal/bus/matrix/fakehs, which replaces it at
// integration, is on this branch.

const (
	clientPrefix = "/_matrix/client/v3"
	roomVersion  = "12"
	humanPower   = 50
	// slowFor is past the goodbye's 4 s bound.
	slowFor   = 6 * time.Second
	sentEvent = "$0000000000000000000000000000000000000000001"
	teamEvent = "$0000000000000000000000000000000000000000002"
	txnMark   = "{txn}"
	// wrongTeam is the team name a "wrong-team" record carries.
	wrongTeam = "other-team"
)

// Failures a scenario or step can switch on: every request answers it.
const (
	failNone      = ""
	fail500       = "500"
	fail403       = "403"
	failSlow      = "slow"
	failWrongTeam = "wrong-team"
)

// roomConf is one team room the homeserver serves: its admin and its team record content.
type roomConf struct {
	Admin string          `json:"admin"`
	Team  json.RawMessage `json:"team"`
}

// request is one received request, as a step's requests.json records it. The body is the
// bytes sent, as text: the hooks send Python's json.dumps, separators and key order included.
type request struct {
	Method      string            `json:"method"`
	Path        string            `json:"path"`
	Query       map[string]string `json:"query"`
	Auth        string            `json:"auth"`
	ContentType string            `json:"content_type"`
	Body        string            `json:"body"`
}

// roomOf is the room segment of a request's path ("" outside the rooms API).
func roomOf(path string) string {
	rest, ok := strings.CutPrefix(path, clientPrefix+"/rooms/")
	if !ok {
		return ""
	}
	return strings.SplitN(rest, "/", 2)[0]
}

type homeserver struct {
	url   string
	rooms map[string]roomConf
	stop  chan struct{}

	mu       sync.Mutex
	fail     string
	received []request
}

var sendPathRE = regexp.MustCompile(`^(` + regexp.QuoteMeta(clientPrefix) + `/rooms/[^/]+/send/m\.room\.message/)([A-Za-z0-9._~-]{1,64})$`)

func startHomeserver(t *testing.T, rooms map[string]roomConf, fail string) *homeserver {
	t.Helper()
	if !knownFailure(fail) {
		t.Fatalf("unknown homeserver failure %q", fail)
	}
	hs := &homeserver{rooms: rooms, stop: make(chan struct{}), fail: fail}
	srv := httptest.NewServer(http.HandlerFunc(hs.serve))
	hs.url = srv.URL
	t.Cleanup(func() {
		close(hs.stop)
		srv.Close()
	})
	return hs
}

func knownFailure(fail string) bool {
	return slices.Contains([]string{failNone, fail500, fail403, failSlow, failWrongTeam}, fail)
}

func (hs *homeserver) setFail(fail string) {
	hs.mu.Lock()
	defer hs.mu.Unlock()
	hs.fail = fail
}

// drain is the requests received since the last drain, normalised, grouped by room in the
// order of each room's first request and in arrival order within a room: goodbyes to several
// teams run in parallel, so only the order per team is the hooks' (spec H94).
func (hs *homeserver) drain(normalise func([]byte) []byte) []request {
	hs.mu.Lock()
	defer hs.mu.Unlock()
	out := hs.received
	hs.received = nil
	for i := range out {
		out[i].Auth = string(normalise([]byte(out[i].Auth)))
		out[i].Body = string(normalise([]byte(out[i].Body)))
	}
	if out == nil {
		return []request{}
	}
	rooms := []string{}
	for _, r := range out {
		if room := roomOf(r.Path); !slices.Contains(rooms, room) {
			rooms = append(rooms, room)
		}
	}
	slices.Sort(rooms)
	slices.SortStableFunc(out, func(a, b request) int {
		return slices.Index(rooms, roomOf(a.Path)) - slices.Index(rooms, roomOf(b.Path))
	})
	return out
}

func (hs *homeserver) record(r *http.Request) string {
	raw, err := io.ReadAll(r.Body)
	body := string(raw)
	if err != nil {
		body = "unreadable: " + err.Error()
	}
	// The path as sent, percent-encoding included: v1 quotes each segment whole (spec H84).
	path := r.URL.EscapedPath()
	if m := sendPathRE.FindStringSubmatch(path); m != nil {
		path = m[1] + txnMark
	}
	query := map[string]string{}
	for k, v := range r.URL.Query() {
		query[k] = strings.Join(v, ",")
	}
	hs.mu.Lock()
	defer hs.mu.Unlock()
	hs.received = append(hs.received, request{
		Method: r.Method, Path: path, Query: query,
		Auth: r.Header.Get("Authorization"), ContentType: r.Header.Get("Content-Type"), Body: body,
	})
	return hs.fail
}

func (hs *homeserver) serve(w http.ResponseWriter, r *http.Request) {
	switch hs.record(r) {
	case fail500:
		reply(w, http.StatusInternalServerError, map[string]any{"errcode": "M_UNKNOWN"})
	case fail403:
		reply(w, http.StatusForbidden, map[string]any{"errcode": "M_FORBIDDEN"})
	case failSlow:
		select {
		case <-time.After(slowFor):
		case <-r.Context().Done():
		case <-hs.stop:
		}
		reply(w, http.StatusInternalServerError, map[string]any{"errcode": "M_UNKNOWN"})
	case failWrongTeam:
		hs.answer(w, r, true)
	default:
		hs.answer(w, r, false)
	}
}

func reply(w http.ResponseWriter, status int, body any) {
	raw, err := json.Marshal(body)
	if err != nil {
		status, raw = http.StatusInternalServerError, []byte(`{"errcode":"M_UNKNOWN"}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(raw); err != nil {
		return
	}
}

var notFound = map[string]any{"errcode": "M_NOT_FOUND"}

// answer serves the room state and the send for the rooms the scenarios know.
func (hs *homeserver) answer(w http.ResponseWriter, r *http.Request, wrong bool) {
	rest, ok := strings.CutPrefix(r.URL.Path, clientPrefix+"/rooms/")
	if !ok {
		reply(w, http.StatusNotFound, notFound)
		return
	}
	parts := strings.Split(rest, "/")
	room, found := hs.rooms[parts[0]]
	switch {
	case !found:
		reply(w, http.StatusNotFound, notFound)
	case r.Method == http.MethodPut && len(parts) == 4 && parts[1] == "send" && parts[2] == "m.room.message":
		reply(w, http.StatusOK, map[string]any{"event_id": sentEvent})
	case r.Method == http.MethodGet && len(parts) == 4 && parts[1] == "state" && parts[3] == "":
		hs.state(w, parts[0], room, parts[2], wrong)
	default:
		reply(w, http.StatusNotFound, notFound)
	}
}

func (hs *homeserver) state(w http.ResponseWriter, roomID string, room roomConf, kind string, wrong bool) {
	var team map[string]any
	if err := json.Unmarshal(room.Team, &team); err != nil {
		reply(w, http.StatusInternalServerError, map[string]any{"errcode": "M_UNKNOWN"})
		return
	}
	switch kind {
	case "m.room.create":
		reply(w, http.StatusOK, map[string]any{
			"type": kind, "state_key": "", "sender": room.Admin,
			"event_id": "$" + roomID[1:], "content": map[string]any{"room_version": roomVersion},
		})
	case "agent_bus.team":
		content := maps.Clone(team)
		if wrong {
			content["team"] = wrongTeam
		}
		reply(w, http.StatusOK, map[string]any{
			"type": kind, "state_key": "", "sender": room.Admin,
			"event_id": teamEvent, "content": content,
		})
	case "m.room.power_levels":
		reply(w, http.StatusOK, powerLevels(team["humans"]))
	default:
		reply(w, http.StatusNotFound, notFound)
	}
}

// powerLevels is protocol.expected_power_levels: the exact levels of a trusted team room.
func powerLevels(humans any) map[string]any {
	users := map[string]any{}
	if list, ok := humans.([]any); ok {
		for _, h := range list {
			if s, ok := h.(string); ok {
				users[s] = humanPower
			}
		}
	}
	return map[string]any{
		"users": users, "users_default": 0, "events_default": 0, "state_default": 100,
		"invite": 100, "kick": 100, "ban": 100, "redact": 100,
		"notifications": map[string]any{"room": humanPower},
		"events": map[string]any{
			"m.room.power_levels": 100, "m.room.tombstone": 150,
			"m.room.redaction": 100, "m.reaction": humanPower, "m.sticker": 100, "agent_bus.status": 0,
		},
	}
}

// answered is one reply the fake gave in its own test.
type answered struct {
	code int
	body map[string]any
}

var testRoom = "!" + strings.Repeat("R", 43)

// testBase is testRoom's API path as the Python sends it, the `!` percent-encoded.
var testBase = clientPrefix + "/rooms/%21" + strings.Repeat("R", 43)

func testHomeserver(t *testing.T) *homeserver {
	t.Helper()
	rooms := map[string]roomConf{testRoom: {Admin: "@admin:x", Team: json.RawMessage(`{"team":"team-a","humans":["@alice:x"]}`)}}
	return startHomeserver(t, rooms, failNone)
}

func callFake(t *testing.T, hs *homeserver, method, path string, body []byte) answered {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, hs.url+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer tok")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Error(err)
		}
	}()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return answered{resp.StatusCode, out}
}

// The fake fails as switched: every request answers the failure.
func TestTheFakeHomeserverFails(t *testing.T) {
	hs := testHomeserver(t)
	hs.setFail(failWrongTeam)
	wrong := callFake(t, hs, http.MethodGet, testBase+"/state/agent_bus.team/?format=event", nil)
	if content, ok := wrong.body["content"].(map[string]any); !ok || content["team"] != wrongTeam {
		t.Errorf("wrong team: %+v", wrong)
	}
	for fail, want := range map[string]int{fail500: 500, fail403: 403} {
		hs.setFail(fail)
		if got := callFake(t, hs, http.MethodGet, testBase+"/state/m.room.create/", nil); got.code != want {
			t.Errorf("%s: %d", fail, got.code)
		}
	}
	if knownFailure("teapot") {
		t.Error("an unknown failure must be refused")
	}
}

// The fake answers what verify_room and the send need, and records each request with its
// transaction ID normalised.
func TestTheFakeHomeserverServesAndRecords(t *testing.T) {
	hs := testHomeserver(t)
	call := func(method, path string, body []byte) answered { return callFake(t, hs, method, path, body) }
	base := testBase
	if got := call(http.MethodGet, base+"/state/m.room.create/?format=event", nil); got.code != 200 || got.body["event_id"] != "$"+strings.Repeat("R", 43) {
		t.Errorf("create: %+v", got)
	}
	if got := call(http.MethodGet, base+"/state/agent_bus.team/?format=event", nil); got.code != 200 || got.body["sender"] != "@admin:x" {
		t.Errorf("team: %+v", got)
	}
	levels := call(http.MethodGet, base+"/state/m.room.power_levels/", nil)
	if users, ok := levels.body["users"].(map[string]any); levels.code != 200 || !ok || users["@alice:x"] != float64(humanPower) {
		t.Errorf("power levels: %+v", levels)
	}
	for _, path := range []string{base + "/state/m.room.topic/?format=event", clientPrefix + "/rooms/%21other/state/m.room.create/", "/elsewhere"} {
		if got := call(http.MethodGet, path, nil); got.code != 404 {
			t.Errorf("%s: %d, want 404", path, got.code)
		}
	}
}

func TestTheFakeHomeserverRecordsTheSend(t *testing.T) {
	hs := testHomeserver(t)
	callFake(t, hs, http.MethodGet, testBase+"/state/m.room.create/?format=event", nil)
	if got := callFake(t, hs, http.MethodPut, testBase+"/send/m.room.message/pbabc", []byte(`{"b": 1, "a": 2}`)); got.code != 200 || got.body["event_id"] != sentEvent {
		t.Errorf("send: %+v", got)
	}
	got := hs.drain(func(b []byte) []byte { return bytes.ReplaceAll(b, []byte("tok"), []byte("{tok}")) })
	if len(got) != 2 {
		t.Fatalf("recorded %d requests, want 2: %+v", len(got), got)
	}
	send := got[1]
	if send.Path != testBase+"/send/m.room.message/"+txnMark || send.Auth != "Bearer {tok}" {
		t.Errorf("send recorded as %+v", send)
	}
	if send.Body != `{"b": 1, "a": 2}` || got[0].Body != "" || got[0].Query["format"] != "event" {
		t.Errorf("send recorded as %+v", send)
	}
	if len(hs.drain(func(b []byte) []byte { return b })) != 0 {
		t.Error("a drain empties the record")
	}
}

// Requests to several rooms are grouped by room, each room's own order kept.
func TestTheFakeHomeserverGroupsByRoom(t *testing.T) {
	hs := testHomeserver(t)
	other := clientPrefix + "/rooms/%21" + strings.Repeat("A", 43)
	for _, path := range []string{testBase + "/state/m.room.create/", other + "/state/m.room.create/", testBase + "/state/agent_bus.team/", other + "/state/agent_bus.team/"} {
		callFake(t, hs, http.MethodGet, path, nil)
	}
	got := hs.drain(func(b []byte) []byte { return b })
	paths := make([]string, 0, len(got))
	for _, r := range got {
		paths = append(paths, strings.TrimPrefix(r.Path, clientPrefix+"/rooms/"))
	}
	a, r := "%21"+strings.Repeat("A", 43), "%21"+strings.Repeat("R", 43)
	want := []string{a + "/state/m.room.create/", a + "/state/agent_bus.team/", r + "/state/m.room.create/", r + "/state/agent_bus.team/"}
	if !slices.Equal(paths, want) {
		t.Errorf("got %v, want %v", paths, want)
	}
	if roomOf("/elsewhere") != "" {
		t.Error("a path outside the rooms API has no room")
	}
}
