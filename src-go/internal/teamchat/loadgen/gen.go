package loadgen

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// The patterns of LG4, each one switch and one rate.
const (
	PatternPings         = "pings"
	PatternQuestions     = "questions"
	PatternNotices       = "notices"
	PatternReports       = "reports"
	PatternReceiptEvents = "receipt-events"
	PatternReceipts      = "receipts"
	PatternPresence      = "presence"
	PatternBig           = "big"
)

// Patterns is every pattern, in the order a run reports them.
var Patterns = []string{PatternPings, PatternQuestions, PatternNotices, PatternReports, PatternReceiptEvents, PatternReceipts, PatternPresence, PatternBig}

// The kinds of event the generator makes, which the tally counts.
const (
	KindPing         = "ping"
	KindAnswer       = "answer"
	KindText         = "text"
	KindReply        = "reply"
	KindBig          = "big"
	KindNotice       = "notice"
	KindReport       = "report"
	KindReceiptEvent = "receipt-event"
	KindHumanMain    = "human-main"
	KindRead         = "read"
	KindQuestion     = "question"
	KindHumanThread  = "human-thread"
	KindThread       = "thread"
	KindClose        = "close"
	KindRetitle      = "retitle"
	KindPresence     = "presence"
)

// The presence states a flap moves between: the specification's values, matrix's own constant
// for the connected one.
const (
	presenceOnline  = matrix.PresenceOnline
	presenceOffline = "offline"
)

// maxHumanMentions is how many agents a human post mentions at most (an agent text's 16).
const maxHumanMentions = proto.MaxTextTo

// reviewRef is the commit every ping's `review` names, in the roster's forge org.
const reviewRef = "commit:" + forgeOrg + "/loadgen@0123456789abcdef0123456789abcdef01234567"

// noLookup is a store that has seen nothing: the generator folds each questions-room event once,
// as it posts it.
type noLookup struct{}

func (noLookup) Seen(string) bool                     { return false }
func (noLookup) Event(string) (proto.Related, bool)   { return proto.Related{}, false }
func (noLookup) OwnPing(string) (proto.OwnPing, bool) { return proto.OwnPing{}, false }

// bigWords is what a big body repeats.
const bigWords = "load big body "

// Options are a run's settings beyond the patterns: the big body's size in bytes (0 is
// proto.MaxText).
type Options struct {
	BigBytes int
}

// Tally is what a run made: events by kind, the prerequisites made for a replay's kinds by
// kind (LG6), and units by pattern.
type Tally struct {
	Made  map[string]int64 `json:"made"`
	Extra map[string]int64 `json:"extra"`
	Units map[string]int64 `json:"units"`
}

// agent is one account and its client.
type agent struct {
	Account

	cl *matrix.Client
}

// sent is an event the generator posted: its ID and sender.
type sent struct{ id, sender string }

// question is the newest question: its number and its root event as the index folded it.
type question struct {
	n    int64
	root proto.Event
}

// Generator sends the test team's traffic (LG4), each event composed by proto.Composer against
// the roster the main room holds.
type Generator struct {
	team   Team
	clock  Clock
	roster proto.Roster
	agents []*agent
	human  *agent
	idx    *proto.QuestionIndex
	// flood is the coordinator's receive window, which a fold of the human's post enters.
	flood    proto.Window
	big      int
	turns    map[string]int
	presence map[string]string
	notices  map[string]string
	// mainLast is the newest event in main, mainItem the newest agent item there.
	mainLast, mainItem *sent
	// roots are the pings not yet answered, oldest first; lastRoot the newest ping.
	roots    []sent
	lastRoot *sent
	q        *question
	seq      int
	tally    Tally
	// making is the kinds asked for now; any other kind made is an extra.
	making []string
}

