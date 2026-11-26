package admin

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/traffic"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// trafficBody is the text the traffic tests' events carry, which no output may hold.
const trafficBody = "SECRET-TRAFFIC-BODY"

// trafficHour is the start of the hour the traffic tests' events are in, a day after t0.
var trafficHour = time.UnixMilli(t0ms).UTC().Truncate(time.Hour).Add(24 * time.Hour)

// setAt moves the world's clock to t.
func (w *world) setAt(t time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.at = t
}

// sendAt posts a message of the typed key into room at minute m of trafficHour.
func (w *world) sendAt(t *testing.T, room string, m int, key string, rel map[string]any) string {
	t.Helper()
	w.setAt(trafficHour.Add(time.Duration(m) * time.Minute))
	content := map[string]any{"msgtype": "m.text", "body": trafficBody, key: map[string]any{"v": 1, "text": trafficBody}}
	if rel != nil {
		content["m.relates_to"] = rel
	}
	raw, err := json.Marshal(content)
	if err != nil {
		t.Fatal(err)
	}
	id, err := w.admin.Send(t.Context(), room, "m.room.message", "traffic"+strconv.Itoa(m), raw)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// traffic is alpha applied with a ping and its thread answer in main, a question in
// questions and a receipt in reports, all in trafficHour.
func (w *world) traffic(t *testing.T) {
	t.Helper()
	w.applied(t)
	ids := w.rooms(t)
	ping := w.sendAt(t, ids.Main, 1, proto.KeyPing, nil)
	w.sendAt(t, ids.Main, 2, proto.KeyText, map[string]any{"rel_type": "m.thread", "event_id": ping})
	w.sendAt(t, ids.Questions, 3, proto.KeyQuestion, nil)
	w.sendAt(t, ids.Reports, 4, proto.KeyReceipt, nil)
	w.setAt(trafficHour.Add(2 * time.Hour))
}

func trafficArgs(more ...string) []string {
	return append([]string{
		"team", "traffic", "--team", "alpha",
		"--since", trafficHour.Format(time.RFC3339), "--until", trafficHour.Add(time.Hour).Format(time.RFC3339),
	}, more...)
}

// TR1: `team traffic` reads the window's counts through the child and prints them as a table,
// with no body.
func TestTeamTrafficPrintsTheTable(t *testing.T) {
	w := newWorld(t)
	w.traffic(t)
	out := w.ok(t, "", trafficArgs()...)
	for _, want := range []string{"team alpha", "teamchat.ping", "teamchat.question", "teamchat.receipt", "thread", "not kept in room history"} {
		if !strings.Contains(out.stdout, want) {
			t.Errorf("table lacks %q:\n%s", want, out.stdout)
		}
	}
	if strings.Contains(out.stdout+out.stderr, trafficBody) {
		t.Errorf("output holds a body:\n%s%s", out.stdout, out.stderr)
	}
	w.lockIsFree(t)
}

// TR2: --json prints one document of the counts.
func TestTeamTrafficPrintsJSON(t *testing.T) {
	w := newWorld(t)
	w.traffic(t)
	out := w.ok(t, "", trafficArgs("--json")...)
	var doc struct {
		Team  string `json:"team"`
		Hours []struct {
			Total int64 `json:"total"`
		} `json:"hours"`
		Rows []struct {
			Room  string `json:"room"`
			Typed string `json:"typed"`
			Rel   string `json:"rel"`
			Count int64  `json:"count"`
		} `json:"rows"`
	}
	if err := json.Unmarshal([]byte(out.stdout), &doc); err != nil {
		t.Fatalf("%s: %v", out.stdout, err)
	}
	if doc.Team != "alpha" || len(doc.Hours) != 1 || doc.Hours[0].Total != 4 || len(doc.Rows) != 4 {
		t.Errorf("json %s", out.stdout)
	}
	if strings.Contains(out.stdout, trafficBody) {
		t.Errorf("json holds a body: %s", out.stdout)
	}
}

// TR3: a flag the verb cannot take is 64, before the child is asked.
func TestTeamTrafficUsage(t *testing.T) {
	w := newWorld(t)
	w.applied(t)
	since, until := trafficHour.Format(time.RFC3339), trafficHour.Add(time.Hour).Format(time.RFC3339)
	for name, args := range map[string][]string{
		"no since":   {"--team", "alpha", "--until", until},
		"no until":   {"--team", "alpha", "--since", since},
		"bad time":   {"--team", "alpha", "--since", "yesterday", "--until", until},
		"reversed":   {"--team", "alpha", "--since", until, "--until", since},
		"too long":   {"--team", "alpha", "--since", since, "--until", trafficHour.Add(traffic.MaxWindow + time.Hour).Format(time.RFC3339)},
		"extra flag": {"--team", "alpha", "--since", since, "--until", until, "--body"},
	} {
		got := w.verb("", append([]string{"team", "traffic"}, args...)...)
		if got.code != term.ExitUsage || got.stdout != "" {
			t.Errorf("%s = %+v", name, got)
		}
	}
}

// TR4: an answer that is not a traffic answer of the window asked is refused, and nothing is
// printed.
func TestTeamTrafficRefusesALyingChild(t *testing.T) {
	for name, lie := range map[string]func(*Answer){
		"other window": func(a *Answer) { a.Traffic.Window.UntilMS += traffic.HourMS },
		"a handle":     func(a *Answer) { a.Handle = sampleHandle },
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			w.traffic(t)
			w.lie = lie
			got := w.verb("", trafficArgs()...)
			if got.code == term.ExitOK || got.stdout != "" || !strings.Contains(got.stderr, "not what it was asked") {
				t.Fatalf("%+v", got)
			}
		})
	}
}

// TR5: a team not applied is refused (70), and so is a window the child would not read.
func TestTeamTrafficOfATeamNotApplied(t *testing.T) {
	w := newWorld(t)
	got := w.verb("", trafficArgs()...)
	if got.code != ExitRefused || !strings.Contains(got.stderr, "not applied") {
		t.Fatalf("%+v", got)
	}
}

// TR6: the child's own checks of its args: a window it would not read, and keys that are not
// integers.
func TestServeTrafficChecksItsArgs(t *testing.T) {
	w := newWorld(t)
	w.applied(t)
	_, tf, err := readTeamFile(w.root, alpha(t))
	if err != nil {
		t.Fatal(err)
	}
	for name, args := range map[string][]proto.Member{
		"reversed":  {proto.M(keySinceMS, proto.Int(2)), proto.M(keyUntilMS, proto.Int(1))},
		"not a int": {proto.M(keySinceMS, proto.Str("1")), proto.M(keyUntilMS, proto.Int(2))},
	} {
		a := Serve(t.Context(), w.open, Request{Op: OpTeamTraffic, Team: alpha(t), Args: proto.Obj(append([]proto.Member{proto.M(keyTeamFile, tf)}, args...)...)})
		if a.OK || a.Error.Class != ClassFailed {
			t.Errorf("%s: %+v", name, a)
		}
	}
}
