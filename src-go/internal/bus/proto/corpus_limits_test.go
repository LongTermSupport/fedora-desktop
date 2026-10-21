package proto

import (
	"errors"
	"maps"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/pyjson"
)

var errNotInt64 = errors.New("not an int within int64")

// int64Of is a case's integer argument: a bool, a float or an int past int64 is a caller error
// (D13).
func int64Of(v any) (int64, error) {
	n, ok := v.(pyjson.Number)
	if !ok {
		return 0, errNotInt64
	}
	i, fits := n.Int64()
	if !fits {
		return 0, errNotInt64
	}
	return i, nil
}

func int64Args(a *args, keys ...string) ([]int64, error) {
	out := make([]int64, len(keys))
	for i, key := range keys {
		v, err := int64Of(a.value(key))
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

func limitsMap(l Limits) map[string]any {
	return map[string]any{
		"send_per_minute": l.SendPerMinute, "send_burst": l.SendBurst, "recv_per_sender_minute": l.RecvPerSenderMinute,
		"ack_timeout_s": l.AckTimeoutS, "halt_ack_timeout_s": l.HaltAckTimeoutS, "human_max_age_s": l.HumanMaxAgeS,
		"wait_timeout_s": l.WaitTimeoutS, "report_per_hour": l.ReportPerHour,
	}
}

func caseLimits(a *args) (Limits, error) { return ParseLimits(a.value("limits")) }

func timeAdapters() map[string]adapter {
	return map[string]adapter{
		"parse_limits": func(a *args) (any, error) {
			l, err := ParseLimits(a.value("value"))
			return limitsMap(l), err
		},
		"check_wait_timeout": func(a *args) (any, error) { return CheckWaitTimeout(a.value("value")) },
		"ack_timeout_s": func(a *args) (any, error) {
			l, err := caseLimits(a)
			if err != nil {
				return nil, err
			}
			s, expected, err := AckTimeoutS(a.str("verb"), l)
			return orNull(s, expected), err
		},
		"ack_deadline_ms": func(a *args) (any, error) {
			l, err := caseLimits(a)
			ts, tsErr := int64Args(a, "sent_ms")
			if err = errors.Join(err, tsErr); err != nil {
				return nil, err
			}
			ms, expected, err := AckDeadlineMS(ts[0], a.str("verb"), l)
			return orNull(ms, expected), err
		},
		"ack_overdue": func(a *args) (any, error) {
			return timed(a, []string{"sent_ms", "now_ms"}, func(ts []int64, l Limits) (any, error) {
				return AckOverdue(ts[0], a.str("verb"), ts[1], l)
			})
		},
		"ping_stale": func(a *args) (any, error) {
			return timed(a, []string{"origin_ms", "now_ms"}, func(ts []int64, l Limits) (any, error) {
				return PingStale(ts[0], a.str("verb"), ts[1], l)
			})
		},
		"human_stale": func(a *args) (any, error) {
			return timed(a, []string{"origin_ms", "now_ms"}, func(ts []int64, l Limits) (any, error) {
				return HumanStale(ts[0], ts[1], l), nil
			})
		},
		"limits_constants": func(*args) (any, error) {
			return map[string]any{
				"duplicate_window_s": DuplicateWindowS, "report_window_s": ReportWindowS, "sync_long_poll_s": SyncLongPollS,
				"server_429_max_tries": Server429MaxTries, "server_429_default_wait_s": Server429DefaultWaitS,
				"server_429_wait_max_s": Server429WaitMaxS,
			}, nil
		},
	}
}

func orNull(v int64, present bool) any {
	if !present {
		return nil
	}
	return v
}

func timed(a *args, keys []string, fn func([]int64, Limits) (any, error)) (any, error) {
	l, err := caseLimits(a)
	ts, tsErr := int64Args(a, keys...)
	if err = errors.Join(err, tsErr); err != nil {
		return nil, err
	}
	return fn(ts, l)
}

func bucketRun(a *args) (any, error) {
	l, err := caseLimits(a)
	if err != nil {
		return nil, err
	}
	b := FullBucket(l, a.int("start_ms"))
	if _, fresh := a.value("state").(pyjson.Null); !fresh {
		if b, err = ParseBucket(a.value("state"), l); err != nil {
			return nil, err
		}
	}
	var took []any
	for _, raw := range a.list("takes") {
		now, err := int64Of(raw)
		if err != nil {
			return nil, err
		}
		var ok bool
		b, ok = b.Take(l, now)
		took = append(took, ok)
	}
	return map[string]any{"took": orEmpty(took), "state": b.State()}, nil
}

func orEmpty(items []any) []any {
	if items == nil {
		return []any{}
	}
	return items
}

func stepArgs(raw any) (*args, error) {
	items, ok := raw.([]any)
	if !ok {
		return nil, errNotInt64
	}
	obj := pyjson.Object{}
	for i, item := range items {
		obj[string(rune('a'+i))] = item
	}
	return &args{obj: obj}, nil
}

func windowRun(a *args) (any, error) {
	w, err := ParseDuplicateWindow(a.value("state"))
	if err != nil {
		return nil, err
	}
	var results []any
	for _, raw := range a.list("ops") {
		step, err := stepArgs(raw)
		if err != nil {
			return nil, err
		}
		if step.str("a") == "seen" {
			results = append(results, w.Seen(step.str("b"), step.int("c")))
		} else {
			w = w.Record(step.str("b"), step.int("c"))
			results = append(results, nil)
		}
		if step.err != nil {
			return nil, step.err
		}
	}
	return map[string]any{"results": orEmpty(results), "state": w.State()}, nil
}

func refusalText(err error) (string, error) {
	if refusal, ok := errors.AsType[*RefusalError](err); ok {
		return string(refusal.Reason), nil
	}
	if err != nil {
		return "", err
	}
	return "ok", nil
}

func gateRun(a *args) (any, error) {
	l, err := caseLimits(a)
	if err != nil {
		return nil, err
	}
	g := FreshSendGate(l, a.int("start_ms"))
	if _, fresh := a.value("state").(pyjson.Null); !fresh {
		if g, err = ParseSendGate(a.value("state"), l); err != nil {
			return nil, err
		}
	}
	var results []any
	for _, raw := range a.list("sends") {
		obj, isObj := raw.(pyjson.Object)
		if !isObj {
			return nil, errNotInt64
		}
		s := &args{obj: obj}
		key := SendKey{Verb: s.str("verb"), Ref: nullable(s, "ref"), Re: nullable(s, "re"), To: s.strs("to")}
		dedupe, isBool := s.value("dedupe").(bool)
		next, admitErr := g.Admit(l, key, s.int("now"), dedupe && isBool)
		text, err := refusalText(admitErr)
		if err = errors.Join(err, s.err); err != nil {
			return nil, err
		}
		g = next
		results = append(results, text)
	}
	return map[string]any{"results": orEmpty(results), "state": g.State()}, nil
}

func nullable(a *args, key string) string {
	s, given := a.optStr(key)
	if !given {
		return ""
	}
	return s
}

func reportRun(a *args) (any, error) {
	w, err := ParseReportWindow(a.value("state"))
	if err != nil {
		return nil, err
	}
	perHour := a.int("per_hour")
	var results []any
	for _, raw := range a.list("ops") {
		step, err := stepArgs(raw)
		if err != nil {
			return nil, err
		}
		var opErr error
		if step.str("a") == "check" {
			opErr = w.Check(perHour, step.int("b"))
		} else {
			var next ReportWindow
			next, opErr = w.Admit(perHour, step.int("b"))
			if opErr == nil {
				w = next
			}
		}
		text, err := refusalText(opErr)
		if err = errors.Join(err, step.err); err != nil {
			return nil, err
		}
		results = append(results, text)
	}
	return map[string]any{"results": orEmpty(results), "state": w.State()}, nil
}

func floodRun(a *args) (any, error) {
	l, err := caseLimits(a)
	if err != nil {
		return nil, err
	}
	flood := ReceiveFlood{}
	var results []any
	for _, raw := range a.list("admits") {
		step, err := stepArgs(raw)
		if err != nil {
			return nil, err
		}
		var ok bool
		flood, ok = flood.Admit(step.str("a"), step.int("b"), l)
		if step.err != nil {
			return nil, step.err
		}
		results = append(results, ok)
	}
	return results, nil
}

// caseHeaders is a header map that answers exactly the names it holds, as a Python dict does;
// a value that is not a string is no header.
type caseHeaders pyjson.Object

func (h caseHeaders) Get(name string) string {
	s, ok := h[name].(string)
	if !ok {
		return ""
	}
	return s
}

func serverWait(a *args) (any, error) {
	var headers HeaderGetter
	if obj, ok := a.value("headers").(pyjson.Object); ok {
		headers = caseHeaders(obj)
	}
	return ServerRetryWaitMS(headers, a.value("body"), nullable(a, "reset_header"), a.int("now_s")), nil
}

func stateAdapters() map[string]adapter {
	return map[string]adapter{
		"token_bucket_run": bucketRun,
		"duplicate_key": func(a *args) (any, error) {
			return DuplicateKey(SendKey{
				Verb: a.str("verb"), Ref: nullable(a, "ref"), Re: nullable(a, "re"), To: a.strs("to"),
			}), nil
		},
		"duplicate_window_run": windowRun,
		"send_gate_run":        gateRun,
		"report_window_run":    reportRun,
		"receive_flood_run":    floodRun,
		"server_retry_wait":    serverWait,
	}
}

// limitsAdapters is the limits and lines suites' corpus adapters.
func limitsAdapters() map[string]adapter {
	all := timeAdapters()
	maps.Copy(all, stateAdapters())
	return all
}
