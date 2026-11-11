package state

import (
	"strings"
	"testing"
)

const (
	sessionStarted = `{"v":1,"run":"` + sampleRun + `","session_id":"` + sampleSessionID + `","source":"startup",` +
		`"transcript":"` + sampleTranscript + `","socket":"` + sampleSocket + `","claude":{"pid":4242,"start_ticks":99},` +
		`"end_reason":null,"stop_guard":{"pending_digest":null,"failure_ms":null,"no_waker_ms":null},` +
		`"told":{"generation":3,"roles":{"alpha":"worker"},"coordinators":{"alpha":"` + sampleCoordinator + `"},"not_known":[]},` +
		`"frame_told":true,"unusable":{"hook_stdin":0,"render":0}}`
	sessionFresh = `{"v":1,"run":"` + sampleRun + `","session_id":null,"source":null,"transcript":null,"socket":null,` +
		`"claude":null,"end_reason":null,"stop_guard":{"pending_digest":null,"failure_ms":null,"no_waker_ms":null},` +
		`"told":{"generation":0,"roles":{},"coordinators":{},"not_known":[]},"frame_told":false,"unusable":{"hook_stdin":0,"render":0}}`
	sampleCoordinator = "repo.2+host.podman"
)

func TestDecodeSession(t *testing.T) {
	ended := edit(t, sessionStarted, `"end_reason":null`, `"end_reason":"logout"`)
	guarded := edit(t, sessionStarted, `"pending_digest":null,"failure_ms":null,"no_waker_ms":null`,
		`"pending_digest":"`+strings.Repeat("a", 64)+`","failure_ms":5,"no_waker_ms":6`)
	noneTold := edit(t, sessionStarted, `"alpha":"`+sampleCoordinator+`"`, `"alpha":""`)
	notTold := edit(t, sessionStarted, `"coordinators":{"alpha":"`+sampleCoordinator+`"}`, `"coordinators":{}`)
	framed := edit(t, sessionStarted, `"not_known":[]`, `"not_known":["bravo","charlie"]`)
	accepts(t, DecodeSession, sessionStarted, sessionFresh, ended, guarded, noneTold, notTold, framed)
	s, err := DecodeSession([]byte(sessionStarted))
	check(t, err)
	if !s.Started() || s.Claude.PID != 4242 || s.Told.Roles[teamID(t)] != "worker" || s.Told.Coordinators[teamID(t)] != sampleCoordinator {
		t.Fatalf("DecodeSession = %+v", s)
	}
	u, err := DecodeSession([]byte(framed))
	if err != nil || len(u.Told.NotKnown) != 2 {
		t.Fatalf("the teams framed as not known = %+v, %v", u.Told.NotKnown, err)
	}
	f, err := DecodeSession([]byte(sessionFresh))
	if err != nil || f.Started() {
		t.Fatalf("a fresh session = %+v, %v", f, err)
	}
	strictRefusals(t, sessionFile, sessionStarted, "generation", MaxSession, DecodeSession)
	refusesAll(t, sessionFile, sessionStarted, DecodeSession, []refusal{
		{sampleSessionID + `"`, `SECRET"`, CodeSchema},
		{`"startup"`, `"SECRET"`, CodeSchema},
		{`"transcript":"/tmp`, `"transcript":"SECRET/tmp`, CodeSchema},
		{`.jsonl"`, `.json"`, CodeSchema},
		{`"socket":"/tmp/x/`, `"socket":"/tmp/x/../`, CodeSchema},
		{`4242.sock`, `424242.sock`, CodeSchema},
		{`"socket":"/tmp`, `"socket":"/` + strings.Repeat("a", maxSocketPath) + `/tmp`, CodeSchema},
		{`"pid":4242`, `"pid":0`, CodeSchema},
		{`"start_ticks":99`, `"start_ticks":-1`, CodeSchema},
		{`"claude":{"pid":4242,"start_ticks":99}`, `"claude":{"pid":4242}`, CodeSchema},
		{`"session_id":"` + sampleSessionID + `"`, `"session_id":null`, CodeSchema},
		{`"socket":"` + sampleSocket + `"`, `"socket":null`, CodeSchema},
		{`"end_reason":null`, `"end_reason":"SECRET"`, CodeSchema},
		{`"pending_digest":null`, `"pending_digest":"SECRET"`, CodeSchema},
		{`"failure_ms":null`, `"failure_ms":-424242`, CodeSchema},
		{`"no_waker_ms":null`, `"no_waker_ms":-1`, CodeSchema},
		{`"stop_guard":{"pending_digest":null,`, `"stop_guard":{`, CodeSchema},
		{`"generation":3`, `"generation":-1`, CodeSchema},
		{`"roles":{"alpha":"worker"}`, `"roles":null`, CodeSchema},
		{`"roles":{"alpha"`, `"roles":{"Alpha"`, CodeSchema},
		{`"worker"`, `"SECRET"`, CodeSchema},
		{`"roles":{"alpha":"worker"}`, `"roles":{` + teamsJSON(maxTeams+1, `"%s":"worker"`) + `}`, CodeSchema},
		{`"coordinators":{"alpha":"` + sampleCoordinator + `"}`, `"coordinators":null`, CodeSchema},
		{`"coordinators":{"alpha":"` + sampleCoordinator + `"}`, `"coordinators":{"beta":""}`, CodeSchema},
		{`"roles":{"alpha":"worker"}`, `"roles":{"alpha":"coordinator"}`, CodeSchema},
		{`"alpha":"` + sampleCoordinator + `"`, `"alpha":"SECRET"`, CodeSchema},
		{`"not_known":[]`, `"not_known":null`, CodeSchema},
		{`"not_known":[]`, `"not_known":["Bravo"]`, CodeSchema},
		{`"not_known":[]`, `"not_known":["alpha"]`, CodeSchema},
		{`"not_known":[]`, `"not_known":["bravo","bravo"]`, CodeSchema},
		{`"not_known":[]`, `"not_known":[` + teamsJSON(maxTeams, `"%s"`) + `]`, CodeSchema},
		{`"frame_told":true`, `"frame_told":null`, CodeSchema},
		{`"hook_stdin":0`, `"hook_stdin":-1`, CodeSchema},
		{`"render":0`, `"render":-1`, CodeSchema},
		{`"unusable":{"hook_stdin":0,"render":0}`, `"unusable":null`, CodeSchema},
	})
	refusesAll(t, sessionFile, sessionFresh, DecodeSession, []refusal{
		{`"claude":null`, `"claude":{"pid":1,"start_ticks":1}`, CodeSchema},
		{`"source":null`, `"source":"startup"`, CodeSchema},
		{`"end_reason":null`, `"end_reason":"exit"`, CodeSchema},
		{`"frame_told":false`, `"frame_told":true`, CodeSchema},
	})
}

