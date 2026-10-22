// Package records is the member binary's output contract (Plan 00209 SPEC-n3 CL3, B 2.2.2):
// every record kind as a typed struct, written as JSON Lines with "kind" first and then "v":1,
// text only as JSON strings, so a body holding tabs, newlines or a forged marker is one harmless
// line. A multi-team verb's answer goes through WriteTeams (CL4, B 2.2.3).
package records

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// RecordVersion is the record version every record carries as "v".
const RecordVersion = 1

// The record kinds of B 2.2.2, B 2.2.3's team-failed, and version's own.
const (
	KindHuman      = "human"
	KindPeer       = "peer"
	KindPing       = "ping"
	KindTimeout    = "timeout"
	KindNotice     = "notice"
	KindFact       = "fact"
	KindOutbox     = "outbox"
	KindSent       = "sent"
	KindPending    = "pending"
	KindQuestion   = "question"
	KindHistory    = "history"
	KindVerified   = "verified"
	KindMore       = "more"
	KindTeam       = "team"
	KindMember     = "member"
	KindRoom       = "room"
	KindRuntime    = "runtime"
	KindTeamFailed = "team-failed"
	KindVersion    = "version"
)

// Kinds is every record kind.
var Kinds = []string{
	KindHuman, KindPeer, KindPing, KindTimeout, KindNotice, KindFact, KindOutbox, KindSent,
	KindPending, KindQuestion, KindHistory, KindVerified, KindMore, KindTeam, KindMember, KindRoom,
	KindRuntime, KindTeamFailed, KindVersion,
}

// Record is one output record; only this package's types are records.
type Record interface {
	Kind() string
	record()
}

// List is a JSON array that is [] when empty, never null.
type List[T any] []T

// MarshalJSON writes the list, [] for none.
func (l List[T]) MarshalJSON() ([]byte, error) {
	if l == nil {
		return []byte("[]"), nil
	}
	return marshal([]T(l))
}

// Human is a HUMAN item; Question is the question number it answers, Revises the event it edits.
type Human struct {
	Team     paths.TeamID `json:"team"`
	ID       string       `json:"id"`
	From     string       `json:"from"`
	FromID   string       `json:"from_id"`
	SentMS   int64        `json:"sent_ms"`
	Room     string       `json:"room"`
	Text     string       `json:"text"`
	Question *int64       `json:"question"`
	Revises  *string      `json:"revises"`
	Restored bool         `json:"restored"`
}

// Peer is a PEER item; CopyOf is the event it copies.
type Peer struct {
	Team     paths.TeamID `json:"team"`
	ID       string       `json:"id"`
	From     string       `json:"from"`
	SentMS   int64        `json:"sent_ms"`
	Text     string       `json:"text"`
	CopyOf   *string      `json:"copy_of"`
	Restored bool         `json:"restored"`
}

// Ping is a PING item; Ref, Branch and Re are null where the verb has none.
type Ping struct {
	Team     paths.TeamID `json:"team"`
	ID       string       `json:"id"`
	From     string       `json:"from"`
	SentMS   int64        `json:"sent_ms"`
	Verb     string       `json:"verb"`
	Ref      *string      `json:"ref"`
	Trust    string       `json:"trust"`
	Branch   *string      `json:"branch"`
	Re       *string      `json:"re"`
	Restored bool         `json:"restored"`
}

// Timeout is a TIMEOUT item: Silent did not answer this member's ping ID in time.
type Timeout struct {
	Team     paths.TeamID `json:"team"`
	ID       string       `json:"id"`
	Silent   string       `json:"silent"`
	Verb     string       `json:"verb"`
	Ref      *string      `json:"ref"`
	Restored bool         `json:"restored"`
}

// Notice is a NOTICE item (A 7.4); Carried is A 7.7's carried object when there is one.
type Notice struct {
	Team    paths.TeamID `json:"team"`
	ID      string       `json:"id"`
	From    string       `json:"from"`
	SentMS  int64        `json:"sent_ms"`
	Event   string       `json:"event"`
	Subject string       `json:"subject"`
	Reason  *string      `json:"reason"`
	Carried *Carried     `json:"carried"`
}

// Carried is A 7.7's carried item; Verb and Ref only for a carried ping.
type Carried struct {
	Kind              string  `json:"kind"`
	Verb              *string `json:"verb"`
	Ref               *string `json:"ref"`
	From              string  `json:"from"`
	SentMS            int64   `json:"sent_ms"`
	V1Event           string  `json:"v1_event"`
	To                string  `json:"to"`
	Text              string  `json:"text"`
	PossibleDuplicate bool    `json:"possible_duplicate"`
}

// Fact is one of A 13.1's trailing lines as a record.
type Fact struct {
	Team  paths.TeamID `json:"team"`
	Fact  string       `json:"fact"`
	From  *string      `json:"from"`
	Count *int64       `json:"count"`
	ID    *string      `json:"id"`
}

