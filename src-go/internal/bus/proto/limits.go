package proto

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"math/big"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/pyjson"
)

// The fixed limits of §10 (SPEC P62, P64, P66).
const (
	DuplicateWindowS      = 60
	ReportWindowS         = 3600
	SyncLongPollS         = 30
	Server429MaxTries     = 3
	Server429DefaultWaitS = 5
	Server429WaitMaxS     = 60
)

// RefuseDuplicate is the send gate's refusal of a ping equal to one sent within the duplicate
// window (SPEC P63); a refusal by the bucket or the report window is ReasonRate.
const RefuseDuplicate Reason = "duplicate"

const (
	msPerSecond = 1000
	msPerMinute = 60 * msPerSecond
)

var (
	// ErrLimits is a member config's limits that are not an object of known keys with integer
	// values in bounds: a config error, never clamped (SPEC P59).
	ErrLimits = errors.New("bad limits")
	// ErrTimeRange is an ack deadline past int64 (D13).
	ErrTimeRange = errors.New("the time is past int64")
	// ErrLimitState is a persisted limit state that is not exactly the shape this package writes.
	ErrLimitState = errors.New("bad limit state")
)

// Limits is §10's member limits (SPEC P59).
type Limits struct {
	SendPerMinute       int64
	SendBurst           int64
	RecvPerSenderMinute int64
	AckTimeoutS         int64
	HaltAckTimeoutS     int64
	HumanMaxAgeS        int64
	WaitTimeoutS        int64
	ReportPerHour       int64
}

type limitBound struct {
	key                string
	low, high, initial int64
	field              func(*Limits) *int64
}

// limitBounds is §10's table, in its order: inclusive bounds and the default.
var limitBounds = []limitBound{
	{"send_per_minute", 1, 60, 20, func(l *Limits) *int64 { return &l.SendPerMinute }},
	{"send_burst", 1, 30, 10, func(l *Limits) *int64 { return &l.SendBurst }},
	{"recv_per_sender_minute", 10, 600, 60, func(l *Limits) *int64 { return &l.RecvPerSenderMinute }},
	{"ack_timeout_s", 60, 86400, 900, func(l *Limits) *int64 { return &l.AckTimeoutS }},
	{"halt_ack_timeout_s", 30, 3600, 300, func(l *Limits) *int64 { return &l.HaltAckTimeoutS }},
	{"human_max_age_s", 3600, 604800, 86400, func(l *Limits) *int64 { return &l.HumanMaxAgeS }},
	{"wait_timeout_s", 1, 1790, 1500, func(l *Limits) *int64 { return &l.WaitTimeoutS }},
	{"report_per_hour", 1, 60, 6, func(l *Limits) *int64 { return &l.ReportPerHour }},
}

// DefaultLimits is §10's defaults (SPEC P59).
func DefaultLimits() Limits {
	var l Limits
	for _, b := range limitBounds {
		*b.field(&l) = b.initial
	}
	return l
}

func (b limitBound) check(v any) (int64, error) {
	n, isNum := v.(pyjson.Number)
	i, fits := n.Int64()
	if !isNum || !fits || i < b.low || i > b.high {
		return 0, fmt.Errorf("%w: limits.%s must be an integer from %d to %d", ErrLimits, b.key, b.low, b.high)
	}
	return i, nil
}

// ParseLimits is SPEC P59: the member config's optional limits object (nil or null for none);
// the keys given are never echoed, as one may be a value pasted into the wrong place.
func ParseLimits(v any) (Limits, error) {
	l := DefaultLimits()
	switch obj := v.(type) {
	case nil, pyjson.Null:
		return l, nil
	case pyjson.Object:
		for key := range obj {
			if !slices.ContainsFunc(limitBounds, func(b limitBound) bool { return b.key == key }) {
				return l, fmt.Errorf("%w: limits may hold only the keys of §10", ErrLimits)
			}
		}
		for _, b := range limitBounds {
			if raw, given := obj[b.key]; given {
				i, err := b.check(raw)
				if err != nil {
					return l, err
				}
				*b.field(&l) = i
			}
		}
		return l, nil
	}
	return l, fmt.Errorf("%w: limits must be an object", ErrLimits)
}

// CheckWaitTimeout is SPEC P59: a `wait --timeout` value, held to wait_timeout_s's bounds.
func CheckWaitTimeout(v any) (int64, error) {
	for _, b := range limitBounds {
		if b.key == "wait_timeout_s" {
			return b.check(v)
		}
	}
	return 0, ErrLimits
}

// elapsed is now - from, or false when that is past int64.
func elapsed(from, now int64) (int64, bool) {
	if (from < 0 && now > math.MaxInt64+from) || (from > 0 && now < math.MinInt64+from) {
		return 0, false
	}
	return now - from, true
}

