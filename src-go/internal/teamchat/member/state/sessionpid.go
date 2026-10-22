package state

import (
	"slices"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// MaxSessionPID bounds session.pid.
const MaxSessionPID = 1 << 10

const sessionPIDFile = "session.pid"

// How a session was launched: `run --claude` or `run --plain` (B 2.4.1 step 5).
const (
	LaunchClaude = "claude"
	LaunchPlain  = "plain"
)

// SessionPID is B 2.4.1 step 4's session.pid: the keeper and, from step 7, the session it
// started, each with its start time so a reused pid is never taken for either; the kit, the
// launch kind, the inbound decision and the member kind. The kit sweep and the runtime sweep
// read it, the host's through the container's root fd.
type SessionPID struct {
	V       int          `json:"v"`
	Run     paths.RunID  `json:"run"`
	Keeper  Process      `json:"keeper"`
	Session *Process     `json:"session"`
	Kit     paths.Digest `json:"kit"`
	Launch  string       `json:"launch"`
	Inbound string       `json:"inbound"`
	Kind    string       `json:"kind"`
}

// Encode is the file's bytes.
func (p SessionPID) Encode() ([]byte, error) {
	p.V = Version
	return encode(sessionPIDFile, p, DecodeSessionPID)
}

// DecodeSessionPID reads session.pid strictly.
func DecodeSessionPID(data []byte) (SessionPID, error) {
	var p SessionPID
	if _, err := decodeStrict(sessionPIDFile, data, MaxSessionPID, &p, func(c *rules) { p.check(c) }); err != nil {
		return SessionPID{}, err
	}
	return p, nil
}

func (p SessionPID) check(c *rules) {
	c.need(p.Keeper.valid(), "keeper is not a process")
	c.need(p.Session == nil || p.Session.valid() && p.Session.PID != p.Keeper.PID, "session is not a process other than the keeper, or null")
	c.need(p.Launch == LaunchClaude || p.Launch == LaunchPlain, "launch is not claude or plain")
	c.need(slices.Contains(inbounds, p.Inbound), "inbound is not accept or refuse")
	c.need(slices.Contains(memberKinds, p.Kind), "kind is not ccy, lxc, host or other")
}
