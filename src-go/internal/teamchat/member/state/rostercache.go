package state

import (
	"encoding/json"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// MaxRosterCache bounds roster.json: a roster is a state event, which Matrix caps at 65536
// bytes whole, so its content and the cache's few fields fit in 65 KiB.
const MaxRosterCache = 65 << 10

const rosterCacheFile = "roster.json"

// RosterCache is a team's last verified roster (n3d-D3), <seat dir>/roster.json: written when
// teamsync verifies a new rev, read at a link's start to compose sends before the first
// verification. Its content is decoded by proto's roster decoder, never trusted beyond that.
type RosterCache struct {
	V   int   `json:"v"`
	Rev int64 `json:"rev"`
	// Content is the roster state event's content as verified.
	Content json.RawMessage `json:"content"`
}

// Encode is the file's bytes.
func (r RosterCache) Encode() ([]byte, error) {
	r.V = Version
	return encode(rosterCacheFile, r, DecodeRosterCache)
}

// DecodeRosterCache reads roster.json strictly.
func DecodeRosterCache(data []byte) (RosterCache, error) {
	var r RosterCache
	if _, err := decodeStrict(rosterCacheFile, data, MaxRosterCache, &r, func(c *rules) { r.check(c) }); err != nil {
		return RosterCache{}, err
	}
	return r, nil
}

func (r RosterCache) check(c *rules) {
	c.need(r.Rev >= 1, "rev is not a roster's rev")
	content, err := proto.Parse(r.Content)
	c.need(err == nil && content.Kind() == proto.KindObject, "content is not an object")
}