// NewGenerator is a generator over team, its roster read from the main room's state (LG3).
func NewGenerator(ctx context.Context, team Team, c Clock, o Options) (*Generator, error) {
	if err := team.check(); err != nil {
		return nil, err
	}
	big := o.BigBytes
	if big == 0 {
		big = proto.MaxText
	}
	if big < 1 || big > proto.MaxText {
		return nil, fmt.Errorf("--big-bytes %d: give 1 to %d (the agent text limit)", o.BigBytes, proto.MaxText)
	}
	g := &Generator{
		team: team, clock: c, big: big, turns: map[string]int{}, presence: map[string]string{}, notices: map[string]string{},
		tally: Tally{Made: map[string]int64{}, Extra: map[string]int64{}, Units: map[string]int64{}},
	}
	if err := g.connect(ctx); err != nil {
		g.Close()
		return nil, err
	}
	g.idx = proto.NewQuestionIndex(team.ServerName, g.roster.QuestionFloor)
	return g, nil
}

func (g *Generator) connect(ctx context.Context) error {
	var err error
	if g.human, err = g.client(g.team.Human); err != nil {
		return err
	}
	for _, a := range g.team.Agents {
		ag, err := g.client(a)
		if err != nil {
			return err
		}
		g.agents = append(g.agents, ag)
	}
	g.roster, err = g.readRoster(ctx)
	return err
}

func (g *Generator) client(a Account) (*agent, error) {
	cl, err := matrix.NewMember(matrix.Config{BaseURL: g.team.Homeserver, Token: a.Token, Clock: g.clock, Wall: g.clock})
	if err != nil {
		return nil, fmt.Errorf("a client for %s: %w", a.UserID, err)
	}
	return &agent{Account: a, cl: cl}, nil
}

func (g *Generator) readRoster(ctx context.Context) (proto.Roster, error) {
	raw, err := g.human.cl.GetState(ctx, g.team.Rooms.Main, proto.RosterType, "")
	if err != nil {
		return proto.Roster{}, fmt.Errorf("read the roster from main: %w", err)
	}
	v, err := proto.Parse(raw)
	if err != nil {
		return proto.Roster{}, fmt.Errorf("the roster: %w", err)
	}
	r, err := proto.DecodeRoster(v, g.team.ServerName, g.team.Team, g.team.Rooms.Main)
	if err != nil {
		return proto.Roster{}, fmt.Errorf("the roster: %w", err)
	}
	return r, nil
}

// Close closes every client.
func (g *Generator) Close() {
	if g.human != nil {
		g.human.cl.Close()
	}
	for _, a := range g.agents {
		a.cl.Close()
	}
}

// Tally is what the generator has made so far, a copy.
func (g *Generator) Tally() Tally {
	return Tally{Made: maps.Clone(g.tally.Made), Extra: maps.Clone(g.tally.Extra), Units: maps.Clone(g.tally.Units)}
}

// unit is a pattern's unit and the kinds it makes; any other kind it makes is an extra.
type unit struct {
	send  func(context.Context) error
	kinds []string
}

// Unit sends one unit of pattern (LG4).
func (g *Generator) Unit(ctx context.Context, pattern string) error {
	units := map[string]unit{
		PatternPings:         {g.pingUnit, []string{KindPing, KindAnswer}},
		PatternQuestions:     {g.questionUnit, []string{KindQuestion, KindHumanThread, KindThread, KindClose, KindRetitle}},
		PatternNotices:       {g.notice, []string{KindNotice}},
		PatternReports:       {g.report, []string{KindReport}},
		PatternReceiptEvents: {g.receiptEvent, []string{KindReceiptEvent}},
		PatternReceipts:      {g.receiptsUnit, []string{KindHumanMain, KindRead}},
		PatternPresence:      {g.flap, []string{KindPresence}},
		PatternBig:           {g.bigText, []string{KindBig}},
	}
	u, ok := units[pattern]
	if !ok {
		return fmt.Errorf("no pattern %q (the patterns are %s)", pattern, strings.Join(Patterns, ", "))
	}
	g.making = u.kinds
	if err := u.send(ctx); err != nil {
		return fmt.Errorf("pattern %s: %w", pattern, err)
	}
	g.tally.Units[pattern]++
	return nil
}

