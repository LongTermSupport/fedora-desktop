package traffic

import (
	"bytes"
	"strings"
	"testing"
)

// FromJSON reads back what JSON wrote: the team and the counts, every row and room, so the load
// generator replays the probe's own document (Plan 00268 Task 2.1).
func TestFromJSONReadsBackWhatJSONWrote(t *testing.T) {
	tm := newTeam(t)
	tm.traffic()
	c, err := Read(t.Context(), tm.admin, tm.rooms, window(t), Options{})
	if err != nil {
		t.Fatal(err)
	}
	doc, err := JSON("alpha", c)
	if err != nil {
		t.Fatal(err)
	}
	team, got, err := FromJSON(doc)
	if err != nil {
		t.Fatalf("FromJSON: %v", err)
	}
	if team != "alpha" || !bytes.Equal(Encode(got).Compact(), Encode(c).Compact()) {
		t.Fatalf("read back team %q\n%s\nwant\n%s", team, Encode(got).Compact(), Encode(c).Compact())
	}
}

func TestFromJSONRefusesAnotherDocument(t *testing.T) {
	tm := newTeam(t)
	tm.traffic()
	c, err := Read(t.Context(), tm.admin, tm.rooms, window(t), Options{})
	if err != nil {
		t.Fatal(err)
	}
	doc, err := JSON("alpha", c)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"not JSON":      "{",
		"version":       strings.Replace(string(doc), `"v":1`, `"v":2`, 1),
		"unknown field": strings.Replace(string(doc), `"v":1`, `"v":1,"extra":1`, 1),
		"bad since":     strings.Replace(string(doc), `"since":"`, `"since":"x`, 1),
		"bad row hour":  strings.Replace(string(doc), `","hour":"`, `","hour":"x`, 1),
		"zero count":    strings.Replace(string(doc), `"count":1}`, `"count":0}`, 1),
		"trailing":      string(doc) + "{}",
	}
	for name, raw := range cases {
		if raw == string(doc) {
			t.Fatalf("%s: the change did not apply", name)
		}
		if _, _, err := FromJSON([]byte(raw)); err == nil {
			t.Errorf("%s: read", name)
		}
	}
}
