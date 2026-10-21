package proto

import (
	"errors"
	"math"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/pyjson"
)

func TestElapsedIsExactOrReportsOverflow(t *testing.T) {
	cases := []struct {
		from, now, want int64
		ok              bool
	}{
		{0, 10, 10, true},
		{10, 0, -10, true},
		{math.MinInt64, 0, 0, false},
		{-1, math.MaxInt64, 0, false},
		{1, math.MinInt64, 0, false},
		{math.MinInt64, -1, math.MaxInt64, true},
	}
	for _, c := range cases {
		got, ok := elapsed(c.from, c.now)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("elapsed(%d, %d) = %d, %v", c.from, c.now, got, ok)
		}
	}
}

func TestOlderThanAndWithinAcrossTheWholeRange(t *testing.T) {
	if !olderThan(math.MinInt64, math.MaxInt64, 1000) || olderThan(math.MaxInt64, math.MinInt64, 1000) {
		t.Fatal("olderThan on an overflowing span")
	}
	if within(math.MinInt64, math.MaxInt64, 1000) || !within(math.MaxInt64, math.MinInt64, 1000) {
		t.Fatal("within on an overflowing span")
	}
	if olderThan(0, 1000, 1000) || !olderThan(0, 1001, 1000) || !within(0, 999, 1000) || within(0, 1000, 1000) {
		t.Fatal("the edges")
	}
}

func TestRefilledCapsWithoutOverflow(t *testing.T) {
	cases := []struct{ credit, at, now, rate, capacity, want int64 }{
		{0, 0, 10, 20, 600000, 200},
		{0, 0, math.MaxInt64, 60, 600000, 600000},
		{0, math.MinInt64, math.MaxInt64, 20, 600000, 600000},
		{700000, 5, 5, 20, 600000, 600000},
		{5, 10, 0, 20, 600000, 5},
		{5, 0, 10, 0, 600000, 5},
		{0, 0, 29999, 20, 600000, 599980},
		{0, 0, 30000, 20, 600000, 600000},
		{0, 0, 30001, 20, 600000, 600000},
	}
	for _, c := range cases {
		if got := refilled(c.credit, c.at, c.now, c.rate, c.capacity); got != c.want {
			t.Errorf("refilled(%+v) = %d", c, got)
		}
	}
}

func TestStateIsAValueInAndAValueOut(t *testing.T) {
	l := DefaultLimits()
	flood := ReceiveFlood{}
	next, ok := flood.Admit("alice", 0, l)
	if !ok || len(flood) != 0 || len(next["alice"]) != 1 {
		t.Fatalf("the flood given was changed: %v %v", flood, next)
	}
	window := DuplicateWindow{"k": 0}
	if recorded := window.Record("x", 1); len(window) != 1 || len(recorded) != 2 {
		t.Fatalf("the window given was changed: %v", window)
	}
	reports := ReportWindow{5}
	if admitted, err := reports.Admit(6, 10); err != nil || len(reports) != 1 || len(admitted) != 2 {
		t.Fatalf("the report window given was changed: %v %v", reports, err)
	}
}

func TestRefusalsAreTyped(t *testing.T) {
	gate := FreshSendGate(DefaultLimits(), 0)
	key := SendKey{Verb: VerbHalt, To: []string{"@app.2+desk.podman:server.test"}}
	gate, err := gate.Admit(DefaultLimits(), key, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	_, err = gate.Admit(DefaultLimits(), key, 1, true)
	var refusal *RefusalError
	if !errors.As(err, &refusal) || refusal.Reason != RefuseDuplicate {
		t.Fatalf("a duplicate: %v", err)
	}
	if _, err := (ReportWindow{0}).Admit(1, 1); !errors.As(err, &refusal) || refusal.Reason != ReasonRate {
		t.Fatalf("a report over the hour's cap: %v", err)
	}
}

func TestUnknownVerbAndRangeAreCallerErrors(t *testing.T) {
	l := DefaultLimits()
	if _, _, err := AckTimeoutS("bogus", l); !errors.Is(err, ErrUnknownVerb) {
		t.Fatal(err)
	}
	if _, err := AckOverdue(0, "bogus", 0, l); !errors.Is(err, ErrUnknownVerb) {
		t.Fatal(err)
	}
	if _, _, err := AckDeadlineMS(math.MaxInt64, VerbFetch, l); !errors.Is(err, ErrTimeRange) {
		t.Fatal(err)
	}
	if _, err := ParseLimits(pyjson.Object{"bogus": pyjson.Number{Lit: "1", Kind: pyjson.Int}}); !errors.Is(err, ErrLimits) {
		t.Fatal(err)
	}
}

type headerMap map[string]string

func (h headerMap) Get(name string) string { return h[name] }

func TestServerRetryWaitSaturates(t *testing.T) {
	if got := ServerRetryWaitMS(headerMap{"r": "99999999999999999999"}, nil, "r", math.MinInt64); got != math.MaxInt64 {
		t.Fatal(got)
	}
	if got := ServerRetryWaitMS(headerMap{"r": "5"}, nil, "r", math.MaxInt64); got != 0 {
		t.Fatal(got)
	}
	body := pyjson.Object{"retry_after_ms": pyjson.Number{Lit: "-0", Kind: pyjson.Int}}
	if got := ServerRetryWaitMS(nil, body, "", 0); got != 0 {
		t.Fatal(got)
	}
	body = pyjson.Object{"retry_after_ms": pyjson.Number{Lit: "-99999999999999999999", Kind: pyjson.Int}}
	if got := ServerRetryWaitMS(nil, body, "", 0); got != Server429DefaultWaitS*1000 {
		t.Fatal(got)
	}
}