// Kind sends one event of kind (LG6), its missing prerequisite made first as an extra.
func (g *Generator) Kind(ctx context.Context, kind string) error {
	kinds := map[string]func(context.Context) error{
		KindPing: g.ping, KindAnswer: g.answer, KindText: g.text, KindReply: g.reply, KindNotice: g.notice,
		KindReport: g.report, KindReceiptEvent: g.receiptEvent, KindHumanMain: g.humanMainOne,
		KindQuestion: g.ask, KindRetitle: g.retitle, KindThread: g.threadPost, KindHumanThread: g.humanThread,
	}
	makeOne, ok := kinds[kind]
	if !ok {
		return fmt.Errorf("no kind %q the replay makes", kind)
	}
	g.making = []string{kind, KindRead}
	if err := makeOne(ctx); err != nil {
		return fmt.Errorf("kind %s: %w", kind, err)
	}
	return nil
}

// made counts one event of kind: an extra when it is not one of the kinds asked for.
func (g *Generator) made(kind string) {
	if !slices.Contains(g.making, kind) {
		g.tally.Extra[kind]++
		return
	}
	g.tally.Made[kind]++
}

// next is pattern's next agent in turn, and the one after it.
func (g *Generator) next(pattern string) (*agent, *agent) {
	a := g.one(pattern)
	return a, g.after(a.UserID)
}

// one is pattern's next agent in turn.
func (g *Generator) one(pattern string) *agent {
	i := g.turns[pattern] % len(g.agents)
	g.turns[pattern]++
	return g.agents[i]
}

func (g *Generator) composer(a *agent) proto.Composer {
	return proto.Composer{Server: g.team.ServerName, Self: a.UserID, Token: a.Token, Roster: g.roster, Questions: g.idx, NowMs: g.clock.WallMS(), Lookup: noLookup{}}
}

func (g *Generator) roomOf(purpose string) (string, error) {
	switch purpose {
	case proto.PurposeMain:
		return g.team.Rooms.Main, nil
	case proto.PurposeReports:
		return g.team.Rooms.Reports, nil
	case proto.PurposeQuestions:
		return g.team.Rooms.Questions, nil
	}
	return "", fmt.Errorf("the composer gave the room %q, which the team does not have", purpose)
}

// post sends a composed event as a, counted under kind.
func (g *Generator) post(ctx context.Context, a *agent, kind string, out proto.Outgoing, err error) (string, error) {
	if err != nil {
		return "", fmt.Errorf("%s composes: %w", a.UserID, err)
	}
	room, err := g.roomOf(out.Purpose)
	if err != nil {
		return "", err
	}
	return g.send(ctx, a, kind, room, out.Content.Compact())
}

func (g *Generator) send(ctx context.Context, a *agent, kind, room string, content []byte) (string, error) {
	id, err := a.cl.Send(ctx, room, proto.MessageType, "", content)
	if err != nil {
		return "", fmt.Errorf("%s sends: %w", a.UserID, err)
	}
	g.made(kind)
	if room == g.team.Rooms.Main {
		g.mainLast = &sent{id: id, sender: a.UserID}
		if a.Role != RoleHuman {
			g.mainItem = g.mainLast
		}
	}
	return id, nil
}

func (g *Generator) words(what string) string {
	g.seq++
	return "load " + what + " " + strconv.Itoa(g.seq)
}

func (g *Generator) pingUnit(ctx context.Context) error {
	if err := g.ping(ctx); err != nil {
		return err
	}
	return g.answer(ctx)
}

