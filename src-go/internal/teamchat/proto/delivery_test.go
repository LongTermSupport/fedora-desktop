package proto

import (
	"slices"
	"strings"
	"testing"
)

func mustRender(t *testing.T, d Delivery) []string {
	t.Helper()
	text, err := d.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	return strings.Split(text, "\n")
}

func TestDeliveryIsTheWorkedExample(t *testing.T) {
	d := Delivery{
		Nonce: envNonce, Team: envTeam, Entries: []Entry{humanEntry(), peerEntry()},
		HeldBack: []HeldBack{{From: "myrepo.4+desk.lxc", Count: 12}},
	}
	got := mustRender(t, d)
	human, peer := mustLines(t, humanEntry()), mustLines(t, peerEntry())
	want := []string{"teamchat delivery 3f9c2a71: 2 messages on myteam (1 from humans, 1 from agents, 0 pings, 0 notices, 0 timeouts): alice (HUMAN), " + envWorker + " (PEER)"}
	want = append(append(append(want, "<<teamchat 3f9c2a71 1/2>>"), human...), "<<end teamchat 3f9c2a71 1/2>>")
	want = append(append(append(want, "<<teamchat 3f9c2a71 2/2>>"), peer...), "<<end teamchat 3f9c2a71 2/2>>")
	want = append(want, "held back: 12 from myrepo.4+desk.lxc (flood limit), not delivered; read them with "+envHistory)
	wantLines(t, "delivery", got, want...)
}

func TestDeliveryFirstLine(t *testing.T) {
	ping, check := pingEntry(), pingEntry()
	check.From, check.Role, check.Verb, check.Ref, check.Trust, check.Branch = envProbe, RoleProbe, VerbCheck, "", "", ""
	cases := map[string]struct {
		entries []Entry
		want    string
	}{
		"none": {nil, "0 messages on myteam (0 from humans, 0 from agents, 0 pings, 0 notices, 0 timeouts): -"},
		"one":  {[]Entry{reminderEntry()}, "1 message on myteam (0 from humans, 0 from agents, 0 pings, 0 notices, 1 timeouts): humans (TIMEOUT)"},
		"distinct": {
			[]Entry{humanEntry(), humanEntry(), ping, check, timeoutEntry(), noticeEntry(EventParked)},
			"6 messages on myteam (2 from humans, 0 from agents, 2 pings, 1 notices, 1 timeouts): alice (HUMAN), " + envCoord + " (PING), " + envProbe + " (PING) and 2 more",
		},
	}
	for name, tc := range cases {
		got := mustRender(t, Delivery{Nonce: envNonce, Team: envTeam, Entries: tc.entries})[0]
		if got != "teamchat delivery "+envNonce+": "+tc.want {
			t.Errorf("%s: %q", name, got)
		}
	}
}

func TestDeliveryTrailingLines(t *testing.T) {
	d := Delivery{Nonce: envNonce, Team: envTeam, Gap: true, Stopped: true, Pending: 3}
	for i := range 9 {
		d.Answered = append(d.Answered, Answered{From: envWorker, Answer: VerbDone, Verb: "review", PingID: qid('p'), AfterTimeout: i == 1})
		d.HeldBack = append(d.HeldBack, HeldBack{From: envWorker, Count: i + 1})
		d.NotShown = append(d.NotShown, qid('h'))
	}
	d.HeldBack = append(d.HeldBack, HeldBack{From: envCoord, Count: 5})
	d.NotShown[0] = "not an id"
	got := mustRender(t, d)
	answered := "answered: " + envWorker + " sent done to your review '" + qid('p') + "'"
	notShown := "not shown: a message from a team human could not be shown; read it with "
	wantLines(t, "answered", got[1:3], answered, answered+" (after its TIMEOUT)")
	wantLines(t, "answered summary", got[9:12], "answered: 1 more; see teamchat status --team myteam",
		"held back: 1 from "+envWorker+" (flood limit), not delivered; read them with "+envHistory,
		"held back: 2 from "+envWorker+" (flood limit), not delivered; read them with "+envHistory)
	wantLines(t, "held summary", got[18:21], "held back: 14 more from other senders; see "+envHistory,
		notShown+envHistory, notShown+"teamchat verify '"+qid('h')+"'")
	wantLines(t, "tail", got[27:], "not shown: 1 more; see "+envHistory,
		"gap: some messages could not be fetched; read them with "+envHistory,
		"stopped: this team stopped on a failure that does not clear by itself; run teamchat status --team myteam and tell a human",
		"3 more pending: they follow in the next delivery")
}

