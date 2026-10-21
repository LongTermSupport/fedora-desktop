package fakehs

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

// The corpus probe H4 recorded against a running Tuwunel (Plan 00161), kept where the v1 fake's
// own replay reads it, so both fakes are proven against one set of answers (SPEC-matrix M38).
// This file's three v1 names (the corpus path, the probe's server name and v1's status type, all
// as recorded) are deliberate v1 references: each is owed a line in D 4.1's allowed.tsv.
const fixtureDir = "../../../../../tests/helpers/pingbus/fixtures/tuwunel"

const probeServerName = "probe.agent-bus.internal"

// Keys the replay does not compare: nothing on either side reads them (test_fakes.py:42-48).
var ignoredKeys = map[string]bool{"device_lists": true, "unsigned": true, "unread_notifications": true}

// Values a server invents, compared by presence and null-ness only (test_fakes.py:49-53).
var volatileKeys = map[string]bool{
	"access_token": true, "device_id": true, "nonce": true, "event_id": true, "room_id": true,
	"origin_server_ts": true, "next_batch": true, "prev_batch": true, "start": true, "end": true,
	"last_seen_ts": true,
}

var batchKeys = []string{"next_batch", "prev_batch", "start", "end"}

var (
	roomIDKey     = regexp.MustCompile(`^![A-Za-z0-9_-]{43}$`)
	placeholderRE = regexp.MustCompile(`^<(token|password|mac):[a-z0-9-]+>$`)
)

// Tuwunel repeats an unchanged v1 status event (key "probe") in an incremental sync's state;
// the fake sends state only when it changed, as the spec says (test_fakes.py:57-64).
const divergenceFixture = "058-sync-held.json"

type fixture struct {
	Name     string            `json:"name"`
	Method   string            `json:"method"`
	Path     string            `json:"path"`
	Query    map[string]string `json:"query"`
	Body     json.RawMessage   `json:"body"`
	Auth     *string           `json:"auth"`
	Status   int               `json:"status"`
	Response json.RawMessage   `json:"response"`
}

func loadFixtures(t *testing.T) []fixture {
	t.Helper()
	index, err := os.ReadFile(filepath.Join(fixtureDir, "index.json"))
	if err != nil {
		t.Fatalf("read the fixture index: %v", err)
	}
	var names []string
	if err := json.Unmarshal(index, &names); err != nil {
		t.Fatalf("decode the fixture index: %v", err)
	}
	out := make([]fixture, 0, len(names))
	for _, name := range names {
		raw, err := os.ReadFile(filepath.Join(fixtureDir, filepath.Base(name)))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		var f fixture
		if err := json.Unmarshal(raw, &f); err != nil {
			t.Fatalf("decode %s: %v", name, err)
		}
		f.Name = name
		out = append(out, f)
	}
	return out
}

func decodeAny(t *testing.T, raw []byte) any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("decode %q: %v", raw, err)
	}
	return v
}

func volatile(v any) string {
	if v == nil {
		return "<null>"
	}
	return "<set>"
}

// normalise is the comparable form of an answer (test_fakes.py:84-104).
func normalise(t *testing.T, v any) any {
	t.Helper()
	switch x := v.(type) {
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = normalise(t, item)
		}
		return out
	case map[string]any:
		out := map[string]any{}
		for key, value := range x {
			if ignoredKeys[key] {
				continue
			}
			name := key
			if roomIDKey.MatchString(key) {
				name = "<room>"
			}
			switch {
			case volatileKeys[key]:
				out[name] = volatile(value)
			case key == "state" || key == "invite_state":
				out[name] = normaliseStateList(t, value)
			default:
				out[name] = normalise(t, value)
			}
		}
		return out
	default:
		return v
	}
}

