package state

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// The member files' caps are taken as the files are written (marshal): a value the rules accept
// is never refused at its write for the escapes encoding/json adds (`<`, `>`, `&` six bytes
// each, U+2028 and U+2029 six). Each test fills the file's free text with each kind of piece.
var filePieces = []string{"a", "<", lineSeparator, `"`}

// repeatTo is piece repeated as often as fits in n bytes.
func repeatTo(piece string, n int) string { return strings.Repeat(piece, n/len(piece)) }

// n3d-D3: a roster is a state event, which Matrix caps at 65536 bytes in its own canonical
// JSON, which writes `<` and U+2028 as they are; the cache writes them escaped, so the largest
// roster of each kind of text is still written and read back.
func TestTheLargestRosterIsCachedAsWritten(t *testing.T) {
	const eventCap = 65536
	for _, piece := range filePieces {
		each := len(jsonText(piece)) - len(`""`) // the piece's bytes in the canonical form
		pad := strings.Repeat(piece, (eventCap-len(`{"v":1,"pad":""}`))/each)
		content := `{"v":1,"pad":` + jsonText(pad) + `}`
		if len(content) > eventCap {
			t.Fatalf("%q: the canonical content is %d bytes", piece, len(content))
		}
		c := RosterCache{Rev: 9007199254740991, Content: json.RawMessage(content)}
		data, err := c.Encode()
		if err != nil {
			t.Errorf("%q: the largest roster's cache is refused: %v", piece, err)
			continue
		}
		if len(data) > MaxRosterCache {
			t.Errorf("%q: the largest roster's cache is %d bytes, over its cap of %d", piece, len(data), MaxRosterCache)
		}
		roundTrips(t, c, MaxRosterCache, DecodeRosterCache)
	}
}

// TestTheLargestSessionIsWritten holds session.json's cap as written: the longest transcript and
// socket paths of each kind of text, every team at the longest id and role, and every number at
// its largest, written within the cap.
func TestTheLargestSessionIsWritten(t *testing.T) {
	for _, piece := range filePieces {
		transcript := "/" + repeatTo(piece, maxTranscriptPath-len("/.jsonl")) + ".jsonl"
		socket := "/" + repeatTo(piece, maxSocketPath-len("//cc-socks/4194304.sock")) + "/cc-socks/4194304.sock"
		doc := edit(t, sessionStarted, `"transcript":"`+sampleTranscript+`"`, `"transcript":`+jsonText(transcript))
		doc = edit(t, doc, `"socket":"`+sampleSocket+`"`, `"socket":`+jsonText(socket))
		doc = edit(t, doc, `"pid":4242,"start_ticks":99`, `"pid":4194304,"start_ticks":9007199254740991`)
		doc = edit(t, doc, `"roles":{"alpha":"worker"}`, `"roles":{`+teamsJSON(maxTeams, `"%s":"coordinator"`)+`}`)
		doc = edit(t, doc, `"pending_digest":null,"failure_ms":null,"no_waker_ms":null`,
			`"pending_digest":"`+strings.Repeat("a", 64)+`","failure_ms":253402300799999,"no_waker_ms":253402300799999`)
		doc = edit(t, doc, `"end_reason":null`, `"end_reason":"logout"`)
		doc = edit(t, doc, `"generation":3`, `"generation":9007199254740991`)
		s, err := DecodeSession([]byte(doc))
		if err != nil {
			t.Errorf("%q: the largest session.json does not decode: %v", piece, err)
			continue
		}
		data, err := s.Encode()
		if err != nil {
			t.Errorf("%q: the largest session.json is refused at its write: %v", piece, err)
			continue
		}
		if len(data) > MaxSession {
			t.Errorf("%q: the largest session.json is written as %d bytes, over its cap of %d", piece, len(data), MaxSession)
		}
		t.Logf("%q: the largest session.json is written as %d bytes", piece, len(data))
	}
}

// jsonText is s as A's compact JSON string, which writes `<` and U+2028 as they are, as Matrix's
// canonical JSON does.
func jsonText(s string) string { return string(proto.Str(s).Compact()) }
