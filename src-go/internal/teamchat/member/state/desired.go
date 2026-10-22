package state

import (
	"slices"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// MaxDesired bounds desired.json; maxDesiredSeats bounds its seats and its farewells each.
const (
	MaxDesired      = 64 << 10
	maxDesiredSeats = 64
)

const desiredFile = "desired.json"

// Why a seat was taken out of the session: the owner released it, or the member left the team
// itself, removed or parked (B 2.10).
const (
	FarewellReleased = "released"
	FarewellRemoved  = "removed"
	FarewellParked   = "parked"
)

// Desired is C 3.7.5's desired.json, the host-written membership a session reads and never
// writes: the seats it should hold, the seats taken out of it, and the inbound decision.
type Desired struct {
	V          int               `json:"v"`
	Session    paths.SessionName `json:"session"`
	Generation int64             `json:"generation"`
	WrittenMS  int64             `json:"written_ms"`
	Seats      []DesiredSeat     `json:"seats"`
	Farewell   []Farewell        `json:"farewell"`
	Inbound    string            `json:"inbound"`
}

// DesiredSeat is a seat the session should hold: the team and the handle's seat.
type DesiredSeat struct {
	Team paths.TeamID `json:"team"`
	Seat string       `json:"seat"`
}

// Farewell is a seat taken out of the session, and why.
type Farewell struct {
	Team   paths.TeamID `json:"team"`
	Seat   string       `json:"seat"`
	Reason string       `json:"reason"`
}

// Encode is the file's bytes.
func (d Desired) Encode() ([]byte, error) {
	d.V = Version
	return encode(desiredFile, d, DecodeDesired)
}

// DecodeDesired reads desired.json strictly. A refusal of a seat rule carries CodeSeat, the
// rest B 2.3.3's other codes, which the keeper names in report.json.
func DecodeDesired(data []byte) (Desired, error) {
	var d Desired
	if _, err := decodeStrict(desiredFile, data, MaxDesired, &d, func(c *rules) { d.check(c) }); err != nil {
		return Desired{}, err
	}
	return d, nil
}

func (d Desired) check(c *rules) {
	c.need(d.Generation >= 0, "generation is negative")
	c.need(validMS(d.WrittenMS), "written_ms is not a time")
	c.need(d.Seats != nil && len(d.Seats) <= maxDesiredSeats, "seats is not a list of at most 64 seats")
	c.need(d.Farewell != nil && len(d.Farewell) <= maxDesiredSeats, "farewell is not a list of at most 64 seats")
	c.need(slices.Contains(inbounds, d.Inbound), "inbound is not accept or refuse")
	teams := map[paths.TeamID]bool{}
	for _, s := range d.Seats {
		c.needCode(proto.ValidSeat(s.Seat), CodeSeat, "a seat is not a handle's seat")
		c.needCode(!teams[s.Team], CodeSeat, "seats holds a team twice")
		teams[s.Team] = true
	}
	gone := map[DesiredSeat]bool{}
	for _, f := range d.Farewell {
		seat := DesiredSeat{Team: f.Team, Seat: f.Seat}
		c.needCode(proto.ValidSeat(f.Seat), CodeSeat, "a farewell's seat is not a handle's seat")
		c.need(f.Reason == FarewellReleased || f.Reason == FarewellRemoved || f.Reason == FarewellParked, "a farewell's reason is not released, removed or parked")
		c.needCode(!gone[seat], CodeSeat, "farewell holds a seat twice")
		c.needCode(!slices.Contains(d.Seats, seat), CodeSeat, "farewell holds a seat that seats holds")
		gone[seat] = true
	}
}
