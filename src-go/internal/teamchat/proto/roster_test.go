package proto

import (
	"errors"
	"strings"
	"testing"
)

const testTeam = "myteam"

var (
	mainRoom      = "!" + strings.Repeat("M", 43)
	reportsRoom   = "!" + strings.Repeat("R", 43)
	questionsRoom = "!" + strings.Repeat("Q", 43)
	coordID       = UserID("myrepo.1+desk.podman", testServer)
	workerID      = UserID("myrepo.2+desk.podman", testServer)
	lxcWorkerID   = UserID("myrepo.3+desk.lxc", testServer)
	parkedID      = UserID("other.4+desk.podman", testServer)
	probeID       = UserID(ProbeHandle("desk").String(), testServer)
	aliceID       = UserID("alice", testServer)
)

// rosterJSON is A 5.1's example roster on the test server.
func rosterJSON() string {
	return `{"v":1,"team":"myteam","name":"My team","rev":42,"restore_epoch":0,"question_floor":0,
"humans":["` + aliceID + `"],
"members":{"` + coordID + `":{"role":"coordinator"},"` + workerID + `":{"role":"worker"},
"` + lxcWorkerID + `":{"role":"worker","human_text":false},"` + parkedID + `":{"role":"worker","parked":true},
"` + probeID + `":{"role":"probe","parked":true}},
"rooms":{"reports":"` + reportsRoom + `","questions":"` + questionsRoom + `"},
"forge":{"kind":"github","org":"example-org","repos":[{"repo":"example-org/myrepo","branches":["main"]},{"repo":"example-org/docs"}],
"exclude":["example-org/private-notes"],"path_prefixes":[]},
"limits":{"send_per_minute":20,"send_burst":10}}`
}

func testRoster(t *testing.T) Roster {
	t.Helper()
	r, err := DecodeRoster(mustParse(t, rosterJSON()), testServer, testTeam, mainRoom)
	if err != nil {
		t.Fatalf("DecodeRoster(example): %v", err)
	}
	return r
}

func rosterRule(err error) string {
	if re, ok := errors.AsType[*RosterError](err); ok {
		return re.Rule
	}
	return ""
}

func TestDecodeRosterExample(t *testing.T) {
	r := testRoster(t)
	if r.Team != testTeam || r.Name != "My team" || r.Rev != 42 || len(r.Humans) != 1 || len(r.Members) != 5 {
		t.Fatalf("roster = %+v", r)
	}
	if m := r.Members[lxcWorkerID]; m.Role != RoleWorker || m.HumanText || m.Parked {
		t.Fatalf("lxc worker = %+v", m)
	}
	if m := r.Members[coordID]; !m.HumanText || m.Parked {
		t.Fatalf("defaults wrong: %+v", m)
	}
}

func TestDecodeRosterExampleSections(t *testing.T) {
	r := testRoster(t)
	if r.Limits != DefaultLimits() {
		t.Fatalf("limits = %+v", r.Limits)
	}
	if r.Forge.Org != "example-org" || len(r.Forge.Repos) != 2 || r.Forge.Repos[1].Branches != nil || r.Forge.PathPrefixes != nil {
		t.Fatalf("forge = %+v", r.Forge)
	}
	if r.Rooms.Reports != reportsRoom || r.Rooms.Questions != questionsRoom {
		t.Fatalf("rooms = %+v", r.Rooms)
	}
}

func TestRosterEncodeRoundTrips(t *testing.T) {
	r := testRoster(t)
	back, err := DecodeRoster(r.Encode(), testServer, testTeam, mainRoom)
	if err != nil {
		t.Fatalf("decode of Encode: %v", err)
	}
	if !back.Encode().Equal(r.Encode()) || string(back.Encode().Compact()) != string(r.Encode().Compact()) {
		t.Fatal("Encode is not stable")
	}
	if !strings.Contains(string(r.Encode().Compact()), `"members":{"@myrepo.1+desk.podman:`) {
		t.Fatalf("members not in sorted order: %s", r.Encode().Compact())
	}
	other := back
	other.Rev = 99
	if !r.SameExceptRev(other) {
		t.Fatal("a roster differing only in rev is not the same")
	}
	other.Name = "Renamed"
	if r.SameExceptRev(other) {
		t.Fatal("a rename is not a difference")
	}
}

// rosterEdit changes one key of the example roster's text.
type rosterEdit struct{ old, new string }

