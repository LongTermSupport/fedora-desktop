package state

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"unicode/utf8"

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

// WhyLaunchFailed is a report's why when Claude Code could not be started or ended before its
// first SessionStart (B 2.4.1 step 7, n3c-D21); CMD's own text is never in the report.
const WhyLaunchFailed = "launch-failed"

// MaxDown bounds a down cause as written (a JSON string, quotes and escapes counted), so a cause
// can never take report.json past its cap.
const MaxDown = 512

// DownNoAnswer is the down cause of a team its link has down without naming why.
const DownNoAnswer = "the homeserver did not answer"

const downRule = "down is not null or a cause of at most 512 bytes as written"

// ValidDown is whether d is a report's down: null, or a cause that is not empty and within
// MaxDown as written.
func ValidDown(d *string) bool {
	if d == nil {
		return true
	}
	n, err := writtenSize(*d)
	return *d != "" && err == nil && n <= MaxDown
}

// ClipDown is cause as a report carries it: DownNoAnswer for none, else cause cut at a character
// boundary to MaxDown as written.
func ClipDown(cause string) string {
	if cause == "" {
		return DownNoAnswer
	}
	for !ValidDown(&cause) {
		_, size := utf8.DecodeLastRuneInString(cause)
		cause = cause[:len(cause)-size]
	}
	return cause
}

// Report is B 2.3.3's report.json, the keeper's report of one held seat for area C's views,
// never authority. Why is null, crash-loop or config with a failed link, or launch-failed.
type Report struct {
	V            int         `json:"v"`
	Run          paths.RunID `json:"run"`
	StartedMS    int64       `json:"started_ms"`
	Seat         SeatRef     `json:"seat"`
	Handle       string      `json:"handle"`
	Link         string      `json:"link"`
	Why          *string     `json:"why"`
	LastSyncOKMS *int64      `json:"last_sync_ok_ms"`
	// Down is why the link has the team down (its homeserver not answering, or a stop's code),
	// null while it is up: with LastSyncOKMS, the proof that a join reached the team (R60).
	Down          *string       `json:"down"`
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

// DecodeReport reads report.json strictly. A keeper built before R60 writes no "down", and the
// host is updated before a running session's image, so that report is read with down null.
func DecodeReport(data []byte) (Report, error) {
	data, limit := withDownNull(data)
	var r Report
	if err := decodeStrict(reportFile, data, limit, &r, func(c *rules) { r.check(c) }); err != nil {
		return Report{}, err
	}
	return r, nil
}

// downNull is the member withDownNull adds to a report that has no "down".
const downNull = `,"down":null`

// withDownNull is data with "down":null added when it is an object within MaxReport that has
// other members and no "down", and the cap to read it by; any other data is left to the
// strict decode to judge.
func withDownNull(data []byte) ([]byte, int) {
	if len(data) > MaxReport {
		return data, MaxReport
	}
	v, err := proto.Parse(data)
	if err != nil || v.Kind() != proto.KindObject || len(v.Members()) == 0 {
		return data, MaxReport
	}
	if _, ok := v.Get("down"); ok {
		return data, MaxReport
	}
	end := bytes.LastIndexByte(data, '}')
	out := make([]byte, 0, len(data)+len(downNull))
	out = append(append(append(out, data[:end]...), downNull...), data[end:]...)
	return out, MaxReport + len(downNull)
}

func (r Report) check(c *rules) {
	c.need(validMS(r.StartedMS), "started_ms is not a time")
	h, ok := proto.ParseHandle(r.Handle)
	c.need(ok && h.Seat == r.Seat.Seat, "handle is not a handle of the seat")
	c.need(slices.Contains(links, r.Link), "link is not running, restarting or failed")
	failedWhy := r.Why != nil && (*r.Why == WhyCrashLoop || *r.Why == WhyConfig)
	c.need(optionalOf(r.Why, []string{WhyCrashLoop, WhyConfig, WhyLaunchFailed}), "why is not crash-loop, config, launch-failed or null")
	c.need(failedWhy == (r.Link == LinkFailed), "why is not crash-loop or config exactly for a failed link")
	c.need(optionalMS(r.LastSyncOKMS), "last_sync_ok_ms is not a time or null")
	c.need(ValidDown(r.Down), downRule)
	c.need(r.Pending >= 0 && r.Pending <= capPending, "pending is not a count the store holds")
	c.need(slices.Contains(deliveries, r.Delivery), "delivery is not socket, wait or none")
	d := r.Desired
	c.need(d.Generation >= 0, "desired.generation is negative")
	c.need(slices.Contains(desiredStates, d.State), "desired.state is not applied or refused")
	c.need((d.Code == nil) == (d.State == DesiredApplied), "desired.code is not null exactly when applied")
	c.need(optionalOf(d.Code, desiredCodes), "desired.code is not json, schema, version, seat or missing")
	c.need(r.Store == StoreOK || r.Store == StoreRefused, "store is not ok or refused")
}
