package engine

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/failpoint"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/cli"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// maxText is A 7.3's bound on a text; the other send bounds are the store's caps
// (state.MaxPingTargets, state.MaxSending, state.MaxPings).
const maxText = 4096

const (
	msgNoText          exits.Template = "say reads its text on stdin, and none was given"
	msgEmptyText       exits.Template = "the text is empty"
	msgTextUTF8        exits.Template = "the text is not UTF-8"
	msgTextSize        exits.Template = "the text is over 4096 bytes"
	msgPingStdin       exits.Template = "ping reads no text; send text with say"
	msgPingAnswer      exits.Template = "{} answers a ping: use teamchat reply <ping id> --verb {}"
	msgAddressees      exits.Template = "name the addressees with exactly one of --to, --to-coordinator"
	msgUnknownMember   exits.Template = "no team held has a member {}; run status for the members"
	msgNoCoordinator   exits.Template = "team {} has no coordinator; run status for the members"
	msgAmbiguousMember exits.Template = "{} is more than one member of team {}; use the full handle"
	msgNoRoster        exits.Template = "team {} has not been verified yet, so its members are not known; the link keeps trying"
	msgNoStore         exits.Template = "team {} has not synced yet; the link keeps trying"
	msgStopped         exits.Template = "team {} is stopped; run status and tell a human"
	msgTooMany         exits.Template = "a ping names at most {} addressees"
	msgRefused         exits.Template = "the send was refused ({}); fix the message"
	msgRole            exits.Template = "this member's role may not send that ({})"
	msgDuplicate       exits.Template = "it was already sent within 60 s; do not resend"
	msgRate            exits.Template = "team {}'s send rate is spent; wait, do not loop"
	msgProvenance      exits.Template = "the reference's provenance failed at the forge"
	msgUnresolved      exits.Template = "the reference does not resolve at the forge; push first"
	msgForgeRate       exits.Template = "the forge's rate limit is spent; wait, do not loop"
	msgForgeDown       exits.Template = "the forge did not answer; try again later"
	msgForgeLogin      exits.Template = "the forge check needs gh's login; tell a human"
	msgSendingFull     exits.Template = "{} sends already wait for team {}'s homeserver; run status"
	msgOutboxFull      exits.Template = "team {} already has {} pings awaiting answers; run status"
	msgStoreWrite      exits.Template = "the store of team {} was not written; run status and tell a human"
	msgServerRefused   exits.Template = "the homeserver refused the send; tell a human"
)

// sendTeam is the team a send goes to, as its op resolved it: its goroutine, the roster it was
// resolved against, and this member in it.
type sendTeam struct {
	id      paths.TeamID
	tm      team.Team
	roster  proto.Roster
	self    string
	anchors proto.Anchors
}

func (st sendTeam) namer() namer {
	return namer{server: st.anchors.Server, roster: st.roster, verified: true}
}

// composeFunc builds one send with the composer the op's steps give it.
type composeFunc func(c proto.Composer) (proto.Outgoing, error)

// sendResult is a waiting op's answer from the sender: the posted event, or the failure that
// removed the send.
type sendResult struct {
	id  string
	err error
}

// say is SD2's text: stdin to the resolved addressees in main.
func (e *Engine) say(ctx context.Context, a wire.SayArgs, stdin *string, out RecordWriter) wire.Done {
	text, err := stdinText(a.Team, stdin, msgNoText)
	if err != nil {
		return doneOf(err)
	}
	st, to, err := e.addressed(ctx, a.Team, a.To, a.ToCoordinator)
	if err != nil {
		return doneOf(err)
	}
	return e.send(ctx, st, state.SendText, func(c proto.Composer) (proto.Outgoing, error) {
		return c.Text(proto.Text{To: to, Text: text}, "", text)
	}, out)
}

