package state

import (
	"slices"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// MaxSession bounds session.json: the longest paths and every team's role fit in half of it.
const MaxSession = 8 << 10

const sessionFile = "session.json"

var roles = []string{proto.RoleCoordinator, proto.RoleWorker, proto.RoleProbe}

// Session is B 2.3.3's session.json, the link's one home of the session's facts. Until the
// first SessionStart the session's id, source, transcript, socket and Claude Code process are
// all null; afterwards all are set.
type Session struct {
	V          int         `json:"v"`
	Run        paths.RunID `json:"run"`
	SessionID  *string     `json:"session_id"`
	Source     *string     `json:"source"`
	Transcript *string     `json:"transcript"`
	Socket     *string     `json:"socket"`
	Claude     *Process    `json:"claude"`
	EndReason  *string     `json:"end_reason"`
	StopGuard  StopGuard   `json:"stop_guard"`
	Told       Told        `json:"told"`
	FrameTold  bool        `json:"frame_told"`
	Unusable   Unusable    `json:"unusable"`
}

// StopGuard is the Stop hook's state: the digest of what it last held the session for, and
// when the delivery failed or found no waker.
type StopGuard struct {
	PendingDigest *string `json:"pending_digest"`
	FailureMS     *int64  `json:"failure_ms"`
	NoWakerMS     *int64  `json:"no_waker_ms"`
}

// Told is what the session has been told: the desired generation whose membership NOTICE
// reached it, and each team's role last announced (B 2.10).
type Told struct {
	Generation int64                   `json:"generation"`
	Roles      map[paths.TeamID]string `json:"roles"`
}

// Unusable counts the inputs the link could not use (B 2.7.2, 2.8).
type Unusable struct {
	HookStdin int64 `json:"hook_stdin"`
	Render    int64 `json:"render"`
}

// Started is whether the session has had its first SessionStart.
func (s Session) Started() bool { return s.SessionID != nil }

// Encode is the file's bytes.
func (s Session) Encode() ([]byte, error) {
	s.V = Version
	return encode(sessionFile, s, DecodeSession)
}

// DecodeSession reads session.json strictly.
func DecodeSession(data []byte) (Session, error) {
	var s Session
	if _, err := decodeStrict(sessionFile, data, MaxSession, &s, func(c *rules) { s.check(c) }); err != nil {
		return Session{}, err
	}
	return s, nil
}

func (s Session) check(c *rules) {
	set := []bool{s.SessionID != nil, s.Source != nil, s.Transcript != nil, s.Socket != nil, s.Claude != nil}
	c.need(!slices.Contains(set, !s.Started()), "session_id, source, transcript, socket and claude are not all null or all set")
	if s.Started() && s.Source != nil && s.Transcript != nil && s.Socket != nil && s.Claude != nil {
		c.need(validSessionID(*s.SessionID), "session_id is not a session id")
		c.need(slices.Contains(sources, *s.Source), "source is not startup, resume, clear or compact")
		c.need(validTranscript(*s.Transcript), "transcript is not an absolute .jsonl path")
		c.need(s.Claude.valid(), "claude is not a process")
		c.need(validSocket(*s.Socket, s.Claude.PID), "socket is not claude's inbox socket")
	}
	c.need(optionalOf(s.EndReason, endReasons), "end_reason is not exit, logout, other or null")
	c.need(s.Started() || s.EndReason == nil && !s.FrameTold, "a session not started has an end_reason or was told its frame")
	g := s.StopGuard
	c.need(g.PendingDigest == nil || sha256Re.MatchString(*g.PendingDigest), "stop_guard.pending_digest is not a digest or null")
	c.need(optionalMS(g.FailureMS) && optionalMS(g.NoWakerMS), "a stop_guard time is not a time or null")
	c.need(s.Told.Generation >= 0, "told.generation is negative")
	c.need(s.Told.Roles != nil && len(s.Told.Roles) <= maxTeams, "told.roles is not a map of at most 64 teams")
	for _, role := range s.Told.Roles {
		c.need(slices.Contains(roles, role), "a told role is not coordinator, worker or probe")
	}
	c.need(s.Unusable.HookStdin >= 0 && s.Unusable.Render >= 0, "an unusable count is negative")
}
