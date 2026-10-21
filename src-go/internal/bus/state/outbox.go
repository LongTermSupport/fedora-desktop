package state

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/pyjson"
)

// timeoutKeySep is relay.KEY_SEP, between a ping's event ID and its silent target.
const timeoutKeySep = "~"

var (
	pingKeys = []string{keyEventID, keyVerb, keyRef, "to", "sent_ms", keyAnswered, keyReported}
	// timeoutTargetPat is relay._TARGET_RE, the localpart a TIMEOUT key names.
	timeoutTargetPat = regexp.MustCompile(`^[a-z0-9][a-z0-9._+-]{0,127}$`)
)

// Ping is one sent ping that expects an ack (S27): who it went to, who answered, and which
// targets' TIMEOUTs were reported.
type Ping struct {
	EventID  string
	Verb     string
	Ref      *string
	To       []string
	SentMS   int64
	Answered []string
	Reported []string
}

// Settled is whether every target answered or had its TIMEOUT reported.
func (p Ping) Settled() bool {
	return !slices.ContainsFunc(p.To, func(target string) bool {
		return !slices.Contains(p.Answered, target) && !slices.Contains(p.Reported, target)
	})
}

// Recent is one key of the send gate's duplicate window and when it was sent.
type Recent struct {
	Key  string
	AtMS int64
}

// Gate is the saved send gate: the token bucket's credit and time, and the duplicate window
// in the order v1 keeps it.
type Gate struct {
	Credit int64
	AtMS   int64
	Recent []Recent
}

// Outbox is `outbox.json`; a nil Gate is a send gate not yet used.
type Outbox struct {
	Pings []Ping
	Gate  *Gate
}

// OutboxTx is one read-modify-write of the outbox under its lock (L7): Outbox is saved when
// the transaction ends without error and its bytes changed.
type OutboxTx struct {
	Outbox Outbox
	team   Team
}

// WithOutbox runs fn on the outbox under the outbox lock: `flock` on the state directory,
// held for the whole read-modify-write (L7, S27). Settled pings are dropped on save. No network
// work belongs inside fn; the send gate is charged inside it.
func (t Team) WithOutbox(fn func(*OutboxTx) error) error {
	if err := t.EnsureDirs(); err != nil {
		return err
	}
	fd, err := unix.Open(t.Dir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open %s: %w", t.Dir, err)
	}
	if err := flockRetry(fd, unix.LOCK_EX); err != nil {
		return errors.Join(fmt.Errorf("lock %s: %w", t.Dir, err), unix.Close(fd))
	}
	return errors.Join(t.outboxTx(fn), unix.Close(fd))
}

// flockRetry is flock(2), retried when a signal interrupts the wait.
func flockRetry(fd, how int) error {
	for {
		err := unix.Flock(fd, how)
		if !errors.Is(err, unix.EINTR) {
			return err
		}
	}
}

func (t Team) outboxTx(fn func(*OutboxTx) error) error {
	path := t.outboxPath()
	raw, found, err := readRegular(path, maxStateFileBytes)
	if err != nil {
		return err
	}
	var box Outbox
	if found {
		if box, err = decodeOutbox(path, raw); err != nil {
			return err
		}
	}
	before, err := encodeOutbox(path, box)
	if err != nil {
		return err
	}
	tx := &OutboxTx{Outbox: box, team: t}
	if err = fn(tx); err != nil {
		return err
	}
	after, err := encodeOutbox(path, tx.Outbox)
	if err != nil || bytes.Equal(before, after) {
		return err
	}
	return writeAtomic(path, after)
}

// ReportTimes are the send times of this member's recent reports, `reports.json` (S28).
func (tx *OutboxTx) ReportTimes() ([]int64, error) {
	path := tx.team.reportsPath()
	o, found, err := readVersioned(path, "sent")
	if err != nil || !found {
		return nil, err
	}
	list, err := listField(path, "sent", field(o, "sent"), maxStateFileBytes)
	if err != nil {
		return nil, err
	}
	sent := make([]int64, len(list))
	for i, v := range list {
		if sent[i], err = anyInt(path, "a send time", v); err != nil {
			return nil, err
		}
	}
	return sent, nil
}