// stdinText is the text of say or a plain reply, checked (n3d-D28: the client removed one
// trailing newline); none is 64 with noText.
func stdinText(t *paths.TeamID, stdin *string, noText exits.Template) (string, error) {
	switch {
	case stdin == nil:
		return "", exits.New(exits.CodeUsage, t, noText)
	case *stdin == "":
		return "", exits.New(exits.CodeEmpty, t, msgEmptyText)
	case !utf8.ValidString(*stdin):
		return "", exits.New(exits.CodeSchema, t, msgTextUTF8)
	case len(*stdin) > maxText:
		return "", exits.New(exits.CodeSize, t, msgTextSize)
	}
	return *stdin, nil
}

// ping is SD2's ping: a verb that is not an answer, with its reference, to the resolved
// addressees in main (n3d-D15).
func (e *Engine) ping(ctx context.Context, a wire.PingArgs, stdin *string, out RecordWriter) wire.Done {
	switch {
	case stdin != nil:
		return doneOf(exits.New(exits.CodeUsage, a.Team, msgPingStdin))
	case a.Verb == proto.VerbAck || a.Verb == proto.VerbNack:
		return doneOf(exits.New(exits.CodeUsage, a.Team, msgPingAnswer, exits.Words(a.Verb), exits.Words(a.Verb)))
	}
	st, to, err := e.addressed(ctx, a.Team, a.To, a.ToCoordinator)
	if err != nil {
		return doneOf(err)
	}
	p := proto.Ping{Verb: a.Verb, To: to}
	if a.Ref != nil {
		p.Ref = *a.Ref
	}
	if a.Branch != nil {
		p.Branch = *a.Branch
	}
	body := pingBody(p, st.namer())
	return e.send(ctx, st, state.SendPing, func(c proto.Composer) (proto.Outgoing, error) {
		return c.Ping(p, "", body)
	}, out)
}

// pingBody is a ping's display text (A 12.1): `<verb> [<ref>] -> <names>`.
func pingBody(p proto.Ping, n namer) string {
	parts := []string{p.Verb}
	if p.Ref != "" {
		parts = append(parts, p.Ref)
	}
	to := make([]string, 0, len(p.To))
	for _, id := range p.To {
		to = append(to, n.name(id))
	}
	return strings.Join(parts, " ") + " -> " + strings.Join(to, ", ")
}

// sendCandidate is one held team as the resolver reads it on the state goroutine.
type sendCandidate struct {
	id      paths.TeamID
	tm      team.Team
	cached  *proto.Roster
	self    string
	anchors proto.Anchors
}

// roster is the team's verified roster, else its cached one (n3d-D3); off the state goroutine.
func (c sendCandidate) roster() (proto.Roster, bool) {
	if c.tm != nil {
		if r, ok := c.tm.Notices().Roster(); ok {
			return r, true
		}
	}
	if c.cached != nil {
		return *c.cached, true
	}
	return proto.Roster{}, false
}

// resolution is one team's reading of a send's names.
type resolution struct {
	st        sendTeam
	to        []string
	missing   string // a name the team has no member for; "" with noCoord for --to-coordinator
	noCoord   bool
	ambiguous string // a name that is two members of the team
}

func (r resolution) whole() bool { return r.missing == "" && !r.noCoord }

// addressed resolves a send's team and addressees (SD2, CL6): with --team that team, else the
// one team in which every name is a member; a human by localpart, an agent by its short name or
// its full handle.
func (e *Engine) addressed(ctx context.Context, flag *paths.TeamID, names []string, toCoordinator bool) (sendTeam, []string, error) {
	if (len(names) > 0) == toCoordinator {
		return sendTeam{}, nil, exits.New(exits.CodeUsage, flag, msgAddressees)
	}
	cands, err := e.candidates(ctx, flag)
	if err != nil {
		return sendTeam{}, nil, err
	}
	found, first, unverified, err := resolveAll(cands, flag, names, toCoordinator)
	if err != nil {
		return sendTeam{}, nil, err
	}
	switch {
	case len(found) > 1:
		active := make([]cli.ActiveTeam, 0, len(found))
		for _, f := range found {
			active = append(active, cli.ActiveTeam{ID: f.st.id, Description: f.st.roster.Name})
		}
		_, err := cli.ResolveTeam(nil, nil, active)
		return sendTeam{}, nil, err
	case len(found) == 1 && found[0].ambiguous != "":
		f := found[0]
		return sendTeam{}, nil, exits.New(exits.CodeAmbiguousMember, &f.st.id, msgAmbiguousMember, exits.Name(f.ambiguous), exits.Team(f.st.id))
	case len(found) == 1:
		return found[0].st, found[0].to, nil
	case unverified != nil:
		// The names may be that team's, so the agent waits for it rather than fixes them.
		return sendTeam{}, nil, exits.New(exits.CodeUnreachable, unverified, msgNoRoster, exits.Team(*unverified))
	case first.noCoord:
		return sendTeam{}, nil, exits.New(exits.CodeUnknownMember, &first.st.id, msgNoCoordinator, exits.Team(first.st.id))
	}
	return sendTeam{}, nil, exits.New(exits.CodeUnknownMember, flag, msgUnknownMember, exits.Name(first.missing))
}

