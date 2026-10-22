package state

import (
	"slices"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// MaxKeeper bounds keeper.json (B 2.9: the status line reads at most 4 KiB).
const MaxKeeper = 4 << 10

const keeperFile = "keeper.json"

// Why a link failed for good: a crash loop, or a final exit 78.
const (
	WhyCrashLoop = "crash-loop"
	WhyConfig    = "config"
)

// Keeper is B 2.3.3's keeper.json, the keeper's view of the link: read by the status line, and
// by a client only when the link does not answer.
type Keeper struct {
	V        int                `json:"v"`
	Run      paths.RunID        `json:"run"`
	Link     string             `json:"link"`
	Why      *string            `json:"why"`
	Restarts int64              `json:"restarts"`
	NextMS   *int64             `json:"next_ms"`
	Kind     string             `json:"kind"`
	Session  *paths.SessionName `json:"session"`
	Desired  string             `json:"desired"`
}

// Encode is the file's bytes.
func (k Keeper) Encode() ([]byte, error) {
	k.V = Version
	return encode(keeperFile, k, DecodeKeeper)
}

// DecodeKeeper reads keeper.json strictly.
func DecodeKeeper(data []byte) (Keeper, error) {
	var k Keeper
	if _, err := decodeStrict(keeperFile, data, MaxKeeper, &k, func(c *rules) { k.check(c) }); err != nil {
		return Keeper{}, err
	}
	return k, nil
}

func (k Keeper) check(c *rules) {
	c.need(slices.Contains(links, k.Link), "link is not running, restarting or failed")
	c.need((k.Why != nil) == (k.Link == LinkFailed), "why is set for a link that is not failed, or missing for one that is")
	c.need(optionalOf(k.Why, []string{WhyCrashLoop, WhyConfig}), "why is not crash-loop, config or null")
	c.need((k.NextMS != nil) == (k.Link == LinkRestarting), "next_ms is set for a link that is not restarting, or missing for one that is")
	c.need(k.NextMS == nil || *k.NextMS >= 0, "next_ms is negative")
	c.need(k.Restarts >= 0, "restarts is negative")
	c.need(slices.Contains(memberKinds, k.Kind), "kind is not ccy, lxc, host or other")
	named := k.Kind == KindCCY || k.Kind == KindLXC
	c.need((k.Session != nil) == named, "session is not set exactly for a ccy or lxc member")
	c.need(slices.Contains(desiredStates, k.Desired), "desired is not applied or refused")
}