func (g *Generator) ping(ctx context.Context) error {
	from, to := g.next(PatternPings)
	p := proto.Ping{Verb: "review", Ref: proto.LowercaseRefRepo(reviewRef), To: []string{to.UserID}}
	out, err := g.composer(from).Ping(p, "", g.words("review"))
	id, err := g.post(ctx, from, KindPing, out, err)
	if err != nil {
		return err
	}
	root := sent{id: id, sender: from.UserID}
	g.roots = append(g.roots, root)
	g.lastRoot = &root
	return nil
}

// answer is an `ack` in the oldest unanswered ping's thread (or the newest ping's), by the
// agent after its sender, which the ping addressed.
func (g *Generator) answer(ctx context.Context) error {
	if g.lastRoot == nil {
		if err := g.ping(ctx); err != nil {
			return err
		}
	}
	root := *g.lastRoot
	if len(g.roots) > 0 {
		root, g.roots = g.roots[0], g.roots[1:]
	}
	from := g.after(root.sender)
	out, err := g.composer(from).Ping(proto.Ping{Verb: proto.VerbAck, To: []string{root.sender}}, root.id, g.words("ack"))
	_, err = g.post(ctx, from, KindAnswer, out, err)
	return err
}

// after is the agent after the one with userID, in the team's order.
func (g *Generator) after(userID string) *agent {
	for i, a := range g.agents {
		if a.UserID == userID {
			return g.agents[(i+1)%len(g.agents)]
		}
	}
	return g.agents[0]
}

func (g *Generator) text(ctx context.Context) error {
	from, to := g.next(KindText)
	out, err := g.composer(from).Text(proto.Text{To: []string{to.UserID}, Text: g.words("text")}, "", "")
	_, err = g.post(ctx, from, KindText, out, err)
	return err
}

// reply is agent text replying to the newest event in main, to its sender when that is another
// agent.
func (g *Generator) reply(ctx context.Context) error {
	if g.mainLast == nil {
		if err := g.text(ctx); err != nil {
			return err
		}
	}
	last := *g.mainLast
	from := g.after(last.sender)
	to := last.sender
	if _, isAgent := g.roster.Members[to]; !isAgent || to == from.UserID {
		to = g.after(from.UserID).UserID
	}
	out, err := g.composer(from).Text(proto.Text{To: []string{to}, Text: g.words("reply")}, last.id, "")
	_, err = g.post(ctx, from, KindReply, out, err)
	return err
}

func (g *Generator) bigText(ctx context.Context) error {
	from, to := g.next(PatternBig)
	text := strings.Repeat(bigWords, g.big/len(bigWords)+1)[:g.big]
	out, err := g.composer(from).Text(proto.Text{To: []string{to.UserID}, Text: text}, "", "")
	_, err = g.post(ctx, from, KindBig, out, err)
	return err
}

// notice is the next agent's own notice: `disconnected`, then `back`, in turn, to the human
// and the coordinator (a notice's addressees, A 7.4), the coordinator's to the human alone.
func (g *Generator) notice(ctx context.Context) error {
	from := g.one(PatternNotices)
	event := proto.EventDisconnected
	if g.notices[from.UserID] == event {
		event = proto.EventBack
	}
	to := []string{g.team.Human.UserID}
	if c := g.coordinator(); c != from {
		to = append(to, c.UserID)
	}
	n := proto.Notice{Event: event, Subject: from.UserID, To: to}
	out, err := g.composer(from).Notice(n, event)
	if _, err := g.post(ctx, from, KindNotice, out, err); err != nil {
		return err
	}
	g.notices[from.UserID] = event
	return nil
}

func (g *Generator) report(ctx context.Context) error {
	from := g.one(PatternReports)
	out, err := g.composer(from).Report(g.words("report") + "\n\nA report the load generator wrote.")
	_, err = g.post(ctx, from, KindReport, out, err)
	return err
}