// olderThan is now - from > span, exactly over the whole int64 range.
func olderThan(from, now, span int64) bool {
	d, ok := elapsed(from, now)
	if !ok {
		return now > from
	}
	return d > span
}

// within is now - at < span, exactly over the whole int64 range.
func within(at, now, span int64) bool {
	d, ok := elapsed(at, now)
	if !ok {
		return now < at
	}
	return d < span
}

// AckTimeoutS is SPEC P60: the seconds a target has to ack or nack the verb, and false when no
// ack is expected; an unknown verb is ErrUnknownVerb.
func AckTimeoutS(verb string, l Limits) (int64, bool, error) {
	rule, known := Verbs[verb]
	switch {
	case !known:
		return 0, false, fmt.Errorf("%w: %q", ErrUnknownVerb, verb)
	case !rule.Ack:
		return 0, false, nil
	case verb == VerbHalt:
		return l.HaltAckTimeoutS, true, nil
	}
	return l.AckTimeoutS, true, nil
}

// AckDeadlineMS is SPEC P60: sent + the verb's timeout, false for a verb that expects no ack,
// ErrTimeRange when past int64.
func AckDeadlineMS(sentMS int64, verb string, l Limits) (int64, bool, error) {
	timeout, expected, err := AckTimeoutS(verb, l)
	if err != nil || !expected {
		return 0, false, err
	}
	if sentMS > math.MaxInt64-timeout*msPerSecond {
		return 0, false, ErrTimeRange
	}
	return sentMS + timeout*msPerSecond, true, nil
}

// AckOverdue is SPEC P60: whether a silent target of an ack-expected ping is owed a TIMEOUT
// line (now past the deadline).
func AckOverdue(sentMS int64, verb string, nowMS int64, l Limits) (bool, error) {
	timeout, expected, err := AckTimeoutS(verb, l)
	if err != nil || !expected {
		return false, err
	}
	return olderThan(sentMS, nowMS, timeout*msPerSecond), nil
}

// PingStale is SPEC P60 (§9 step 08p): older than the verb's ack timeout, ack_timeout_s for a
// verb that expects none.
func PingStale(originMS int64, verb string, nowMS int64, l Limits) (bool, error) {
	timeout, expected, err := AckTimeoutS(verb, l)
	if err != nil {
		return false, err
	}
	if !expected {
		timeout = l.AckTimeoutS
	}
	return olderThan(originMS, nowMS, timeout*msPerSecond), nil
}

// HumanStale is SPEC P60: a human message, or an agent text, older than human_max_age_s.
func HumanStale(originMS, nowMS int64, l Limits) bool {
	return olderThan(originMS, nowMS, l.HumanMaxAgeS*msPerSecond)
}

// Bucket is the send token bucket in integer credit (SPEC P61): a token is a minute of
// milliseconds, and send_per_minute credit accrues per millisecond, so refill is exact. A
// Bucket comes from FullBucket or ParseBucket.
type Bucket struct {
	Credit int64
	AtMS   int64
}

// FullBucket is a bucket holding send_burst tokens at now.
func FullBucket(l Limits, nowMS int64) Bucket {
	return Bucket{Credit: l.SendBurst * msPerMinute, AtMS: nowMS}
}

// refilled is min(capacity, credit + max(0, now - at) * rate), without overflow.
func refilled(credit, at, now, rate, capacity int64) int64 {
	if credit >= capacity {
		return capacity
	}
	if now <= at || rate <= 0 {
		return credit
	}
	d, ok := elapsed(at, now)
	if !ok || d > (capacity-credit)/rate {
		return capacity
	}
	return credit + d*rate
}

// Take is SPEC P61: the bucket refilled to now (the clock never moved back) less one token, and
// whether there was one. A refused take still returns the refilled bucket.
func (b Bucket) Take(l Limits, nowMS int64) (Bucket, bool) {
	b.Credit = refilled(b.Credit, b.AtMS, nowMS, l.SendPerMinute, l.SendBurst*msPerMinute)
	b.AtMS = max(b.AtMS, nowMS)
	if b.Credit < msPerMinute {
		return b, false
	}
	b.Credit -= msPerMinute
	return b, true
}

// State is the bucket as its persisted object.
func (b Bucket) State() map[string]any { return map[string]any{"credit": b.Credit, "at_ms": b.AtMS} }

func exactKeys(state any, keys ...string) (pyjson.Object, error) {
	obj, ok := state.(pyjson.Object)
	if !ok || len(obj) != len(keys) {
		return nil, ErrLimitState
	}
	for _, key := range keys {
		if _, has := obj[key]; !has {
			return nil, ErrLimitState
		}
	}
	return obj, nil
}

