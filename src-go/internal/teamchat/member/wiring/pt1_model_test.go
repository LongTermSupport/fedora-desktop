//go:build busfailpoints

package wiring

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"maps"
	"math/rand/v2"
	"slices"
	"strings"
)

// PT1, the interleaving harness (SPEC-n3 PT1, B 2.15): this file is its pure half, so that the
// same seed is the same schedule on every machine and the properties are judged by one function
// that its own tests can show red. pt1_run_test.go drives real keeper and link processes with it.

// pt1Kind is one operation of a run.
type pt1Kind int

const (
	pt1Say      pt1Kind = iota // a human posts to the session's seat
	pt1Peer                    // a peer agent posts to it
	pt1Recv                    // the seat's agent runs recv
	pt1Wait                    // ... or wait, for one second
	pt1Send                    // the seat's agent says something to the peer
	pt1Hold                    // the inbox stops reading: a delivery blocks
	pt1Release                 // the inbox reads again
	pt1Refuse                  // the inbox refuses every connect for the rest of the run
	pt1Restart                 // the homeserver drops every connection and serves again
	pt1KillLink                // the link is SIGKILLed where it stands
)

// pt1Names is every kind's name, in the order of the kinds; the tables below are as long.
var pt1Names = [...]string{"say", "peer", "recv", "wait", "send", "hold", "release", "refuse", "restart", "kill-link"}

// pt1Weights are how often a kind is drawn; pt1Limits cap a kind per run (0 is no cap). The caps
// keep a run cheap (a wait is up to a second, a kill a restart backoff) and inside the budgets
// the code under test is entitled to apply: a peer's flood budget is 10 items in 600 s, so at
// most 6 peer items are posted and none is ever held back.
var (
	pt1Weights = [len(pt1Names)]uint64{pt1Say: 24, pt1Peer: 14, pt1Recv: 22, pt1Wait: 4, pt1Send: 10, pt1Hold: 6, pt1Release: 8, pt1Refuse: 4, pt1Restart: 6, pt1KillLink: 4}
	pt1Limits  = [len(pt1Names)]int{pt1Say: 8, pt1Peer: 6, pt1Wait: 3, pt1Send: 3, pt1Hold: 2, pt1Refuse: 1, pt1Restart: 2, pt1KillLink: 1}
)

// pt1Kinds is the kinds, in order.
func pt1Kinds() []pt1Kind {
	out := make([]pt1Kind, len(pt1Names))
	for i := range out {
		out[i] = pt1Kind(i)
	}
	return out
}

// pt1Op is one operation; n is its place in the schedule, which names the text it posts.
type pt1Op struct {
	kind pt1Kind
	n    int
}

func (o pt1Op) String() string { return fmt.Sprintf("%s#%d", pt1Names[o.kind], o.n) }

// pt1Gen is a schedule being drawn: the seed's stream of numbers, how many of each kind so far,
// and the inbox's state.
type pt1Gen struct {
	rng           *rand.ChaCha8
	count         [len(pt1Names)]int
	held, refused bool
}

// pt1Gener is seed's generator: ChaCha8 keyed by the seed alone, whose stream is fixed by Go's
// compatibility promise, so a seed is the same schedule on every machine and toolchain.
func pt1Gener(seed uint64) *pt1Gen {
	var key [32]byte
	binary.LittleEndian.PutUint64(key[:8], seed)
	return &pt1Gen{rng: rand.NewChaCha8(key)}
}

func (g *pt1Gen) allowed(k pt1Kind) bool {
	if k == pt1Release {
		return g.held
	}
	if k == pt1Hold && (g.held || g.refused) || k == pt1Refuse && g.held {
		return false
	}
	return pt1Limits[k] == 0 || g.count[k] < pt1Limits[k]
}

