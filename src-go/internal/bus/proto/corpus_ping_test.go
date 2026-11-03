package proto

import (
	"maps"
)

// orNil is "" as null.
func orNil(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func pingJSON(p Ping) map[string]any {
	var ref any
	if p.Ref != nil {
		ref = p.Ref.Text
	}
	return map[string]any{
		"verb": p.Verb, "to": p.To, "ref": ref, "re": orNil(p.Re), "trust": orNil(p.Trust), "branch": orNil(p.Branch),
	}
}

func outcomeJSON(o Outcome) any {
	switch {
	case o.Ping != nil:
		item := pingJSON(*o.Ping)
		maps.Copy(item, map[string]any{
			"sender": o.Ping.Sender, "event_id": o.Ping.EventID, "ts": o.Ping.TS, "copy_of": o.Ping.CopyOf,
		})
		return map[string]any{"verdict": string(Accept), "ping": item}
	case o.Human != nil:
		h := o.Human
		return map[string]any{"verdict": string(Accept), "human": map[string]any{
			"event_id": h.EventID, "sender": h.Sender, "ts": h.TS, "text": h.Text, "revises": orNil(h.Revises),
		}}
	case o.Peer != nil:
		p := o.Peer
		return map[string]any{"verdict": string(Accept), "peer": map[string]any{
			"event_id": p.EventID, "sender": p.Sender, "ts": p.TS, "text": p.Text, "copy_of": p.CopyOf,
		}}
	}
	return map[string]any{"verdict": string(o.Verdict), "reason": string(o.Reason)}
}

func (a *args) flag(key string) bool {
	b, ok := a.value(key).(bool)
	if !ok {
		a.fail(key, "a bool")
	}
	return b
}

// optField is a nullable string argument, "" for null.
func (a *args) optField(key string) string {
	s, given := a.optStr(key)
	if !given {
		return ""
	}
	return s
}

func (a *args) pingFields() PingFields {
	return PingFields{Ref: a.optField("ref"), Re: a.optField("re"), Branch: a.optField("branch")}
}

func receiveArgs(a *args) (Outcome, error) {
	seen := map[string]bool{}
	for _, id := range a.strs("seen") {
		seen[id] = true
	}
	r := Receiver{Self: a.str("self"), Seen: seen, HumanText: a.flag("human_text"), Room: a.str("room")}
	return ValidateEvent(a.value("event"), a.context(), r)
}

func reasonStrings(rs []Reason) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = string(r)
	}
	return out
}

// pingAdapters is the ping, text, secret and receive suites' corpus adapters.
func pingAdapters() map[string]adapter {
	all := map[string]adapter{
		"build_ping": func(a *args) (any, error) {
			return BuildPing(a.str("verb"), a.strs("to"), a.pingFields()), nil
		},
		"render_ping": func(a *args) (any, error) {
			return RenderPing(a.str("verb"), a.strs("to"), a.pingFields()), nil
		},
		"validate_ping_content": func(a *args) (any, error) {
			p, err := ValidatePingContent(a.value("content"), a.context())
			return pingJSON(p), err
		},
		"build_text":  func(a *args) (any, error) { return BuildText(a.strs("to"), a.str("text"), a.optField("re")) },
		"render_text": func(a *args) (any, error) { return RenderText(a.strs("to"), a.str("text")), nil },
		"validate_text_content": func(a *args) (any, error) {
			t, err := ValidateTextContent(a.value("content"), a.context(), a.flag("check_secret"))
			return map[string]any{"to": t.To, "text": t.Text}, err
		},
		"secret_shape": func(a *args) (any, error) { return orNil(SecretShape(a.str("value"))), nil },
	}
	maps.Copy(all, receiveAdapters())
	return all
}

func receiveAdapters() map[string]adapter {
	return map[string]adapter{
		"validate_event": func(a *args) (any, error) {
			o, err := receiveArgs(a)
			return outcomeJSON(o), err
		},
		"strip_reply_fallback": func(a *args) (any, error) { return StripReplyFallback(a.str("value")), nil },
		"reached_no_agent": func(a *args) (any, error) {
			o, err := receiveArgs(a)
			return Unheard(a.value("event"), a.context(), o), err
		},
		"closed_sets": func(*args) (any, error) {
			return map[string]any{
				"drop":    reasonStrings(DropReasons),
				"send":    reasonStrings(SendRefusals),
				"unheard": reasonStrings(UnheardReasons),
			}, nil
		},
	}
}
