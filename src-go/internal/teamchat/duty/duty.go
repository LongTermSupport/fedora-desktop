// Package duty is a v2 seat's duty (Plan 00209 PROPOSAL-seat-kinds.md, owner decisions OD1-OD5):
// what a seat is for in one team, beside its wire role (coordinator or worker) and its member
// kind (the transport). It is a host-side fact and never travels on the Matrix wire: the host
// derives it from the team's applied `coordinator` and `duties` and hands it to the session in
// the desired membership (OD2 (a)).
//
// The set is closed: coordinator, worker and host (OD4). The team's coordinator handle is
// coordinator, a host member (`+<site>.host`) is host, every other member is worker; an entry of
// the team's `duties` overrides that default, and a host member is never the coordinator (OD1).
package duty

import (
	"slices"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// Duty is a seat's duty in one team.
type Duty string

// The duties.
const (
	Coordinator Duty = "coordinator"
	Worker      Duty = "worker"
	Host        Duty = "host"
)

// All is the closed set, in the order a message lists it.
var All = []Duty{Coordinator, Worker, Host}

// hostType is the handle type of a member on a host (A 2.3).
const hostType = "host"

// Parse is s as a duty; false for anything outside the set.
func Parse(s string) (Duty, bool) {
	d := Duty(s)
	return d, slices.Contains(All, d)
}

// Valid is whether d is one of the set.
func (d Duty) Valid() bool { return slices.Contains(All, d) }

// HostHandle is whether handle is an agent handle of the host type, `<repo>.<seat>+<site>.host`.
func HostHandle(handle string) bool {
	h, ok := proto.ParseHandle(handle)
	return ok && h.Type == hostType
}

// Of is handle's duty in a team whose coordinator is coordinator ("" for none) and whose
// declared overrides are explicit: the override, else coordinator for the coordinator, host for
// a host member, worker for any other.
func Of(handle, coordinator string, explicit map[string]Duty) Duty {
	if d, ok := explicit[handle]; ok {
		return d
	}
	switch {
	case handle == coordinator && coordinator != "":
		return Coordinator
	case HostHandle(handle):
		return Host
	}
	return Worker
}