// receiptEvent is a `delivered` receipt of the newest agent item in main, by the agent after
// its sender, to its sender.
func (g *Generator) receiptEvent(ctx context.Context) error {
	if g.mainItem == nil {
		if err := g.ping(ctx); err != nil {
			return err
		}
	}
	item := *g.mainItem
	from := g.after(item.sender)
	content, err := proto.ReceiptContent(proto.Receipt{Of: item.id, To: []string{item.sender}, State: proto.ReceiptDelivered, At: g.clock.WallMS()})
	if err != nil {
		return fmt.Errorf("the receipt does not compose: %w", err)
	}
	_, err = g.send(ctx, from, KindReceiptEvent, g.team.Rooms.Reports, content.Compact())
	return err
}

// humanPost is the human's post in main mentioning to, then each one's `m.read` receipt of it.
func (g *Generator) humanPost(ctx context.Context, to []*agent) error {
	ids := make([]string, 0, len(to))
	for _, a := range to {
		ids = append(ids, a.UserID)
	}
	content, err := json.Marshal(map[string]any{"msgtype": proto.MsgText, "body": g.words("human post"), "m.mentions": map[string]any{"user_ids": ids}})
	if err != nil {
		return err
	}
	id, err := g.send(ctx, g.human, KindHumanMain, g.team.Rooms.Main, content)
	if err != nil {
		return err
	}
	for _, a := range to {
		if err := a.cl.ReadReceipt(ctx, g.team.Rooms.Main, id); err != nil {
			return fmt.Errorf("%s reads %s: %w", a.UserID, id, err)
		}
		g.made(KindRead)
	}
	return nil
}

func (g *Generator) receiptsUnit(ctx context.Context) error {
	return g.humanPost(ctx, g.agents[:min(len(g.agents), maxHumanMentions)])
}

// humanMainOne is a human post to the coordinator, which reads it.
func (g *Generator) humanMainOne(ctx context.Context) error {
	return g.humanPost(ctx, g.agents[:1])
}

// flap sets the next agent's presence to the other of online and offline, online first.
func (g *Generator) flap(ctx context.Context) error {
	a := g.one(PatternPresence)
	state := presenceOnline
	if g.presence[a.UserID] == presenceOnline {
		state = presenceOffline
	}
	if err := a.cl.SetPresence(ctx, a.UserID, state); err != nil {
		return fmt.Errorf("%s sets presence %s: %w", a.UserID, state, err)
	}
	g.presence[a.UserID] = state
	g.made(KindPresence)
	return nil
}

// coordinator is the team's coordinator, its first agent.
func (g *Generator) coordinator() *agent { return g.agents[0] }

// fold reads a questions-room event back and folds it into the index, as the coordinator's
// link does.
func (g *Generator) fold(ctx context.Context, id string) (proto.Verdict, error) {
	c := g.coordinator()
	raw, err := c.cl.GetEvent(ctx, g.team.Rooms.Questions, id)
	if err != nil {
		return proto.Verdict{}, fmt.Errorf("the coordinator %s reads %s back: %w", c.UserID, id, err)
	}
	v, err := proto.Parse(raw)
	if err != nil {
		return proto.Verdict{}, fmt.Errorf("the event %s: %w", id, err)
	}
	j := proto.Judge{
		Server: g.team.ServerName, Self: c.UserID, RoomID: g.team.Rooms.Questions, Purpose: proto.PurposeQuestions,
		Roster: g.roster, NowMs: g.clock.WallMS(), Lookup: noLookup{}, Flood: &g.flood,
	}
	verdict := j.Judge(v)
	g.idx.Feed(verdict)
	if verdict.Human.Body != "" {
		g.idx.Attach(verdict.Event, verdict.Human)
	}
	return verdict, nil
}

func (g *Generator) postQuestions(ctx context.Context, kind string, out proto.Outgoing, err error) (proto.Verdict, error) {
	id, err := g.post(ctx, g.coordinator(), kind, out, err)
	if err != nil {
		return proto.Verdict{}, err
	}
	return g.fold(ctx, id)
}