// Outbox is one ping this member sent that expects or recently had an answer.
type Outbox struct {
	Team     paths.TeamID `json:"team"`
	ID       string       `json:"id"`
	Verb     string       `json:"verb"`
	Ref      *string      `json:"ref"`
	To       List[string] `json:"to"`
	DueMS    int64        `json:"due_ms"`
	Answers  List[Answer] `json:"answers"`
	TimedOut List[string] `json:"timed_out"`
}

// Answer is one answer recorded against an outbox ping.
type Answer struct {
	From string `json:"from"`
	Verb string `json:"verb"`
	ID   string `json:"id"`
	Late bool   `json:"late"`
}

// Sent is a send's event.
type Sent struct {
	Team paths.TeamID `json:"team"`
	ID   string       `json:"id"`
	Room string       `json:"room"`
}

// Pending is one pending item, listed without its body.
type Pending struct {
	Team   paths.TeamID `json:"team"`
	ID     string       `json:"id"`
	From   string       `json:"from"`
	Item   string       `json:"item"`
	SentMS int64        `json:"sent_ms"`
}

// Question is one question in the questions room.
type Question struct {
	Team       paths.TeamID `json:"team"`
	N          int64        `json:"n"`
	State      string       `json:"state"`
	ID         string       `json:"id"`
	AskedBy    List[string] `json:"asked_by"`
	Unanswered List[string] `json:"unanswered"`
}

// History is one item received ("in") or sent ("out"), the item a record of its own kind.
type History struct {
	Team paths.TeamID
	Dir  string
	Item Record
}

// MarshalJSON writes the history record with its item encoded as a record.
func (h History) MarshalJSON() ([]byte, error) {
	if h.Item == nil {
		return nil, errors.New("a history record with no item")
	}
	item, err := Encode(h.Item)
	if err != nil {
		return nil, err
	}
	return marshal(struct {
		Team paths.TeamID    `json:"team"`
		Dir  string          `json:"dir"`
		Item json.RawMessage `json:"item"`
	}{h.Team, h.Dir, item})
}

// Verified is one event re-fetched from the homeserver, with its full text and its hash.
type Verified struct {
	Team       paths.TeamID `json:"team"`
	ID         string       `json:"id"`
	Item       string       `json:"item"`
	From       string       `json:"from"`
	Room       string       `json:"room"`
	Text       string       `json:"text"`
	BodySHA256 string       `json:"body_sha256"`
	SentMS     int64        `json:"sent_ms"`
}

// More ends a recv or wait that stopped at the caps; its team is null, as it counts all teams.
type More struct {
	Pending int64
}

// MarshalJSON writes the more record with its null team.
func (m More) MarshalJSON() ([]byte, error) {
	return marshal(struct {
		Team    *paths.TeamID `json:"team"`
		Pending int64         `json:"pending"`
	}{nil, m.Pending})
}

// Team is one of the session's teams as status shows it.
type Team struct {
	Team     paths.TeamID `json:"team"`
	Name     string       `json:"name"`
	Handle   string       `json:"handle"`
	Role     string       `json:"role"`
	About    string       `json:"about"`
	Trust    string       `json:"trust"`
	Delivery string       `json:"delivery"`
	Pending  int64        `json:"pending"`
	Down     bool         `json:"down"`
}

// Member is one member of a team.
type Member struct {
	Team    paths.TeamID `json:"team"`
	Handle  string       `json:"handle"`
	Role    string       `json:"role"`
	Present bool         `json:"present"`
}

// Room is one of a team's side rooms.
type Room struct {
	Team    paths.TeamID `json:"team"`
	Purpose string       `json:"purpose"`
	State   string       `json:"state"`
}

// Runtime is the session's resident cost (B40).
type Runtime struct {
	Keeper KeeperCost `json:"keeper"`
	Link   LinkCost   `json:"link"`
}

// KeeperCost is the keeper's resident cost.
type KeeperCost struct {
	RSSKiB  int64 `json:"rss_kib"`
	CPUMS   int64 `json:"cpu_ms"`
	Wakeups int64 `json:"wakeups"`
	FDs     int64 `json:"fds"`
	UptimeS int64 `json:"uptime_s"`
}

// LinkCost is the link's resident cost.
type LinkCost struct {
	RSSKiB   int64 `json:"rss_kib"`
	CPUMS    int64 `json:"cpu_ms"`
	Wakeups  int64 `json:"wakeups"`
	FDs      int64 `json:"fds"`
	Conns    int64 `json:"conns"`
	Restarts int64 `json:"restarts"`
	UptimeS  int64 `json:"uptime_s"`
}

// TeamFailed is one team's failure in a multi-team verb's answer (B 2.2.3).
type TeamFailed struct {
	Team paths.TeamID  `json:"team"`
	Exit term.ExitCode `json:"exit"`
	Code string        `json:"code"`
}

