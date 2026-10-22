package proto

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strconv"
	"strings"
)

// Limits are the roster's team limits (A 11.1), the same for every member.
type Limits struct {
	SendPerMinute       int64
	SendBurst           int64
	RecvPerSenderMinute int64
	AckTimeoutS         int64
	HaltAckTimeoutS     int64
	HumanMaxAgeS        int64
	ReportPerHour       int64
	DeliverPerSender    int64
}

// limitKey is one row of A 11.1: its wire key, default, bounds and field.
type limitKey struct {
	key           string
	def, low, max int64
	field         func(*Limits) *int64
}

var limitKeys = []limitKey{
	{"send_per_minute", 20, 1, 60, func(l *Limits) *int64 { return &l.SendPerMinute }},
	{"send_burst", 10, 1, 30, func(l *Limits) *int64 { return &l.SendBurst }},
	{"recv_per_sender_minute", 60, 10, 600, func(l *Limits) *int64 { return &l.RecvPerSenderMinute }},
	{"ack_timeout_s", 900, 60, 86400, func(l *Limits) *int64 { return &l.AckTimeoutS }},
	{"halt_ack_timeout_s", 300, 30, 3600, func(l *Limits) *int64 { return &l.HaltAckTimeoutS }},
	{"human_max_age_s", 86400, 3600, 604800, func(l *Limits) *int64 { return &l.HumanMaxAgeS }},
	{"report_per_hour", 6, 1, 60, func(l *Limits) *int64 { return &l.ReportPerHour }},
	{"deliver_per_sender", 10, 1, 60, func(l *Limits) *int64 { return &l.DeliverPerSender }},
}

// DefaultLimits are A 11.1's defaults.
func DefaultLimits() Limits {
	var l Limits
	for _, k := range limitKeys {
		*k.field(&l) = k.def
	}
	return l
}

// DecodeLimits reads a roster's `limits` object: an absent key takes its default, unknown keys
// are ignored, and a value outside its bounds is an error, never clamped (A 11.1).
func DecodeLimits(v Value) (Limits, error) {
	f := readFields(v, "limits")
	l := DefaultLimits()
	for _, k := range limitKeys {
		*k.field(&l) = f.integer(k.key, false, k.def, "limits")
	}
	if f.rule != "" {
		return Limits{}, &RosterError{Rule: f.rule}
	}
	return l, l.Validate()
}

// Validate holds every limit to its bounds.
func (l Limits) Validate() error {
	for _, k := range limitKeys {
		if n := *k.field(&l); n < k.low || n > k.max {
			return &RosterError{Rule: "limits"}
		}
	}
	return nil
}

// Encode is the `limits` object with every key.
func (l Limits) Encode() Value {
	members := make([]Member, 0, len(limitKeys))
	for _, k := range limitKeys {
		members = append(members, M(k.key, Int(*k.field(&l))))
	}
	return Obj(members...)
}

// The fixed windows of A 11.2 and 11.1, in milliseconds. DeliveryWindowMs is the window of B's
// delivery budget, deliver_per_sender items per sender per 600 s (A 11.1, 11.4).
const (
	msPerMinute       = 60_000
	DuplicateWindowMs = 60_000
	ReportWindowMs    = 3_600_000
	ReceiveWindowMs   = msPerMinute
	DeliveryWindowMs  = 600_000
)

// Bucket is the sender's token bucket in integer credit (P61): a token is msPerMinute credit,
// and send_per_minute credit accrues per millisecond, so refill is exact.
type Bucket struct {
	Credit, AtMs int64
}

// FullBucket is a bucket holding its whole burst.
func FullBucket(l Limits, nowMs int64) Bucket {
	return Bucket{Credit: l.SendBurst * msPerMinute, AtMs: nowMs}
}

// Take spends one token if the bucket holds one. A clock that goes back refills nothing.
func (b *Bucket) Take(l Limits, nowMs int64) bool {
	if nowMs > b.AtMs {
		b.Credit += (nowMs - b.AtMs) * l.SendPerMinute
		b.AtMs = nowMs
	}
	b.Credit = min(b.Credit, l.SendBurst*msPerMinute)
	if b.Credit < msPerMinute {
		return false
	}
	b.Credit -= msPerMinute
	return true
}

// DuplicateKey is A 11.2's duplicate identity: the verb or kind, the reference, the `re`, the
// distinct addressees sorted, and the SHA-256 of the free text (empty for a ping, whose text
// is empty). Absent values are empty strings.
func DuplicateKey(verbOrKind, ref, re string, to []string, text string) string {
	sorted := slices.Clone(to)
	slices.Sort(sorted)
	sorted = slices.Compact(sorted)
	addressees := make([]Value, 0, len(sorted))
	for _, id := range sorted {
		addressees = append(addressees, Str(id))
	}
	digest := ""
	if text != "" {
		sum := sha256.Sum256([]byte(text))
		digest = hex.EncodeToString(sum[:])
	}
	return string(Arr(Str(verbOrKind), Str(ref), Str(re), Arr(addressees...), Str(digest)).Compact())
}

