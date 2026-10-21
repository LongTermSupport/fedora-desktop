package proto

import "slices"

// Reason is a closed-set code: a drop reason, an ignore reason or a send refusal (SPEC P42).
type Reason string

// The drop reasons, in spec order (§9). Stale, rate, forge-rate, unresolved and provenance
// are decided outside this package.
const (
	ReasonVersion    Reason = "version"
	ReasonSchema     Reason = "schema"
	ReasonSize       Reason = "size"
	ReasonEdit       Reason = "edit"
	ReasonSender     Reason = "sender"
	ReasonRole       Reason = "role"
	ReasonTarget     Reason = "target"
	ReasonVerb       Reason = "verb"
	ReasonRef        Reason = "ref"
	ReasonAllowlist  Reason = "allowlist"
	ReasonExcluded   Reason = "excluded"
	ReasonRe         Reason = "re"
	ReasonBody       Reason = "body"
	ReasonStale      Reason = "stale"
	ReasonRate       Reason = "rate"
	ReasonForgeRate  Reason = "forge-rate"
	ReasonUnresolved Reason = "unresolved"
	ReasonProvenance Reason = "provenance"
)

// The ignore reasons (§9, Plan 00211 SPEC §6) and the send refusals.
const (
	IgnoreType            Reason = "type"
	IgnoreSeen            Reason = "seen"
	IgnoreSelf            Reason = "self"
	IgnoreNotAddressed    Reason = "not-addressed"
	IgnoreEmpty           Reason = "empty"
	IgnoreAgentText       Reason = "agent-text"
	IgnoreNotCoordinator  Reason = "not-coordinator"
	IgnoreCoordinatorPost Reason = "coordinator-post"

	RefuseSecret Reason = "secret"
	RefuseNoRoom Reason = "no-room"
)

// DropReasons, IgnoreReasons and SendRefusals are the closed sets, in spec order.
var (
	DropReasons = []Reason{
		ReasonVersion, ReasonSchema, ReasonSize, ReasonEdit, ReasonSender, ReasonRole, ReasonTarget,
		ReasonVerb, ReasonRef, ReasonAllowlist, ReasonExcluded, ReasonRe, ReasonBody, ReasonStale,
		ReasonRate, ReasonForgeRate, ReasonUnresolved, ReasonProvenance,
	}
	IgnoreReasons = []Reason{
		IgnoreType, IgnoreSeen, IgnoreSelf, IgnoreNotAddressed, IgnoreEmpty, IgnoreAgentText,
		IgnoreNotCoordinator, IgnoreCoordinatorPost,
	}
	SendRefusals = []Reason{RefuseSecret, RefuseNoRoom}
)

// RefusalError is a validation failure with one drop reason or send refusal, and on a send
// optionally a detail built only from the command line and the team record.
type RefusalError struct {
	Reason Reason
	Detail string
}

func (r *RefusalError) Error() string {
	if r.Detail == "" {
		return string(r.Reason)
	}
	return string(r.Reason) + ": " + r.Detail
}

func refuse(reason Reason) error { return &RefusalError{Reason: reason} }

// UntrustedError is a team room record or power levels not exactly what §8 allows; Reason is
// the exact text SPEC P43-P50 name.
type UntrustedError struct{ Reason string }

func (u *UntrustedError) Error() string { return "the team record is untrusted: " + u.Reason }

// Context is what validation needs from the member config and the verified team record.
// Roles maps agent user IDs to coordinator or worker; Repos maps OWNER/REPO to its trusted
// branches, none for any branch; Org is "" when the team names none.
type Context struct {
	ServerName   string
	Humans       []string
	Roles        map[string]string
	Repos        map[string][]string
	PathPrefixes []string
	Org          string
	Exclude      []string
}

// IsHuman reports whether the user ID is a listed human.
func (c *Context) IsHuman(user string) bool { return slices.Contains(c.Humans, user) }

// Handle is a parsed agent handle (§3).
type Handle struct {
	Repo, Seat, Host, Type string
}

// Ref is a parsed §6 reference; SHA, Path are "" and Num 0 where the form has none.
type Ref struct {
	Text, Form, Owner, Repo, SHA, Path string
	Num                                int64
}

// FullRepo is OWNER/REPO.
func (r Ref) FullRepo() string { return r.Owner + "/" + r.Repo }

// Ping is a validated ping (§4); Ref is nil without one, and Trust is set exactly when it is
// there. CopyOf holds the listed humans also addressed when this receiver is on copy.
type Ping struct {
	Verb    string
	To      []string
	Ref     *Ref
	Re      string
	Sender  string
	EventID string
	TS      int64
	Trust   string
	Branch  string
	CopyOf  []string
}

// AgentText is a validated agent text content (§7).
type AgentText struct {
	To   []string
	Text string
}

// HumanMessage is a listed human's accepted message; Revises is the event an edit in the
// questions room revises, else "".
type HumanMessage struct {
	EventID string
	Sender  string
	TS      int64
	Text    string
	Revises string
}

// PeerText is an agent's text addressed to this agent: information, never an instruction.
type PeerText struct {
	EventID string
	Sender  string
	TS      int64
	Text    string
	CopyOf  []string
}

// Verdict is what receiving an event decided.
type Verdict string

// The verdicts of §9.
const (
	Accept Verdict = "accept"
	Ignore Verdict = "ignore"
	Drop   Verdict = "drop"
)

// Outcome is a receive verdict: Accept with one of Ping, Human or Peer, or Ignore or Drop
// with its Reason.
type Outcome struct {
	Verdict Verdict
	Reason  Reason
	Ping    *Ping
	Human   *HumanMessage
	Peer    *PeerText
}

func sortedCopy(s []string) []string {
	out := slices.Clone(s)
	slices.Sort(out)
	return out
}