// resolveAll reads the names in every candidate: the teams that hold them all, the first that
// does not, and the first team skipped for having no roster (an error with --team).
func resolveAll(cands []sendCandidate, flag *paths.TeamID, names []string, toCoordinator bool) ([]resolution, *resolution, *paths.TeamID, error) {
	var (
		found      []resolution
		first      *resolution
		unverified *paths.TeamID
	)
	for _, c := range cands {
		r, ok := c.roster()
		if !ok {
			if flag != nil {
				return nil, nil, nil, exits.New(exits.CodeUnreachable, &c.id, msgNoRoster, exits.Team(c.id))
			}
			if unverified == nil {
				unverified = &c.id
			}
			continue
		}
		res := resolve(sendTeam{id: c.id, tm: c.tm, roster: r, self: c.self, anchors: c.anchors}, names, toCoordinator)
		if res.whole() {
			found = append(found, res)
		} else if first == nil {
			first = &res
		}
	}
	return found, first, unverified, nil
}

// candidates are the held teams a send may go to: the flag's, or every one.
func (e *Engine) candidates(ctx context.Context, flag *paths.TeamID) ([]sendCandidate, error) {
	var cands []sendCandidate
	if err := e.do(ctx, func() {
		for _, id := range e.order() {
			h := e.teams[id]
			if h.removed || flag != nil && *flag != id {
				continue
			}
			c := sendCandidate{id: id, tm: h.team, self: h.userID, anchors: h.anchors}
			if h.roster != nil {
				r := *h.roster
				c.cached = &r
			}
			cands = append(cands, c)
		}
	}); err != nil {
		return nil, err
	}
	if len(cands) == 0 {
		if flag != nil {
			return nil, exits.New(exits.CodeUsage, flag, msgNotHeld, exits.Team(*flag))
		}
		_, err := cli.ResolveTeam(nil, nil, nil)
		return nil, err
	}
	return cands, nil
}

// resolve is names (or the coordinator) as members of st's roster.
func resolve(st sendTeam, names []string, toCoordinator bool) resolution {
	res := resolution{st: st}
	if toCoordinator {
		c, ok := st.roster.Coordinator()
		res.to, res.noCoord = []string{c}, !ok
		return res
	}
	for _, name := range names {
		ids := members(st.roster, st.anchors.Server, name)
		switch {
		case len(ids) == 0:
			res.missing = name
			return res
		case len(ids) > 1 && res.ambiguous == "":
			res.ambiguous = name
		}
		res.to = append(res.to, ids[0])
	}
	return res
}