// DupWindow holds the duplicate keys sent in the last DuplicateWindowMs, with when.
type DupWindow struct {
	Sent map[string]int64
}

// Seen is whether key was sent within the window.
func (w *DupWindow) Seen(key string, nowMs int64) bool {
	at, ok := w.Sent[key]
	return ok && nowMs-at < DuplicateWindowMs
}

// Record enters key at now and forgets the keys that left the window.
func (w *DupWindow) Record(key string, nowMs int64) {
	if w.Sent == nil {
		w.Sent = map[string]int64{}
	}
	for k := range w.Sent {
		if !w.Seen(k, nowMs) {
			delete(w.Sent, k)
		}
	}
	w.Sent[key] = nowMs
}

// SendGate is the sender's flood control: the duplicate window, then the bucket (A 11.2).
type SendGate struct {
	Bucket Bucket
	Recent DupWindow
}

// NewSendGate is a gate with a full bucket and an empty window.
func NewSendGate(l Limits, nowMs int64) SendGate {
	return SendGate{Bucket: FullBucket(l, nowMs)}
}

// Admit records one send, or returns its refusal (duplicate, rate) having recorded nothing.
// A notice is exempt from the duplicate window (dedupe false) and does not enter it.
func (g *SendGate) Admit(l Limits, key string, dedupe bool, nowMs int64) string {
	if dedupe && g.Recent.Seen(key, nowMs) {
		return ReasonDuplicate
	}
	if !g.Bucket.Take(l, nowMs) {
		return ReasonRate
	}
	if dedupe {
		g.Recent.Record(key, nowMs)
	}
	return ""
}

// Window counts admissions per key over a sliding span: the report window (one key, the
// member), the receive flood (per sender; a refusal is never recorded, so a flood cannot
// extend its own window) and the delivery budget (per DeliveryKey).
type Window struct {
	Times map[string][]int64
}

// Count is the admissions of key within the span.
func (w *Window) Count(key string, spanMs, nowMs int64) int {
	return len(w.recent(key, spanMs, nowMs))
}

// Admit records one admission of key unless limit are already within the span.
func (w *Window) Admit(key string, limit int64, spanMs, nowMs int64) bool {
	times := w.recent(key, spanMs, nowMs)
	if int64(len(times)) >= limit {
		return false
	}
	if w.Times == nil {
		w.Times = map[string][]int64{}
	}
	w.Times[key] = append(slices.Clone(times), nowMs)
	return true
}

// Prune forgets every key with nothing left in the span.
func (w *Window) Prune(spanMs, nowMs int64) {
	for key := range w.Times {
		if times := w.recent(key, spanMs, nowMs); len(times) == 0 {
			delete(w.Times, key)
		} else {
			w.Times[key] = times
		}
	}
}

func (w *Window) recent(key string, spanMs, nowMs int64) []int64 {
	times := w.Times[key]
	i := 0
	for i < len(times) && nowMs-times[i] >= spanMs {
		i++
	}
	return times[i:]
}

// DeliveryKey is the delivery budget's key, one per (team, sender) (A 11.1). A team id holds
// no space, so the pair cannot collide.
func DeliveryKey(team, sender string) string { return team + " " + sender }

// The server-wait rules of A 11.3.
const (
	MaxSendTries       = 3
	defaultRetryWaitMs = 5000
	maxSleptWaitMs     = 60_000
)

// RetryWait is how long to wait before retrying a 429 (A 11.3): `Retry-After` in seconds, else
// the reset header (Unix seconds) against now, else the body's `retry_after_ms`, else 5 s. A
// wait over 60 s is reported, not slept (report true).
func RetryWait(retryAfter, reset string, bodyMs int64, hasBody bool, nowS int64) (int64, bool) {
	var waitMs int64
	switch {
	case digits(retryAfter):
		waitMs = mustDigits(retryAfter) * 1000
	case digits(reset):
		waitMs = max(0, mustDigits(reset)-nowS) * 1000
	case hasBody && bodyMs >= 0:
		waitMs = bodyMs
	default:
		waitMs = defaultRetryWaitMs
	}
	return waitMs, waitMs > maxSleptWaitMs
}

// digits is a header of at most nine decimal digits, surrounding spaces allowed.
func digits(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 9 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func mustDigits(s string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0
	}
	return n
}