// take records k as drawn.
func (g *pt1Gen) take(k pt1Kind) {
	g.count[k]++
	if k == pt1Hold || k == pt1Release {
		g.held = k == pt1Hold
	}
	g.refused = g.refused || k == pt1Refuse
}

// draw is the next kind: the weights of the kinds still allowed, one number drawn over them in
// the order of the kinds.
func (g *pt1Gen) draw() pt1Kind {
	var total uint64
	for _, k := range pt1Kinds() {
		if g.allowed(k) {
			total += pt1Weights[k]
		}
	}
	n := g.rng.Uint64() % total
	for _, k := range pt1Kinds() {
		if !g.allowed(k) {
			continue
		}
		if n < pt1Weights[k] {
			return k
		}
		n -= pt1Weights[k]
	}
	panic("pt1: a draw over the weights found no kind")
}

// pt1Lengths are the lengths a schedule is drawn at.
const pt1LengthCount uint64 = 7

var pt1Lengths = [pt1LengthCount]int{12, 13, 14, 15, 16, 17, 18}

// pt1Schedule is seed's run: a say first, so that something is owed, 12 to 18 operations drawn,
// and a release if the inbox is still held, so that no schedule ends in a block.
func pt1Schedule(seed uint64) []pt1Op {
	g := pt1Gener(seed)
	ops := []pt1Op{{pt1Say, 0}}
	g.take(pt1Say)
	for want := pt1Lengths[g.rng.Uint64()%pt1LengthCount]; len(ops) < want; {
		k := g.draw()
		g.take(k)
		ops = append(ops, pt1Op{k, len(ops)})
	}
	if g.held {
		ops = append(ops, pt1Op{pt1Release, len(ops)})
	}
	return ops
}

// pt1Text is the text of the item or send the op posts: unique in the run and in every run of
// another seed.
func pt1Text(seed uint64, o pt1Op) string { return fmt.Sprintf("pt1 seed %d op %d", seed, o.n) }

// pt1Copy is one copy of an item the transcript holds: its event id, and whether the entry said
// it may be a second copy.
type pt1Copy struct {
	id     string
	repeat bool
}

// pt1Row is the part of a transcript line a delivery is read from: an idle user turn's message,
// or a busy session's queued command.
type pt1Row struct {
	Type    string `json:"type"`
	Message struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
	Attachment struct {
		Prompt string `json:"prompt"`
	} `json:"attachment"`
}

// text is the delivery the row carries, "" for a row that carries none.
func (r pt1Row) text() string {
	switch r.Type {
	case "user":
		var s string
		if json.Unmarshal(r.Message.Content, &s) != nil {
			return ""
		}
		return s
	case "attachment":
		return r.Attachment.Prompt
	}
	return ""
}

// pt1Copies is every item copy the transcript's deliveries hold, in the order written.
func pt1Copies(jsonl string) ([]pt1Copy, error) {
	var out []pt1Copy
	for i, line := range strings.Split(jsonl, "\n") {
		if line == "" {
			continue
		}
		var row pt1Row
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return nil, fmt.Errorf("transcript line %d: %w", i+1, err)
		}
		out = append(out, pt1Entries(row.text())...)
	}
	return out, nil
}

// pt1Entries are the entries of one delivery's text: each between its `<<teamchat` line and its
// `<<end teamchat` line, with the first `id:` line and a `repeat:` line it holds.
func pt1Entries(text string) []pt1Copy {
	var out []pt1Copy
	var cur *pt1Copy
	for line := range strings.SplitSeq(text, "\n") {
		switch {
		case strings.HasPrefix(line, "<<teamchat "):
			cur = &pt1Copy{}
		case strings.HasPrefix(line, "<<end teamchat "):
			if cur != nil && cur.id != "" {
				out = append(out, *cur)
			}
			cur = nil
		case cur == nil:
		case strings.HasPrefix(line, "id: ") && cur.id == "":
			cur.id = strings.TrimPrefix(line, "id: ")
		case strings.HasPrefix(line, "repeat: "):
			cur.repeat = true
		}
	}
	return out
}