func normaliseStateList(t *testing.T, v any) any {
	t.Helper()
	section, ok := v.(map[string]any)
	if !ok {
		return normalise(t, v)
	}
	out := map[string]any{}
	for key, value := range section {
		if key != "events" {
			out[key] = normalise(t, value)
		}
	}
	events, ok := section["events"].([]any)
	if !ok {
		events = nil
	}
	sorted := make([]any, len(events))
	for i, e := range events {
		sorted[i] = normalise(t, e)
	}
	slices.SortFunc(sorted, func(a, b any) int { return strings.Compare(mustJSON(t, a), mustJSON(t, b)) })
	out["events"] = sorted
	return out
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return string(raw)
}

func asObject(t *testing.T, v any) map[string]any {
	t.Helper()
	obj, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("not a JSON object: %v", v)
	}
	return obj
}

func asList(t *testing.T, v any) []any {
	t.Helper()
	list, ok := v.([]any)
	if !ok {
		t.Fatalf("not a JSON array: %v", v)
	}
	return list
}

func dropDivergence(t *testing.T, name string, response any) any {
	t.Helper()
	if name != divergenceFixture {
		return response
	}
	join := asObject(t, asObject(t, asObject(t, response)["rooms"])["join"])
	dropped := false
	for _, room := range join {
		state := asObject(t, asObject(t, room)["state"])
		var kept []any
		for _, e := range asList(t, state["events"]) {
			ev := asObject(t, e)
			if ev["type"] == "agent_bus.status" && ev["state_key"] == "probe" {
				dropped = true
				continue
			}
			kept = append(kept, e)
		}
		state["events"] = kept
	}
	if !dropped {
		t.Fatalf("%s: the documented divergence is no longer in the fixture", name)
	}
	return response
}

// synapseMAC is Synapse's shared-secret registration MAC, written here independently of the
// fake (test_fakes.py:74-77).
func synapseMAC(key, nonce, user, password string, admin bool) string {
	kind := "notadmin"
	if admin {
		kind = "admin"
	}
	mac := hmac.New(sha1.New, []byte(key))
	mac.Write([]byte(strings.Join([]string{nonce, user, password, kind}, "\x00")))
	return hex.EncodeToString(mac.Sum(nil))
}

// replay maps the fixtures' scrubbed values onto those a fresh fake hands out
// (test_fakes.py:121-177).
type replay struct {
	t       *testing.T
	macKey  string
	tokens  map[string]string
	ids     map[string]string
	batches map[string]string
	nonces  map[string]string
}

func (r *replay) subIDs(text string) string {
	for recorded, served := range r.ids {
		text = strings.ReplaceAll(text, recorded, served)
	}
	return text
}

func (r *replay) body(f fixture) []byte {
	if len(f.Body) == 0 || string(f.Body) == "null" {
		return nil
	}
	body := []byte(r.subIDs(string(f.Body)))
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		r.t.Fatalf("%s: the recorded body is not an object: %v", f.Name, err)
	}
	if nonce, ok := obj["nonce"].(string); ok {
		obj["nonce"] = r.nonces[nonce]
	}
	if mac, ok := obj["mac"].(string); ok && placeholderRE.MatchString(mac) {
		admin := obj["admin"] == true
		obj["mac"] = synapseMAC(r.macKey, fmt.Sprint(obj["nonce"]), fmt.Sprint(obj["username"]),
			fmt.Sprint(obj["password"]), admin)
	}
	return []byte(mustJSON(r.t, obj))
}

func (r *replay) request(f fixture) (string, string, url.Values, []byte, string) {
	query := url.Values{}
	for key, value := range f.Query {
		if key == "since" || key == "from" || key == "to" {
			query.Set(key, r.batches[value])
		} else {
			query.Set(key, r.subIDs(value))
		}
	}
	token := ""
	if f.Auth != nil {
		token = r.tokens[*f.Auth]
	}
	return f.Method, r.subIDs(f.Path), query, r.body(f), token
}