// TestSessionCap holds the largest session.json within its cap: the longest paths, and every
// team at the longest id, a worker told the longest coordinator handle.
func TestSessionCap(t *testing.T) {
	transcript := "/" + strings.Repeat("a", maxTranscriptPath-len(sampleSessionID)-8) + "/" + sampleSessionID + ".jsonl"
	socket := "/" + strings.Repeat("a", maxSocketPath-len("/cc-socks/4194304.sock")-1) + "/cc-socks/4194304.sock"
	doc := edit(t, sessionStarted, sampleTranscript, transcript)
	doc = edit(t, doc, sampleSocket, socket)
	doc = edit(t, doc, `"pid":4242,"start_ticks":99`, `"pid":4194304,"start_ticks":9007199254740991`)
	doc = edit(t, doc, `"roles":{"alpha":"worker"}`, `"roles":{`+teamsJSON(maxTeams, `"%s":"worker"`)+`}`)
	doc = edit(t, doc, `"coordinators":{"alpha":"`+sampleCoordinator+`"}`, longestCoordinators())
	doc = edit(t, doc, `"pending_digest":null,"failure_ms":null,"no_waker_ms":null`,
		`"pending_digest":"`+strings.Repeat("a", 64)+`","failure_ms":253402300799999,"no_waker_ms":253402300799999`)
	doc = edit(t, doc, `"end_reason":null`, `"end_reason":"logout"`)
	doc = edit(t, doc, `"generation":3`, `"generation":9007199254740991`)
	doc = edit(t, doc, `"hook_stdin":0,"render":0`, `"hook_stdin":9007199254740991,"render":9007199254740991`)
	if len(transcript) != maxTranscriptPath || len(socket) != maxSocketPath || len(doc) > MaxSession {
		t.Fatalf("the largest session.json is %d bytes, over %d", len(doc), MaxSession)
	}
	accepts(t, DecodeSession, doc)
}

// longestCoordinators is told.coordinators at its largest: every team at the longest id told
// the longest handle the grammar allows (repo 48, seat 12, site 63, type 6).
func longestCoordinators() string {
	handle := strings.Repeat("r", 48) + "." + strings.Repeat("s", 12) + "+" + strings.Repeat("h", 63) + ".podman"
	return `"coordinators":{` + teamsJSON(maxTeams, `"%s":"`+handle+`"`) + `}`
}

func TestSessionRoundTrip(t *testing.T) {
	s, err := DecodeSession([]byte(sessionStarted))
	check(t, err)
	roundTrips(t, s, MaxSession, DecodeSession)
}