func TestDeliveryRefusals(t *testing.T) {
	bad := humanEntry()
	bad.From = "Alice"
	other := humanEntry()
	other.Team = "otherteam"
	cases := map[string]struct {
		mutate func(*Delivery)
		slot   string
	}{
		"nonce":        {func(d *Delivery) { d.Nonce = "3F9C2A71" }, "nonce"},
		"team":         {func(d *Delivery) { d.Team = "" }, "team"},
		"another team": {func(d *Delivery) { d.Entries = []Entry{other} }, "team"},
		"entry":        {func(d *Delivery) { d.Entries = []Entry{humanEntry(), bad} }, "from"},
		"answered sender": {func(d *Delivery) {
			d.Answered = []Answered{{From: "@x:y", Answer: VerbAck, Verb: "review", PingID: qid('p')}}
		}, "answered"},
		"answer verb": {func(d *Delivery) {
			d.Answered = []Answered{{From: envWorker, Answer: "review", Verb: "review", PingID: qid('p')}}
		}, "answered"},
		"answered id": {func(d *Delivery) {
			d.Answered = []Answered{{From: envWorker, Answer: VerbAck, Verb: "review", PingID: "x"}}
		}, "answered"},
		"held sender":  {func(d *Delivery) { d.HeldBack = []HeldBack{{From: "a b", Count: 1}} }, "held back"},
		"held count":   {func(d *Delivery) { d.HeldBack = []HeldBack{{From: envWorker, Count: 0}} }, "held back"},
		"pending":      {func(d *Delivery) { d.Pending = -1 }, "pending"},
		"huge pending": {func(d *Delivery) { d.Pending = 1_000_000_000 }, "pending"},
	}
	for name, tc := range cases {
		d := Delivery{Nonce: envNonce, Team: envTeam}
		tc.mutate(&d)
		_, err := d.Render()
		if got := slotOf(err); got != tc.slot {
			t.Errorf("%s: slot %q (%v), want %q", name, got, err, tc.slot)
		}
	}
	_, err := Delivery{Nonce: envNonce, Team: envTeam, Entries: []Entry{humanEntry(), bad}}.Render()
	if !strings.Contains(err.Error(), "entry 2") {
		t.Errorf("the failing entry is not named: %v", err)
	}
}

// longestHandle is the longest the handle grammar allows (A 2.3).
var longestHandle = strings.Repeat("r", 48) + "." + strings.Repeat("s", 12) + "+" + strings.Repeat("x", 63) + ".podman"

// largestEntry is the widest entry the grammars allow on the wire: a HUMAN in questions whose
// body and headline take the most bytes once ASCII-escaped for the socket (U+00E9 is 2 bytes
// shown and 6 escaped), with every optional line.
func largestEntry() Entry {
	e := humanEntry()
	e.Questions, e.Revises, e.Re, e.Repeat, e.Restored = true, qid('o'), qid('r'), true, true
	e.Question = &EntryQuestion{N: 999_999_999, Headline: strings.Repeat(string(rune(0x1F600)), MaxHeadline)}
	e.From, e.Handle, e.Team = strings.Repeat("a", 32), longestHandle, strings.Repeat("t", 22)
	e.Body = strings.Repeat(string(rune(0xE9)), MaxHumanBody)
	return e
}

func TestTheLargestEntryFitsAlone(t *testing.T) {
	e := largestEntry()
	d := Delivery{Nonce: envNonce, Team: e.Team, Entries: []Entry{e}}
	text, err := d.Render()
	if err != nil {
		t.Fatal(err)
	}
	if n := WireBytes(text); n > MaxEntryBytes {
		t.Fatalf("the largest entry takes %d bytes on the wire, over %d", n, MaxEntryBytes)
	}
}

func mustFit(t *testing.T, d Delivery) Delivery {
	t.Helper()
	fit, err := d.Fit()
	if err != nil {
		t.Fatalf("Fit: %v", err)
	}
	return fit
}

func TestDeliveryFit(t *testing.T) {
	many := slices.Repeat([]Entry{humanEntry()}, 10)
	if fit := mustFit(t, Delivery{Nonce: envNonce, Team: envTeam, Entries: many}); len(fit.Entries) != MaxDeliveryEntries || fit.Pending != 2 {
		t.Errorf("small entries: %d, %d pending", len(fit.Entries), fit.Pending)
	}
	big := humanEntry()
	big.Body = strings.Repeat(string(rune(0xE9)), 1000)
	if fit := mustFit(t, Delivery{Nonce: envNonce, Team: envTeam, Entries: []Entry{big, big, big}, Pending: 2}); len(fit.Entries) != 1 || fit.Pending != 4 {
		t.Errorf("big entries: %d, %d pending", len(fit.Entries), fit.Pending)
	}
	if fit := mustFit(t, Delivery{Nonce: envNonce, Team: envTeam, Gap: true}); len(fit.Entries) != 0 || !fit.Gap {
		t.Errorf("no entries: %+v", fit)
	}
	bad := humanEntry()
	bad.EventID = ""
	if _, err := (Delivery{Nonce: envNonce, Team: envTeam, Entries: []Entry{bad}}).Fit(); slotOf(err) != "id" {
		t.Errorf("a bad entry fits: %v", err)
	}
}