// stateInt is an int (not a bool) within int64 (D13).
func stateInt(v any) (int64, error) {
	n, isNum := v.(pyjson.Number)
	i, fits := n.Int64()
	if !isNum || !fits {
		return 0, ErrLimitState
	}
	return i, nil
}

// ParseBucket is SPEC P61: exactly {credit, at_ms}, ints, credit non-negative and capped at the
// capacity on load (a credit past int64 is capped too).
func ParseBucket(state any, l Limits) (Bucket, error) {
	obj, err := exactKeys(state, "credit", "at_ms")
	if err != nil {
		return Bucket{}, err
	}
	at, err := stateInt(obj["at_ms"])
	if err != nil {
		return Bucket{}, err
	}
	capacity := l.SendBurst * msPerMinute
	n, isNum := obj["credit"].(pyjson.Number)
	if !isNum || n.Kind != pyjson.Int || strings.HasPrefix(n.Lit, "-") && n.Lit != "-0" {
		return Bucket{}, ErrLimitState
	}
	credit, fits := n.Int64()
	if !fits {
		credit = capacity
	}
	return Bucket{Credit: min(credit, capacity), AtMS: at}, nil
}

// SendKey is what the duplicate window compares: the verb, the ref and re ("" for none) and the
// set of to (SPEC P62).
type SendKey struct {
	Verb, Ref, Re string
	To            []string
}

// DuplicateKey is SPEC P62: compact ASCII JSON of [verb, ref, re, sorted(set(to))].
func DuplicateKey(k SendKey) string {
	to := slices.Compact(sortedCopy(k.To))
	items := make([]any, len(to))
	for i, s := range to {
		items[i] = s
	}
	return dumps([]any{k.Verb, nullIfEmpty(k.Ref), nullIfEmpty(k.Re), items}, true)
}

func nullIfEmpty(s string) any {
	if s == "" {
		return pyjson.Null{}
	}
	return s
}

// DuplicateWindow is the keys of pings sent in the last DuplicateWindowS, with when (SPEC P62).
type DuplicateWindow map[string]int64

// Seen is whether the key was sent within the window before now.
func (w DuplicateWindow) Seen(key string, nowMS int64) bool {
	at, ok := w[key]
	return ok && within(at, nowMS, DuplicateWindowS*msPerSecond)
}

// Record is a new window holding the key at now, less the keys the window has dropped.
func (w DuplicateWindow) Record(key string, nowMS int64) DuplicateWindow {
	out := DuplicateWindow{}
	for k, at := range w {
		if w.Seen(k, nowMS) {
			out[k] = at
		}
	}
	out[key] = nowMS
	return out
}

// State is the window as its persisted object.
func (w DuplicateWindow) State() map[string]any {
	out := make(map[string]any, len(w))
	for k, at := range w {
		out[k] = at
	}
	return out
}

// ParseDuplicateWindow is SPEC P62: an object of string to int.
func ParseDuplicateWindow(state any) (DuplicateWindow, error) {
	obj, ok := state.(pyjson.Object)
	if !ok {
		return nil, ErrLimitState
	}
	out := DuplicateWindow{}
	for k, raw := range obj {
		at, err := stateInt(raw)
		if err != nil {
			return nil, err
		}
		out[k] = at
	}
	return out, nil
}

// SendGate is §9 send step 5 (SPEC P63): the duplicate window, then the token bucket.
type SendGate struct {
	Bucket Bucket
	Recent DuplicateWindow
}

// FreshSendGate is a full bucket at now and an empty window.
func FreshSendGate(l Limits, nowMS int64) SendGate {
	return SendGate{Bucket: FullBucket(l, nowMS), Recent: DuplicateWindow{}}
}

// Admit is SPEC P63: the gate with the send recorded, or a RefusalError (RefuseDuplicate, then
// ReasonRate) with nothing recorded. dedupe false skips the window both ways (pingbus's own
// notices).
func (g SendGate) Admit(l Limits, k SendKey, nowMS int64, dedupe bool) (SendGate, error) {
	key := DuplicateKey(k)
	if dedupe && g.Recent.Seen(key, nowMS) {
		return g, refuse(RefuseDuplicate)
	}
	bucket, took := g.Bucket.Take(l, nowMS)
	g.Bucket = bucket
	if !took {
		return g, refuse(ReasonRate)
	}
	if dedupe {
		g.Recent = g.Recent.Record(key, nowMS)
	}
	return g, nil
}

// State is the gate as its persisted object.
func (g SendGate) State() map[string]any {
	return map[string]any{"bucket": g.Bucket.State(), "recent": g.Recent.State()}
}