func TestDecodeRosterRefuses(t *testing.T) {
	cases := map[string]rosterEdit{
		"version":        {`"v":1`, `"v":2`},
		"features":       {`"v":1,`, `"v":1,"features":[],`},
		"team":           {`"team":"myteam"`, `"team":"other"`},
		"name":           {`"name":"My team"`, `"name":""`},
		"rev":            {`"rev":42`, `"rev":0`},
		"restore_epoch":  {`"restore_epoch":0`, `"restore_epoch":1000000000`},
		"question_floor": {`"question_floor":0`, `"question_floor":-1`},
		"humans":         {`"humans":["` + aliceID + `"]`, `"humans":[]`},
		"rooms":          {`"questions":"` + questionsRoom + `"`, `"questions":"` + reportsRoom + `"`},
		"forge":          {`"kind":"github"`, `"kind":"gitlab"`},
		"limits":         {`"send_burst":10`, `"send_burst":31`},
		"shape":          {`"rev":42`, `"rev":"42"`},
	}
	for want, edit := range cases {
		t.Run(want, func(t *testing.T) {
			text := rosterJSON()
			if !strings.Contains(text, edit.old) {
				t.Fatalf("edit %q does not apply", edit.old)
			}
			_, err := DecodeRoster(mustParse(t, strings.Replace(text, edit.old, edit.new, 1)), testServer, testTeam, mainRoom)
			if got := rosterRule(err); got != want {
				t.Fatalf("rule = %q (%v), want %q", got, err, want)
			}
		})
	}
	if _, err := DecodeRoster(Int(1), testServer, testTeam, mainRoom); rosterRule(err) != "shape" {
		t.Fatalf("a non-object gave %v", err)
	}
}

func TestRosterValidateRules(t *testing.T) {
	cases := map[string]func(r *Roster){
		"humans duplicate":       func(r *Roster) { r.Humans = append(r.Humans, aliceID) },
		"humans agent":           func(r *Roster) { r.Humans = []string{workerID} },
		"humans other server":    func(r *Roster) { r.Humans = []string{"@alice:elsewhere"} },
		"humans too many":        func(r *Roster) { r.Humans = manyHumans(17) },
		"members human":          func(r *Roster) { r.Members[aliceID] = RosterMember{Role: RoleWorker, HumanText: true} },
		"members role":           func(r *Roster) { r.Members[workerID] = RosterMember{Role: "orchestrator", HumanText: true} },
		"two coordinators":       func(r *Roster) { r.Members[workerID] = RosterMember{Role: RoleCoordinator, HumanText: true} },
		"coordinator human_text": func(r *Roster) { r.Members[coordID] = RosterMember{Role: RoleCoordinator} },
		"parked coordinator text": func(r *Roster) {
			r.Members[parkedID] = RosterMember{Role: RoleCoordinator, Parked: true}
		},
		"probe role on a worker": func(r *Roster) { r.Members[workerID] = RosterMember{Role: RoleProbe, HumanText: true} },
		"probe handle as worker": func(r *Roster) { r.Members[probeID] = RosterMember{Role: RoleWorker, HumanText: true} },
		"two probes": func(r *Roster) {
			r.Members[UserID(ProbeHandle("lab").String(), testServer)] = RosterMember{Role: RoleProbe}
		},
		"rooms main":              func(r *Roster) { r.Rooms.Reports = mainRoom },
		"rooms grammar":           func(r *Roster) { r.Rooms.Questions = "!short" },
		"forge no org no repo":    func(r *Roster) { r.Forge.Org, r.Forge.Repos = "", nil },
		"forge bad org":           func(r *Roster) { r.Forge.Org = "Example" },
		"forge repo duplicate":    func(r *Roster) { r.Forge.Repos = append(r.Forge.Repos, ForgeRepo{Repo: "example-org/docs"}) },
		"forge repo grammar":      func(r *Roster) { r.Forge.Repos[0].Repo = "example-org/x.git" },
		"forge branches empty":    func(r *Roster) { r.Forge.Repos[0].Branches = []string{} },
		"forge branches too many": func(r *Roster) { r.Forge.Repos[0].Branches = strings.Split("a b c d e f g h i", " ") },
		"forge branch grammar":    func(r *Roster) { r.Forge.Repos[0].Branches = []string{"a b"} },
		"forge exclude listed":    func(r *Roster) { r.Forge.Exclude = []string{"example-org/docs"} },
		"forge exclude grammar":   func(r *Roster) { r.Forge.Exclude = []string{"private-notes"} },
		"forge prefix grammar":    func(r *Roster) { r.Forge.PathPrefixes = []string{"../x"} },
		"limits low":              func(r *Roster) { r.Limits.RecvPerSenderMinute = 9 },
		"limits high":             func(r *Roster) { r.Limits.DeliverPerSender = 61 },
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			r := testRoster(t)
			edit(&r)
			if err := r.Validate(testServer, testTeam, mainRoom); err == nil {
				t.Fatal("Validate accepted it")
			}
		})
	}
	if err := testRoster(t).Validate(testServer, testTeam, mainRoom); err != nil {
		t.Fatalf("example invalid: %v", err)
	}
	r := testRoster(t)
	r.Members[coordID] = RosterMember{Role: RoleCoordinator, HumanText: true, Parked: true}
	r.Members[workerID] = RosterMember{Role: RoleCoordinator, HumanText: true}
	if err := r.Validate(testServer, testTeam, mainRoom); err != nil {
		t.Fatalf("a parked coordinator beside a live one was refused: %v", err)
	}
}