// SaveReportTimes replaces `reports.json`.
func (tx *OutboxTx) SaveReportTimes(sent []int64) error {
	list := make([]any, len(sent))
	for i, at := range sent {
		list[i] = at
	}
	return saveCompact(tx.team.reportsPath(), pyjson.Member{Key: "sent", Value: list})
}

// encodeOutbox is v1's bytes for box with settled pings dropped, once box with every ping is
// one the reader accepts: the writer never writes what a reader would refuse.
func encodeOutbox(path string, box Outbox) ([]byte, error) {
	whole, err := outboxBytes(box.Pings, box.Gate)
	if err != nil {
		return nil, fmt.Errorf("encode %s: %w", path, err)
	}
	if _, err := decodeOutbox(path, whole); err != nil {
		return nil, err
	}
	kept := slices.DeleteFunc(slices.Clone(box.Pings), Ping.Settled)
	return outboxBytes(kept, box.Gate)
}

func outboxBytes(pings []Ping, gate *Gate) ([]byte, error) {
	list := make([]any, len(pings))
	for i, p := range pings {
		list[i] = pyjson.Ordered{
			{Key: keyEventID, Value: p.EventID},
			{Key: keyVerb, Value: p.Verb},
			{Key: keyRef, Value: nullable(p.Ref)},
			{Key: "to", Value: strings2list(p.To)},
			{Key: "sent_ms", Value: p.SentMS},
			{Key: keyAnswered, Value: strings2list(p.Answered)},
			{Key: keyReported, Value: strings2list(p.Reported)},
		}
	}
	var g any = pyjson.Null{}
	if gate != nil {
		recent := make(pyjson.Ordered, len(gate.Recent))
		for i, r := range gate.Recent {
			recent[i] = pyjson.Member{Key: r.Key, Value: r.AtMS}
		}
		g = pyjson.Ordered{
			{Key: "bucket", Value: pyjson.Ordered{{Key: "credit", Value: gate.Credit}, {Key: "at_ms", Value: gate.AtMS}}},
			{Key: "recent", Value: recent},
		}
	}
	return pyjson.Encode(pyjson.Ordered{{Key: "v", Value: 1}, {Key: "pings", Value: list}, {Key: "gate", Value: g}}, pyjson.Compact)
}

// decodeOutbox is `outbox.json` checked whole, the gate included (SB4).
func decodeOutbox(path string, raw []byte) (Outbox, error) {
	v, err := decodeJSON(path, raw)
	if err != nil {
		return Outbox{}, err
	}
	o, err := object(path, v)
	if err != nil {
		return Outbox{}, err
	}
	if err = checkKeys(path, o, []string{"v", "pings", "gate"}, nil, unknownFirst); err != nil {
		return Outbox{}, err
	}
	if err = version(path, field(o, "v")); err != nil {
		return Outbox{}, err
	}
	list, err := listField(path, "pings", field(o, "pings"), maxStateFileBytes)
	if err != nil {
		return Outbox{}, err
	}
	box := Outbox{Pings: make([]Ping, len(list))}
	for i, entry := range list {
		if box.Pings[i], err = decodePing(path, entry); err != nil {
			return Outbox{}, err
		}
	}
	if _, isNull := field(o, "gate").(pyjson.Null); !isNull {
		gate, err := decodeGate(path, field(o, "gate"))
		box.Gate = &gate
		return box, err
	}
	return box, nil
}

func decodePing(path string, v any) (Ping, error) {
	o, err := object(path, v)
	if err != nil {
		return Ping{}, err
	}
	if err = checkKeys(path, o, pingKeys, nil, unknownFirst); err != nil {
		return Ping{}, err
	}
	var p Ping
	if p.EventID, err = strField(path, keyEventID, field(o, keyEventID), isEventID); err != nil {
		return Ping{}, err
	}
	if p.Verb, err = strField(path, keyVerb, field(o, keyVerb), isAckVerb); err != nil {
		return Ping{}, err
	}
	ref, set, err := nullableStr(path, keyRef, field(o, keyRef), anyText)
	if err != nil {
		return Ping{}, err
	}
	p.Ref = ptr(ref, set)
	if p.SentMS, err = anyInt(path, "sent_ms", field(o, "sent_ms")); err != nil {
		return Ping{}, err
	}
	if p.To, p.Answered, p.Reported, err = decodeTargets(path, o); err != nil {
		return Ping{}, err
	}
	return p, nil
}

