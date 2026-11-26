package traffic

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"
)

// notCounted is what the read cannot see: ephemeral events, which room history does not keep.
var notCounted = []string{"m.read receipts", "typing", "presence"}

const (
	hourForm = "2006-01-02T15:04Z"
	timeForm = time.RFC3339
)

func at(ms int64, form string) string { return time.UnixMilli(ms).UTC().Format(form) }

// hour is one hour of the window: each room's events in it and their total.
type hour struct {
	startMS int64
	rooms   map[string]int64
	total   int64
}

// hours is every hour of the window, the quiet ones with 0.
func (c Counts) hours() []hour {
	var out []hour
	index := map[int64]int{}
	for h := c.Window.firstHour(); h < c.Window.UntilMS; h += HourMS {
		index[h] = len(out)
		out = append(out, hour{startMS: h, rooms: map[string]int64{}})
	}
	for _, r := range c.Rows {
		h := &out[index[r.HourMS]]
		h.rooms[r.Room] += r.Count
		h.total += r.Count
	}
	return out
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// WriteTable is the counts for a person: the window, each room's read, the events per hour
// and room, then each row.
func WriteTable(w io.Writer, team string, c Counts) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	var b strings.Builder
	fmt.Fprintf(&b, "team %s: %s to %s (UTC, the end excluded)\n\nroom\tevents\tpages\n", team, at(c.Window.SinceMS, timeForm), at(c.Window.UntilMS, timeForm))
	for _, r := range c.Rooms {
		fmt.Fprintf(&b, "%s\t%d\t%d\n", r.Room, r.Events, r.Pages)
	}
	b.WriteString("\nevents per hour\nhour (UTC)")
	for _, r := range c.Rooms {
		b.WriteString("\t" + r.Room)
	}
	b.WriteString("\ttotal\n")
	for _, h := range c.hours() {
		b.WriteString(at(h.startMS, hourForm))
		for _, r := range c.Rooms {
			fmt.Fprintf(&b, "\t%d", h.rooms[r.Room])
		}
		fmt.Fprintf(&b, "\t%d\n", h.total)
	}
	b.WriteString("\nevents by kind\nhour (UTC)\troom\ttype\ttyped\trelation\tmembership\tcount\n")
	for _, r := range c.Rows {
		fmt.Fprintf(&b, "%s\t%s\t%s\t%s\t%s\t%s\t%d\n", at(r.HourMS, hourForm), r.Room, r.Type, dash(r.Typed), dash(r.Rel), dash(r.Membership), r.Count)
	}
	fmt.Fprintf(&b, "\n%s are not kept in room history: not counted.\n", strings.Join(notCounted, ", "))
	if _, err := io.WriteString(tw, b.String()); err != nil {
		return err
	}
	return tw.Flush()
}

type jsonRoom struct {
	Room   string `json:"room"`
	Events int64  `json:"events"`
	Pages  int64  `json:"pages"`
}

type jsonHour struct {
	Hour  string           `json:"hour"`
	Rooms map[string]int64 `json:"rooms"`
	Total int64            `json:"total"`
}

type jsonRow struct {
	Room       string `json:"room"`
	Hour       string `json:"hour"`
	Type       string `json:"type"`
	Typed      string `json:"typed"`
	Rel        string `json:"rel"`
	Membership string `json:"membership"`
	Count      int64  `json:"count"`
}

type jsonDoc struct {
	V          int        `json:"v"`
	Team       string     `json:"team"`
	Since      string     `json:"since"`
	Until      string     `json:"until"`
	Rooms      []jsonRoom `json:"rooms"`
	Hours      []jsonHour `json:"hours"`
	Rows       []jsonRow  `json:"rows"`
	NotCounted []string   `json:"not_counted"`
}

// FromJSON reads JSON's document back: the team and the counts, refused unless the document
// is version 1 holding only JSON's fields and the counts keep Validate's rule. The hours are
// derived from the rows, so they are not read.
func FromJSON(data []byte) (string, Counts, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var doc jsonDoc
	if err := dec.Decode(&doc); err != nil {
		return "", Counts{}, fmt.Errorf("the traffic document: %w", err)
	}
	if dec.More() {
		return "", Counts{}, errors.New("the traffic document has more after its one object")
	}
	if doc.V != 1 {
		return "", Counts{}, fmt.Errorf("the traffic document's version is %d, want 1", doc.V)
	}
	since, err := time.Parse(timeForm, doc.Since)
	if err != nil {
		return "", Counts{}, fmt.Errorf("the traffic document's since: %w", err)
	}
	until, err := time.Parse(timeForm, doc.Until)
	if err != nil {
		return "", Counts{}, fmt.Errorf("the traffic document's until: %w", err)
	}
	c := Counts{Window: Window{SinceMS: since.UnixMilli(), UntilMS: until.UnixMilli()}}
	for _, r := range doc.Rooms {
		c.Rooms = append(c.Rooms, RoomRead(r))
	}
	for _, r := range doc.Rows {
		hour, err := time.Parse(timeForm, r.Hour)
		if err != nil {
			return "", Counts{}, fmt.Errorf("a row's hour: %w", err)
		}
		c.Rows = append(c.Rows, Row{Key: Key{Room: r.Room, HourMS: hour.UnixMilli(), Type: r.Type, Typed: r.Typed, Rel: r.Rel, Membership: r.Membership}, Count: r.Count})
	}
	if err := c.Validate(); err != nil {
		return "", Counts{}, fmt.Errorf("the traffic document: %w", err)
	}
	c.sortRows()
	return doc.Team, c, nil
}

// JSON is the counts for a program: one document, times in RFC 3339 UTC, every hour of the
// window listed with each room read.
func JSON(team string, c Counts) ([]byte, error) {
	doc := jsonDoc{
		V: 1, Team: team, Since: at(c.Window.SinceMS, timeForm), Until: at(c.Window.UntilMS, timeForm),
		Rooms: []jsonRoom{}, Hours: []jsonHour{}, Rows: []jsonRow{}, NotCounted: notCounted,
	}
	for _, r := range c.Rooms {
		doc.Rooms = append(doc.Rooms, jsonRoom(r))
	}
	for _, h := range c.hours() {
		rooms := map[string]int64{}
		for _, r := range c.Rooms {
			rooms[r.Room] = h.rooms[r.Room]
		}
		doc.Hours = append(doc.Hours, jsonHour{Hour: at(h.startMS, timeForm), Rooms: rooms, Total: h.total})
	}
	for _, r := range c.Rows {
		doc.Rows = append(doc.Rows, jsonRow{
			Room: r.Room, Hour: at(r.HourMS, timeForm), Type: r.Type, Typed: r.Typed, Rel: r.Rel, Membership: r.Membership, Count: r.Count,
		})
	}
	return json.Marshal(doc)
}