// pt1Sent is a send the run made: its text, and whether the verb answered a `sent` record.
type pt1Sent struct {
	text     string
	answered bool
}

// pt1Seen is what a finished run saw, the whole input of the P-checks.
type pt1Seen struct {
	// posted are the ids of the items the homeserver accepted for the seat.
	posted []string
	// recv is the copies of each id that recv or wait printed.
	recv map[string]int
	// transcript is every copy the fake transcript holds, in order.
	transcript []pt1Copy
	// crashes is the links that were started after the first: each is a crash or a kill.
	crashes int
	// linksAlive is the links of the session alive when the run ended.
	linksAlive int
	// notices is the seat's lifecycle notices in main, each "event reason", by count.
	notices map[string]int
	sends   []pt1Sent
	// inMain is how many times each send's text is in main.
	inMain map[string]int
}

// pt1Check is the properties B 2.15 names, as far as a run's observations reach them; each
// finding starts with its property. P3 (the position is never ahead of its items) has no
// observation of its own: an item the position outran is an item P1 finds lost.
func pt1Check(o pt1Seen) []string {
	out := pt1Items(o)
	out = append(out, pt1Sends(o)...)
	for _, name := range slices.Sorted(maps.Keys(o.notices)) {
		if (strings.HasPrefix(name, "left") || strings.HasPrefix(name, "disconnected")) && o.notices[name] > 1 {
			out = append(out, fmt.Sprintf("P5: the notice %q was posted %d times", name, o.notices[name]))
		}
	}
	if o.linksAlive > 1 {
		out = append(out, fmt.Sprintf("P6: %d links of one session are alive", o.linksAlive))
	}
	return out
}

// pt1Items is P1 (every accepted item shown by a channel), P2 (a transcript copy after the first
// carries repeat) and P11 (handed is exclusive: a copy recv printed, a second copy no mark
// explains). A recv record carries no repeat mark, so every recv copy counts as unmarked; a crash
// may explain one more unmarked copy of any item (a recv whose acknowledgement died with the
// link), and the check cannot tell which, so it allows one per crash.
func pt1Items(o pt1Seen) []string {
	var out []string
	byID := map[string][]pt1Copy{}
	for _, c := range o.transcript {
		byID[c.id] = append(byID[c.id], c)
	}
	for _, id := range slices.Sorted(maps.Keys(byID)) {
		for i, c := range byID[id] {
			if i > 0 && !c.repeat {
				out = append(out, fmt.Sprintf("P2: %s is in the transcript %d times and copy %d is not marked repeat", id, len(byID[id]), i+1))
			}
		}
	}
	for _, id := range o.posted {
		unmarked := o.recv[id]
		for _, c := range byID[id] {
			if !c.repeat {
				unmarked++
			}
		}
		switch {
		case o.recv[id]+len(byID[id]) == 0:
			out = append(out, fmt.Sprintf("P1: %s was accepted and neither recv nor the transcript ever showed it", id))
		case o.recv[id] > 0 && unmarked > 1+o.crashes:
			out = append(out, fmt.Sprintf("P11: %s was shown %d times with no repeat mark (recv %d) after %d crashes", id, unmarked, o.recv[id], o.crashes))
		}
	}
	return out
}

// pt1Sends is P1 for the sends: one the verb answered `sent` is in main exactly once, and none is
// in main twice, a failed one included.
func pt1Sends(o pt1Seen) []string {
	var out []string
	for _, s := range o.sends {
		n := o.inMain[s.text]
		switch {
		case s.answered && n != 1:
			out = append(out, fmt.Sprintf("P1 send: %q was answered sent and is in main %d times", s.text, n))
		case n > 1:
			out = append(out, fmt.Sprintf("P1 send: %q is in main %d times", s.text, n))
		}
	}
	return out
}
