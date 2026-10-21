package proto

import (
	"errors"
	"slices"
)

// The roles (SPEC P18) and the sender classes beside them.
const (
	RoleCoordinator = "coordinator"
	RoleWorker      = "worker"
	SenderHuman     = "human"
	SenderAddressed = "addressed"
)

// ErrUnknownVerb is a verb outside the closed set, asked of a function that needs one.
var ErrUnknownVerb = errors.New("not a protocol verb")

// Need is whether a ping key is required, optional or forbidden for a verb.
type Need string

// The needs of §5's table.
const (
	Required  Need = "required"
	Optional  Need = "optional"
	Forbidden Need = "forbidden"
)

// VerbRule is one row of §5's table (SPEC P19).
type VerbRule struct {
	Ref      Need
	Forms    []string
	Re       Need
	Ack      bool
	Senders  []string
	External bool
}

// The reference forms of §6.
const (
	FormPath   = "path"
	FormCommit = "commit"
	FormPR     = "pr"
	FormIssue  = "issue"
)

// The verbs of §5.
const (
	VerbFetch   = "fetch"
	VerbSync    = "sync"
	VerbReview  = "review"
	VerbRunQA   = "run-qa"
	VerbHalt    = "halt"
	VerbAck     = "ack"
	VerbNack    = "nack"
	VerbDone    = "done"
	VerbBlocked = "blocked"
)

var (
	refForms   = []string{FormPath, FormCommit, FormPR, FormIssue}
	coord      = []string{RoleCoordinator}
	coordWork  = []string{RoleCoordinator, RoleWorker}
	addressed  = []string{SenderAddressed}
	toHumans   = []string{VerbAck, VerbNack, VerbDone, VerbBlocked}
	knownRoles = []string{RoleCoordinator, RoleWorker}
)

// VerbOrder is the closed verb set in spec order; Verbs is its table.
var (
	VerbOrder = []string{
		VerbFetch, VerbSync, VerbReview, VerbRunQA, VerbHalt, VerbAck, VerbNack, VerbDone, VerbBlocked,
	}
	Verbs = map[string]VerbRule{
		VerbFetch:   {Required, []string{FormPath, FormCommit}, Forbidden, true, coord, true},
		VerbSync:    {Required, []string{FormPath, FormCommit}, Forbidden, true, coord, false},
		VerbReview:  {Required, []string{FormPath, FormCommit, FormPR}, Forbidden, true, coordWork, true},
		VerbRunQA:   {Required, []string{FormCommit, FormPR}, Forbidden, true, coordWork, false},
		VerbHalt:    {Forbidden, nil, Forbidden, true, coord, false},
		VerbAck:     {Forbidden, nil, Required, false, addressed, false},
		VerbNack:    {Optional, refForms, Required, false, addressed, true},
		VerbDone:    {Required, refForms, Optional, false, coordWork, true},
		VerbBlocked: {Required, refForms, Optional, false, coordWork, true},
	}
)

// ToHumans is SPEC P20: whether the verb's to may name a listed human.
func ToHumans(verb string) bool { return slices.Contains(toHumans, verb) }

// NormaliseRole is SPEC P18: orchestrator reads as coordinator; anything else is unchanged.
func NormaliseRole(role string) string {
	if role == "orchestrator" {
		return RoleCoordinator
	}
	return role
}

// Coordinators is SPEC P21: the coordinator user IDs, sorted.
func Coordinators(ctx *Context) []string {
	out := []string{}
	for user, role := range ctx.Roles {
		if role == RoleCoordinator {
			out = append(out, user)
		}
	}
	slices.Sort(out)
	return out
}

// WithCoordinatorCopy is SPEC P21: a worker writing to a listed human copies the coordinators.
func WithCoordinatorCopy(to []string, sender string, ctx *Context) []string {
	out := slices.Clone(to)
	if ctx.Roles[sender] != RoleWorker || !slices.ContainsFunc(to, ctx.IsHuman) {
		return out
	}
	for _, user := range Coordinators(ctx) {
		if !slices.Contains(to, user) {
			out = append(out, user)
		}
	}
	return out
}

// CopyOf is SPEC P21: the listed humans of to, sorted, when the receiver is a coordinator.
func CopyOf(to []string, self string, ctx *Context) []string {
	out := []string{}
	if ctx.Roles[self] != RoleCoordinator {
		return out
	}
	for _, user := range to {
		if ctx.IsHuman(user) {
			out = append(out, user)
		}
	}
	slices.Sort(out)
	return out
}

// SenderClass is SPEC P22: human, the role an agent holds, or "" (a non-string included).
func SenderClass(user any, ctx *Context) string {
	s, ok := user.(string)
	switch {
	case !ok:
		return ""
	case ctx.IsHuman(s):
		return SenderHuman
	case slices.Contains(knownRoles, ctx.Roles[s]):
		return ctx.Roles[s]
	}
	return ""
}

// Addressable is SPEC P22: a role-holding agent; or a listed human for agent text (verb "")
// or a P20 verb.
func Addressable(user, verb string, ctx *Context) bool {
	if slices.Contains(knownRoles, ctx.Roles[user]) && IsAgentUserID(user, ctx.ServerName) {
		return true
	}
	return (verb == "" || ToHumans(verb)) && ctx.IsHuman(user) && IsHumanUserID(user, ctx.ServerName)
}

// CheckRole is SPEC P23.
func CheckRole(verb, sender string, ctx *Context) error {
	rule, ok := Verbs[verb]
	if !ok {
		return refuse(ReasonVerb)
	}
	class := SenderClass(sender, ctx)
	if !slices.Contains(knownRoles, class) {
		return refuse(ReasonRole)
	}
	if !slices.Contains(rule.Senders, SenderAddressed) && !slices.Contains(rule.Senders, class) {
		return refuse(ReasonRole)
	}
	return nil
}

// CheckTextSender is SPEC P23 for agent text: a role holder.
func CheckTextSender(sender string, ctx *Context) error {
	if !slices.Contains(knownRoles, SenderClass(sender, ctx)) {
		return refuse(ReasonRole)
	}
	return nil
}