func (r *replay) learnID(recorded, served any) {
	rec, got := fmt.Sprint(recorded)[1:], fmt.Sprint(served)[1:]
	if prev, ok := r.ids[rec]; ok && prev != got {
		r.t.Fatalf("recorded ID %v was answered as two different fake IDs", recorded)
	}
	r.ids[rec] = got
}

func (r *replay) learnBatch(recorded, served any) {
	rec, got := fmt.Sprint(recorded), fmt.Sprint(served)
	if prev, ok := r.batches[rec]; ok && prev != got {
		r.t.Fatalf("recorded batch %s answered as %s and %s", rec, prev, got)
	}
	r.batches[rec] = got
}

func (r *replay) learn(recordedAny, servedAny any) {
	recorded, ok1 := recordedAny.(map[string]any)
	served, ok2 := servedAny.(map[string]any)
	if !ok1 || !ok2 {
		return
	}
	for _, key := range []string{"room_id", "event_id"} {
		if rv, ok := recorded[key]; ok {
			if gv, ok := served[key]; ok {
				r.learnID(rv, gv)
			}
		}
	}
	if rv, ok := recorded["access_token"].(string); ok {
		r.tokens[rv] = fmt.Sprint(served["access_token"])
	}
	if rv, ok := recorded["nonce"].(string); ok {
		r.nonces[rv] = fmt.Sprint(served["nonce"])
	}
	for _, key := range batchKeys {
		if rv, ok := recorded[key]; ok {
			if gv, ok := served[key]; ok {
				r.learnBatch(rv, gv)
			}
		}
	}
	rooms, ok := recorded["rooms"].(map[string]any)
	if !ok {
		return
	}
	join, ok := rooms["join"].(map[string]any)
	if !ok {
		return
	}
	servedJoin := asObject(r.t, asObject(r.t, served["rooms"])["join"])
	for roomID, room := range join {
		timeline := asObject(r.t, asObject(r.t, room)["timeline"])
		servedRoom := asObject(r.t, servedJoin["!"+r.ids[roomID[1:]]])
		r.learnBatch(timeline["prev_batch"], asObject(r.t, servedRoom["timeline"])["prev_batch"])
	}
}

func fixedNow() time.Time { return time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC) }

func TestReplaysEveryRecordedExchange(t *testing.T) {
	server := New(Options{ServerName: probeServerName, Now: fixedNow, Quirks: TuwunelQuirks()})
	r := &replay{
		t: t, macKey: server.RegistrationKey(), tokens: map[string]string{}, ids: map[string]string{},
		batches: map[string]string{}, nonces: map[string]string{},
	}
	fixtures := loadFixtures(t)
	if len(fixtures) != 73 {
		t.Fatalf("the corpus holds %d exchanges, want 73", len(fixtures))
	}
	for _, f := range fixtures {
		method, path, query, body, token := r.request(f)
		status, answer := server.Handle(context.Background(), method, path, query, body, token)
		if status != f.Status {
			t.Fatalf("%s: status %d, want %d: %s", f.Name, status, f.Status, answer)
		}
		got := decodeAny(t, answer)
		want := dropDivergence(t, f.Name, decodeAny(t, f.Response))
		if !reflect.DeepEqual(normalise(t, got), normalise(t, want)) {
			t.Fatalf("%s:\n got  %s\n want %s", f.Name, mustJSON(t, normalise(t, got)), mustJSON(t, normalise(t, want)))
		}
		r.learn(decodeAny(t, f.Response), got)
	}
	if err := server.CheckModelled(); err != nil {
		t.Fatal(err)
	}
}

func TestEveryFixtureIsScrubbed(t *testing.T) {
	for _, f := range loadFixtures(t) {
		if f.Auth != nil && !placeholderRE.MatchString(*f.Auth) {
			t.Errorf("%s: auth %q is not a placeholder", f.Name, *f.Auth)
		}
		raw, err := os.ReadFile(filepath.Join(fixtureDir, f.Name))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(raw, []byte("Bearer")) {
			t.Errorf("%s carries a Bearer header", f.Name)
		}
	}
}
