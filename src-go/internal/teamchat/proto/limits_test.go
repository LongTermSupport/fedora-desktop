package proto

import (
	"strings"
	"testing"
)

func TestDefaultLimitsAreA11(t *testing.T) {
	want := Limits{
		SendPerMinute: 20, SendBurst: 10, RecvPerSenderMinute: 60, AckTimeoutS: 900,
		HaltAckTimeoutS: 300, HumanMaxAgeS: 86400, ReportPerHour: 6, DeliverPerSender: 10,
	}
	if DefaultLimits() != want {
		t.Fatalf("DefaultLimits = %+v", DefaultLimits())
	}
	if err := want.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestLimitBoundsAreInclusive(t *testing.T) {
	bounds := map[string][2]int64{
		"send_per_minute": {1, 60}, "send_burst": {1, 30}, "recv_per_sender_minute": {10, 600},
		"ack_timeout_s": {60, 86400}, "halt_ack_timeout_s": {30, 3600}, "human_max_age_s": {3600, 604800},
		"report_per_hour": {1, 60}, "deliver_per_sender": {1, 60},
	}
	if len(bounds) != len(limitKeys) {
		t.Fatalf("limitKeys has %d keys", len(limitKeys))
	}
	for key, b := range bounds {
		for value, ok := range map[int64]bool{b[0]: true, b[1]: true, b[0] - 1: false, b[1] + 1: false} {
			_, err := DecodeLimits(Obj(M(key, Int(value))))
			if (err == nil) != ok {
				t.Errorf("%s = %d: err %v", key, value, err)
			}
		}
	}
	l := DefaultLimits()
	l.SendPerMinute = 3
	back, err := DecodeLimits(l.Encode())
	if err != nil || back != l {
		t.Fatalf("Encode round trip = %+v, %v", back, err)
	}
}

func TestBucketRefillsInIntegerCredit(t *testing.T) {
	l := DefaultLimits()
	b := FullBucket(l, 0)
	for i := range 10 {
		if !b.Take(l, 0) {
			t.Fatalf("burst refused at %d", i)
		}
	}
	if b.Take(l, 0) {
		t.Fatal("eleventh send at once admitted")
	}
	if b.Take(l, 2999) {
		t.Fatal("refilled before 3 s at 20 per minute")
	}
	if !b.Take(l, 3000) {
		t.Fatal("not refilled after 3 s")
	}
	b.Take(l, 1000)
	if b.AtMs != 3000 {
		t.Fatalf("a clock going back moved the bucket: %d", b.AtMs)
	}
	full := FullBucket(l, 0)
	full.Take(l, 10_000_000)
	if full.Credit != (10-1)*msPerMinute {
		t.Fatalf("credit over capacity: %d", full.Credit)
	}
}

func TestDuplicateKey(t *testing.T) {
	a := DuplicateKey("review", "commit:o/r@x", "", []string{"@b:s", "@a:s"}, "")
	b := DuplicateKey("review", "commit:o/r@x", "", []string{"@a:s", "@b:s", "@a:s"}, "")
	if a != b || !strings.HasSuffix(a, `,""]`) {
		t.Fatalf("keys %s and %s", a, b)
	}
	t1 := DuplicateKey("text", "", "", []string{"@a:s"}, "hello")
	t2 := DuplicateKey("text", "", "", []string{"@a:s"}, "hello!")
	if t1 == t2 || !strings.Contains(t1, "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824") {
		t.Fatalf("text digest wrong: %s", t1)
	}
}

func TestSendGate(t *testing.T) {
	l := DefaultLimits()
	l.SendBurst = 2
	g := NewSendGate(l, 0)
	key := DuplicateKey("halt", "", "", []string{"@a:s"}, "")
	if r := g.Admit(l, key, true, 0); r != "" {
		t.Fatalf("first send refused %q", r)
	}
	if r := g.Admit(l, key, true, 1); r != ReasonDuplicate {
		t.Fatalf("repeat in window gave %q", r)
	}
	if !g.Recent.Seen(key, 59_999) {
		t.Fatal("the window forgot a key before 60 s")
	}
	if r := g.Admit(l, "other", true, 1); r != "" {
		t.Fatalf("second key refused %q", r)
	}
	if r := g.Admit(l, "third", true, 1); r != ReasonRate {
		t.Fatalf("over the bucket gave %q", r)
	}
	if g.Recent.Seen("third", 1) {
		t.Fatal("a refused send entered the window")
	}
	if r := g.Admit(l, key, false, 60_000); r != "" {
		t.Fatalf("a notice was refused %q", r)
	}
	if g.Recent.Seen(key, 60_000) {
		t.Fatal("the window keeps a key at 60 s, or a notice entered it")
	}
}

func TestSlidingWindows(t *testing.T) {
	var w Window
	for i := range 6 {
		if !w.Admit("me", 6, ReportWindowMs, int64(i)) {
			t.Fatalf("report %d refused", i)
		}
	}
	if w.Admit("me", 6, ReportWindowMs, 3_599_999) {
		t.Fatal("seventh report in the hour admitted")
	}
	if !w.Admit("me", 6, ReportWindowMs, 3_600_000) {
		t.Fatal("the oldest report did not leave the window")
	}
	if w.Count("me", ReportWindowMs, 3_600_000) != 6 || w.Count("other", ReportWindowMs, 0) != 0 {
		t.Fatal("Count wrong")
	}
	if !w.Admit("other", 1, ReportWindowMs, 0) {
		t.Fatal("keys share a window")
	}
	w.Prune(ReportWindowMs, 10*ReportWindowMs)
	if len(w.Times) != 0 {
		t.Fatalf("Prune kept %v", w.Times)
	}
	if DeliveryKey("t", "@a:s") == DeliveryKey("t@", "a:s") {
		t.Fatal("delivery keys collide")
	}
}

func TestRetryWait(t *testing.T) {
	cases := []struct {
		name              string
		retryAfter, reset string
		bodyMs            int64
		hasBody           bool
		wantMs            int64
		wantReport        bool
	}{
		{name: "retry-after", retryAfter: " 7 ", reset: "999", wantMs: 7000},
		{name: "reset", retryAfter: "soon", reset: "1012", wantMs: 12000},
		{name: "reset past", reset: "990", wantMs: 0},
		{name: "body", bodyMs: 1500, hasBody: true, wantMs: 1500},
		{name: "default", wantMs: 5000},
		{name: "long", retryAfter: "61", wantMs: 61000, wantReport: true},
		{name: "sixty", retryAfter: "60", wantMs: 60000},
		{name: "negative body", bodyMs: -1, hasBody: true, wantMs: 5000},
	}
	for _, c := range cases {
		ms, report := RetryWait(c.retryAfter, c.reset, c.bodyMs, c.hasBody, 1000)
		if ms != c.wantMs || report != c.wantReport {
			t.Errorf("%s: RetryWait = %d, %v; want %d, %v", c.name, ms, report, c.wantMs, c.wantReport)
		}
	}
	if MaxSendTries != 3 {
		t.Fatal("tries")
	}
}

func TestTheFixedWindowsAreA11(t *testing.T) {
	windows := map[string][2]int64{
		"duplicate": {DuplicateWindowMs, 60_000}, "report_per_hour": {ReportWindowMs, 3_600_000},
		"recv_per_sender_minute": {ReceiveWindowMs, 60_000}, "deliver_per_sender": {DeliveryWindowMs, 600_000},
	}
	for name, w := range windows {
		if w[0] != w[1] {
			t.Errorf("%s window %d ms, want %d", name, w[0], w[1])
		}
	}
	budget := DefaultLimits().DeliverPerSender
	var w Window
	for i := range budget {
		if !w.Admit("sender", budget, DeliveryWindowMs, i) {
			t.Fatalf("delivery %d refused", i)
		}
	}
	if w.Admit("sender", budget, DeliveryWindowMs, DeliveryWindowMs-1) || !w.Admit("sender", budget, DeliveryWindowMs, DeliveryWindowMs) {
		t.Fatalf("the delivery budget is not %d per 600 s", budget)
	}
}