// ParseSendGate is SPEC P63: exactly {bucket, recent}.
func ParseSendGate(state any, l Limits) (SendGate, error) {
	obj, err := exactKeys(state, "bucket", "recent")
	if err != nil {
		return SendGate{}, err
	}
	bucket, err := ParseBucket(obj["bucket"], l)
	if err != nil {
		return SendGate{}, err
	}
	recent, err := ParseDuplicateWindow(obj["recent"])
	if err != nil {
		return SendGate{}, err
	}
	return SendGate{Bucket: bucket, Recent: recent}, nil
}

// ReportWindow is the send times of this member's reports, oldest first (SPEC P64).
type ReportWindow []int64

// Check is SPEC P64: ReasonRate when another report now would pass perHour.
func (w ReportWindow) Check(perHour, nowMS int64) error {
	if int64(len(w.recent(nowMS))) >= perHour {
		return refuse(ReasonRate)
	}
	return nil
}

// Admit is SPEC P64: a new window with a report sent now, or ReasonRate having recorded nothing.
func (w ReportWindow) Admit(perHour, nowMS int64) (ReportWindow, error) {
	if err := w.Check(perHour, nowMS); err != nil {
		return w, err
	}
	return append(w.recent(nowMS), nowMS), nil
}

// State is the window as its persisted list.
func (w ReportWindow) State() []any {
	out := make([]any, len(w))
	for i, at := range w {
		out[i] = at
	}
	return out
}

func (w ReportWindow) recent(nowMS int64) ReportWindow {
	var out ReportWindow
	for _, at := range w {
		if within(at, nowMS, ReportWindowS*msPerSecond) {
			out = append(out, at)
		}
	}
	return out
}

// ParseReportWindow is SPEC P64: a list of ints.
func ParseReportWindow(state any) (ReportWindow, error) {
	items, ok := state.([]any)
	if !ok {
		return nil, ErrLimitState
	}
	out := make(ReportWindow, len(items))
	for i, raw := range items {
		at, err := stateInt(raw)
		if err != nil {
			return nil, err
		}
		out[i] = at
	}
	return out, nil
}

// ReceiveFlood is the times admitted per sender (SPEC P65); it lives only as long as the
// process that receives.
type ReceiveFlood map[string][]int64

// Admit is SPEC P65: a new flood state and whether the sender is admitted now, at most
// recv_per_sender_minute in any sliding minute; a refusal is not counted.
func (f ReceiveFlood) Admit(sender string, nowMS int64, l Limits) (ReceiveFlood, bool) {
	times := f[sender]
	for len(times) > 0 && !within(times[0], nowMS, msPerMinute) {
		times = times[1:]
	}
	out := maps.Clone(f)
	if out == nil {
		out = ReceiveFlood{}
	}
	admitted := int64(len(times)) < l.RecvPerSenderMinute
	if admitted {
		times = append(slices.Clone(times), nowMS)
	}
	out[sender] = times
	return out, admitted
}

// HeaderGetter is a response's headers (http.Header is one); "" is an absent header.
type HeaderGetter interface{ Get(name string) string }

// headerSeconds is the header as whole seconds: ASCII digits after Python's strip() (D14).
func headerSeconds(h HeaderGetter, name string) (*big.Int, bool) {
	if h == nil || name == "" {
		return nil, false
	}
	text := pyStrip(h.Get(name))
	if text == "" || strings.Trim(text, "0123456789") != "" {
		return nil, false
	}
	return new(big.Int).SetString(text, 10)
}

func saturatedMS(ms *big.Int) int64 {
	if !ms.IsInt64() {
		return math.MaxInt64
	}
	return ms.Int64()
}

var thousand = big.NewInt(msPerSecond)

// ServerRetryWaitMS is SPEC P66: the wait before retrying a server 429, in milliseconds held to
// int64: Retry-After, then resetHeader when named (Unix seconds against nowS, never negative),
// then the body's retry_after_ms, then Server429DefaultWaitS.
func ServerRetryWaitMS(h HeaderGetter, body any, resetHeader string, nowS int64) int64 {
	if secs, ok := headerSeconds(h, "Retry-After"); ok {
		return saturatedMS(secs.Mul(secs, thousand))
	}
	if reset, ok := headerSeconds(h, resetHeader); ok {
		reset.Sub(reset, big.NewInt(nowS))
		if reset.Sign() < 0 {
			return 0
		}
		return saturatedMS(reset.Mul(reset, thousand))
	}
	if obj, ok := body.(pyjson.Object); ok {
		if n, isNum := obj["retry_after_ms"].(pyjson.Number); isNum && n.Kind == pyjson.Int {
			if ms, parsed := new(big.Int).SetString(n.Lit, 10); parsed && ms.Sign() >= 0 {
				return saturatedMS(ms)
			}
		}
	}
	return Server429DefaultWaitS * msPerSecond
}