func manyHumans(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = UserID("h"+strings.Repeat("x", i), testServer)
	}
	return out
}

func TestRosterListBounds(t *testing.T) {
	r := testRoster(t)
	r.Humans = manyHumans(16)
	r.Forge.Exclude = nil
	r.Forge.PathPrefixes = []string{"docs/", "README"}
	if err := r.Validate(testServer, testTeam, mainRoom); err != nil {
		t.Fatalf("16 humans refused: %v", err)
	}
	r.Members = map[string]RosterMember{}
	for i := 1; i <= 65; i++ {
		h, ok := Prefix{Repo: "r", Site: "s", Type: "podman"}.Numbered(i)
		if !ok {
			t.Fatal("handle")
		}
		r.Members[UserID(h.String(), testServer)] = RosterMember{Role: RoleWorker, HumanText: true}
	}
	if err := r.Validate(testServer, testTeam, mainRoom); err == nil {
		t.Fatal("65 members accepted")
	}
	r.Forge.Repos = nil
	for i := range 33 {
		r.Forge.Repos = append(r.Forge.Repos, ForgeRepo{Repo: "o/r" + strings.Repeat("x", i)})
	}
	if err := r.Forge.Validate(); err == nil {
		t.Fatal("33 repos accepted")
	}
}

func TestSenderClassAndAddressing(t *testing.T) {
	r := testRoster(t)
	cases := map[string]SenderClass{
		AdminID(testServer):            SenderAdmin,
		aliceID:                        SenderHuman,
		coordID:                        SenderCoordinator,
		workerID:                       SenderWorker,
		lxcWorkerID:                    SenderWorker,
		parkedID:                       SenderNone,
		probeID:                        SenderNone,
		UserID("teamchat", testServer): SenderNone,
		UserID("bob", testServer):      SenderNone,
	}
	for id, want := range cases {
		if got := r.Sender(id, testServer); got != want {
			t.Errorf("Sender(%s) = %v, want %v", id, got, want)
		}
	}
	r.Members[probeID] = RosterMember{Role: RoleProbe}
	if r.Sender(probeID, testServer) != SenderProbe {
		t.Fatal("an unparked probe is not class probe")
	}
	if c, ok := r.Coordinator(); !ok || c != coordID {
		t.Fatalf("Coordinator = %q, %v", c, ok)
	}
	for id, want := range map[string]bool{coordID: true, workerID: true, parkedID: false, probeID: false, aliceID: false} {
		if r.AddressableAgent(id) != want {
			t.Errorf("AddressableAgent(%s) = %v", id, !want)
		}
	}
	if !r.HumanText(coordID) || r.HumanText(lxcWorkerID) || r.HumanText(aliceID) {
		t.Fatal("HumanText wrong")
	}
	if !r.IsHuman(aliceID) || r.IsHuman(coordID) {
		t.Fatal("IsHuman wrong")
	}
	r.Members[coordID] = RosterMember{Role: RoleCoordinator, HumanText: true, Parked: true}
	if _, ok := r.Coordinator(); ok {
		t.Fatal("a parked coordinator is the coordinator")
	}
}

func TestNextRev(t *testing.T) {
	for _, c := range [][3]int64{{0, 0, 1}, {5, 3, 6}, {3, 5, 6}, {7, 7, 8}} {
		if got := NextRev(c[0], c[1]); got != c[2] {
			t.Errorf("NextRev(%d, %d) = %d", c[0], c[1], got)
		}
	}
}

func TestDecodeForgeAndLimitsAlone(t *testing.T) {
	f, err := DecodeForge(mustParse(t, `{"kind":"github","repos":[{"repo":"o/r","branches":["main","dev"]}],"path_prefixes":["docs/"]}`))
	if err != nil || f.Org != "" || len(f.Repos[0].Branches) != 2 || f.PathPrefixes[0] != "docs/" {
		t.Fatalf("DecodeForge = %+v, %v", f, err)
	}
	for _, bad := range []string{`[]`, `{"kind":"github","repos":[{"repo":"o/r","x":1}]}`, `{"kind":"github","repos":["o/r"]}`, `{"kind":"github"}`, `{"repos":[]}`} {
		if _, refused := DecodeForge(mustParse(t, bad)); refused == nil {
			t.Errorf("DecodeForge(%s) accepted", bad)
		}
	}
	l, err := DecodeLimits(mustParse(t, `{"ack_timeout_s":60,"unknown":1}`))
	if err != nil || l.AckTimeoutS != 60 || l.SendBurst != 10 {
		t.Fatalf("DecodeLimits = %+v, %v", l, err)
	}
	for _, bad := range []string{`{"ack_timeout_s":59}`, `{"halt_ack_timeout_s":"300"}`, `1`} {
		if _, err := DecodeLimits(mustParse(t, bad)); err == nil {
			t.Errorf("DecodeLimits(%s) accepted", bad)
		}
	}
}