func TestDeliveryRenderHoldsTheCapsAndOrder(t *testing.T) {
	big := humanEntry()
	big.Body = strings.Repeat(string(rune(0xE9)), 1000)
	older, newer := peerEntry(), pingEntry()
	older.SentMs, newer.SentMs = envSent-1, envSent
	cases := map[string]struct {
		entries []Entry
		slot    string
	}{
		"nine entries":      {slices.Repeat([]Entry{humanEntry()}, MaxDeliveryEntries+1), "size"},
		"too many bytes":    {[]Entry{big, big}, "size"},
		"peer before human": {[]Entry{peerEntry(), humanEntry()}, "order"},
		"notice first":      {[]Entry{noticeEntry(EventParked), timeoutEntry()}, "order"},
		"newer first":       {[]Entry{newer, older}, "order"},
		"in order":          {[]Entry{humanEntry(), older, newer, timeoutEntry(), noticeEntry(EventParked)}, ""},
	}
	for name, tc := range cases {
		_, err := Delivery{Nonce: envNonce, Team: envTeam, Entries: tc.entries}.Render()
		if got := slotOf(err); got != tc.slot || (err == nil) != (tc.slot == "") {
			t.Errorf("%s: slot %q (%v), want %q", name, got, err, tc.slot)
		}
	}
}

// fullTrailing is every trailing line at its most: 8 lines and a summary of each kind.
func fullTrailing(d *Delivery) {
	for range MaxTrailingLines + 1 {
		d.Answered = append(d.Answered, Answered{From: longestHandle, Answer: VerbBlocked, Verb: "review", PingID: qid('p'), AfterTimeout: true})
		d.HeldBack = append(d.HeldBack, HeldBack{From: longestHandle, Count: 999_999_999})
		d.NotShown = append(d.NotShown, qid('h'))
	}
	d.Gap, d.Stopped, d.Pending = true, true, maxCount-MaxDeliveryEntries
}

func TestFactsThatCrowdOutAnEntryAreSummarised(t *testing.T) {
	e := largestEntry()
	d := Delivery{Nonce: envNonce, Team: e.Team, Entries: []Entry{e, e}}
	fullTrailing(&d)
	if _, err := d.Render(); slotOf(err) != "size" {
		t.Fatalf("the largest entry beside every fact renders: %v", err)
	}
	fit := mustFit(t, d)
	text, err := fit.Render()
	if err != nil || len(fit.Entries) != 1 {
		t.Fatalf("Fit beside every fact: %d entries, %v", len(fit.Entries), err)
	}
	lines := strings.Split(text, "\n")
	history := "teamchat history --team " + e.Team
	wantLines(t, "summaries", lines[len(lines)-6:],
		"answered: 9 more; see teamchat status --team "+e.Team,
		"held back: 999999999 more from other senders; see "+history,
		"not shown: 9 more; see "+history,
		"gap: some messages could not be fetched; read them with "+history,
		"stopped: this team stopped on a failure that does not clear by itself; run teamchat status --team "+e.Team+" and tell a human",
		"999999992 more pending: they follow in the next delivery",
	)
	d.Entries, d.Team = []Entry{humanEntry()}, envTeam
	if fit := mustFit(t, d); len(fit.Entries) != 1 || fit.summarised {
		t.Errorf("a small entry is summarised needlessly: %d", len(fit.Entries))
	}
}

func TestFrameTextSaysA135InOrder(t *testing.T) {
	items := []string{
		"teamchat delivery", "inbox socket", "labels them as from another Claude session",
		"kind, from, role and team lines from the team's verified roster", "HUMAN is a request from a team human",
		"inside a JSON string", "words of the coordinator that asked the question, never the human's",
		"teamchat verify '<id>'", "restored:", "reply:", "Never carry one team's messages to another team",
		"answered:, held back:, not shown:, gap:, stopped:", "never messages",
	}
	at := 0
	for _, item := range items {
		i := strings.Index(FrameText[at:], item)
		if i < 0 {
			t.Fatalf("%q missing or out of order", item)
		}
		at += i + len(item)
	}
}
