package v1

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// v1's event types and content keys the renderer knows (E15).
const (
	typeMessage   = "m.room.message"
	typeReaction  = "m.reaction"
	typeRedaction = "m.room.redaction"
	typeStatus    = "agent_bus.status"
	keyPing       = "agent_bus.ping"
	keyText       = "agent_bus.text"
	keyReport     = "agent_bus.report"
	keyQuestion   = "agent_bus.question"
	relReplace    = "m.replace"
	relThread     = "m.thread"
)

// timeLayout is each line's UTC time.
const timeLayout = "2006-01-02T15:04:05.000Z"

// roomRecord is one room read: its purpose, id, events in chronological order and state as sent.
type roomRecord struct {
	purpose string
	id      string
	events  []json.RawMessage
	state   []byte
}

// renderHistory is history.md (E15): one section per room, each event's lines in order, the
// agent_bus.status events counted at the section's end.
func renderHistory(teamName, server string, at time.Time, rooms []roomRecord) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# v1 team %s\n\nServer %s, exported %s.\n\n", teamName, server, at.UTC().Format(timeLayout))
	for _, r := range rooms {
		fmt.Fprintf(&b, "## %s %s\n\n", r.purpose, r.id)
		if len(r.events) == 0 {
			b.WriteString("(no events)\n\n")
			continue
		}
		statuses := 0
		for _, raw := range r.events {
			text, status := renderEvent(raw)
			if status {
				statuses++
				continue
			}
			b.WriteString(text + "\n")
		}
		if statuses > 0 {
			fmt.Fprintf(&b, "%s: %d events, counted and not listed.\n\n", typeStatus, statuses)
		}
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

// event is the shape every event the renderer reads has.
type event struct {
	EventID  string          `json:"event_id"`
	Type     string          `json:"type"`
	Sender   string          `json:"sender"`
	TS       int64           `json:"origin_server_ts"`
	StateKey *string         `json:"state_key"`
	Content  json.RawMessage `json:"content"`
	Redacts  string          `json:"redacts"`
	Unsigned struct {
		RedactedBecause json.RawMessage `json:"redacted_because"`
	} `json:"unsigned"`
}

type relation struct {
	RelType string  `json:"rel_type"`
	EventID string  `json:"event_id"`
	Key     *string `json:"key"`
	Reply   *struct {
		EventID string `json:"event_id"`
	} `json:"m.in_reply_to"`
}

type messageContent struct {
	Body       *string   `json:"body"`
	RelatesTo  *relation `json:"m.relates_to"`
	NewContent *struct {
		Body *string `json:"body"`
	} `json:"m.new_content"`
	Ping     json.RawMessage `json:"agent_bus.ping"`
	Text     json.RawMessage `json:"agent_bus.text"`
	Report   json.RawMessage `json:"agent_bus.report"`
	Question json.RawMessage `json:"agent_bus.question"`
	Redacts  string          `json:"redacts"`
}

// renderEvent is one event's lines: a header (time, sender, what it is) and its body indented.
// An agent_bus.status state event is not rendered: status is true. An event of a shape the
// renderer does not know is an UNREADABLE line, never dropped.
func renderEvent(raw json.RawMessage) (string, bool) {
	var e event
	if err := json.Unmarshal(raw, &e); err != nil || e.EventID == "" || e.Type == "" || e.Sender == "" || e.TS == 0 {
		return unreadable(raw), false
	}
	prefix := time.UnixMilli(e.TS).UTC().Format(timeLayout) + " " + oneLine(e.Sender) + " "
	if e.StateKey != nil {
		if e.Type == typeStatus {
			return "", true
		}
		key := *e.StateKey
		if key == "" {
			key = `""`
		}
		return prefix + "STATE " + oneLine(e.Type) + " " + oneLine(key) + "\n", false
	}
	if len(e.Unsigned.RedactedBecause) > 0 && string(e.Unsigned.RedactedBecause) != "null" {
		return prefix + "[redacted] " + oneLine(e.Type) + "\n", false
	}
	head, body, ok := kind(e)
	if !ok {
		return prefix + "UNREADABLE " + oneLine(e.EventID) + " " + oneLine(e.Type) + "\n", false
	}
	return prefix + head + "\n" + indent(body), false
}

// kind is the header word and body of a timeline event that is not state and not redacted.
func kind(e event) (string, string, bool) {
	switch e.Type {
	case typeReaction:
		var c struct {
			RelatesTo *relation `json:"m.relates_to"`
		}
		if json.Unmarshal(e.Content, &c) != nil || c.RelatesTo == nil || c.RelatesTo.Key == nil || c.RelatesTo.EventID == "" {
			return "", "", false
		}
		return "REACTION " + oneLine(*c.RelatesTo.Key) + " to " + oneLine(c.RelatesTo.EventID), "", true
	case typeRedaction:
		var c messageContent
		if json.Unmarshal(e.Content, &c) != nil {
			return "", "", false
		}
		target := e.Redacts
		if target == "" {
			target = c.Redacts
		}
		return "REDACTION of " + oneLine(target), "", target != ""
	case typeMessage:
		var c messageContent
		if json.Unmarshal(e.Content, &c) != nil || c.Body == nil {
			return "", "", false
		}
		head, body, ok := message(c)
		if ok && c.RelatesTo != nil && c.RelatesTo.RelType == relThread {
			head += " in thread " + oneLine(c.RelatesTo.EventID)
		}
		return head, body, ok
	}
	return "", "", false
}

// message is an m.room.message's header word and body: an edit, a v1 ping, text, report or
// question, or any other message with its reply fallback removed.
func message(c messageContent) (string, string, bool) {
	body := *c.Body
	switch {
	case c.RelatesTo != nil && c.RelatesTo.RelType == relReplace:
		if c.RelatesTo.EventID == "" {
			return "", "", false
		}
		if c.NewContent != nil && c.NewContent.Body != nil {
			body = *c.NewContent.Body
		}
		return "EDIT of " + oneLine(c.RelatesTo.EventID), body, true
	case c.Ping != nil:
		head, ok := pingHead(c.Ping)
		return head, "", ok
	case c.Text != nil:
		var tx struct {
			To   []string `json:"to"`
			Text *string  `json:"text"`
		}
		if json.Unmarshal(c.Text, &tx) != nil || tx.To == nil || tx.Text == nil {
			return "", "", false
		}
		return "TEXT -> " + oneLine(strings.Join(tx.To, ",")), *tx.Text, true
	case c.Report != nil:
		return "REPORT", body, true
	case c.Question != nil:
		var q struct {
			N *int `json:"n"`
		}
		if json.Unmarshal(c.Question, &q) != nil || q.N == nil {
			return "", "", false
		}
		return "QUESTION Q" + strconv.Itoa(*q.N), body, true
	}
	if c.RelatesTo != nil && c.RelatesTo.Reply != nil {
		body = stripReplyFallback(body)
	}
	return "MESSAGE", body, true
}

// pingHead is a v1 ping's line: PING <verb> <ref or -> [on <branch>] -> <to> [re <event>].
func pingHead(raw json.RawMessage) (string, bool) {
	var p struct {
		Verb   string   `json:"verb"`
		To     []string `json:"to"`
		Ref    *string  `json:"ref"`
		Branch *string  `json:"branch"`
		Re     *string  `json:"re"`
	}
	if json.Unmarshal(raw, &p) != nil || p.Verb == "" || p.To == nil {
		return "", false
	}
	head := "PING " + oneLine(p.Verb) + " " + orDash(p.Ref)
	if p.Branch != nil {
		head += " on " + oneLine(*p.Branch)
	}
	head += " -> " + oneLine(strings.Join(p.To, ","))
	if p.Re != nil {
		head += " re " + oneLine(*p.Re)
	}
	return head, true
}

// stripReplyFallback removes a reply's leading block of `>` lines and the one blank line after it.
func stripReplyFallback(text string) string {
	lines := strings.Split(text, "\n")
	i := 0
	for i < len(lines) && strings.HasPrefix(lines[i], ">") {
		i++
	}
	if i == 0 {
		return text
	}
	if i < len(lines) && lines[i] == "" {
		i++
	}
	return strings.Join(lines[i:], "\n")
}

// unreadable is the line of an event that is not one the renderer can read: what can be read of
// its time, sender, id and type, else "-" for each.
func unreadable(raw json.RawMessage) string {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		fields = nil
	}
	str := func(key string) string {
		var s string
		if json.Unmarshal(fields[key], &s) != nil || s == "" {
			return "-"
		}
		return oneLine(s)
	}
	at := "-"
	var ts int64
	if json.Unmarshal(fields["origin_server_ts"], &ts) == nil && ts != 0 {
		at = time.UnixMilli(ts).UTC().Format(timeLayout)
	}
	return at + " " + str("sender") + " UNREADABLE " + str("event_id") + " " + str("type") + "\n"
}

func orDash(s *string) string {
	if s == nil {
		return "-"
	}
	return oneLine(*s)
}

// indent is body's lines, each indented four spaces, control characters other than the newline
// shown escaped; an empty body is no lines.
func indent(body string) string {
	if body == "" {
		return ""
	}
	var b strings.Builder
	for line := range strings.SplitSeq(body, "\n") {
		b.WriteString("    " + escape(line) + "\n")
	}
	return b.String()
}

// oneLine is s with every control character, the newline among them, shown escaped.
func oneLine(s string) string { return escape(s) }

func escape(s string) string {
	if !strings.ContainsFunc(s, unicode.IsControl) {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		if unicode.IsControl(r) {
			fmt.Fprintf(&b, `\u%04x`, r)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