// members are the user ids name may mean in r: a human by localpart, an agent by its full
// handle or its short name (P69).
func members(r proto.Roster, server, name string) []string {
	var ids []string
	for _, id := range r.Humans {
		if local, ok := proto.Localpart(id, server); ok && local == name {
			ids = append(ids, id)
		}
	}
	for id := range r.Members {
		if h, ok := proto.AgentHandle(id, server); ok && (h.String() == name || h.Short() == name) {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids
}

// send is a resolved send's steps 3 to 8 (A 9.3's order, SD3): the team's stop, the compose,
// the addressee bound, the forge check, the charge in one store write, and the answer: queued
// at once for a down team or behind older sends, else posted or queued by the deadline.
func (e *Engine) send(ctx context.Context, st sendTeam, kind string, compose composeFunc, out RecordWriter) wire.Done {
	var (
		lk     storeLookup
		lkErr  error
		ending = func() wire.Done { return doneOf(exits.New(exits.CodeTimedOut, &st.id, msgTimedOut, exits.Team(st.id))) }
	)
	if err := e.do(ctx, func() { lk, lkErr = e.sendable(st.id) }); err != nil {
		return ending()
	}
	if lkErr != nil {
		return doneOf(lkErr)
	}
	c := proto.Composer{
		Server: st.anchors.Server, Self: st.self, Token: st.tm.Notices().Token(), Roster: st.roster,
		Lookup: lk.with(st.roster), NowMs: e.cfg.Clock.WallMS(),
	}
	o, err := compose(c)
	if err != nil {
		return doneOf(refusal(st.id, err))
	}
	entry, err := sendingOf(kind, o, st.roster.Limits)
	if err != nil {
		return internal(err)
	}
	if kind == state.SendPing && len(contentTo(o.Content)) > state.MaxPingTargets {
		return doneOf(exits.New(exits.CodeUsage, &st.id, msgTooMany, exits.Count(state.MaxPingTargets)))
	}
	if o.Forge != nil {
		if d, refused := e.forgeCheck(ctx, st, *o.Forge); refused {
			return d
		}
	}
	var (
		wait      chan sendResult
		queuedNow bool
		chargeErr error
	)
	if err := e.do(ctx, func() { wait, queuedNow, chargeErr = e.charge(st, c, o, &entry) }); err != nil {
		return ending()
	}
	if chargeErr != nil {
		if xe, ok := errors.AsType[*exits.Error](chargeErr); ok {
			return doneOf(xe)
		}
		e.logf("team %s: a send was not charged: %v", st.id, chargeErr)
		return doneOf(exits.New(exits.CodeStore, &st.id, msgStoreWrite, exits.Team(st.id)))
	}
	failpoint.Hit(failpoint.LinkAfterChargeBeforeSend)
	if queuedNow {
		return answerSent(out, st.id, nil)
	}
	return e.await(ctx, out, st.id, entry.Txn, wait)
}

// await is a charged send's answer: posted when the sender answers by ctx's end, else queued.
func (e *Engine) await(ctx context.Context, out RecordWriter, t paths.TeamID, txn string, wait chan sendResult) wire.Done {
	select {
	case r := <-wait:
		return e.answerResult(out, t, r)
	case <-ctx.Done():
	}
	if r, ok := e.unwait(context.WithoutCancel(ctx), t, txn, wait); ok {
		return e.answerResult(out, t, r)
	}
	return answerSent(out, t, nil)
}

func (e *Engine) answerResult(out RecordWriter, t paths.TeamID, r sendResult) wire.Done {
	if r.err != nil {
		return doneOf(r.err)
	}
	return answerSent(out, t, &r.id)
}

// answerSent is the sent record: the event id, or null for a send the link holds.
func answerSent(out RecordWriter, t paths.TeamID, id *string) wire.Done {
	if err := out.Record(records.Sent{Team: t, ID: id, Room: proto.PurposeMain, Queued: id == nil}); err != nil {
		return internal(err)
	}
	if err := out.Flush(); err != nil {
		return internal(err)
	}
	return wire.Done{}
}

// unwait takes an ended op's send off the sender's answer list; a result already given is
// still the op's. ctx must not be the op's ended one.
func (e *Engine) unwait(ctx context.Context, t paths.TeamID, txn string, wait chan sendResult) (sendResult, bool) {
	if err := e.do(ctx, func() {
		if h := e.teams[t]; h != nil {
			delete(h.waiting, txn)
		}
	}); err != nil {
		e.logf("team %s: an ended send was not taken off the sender's list: %v", t, err)
	}
	select {
	case r := <-wait:
		return r, true
	default:
		return sendResult{}, false
	}
}

// sendable is the store lookup of a team that can take a send, or why it cannot; on the state
// goroutine.
func (e *Engine) sendable(t paths.TeamID) (storeLookup, error) {
	h := e.teams[t]
	if h == nil || h.removed {
		return storeLookup{}, exits.New(exits.CodeUsage, &t, msgNotHeld, exits.Team(t))
	}
	if err := stoppedErr(h); err != nil {
		return storeLookup{}, err
	}
	if h.store == nil {
		return storeLookup{}, exits.New(exits.CodeUnreachable, &t, msgNoStore, exits.Team(t))
	}
	return lookupOf(h), nil
}

// stoppedErr is the refusal of a team stopped permanently (SY8, or its sends' stop, SD3-8c),
// nil for one that runs.
func stoppedErr(h *held) error {
	code := h.shown().Stopped
	switch {
	case h.refused:
		code = exits.CodeStore
	case h.team == nil:
		code = exits.CodeBundle
	case code == "":
		return nil
	}
	if _, known := exits.ExitOf(code); !known {
		code = exits.CodeInternal
	}
	return exits.New(code, &h.id, msgStopped, exits.Team(h.id))
}

// refusal is a composer's or Admit's refusal as the send's exit (A 7.6's send refusals, B
// 2.2.4): role for the sender's class, 15 and 14 for the gate, else 10 with its reason.
func refusal(t paths.TeamID, err error) error {
	re, ok := errors.AsType[*proto.RefusalError](err)
	if !ok {
		return err
	}
	switch re.Reason {
	case proto.ReasonSender, proto.ReasonRole:
		return exits.New(exits.CodeRole, &t, msgRole, exits.Words(re.Reason))
	case proto.ReasonDuplicate:
		return exits.New(exits.CodeDuplicate, &t, msgDuplicate)
	case proto.ReasonRate:
		return exits.New(exits.CodeRate, &t, msgRate, exits.Team(t))
	case proto.ReasonAllowlist, proto.ReasonExcluded:
		return exits.New(exits.CodeRef, &t, msgRefused, exits.Words(re.Reason))
	}
	code := exits.CodeSchema
	if exit, known := exits.ExitOf(re.Reason); known && exit == exits.ExitRefused {
		code = re.Reason
	}
	return exits.New(code, &t, msgRefused, exits.Words(re.Reason))
}

// forgeCheck is step 6 (n3d-D11), off the state goroutine; nothing is spent by a refusal. It is
// the send's answer and true for a reference refused or a check that failed. A failed check is
// logged with its cause, and answered by what it shows (n3d-D32): the request's own deadline
// ended is 4 timed-out, as every other step of a send; only a forge found unreachable is 20
// forge-down; any other error is the link's own, 1 internal.
func (e *Engine) forgeCheck(ctx context.Context, st sendTeam, need proto.ForgeCheck) (wire.Done, bool) {
	res, err := e.cfg.Forge.Check(ctx, need, st.roster.Forge)
	if err != nil {
		e.logf("team %s: a send's forge check failed: %v", st.id, err)
	}
	var refused error
	switch {
	case err == nil:
	case ctx.Err() != nil:
		refused = exits.New(exits.CodeTimedOut, &st.id, msgTimedOut, exits.Team(st.id))
	case errors.Is(err, team.ErrForgeLogin):
		refused = exits.New(exits.CodeForgeLogin, &st.id, msgForgeLogin)
	case errors.Is(err, team.ErrForgeDown):
		refused = exits.New(exits.CodeForgeDown, &st.id, msgForgeDown)
	default:
		return internal(err), true
	}
	if refused != nil {
		return doneOf(refused), true
	}
	switch res.Outcome {
	case team.ForgeProven:
		return wire.Done{}, false
	case proto.ReasonProvenance:
		refused = exits.New(exits.CodeProvenance, &st.id, msgProvenance)
	case proto.ReasonForgeRate:
		refused = exits.New(exits.CodeForgeRate, &st.id, msgForgeRate)
	default:
		refused = exits.New(exits.CodeForge, &st.id, msgUnresolved)
	}
	return doneOf(refused), true
}

// sendingFull is the refusal of a send while the store's sends in flight are at their cap.
func sendingFull(t paths.TeamID) error {
	return exits.New(exits.CodeStore, &t, msgSendingFull, exits.Count(state.MaxSending), exits.Team(t))
}

// sendingOf is o's entry in sending, its txn drawn, its charge time and tried set at the charge.
func sendingOf(kind string, o proto.Outgoing, l proto.Limits) (state.Sending, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return state.Sending{}, fmt.Errorf("engine: draw a transaction id: %w", err)
	}
	s := state.Sending{
		Txn: "tc" + hex.EncodeToString(b), Room: proto.PurposeMain, Kind: kind, Type: proto.MessageType,
		Content: o.Content.Compact(),
	}
	if kind != state.SendPing {
		return s, nil
	}
	dc := decoded(o.Content)
	if ms, ok := proto.PingDeadlineMs(0, dc.Ping.Verb, l); ok {
		s.Ping = &state.SendingPing{Verb: dc.Ping.Verb, Ref: optional(dc.Ping.Ref), To: dc.Ping.To, TimeoutMS: ms}
	}
	return s, nil
}

// decoded is content the composer built or the store holds, decoded as an agent's main event;
// the zero Content for one that does not decode, which the store's own check refuses.
func decoded(content proto.Value) proto.Content {
	dc, reason := proto.DecodeContent(content, proto.MainKeys)
	if reason != "" {
		return proto.Content{}
	}
	return dc
}

// contentTo is the addressees of a text or ping content.
func contentTo(content proto.Value) []string {
	dc := decoded(content)
	if dc.Key == proto.KeyPing {
		return dc.Ping.To
	}
	return dc.Text.To
}

// charge is step 7 on the state goroutine (SD3): the caps checked, the gate built from the store
// and Admit run, then in one store write the gate and the entry; nothing is written on a
// refusal. It is the channel the sender answers on, or queued true when the send waits behind
// a down team, older sends or the run's recovery (n3d-D19).
func (e *Engine) charge(st sendTeam, c proto.Composer, o proto.Outgoing, entry *state.Sending) (chan sendResult, bool, error) {
	h := e.teams[st.id]
	if h == nil || h.removed {
		return nil, false, exits.New(exits.CodeUsage, &st.id, msgNotHeld, exits.Team(st.id))
	}
	if err := stoppedErr(h); err != nil {
		return nil, false, err
	}
	var queuedNow bool
	err := e.changeLocked(st.id, func(h *held, s *state.Store) (bool, error) {
		switch {
		case len(s.Outbox.Sending) >= state.MaxSending:
			return false, sendingFull(st.id)
		case entry.Ping != nil && ownPings(s) >= state.MaxPings:
			return false, exits.New(exits.CodeStore, &st.id, msgOutboxFull, exits.Team(st.id), exits.Count(state.MaxPings))
		}
		now := e.cfg.Clock.WallMS()
		key := o.DuplicateKey()
		gate := sendGate(s.Outbox.Gate, key, now)
		c.Gate, c.NowMs = &gate, now
		if err := c.Admit(o); err != nil {
			return false, refusal(st.id, err)
		}
		if err := storeGate(s, gate, key, now); err != nil {
			return false, err
		}
		older := len(s.Outbox.Sending) > 0
		queuedNow = h.health.Down || older || !h.recovered
		entry.SinceMS = now
		entry.Tried = h.recovered && !queuedNow
		if err := s.AddSending(*entry); err != nil {
			return false, sendingFull(st.id)
		}
		return true, nil
	})
	if err != nil {
		return nil, false, err
	}
	h.pokeSender()
	if queuedNow {
		return nil, true, nil
	}
	wait := make(chan sendResult, 1)
	h.waiting[entry.Txn] = wait
	return wait, false, nil
}

// ownPings are the answer-expecting pings in the outbox and in flight.
func ownPings(s *state.Store) int {
	n := len(s.Outbox.Pings)
	for _, x := range s.Outbox.Sending {
		if x.Ping != nil {
			n++
		}
	}
	return n
}

// keyDigest is how the store holds a duplicate key: its SHA-256.
func keyDigest(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// sendGate is proto's gate from the store's (n3d-D18): the bucket, and the window holding key
// when the store has its digest within the window; no other key can decide this Admit.
func sendGate(g state.Gate, key string, now int64) proto.SendGate {
	sg := proto.SendGate{Bucket: proto.Bucket{Credit: g.Credit, AtMs: g.AtMS}}
	if key == "" {
		return sg
	}
	digest := keyDigest(key)
	for _, k := range g.Recent {
		if k.Key == digest && now-k.AtMS < proto.DuplicateWindowMs {
			if sg.Recent.Sent == nil || k.AtMS > sg.Recent.Sent[key] {
				sg.Recent.Sent = map[string]int64{key: k.AtMS}
			}
		}
	}
	return sg
}

// storeGate is the admitted gate back in the store: the bucket, the keys still in the window,
// and key's digest at now.
func storeGate(s *state.Store, g proto.SendGate, key string, now int64) error {
	digest := keyDigest(key)
	recent := []state.GateKey{}
	for _, k := range s.Outbox.Gate.Recent {
		if now-k.AtMS < proto.DuplicateWindowMs && (key == "" || k.Key != digest) {
			recent = append(recent, k)
		}
	}
	s.Outbox.Gate = state.Gate{Credit: g.Bucket.Credit, AtMS: g.Bucket.AtMs, Recent: recent}
	if key == "" {
		return nil
	}
	return s.AddGateKey(state.GateKey{Key: digest, AtMS: now})
}

// storeLookup is proto.Lookup over a copy of a store taken on the state goroutine: what the
// composer reads of earlier events, with no network call.
type storeLookup struct {
	self, main string
	seen       map[string]bool
	ledger     map[string]state.Ledger
	pings      map[string]state.Ping
	roster     proto.Roster
}

func lookupOf(h *held) storeLookup {
	l := storeLookup{
		self: h.store.UserID, main: h.anchors.MainRoom, seen: map[string]bool{},
		ledger: map[string]state.Ledger{}, pings: map[string]state.Ping{},
	}
	// A pending item is related as a delivered one, so a reply to it needs no fetch.
	for _, p := range h.store.Pending {
		l.seen[p.ID] = true
		l.ledger[p.ID] = ledgerOfPending(p)
	}
	for _, x := range h.store.Ledger {
		l.seen[x.ID] = true
		l.ledger[x.ID] = x
	}
	for _, p := range h.store.Outbox.Pings {
		l.pings[p.ID] = p
	}
	return l
}

func (l storeLookup) Seen(id string) bool { return l.seen[id] }

func (l storeLookup) Event(id string) (proto.Related, bool) {
	if x, ok := l.ledger[id]; ok {
		return ledgerRelated(x, l.main, l.roster), true
	}
	if p, ok := l.pings[id]; ok {
		return pingRelated(p, l.self, l.main, l.roster), true
	}
	return proto.Related{}, false
}

func (l storeLookup) OwnPing(id string) (proto.OwnPing, bool) {
	p, ok := l.pings[id]
	if !ok {
		return proto.OwnPing{}, false
	}
	return proto.OwnPing{Verb: p.Verb, To: slices.Clone(p.To), OriginTs: p.SentMS}, true
}

// with is the lookup judging against r.
func (l storeLookup) with(r proto.Roster) storeLookup {
	l.roster = r
	return l
}

// queuedRecords are status's queued records of a team's sends in flight (n3d-D21).
func queuedRecords(t paths.TeamID, sending []state.Sending, n namer) []records.Record {
	out := make([]records.Record, 0, len(sending))
	for _, s := range sending {
		q := records.Queued{Team: t, Room: s.Room, Send: s.Kind, SinceMS: s.SinceMS, Tried: s.Tried, To: records.List[string]{}}
		if v, err := proto.Parse(s.Content); err == nil {
			dc := decoded(v)
			if dc.Key == proto.KeyPing {
				q.Verb = optional(dc.Ping.Verb)
			}
			q.To = names(n, contentTo(v), nil)
		}
		out = append(out, q)
	}
	return out
}
