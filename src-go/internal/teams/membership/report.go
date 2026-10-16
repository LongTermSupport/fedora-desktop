package membership

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	Told       json.RawMessage `json:"told,omitempty"`
}

// ReportPath is where a session's keeper writes its report, beside the checkout's seats.
func ReportPath(checkout, session string) string {
	return filepath.Join(checkout, ".claude", "ccy", "pingbus", "sessions", session, "applied.json")
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
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var r Report
	if err := dec.Decode(&r); err != nil || r.V != 1 || len(data) > maxDesiredBytes {
		return Report{}, fmt.Errorf("the seat keeper's report %s is not what a keeper writes: %w", path, errors.Join(err, errNotAReport))
	}
	return r, nil
}

var errNotAReport = errors.New("not a version 1 report")

// Holds is whether the report shows the seat held.
func (r Report) Holds(team, seat string) bool {
	for _, s := range r.Seats {
		if s.Team == team && s.Seat == seat && s.State == "held" {
			return true
		}
	}
	return false
}

// told is section 3.2's told: the generation and the roles the session was told.
type told struct {
	Roles []struct {
		Team   string `json:"team"`
		Handle string `json:"handle"`
		Role   string `json:"role"`
	} `json:"roles"`
}

// ToldRole is the role the session was told it has in team as handle; "" when it was told
// none, or its told cannot be read (it explains, it is never authority).
func (r Report) ToldRole(team, handle string) string {
	var t told
	if len(r.Told) == 0 || json.Unmarshal(r.Told, &t) != nil {
		return ""
	}
	for _, x := range t.Roles {
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
			if x.Holder != "" && x.Holder != "-" {
				return x.Why + " (held by a " + x.Holder + " lock)"
			}
			return x.Why
		}
	}
	return ""
}
