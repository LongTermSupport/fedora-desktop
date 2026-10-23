package state

import (
	"encoding/json"
	"strings"
	"testing"
)

const rosterCacheGood = `{"v":1,"rev":7,"content":{"v":1,"team":"alpha","members":{}}}`

// n3d-D3: the roster cache is read strictly, its content an object kept as it was verified.
func TestN3dD3RosterCacheDecodes(t *testing.T) {
	c, err := DecodeRosterCache([]byte(rosterCacheGood))
	check(t, err)
	if c.Rev != 7 || string(c.Content) != `{"v":1,"team":"alpha","members":{}}` {
		t.Fatalf("DecodeRosterCache = %+v", c)
	}
	strictRefusals(t, rosterCacheFile, rosterCacheGood, "rev", MaxRosterCache, DecodeRosterCache)
	refusesAll(t, rosterCacheFile, rosterCacheGood, DecodeRosterCache, []refusal{
		{`"rev":7`, `"rev":0`, CodeSchema},
		{`"rev":7`, `"rev":-424242`, CodeSchema},
		{`"content":{"v":1,"team":"alpha","members":{}}`, `"content":["SECRET"]`, CodeSchema},
		{`"content":{"v":1,"team":"alpha","members":{}}`, `"content":null`, CodeSchema},
		{`"content":{"v":1,`, `"content":{"v":1.5,`, CodeJSON},
		{`"content":{"v":1,`, `"content":{"v":1,"v":1,`, CodeJSON},
	})
}

// n3d-D3: the cache goes through state.Write and Read, 0600, and the zero cache is refused.
func TestN3dD3RosterCacheRoundTrips(t *testing.T) {
	c, err := DecodeRosterCache([]byte(rosterCacheGood))
	check(t, err)
	roundTrips(t, c, MaxRosterCache, DecodeRosterCache)
}

// n3d-D3: a roster is a state event, which Matrix caps at 65536 bytes whole, so its content is
// smaller still: a cache holding a content of the whole event cap fits.
func TestN3dD3TheLargestRosterFits(t *testing.T) {
	const eventCap = 65536
	pad := eventCap - len(`{"v":1,"pad":""}`)
	content := `{"v":1,"pad":"` + strings.Repeat("x", pad) + `"}`
	if len(content) != eventCap {
		t.Fatalf("content is %d bytes", len(content))
	}
	c := RosterCache{Rev: 9007199254740991, Content: json.RawMessage(content)}
	data, err := c.Encode()
	check(t, err)
	if len(data) > MaxRosterCache {
		t.Fatalf("the largest roster cache is %d bytes, over its cap of %d", len(data), MaxRosterCache)
	}
	roundTrips(t, c, MaxRosterCache, DecodeRosterCache)
}
