package proto

import "slices"

// Need is whether a ping field is required, forbidden or optional for a verb.
type Need int

// The three needs of A 8.1's table.
const (
	Forbidden Need = iota
	Required
	Optional
)

// The verbs of A 8.1 that this package names.
const (
	VerbHalt    = "halt"
	VerbCheck   = "check"
	VerbAck     = "ack"
	VerbNack    = "nack"
	VerbDone    = "done"
	VerbBlocked = "blocked"
)

// VerbRule is one row of A 8.1's verb table. Forms nil means any form; "Reply relation" is
// the answer form of A 7.2.
type VerbRule struct {
	Ref           Need
	Forms         []string
	Relation      Need
	ExpectsAnswer bool
	Senders       []SenderClass
	External      bool
}

// AllowsForm is whether a reference of this form may go with the verb.
func (v VerbRule) AllowsForm(form string) bool {
	return v.Ref != Forbidden && (v.Forms == nil || slices.Contains(v.Forms, form))
}

// SentBy is whether a sender of this class may send the verb.
func (v VerbRule) SentBy(c SenderClass) bool { return slices.Contains(v.Senders, c) }

var (
	coordinatorOnly   = []SenderClass{SenderCoordinator}
	coordinatorWorker = []SenderClass{SenderCoordinator, SenderWorker}
)

// Verbs is A 8.1's closed verb table. "Any role" is a coordinator or a worker: the probe sends
// `check` and nothing else (A 6.7).
var Verbs = map[string]VerbRule{
	"fetch":     {Required, []string{RefPath, RefCommit}, Forbidden, true, coordinatorOnly, true},
	"sync":      {Required, []string{RefPath, RefCommit}, Forbidden, true, coordinatorOnly, false},
	"review":    {Required, []string{RefPath, RefCommit, RefPR}, Forbidden, true, coordinatorWorker, true},
	"run-qa":    {Required, []string{RefCommit, RefPR}, Forbidden, true, coordinatorWorker, false},
	VerbHalt:    {Forbidden, nil, Forbidden, true, coordinatorOnly, false},
	VerbCheck:   {Forbidden, nil, Forbidden, true, []SenderClass{SenderProbe}, false},
	VerbAck:     {Forbidden, nil, Required, false, coordinatorWorker, false},
	VerbNack:    {Optional, nil, Required, false, coordinatorWorker, true},
	VerbDone:    {Required, nil, Optional, false, coordinatorWorker, true},
	VerbBlocked: {Required, nil, Optional, false, coordinatorWorker, true},
}

// IsAnswerVerb is whether a verb can answer a ping (A 8.2).
func IsAnswerVerb(verb string) bool {
	return verb == VerbAck || verb == VerbNack || verb == VerbDone || verb == VerbBlocked
}

// AddressableFor is whether id may be in the `to` of a ping with this verb (A 8.1): a
// non-parked coordinator or worker; for an answer verb also a listed human, and the probe
// when the answer is to its own `check` (probeCheck); for `check`, also the probe itself.
func (r Roster) AddressableFor(verb, id string, probeCheck bool) bool {
	if r.AddressableAgent(id) {
		return true
	}
	isProbe := r.Members[id].Role == RoleProbe
	switch {
	case IsAnswerVerb(verb):
		return r.IsHuman(id) || (isProbe && probeCheck)
	case verb == VerbCheck:
		return isProbe
	}
	return false
}

// timeoutGraceMs is one long-poll interval: an answer the server already holds has had one sync
// to arrive (A 8.3).
const timeoutGraceMs = 30_000

// answerTimeoutS is the verb's timeout: halt_ack_timeout_s for `halt`, ack_timeout_s for the
// rest that expect an answer; false for a verb that expects none.
func answerTimeoutS(verb string, l Limits) (int64, bool) {
	if !Verbs[verb].ExpectsAnswer {
		return 0, false
	}
	if verb == VerbHalt {
		return l.HaltAckTimeoutS, true
	}
	return l.AckTimeoutS, true
}

// PingDeadlineMs is a ping's deadline: its own origin_server_ts plus its verb's timeout
// (A 8.2); false for a verb that expects no answer.
func PingDeadlineMs(originTs int64, verb string, l Limits) (int64, bool) {
	s, ok := answerTimeoutS(verb, l)
	return originTs + s*1000, ok
}

// AnswerOnTime is whether an answer's origin_server_ts is at or before the deadline (A 8.2).
func AnswerOnTime(answerTs, deadlineMs int64) bool { return answerTs <= deadlineMs }

// TimeoutDue is whether the member's clock has passed the deadline and its grace (A 8.3).
func TimeoutDue(deadlineMs, nowMs int64) bool { return nowMs > deadlineMs+timeoutGraceMs }

// TimeoutKey is a TIMEOUT's key, `<ping event ID>~<addressee localpart>` (A 8.3).
func TimeoutKey(pingID, localpart string) string { return pingID + "~" + localpart }

// PingStale is whether a ping is older than its verb's timeout, ack_timeout_s for a verb that
// expects no answer, when first judged (A 8.5). An unknown verb is never stale: it is dropped
// `verb` first.
func PingStale(originTs int64, verb string, nowMs int64, l Limits) bool {
	if _, known := Verbs[verb]; !known {
		return false
	}
	s, ok := answerTimeoutS(verb, l)
	if !ok {
		s = l.AckTimeoutS
	}
	return nowMs-originTs > s*1000
}

// TextStale is whether a human message or agent text is older than human_max_age_s (A 8.5).
func TextStale(originTs, nowMs int64, l Limits) bool {
	return nowMs-originTs > l.HumanMaxAgeS*1000
}
