package loadgen

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/traffic"
)

// shapes is the messages in the team's rooms as the traffic probe counts them, one
// "room typed rel" key each ("-" for none), state and membership left out.
func shapes(t *testing.T, team Team, c *stepClock) map[string]int64 {
	t.Helper()
	counts := probe(t, team, traffic.Window{SinceMS: startMS - traffic.HourMS, UntilMS: c.WallMS() + traffic.HourMS})
	out := map[string]int64{}
	for _, r := range counts.Rows {
		if r.Type == proto.MessageType {
			out[fmt.Sprintf("%s %s %s", r.Room, dash(r.Typed), dash(r.Rel))] += r.Count
		}
	}
	return out
}

// probe is the traffic probe's read of the team's rooms over w, as the human.
func probe(t *testing.T, team Team, w traffic.Window) traffic.Counts {
	t.Helper()
	rooms := []traffic.Room{{Purpose: proto.PurposeMain, ID: team.Rooms.Main}, {Purpose: proto.PurposeReports, ID: team.Rooms.Reports}, {Purpose: proto.PurposeQuestions, ID: team.Rooms.Questions}}
	counts, err := traffic.Read(t.Context(), member(t, team, team.Human.Token), rooms, w, traffic.Options{})
	if err != nil {
		t.Fatalf("traffic.Read: %v", err)
	}
	return counts
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// field is v's member name, failing the test when it has none.
func field(t *testing.T, v proto.Value, name string) proto.Value {
	t.Helper()
	got, ok := v.Get(name)
	if !ok {
		t.Fatalf("no %s in %s", name, v.Compact())
	}
	return got
}

func newGen(t *testing.T, team Team, c *stepClock, o Options) *Generator {
	t.Helper()
	g, err := NewGenerator(t.Context(), team, c, o)
	if err != nil {
		t.Fatalf("NewGenerator: %v", err)
	}
	t.Cleanup(g.Close)
	return g
}

func TestEachPatternUnitIsTeamchatsShape(t *testing.T) {
	cases := []struct {
		pattern string
		want    map[string]int64
		made    map[string]int64
		extra   map[string]int64
	}{
		{PatternPings, map[string]int64{"main teamchat.ping -": 1, "main teamchat.ping thread": 1}, map[string]int64{KindPing: 1, KindAnswer: 1}, map[string]int64{}},
		{PatternQuestions, map[string]int64{
			"questions teamchat.question -": 1, "questions - thread": 1, "questions teamchat.thread thread": 2, "questions - edit": 1,
		}, map[string]int64{KindQuestion: 1, KindHumanThread: 1, KindThread: 1, KindClose: 1, KindRetitle: 1}, map[string]int64{}},
		{PatternNotices, map[string]int64{"main teamchat.notice -": 1}, map[string]int64{KindNotice: 1}, map[string]int64{}},
		{PatternReports, map[string]int64{"reports teamchat.report -": 1}, map[string]int64{KindReport: 1}, map[string]int64{}},
		{PatternReceiptEvents, map[string]int64{"main teamchat.ping -": 1, "reports teamchat.receipt -": 1}, map[string]int64{KindReceiptEvent: 1}, map[string]int64{KindPing: 1}},
		{PatternReceipts, map[string]int64{"main - -": 1}, map[string]int64{KindHumanMain: 1, KindRead: 3}, map[string]int64{}},
		{PatternPresence, map[string]int64{}, map[string]int64{KindPresence: 1}, map[string]int64{}},
		{PatternBig, map[string]int64{"main teamchat.text -": 1}, map[string]int64{KindBig: 1}, map[string]int64{}},
	}
	for _, tc := range cases {
		t.Run(tc.pattern, func(t *testing.T) {
			c := newStepClock()
			team := labTeam(t, c, 3)
			g := newGen(t, team, c, Options{})
			if err := g.Unit(t.Context(), tc.pattern); err != nil {
				t.Fatalf("Unit: %v", err)
			}
			if got := shapes(t, team, c); !maps.Equal(got, tc.want) {
				t.Fatalf("shapes %v, want %v", got, tc.want)
			}
			tally := g.Tally()
			if !maps.Equal(tally.Made, tc.made) || !maps.Equal(tally.Extra, tc.extra) || !maps.Equal(tally.Units, map[string]int64{tc.pattern: 1}) {
				t.Fatalf("tally %+v, want made %v, extra %v and one unit", tally, tc.made, tc.extra)
			}
		})
	}
}

func TestPatternsRepeat(t *testing.T) {
	c := newStepClock()
	team := labTeam(t, c, 2)
	g := newGen(t, team, c, Options{})
	for range 3 {
		for _, p := range Patterns {
			if err := g.Unit(t.Context(), p); err != nil {
				t.Fatalf("Unit %s: %v", p, err)
			}
		}
	}
	got := shapes(t, team, c)
	if got["questions teamchat.question -"] != 3 || got["main teamchat.ping thread"] != 3 || got["reports teamchat.receipt -"] != 3 {
		t.Fatalf("three rounds of every pattern: %v", got)
	}
}

func TestReceiptsUnitReadsTheHumanPostByEveryAgent(t *testing.T) {
	c := newStepClock()
	srv := lab(t, labName, c)
	team := setUp(t, srv, c, 3)
	g := newGen(t, team, c, Options{})
	if err := g.Unit(t.Context(), PatternReceipts); err != nil {
		t.Fatalf("Unit: %v", err)
	}
	readers := []string{}
	for _, r := range srv.Receipts() {
		if r.Type == "m.read" && r.RoomID == team.Rooms.Main {
			readers = append(readers, r.UserID)
		}
	}
	slices.Sort(readers)
	want := []string{}
	for _, a := range team.Agents {
		want = append(want, a.UserID)
	}
	slices.Sort(want)
	if !slices.Equal(readers, want) {
		t.Fatalf("m.read by %v, want every agent %v", readers, want)
	}
}

func TestPresenceUnitsFlapInTurn(t *testing.T) {
	c := newStepClock()
	team := labTeam(t, c, 2)
	g := newGen(t, team, c, Options{})
	cl := member(t, team, team.Human.Token)
	want := []struct{ who, presence string }{
		{team.Agents[0].UserID, "online"}, {team.Agents[1].UserID, "online"}, {team.Agents[0].UserID, "offline"},
	}
	for i, w := range want {
		if err := g.Unit(t.Context(), PatternPresence); err != nil {
			t.Fatalf("Unit %d: %v", i, err)
		}
		got, known, err := cl.Presence(t.Context(), w.who)
		if err != nil || !known || got != w.presence {
			t.Fatalf("after unit %d, %s is %q (known %v, %v), want %q", i, w.who, got, known, err, w.presence)
		}
	}
}

func TestBigBodiesAreTheSizeAsked(t *testing.T) {
	for _, size := range []int{1, 1000, proto.MaxText} {
		c := newStepClock()
		team := labTeam(t, c, 2)
		g := newGen(t, team, c, Options{BigBytes: size})
		if err := g.Unit(t.Context(), PatternBig); err != nil {
			t.Fatalf("Unit: %v", err)
		}
		page, err := member(t, team, team.Human.Token).Messages(t.Context(), team.Rooms.Main, matrix.MessagesRequest{Dir: matrix.Backward, Limit: 1})
		if err != nil || len(page.Chunk) != 1 {
			t.Fatalf("Messages: %v", err)
		}
		v, err := proto.Parse(page.Chunk[0])
		if err != nil {
			t.Fatal(err)
		}
		content := field(t, v, "content")
		if text := field(t, field(t, content, proto.KeyText), "text").Str(); len(text) != size {
			t.Fatalf("big text of %d bytes, want %d", len(text), size)
		}
		field(t, content, "formatted_body")
	}
}

func TestOptionsAreBounded(t *testing.T) {
	c := newStepClock()
	team := labTeam(t, c, 2)
	for _, size := range []int{-1, proto.MaxText + 1} {
		if _, err := NewGenerator(t.Context(), team, c, Options{BigBytes: size}); err == nil || !strings.Contains(err.Error(), "--big-bytes") {
			t.Errorf("BigBytes %d: %v", size, err)
		}
	}
}

func TestUnknownPatternOrKindIsRefused(t *testing.T) {
	c := newStepClock()
	team := labTeam(t, c, 2)
	g := newGen(t, team, c, Options{})
	if err := g.Unit(t.Context(), "floods"); err == nil || !strings.Contains(err.Error(), "floods") {
		t.Fatalf("Unit: %v", err)
	}
	if err := g.Kind(t.Context(), "nothing"); err == nil || !strings.Contains(err.Error(), "nothing") {
		t.Fatalf("Kind: %v", err)
	}
}
