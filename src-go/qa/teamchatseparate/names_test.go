package teamchatseparate

import (
	"os"
	"slices"
	"strings"
	"testing"
)

func TestParseNamesReadsBothForms(t *testing.T) {
	names, err := ParseNames("# comment\n\nagent-bus\n  pingbus  \nElement- !teamchat-\n")
	if err != nil {
		t.Fatal(err)
	}
	want := []Name{{Text: "agent-bus"}, {Text: "pingbus"}, {Text: "Element-", Next: "teamchat-"}}
	if !slices.Equal(names, want) {
		t.Fatalf("got %+v want %+v", names, want)
	}
}

func TestParseNamesRefusesABadList(t *testing.T) {
	for _, c := range []struct{ data, want string }{
		{"", "no v1 name"},
		{"# only a comment\n", "no v1 name"},
		{"pingbus\npingbus\n", "line 2: pingbus is listed twice"},
		{"Element- !\n", "line 1: "},
		{" !teamchat-\n", "line 1: "},
		{"a !b !c\n", "line 1: "},
		{"pïngbus\n", "line 1: not ASCII"},
		{"two words\n", "line 1: "},
	} {
		_, err := ParseNames(c.data)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("ParseNames(%q) = %v, want an error containing %q", c.data, err, c.want)
		}
	}
}

// The plain form is found in any letter case, every occurrence, with its byte range.
func TestPlainNameIsFoundInAnyCase(t *testing.T) {
	n := Name{Text: "pingbus"}
	got := n.Find("x PingBus y PINGBUS_z pingbus")
	want := []Hit{{Start: 2, End: 9}, {Start: 12, End: 19}, {Start: 22, End: 29}}
	if !slices.Equal(got, want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
	if hits := n.Find("ping bus, pingbu"); len(hits) != 0 {
		t.Fatalf("a near miss was found: %+v", hits)
	}
}

// A non-ASCII letter before the name does not shift the reported range.
func TestPlainNameRangeIsInBytesOfTheLine(t *testing.T) {
	line := "İ pingbus"
	hits := Name{Text: "pingbus"}.Find(line)
	if len(hits) != 1 || line[hits[0].Start:hits[0].End] != "pingbus" {
		t.Fatalf("hits %+v", hits)
	}
}

// The exact form is found as written, wherever its Next does not follow.
func TestExactNameIsFoundUnlessNextFollows(t *testing.T) {
	n := Name{Text: "Element-", Next: "teamchat-"}
	for _, c := range []struct {
		line string
		hits int
	}{
		{"config/Element-ops", 1},
		{"config/Element-teamchat-ops", 0},
		{"config/element-ops", 0},
		{"teamchat-ops-element.desktop", 0},
		{"Element-teamchat-a and Element-b", 1},
		{"Element-", 1},
	} {
		if got := n.Find(c.line); len(got) != c.hits {
			t.Errorf("Find(%q) = %+v, want %d hit(s)", c.line, got, c.hits)
		}
	}
}

// The shipped list is D 4.1's, parses, and finds each of its own names.
func TestTheShippedListIsD41s(t *testing.T) {
	data, err := os.ReadFile(namesFile[len(packageDir):])
	if err != nil {
		t.Fatal(err)
	}
	names, err := ParseNames(string(data))
	if err != nil {
		t.Fatal(err)
	}
	texts := make([]string, 0, len(names))
	for _, n := range names {
		texts = append(texts, n.Text)
	}
	want := []string{
		"agent-bus", "agent_bus", "agentbus", "pingbus", "PINGBUS_", "AGENT_BUS_", "CCY_TEAMS_PINGBUS",
		"CC_TEAMS_", "/run/ccy/membership", "$XDG_RUNTIME_DIR/ccy/membership", ".claude/ccy/pingbus",
		"/opt/claude-yolo/optional/agent-bus", "Element-",
	}
	if !slices.Equal(texts, want) {
		t.Fatalf("the list is %q, want D 4.1's %q", texts, want)
	}
	for _, n := range names {
		if len(n.Find("x"+n.Text+"y")) == 0 {
			t.Errorf("%q does not find itself", n.Text)
		}
	}
}