// decodeTargets are a ping's distinct targets, at least one, and its answered and reported
// lists, each drawn from them (duplicates allowed, as v1 allows them).
func decodeTargets(path string, o pyjson.Ordered) ([]string, []string, []string, error) {
	lists := make([][]string, 3)
	for i, name := range []string{"to", keyAnswered, keyReported} {
		list, err := listField(path, name, field(o, name), maxStateFileBytes)
		if err != nil {
			return nil, nil, nil, err
		}
		lists[i] = make([]string, len(list))
		for j, item := range list {
			if lists[i][j], err = strField(path, "a "+name+" entry", item, anyText); err != nil {
				return nil, nil, nil, err
			}
		}
	}
	to := lists[0]
	if len(to) == 0 || !distinct(to) {
		return nil, nil, nil, refuse(path, ClassGrammar, "a ping's targets are distinct, at least one")
	}
	notTarget := func(s string) bool { return !slices.Contains(to, s) }
	if slices.ContainsFunc(lists[1], notTarget) || slices.ContainsFunc(lists[2], notTarget) {
		return nil, nil, nil, refuse(path, ClassGrammar, "an answer or report names no target")
	}
	return to, lists[1], lists[2], nil
}

func decodeGate(path string, v any) (Gate, error) {
	o, err := object(path, v)
	if err != nil {
		return Gate{}, err
	}
	if err = checkKeys(path, o, []string{"bucket", "recent"}, nil, unknownFirst); err != nil {
		return Gate{}, err
	}
	bucket, err := object(path, field(o, "bucket"))
	if err != nil {
		return Gate{}, err
	}
	if err = checkKeys(path, bucket, []string{"credit", "at_ms"}, nil, unknownFirst); err != nil {
		return Gate{}, err
	}
	var g Gate
	if g.Credit, err = intField(path, "credit", field(bucket, "credit"), 0); err != nil {
		return Gate{}, err
	}
	if g.AtMS, err = anyInt(path, "at_ms", field(bucket, "at_ms")); err != nil {
		return Gate{}, err
	}
	recent, err := mapField(path, "recent", field(o, "recent"), maxStateFileBytes)
	if err != nil {
		return Gate{}, err
	}
	g.Recent = make([]Recent, len(recent))
	for i, m := range recent {
		g.Recent[i].Key = m.Key
		if g.Recent[i].AtMS, err = anyInt(path, "a duplicate window time", m.Value); err != nil {
			return Gate{}, err
		}
	}
	return g, nil
}

// DuplicateKey is limits.duplicate_key, the identity the send gate's duplicate window
// compares: `[verb, ref, re, sorted distinct to]` as compact ASCII JSON.
func DuplicateKey(verb string, ref, re *string, to []string) (string, error) {
	targets := slices.Clone(to)
	slices.Sort(targets)
	data, err := pyjson.Encode([]any{verb, nullable(ref), nullable(re), strings2list(slices.Compact(targets))}, pyjson.Compact)
	if err != nil {
		return "", fmt.Errorf("a duplicate key: %w", err)
	}
	return string(data), nil
}

const timeoutKeyWhat = "a TIMEOUT key"

// TimeoutKey is relay.key: the relay spool's name for the TIMEOUT owed for target (a
// localpart) of ping eventID.
func TimeoutKey(eventID, target string) (string, error) {
	if !isEventID(eventID) || !timeoutTargetPat.MatchString(target) {
		return "", refuse(timeoutKeyWhat, ClassGrammar, "an event ID and a target localpart")
	}
	return eventID + timeoutKeySep + target, nil
}

// SplitTimeoutKey is relay.split_key: a spool name's event ID and target, the target empty
// for a name that is an event ID alone.
func SplitTimeoutKey(name string) (string, string, error) {
	eventID, target, hasTarget := strings.Cut(name, timeoutKeySep)
	if !isEventID(eventID) || (hasTarget && !timeoutTargetPat.MatchString(target)) {
		return "", "", refuse(timeoutKeyWhat, ClassGrammar, "not an event ID with an optional target")
	}
	return eventID, target, nil
}

func (t Team) outboxPath() string  { return filepath.Join(t.Dir, "outbox.json") }
func (t Team) reportsPath() string { return filepath.Join(t.Dir, "reports.json") }
