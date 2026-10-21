package membership

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

// ReportSeat is a seat the keeper holds, as its report says.
type ReportSeat struct {
	Team   string `json:"team"`
	Seat   string `json:"seat"`
	Handle string `json:"handle"`
	State  string `json:"state"`
}

// ReportRefused is a desired seat the keeper did not take, and why (section 3.2's closed set).
type ReportRefused struct {
	Team   string `json:"team"`
	Seat   string `json:"seat"`
	Why    string `json:"why"`
	Holder string `json:"holder"`
}

// Report is the keeper's report for the host (section 3.3): never authority, the seat lock is
// the truth; it explains the lock. Inbound is nil when the keeper did not say.
type Report struct {
	V          int             `json:"v"`
	Generation int             `json:"generation"`
	Inbound    *bool           `json:"inbound,omitempty"`
	Seats      []ReportSeat    `json:"seats"`
	Refused    []ReportRefused `json:"refused"`
	Told       *Told           `json:"told,omitempty"`
}

// Told is what the session was last told: the desired generation and its roles.
type Told struct {
	Generation int         `json:"generation"`
	Roles      []ToldEntry `json:"roles"`
}

// ToldEntry is a role the session was told it has in a team as a handle.
type ToldEntry struct {
	Team   string `json:"team"`
	Handle string `json:"handle"`
	Role   string `json:"role"`
}

// Section 3.2's closed sets (helpers/pingbus/membership.py SEAT_STATES, WHY, NO_HOLDER,
// _HOLDER_RE and ROLES; a test pins the copy).
const (
	stateHeld     = "held"
	noHolder      = "-"
	holderPattern = `[a-z-]{1,32}`
	whyPattern    = `held-elsewhere|no-bundle|bundle-invalid|not-desired`
	rolePattern   = `worker|coordinator`
)

var (
	holderName = regexp.MustCompile(`^(?:` + holderPattern + `)$`)
	whyName    = regexp.MustCompile(`^(?:` + whyPattern + `)$`)
	roleName   = regexp.MustCompile(`^(?:` + rolePattern + `)$`)
)

// SessionDir is a session's directory beside the checkout's seats: the keeper's report and
// the conversation the SessionStart hook records (helpers/pingbus seat.py and hooks.py).
func SessionDir(checkout, session string) string {
	return filepath.Join(checkout, ".claude", "ccy", "pingbus", "sessions", session)
}

// ReportPath is where a session's keeper writes its report, beside the checkout's seats.
func ReportPath(checkout, session string) string {
	return filepath.Join(SessionDir(checkout, session), "applied.json")
}

// ReadReport is the session's report; os.ErrNotExist before its keeper wrote one.
func ReadReport(checkout, session string) (Report, error) {
	path := ReportPath(checkout, session)
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return Report{}, fmt.Errorf("the seat keeper's report %s: %w", path, err)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxDesiredBytes+1))
	if err = errors.Join(err, f.Close()); err != nil {
		return Report{}, fmt.Errorf("could not read the seat keeper's report %s: %w", path, err)
	}
	if len(data) > maxDesiredBytes {
		return Report{}, fmt.Errorf("the seat keeper's report %s is larger than %d bytes", path, maxDesiredBytes)
	}
	r, err := parseReport(data)
	if err == nil {
		err = r.check()
	}
	if err != nil {
		return Report{}, fmt.Errorf("the seat keeper's report %s is not what a keeper writes: %w", path, err)
	}
	return r, nil
}

// parseReport is section 3.2's keys and their JSON types; check judges the values.
func parseReport(data []byte) (Report, error) {
	doc, err := decodeDoc(data)
	if err != nil {
		return Report{}, err
	}
	var dec decoder
	o := dec.object(doc, "", []string{"v", "generation", "seats", "refused"}, "inbound", "told")
	r := Report{V: o.integer("v"), Generation: o.integer("generation"), Seats: []ReportSeat{}, Refused: []ReportRefused{}}
	if o.has("inbound") {
		inbound := o.boolean("inbound")
		r.Inbound = &inbound
	}
	for _, s := range o.objects("seats", maxSeats, "team", "seat", "handle", "state") {
		r.Seats = append(r.Seats, ReportSeat{Team: s.str("team"), Seat: s.str("seat"), Handle: s.str("handle"), State: s.str("state")})
	}
	for _, x := range o.objects("refused", maxSeats, "team", "seat", "why", "holder") {
		r.Refused = append(r.Refused, ReportRefused{Team: x.str("team"), Seat: x.str("seat"), Why: x.str("why"), Holder: x.str("holder")})
	}
	if o.has("told") {
		t := o.child("told", "generation", "roles")
		r.Told = &Told{Generation: t.integer("generation"), Roles: []ToldEntry{}}
		for _, e := range t.objects("roles", maxSeats, "team", "handle", "role") {
			r.Told.Roles = append(r.Told.Roles, ToldEntry{Team: e.str("team"), Handle: e.str("handle"), Role: e.str("role")})
		}
	}
	return r, dec.err
}

// Holds is whether the report shows the seat held.
func (r Report) Holds(team, seat string) bool {
	for _, s := range r.Seats {
		if s.Team == team && s.Seat == seat && s.State == stateHeld {
			return true
		}
	}
	return false
}

// ToldRole is the role the session was told it has in team as handle; "" when it was told
// none (it explains, it is never authority).
func (r Report) ToldRole(team, handle string) string {
	if r.Told == nil {
		return ""
	}
	for _, x := range r.Told.Roles {
		if x.Team == team && x.Handle == handle {
			return x.Role
		}
	}
	return ""
}

// Why is the keeper's reason for not holding a desired seat, in words; "" when it gave none.
func (r Report) Why(team, seat string) string {
	for _, x := range r.Refused {
		if x.Team == team && x.Seat == seat {
			if x.Holder != noHolder {
				return x.Why + " (held by a " + x.Holder + " lock)"
			}
			return x.Why
		}
	}
	return ""
}

// check judges the values against section 3.2's grammar and closed sets.
func (r Report) check() error {
	if r.V != 1 || r.Generation < 0 {
		return fmt.Errorf("v %d is not 1, or generation %d is negative", r.V, r.Generation)
	}
	for _, s := range r.Seats {
		if !s.valid() {
			return fmt.Errorf("the seat %q@%q (%q, %q) is not a team's seat held by a handle", s.Seat, s.Team, s.Handle, s.State)
		}
	}
	for _, x := range r.Refused {
		if !x.valid() {
			return fmt.Errorf("the refusal of %q@%q (%q, %q) is not a team's seat, a reason and a holder", x.Seat, x.Team, x.Why, x.Holder)
		}
	}
	if r.Told == nil {
		return nil
	}
	if r.Told.Generation < 0 {
		return fmt.Errorf("told: generation %d is negative", r.Told.Generation)
	}
	for _, e := range r.Told.Roles {
		if !e.valid() {
			return fmt.Errorf("told: %q as %q in %q is not a team, a handle and a role", e.Role, e.Handle, e.Team)
		}
	}
	return nil
}

func (s ReportSeat) valid() bool {
	return teamName.MatchString(s.Team) && seatName.MatchString(s.Seat) && handleName.MatchString(s.Handle) && s.State == stateHeld
}

func (x ReportRefused) valid() bool {
	return teamName.MatchString(x.Team) && seatName.MatchString(x.Seat) && whyName.MatchString(x.Why) &&
		(x.Holder == noHolder || holderName.MatchString(x.Holder))
}

func (e ToldEntry) valid() bool {
	return teamName.MatchString(e.Team) && handleName.MatchString(e.Handle) && roleName.MatchString(e.Role)
}