// TeamFailedOf is the team-failed record of a team's error; one no verb named is internal.
func TeamFailedOf(team paths.TeamID, err error) TeamFailed {
	if e, ok := errors.AsType[*exits.Error](err); ok {
		return TeamFailed{Team: team, Exit: e.Exit, Code: e.Code}
	}
	return TeamFailed{Team: team, Exit: exits.ExitInternal, Code: exits.CodeInternal}
}

// Version is `teamchat version`'s record: the binary's source digest and its record version.
type Version struct {
	Digest  string `json:"digest"`
	Records int    `json:"records"`
}

// Kind is the record kind.
func (Human) Kind() string { return KindHuman }

// Kind is the record kind.
func (Peer) Kind() string { return KindPeer }

// Kind is the record kind.
func (Ping) Kind() string { return KindPing }

// Kind is the record kind.
func (Timeout) Kind() string { return KindTimeout }

// Kind is the record kind.
func (Notice) Kind() string { return KindNotice }

// Kind is the record kind.
func (Fact) Kind() string { return KindFact }

// Kind is the record kind.
func (Outbox) Kind() string { return KindOutbox }

// Kind is the record kind.
func (Sent) Kind() string { return KindSent }

// Kind is the record kind.
func (Pending) Kind() string { return KindPending }

// Kind is the record kind.
func (Question) Kind() string { return KindQuestion }

// Kind is the record kind.
func (History) Kind() string { return KindHistory }

// Kind is the record kind.
func (Verified) Kind() string { return KindVerified }

// Kind is the record kind.
func (More) Kind() string { return KindMore }

// Kind is the record kind.
func (Team) Kind() string { return KindTeam }

// Kind is the record kind.
func (Member) Kind() string { return KindMember }

// Kind is the record kind.
func (Room) Kind() string { return KindRoom }

// Kind is the record kind.
func (Runtime) Kind() string { return KindRuntime }

// Kind is the record kind.
func (TeamFailed) Kind() string { return KindTeamFailed }

// Kind is the record kind.
func (Version) Kind() string { return KindVersion }

func (Human) record()      {}
func (Peer) record()       {}
func (Ping) record()       {}
func (Timeout) record()    {}
func (Notice) record()     {}
func (Fact) record()       {}
func (Outbox) record()     {}
func (Sent) record()       {}
func (Pending) record()    {}
func (Question) record()   {}
func (History) record()    {}
func (Verified) record()   {}
func (More) record()       {}
func (Team) record()       {}
func (Member) record()     {}
func (Room) record()       {}
func (Runtime) record()    {}
func (TeamFailed) record() {}
func (Version) record()    {}

// marshal is v as compact JSON with no HTML escaping, so text reads as it was written.
func marshal(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(b.Bytes(), []byte("\n")), nil
}

// Encode is r as one JSON object, "kind" and "v" spliced before its fields, with no newline.
func Encode(r Record) ([]byte, error) {
	body, err := marshal(r)
	if err != nil {
		return nil, fmt.Errorf("encoding a %s record: %w", r.Kind(), err)
	}
	out := fmt.Appendf(nil, `{"kind":%q,"v":%d`, r.Kind(), RecordVersion)
	if len(body) > 2 {
		out = append(out, ',')
	}
	return append(out, body[1:]...), nil
}

// Write writes r as one line; a record that does not encode writes nothing.
func Write(w io.Writer, r Record) error {
	line, err := Encode(r)
	if err != nil {
		return err
	}
	_, err = w.Write(append(line, '\n'))
	return err
}

// TeamResult is one team's part of a multi-team verb: its records, or its error.
type TeamResult struct {
	Team    paths.TeamID
	Records []Record
	Err     error
}

const partialMessage exits.Template = "{} of {} teams failed; read the team-failed records"

// WriteTeams writes a multi-team verb's answer (CL4, B 2.2.3): each team's records in order, or
// its team-failed record. With every team well it is 0; with some failed it is 5 `partial`;
// with every team failed nothing succeeded, so it is the first failure's own exit and object.
func WriteTeams(stdout, stderr io.Writer, results []TeamResult) term.ExitCode {
	var failed []error
	for _, r := range results {
		recs := r.Records
		if r.Err != nil {
			failed = append(failed, r.Err)
			recs = []Record{TeamFailedOf(r.Team, r.Err)}
		}
		for _, rec := range recs {
			if err := Write(stdout, rec); err != nil {
				return exits.Write(stderr, err)
			}
		}
	}
	switch {
	case len(failed) == 0:
		return term.ExitOK
	case len(failed) == len(results):
		return exits.Write(stderr, failed[0])
	}
	return exits.Write(stderr, exits.New(exits.CodePartial, nil, partialMessage,
		exits.Count(int64(len(failed))), exits.Count(int64(len(results)))))
}
