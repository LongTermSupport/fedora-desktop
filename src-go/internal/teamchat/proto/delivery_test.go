package proto

import (
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
			[]Entry{humanEntry(), humanEntry(), ping, timeoutEntry(), noticeEntry(EventParked), check},
			"6 messages on myteam (2 from humans, 0 from agents, 2 pings, 1 notices, 1 timeouts): alice (HUMAN), " + envCoord + " (PING), " + envWorker + " (TIMEOUT) and 2 more",
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

func TestDeliveryFit(t *testing.T) {
	many := make([]Entry, 10)
	for i := range many {
		many[i] = humanEntry()
	}
	if n, err := (Delivery{Nonce: envNonce, Team: envTeam, Entries: many}).Fit(); err != nil || n != MaxDeliveryEntries {
		t.Errorf("small entries: %d %v", n, err)
	}
	big := humanEntry()
	big.Body = strings.Repeat(string(rune(0xE9)), 1000)
	d := Delivery{Nonce: envNonce, Team: envTeam, Entries: []Entry{big, big, big}, Pending: 2}
	n, err := d.Fit()
	if err != nil || n != 1 {
		t.Fatalf("big entries: %d %v", n, err)
	}
	d.Entries, d.Pending = d.Entries[:2], d.Pending+1
	text, err := d.Render()
	if err != nil || WireBytes(text) <= MaxDeliveryBytes {
		t.Errorf("two big entries fit after all: %v", err)
	}
	if n, err := (Delivery{Nonce: envNonce, Team: envTeam}).Fit(); err != nil || n != 0 {
		t.Errorf("no entries: %d %v", n, err)
	}
	bad := humanEntry()
	bad.EventID = ""
	if _, err := (Delivery{Nonce: envNonce, Team: envTeam, Entries: []Entry{bad}}).Fit(); slotOf(err) != "id" {
		t.Errorf("a bad entry fits: %v", err)
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

func TestFactsThatCrowdOutAnEntryGoAlone(t *testing.T) {
	e := largestEntry()
	d := Delivery{Nonce: envNonce, Team: e.Team, Entries: []Entry{e, e}}
	fullTrailing(&d)
	if n, err := d.Fit(); err != nil || n != 0 {
		t.Fatalf("Fit beside every fact: %d %v", n, err)
	}
	d.Entries = nil
	text, err := d.Render()
	if err != nil || WireBytes(text) > MaxDeliveryBytes {
		t.Fatalf("the facts alone do not fit: %v", err)
	}
	d = Delivery{Nonce: envNonce, Team: e.Team, Entries: []Entry{e, e}, Pending: maxCount - 1}
	if n, err := d.Fit(); err != nil || n != 1 {
		t.Fatalf("Fit with only pending: %d %v", n, err)
	}
}

func TestRenderMembership(t *testing.T) {
	desc := "The docs team " + string(rune(0x2014)) + " site and blog"
	got, err := RenderMembership([]string{"joined myteam as " + envHandle + " (worker): " + desc, "teams: myteam"}, envNonce)
	if err != nil {
		t.Fatal(err)
	}
	wantLines(t, "membership", strings.Split(got, "\n"),
		"teamchat delivery 3f9c2a71: a membership notice from teamchat: this session's teams changed",
		"<<teamchat 3f9c2a71 1/1>>",
		"kind: MEMBERSHIP (teamchat's own notice: the owner changed this session's teams on the host)",
		"from: teamchat",
		"to: this session",
		"joined myteam as "+envHandle+" (worker): "+desc,
		"teams: myteam",
		"authority: the owner's change, made on the host. A fact about this session's teams; no team member sent it, and none can.",
		"<<end teamchat 3f9c2a71 1/1>>",
	)
	refused := map[string][]string{
		"none":      nil,
		"empty":     {""},
		"long":      {strings.Repeat(string(rune(0xE9)), MaxMembershipLine+1)},
		"newline":   {"a\nb"},
		"del":       {"a\x7fb"},
		"nel":       {"a" + string(rune(0x85)) + "b"},
		"ls":        {"a" + string(rune(0x2028)) + "b"},
		"ps":        {"a" + string(rune(0x2029)) + "b"},
		"open":      {"a << b"},
		"close":     {"a >> b"},
		"not utf-8": {"a\xffb"},
	}
	for name, lines := range refused {
		if _, err := RenderMembership(lines, envNonce); slotOf(err) != "membership line" {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := RenderMembership([]string{strings.Repeat(string(rune(0xE9)), MaxMembershipLine)}, envNonce); err != nil {
		t.Errorf("600 characters: %v", err)
	}
	if _, err := RenderMembership([]string{"teams: none"}, "nonce"); slotOf(err) != "nonce" {
		t.Errorf("nonce: %v", err)
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
