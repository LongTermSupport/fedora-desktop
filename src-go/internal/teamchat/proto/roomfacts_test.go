package proto

import (
	"slices"
	"testing"
)

func TestColourContent(t *testing.T) {
	if got := string(ColourContent("#2dd4bf").Compact()); got != `{"colour":"#2dd4bf"}` {
		t.Errorf("ColourContent = %s", got)
	}
	if got := string(ColourContent("").Compact()); got != `{}` {
		t.Errorf("ColourContent of none = %s", got)
	}
}

// n3e-D4: exactly {"colour": <stored hex>} or {}; anything else is not read.
func TestDecodeColour(t *testing.T) {
	for doc, want := range map[string]string{`{}`: "", `{"colour":"#2dd4bf"}`: "#2dd4bf", `{"colour":"#000000"}`: "#000000"} {
		if got, ok := DecodeColour(mustParse(t, doc)); !ok || got != want {
			t.Errorf("DecodeColour(%s) = %q, %v", doc, got, ok)
		}
	}
	for _, doc := range []string{
		`{"colour":"#2DD4BF"}`, `{"colour":"teal"}`, `{"colour":""}`, `{"colour":"#2dd4bf","x":1}`,
		`{"x":1}`, `{"colour":1}`, `{"colour":null}`, `[]`, `"#2dd4bf"`, `{"Colour":"#2dd4bf"}`,
	} {
		if got, ok := DecodeColour(mustParse(t, doc)); ok || got != "" {
			t.Errorf("DecodeColour(%s) = %q, %v", doc, got, ok)
		}
	}
}

func colourEvent(t *testing.T, sender, key, content string) StateEvent {
	t.Helper()
	return StateEvent{Type: ColourType, StateKey: key, Sender: sender, EventID: "$c", Content: mustParse(t, content)}
}

// MainColour reads the admin's event alone: a member's, or one under another key, is no
// override, and the absence of one is ("", true).
func TestMainColour(t *testing.T) {
	admin := AdminID(testServer)
	other := StateEvent{Type: topicType, Sender: admin, Content: mustParse(t, `{"topic":"x"}`)}
	cases := []struct {
		name  string
		state []StateEvent
		hex   string
		ok    bool
	}{
		{"none", []StateEvent{other}, "", true},
		{"the admin's", []StateEvent{other, colourEvent(t, admin, "", `{"colour":"#2dd4bf"}`)}, "#2dd4bf", true},
		{"the admin's cleared", []StateEvent{colourEvent(t, admin, "", `{}`)}, "", true},
		{"the admin's unreadable", []StateEvent{colourEvent(t, admin, "", `{"colour":"#2DD4BF"}`)}, "", false},
		{"a member's", []StateEvent{colourEvent(t, workerID, "", `{"colour":"#2dd4bf"}`)}, "", true},
		{"another key", []StateEvent{colourEvent(t, admin, "x", `{"colour":"#2dd4bf"}`)}, "", true},
	}
	for _, c := range cases {
		if hex, ok := MainColour(c.state, testServer); hex != c.hex || ok != c.ok {
			t.Errorf("%s: MainColour = %q, %v; want %q, %v", c.name, hex, ok, c.hex, c.ok)
		}
	}
}

func topicEvent(t *testing.T, sender, key, content string) StateEvent {
	t.Helper()
	return StateEvent{Type: topicType, StateKey: key, Sender: sender, EventID: "$t", Content: mustParse(t, content)}
}

// MainDescription is the admin's topic when it is a team description (n3e-D5): never the
// default topic, an invalid description, or anyone else's topic.
func TestMainDescription(t *testing.T) {
	admin := AdminID(testServer)
	cases := []struct {
		name  string
		state []StateEvent
		want  string
		ok    bool
	}{
		{"a description", []StateEvent{topicEvent(t, admin, "", `{"topic":"Builds the docs."}`)}, "Builds the docs.", true},
		{"the default topic", []StateEvent{topicEvent(t, admin, "", `{"topic":"`+defaultMainTopic+`"}`)}, "", false},
		{"an invalid description", []StateEvent{topicEvent(t, admin, "", `{"topic":" padded "}`)}, "", false},
		{"a topic that is not a string", []StateEvent{topicEvent(t, admin, "", `{"topic":1}`)}, "", false},
		{"a member's topic", []StateEvent{topicEvent(t, workerID, "", `{"topic":"Builds the docs."}`)}, "", false},
		{"another key", []StateEvent{topicEvent(t, admin, "x", `{"topic":"Builds the docs."}`)}, "", false},
		{"no topic", nil, "", false},
	}
	for _, c := range cases {
		if got, ok := MainDescription(c.state, testServer); got != c.want || ok != c.ok {
			t.Errorf("%s: MainDescription = %q, %v; want %q, %v", c.name, got, ok, c.want, c.ok)
		}
	}
	// What Desired writes is what MainDescription reads back.
	main := specFor(t, Desired(testServer, testRoster(t), allRooms(), "Builds the docs.", ""), PurposeMain)
	for i := range main.State {
		main.State[i].Sender = admin
	}
	if got, ok := MainDescription(main.State, testServer); !ok || got != "Builds the docs." {
		t.Errorf("Desired's topic reads back as %q, %v", got, ok)
	}
}

// Desired carries the declared colour in main, read back by MainColour; with none it writes no
// colour event, so a team that never declared one is left as it is (C8; n3e-8 writes {} only
// to clear an event that exists).
func TestDesiredColour(t *testing.T) {
	admin := AdminID(testServer)
	for _, ids := range []RoomIDs{{}, allRooms()} {
		main := specFor(t, Desired(testServer, testRoster(t), ids, "", "#2dd4bf"), PurposeMain)
		if got := string(stateOf(t, main, ColourType).Compact()); got != `{"colour":"#2dd4bf"}` {
			t.Errorf("main colour %s", got)
		}
		for i := range main.State {
			main.State[i].Sender = admin
		}
		if hex, ok := MainColour(main.State, testServer); !ok || hex != "#2dd4bf" {
			t.Errorf("Desired's colour reads back as %q, %v", hex, ok)
		}
	}
	for _, colour := range []string{"", "#2DD4BF", "teal"} {
		specs := Desired(testServer, testRoster(t), allRooms(), "", colour)
		if slices.ContainsFunc(specs, func(s RoomSpec) bool { return hasState(s, ColourType) }) {
			t.Errorf("Desired with colour %q writes a colour event", colour)
		}
	}
	for _, side := range []string{PurposeReports, PurposeQuestions} {
		if hasState(specFor(t, Desired(testServer, testRoster(t), allRooms(), "", "#2dd4bf"), side), ColourType) {
			t.Errorf("the %s room has a colour", side)
		}
	}
}

// n3e-D5: the colour and the topic are read from the sync's state section.
func TestTheFilterCarriesTheRoomFacts(t *testing.T) {
	if !slices.Contains(StateTypes, ColourType) || !slices.Contains(StateTypes, topicType) {
		t.Fatalf("state types %v", StateTypes)
	}
}
