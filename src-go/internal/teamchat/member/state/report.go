package state

import (
	"errors"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// MaxReport bounds report.json.
const MaxReport = 4 << 10

const reportFile = "report.json"

// CodeMissing is the refusal code of a desired file deleted under a live keeper (B 2.10).
const CodeMissing = "missing"

// The store's state in a report.
const (
	StoreOK      = "ok"
	StoreRefused = "refused"
)

var desiredCodes = []string{CodeJSON, CodeSchema, CodeVersion, CodeSeat, CodeMissing}

// errSeatRef is a seat reference off its grammar; its text never quotes the value.
var errSeatRef = errors.New("state: a seat is not <team>/<seat>")

// SeatRef is a seat as report.json names it, `<team>/<seat>`: the team id and the handle's seat.
type SeatRef struct {
	Team paths.TeamID
	Seat string
}

// MarshalText is the reference's text, refused unless both halves are of their grammars.
func (s SeatRef) MarshalText() ([]byte, error) {
	team, err := s.Team.MarshalText()
	if err != nil || !proto.ValidSeat(s.Seat) {
		return nil, errSeatRef
	}
	return []byte(string(team) + "/" + s.Seat), nil
}

// UnmarshalText reads `<team>/<seat>`.
func (s *SeatRef) UnmarshalText(b []byte) error {
	team, seat, ok := strings.Cut(string(b), "/")
	id, err := paths.ParseTeamID(team)
	if !ok || err != nil || !proto.ValidSeat(seat) {
		return errSeatRef
	}
	*s = SeatRef{Team: id, Seat: seat}
	return nil
}

// Report is B 2.3.3's report.json, the keeper's report of one held seat for area C's views,
// never authority.
type Report struct {
	V             int           `json:"v"`
	Run           paths.RunID   `json:"run"`
	StartedMS     int64         `json:"started_ms"`
	Seat          SeatRef       `json:"seat"`
	Handle        string        `json:"handle"`
	Link          string        `json:"link"`
	LastSyncOKMS  *int64        `json:"last_sync_ok_ms"`
	Pending       int64         `json:"pending"`
	Delivery      string        `json:"delivery"`
	Desired       ReportDesired `json:"desired"`
	Store         string        `json:"store"`
	NeedsRelaunch bool          `json:"needs_relaunch"`
}

// ReportDesired is the last read of the desired membership: the generation applied, and why
// the last read was refused.
type ReportDesired struct {
	Generation int64   `json:"generation"`
	State      string  `json:"state"`
	Code       *string `json:"code"`
}

// Encode is the file's bytes.
func (r Report) Encode() ([]byte, error) {
	r.V = Version
	return encode(reportFile, r, DecodeReport)
}

// DecodeReport reads report.json strictly.
func DecodeReport(data []byte) (Report, error) {
	var r Report
	if _, err := decodeStrict(reportFile, data, MaxReport, &r, func(c *rules) { r.check(c) }); err != nil {
		return Report{}, err
	}
	return r, nil
}

func (r Report) check(c *rules) {
	c.need(validMS(r.StartedMS), "started_ms is not a time")
	h, ok := proto.ParseHandle(r.Handle)
	c.need(ok && h.Seat == r.Seat.Seat, "handle is not a handle of the seat")
	c.need(slices.Contains(links, r.Link), "link is not running, restarting or failed")
	c.need(optionalMS(r.LastSyncOKMS), "last_sync_ok_ms is not a time or null")
	c.need(r.Pending >= 0 && r.Pending <= capPending, "pending is not a count the store holds")
	c.need(slices.Contains(deliveries, r.Delivery), "delivery is not socket, wait or none")
	d := r.Desired
	c.need(d.Generation >= 0, "desired.generation is negative")
	c.need(slices.Contains(desiredStates, d.State), "desired.state is not applied or refused")
	c.need((d.Code == nil) == (d.State == DesiredApplied), "desired.code is not null exactly when applied")
	c.need(optionalOf(d.Code, desiredCodes), "desired.code is not json, schema, version, seat or missing")
	c.need(r.Store == StoreOK || r.Store == StoreRefused, "store is not ok or refused")
}