func (g *Generator) ask(ctx context.Context) error {
	text := g.words("question") + "\n" + proto.SectionContext + " the load generator asks.\n" +
		proto.SectionQuestion + " is this load?\n" + proto.SectionWhy + " " + proto.WhyDecision
	n := g.idx.Next()
	out, err := g.composer(g.coordinator()).Question(proto.QuestionPost{Text: text})
	v, err := g.postQuestions(ctx, KindQuestion, out, err)
	if err != nil {
		return err
	}
	if _, ok := g.idx.Question(n); !ok {
		return fmt.Errorf("the question Q%d was not folded (%v %s)", n, v.Outcome, v.Reason)
	}
	g.q = &question{n: n, root: v.Event}
	return nil
}

// current is the newest question, asked first when there is none.
func (g *Generator) current(ctx context.Context) (proto.IndexedQuestion, error) {
	if g.q == nil {
		if err := g.ask(ctx); err != nil {
			return proto.IndexedQuestion{}, err
		}
	}
	q, ok := g.idx.Question(g.q.n)
	if !ok {
		return proto.IndexedQuestion{}, fmt.Errorf("question Q%d has left the index", g.q.n)
	}
	return q, nil
}

// open is the newest question, asked first when there is none or it is closed.
func (g *Generator) open(ctx context.Context) (proto.IndexedQuestion, error) {
	q, err := g.current(ctx)
	if err != nil || !q.Closed {
		return q, err
	}
	if err := g.ask(ctx); err != nil {
		return proto.IndexedQuestion{}, err
	}
	return g.current(ctx)
}

// humanThread is the human's post in the newest open question's thread.
func (g *Generator) humanThread(ctx context.Context) error {
	q, err := g.open(ctx)
	if err != nil {
		return err
	}
	content, err := json.Marshal(map[string]any{
		"msgtype": proto.MsgText, "body": g.words("human answer"),
		"m.relates_to": map[string]any{
			"rel_type": "m.thread", "event_id": q.EventID, "is_falling_back": true,
			"m.in_reply_to": map[string]any{"event_id": q.Latest},
		},
	})
	if err != nil {
		return err
	}
	id, err := g.send(ctx, g.human, KindHumanThread, g.team.Rooms.Questions, content)
	if err != nil {
		return err
	}
	_, err = g.fold(ctx, id)
	return err
}

// threadPost is the coordinator's post in the newest open question's thread, which waits on
// the humans.
func (g *Generator) threadPost(ctx context.Context) error { return g.inThread(ctx, KindThread, false) }

func (g *Generator) closeQuestion(ctx context.Context) error { return g.inThread(ctx, KindClose, true) }

func (g *Generator) inThread(ctx context.Context, kind string, closes bool) error {
	q, err := g.open(ctx)
	if err != nil {
		return err
	}
	text := g.words("thread post")
	if closes {
		text = ""
	}
	out, err := g.composer(g.coordinator()).Thread(proto.ThreadPost{N: q.N, Text: text, Close: closes})
	_, err = g.postQuestions(ctx, kind, out, err)
	return err
}

// retitle is the coordinator's retitle of the newest question.
func (g *Generator) retitle(ctx context.Context) error {
	if _, err := g.current(ctx); err != nil {
		return err
	}
	out, err := g.composer(g.coordinator()).Retitle(proto.RetitlePost{N: g.q.n, Headline: g.words("retitled"), Original: g.q.root})
	_, err = g.postQuestions(ctx, KindRetitle, out, err)
	return err
}

// questionUnit is a whole question thread: asked, the human's post in it, the coordinator's
// thread post, the close and the retitle of the closed question.
func (g *Generator) questionUnit(ctx context.Context) error {
	for _, step := range []func(context.Context) error{g.ask, g.humanThread, g.threadPost, g.closeQuestion, g.retitle} {
		if err := step(ctx); err != nil {
			return err
		}
	}
	if q, err := g.current(ctx); err != nil || !q.Closed {
		return errors.Join(errors.New("the question thread's close did not close it"), err)
	}
	return nil
}
